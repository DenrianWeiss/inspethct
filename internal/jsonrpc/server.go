package jsonrpc

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"inspethct/internal/engine"
	"inspethct/internal/forkengine"
	"inspethct/internal/forkengine/ext"
	"inspethct/internal/forkengine/upstream"
)

type Server struct {
	engine    *forkengine.Engine
	sessionID uint64
	mu        sync.Mutex
	sessions  map[string]*ReplaySession
}

type ReplaySession struct {
	ID         string                          `json:"id"`
	TargetTx   string                          `json:"targetTx"`
	Exact      bool                            `json:"exact"`
	Limitation string                          `json:"limitation,omitempty"`
	Position   int                             `json:"position"`
	Done       bool                            `json:"done"`
	Result     *engine.ExecutionResult         `json:"result,omitempty"`
	Trace      []forkengine.ReplayTraceStep    `json:"trace"`
}

type request struct {
	JSONRPC string            `json:"jsonrpc"`
	ID      any               `json:"id"`
	Method  string            `json:"method"`
	Params  []json.RawMessage `json:"params"`
}

type response struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      any         `json:"id,omitempty"`
	Result  any         `json:"result,omitempty"`
	Error   *respError  `json:"error,omitempty"`
}

type respError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type callArgs struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Input    string `json:"input"`
	Data     string `json:"data"`
	Value    string `json:"value"`
	Gas      string `json:"gas"`
	GasPrice string `json:"gasPrice"`
}

func NewServer(engineRef *forkengine.Engine) *Server {
	return &Server{engine: engineRef, sessions: make(map[string]*ReplaySession)}
}

func (server *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()
	var req request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeResponse(w, response{JSONRPC: "2.0", Error: &respError{Code: -32700, Message: err.Error()}})
		return
	}
	result, rpcErr := server.handle(r.Context(), req)
	resp := response{JSONRPC: "2.0", ID: req.ID, Result: result}
	if rpcErr != nil {
		resp.Result = nil
		resp.Error = rpcErr
	}
	writeResponse(w, resp)
}

func (server *Server) handle(ctx context.Context, req request) (any, *respError) {
	switch req.Method {
	case "eth_chainId":
		chainID, err := server.engine.ChainID(ctx)
		if err != nil {
			return nil, internalError(err)
		}
		return encodeQuantity(chainID), nil
	case "eth_call":
		callReq, rpcErr := decodeEthCall(req.Params)
		if rpcErr != nil {
			return nil, rpcErr
		}
		result, err := server.engine.EthCall(ctx, callReq)
		if err != nil {
			return nil, internalError(err)
		}
		return encodeBytes(result), nil
	case "eth_estimateGas":
		callReq, rpcErr := decodeEthCall(req.Params)
		if rpcErr != nil {
			return nil, rpcErr
		}
		gas, err := server.engine.EstimateGas(ctx, ext.EstimateGasRequest(callReq))
		if err != nil {
			return nil, internalError(err)
		}
		return encodeQuantity(new(big.Int).SetUint64(gas)), nil
	case "debug_replayTransaction":
		txHash, rpcErr := decodeHashParam(req.Params)
		if rpcErr != nil {
			return nil, rpcErr
		}
		replay, err := server.engine.ReplayTransaction(ctx, txHash)
		if err != nil {
			return nil, internalError(err)
		}
		return encodeReplayExecution(replay), nil
	case "debug_exportSnapshot":
		var txHash *engine.Hash
		if len(req.Params) > 0 && len(req.Params[0]) > 0 && string(req.Params[0]) != "null" {
			decoded, rpcErr := decodeHashParam(req.Params)
			if rpcErr != nil {
				return nil, rpcErr
			}
			txHash = &decoded
		}
		bundle, err := server.engine.ExportBundle(ctx, txHash)
		if err != nil {
			return nil, internalError(err)
		}
		return bundle, nil
	case "debug_findReplayMismatch":
		txHash, rpcErr := decodeHashParam(req.Params)
		if rpcErr != nil {
			return nil, rpcErr
		}
		report, err := server.engine.ExportReplayReport(ctx, txHash)
		if err != nil {
			return nil, internalError(err)
		}
		return report, nil
	case "gdb.startReplaySession":
		txHash, rpcErr := decodeHashParam(req.Params)
		if rpcErr != nil {
			return nil, rpcErr
		}
		return server.startReplaySession(ctx, txHash)
	case "gdb.next":
		sessionID, rpcErr := decodeStringParam(req.Params)
		if rpcErr != nil {
			return nil, rpcErr
		}
		return server.advanceSession(sessionID, false)
	case "gdb.continue":
		sessionID, rpcErr := decodeStringParam(req.Params)
		if rpcErr != nil {
			return nil, rpcErr
		}
		return server.advanceSession(sessionID, true)
	case "gdb.state":
		sessionID, rpcErr := decodeStringParam(req.Params)
		if rpcErr != nil {
			return nil, rpcErr
		}
		return server.sessionState(sessionID)
	default:
		return nil, &respError{Code: -32601, Message: fmt.Sprintf("method %s not found", req.Method)}
	}
}

func (server *Server) startReplaySession(ctx context.Context, txHash engine.Hash) (any, *respError) {
	replay, err := server.engine.ReplayTransactionWithLocalTrace(ctx, txHash)
	if err != nil {
		return nil, internalError(err)
	}
	id := fmt.Sprintf("replay-%d", atomic.AddUint64(&server.sessionID, 1))
	session := &ReplaySession{
		ID:         id,
		TargetTx:   hashHex(txHash),
		Exact:      replay.Exact,
		Limitation: replay.Limitation,
		Position:   -1,
		Done:       len(replay.LocalTrace) == 0,
		Result:     replay.Result,
		Trace:      append([]forkengine.ReplayTraceStep(nil), replay.LocalTrace...),
	}
	server.mu.Lock()
	server.sessions[id] = session
	server.mu.Unlock()
	return server.describeSession(session), nil
}

func (server *Server) advanceSession(sessionID string, all bool) (any, *respError) {
	server.mu.Lock()
	defer server.mu.Unlock()
	session, ok := server.sessions[sessionID]
	if !ok {
		return nil, &respError{Code: -32602, Message: "unknown gdb session"}
	}
	if len(session.Trace) == 0 {
		session.Done = true
		return server.describeSession(session), nil
	}
	if all {
		session.Position = len(session.Trace) - 1
	} else if session.Position < len(session.Trace)-1 {
		session.Position++
	}
	session.Done = session.Position >= len(session.Trace)-1
	return server.describeSession(session), nil
}

func (server *Server) sessionState(sessionID string) (any, *respError) {
	server.mu.Lock()
	defer server.mu.Unlock()
	session, ok := server.sessions[sessionID]
	if !ok {
		return nil, &respError{Code: -32602, Message: "unknown gdb session"}
	}
	return server.describeSession(session), nil
}

func (server *Server) describeSession(session *ReplaySession) map[string]any {
	state := map[string]any{
		"id":         session.ID,
		"targetTx":   session.TargetTx,
		"exact":      session.Exact,
		"limitation": session.Limitation,
		"position":   session.Position,
		"done":       session.Done,
		"traceLength": len(session.Trace),
		"result":     encodeExecutionResult(session.Result),
	}
	if session.Position >= 0 && session.Position < len(session.Trace) {
		state["currentStep"] = session.Trace[session.Position]
	}
	return state
}

func decodeEthCall(params []json.RawMessage) (ext.EthCallRequest, *respError) {
	if len(params) == 0 {
		return ext.EthCallRequest{}, &respError{Code: -32602, Message: "missing call object"}
	}
	var args callArgs
	if err := json.Unmarshal(params[0], &args); err != nil {
		return ext.EthCallRequest{}, &respError{Code: -32602, Message: err.Error()}
	}
	from, err := decodeAddress(args.From)
	if err != nil {
		return ext.EthCallRequest{}, &respError{Code: -32602, Message: err.Error()}
	}
	to, err := decodeAddress(args.To)
	if err != nil {
		return ext.EthCallRequest{}, &respError{Code: -32602, Message: err.Error()}
	}
	input, err := decodeBytes(firstNonEmpty(args.Input, args.Data))
	if err != nil {
		return ext.EthCallRequest{}, &respError{Code: -32602, Message: err.Error()}
	}
	value, err := decodeBig(args.Value)
	if err != nil {
		return ext.EthCallRequest{}, &respError{Code: -32602, Message: err.Error()}
	}
	gasLimit, err := decodeUint64(args.Gas)
	if err != nil {
		return ext.EthCallRequest{}, &respError{Code: -32602, Message: err.Error()}
	}
	block, err := decodeBlockRef(params)
	if err != nil {
		return ext.EthCallRequest{}, &respError{Code: -32602, Message: err.Error()}
	}
	_ = args.GasPrice
	return ext.EthCallRequest{From: from, To: to, Input: input, Value: value, GasLimit: gasLimit, Block: block}, nil
}

func decodeBlockRef(params []json.RawMessage) (upstream.BlockRef, error) {
	if len(params) < 2 || string(params[1]) == "null" {
		return upstream.LatestBlock(), nil
	}
	var raw string
	if err := json.Unmarshal(params[1], &raw); err != nil {
		return upstream.BlockRef{}, err
	}
	raw = strings.TrimSpace(raw)
	if raw == "latest" || raw == "pending" || raw == "earliest" || raw == "safe" || raw == "finalized" {
		return upstream.BlockRef{Tag: upstream.BlockTag(raw)}, nil
	}
	value, err := decodeBig(raw)
	if err != nil {
		return upstream.BlockRef{}, err
	}
	if !value.IsUint64() {
		return upstream.BlockRef{}, fmt.Errorf("block number exceeds uint64")
	}
	return upstream.BlockNumber(value.Uint64()), nil
}

func decodeHashParam(params []json.RawMessage) (engine.Hash, *respError) {
	text, rpcErr := decodeStringParam(params)
	if rpcErr != nil {
		return engine.Hash{}, rpcErr
	}
	bytes, err := decodeFixedBytes(text, 32)
	if err != nil {
		return engine.Hash{}, &respError{Code: -32602, Message: err.Error()}
	}
	var hash engine.Hash
	copy(hash[:], bytes)
	return hash, nil
}

func decodeStringParam(params []json.RawMessage) (string, *respError) {
	if len(params) == 0 {
		return "", &respError{Code: -32602, Message: "missing parameter"}
	}
	var text string
	if err := json.Unmarshal(params[0], &text); err != nil {
		return "", &respError{Code: -32602, Message: err.Error()}
	}
	return text, nil
}

func decodeAddress(input string) (engine.Address, error) {
	bytes, err := decodeFixedBytes(input, 20)
	if err != nil {
		return engine.Address{}, err
	}
	var addr engine.Address
	copy(addr[:], bytes)
	return addr, nil
}

func decodeFixedBytes(input string, size int) ([]byte, error) {
	decoded, err := decodeBytes(input)
	if err != nil {
		return nil, err
	}
	if len(decoded) != size {
		return nil, fmt.Errorf("expected %d bytes, got %d", size, len(decoded))
	}
	return decoded, nil
}

func decodeBytes(input string) ([]byte, error) {
	input = strings.TrimPrefix(strings.TrimSpace(input), "0x")
	if input == "" {
		return nil, nil
	}
	if len(input)%2 == 1 {
		input = "0" + input
	}
	decoded, err := hex.DecodeString(input)
	if err != nil {
		return nil, err
	}
	return decoded, nil
}

func decodeBig(input string) (*big.Int, error) {
	if strings.TrimSpace(input) == "" {
		return big.NewInt(0), nil
	}
	input = strings.TrimSpace(input)
	value := new(big.Int)
	if strings.HasPrefix(input, "0x") || strings.HasPrefix(input, "0X") {
		if _, ok := value.SetString(strings.TrimPrefix(strings.TrimPrefix(input, "0x"), "0X"), 16); !ok {
			return nil, fmt.Errorf("invalid hex quantity %q", input)
		}
		return value, nil
	}
	if _, ok := value.SetString(input, 10); !ok {
		return nil, fmt.Errorf("invalid quantity %q", input)
	}
	return value, nil
}

func decodeUint64(input string) (uint64, error) {
	value, err := decodeBig(input)
	if err != nil {
		return 0, err
	}
	if value == nil || value.Sign() == 0 {
		return 0, nil
	}
	if !value.IsUint64() {
		return 0, fmt.Errorf("quantity exceeds uint64")
	}
	return value.Uint64(), nil
}

func encodeQuantity(value *big.Int) string {
	if value == nil || value.Sign() == 0 {
		return "0x0"
	}
	return "0x" + strings.TrimLeft(value.Text(16), "0")
}

func hashHex(hash engine.Hash) string {
	return upstream.EncodeHex(hash[:])
}

func encodeReplayExecution(replay *forkengine.ReplayExecution) map[string]any {
	if replay == nil {
		return map[string]any{}
	}
	applied := make([]string, 0, len(replay.AppliedPriorTransactions))
	for _, hash := range replay.AppliedPriorTransactions {
		applied = append(applied, hashHex(hash))
	}
	return map[string]any{
		"exact":                    replay.Exact,
		"limitation":               replay.Limitation,
		"appliedPriorTransactions": applied,
		"transactionHash":          hashHex(replay.Transaction.Hash),
		"transactionType":          replay.Transaction.Type,
		"blobHashCount":            len(replay.Transaction.BlobHashes),
		"result":                   encodeExecutionResult(replay.Result),
	}
}

func encodeExecutionResult(result *engine.ExecutionResult) map[string]any {
	if result == nil {
		return nil
	}
	response := map[string]any{
		"status":       result.Status,
		"gasUsed":      result.GasUsed,
		"gasRemaining": result.GasRemaining,
		"gasRefund":    result.GasRefund,
		"returnData":   encodeBytes(result.ReturnData),
	}
	if result.CreatedAddress != nil {
		response["createdAddress"] = addressHex(*result.CreatedAddress)
	}
	if result.Err != nil {
		response["error"] = result.Err.Error()
	}
	response["logs"] = result.Logs
	response["traceLength"] = len(result.Trace)
	return response
}

func addressHex(addr engine.Address) string {
	return upstream.EncodeHex(addr[:])
}

func encodeBytes(data []byte) string {
	if len(data) == 0 {
		return "0x"
	}
	return upstream.EncodeHex(data)
}

func writeResponse(w http.ResponseWriter, resp response) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(response{JSONRPC: "2.0", ID: resp.ID, Error: &respError{Code: -32001, Message: err.Error()}})
	}
}

func internalError(err error) *respError {
	return &respError{Code: -32000, Message: err.Error()}
}

func firstNonEmpty(left, right string) string {
	if strings.TrimSpace(left) != "" {
		return left
	}
	return right
}