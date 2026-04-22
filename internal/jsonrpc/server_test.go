package jsonrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"inspethct/internal/engine"
	"inspethct/internal/forkengine"
	"inspethct/internal/forkengine/upstream"
)

type stubProvider struct {
	block         upstream.Block
	codeByAddress map[engine.Address][]byte
	storageByAddr map[engine.Address]map[engine.Hash]engine.Hash
	transactions  map[engine.Hash]upstream.Transaction
	receipts      map[engine.Hash]upstream.Receipt
}

func (provider *stubProvider) ChainID(ctx context.Context) (*big.Int, error) {
	if provider.block.ChainID != nil {
		return new(big.Int).Set(provider.block.ChainID), nil
	}
	return big.NewInt(1), nil
}

func (provider *stubProvider) GetBalance(ctx context.Context, addr engine.Address, block upstream.BlockRef) (*big.Int, error) {
	return big.NewInt(0), nil
}

func (provider *stubProvider) GetNonce(ctx context.Context, addr engine.Address, block upstream.BlockRef) (uint64, error) {
	return 0, nil
}

func (provider *stubProvider) GetCode(ctx context.Context, addr engine.Address, block upstream.BlockRef) ([]byte, error) {
	return append([]byte(nil), provider.codeByAddress[addr]...), nil
}

func (provider *stubProvider) GetStorageAt(ctx context.Context, addr engine.Address, slot engine.Hash, block upstream.BlockRef) (engine.Hash, error) {
	if provider.storageByAddr != nil {
		if slots := provider.storageByAddr[addr]; slots != nil {
			if value, ok := slots[slot]; ok {
				return value, nil
			}
		}
	}
	return engine.Hash{}, nil
}

func (provider *stubProvider) GetBlock(ctx context.Context, block upstream.BlockRef) (upstream.Block, error) {
	return provider.block.Clone(), nil
}

func (provider *stubProvider) GetTransactionByHash(ctx context.Context, hash engine.Hash) (upstream.Transaction, error) {
	return provider.transactions[hash].Clone(), nil
}

func (provider *stubProvider) GetTransactionReceipt(ctx context.Context, hash engine.Hash) (upstream.Receipt, error) {
	return provider.receipts[hash].Clone(), nil
}

func TestServerSupportsEthDebugAndGDBMethods(t *testing.T) {
	contract := engine.Address{19: 0x44}
	txHash := engine.Hash{0xaa}
	provider := &stubProvider{
		block: upstream.Block{Number: big.NewInt(5), GasLimit: 1_000_000, BaseFee: big.NewInt(1), ChainID: big.NewInt(1)},
		codeByAddress: map[engine.Address][]byte{
			contract: {0x60, 0x2a, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3},
		},
		transactions: map[engine.Hash]upstream.Transaction{
			txHash: {
				Hash:        txHash,
				BlockNumber: big.NewInt(5),
				From:        engine.Address{19: 0x01},
				To:          &contract,
				Gas:         100000,
				GasPrice:    big.NewInt(1),
				Value:       big.NewInt(0),
			},
		},
		receipts: map[engine.Hash]upstream.Receipt{
			txHash: {TransactionHash: txHash, BlockNumber: big.NewInt(5), GasUsed: 21000},
		},
	}
	engineRef, err := forkengine.New(forkengine.Config{Provider: provider, Fork: engine.ForkCancun, Block: upstream.BlockNumber(5)})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	server := httptest.NewServer(NewServer(engineRef))
	defer server.Close()

	callResult := rpcCall(t, server.URL, "eth_call", []any{map[string]any{"from": "0x0000000000000000000000000000000000000001", "to": "0x0000000000000000000000000000000000000044", "input": "0x"}, "0x5"})
	if callResult.(string) != "0x000000000000000000000000000000000000000000000000000000000000002a" {
		t.Fatalf("eth_call result = %v", callResult)
	}

	bundle := rpcCall(t, server.URL, "debug_exportSnapshot", []any{hashHex(txHash)}).(map[string]any)
	if _, ok := bundle["snapshot"]; !ok {
		t.Fatalf("debug_exportSnapshot missing snapshot: %#v", bundle)
	}
	if _, ok := bundle["replayReport"]; !ok {
		t.Fatalf("debug_exportSnapshot missing replayReport: %#v", bundle)
	}

	session := rpcCall(t, server.URL, "gdb.startReplaySession", []any{hashHex(txHash)}).(map[string]any)
	sessionID, ok := session["id"].(string)
	if !ok || sessionID == "" {
		t.Fatalf("gdb.startReplaySession id = %#v", session["id"])
	}
	next := rpcCall(t, server.URL, "gdb.next", []any{sessionID}).(map[string]any)
	if _, ok := next["currentStep"]; !ok {
		t.Fatalf("gdb.next missing currentStep: %#v", next)
	}
	state := rpcCall(t, server.URL, "gdb.state", []any{sessionID}).(map[string]any)
	if state["id"] != sessionID {
		t.Fatalf("gdb.state id = %#v, want %q", state["id"], sessionID)
	}
}

func TestGDBSourceBreakpointAndWriteStorage(t *testing.T) {
	contract := engine.Address{19: 0x55}
	txHash := engine.Hash{0xbb}
	code := []byte{0x60, 0x00, 0x54, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3}
	provider := &stubProvider{
		block:         upstream.Block{Number: big.NewInt(5), GasLimit: 1_000_000, BaseFee: big.NewInt(1), ChainID: big.NewInt(1)},
		codeByAddress: map[engine.Address][]byte{contract: code},
		transactions: map[engine.Hash]upstream.Transaction{
			txHash: {Hash: txHash, BlockNumber: big.NewInt(5), From: engine.Address{19: 0x01}, To: &contract, Gas: 100000, GasPrice: big.NewInt(1), Value: big.NewInt(0)},
		},
		receipts: map[engine.Hash]upstream.Receipt{
			txHash: {TransactionHash: txHash, BlockNumber: big.NewInt(5), GasUsed: 21000},
		},
	}
	engineRef, err := forkengine.New(forkengine.Config{Provider: provider, Fork: engine.ForkCancun, Block: upstream.BlockNumber(5)})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	server := httptest.NewServer(NewServer(engineRef))
	defer server.Close()

	artifactPath := filepath.Join(t.TempDir(), "bundle.json")
	if err := os.WriteFile(artifactPath, []byte(`{
		"sources": {
			"A.sol": {
				"id": 0,
				"content": "aaaa\nbbbb\ncccc\ndddd\n",
				"ast": {"id": 1, "nodeType": "SourceUnit", "src": "0:20:0", "nodes": []}
			}
		},
		"contracts": {
			"A.sol": {
				"C": {
					"abi": [{"type": "function", "name": "f", "inputs": [], "outputs": [{"type": "uint256"}]}],
					"storageLayout": {"storage": [{"astId": 1, "contract": "A.sol:C", "label": "x", "offset": 0, "slot": "0", "type": "t_uint256"}], "types": {"t_uint256": {"encoding": "inplace", "label": "uint256", "numberOfBytes": "32"}}},
					"transientStorageLayout": {"storage": [], "types": {}},
					"evm": {
						"bytecode": {"object": "60005460005260206000f3", "sourceMap": "0:4:0;5:4:0;10:4:0;10:4:0;15:4:0;15:4:0;15:4:0", "generatedSources": []},
						"deployedBytecode": {"object": "60005460005260206000f3", "sourceMap": "0:4:0;5:4:0;10:4:0;10:4:0;15:4:0;15:4:0;15:4:0", "generatedSources": []}
					}
				}
			}
		}
	}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	session := rpcCall(t, server.URL, "gdb.startReplaySession", []any{hashHex(txHash)}).(map[string]any)
	sessionID := session["id"].(string)
	current := session["current"].(map[string]any)
	if current["stepIndex"].(float64) != 0 {
		t.Fatalf("initial pause step = %v, want 0", current["stepIndex"])
	}

	_ = rpcCall(t, server.URL, "gdb.loadSourceBundle", []any{sessionID, map[string]any{"kind": "standard-json", "standardJsonPath": artifactPath, "sourceName": "A.sol", "contractName": "C", "runtime": true, "address": "0x0000000000000000000000000000000000000055"}})
	_ = rpcCall(t, server.URL, "gdb.setSourceBreakpoint", []any{sessionID, map[string]any{"sourceName": "A.sol", "line": 2, "column": 1}})
	paused := rpcCall(t, server.URL, "gdb.continue", []any{sessionID}).(map[string]any)
	current = paused["current"].(map[string]any)
	if current["reason"] != "source_breakpoint" {
		t.Fatalf("pause reason = %v, want source_breakpoint", current["reason"])
	}
	step := current["step"].(map[string]any)
	if step["pc"].(float64) != 2 {
		t.Fatalf("breakpoint pc = %v, want 2", step["pc"])
	}

	_ = rpcCall(t, server.URL, "gdb.writeStorage", []any{sessionID, map[string]any{"slot": "0x0", "value": "0x2a"}})
	completed := rpcCall(t, server.URL, "gdb.continue", []any{sessionID}).(map[string]any)
	if done, ok := completed["done"].(bool); !ok || !done {
		t.Fatalf("done = %#v, want true", completed["done"])
	}
	result := completed["result"].(map[string]any)
	if result["returnData"] != "0x000000000000000000000000000000000000000000000000000000000000002a" {
		t.Fatalf("returnData = %v", result["returnData"])
	}
}

func TestDBGServerRestrictsNonGDBMethods(t *testing.T) {
	provider := &stubProvider{block: upstream.Block{Number: big.NewInt(5), GasLimit: 1_000_000, BaseFee: big.NewInt(1), ChainID: big.NewInt(1)}}
	engineRef, err := forkengine.New(forkengine.Config{Provider: provider, Fork: engine.ForkCancun, Block: upstream.BlockNumber(5)})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	server := httptest.NewServer(NewGDBServer(engineRef))
	defer server.Close()

	capabilities := rpcCall(t, server.URL, "dbgserver.capabilities", nil).(map[string]any)
	if capabilities["mode"] != string(serverModeGDBOnly) {
		t.Fatalf("mode = %v, want %q", capabilities["mode"], serverModeGDBOnly)
	}
	errResp := rpcCallExpectError(t, server.URL, "eth_call", []any{map[string]any{"from": "0x0000000000000000000000000000000000000001", "to": "0x0000000000000000000000000000000000000044", "input": "0x"}})
	if errResp == nil {
		t.Fatal("expected eth_call to be rejected on dbgserver")
	}
	if errResp["message"] != "method eth_call not available on dbgserver" {
		t.Fatalf("unexpected error response = %#v", errResp)
	}
}

func TestDBGServerCallSessionSupportsFunctionStorageAndMemoryBreakpoints(t *testing.T) {
	contract := engine.Address{19: 0x66}
	slotZero := engine.Hash{}
	value := engine.Hash{31: 0x2a}
	provider := &stubProvider{
		block:         upstream.Block{Number: big.NewInt(5), GasLimit: 1_000_000, BaseFee: big.NewInt(1), ChainID: big.NewInt(1)},
		codeByAddress: map[engine.Address][]byte{contract: {0x60, 0x00, 0x54, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3}},
		storageByAddr: map[engine.Address]map[engine.Hash]engine.Hash{contract: {slotZero: value}},
	}
	engineRef, err := forkengine.New(forkengine.Config{Provider: provider, Fork: engine.ForkCancun, Block: upstream.BlockNumber(5)})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	server := httptest.NewServer(NewGDBServer(engineRef))
	defer server.Close()

	session := rpcCall(t, server.URL, "gdb.startCallSession", []any{map[string]any{"from": "0x0000000000000000000000000000000000000001", "to": addressHex(contract), "input": "0xc2985578"}, "0x5"}).(map[string]any)
	sessionID := session["id"].(string)
	if session["kind"] != "call" {
		t.Fatalf("kind = %v, want call", session["kind"])
	}
	_ = rpcCall(t, server.URL, "gdb.setFunctionBreakpoint", []any{sessionID, map[string]any{"signature": "foo()"}})
	_ = rpcCall(t, server.URL, "gdb.setStorageBreakpoint", []any{sessionID, map[string]any{"slot": "0x0", "access": "read", "address": addressHex(contract)}})
	_ = rpcCall(t, server.URL, "gdb.setMemoryBreakpoint", []any{sessionID, map[string]any{"offset": 0, "size": 32, "access": "write", "address": addressHex(contract)}})

	paused := rpcCall(t, server.URL, "gdb.continue", []any{sessionID}).(map[string]any)
	current := paused["current"].(map[string]any)
	if current["reason"] != "function_breakpoint" {
		t.Fatalf("function pause reason = %v, want function_breakpoint", current["reason"])
	}

	paused = rpcCall(t, server.URL, "gdb.continue", []any{sessionID}).(map[string]any)
	current = paused["current"].(map[string]any)
	if current["reason"] != "storage_read_breakpoint" {
		t.Fatalf("storage pause reason = %v, want storage_read_breakpoint", current["reason"])
	}
	storageAccess := current["storageAccess"].(map[string]any)
	if storageAccess["slot"] != "0x0000000000000000000000000000000000000000000000000000000000000000" {
		t.Fatalf("storage slot = %v", storageAccess["slot"])
	}

	paused = rpcCall(t, server.URL, "gdb.continue", []any{sessionID}).(map[string]any)
	current = paused["current"].(map[string]any)
	if current["reason"] != "memory_write_breakpoint" {
		t.Fatalf("memory pause reason = %v, want memory_write_breakpoint", current["reason"])
	}
	memoryAccess := current["memoryAccess"].(map[string]any)
	if memoryAccess["offset"].(float64) != 0 || memoryAccess["size"].(float64) != 32 {
		t.Fatalf("memory access = %#v", memoryAccess)
	}

	completed := rpcCall(t, server.URL, "gdb.continue", []any{sessionID}).(map[string]any)
	if done, ok := completed["done"].(bool); !ok || !done {
		t.Fatalf("done = %#v, want true", completed["done"])
	}
	result := completed["result"].(map[string]any)
	if result["returnData"] != "0x000000000000000000000000000000000000000000000000000000000000002a" {
		t.Fatalf("returnData = %v", result["returnData"])
	}
}

func TestDBGServerCallSessionSupportsCallBreakpoint(t *testing.T) {
	caller := engine.Address{19: 0x77}
	callee := engine.Address{19: 0xab}
	callerCode := []byte{0x60, 0x00, 0x60, 0x00, 0x60, 0x00, 0x60, 0x00, 0x60, 0x00, 0x73}
	callerCode = append(callerCode, callee[:]...)
	callerCode = append(callerCode, 0x61, 0xff, 0xff, 0xf1, 0x00)
	provider := &stubProvider{
		block: upstream.Block{Number: big.NewInt(5), GasLimit: 1_000_000, BaseFee: big.NewInt(1), ChainID: big.NewInt(1)},
		codeByAddress: map[engine.Address][]byte{
			caller: callerCode,
			callee: {0x60, 0x00, 0x60, 0x00, 0xf3},
		},
	}
	engineRef, err := forkengine.New(forkengine.Config{Provider: provider, Fork: engine.ForkCancun, Block: upstream.BlockNumber(5)})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	server := httptest.NewServer(NewGDBServer(engineRef))
	defer server.Close()

	session := rpcCall(t, server.URL, "gdb.startCallSession", []any{map[string]any{"from": "0x0000000000000000000000000000000000000001", "to": addressHex(caller), "input": "0x"}, "0x5"}).(map[string]any)
	sessionID := session["id"].(string)
	_ = rpcCall(t, server.URL, "gdb.setCallBreakpoint", []any{sessionID, map[string]any{"address": addressHex(callee)}})

	paused := rpcCall(t, server.URL, "gdb.continue", []any{sessionID}).(map[string]any)
	current := paused["current"].(map[string]any)
	if current["reason"] != "call_breakpoint" {
		t.Fatalf("call pause reason = %v, want call_breakpoint", current["reason"])
	}
	callAccess := current["callAccess"].(map[string]any)
	if callAccess["callee"] != addressHex(callee) {
		t.Fatalf("call access = %#v", callAccess)
	}
}

func TestDBGServerListAndDeleteBreakpoints(t *testing.T) {
	contract := engine.Address{19: 0x88}
	provider := &stubProvider{
		block:         upstream.Block{Number: big.NewInt(5), GasLimit: 1_000_000, BaseFee: big.NewInt(1), ChainID: big.NewInt(1)},
		codeByAddress: map[engine.Address][]byte{contract: {0x00}},
	}
	engineRef, err := forkengine.New(forkengine.Config{Provider: provider, Fork: engine.ForkCancun, Block: upstream.BlockNumber(5)})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	server := httptest.NewServer(NewGDBServer(engineRef))
	defer server.Close()

	session := rpcCall(t, server.URL, "gdb.startCallSession", []any{map[string]any{"from": "0x0000000000000000000000000000000000000001", "to": addressHex(contract), "input": "0xc2985578"}, "0x5"}).(map[string]any)
	sessionID := session["id"].(string)
	_ = rpcCall(t, server.URL, "gdb.setFunctionBreakpoint", []any{sessionID, map[string]any{"id": "bp-func", "signature": "foo()"}})
	_ = rpcCall(t, server.URL, "gdb.setMemoryBreakpoint", []any{sessionID, map[string]any{"id": "bp-mem", "offset": 0, "size": 32, "access": "write"}})

	listed := rpcCall(t, server.URL, "gdb.listBreakpoints", []any{sessionID}).([]any)
	if len(listed) != 2 {
		t.Fatalf("list length = %d, want 2", len(listed))
	}

	updated := rpcCall(t, server.URL, "gdb.deleteBreakpoint", []any{sessionID, map[string]any{"id": "bp-func"}}).(map[string]any)
	breakpoints := updated["breakpoints"].([]any)
	if len(breakpoints) != 1 {
		t.Fatalf("remaining breakpoints = %d, want 1", len(breakpoints))
	}
	remaining := breakpoints[0].(map[string]any)
	if remaining["id"] != "bp-mem" {
		t.Fatalf("remaining breakpoint = %#v", remaining)
	}

	errResp := rpcCallExpectError(t, server.URL, "gdb.deleteBreakpoint", []any{sessionID, map[string]any{"id": "missing"}})
	if errResp == nil || errResp["message"] != "unknown breakpoint \"missing\"" {
		t.Fatalf("unexpected delete error = %#v", errResp)
	}
}

func rpcCall(t *testing.T, url string, method string, params []any) any {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("http.Post() error = %v", err)
	}
	defer resp.Body.Close()
	var decoded struct {
		Result any            `json:"result"`
		Error  map[string]any `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if decoded.Error != nil {
		t.Fatalf("rpc error = %#v", decoded.Error)
	}
	return decoded.Result
}

func rpcCallExpectError(t *testing.T, url string, method string, params []any) map[string]any {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("http.Post() error = %v", err)
	}
	defer resp.Body.Close()
	var decoded struct {
		Result any            `json:"result"`
		Error  map[string]any `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	return decoded.Error
}
