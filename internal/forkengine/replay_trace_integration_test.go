package forkengine

import (
	"context"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"inspethct/internal/engine"
	"inspethct/internal/forkengine/upstream"
)

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
