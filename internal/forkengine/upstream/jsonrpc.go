package upstream

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"inspethct/internal/engine"
)

type BatchMode string

const (
	BatchDisabled BatchMode = "disabled"
	BatchEnabled  BatchMode = "enabled"
	BatchProbe    BatchMode = "probe"
)

type RetryConfig struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

type RateLimitConfig struct {
	RequestsPerSecond float64
}

type BatchConfig struct {
	Mode         BatchMode
	MaxBatchSize int
}

type JSONRPCConfig struct {
	Endpoint   string
	HTTPClient *http.Client
	Headers    map[string]string
	Retry      RetryConfig
	RateLimit  RateLimitConfig
	Batch      BatchConfig
}

type BatchCall struct {
	Method string
	Params any
	Result any
}

type JSONRPCClient struct {
	endpoint   string
	httpClient *http.Client
	headers    map[string]string
	retry      RetryConfig
	rateLimit  RateLimitConfig
	batch      BatchConfig

	mu             sync.Mutex
	lastRequest    time.Time
	batchProbeDone bool
	batchSupported bool
	now            func() time.Time
	sleep          func(time.Duration)
	requestID      uint64
}

type JSONRPCProvider struct {
	client *JSONRPCClient
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      uint64 `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (err rpcError) Error() string {
	return fmt.Sprintf("json-rpc error %d: %s", err.Code, err.Message)
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      uint64          `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

func NewJSONRPCClient(cfg JSONRPCConfig) (*JSONRPCClient, error) {
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, errors.New("upstream: json-rpc endpoint is required")
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	retry := cfg.Retry
	if retry.MaxAttempts <= 0 {
		retry.MaxAttempts = 1
	}
	if retry.BaseDelay <= 0 {
		retry.BaseDelay = 100 * time.Millisecond
	}
	if retry.MaxDelay <= 0 {
		retry.MaxDelay = retry.BaseDelay * 8
	}
	batch := cfg.Batch
	if batch.Mode == "" {
		batch.Mode = BatchDisabled
	}
	if batch.MaxBatchSize <= 0 {
		batch.MaxBatchSize = 20
	}
	return &JSONRPCClient{
		endpoint:   cfg.Endpoint,
		httpClient: client,
		headers:    cloneHeaders(cfg.Headers),
		retry:      retry,
		rateLimit:  cfg.RateLimit,
		batch:      batch,
		now:        time.Now,
		sleep:      time.Sleep,
	}, nil
}

func NewJSONRPCProvider(cfg JSONRPCConfig) (*JSONRPCProvider, error) {
	client, err := NewJSONRPCClient(cfg)
	if err != nil {
		return nil, err
	}
	return &JSONRPCProvider{client: client}, nil
}

func (client *JSONRPCClient) Call(ctx context.Context, method string, params any, result any) error {
	request := rpcRequest{
		JSONRPC: "2.0",
		ID:      client.nextID(),
		Method:  method,
		Params:  normalizeParams(params),
	}
	response, err := client.doSingle(ctx, request)
	if err != nil {
		return err
	}
	if response.Error != nil {
		return response.Error
	}
	if result == nil || len(response.Result) == 0 || string(response.Result) == "null" {
		return nil
	}
	if err := json.Unmarshal(response.Result, result); err != nil {
		return fmt.Errorf("upstream: decode result for %s: %w", method, err)
	}
	return nil
}

func (client *JSONRPCClient) BatchCall(ctx context.Context, calls []BatchCall) error {
	if len(calls) == 0 {
		return nil
	}
	if len(calls) == 1 {
		return client.Call(ctx, calls[0].Method, calls[0].Params, calls[0].Result)
	}
	mode := client.batch.Mode
	if mode == BatchDisabled {
		return client.callSerial(ctx, calls)
	}
	if mode == BatchEnabled {
		return client.callBatched(ctx, calls)
	}
	if client.batchIsSupported() {
		return client.callBatched(ctx, calls)
	}
	err := client.callBatched(ctx, calls)
	if err == nil {
		client.setBatchSupported(true)
		return nil
	}
	if isBatchUnsupported(err) {
		client.setBatchSupported(false)
		return client.callSerial(ctx, calls)
	}
	return err
}

func (provider *JSONRPCProvider) ChainID(ctx context.Context) (*big.Int, error) {
	var raw string
	if err := provider.client.Call(ctx, "eth_chainId", nil, &raw); err != nil {
		return nil, err
	}
	return parseHexBig(raw)
}

func (provider *JSONRPCProvider) GetBalance(ctx context.Context, addr engine.Address, block BlockRef) (*big.Int, error) {
	var raw string
	if err := provider.client.Call(ctx, "eth_getBalance", []any{encodeAddress(addr), encodeBlockRef(block)}, &raw); err != nil {
		return nil, err
	}
	return parseHexBig(raw)
}

func (provider *JSONRPCProvider) GetNonce(ctx context.Context, addr engine.Address, block BlockRef) (uint64, error) {
	var raw string
	if err := provider.client.Call(ctx, "eth_getTransactionCount", []any{encodeAddress(addr), encodeBlockRef(block)}, &raw); err != nil {
		return 0, err
	}
	return parseHexUint64(raw)
}

func (provider *JSONRPCProvider) GetCode(ctx context.Context, addr engine.Address, block BlockRef) ([]byte, error) {
	var raw string
	if err := provider.client.Call(ctx, "eth_getCode", []any{encodeAddress(addr), encodeBlockRef(block)}, &raw); err != nil {
		return nil, err
	}
	return parseHexBytes(raw)
}

func (provider *JSONRPCProvider) GetStorageAt(ctx context.Context, addr engine.Address, slot engine.Hash, block BlockRef) (engine.Hash, error) {
	var raw string
	if err := provider.client.Call(ctx, "eth_getStorageAt", []any{encodeAddress(addr), encodeHash(slot), encodeBlockRef(block)}, &raw); err != nil {
		return engine.Hash{}, err
	}
	return parseHash(raw)
}

func (provider *JSONRPCProvider) GetBlock(ctx context.Context, block BlockRef) (Block, error) {
	var raw rpcBlock
	if err := provider.client.Call(ctx, "eth_getBlockByNumber", []any{encodeBlockRef(block), false}, &raw); err != nil {
		return Block{}, err
	}
	return raw.intoBlock()
}

func (provider *JSONRPCProvider) GetBlockTransactions(ctx context.Context, block BlockRef) ([]Transaction, error) {
	var raw rpcBlockWithTransactions
	if err := provider.client.Call(ctx, "eth_getBlockByNumber", []any{encodeBlockRef(block), true}, &raw); err != nil {
		return nil, err
	}
	transactions := make([]Transaction, 0, len(raw.Transactions))
	for _, entry := range raw.Transactions {
		transaction, err := entry.intoTransaction()
		if err != nil {
			return nil, err
		}
		transactions = append(transactions, transaction)
	}
	return transactions, nil
}

func (provider *JSONRPCProvider) GetTransactionByHash(ctx context.Context, txHash engine.Hash) (Transaction, error) {
	var raw rpcTransaction
	if err := provider.client.Call(ctx, "eth_getTransactionByHash", []any{encodeHash(txHash)}, &raw); err != nil {
		return Transaction{}, err
	}
	return raw.intoTransaction()
}

func (provider *JSONRPCProvider) GetTransactionReceipt(ctx context.Context, txHash engine.Hash) (Receipt, error) {
	var raw rpcReceipt
	if err := provider.client.Call(ctx, "eth_getTransactionReceipt", []any{encodeHash(txHash)}, &raw); err != nil {
		return Receipt{}, err
	}
	return raw.intoReceipt()
}

func (provider *JSONRPCProvider) DebugTraceTransaction(ctx context.Context, txHash engine.Hash, cfg TraceTransactionConfig) (TraceTransactionResult, error) {
	result := TraceTransactionResult{}
	params := []any{encodeHash(txHash), cfg.intoMap()}
	if err := provider.client.Call(ctx, "debug_traceTransaction", params, &result); err != nil {
		return nil, err
	}
	return result, nil
}

type rpcBlock struct {
	Number      string `json:"number"`
	Hash        string `json:"hash"`
	ParentHash  string `json:"parentHash"`
	Timestamp   string `json:"timestamp"`
	GasLimit    string `json:"gasLimit"`
	BaseFee     string `json:"baseFeePerGas"`
	BlobBaseFee string `json:"blobBaseFeePerGas"`
	Miner       string `json:"miner"`
	PrevRandao  string `json:"mixHash"`
}

type rpcBlockWithTransactions struct {
	Transactions []rpcTransaction `json:"transactions"`
}

type rpcAccessListEntry struct {
	Address     string   `json:"address"`
	StorageKeys []string `json:"storageKeys"`
}

type rpcTransaction struct {
	Hash             string               `json:"hash"`
	BlockHash        string               `json:"blockHash"`
	BlockNumber      string               `json:"blockNumber"`
	From             string               `json:"from"`
	To               *string              `json:"to"`
	Type             string               `json:"type"`
	Gas              string               `json:"gas"`
	GasPrice         string               `json:"gasPrice"`
	BlobGasFeeCap    string               `json:"maxFeePerBlobGas"`
	BlobHashes       []string             `json:"blobVersionedHashes"`
	AccessList       []rpcAccessListEntry `json:"accessList"`
	Input            string               `json:"input"`
	Nonce            string               `json:"nonce"`
	TransactionIndex *string              `json:"transactionIndex"`
	Value            string               `json:"value"`
}

type rpcReceipt struct {
	TransactionHash   string   `json:"transactionHash"`
	TransactionIndex  string   `json:"transactionIndex"`
	BlockHash         string   `json:"blockHash"`
	BlockNumber       string   `json:"blockNumber"`
	From              string   `json:"from"`
	To                *string  `json:"to"`
	GasUsed           string   `json:"gasUsed"`
	CumulativeGasUsed string   `json:"cumulativeGasUsed"`
	EffectiveGasPrice string   `json:"effectiveGasPrice"`
	ContractAddress   *string  `json:"contractAddress"`
	Status            *string  `json:"status"`
	Logs              []rpcLog `json:"logs"`
	LogsBloom         string   `json:"logsBloom"`
}

type rpcLog struct {
	Address string   `json:"address"`
	Topics  []string `json:"topics"`
	Data    string   `json:"data"`
}

func (block rpcBlock) intoBlock() (Block, error) {
	number, err := parseHexBig(block.Number)
	if err != nil {
		return Block{}, err
	}
	hash, err := parseHash(block.Hash)
	if err != nil {
		return Block{}, err
	}
	parentHash, err := parseHash(block.ParentHash)
	if err != nil {
		return Block{}, err
	}
	timestamp, err := parseHexUint64(block.Timestamp)
	if err != nil {
		return Block{}, err
	}
	gasLimit, err := parseHexUint64(block.GasLimit)
	if err != nil {
		return Block{}, err
	}
	baseFee, err := parseOptionalHexBig(block.BaseFee)
	if err != nil {
		return Block{}, err
	}
	blobBaseFee, err := parseOptionalHexBig(block.BlobBaseFee)
	if err != nil {
		return Block{}, err
	}
	coinbase, err := parseAddress(block.Miner)
	if err != nil {
		return Block{}, err
	}
	prevRandao, err := parseOptionalHash(block.PrevRandao)
	if err != nil {
		return Block{}, err
	}
	return Block{Number: number, Hash: hash, ParentHash: parentHash, Timestamp: timestamp, GasLimit: gasLimit, BaseFee: baseFee, BlobBaseFee: blobBaseFee, Coinbase: coinbase, PrevRandao: prevRandao}, nil
}

func (tx rpcTransaction) intoTransaction() (Transaction, error) {
	blockNumber, err := parseOptionalHexBig(tx.BlockNumber)
	if err != nil {
		return Transaction{}, err
	}
	hash, err := parseHash(tx.Hash)
	if err != nil {
		return Transaction{}, err
	}
	blockHash, err := parseOptionalHash(tx.BlockHash)
	if err != nil {
		return Transaction{}, err
	}
	from, err := parseAddress(tx.From)
	if err != nil {
		return Transaction{}, err
	}
	to, err := parseOptionalAddress(tx.To)
	if err != nil {
		return Transaction{}, err
	}
	txType, err := parseOptionalHexUint64Value(tx.Type)
	if err != nil {
		return Transaction{}, err
	}
	gas, err := parseHexUint64(tx.Gas)
	if err != nil {
		return Transaction{}, err
	}
	gasPrice, err := parseHexBig(tx.GasPrice)
	if err != nil {
		return Transaction{}, err
	}
	blobGasFeeCap, err := parseOptionalHexBig(tx.BlobGasFeeCap)
	if err != nil {
		return Transaction{}, err
	}
	blobHashes, err := parseHashList(tx.BlobHashes)
	if err != nil {
		return Transaction{}, err
	}
	accessList, err := parseAccessList(tx.AccessList)
	if err != nil {
		return Transaction{}, err
	}
	input, err := parseHexBytes(tx.Input)
	if err != nil {
		return Transaction{}, err
	}
	nonce, err := parseHexUint64(tx.Nonce)
	if err != nil {
		return Transaction{}, err
	}
	index, err := parseOptionalHexUint64(tx.TransactionIndex)
	if err != nil {
		return Transaction{}, err
	}
	value, err := parseHexBig(tx.Value)
	if err != nil {
		return Transaction{}, err
	}
	return Transaction{Hash: hash, BlockHash: blockHash, BlockNumber: blockNumber, From: from, To: to, Type: txType, Gas: gas, GasPrice: gasPrice, BlobGasFeeCap: blobGasFeeCap, BlobHashes: blobHashes, AccessList: accessList, Input: input, Nonce: nonce, TransactionIndex: index, Value: value}, nil
}

func (receipt rpcReceipt) intoReceipt() (Receipt, error) {
	txHash, err := parseHash(receipt.TransactionHash)
	if err != nil {
		return Receipt{}, err
	}
	txIndex, err := parseHexUint64(receipt.TransactionIndex)
	if err != nil {
		return Receipt{}, err
	}
	blockHash, err := parseHash(receipt.BlockHash)
	if err != nil {
		return Receipt{}, err
	}
	blockNumber, err := parseHexBig(receipt.BlockNumber)
	if err != nil {
		return Receipt{}, err
	}
	from, err := parseAddress(receipt.From)
	if err != nil {
		return Receipt{}, err
	}
	to, err := parseOptionalAddress(receipt.To)
	if err != nil {
		return Receipt{}, err
	}
	gasUsed, err := parseHexUint64(receipt.GasUsed)
	if err != nil {
		return Receipt{}, err
	}
	cumulativeGasUsed, err := parseHexUint64(receipt.CumulativeGasUsed)
	if err != nil {
		return Receipt{}, err
	}
	effectiveGasPrice, err := parseOptionalHexBig(receipt.EffectiveGasPrice)
	if err != nil {
		return Receipt{}, err
	}
	contractAddress, err := parseOptionalAddress(receipt.ContractAddress)
	if err != nil {
		return Receipt{}, err
	}
	status, err := parseOptionalHexUint64(receipt.Status)
	if err != nil {
		return Receipt{}, err
	}
	logs := make([]engine.Log, 0, len(receipt.Logs))
	for _, logEntry := range receipt.Logs {
		logValue, err := logEntry.intoLog()
		if err != nil {
			return Receipt{}, err
		}
		logs = append(logs, logValue)
	}
	return Receipt{TransactionHash: txHash, TransactionIndex: txIndex, BlockHash: blockHash, BlockNumber: blockNumber, From: from, To: to, GasUsed: gasUsed, CumulativeGasUsed: cumulativeGasUsed, EffectiveGasPrice: effectiveGasPrice, ContractAddress: contractAddress, Status: status, Logs: logs}, nil
}

func (logEntry rpcLog) intoLog() (engine.Log, error) {
	address, err := parseAddress(logEntry.Address)
	if err != nil {
		return engine.Log{}, err
	}
	topics := make([]engine.Hash, 0, len(logEntry.Topics))
	for _, topic := range logEntry.Topics {
		parsed, err := parseHash(topic)
		if err != nil {
			return engine.Log{}, err
		}
		topics = append(topics, parsed)
	}
	data, err := parseHexBytes(logEntry.Data)
	if err != nil {
		return engine.Log{}, err
	}
	return engine.Log{Address: address, Topics: topics, Data: data}, nil
}

func (cfg TraceTransactionConfig) intoMap() map[string]any {
	options := map[string]any{}
	if cfg.Tracer != "" {
		options["tracer"] = cfg.Tracer
	}
	if cfg.Timeout != "" {
		options["timeout"] = cfg.Timeout
	}
	if cfg.Reexec != nil {
		options["reexec"] = *cfg.Reexec
	}
	if cfg.DisableStack {
		options["disableStack"] = true
	}
	if cfg.DisableMemory {
		options["disableMemory"] = true
	}
	if cfg.DisableStorage {
		options["disableStorage"] = true
	}
	return options
}

func (client *JSONRPCClient) callSerial(ctx context.Context, calls []BatchCall) error {
	for _, call := range calls {
		if err := client.Call(ctx, call.Method, call.Params, call.Result); err != nil {
			return err
		}
	}
	return nil
}

func (client *JSONRPCClient) callBatched(ctx context.Context, calls []BatchCall) error {
	for start := 0; start < len(calls); start += client.batch.MaxBatchSize {
		end := start + client.batch.MaxBatchSize
		if end > len(calls) {
			end = len(calls)
		}
		if err := client.doBatchChunk(ctx, calls[start:end]); err != nil {
			return err
		}
	}
	return nil
}

func (client *JSONRPCClient) doBatchChunk(ctx context.Context, calls []BatchCall) error {
	requests := make([]rpcRequest, 0, len(calls))
	results := make(map[uint64]any, len(calls))
	for _, call := range calls {
		request := rpcRequest{JSONRPC: "2.0", ID: client.nextID(), Method: call.Method, Params: normalizeParams(call.Params)}
		requests = append(requests, request)
		results[request.ID] = call.Result
	}
	body, err := client.doWithRetry(ctx, requests)
	if err != nil {
		return err
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return errBatchUnsupported
	}
	responses := make([]rpcResponse, 0, len(calls))
	if err := json.Unmarshal(trimmed, &responses); err != nil {
		return errBatchUnsupported
	}
	byID := make(map[uint64]rpcResponse, len(responses))
	for _, response := range responses {
		byID[response.ID] = response
	}
	for _, request := range requests {
		response, ok := byID[request.ID]
		if !ok {
			return fmt.Errorf("upstream: missing batch response for id %d", request.ID)
		}
		if response.Error != nil {
			return response.Error
		}
		result := results[request.ID]
		if result == nil || len(response.Result) == 0 || string(response.Result) == "null" {
			continue
		}
		if err := json.Unmarshal(response.Result, result); err != nil {
			return fmt.Errorf("upstream: decode batch result for %s: %w", request.Method, err)
		}
	}
	return nil
}

var errBatchUnsupported = errors.New("upstream: batch json-rpc unsupported")

func isBatchUnsupported(err error) bool {
	return errors.Is(err, errBatchUnsupported)
}

func (client *JSONRPCClient) doSingle(ctx context.Context, request rpcRequest) (rpcResponse, error) {
	body, err := client.doWithRetry(ctx, request)
	if err != nil {
		return rpcResponse{}, err
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || trimmed[0] == '[' {
		return rpcResponse{}, fmt.Errorf("upstream: invalid single json-rpc response")
	}
	var response rpcResponse
	if err := json.Unmarshal(trimmed, &response); err != nil {
		return rpcResponse{}, err
	}
	return response, nil
}

func (client *JSONRPCClient) doWithRetry(ctx context.Context, payload any) ([]byte, error) {
	var lastErr error
	methods := payloadMethods(payload)
	for attempt := 1; attempt <= client.retry.MaxAttempts; attempt++ {
		if err := client.waitRateLimit(ctx); err != nil {
			return nil, err
		}
		body, status, err := client.doHTTP(ctx, payload)
		if err == nil && status < 400 {
			return body, nil
		}
		if err == nil {
			lastErr = classifyHTTPError(status, body)
		} else {
			lastErr = err
		}
		log.Printf("upstream json-rpc request failed: endpoint=%s methods=%s attempt=%d/%d status=%d err=%v body_prefix=%q", client.endpoint, methods, attempt, client.retry.MaxAttempts, status, lastErr, summarizeBodyPrefix(body, 240))
		if attempt == client.retry.MaxAttempts || !shouldRetry(status, lastErr) {
			break
		}
		delay := retryDelay(client.retry, attempt)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		client.sleep(delay)
	}
	return nil, lastErr
}

func (client *JSONRPCClient) doHTTP(ctx context.Context, payload any) ([]byte, int, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.endpoint, bytes.NewReader(encoded))
	if err != nil {
		return nil, 0, err
	}
	request.Header.Set("Content-Type", "application/json")
	for key, value := range client.headers {
		request.Header.Set(key, value)
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, response.StatusCode, err
	}
	if response.StatusCode >= 400 && response.StatusCode < 500 && response.StatusCode != http.StatusTooManyRequests {
		trimmed := bytes.TrimSpace(body)
		if len(trimmed) > 0 && trimmed[0] != '[' && trimmed[0] != '{' {
			return body, response.StatusCode, fmt.Errorf("%w: status=%d body_prefix=%q", errBatchUnsupported, response.StatusCode, summarizeBodyPrefix(body, 240))
		}
	}
	return body, response.StatusCode, nil
}

func (client *JSONRPCClient) waitRateLimit(ctx context.Context) error {
	if client.rateLimit.RequestsPerSecond <= 0 {
		return nil
	}
	interval := time.Duration(float64(time.Second) / client.rateLimit.RequestsPerSecond)
	client.mu.Lock()
	now := client.now()
	if client.lastRequest.IsZero() {
		client.lastRequest = now
		client.mu.Unlock()
		return nil
	}
	next := client.lastRequest.Add(interval)
	if next.After(now) {
		wait := next.Sub(now)
		client.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		client.sleep(wait)
		client.mu.Lock()
		client.lastRequest = next
		client.mu.Unlock()
		return nil
	}
	client.lastRequest = now
	client.mu.Unlock()
	return nil
}

func (client *JSONRPCClient) batchIsSupported() bool {
	client.mu.Lock()
	defer client.mu.Unlock()
	return client.batchProbeDone && client.batchSupported
}

func (client *JSONRPCClient) setBatchSupported(supported bool) {
	client.mu.Lock()
	defer client.mu.Unlock()
	client.batchProbeDone = true
	client.batchSupported = supported
}

func (client *JSONRPCClient) nextID() uint64 {
	client.mu.Lock()
	defer client.mu.Unlock()
	client.requestID++
	return client.requestID
}

func normalizeParams(params any) any {
	if params == nil {
		return []any{}
	}
	return params
}

func retryDelay(cfg RetryConfig, attempt int) time.Duration {
	multiplier := math.Pow(2, float64(attempt-1))
	delay := time.Duration(float64(cfg.BaseDelay) * multiplier)
	if delay > cfg.MaxDelay {
		return cfg.MaxDelay
	}
	return delay
}

func shouldRetry(status int, err error) bool {
	if errors.Is(err, errBatchUnsupported) {
		return false
	}
	if status == http.StatusTooManyRequests {
		return true
	}
	return status >= 500 || status == 0
}

func classifyHTTPError(status int, body []byte) error {
	if status >= 400 && status < 500 {
		trimmed := bytes.TrimSpace(body)
		if len(trimmed) == 0 || (trimmed[0] != '{' && trimmed[0] != '[') {
			return fmt.Errorf("%w: status=%d body_prefix=%q", errBatchUnsupported, status, summarizeBodyPrefix(body, 240))
		}
	}
	return fmt.Errorf("upstream: http status %d body_prefix=%q", status, summarizeBodyPrefix(body, 240))
}

func payloadMethods(payload any) string {
	switch value := payload.(type) {
	case rpcRequest:
		if value.Method == "" {
			return "<unknown>"
		}
		return value.Method
	case []rpcRequest:
		if len(value) == 0 {
			return "<none>"
		}
		parts := make([]string, 0, len(value))
		for _, request := range value {
			method := strings.TrimSpace(request.Method)
			if method == "" {
				method = "<unknown>"
			}
			parts = append(parts, method)
		}
		return strings.Join(parts, ",")
	default:
		return "<unknown>"
	}
}

func summarizeBodyPrefix(body []byte, limit int) string {
	if limit <= 0 {
		limit = 120
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return ""
	}
	if len(trimmed) > limit {
		return trimmed[:limit] + "..."
	}
	return trimmed
}

func cloneHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	clone := make(map[string]string, len(headers))
	for key, value := range headers {
		clone[key] = value
	}
	return clone
}

func encodeBlockRef(ref BlockRef) string {
	normalized := ref.Normalize()
	if normalized.Number != nil {
		return "0x" + normalized.Number.Text(16)
	}
	return string(normalized.Tag)
}

func encodeAddress(addr engine.Address) string {
	return "0x" + hex.EncodeToString(addr[:])
}

func encodeHash(hash engine.Hash) string {
	return "0x" + hex.EncodeToString(hash[:])
}

func parseHexBig(input string) (*big.Int, error) {
	trimmed := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(input), "0x"), "0X")
	if trimmed == "" {
		return big.NewInt(0), nil
	}
	value, ok := new(big.Int).SetString(trimmed, 16)
	if !ok {
		return nil, fmt.Errorf("upstream: invalid hex integer %q", input)
	}
	return value, nil
}

func parseOptionalHexBig(input string) (*big.Int, error) {
	if strings.TrimSpace(input) == "" || strings.TrimSpace(input) == "null" {
		return nil, nil
	}
	return parseHexBig(input)
}

func parseHexUint64(input string) (uint64, error) {
	value, err := parseHexBig(input)
	if err != nil {
		return 0, err
	}
	if !value.IsUint64() {
		return 0, fmt.Errorf("upstream: value %q overflows uint64", input)
	}
	return value.Uint64(), nil
}

func parseOptionalHexUint64(input *string) (*uint64, error) {
	if input == nil || strings.TrimSpace(*input) == "" || strings.TrimSpace(*input) == "null" {
		return nil, nil
	}
	value, err := parseHexUint64(*input)
	if err != nil {
		return nil, err
	}
	copyValue := value
	return &copyValue, nil
}

func parseOptionalHexUint64Value(input string) (uint64, error) {
	if strings.TrimSpace(input) == "" || strings.TrimSpace(input) == "null" {
		return 0, nil
	}
	return parseHexUint64(input)
}

func parseHexBytes(input string) ([]byte, error) {
	trimmed := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(input), "0x"), "0X")
	if trimmed == "" {
		return nil, nil
	}
	if len(trimmed)%2 == 1 {
		trimmed = "0" + trimmed
	}
	decoded, err := hex.DecodeString(trimmed)
	if err != nil {
		return nil, err
	}
	return decoded, nil
}

func parseHash(input string) (engine.Hash, error) {
	decoded, err := parseHexBytes(input)
	if err != nil {
		return engine.Hash{}, err
	}
	if len(decoded) != len(engine.Hash{}) {
		return engine.Hash{}, fmt.Errorf("upstream: invalid hash length %d", len(decoded))
	}
	var hash engine.Hash
	copy(hash[:], decoded)
	return hash, nil
}

func parseHashList(inputs []string) ([]engine.Hash, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	hashes := make([]engine.Hash, 0, len(inputs))
	for _, input := range inputs {
		hash, err := parseHash(input)
		if err != nil {
			return nil, err
		}
		hashes = append(hashes, hash)
	}
	return hashes, nil
}

func parseAccessList(entries []rpcAccessListEntry) ([]AccessListEntry, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	parsed := make([]AccessListEntry, 0, len(entries))
	for _, entry := range entries {
		address, err := parseAddress(entry.Address)
		if err != nil {
			return nil, err
		}
		keys, err := parseHashList(entry.StorageKeys)
		if err != nil {
			return nil, err
		}
		parsed = append(parsed, AccessListEntry{Address: address, StorageKeys: keys})
	}
	return parsed, nil
}

func parseOptionalHash(input string) (engine.Hash, error) {
	if strings.TrimSpace(input) == "" || strings.TrimSpace(input) == "null" {
		return engine.Hash{}, nil
	}
	return parseHash(input)
}

func parseAddress(input string) (engine.Address, error) {
	decoded, err := parseHexBytes(input)
	if err != nil {
		return engine.Address{}, err
	}
	if len(decoded) != len(engine.Address{}) {
		return engine.Address{}, fmt.Errorf("upstream: invalid address length %d", len(decoded))
	}
	var address engine.Address
	copy(address[:], decoded)
	return address, nil
}

func parseOptionalAddress(input *string) (*engine.Address, error) {
	if input == nil || strings.TrimSpace(*input) == "" || strings.TrimSpace(*input) == "null" {
		return nil, nil
	}
	address, err := parseAddress(*input)
	if err != nil {
		return nil, err
	}
	return &address, nil
}
