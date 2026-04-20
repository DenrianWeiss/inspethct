package forkengine

import (
	"context"
	"math/big"
	"testing"

	"inspethct/internal/engine"
	"inspethct/internal/forkengine/upstream"
)

type traceStubProvider struct {
	*stubProvider
	traces map[engine.Hash]upstream.TraceTransactionResult
}

func (provider *traceStubProvider) DebugTraceTransaction(ctx context.Context, txHash engine.Hash, cfg upstream.TraceTransactionConfig) (upstream.TraceTransactionResult, error) {
	if trace, ok := provider.traces[txHash]; ok {
		return trace, nil
	}
	return nil, nil
}

func TestCompareReplayTraceIgnoresTopLevelDepthOffset(t *testing.T) {
	comparison := CompareReplayTrace(
		[]ReplayTraceStep{{PC: 0, Op: "PUSH1", Depth: 0}, {PC: 2, Op: "STOP", Depth: 0}},
		&engine.ExecutionResult{Status: engine.StatusSuccess},
		upstream.StructuredTrace{StructLogs: []upstream.StructuredTraceStep{{PC: 0, Op: "PUSH1", Depth: 1}, {PC: 2, Op: "STOP", Depth: 1}}},
	)
	if !comparison.Match {
		t.Fatalf("comparison.Match = false, mismatch = %#v", comparison.FirstMismatch)
	}
}

func TestReplayTransactionWithTraceComparisonMatchesStructuredTrace(t *testing.T) {
	txHash := engine.Hash{0xaa}
	contract := engine.Address{0x10}
	slot := engine.Hash{}
	provider := &traceStubProvider{
		stubProvider: &stubProvider{
			block: upstream.Block{Number: big.NewInt(3), GasLimit: 1_000_000, ChainID: big.NewInt(1)},
			codeByAddress: map[engine.Address][]byte{
				contract: {0x60, 0x00, 0x54, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3},
			},
			storageByAddress: map[engine.Address]map[engine.Hash]engine.Hash{
				contract: {slot: {31: 0x2a}},
			},
			transactions: map[engine.Hash]upstream.Transaction{
				txHash: {
					Hash:        txHash,
					BlockNumber: big.NewInt(3),
					From:        engine.Address{0x01},
					To:          &contract,
					Gas:         100000,
					GasPrice:    big.NewInt(1),
					Value:       big.NewInt(0),
				},
			},
			receipts: map[engine.Hash]upstream.Receipt{
				txHash: {TransactionHash: txHash, BlockNumber: big.NewInt(3), GasUsed: 23456},
			},
		},
		traces: map[engine.Hash]upstream.TraceTransactionResult{
			txHash: {
				"gas":         float64(23456),
				"failed":      false,
				"returnValue": "0x000000000000000000000000000000000000000000000000000000000000002a",
				"structLogs": []any{
					map[string]any{"pc": float64(0), "op": "PUSH1", "depth": float64(1)},
					map[string]any{"pc": float64(2), "op": "SLOAD", "depth": float64(1)},
					map[string]any{"pc": float64(3), "op": "PUSH1", "depth": float64(1)},
					map[string]any{"pc": float64(5), "op": "MSTORE", "depth": float64(1)},
					map[string]any{"pc": float64(6), "op": "PUSH1", "depth": float64(1)},
					map[string]any{"pc": float64(8), "op": "PUSH1", "depth": float64(1)},
					map[string]any{"pc": float64(10), "op": "RETURN", "depth": float64(1)},
				},
			},
		},
	}

	engineRef, err := New(Config{Provider: provider, Fork: engine.ForkLondon, Block: upstream.BlockNumber(3)})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	comparison, err := engineRef.ReplayTransactionWithTraceComparison(context.Background(), txHash, upstream.TraceTransactionConfig{DisableMemory: true, DisableStorage: true, DisableStack: true})
	if err != nil {
		t.Fatalf("ReplayTransactionWithTraceComparison() error = %v", err)
	}
	if comparison.LocalResult == nil {
		t.Fatalf("local result is nil")
	}
	if comparison.LocalResult.Status != engine.StatusSuccess {
		t.Fatalf("local status = %v, want success", comparison.LocalResult.Status)
	}
	if len(comparison.LocalTrace) != 7 {
		t.Fatalf("local trace length = %d, want 7", len(comparison.LocalTrace))
	}
	if !comparison.Comparison.Match {
		t.Fatalf("comparison mismatch = %#v", comparison.Comparison.FirstMismatch)
	}
	if comparison.Prepared.BlockRef.CacheKey() != upstream.BlockNumber(3).CacheKey() {
		t.Fatalf("prepared block ref = %s, want %s", comparison.Prepared.BlockRef.CacheKey(), upstream.BlockNumber(3).CacheKey())
	}
	if len(comparison.LocalResult.ReturnData) != 32 || comparison.LocalResult.ReturnData[31] != 0x2a {
		t.Fatalf("local return data = %x, want last byte 0x2a", comparison.LocalResult.ReturnData)
	}
}

func TestCompareReplayTraceReportsFirstMismatch(t *testing.T) {
	comparison := CompareReplayTrace(
		[]ReplayTraceStep{{PC: 0, Op: "PUSH1", Depth: 0}},
		&engine.ExecutionResult{Status: engine.StatusSuccess},
		upstream.StructuredTrace{StructLogs: []upstream.StructuredTraceStep{{PC: 1, Op: "PUSH1", Depth: 1}}},
	)
	if comparison.Match {
		t.Fatalf("comparison.Match = true, want false")
	}
	if comparison.FirstMismatch == nil || comparison.FirstMismatch.Field != "pc" {
		t.Fatalf("first mismatch = %#v, want pc mismatch", comparison.FirstMismatch)
	}
	if comparison.FirstMismatch.Index != 0 {
		t.Fatalf("mismatch index = %d, want 0", comparison.FirstMismatch.Index)
	}
}

func TestCompareReplayTraceChecksGasCostAndError(t *testing.T) {
	gasRemaining := uint64(50000)
	gasCost := uint64(2100)
	comparison := CompareReplayTrace(
		[]ReplayTraceStep{{PC: 0, Op: "SLOAD", Depth: 0, GasRemaining: 50000, GasCost: 2100, Error: ""}},
		&engine.ExecutionResult{Status: engine.StatusSuccess},
		upstream.StructuredTrace{StructLogs: []upstream.StructuredTraceStep{{PC: 0, Op: "SLOAD", Depth: 1, Gas: &gasRemaining, GasCost: &gasCost, Error: ""}}},
	)
	if !comparison.Match {
		t.Fatalf("comparison.Match = false, mismatch = %#v", comparison.FirstMismatch)
	}

	mismatch := CompareReplayTrace(
		[]ReplayTraceStep{{PC: 0, Op: "SLOAD", Depth: 0, GasRemaining: 49999, GasCost: 2100, Error: "write protection"}},
		&engine.ExecutionResult{Status: engine.StatusHalt, Err: engine.ErrWriteProtection},
		upstream.StructuredTrace{Failed: true, StructLogs: []upstream.StructuredTraceStep{{PC: 0, Op: "SLOAD", Depth: 1, Gas: &gasRemaining, GasCost: &gasCost, Error: "write protection"}}},
	)
	if mismatch.Match {
		t.Fatalf("mismatch.Match = true, want false")
	}
	if mismatch.FirstMismatch == nil || mismatch.FirstMismatch.Field != "gas" {
		t.Fatalf("first mismatch = %#v, want gas mismatch", mismatch.FirstMismatch)
	}
}

func TestCompareReplayToReceiptMatchesStatusGasAndLogs(t *testing.T) {
	tx := upstream.Transaction{Input: []byte{0xaa, 0x00}}
	status := uint64(1)
	result := &engine.ExecutionResult{
		Status:  engine.StatusSuccess,
		GasUsed: 1234,
		Logs: []engine.Log{{
			Address: engine.Address{0x01},
			Topics:  []engine.Hash{{0x02}},
			Data:    []byte{0x03},
		}},
	}
	receipt := upstream.Receipt{
		Status:  &status,
		GasUsed: 21000 + 16 + 4 + 1234,
		Logs: []engine.Log{{
			Address: engine.Address{0x01},
			Topics:  []engine.Hash{{0x02}},
			Data:    []byte{0x03},
		}},
	}
	comparison := CompareReplayToReceipt(tx, receipt, result)
	if !comparison.Match {
		t.Fatalf("comparison.Match = false, mismatch = %#v", comparison.FirstMismatch)
	}
}

func TestReplayGasUsedAppliesRefundCap(t *testing.T) {
	input := make([]byte, 229)
	for index := 0; index < 227; index++ {
		input[index] = 0xaa
	}
	tx := upstream.Transaction{Input: input}
	result := &engine.ExecutionResult{GasUsed: 366579, GasRefund: 79600}

	gasUsed := replayGasUsed(tx, result)
	if gasUsed != 312976 {
		t.Fatalf("gasUsed = %d, want 312976", gasUsed)
	}
}
