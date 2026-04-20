package jsonrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"inspethct/internal/engine"
	"inspethct/internal/forkengine"
	"inspethct/internal/forkengine/upstream"
)

type stubProvider struct {
	block         upstream.Block
	codeByAddress map[engine.Address][]byte
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
