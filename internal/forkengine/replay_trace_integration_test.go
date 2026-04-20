package forkengine

import (
	"context"
	"bytes"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"inspethct/internal/engine"
	"inspethct/internal/forkengine/upstream"
)

type recordingPrecompile struct {
	inner   engine.Precompile
	inputs  [][]byte
	outputs [][]byte
	errs    []error
}

func (p *recordingPrecompile) RequiredGas(input []byte, fork engine.Fork) (uint64, error) {
	return p.inner.RequiredGas(input, fork)
}

func (p *recordingPrecompile) Run(input []byte, fork engine.Fork) ([]byte, error) {
	clonedInput := append([]byte(nil), input...)
	p.inputs = append(p.inputs, clonedInput)
	output, err := p.inner.Run(input, fork)
	p.outputs = append(p.outputs, append([]byte(nil), output...))
	p.errs = append(p.errs, err)
	return output, err
}

func TestReplayTransactionAgainstRPCReceipts(t *testing.T) {
	rpcURL, hashes := integrationReplayTargets()
	if rpcURL == "" || len(hashes) == 0 {
		t.Skip("set FORKENGINE_RPC_URL or RPC_URL plus replay tx hash env vars to enable receipt replay integration test")
	}
	provider, err := upstream.NewJSONRPCProvider(upstream.JSONRPCConfig{Endpoint: rpcURL})
	if err != nil {
		t.Fatalf("NewJSONRPCProvider() error = %v", err)
	}
	engineRef, err := New(Config{Mode: ModeDiff, Provider: provider, Fork: engine.ForkCancun, Block: upstream.LatestBlock()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	exactCount := 0
	for _, txHash := range hashes {
		replay, err := engineRef.ReplayTransaction(context.Background(), txHash)
		if err != nil {
			t.Fatalf("ReplayTransaction(%x) error = %v", txHash, err)
		}
		if !replay.Exact {
			t.Logf("skip strict receipt comparison for %x: %s", txHash, replay.Limitation)
			continue
		}
		exactCount++
		comparison := CompareReplayToReceipt(replay.Transaction, replay.Receipt, replay.Result)
		if !comparison.Match {
			t.Fatalf("receipt mismatch for %x: %#v", txHash, comparison.FirstMismatch)
		}
	}
	if exactCount == 0 {
		t.Skip("all provided transactions require intra-block prestate reconstruction, so strict receipt comparison is approximate")
	}
}

func TestReplayTransactionWithTraceComparisonAgainstRPC(t *testing.T) {
	rpcURL, hashes := integrationReplayTargets()
	if rpcURL == "" || len(hashes) == 0 {
		t.Skip("set FORKENGINE_RPC_URL and FORKENGINE_REPLAY_TX_HASH to enable replay integration test")
	}

	provider, err := upstream.NewJSONRPCProvider(upstream.JSONRPCConfig{Endpoint: rpcURL})
	if err != nil {
		t.Fatalf("NewJSONRPCProvider() error = %v", err)
	}

	engineRef, err := New(Config{Mode: ModeDiff, Provider: provider, Fork: engine.ForkCancun, Block: upstream.LatestBlock()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	for _, txHash := range hashes {
		comparison, err := engineRef.ReplayTransactionWithTraceComparison(context.Background(), txHash, upstream.TraceTransactionConfig{DisableMemory: true, DisableStorage: true, DisableStack: true})
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "does not exist") || strings.Contains(strings.ToLower(err.Error()), "not available") {
				t.Skip("upstream RPC does not expose trace methods")
			}
			t.Fatalf("ReplayTransactionWithTraceComparison(%x) error = %v", txHash, err)
		}
		if !comparison.Comparison.Match {
			t.Fatalf("trace mismatch for %x: %#v", txHash, comparison.Comparison.FirstMismatch)
		}
	}
}

func TestReplayTransactionCapturesMainnetECRecoverAgainstRPC(t *testing.T) {
	rpcURL := strings.TrimSpace(os.Getenv("RPC_URL"))
	if rpcURL == "" {
		rpcURL = strings.TrimSpace(os.Getenv("FORKENGINE_RPC_URL"))
	}
	if rpcURL == "" {
		t.Skip("set RPC_URL or FORKENGINE_RPC_URL to enable ecrecover replay integration test")
	}
	txHash, err := parseIntegrationHash("0x31526d91c2dcf740fca47a1f6be59ea88dd5673df4eba3c2e37f1dd19e9167cc")
	if err != nil {
		t.Fatalf("parseIntegrationHash() error = %v", err)
	}
	provider, err := upstream.NewJSONRPCProvider(upstream.JSONRPCConfig{Endpoint: rpcURL})
	if err != nil {
		t.Fatalf("NewJSONRPCProvider() error = %v", err)
	}
	engineRef, err := New(Config{Mode: ModeDiff, Provider: provider, Fork: engine.ForkCancun, Block: upstream.LatestBlock()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	prepared, _, _, err := engineRef.PrepareReplay(context.Background(), txHash)
	if err != nil {
		t.Fatalf("PrepareReplay() error = %v", err)
	}
	var ecrecoverAddr engine.Address
	ecrecoverAddr[19] = 0x01
	registry := engine.MainnetPrecompilesForFork(prepared.Config.Fork).Clone()
	inner, ok := registry.Resolve(ecrecoverAddr)
	if !ok {
		t.Fatalf("ecrecover precompile missing for fork %s", prepared.Config.Fork)
	}
	recorder := &recordingPrecompile{inner: inner}
	registry.Register(ecrecoverAddr, recorder)
	prepared.Config.Precompiles = registry
	_, _ = engineRef.ExecutePreparedCall(prepared)
	if len(recorder.inputs) == 0 {
		t.Fatalf("expected at least one ecrecover call")
	}
	wantInput, err := hex.DecodeString(
		"ee8f543390b5647e7774898d06db49a45323d918bb96a24df6943ae3cb6a0807" +
			"000000000000000000000000000000000000000000000000000000000000001c" +
			"977cd7acc7e4837f5afe54f395b276fad8c4144a738d4d817c0a88d9d4b91144" +
			"5dcde20164af184639b85fb80d9f8cc6e65fb80640328fc69236982b7dc07f25",
	)
	if err != nil {
		t.Fatalf("hex.DecodeString() want input error = %v", err)
	}
	wantOutput, err := hex.DecodeString("00000000000000000000000057ed531a9da5b77c504cfd9b1c76b49e8dd76d05")
	if err != nil {
		t.Fatalf("hex.DecodeString() want output error = %v", err)
	}
	if !bytes.Equal(recorder.inputs[0], wantInput) {
		t.Fatalf("first ecrecover input = %x, want %x", recorder.inputs[0], wantInput)
	}
	if recorder.errs[0] != nil {
		t.Fatalf("first ecrecover error = %v", recorder.errs[0])
	}
	if !bytes.Equal(recorder.outputs[0], wantOutput) {
		t.Fatalf("first ecrecover output = %x, want %x", recorder.outputs[0], wantOutput)
	}
}

func integrationReplayTargets() (string, []engine.Hash) {
	rpcURL := strings.TrimSpace(os.Getenv("FORKENGINE_RPC_URL"))
	if rpcURL == "" {
		rpcURL = strings.TrimSpace(os.Getenv("RPC_URL"))
	}
	rawHashes := strings.TrimSpace(os.Getenv("FORKENGINE_REPLAY_TX_HASHES"))
	if rawHashes == "" {
		first := strings.TrimSpace(os.Getenv("FORKENGINE_REPLAY_TX_HASH"))
		second := strings.TrimSpace(os.Getenv("FORKENGINE_REPLAY_TX_HASH_2"))
		parts := make([]string, 0, 2)
		if first != "" {
			parts = append(parts, first)
		}
		if second != "" {
			parts = append(parts, second)
		}
		rawHashes = strings.Join(parts, ",")
	}
	if rpcURL == "" || rawHashes == "" {
		return rpcURL, nil
	}
	parts := strings.Split(rawHashes, ",")
	hashes := make([]engine.Hash, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		hash, err := parseIntegrationHash(trimmed)
		if err != nil {
			continue
		}
		hashes = append(hashes, hash)
	}
	return rpcURL, hashes
}

func parseIntegrationHash(input string) (engine.Hash, error) {
	trimmed := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(input), "0x"), "0X")
	decoded, err := hex.DecodeString(trimmed)
	if err != nil {
		return engine.Hash{}, err
	}
	var hash engine.Hash
	copy(hash[:], decoded)
	return hash, nil
}
