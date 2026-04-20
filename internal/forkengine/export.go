package forkengine

import (
	"context"
	"fmt"
	"math/big"

	"inspethct/internal/engine"
	"inspethct/internal/forkengine/upstream"
)

type ExportBundle struct {
	Snapshot     ExportedSnapshot    `json:"snapshot"`
	ReplayReport *ReplayExportReport `json:"replayReport,omitempty"`
}

type ReplayExportReport struct {
	TargetTransactionHash    string              `json:"targetTransactionHash"`
	ExecutionBlockRef        string              `json:"executionBlockRef"`
	StateSourceBlockRef      string              `json:"stateSourceBlockRef"`
	Exact                    bool                `json:"exact"`
	Limitation               string              `json:"limitation,omitempty"`
	AppliedPriorTransactions []string            `json:"appliedPriorTransactions"`
	PriorTransactions        []ReplayCheckReport `json:"priorTransactions"`
	FirstMismatch            *ReplayCheckReport  `json:"firstMismatch,omitempty"`
	BlobSuspicion            bool                `json:"blobSuspicion"`
}

type ReplayCheckReport struct {
	Hash             string         `json:"hash"`
	TransactionIndex uint64         `json:"transactionIndex"`
	Type             uint64         `json:"type"`
	Blob             bool           `json:"blob"`
	BlobHashCount    int            `json:"blobHashCount"`
	BlobGasFeeCap    string         `json:"blobGasFeeCap,omitempty"`
	Match            bool           `json:"match"`
	Mismatch         *TraceMismatch `json:"mismatch,omitempty"`
}

type ReplayLocalTrace struct {
	Prepared                 *PreparedCall
	Transaction              upstream.Transaction
	Receipt                  upstream.Receipt
	Result                   *engine.ExecutionResult
	LocalTrace               []ReplayTraceStep
	Exact                    bool
	Limitation               string
	AppliedPriorTransactions []engine.Hash
}

func (engineRef *Engine) ExportBundle(ctx context.Context, txHash *engine.Hash) (*ExportBundle, error) {
	bundle := &ExportBundle{Snapshot: engineRef.ExportSnapshot()}
	if txHash == nil {
		return bundle, nil
	}
	report, err := engineRef.ExportReplayReport(ctx, *txHash)
	if err != nil {
		return nil, err
	}
	bundle.ReplayReport = report
	return bundle, nil
}

func (engineRef *Engine) ExportReplayReport(ctx context.Context, txHash engine.Hash) (*ReplayExportReport, error) {
	txProvider, ok := engineRef.provider.(upstream.TransactionProvider)
	if !ok {
		return nil, fmt.Errorf("forkengine: provider does not implement transaction replay capabilities")
	}
	tx, err := txProvider.GetTransactionByHash(ctx, txHash)
	if err != nil {
		return nil, err
	}
	executionBlockRef := engineRef.block
	if tx.BlockNumber != nil && tx.BlockNumber.IsUint64() {
		executionBlockRef = upstream.BlockNumber(tx.BlockNumber.Uint64())
	}
	report := &ReplayExportReport{
		TargetTransactionHash: hashToHex(tx.Hash),
		ExecutionBlockRef:     executionBlockRef.CacheKey(),
		StateSourceBlockRef:   replayStateBlockRef(tx).CacheKey(),
		Exact:                 true,
	}
	if tx.TransactionIndex == nil || *tx.TransactionIndex == 0 {
		return report, nil
	}
	blockProvider, ok := engineRef.provider.(upstream.BlockTransactionsProvider)
	if !ok {
		report.Exact = false
		report.Limitation = "provider does not expose block transaction lists for prior-transaction reconstruction"
		return report, nil
	}
	transactions, err := blockProvider.GetBlockTransactions(ctx, executionBlockRef)
	if err != nil {
		report.Exact = false
		report.Limitation = fmt.Sprintf("failed to load block transactions: %v", err)
		return report, nil
	}
	stateView := preparedStateView{CacheBlockRef: syntheticReplayStateBlockRef(tx), SourceBlockRef: replayStateBlockRef(tx)}
	for _, priorTx := range transactions {
		if priorTx.Hash == tx.Hash {
			break
		}
		receipt, err := txProvider.GetTransactionReceipt(ctx, priorTx.Hash)
		if err != nil {
			report.Exact = false
			report.Limitation = fmt.Sprintf("failed to load receipt for prior transaction %x: %v", priorTx.Hash, err)
			break
		}
		prepared, err := engineRef.prepareReplayTransactionWithState(ctx, priorTx, receipt, executionBlockRef, stateView, nil)
		if err != nil {
			report.Exact = false
			report.Limitation = fmt.Sprintf("failed to prepare prior transaction %x: %v", priorTx.Hash, err)
			break
		}
		result, execErr := engineRef.ExecutePreparedCall(prepared)
		if execErr != nil && result == nil {
			report.Exact = false
			report.Limitation = fmt.Sprintf("failed to execute prior transaction %x: %v", priorTx.Hash, execErr)
			break
		}
		if commitErr := engineRef.CommitLocalWrites(prepared, result); commitErr != nil {
			report.Exact = false
			report.Limitation = fmt.Sprintf("failed to commit prior transaction %x: %v", priorTx.Hash, commitErr)
			break
		}
		comparison := CompareReplayToReceipt(priorTx, receipt, result)
		check := replayCheckReport(priorTx, comparison)
		report.PriorTransactions = append(report.PriorTransactions, check)
		if comparison.Match {
			report.AppliedPriorTransactions = append(report.AppliedPriorTransactions, check.Hash)
			continue
		}
		report.Exact = false
		report.FirstMismatch = &check
		report.BlobSuspicion = check.Blob
		report.Limitation = fmt.Sprintf("prior transaction %s replay mismatch on %s", check.Hash, comparison.FirstMismatch.Field)
		break
	}
	if report.FirstMismatch == nil {
		for _, check := range report.PriorTransactions {
			if check.Blob {
				report.BlobSuspicion = true
				break
			}
		}
	}
	return report, nil
}

func (engineRef *Engine) ReplayTransactionWithLocalTrace(ctx context.Context, txHash engine.Hash) (*ReplayLocalTrace, error) {
	prepared, tx, receipt, err := engineRef.PrepareReplay(ctx, txHash)
	if err != nil {
		return nil, err
	}
	collector := newReplayTraceCollector()
	prepared.Config.Hooks = mergeHookRegistries(prepared.Config.Hooks, collector.registry())
	result, err := engineRef.ExecutePreparedCall(prepared)
	if err != nil && result == nil {
		return nil, err
	}
	if commitErr := engineRef.CommitLocalWrites(prepared, result); commitErr != nil && err == nil {
		err = commitErr
	}
	if err != nil && result == nil {
		return nil, err
	}
	collector.applyResult(result)
	replayState := cloneReplayState(prepared.ReplayState)
	exact := true
	limitation := ""
	applied := []engine.Hash(nil)
	if replayState != nil {
		exact = replayState.Exact
		limitation = replayState.Limitation
		applied = append([]engine.Hash(nil), replayState.AppliedPriorTransactions...)
	}
	return &ReplayLocalTrace{Prepared: prepared, Transaction: tx, Receipt: receipt, Result: result, LocalTrace: append([]ReplayTraceStep(nil), collector.steps...), Exact: exact, Limitation: limitation, AppliedPriorTransactions: applied}, nil
}

func replayCheckReport(tx upstream.Transaction, comparison ReceiptComparison) ReplayCheckReport {
	report := ReplayCheckReport{
		Hash:             hashToHex(tx.Hash),
		TransactionIndex: derefUint64(tx.TransactionIndex),
		Type:             tx.Type,
		Blob:             transactionIsBlob(tx),
		BlobHashCount:    len(tx.BlobHashes),
		BlobGasFeeCap:    bigToString(tx.BlobGasFeeCap),
		Match:            comparison.Match,
		Mismatch:         comparison.FirstMismatch,
	}
	return report
}

func transactionIsBlob(tx upstream.Transaction) bool {
	return tx.Type == 3 || len(tx.BlobHashes) > 0 || (tx.BlobGasFeeCap != nil && tx.BlobGasFeeCap.Sign() > 0)
}

func hashToHex(hash engine.Hash) string {
	return upstream.EncodeHex(hash[:])
}

func bigToString(value *big.Int) string {
	if value == nil {
		return ""
	}
	return value.String()
}

func derefUint64(value *uint64) uint64 {
	if value == nil {
		return 0
	}
	return *value
}
