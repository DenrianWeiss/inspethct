package forkengine

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"testing"

	"inspethct/internal/engine"
	"inspethct/internal/forkengine/upstream"
)

func TestVerifyReplay(t *testing.T) {
	raw, _ := hex.DecodeString("6b2b23737d33ef90766f92ad75579ecfc9a1730d908545c20062dc51da8a8c59")
	var txHash engine.Hash
	copy(txHash[:], raw)
	// Read rpc endpoint from env
	endpoint := os.Getenv("RPC_ENDPOINT")
	if endpoint == "" {
		t.Log("No upstream set")
		t.Skip()
	}

	provider, err := upstream.NewJSONRPCProvider(upstream.JSONRPCConfig{Endpoint: endpoint})
	if err != nil {
		t.Fatal("provider error:", err)
	}
	eng, err := New(Config{Mode: ModeDiff, Provider: provider, Fork: engine.ForkCancun, Block: upstream.LatestBlock()})
	if err != nil {
		t.Fatal("engine error:", err)
	}
	replay, err := eng.ReplayTransaction(context.Background(), txHash)
	if err != nil {
		t.Fatal("replay error:", err)
	}
	fmt.Printf("Replay exact=%v limitation=%q\n", replay.Exact, replay.Limitation)
	fmt.Printf("Applied prior txs: %d\n", len(replay.AppliedPriorTransactions))
	fmt.Printf("Result status=%v evm_gasUsed=%d\n", replay.Result.Status, replay.Result.GasUsed)
	fmt.Printf("AccessList entries in tx: %d addrs, storage keys total: %d\n",
		len(replay.Transaction.AccessList),
		func() int {
			n := 0
			for _, e := range replay.Transaction.AccessList {
				n += len(e.StorageKeys)
			}
			return n
		}(),
	)
	cmp := CompareReplayToReceipt(replay.Transaction, replay.Receipt, replay.Result)
	fmt.Printf("Receipt gasUsed (chain): %d\n", replay.Receipt.GasUsed)
	fmt.Printf("Receipt comparison match=%v gasUsedMatch=%v\n", cmp.Match, cmp.GasUsedMatch)
	if cmp.FirstMismatch != nil {
		fmt.Printf("First mismatch: field=%s local=%s upstream=%s\n", cmp.FirstMismatch.Field, cmp.FirstMismatch.Local, cmp.FirstMismatch.Upstream)
	}
}
