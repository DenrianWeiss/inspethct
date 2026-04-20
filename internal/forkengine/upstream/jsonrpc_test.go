package upstream

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"inspethct/internal/engine"
)

func TestJSONRPCClientRetriesBoundedlyOnRateLimit(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&attempts, 1)
		if count < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"rate limited"}`))
			return
		}
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x1"}`))
	}))
	defer server.Close()

	client, err := NewJSONRPCClient(JSONRPCConfig{Endpoint: server.URL, Retry: RetryConfig{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond}})
	if err != nil {
		t.Fatalf("NewJSONRPCClient() error = %v", err)
	}
	var result string
	if err := client.Call(context.Background(), "eth_chainId", nil, &result); err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}
	if result != "0x1" {
		t.Fatalf("result = %q, want 0x1", result)
	}
}

func TestJSONRPCClientProbesBatchAndFallsBackToSerial(t *testing.T) {
	var batchAttempts int32
	var singleAttempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		switch payload.(type) {
		case []any:
			atomic.AddInt32(&batchAttempts, 1)
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("batch disabled"))
		default:
			atomic.AddInt32(&singleAttempts, 1)
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x1"}`))
		}
	}))
	defer server.Close()

	client, err := NewJSONRPCClient(JSONRPCConfig{Endpoint: server.URL, Batch: BatchConfig{Mode: BatchProbe, MaxBatchSize: 10}})
	if err != nil {
		t.Fatalf("NewJSONRPCClient() error = %v", err)
	}
	var left, right string
	err = client.BatchCall(context.Background(), []BatchCall{{Method: "eth_chainId", Result: &left}, {Method: "eth_chainId", Result: &right}})
	if err != nil {
		t.Fatalf("BatchCall() error = %v", err)
	}
	if atomic.LoadInt32(&batchAttempts) != 1 {
		t.Fatalf("batchAttempts = %d, want 1", atomic.LoadInt32(&batchAttempts))
	}
	if atomic.LoadInt32(&singleAttempts) != 2 {
		t.Fatalf("singleAttempts = %d, want 2", atomic.LoadInt32(&singleAttempts))
	}
	if left != "0x1" || right != "0x1" {
		t.Fatalf("results = %q, %q, want 0x1, 0x1", left, right)
	}
}

func TestJSONRPCClientRateLimitPacesRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x1"}`))
	}))
	defer server.Close()

	client, err := NewJSONRPCClient(JSONRPCConfig{Endpoint: server.URL, RateLimit: RateLimitConfig{RequestsPerSecond: 2}})
	if err != nil {
		t.Fatalf("NewJSONRPCClient() error = %v", err)
	}
	var mu sync.Mutex
	slept := make([]time.Duration, 0, 1)
	now := time.Unix(100, 0)
	client.now = func() time.Time { return now }
	client.sleep = func(d time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		slept = append(slept, d)
		now = now.Add(d)
	}
	var result string
	if err := client.Call(context.Background(), "eth_chainId", nil, &result); err != nil {
		t.Fatalf("first Call() error = %v", err)
	}
	if err := client.Call(context.Background(), "eth_chainId", nil, &result); err != nil {
		t.Fatalf("second Call() error = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(slept) != 1 {
		t.Fatalf("sleep count = %d, want 1", len(slept))
	}
	if slept[0] != 500*time.Millisecond {
		t.Fatalf("sleep duration = %s, want 500ms", slept[0])
	}
}

func TestJSONRPCProviderParsesChainAndTraceMethods(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		switch payload.Method {
		case "eth_chainId":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x1"}`))
		case "debug_traceTransaction":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"gas":21000,"failed":false}}`))
		case "eth_getBlockByNumber":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"number":"0x1","hash":"0x0300000000000000000000000000000000000000000000000000000000000000","parentHash":"0x0400000000000000000000000000000000000000000000000000000000000000","timestamp":"0x5","gasLimit":"0x1c9c380","baseFeePerGas":"0x10","blobBaseFeePerGas":"0x20","miner":"0x0000000000000000000000000000000000000003","mixHash":"0x0500000000000000000000000000000000000000000000000000000000000000","transactions":[{"hash":"0x0100000000000000000000000000000000000000000000000000000000000000","blockHash":"0x0200000000000000000000000000000000000000000000000000000000000000","blockNumber":"0x1","from":"0x0000000000000000000000000000000000000001","to":"0x0000000000000000000000000000000000000002","type":"0x3","gas":"0x5208","gasPrice":"0x1","maxFeePerBlobGas":"0x7","blobVersionedHashes":["0x0600000000000000000000000000000000000000000000000000000000000000"],"input":"0x","nonce":"0x0","transactionIndex":"0x0","value":"0x0"}]}}`))
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()

	provider, err := NewJSONRPCProvider(JSONRPCConfig{Endpoint: server.URL})
	if err != nil {
		t.Fatalf("NewJSONRPCProvider() error = %v", err)
	}
	chainID, err := provider.ChainID(context.Background())
	if err != nil {
		t.Fatalf("ChainID() error = %v", err)
	}
	if chainID.Uint64() != 1 {
		t.Fatalf("chainID = %d, want 1", chainID.Uint64())
	}
	trace, err := provider.DebugTraceTransaction(context.Background(), engine.Hash{0x01}, TraceTransactionConfig{})
	if err != nil {
		t.Fatalf("DebugTraceTransaction() error = %v", err)
	}
	if trace["failed"] != false {
		t.Fatalf("trace failed = %v, want false", trace["failed"])
	}
	if int(trace["gas"].(float64)) != 21000 {
		t.Fatalf("trace gas = %v, want 21000", trace["gas"])
	}
	transactions, err := provider.GetBlockTransactions(context.Background(), BlockNumber(1))
	if err != nil {
		t.Fatalf("GetBlockTransactions() error = %v", err)
	}
	if len(transactions) != 1 {
		t.Fatalf("len(transactions) = %d, want 1", len(transactions))
	}
	if transactions[0].Type != 3 {
		t.Fatalf("transactions[0].Type = %d, want 3", transactions[0].Type)
	}
	if transactions[0].BlobGasFeeCap == nil || transactions[0].BlobGasFeeCap.Uint64() != 7 {
		t.Fatalf("transactions[0].BlobGasFeeCap = %v, want 7", transactions[0].BlobGasFeeCap)
	}
	if len(transactions[0].BlobHashes) != 1 {
		t.Fatalf("len(transactions[0].BlobHashes) = %d, want 1", len(transactions[0].BlobHashes))
	}
	block, err := provider.GetBlock(context.Background(), BlockNumber(1))
	if err != nil {
		t.Fatalf("GetBlock() error = %v", err)
	}
	if block.BlobBaseFee == nil || block.BlobBaseFee.Uint64() != 32 {
		t.Fatalf("block.BlobBaseFee = %v, want 32", block.BlobBaseFee)
	}
}
