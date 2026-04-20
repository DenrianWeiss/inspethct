package forkengine

import (
	"context"
	"math/big"
	"testing"

	"inspethct/internal/engine"
	"inspethct/internal/forkengine/cache"
	"inspethct/internal/forkengine/upstream"
)

type stubProvider struct {
	blockCalls        int
	block             upstream.Block
	balanceByAddress  map[engine.Address]*big.Int
	nonceByAddress    map[engine.Address]uint64
	codeByAddress     map[engine.Address][]byte
	storageByAddress  map[engine.Address]map[engine.Hash]engine.Hash
	transactions      map[engine.Hash]upstream.Transaction
	receipts          map[engine.Hash]upstream.Receipt
	blockTransactions map[string][]upstream.Transaction
}

func (provider *stubProvider) ChainID(ctx context.Context) (*big.Int, error) {
	return big.NewInt(1), nil
}

func (provider *stubProvider) GetBalance(ctx context.Context, addr engine.Address, block upstream.BlockRef) (*big.Int, error) {
	if value, ok := provider.balanceByAddress[addr]; ok {
		return new(big.Int).Set(value), nil
	}
	return big.NewInt(0), nil
}

func (provider *stubProvider) GetNonce(ctx context.Context, addr engine.Address, block upstream.BlockRef) (uint64, error) {
	return provider.nonceByAddress[addr], nil
}

func (provider *stubProvider) GetCode(ctx context.Context, addr engine.Address, block upstream.BlockRef) ([]byte, error) {
	return append([]byte(nil), provider.codeByAddress[addr]...), nil
}

func (provider *stubProvider) GetStorageAt(ctx context.Context, addr engine.Address, slot engine.Hash, block upstream.BlockRef) (engine.Hash, error) {
	if provider.storageByAddress[addr] != nil {
		return provider.storageByAddress[addr][slot], nil
	}
	return engine.Hash{}, nil
}

func (provider *stubProvider) GetBlock(ctx context.Context, block upstream.BlockRef) (upstream.Block, error) {
	provider.blockCalls++
	return provider.block, nil
}

func (provider *stubProvider) GetTransactionByHash(ctx context.Context, txHash engine.Hash) (upstream.Transaction, error) {
	return provider.transactions[txHash].Clone(), nil
}

func (provider *stubProvider) GetTransactionReceipt(ctx context.Context, txHash engine.Hash) (upstream.Receipt, error) {
	return provider.receipts[txHash].Clone(), nil
}

func (provider *stubProvider) GetBlockTransactions(ctx context.Context, block upstream.BlockRef) ([]upstream.Transaction, error) {
	entries := provider.blockTransactions[block.CacheKey()]
	transactions := make([]upstream.Transaction, 0, len(entries))
	for _, entry := range entries {
		transactions = append(transactions, entry.Clone())
	}
	return transactions, nil
}

func TestNewPinnedModeRequiresPinnedBlock(t *testing.T) {
	_, err := New(Config{Mode: ModePinned, Provider: &stubProvider{}})
	if err == nil {
		t.Fatalf("New() error = nil, want pinned block validation error")
	}
}

func TestResolveBlockUsesCache(t *testing.T) {
	provider := &stubProvider{block: upstream.Block{Number: big.NewInt(9)}}
	engineRef, err := New(Config{
		Mode:     ModePinned,
		Provider: provider,
		Block:    upstream.BlockNumber(9),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx := context.Background()
	if _, err := engineRef.ResolveBlock(ctx, upstream.BlockRef{}); err != nil {
		t.Fatalf("ResolveBlock() error = %v", err)
	}
	if _, err := engineRef.ResolveBlock(ctx, upstream.BlockRef{}); err != nil {
		t.Fatalf("ResolveBlock() second error = %v", err)
	}
	if provider.blockCalls != 1 {
		t.Fatalf("provider.blockCalls = %d, want 1", provider.blockCalls)
	}
}

func TestPrepareCallBuildsOverlayAndConfig(t *testing.T) {
	provider := &stubProvider{block: upstream.Block{Number: big.NewInt(1)}}
	engineRef, err := New(Config{Provider: provider, Block: upstream.LatestBlock(), Fork: engine.ForkLondon})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	prepared, err := engineRef.PrepareCall(context.Background(), CallRequest{
		From:     engine.Address{0x01},
		To:       engine.Address{0x02},
		Input:    []byte{0xaa},
		Value:    big.NewInt(3),
		GasLimit: 21000,
	})
	if err != nil {
		t.Fatalf("PrepareCall() error = %v", err)
	}
	if prepared.Overlay == nil {
		t.Fatalf("prepared overlay is nil")
	}
	if prepared.Config.ContractAddress != (engine.Address{0x02}) {
		t.Fatalf("prepared contract address = %#v, want %#v", prepared.Config.ContractAddress, engine.Address{0x02})
	}
	if prepared.Config.Value.Cmp(big.NewInt(3)) != 0 {
		t.Fatalf("prepared value = %s, want 3", prepared.Config.Value.String())
	}
	if prepared.Config.BlockContext == nil {
		t.Fatalf("prepared block context is nil")
	}
	if prepared.Config.TxContext == nil {
		t.Fatalf("prepared tx context is nil")
	}
	if prepared.Config.BlockContext.Number().Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("prepared block number = %s, want 1", prepared.Config.BlockContext.Number().String())
	}
	if prepared.BlockRef.Tag != upstream.BlockTagLatest {
		t.Fatalf("prepared block ref = %#v, want latest", prepared.BlockRef)
	}
}

func TestCommitLocalWritesHonorsDiffCachePolicy(t *testing.T) {
	provider := &stubProvider{block: upstream.Block{Number: big.NewInt(1)}}
	store := cache.NewMemoryStore()
	engineRef, err := New(Config{Mode: ModeDiff, Provider: provider, Cache: store, CachePolicy: CachePolicy{CacheLocalWritesInDiff: true}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	prepared, err := engineRef.PrepareCall(context.Background(), CallRequest{From: engine.Address{0x01}, To: engine.Address{0x02}, GasLimit: 21000})
	if err != nil {
		t.Fatalf("PrepareCall() error = %v", err)
	}
	prepared.Overlay.SetBalance(engine.Address{0x02}, big.NewInt(8))
	prepared.Overlay.SetStorage(engine.Address{0x02}, engine.Hash{0x09}, engine.Hash{0xaa})
	if err := engineRef.CommitLocalWrites(prepared, nil); err != nil {
		t.Fatalf("CommitLocalWrites() error = %v", err)
	}
	dirty := store.DirtyState(prepared.BlockRef)
	if _, ok := dirty.Accounts[engine.Address{0x02}]; !ok {
		t.Fatalf("dirty writes not committed in diff mode")
	}
}

func TestCommitLocalWritesSkipsDiffWritebackWhenDisabled(t *testing.T) {
	provider := &stubProvider{block: upstream.Block{Number: big.NewInt(1)}}
	store := cache.NewMemoryStore()
	engineRef, err := New(Config{Mode: ModeDiff, Provider: provider, Cache: store})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	prepared, err := engineRef.PrepareCall(context.Background(), CallRequest{From: engine.Address{0x01}, To: engine.Address{0x02}, GasLimit: 21000})
	if err != nil {
		t.Fatalf("PrepareCall() error = %v", err)
	}
	prepared.Overlay.SetBalance(engine.Address{0x02}, big.NewInt(8))
	if err := engineRef.CommitLocalWrites(prepared, nil); err != nil {
		t.Fatalf("CommitLocalWrites() error = %v", err)
	}
	dirty := store.DirtyState(prepared.BlockRef)
	if len(dirty.Accounts) != 0 {
		t.Fatalf("dirty writes should not be committed when diff cache is disabled: %#v", dirty)
	}
}

func TestExecuteCallUsesForkEnvironmentState(t *testing.T) {
	contract := engine.Address{0x10}
	slot := engine.Hash{}
	provider := &stubProvider{
		block: upstream.Block{Number: big.NewInt(3), GasLimit: 1_000_000, ChainID: big.NewInt(1)},
		codeByAddress: map[engine.Address][]byte{
			contract: {0x60, 0x00, 0x54, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3},
		},
		storageByAddress: map[engine.Address]map[engine.Hash]engine.Hash{
			contract: {slot: engine.Hash{31: 0x2a}},
		},
	}
	engineRef, err := New(Config{Provider: provider, Fork: engine.ForkLondon, Block: upstream.BlockNumber(3)})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	prepared, result, err := engineRef.ExecuteCall(context.Background(), CallRequest{From: engine.Address{0x01}, To: contract, Block: upstream.BlockNumber(3)})
	if err != nil {
		t.Fatalf("ExecuteCall() error = %v", err)
	}
	if prepared == nil || result == nil {
		t.Fatalf("prepared/result should not be nil")
	}
	if result.Status != engine.StatusSuccess {
		t.Fatalf("result status = %v, want success", result.Status)
	}
	if len(result.ReturnData) != 32 || result.ReturnData[31] != 0x2a {
		t.Fatalf("return data = %x, want last byte 0x2a", result.ReturnData)
	}
}

func TestPrepareReplayBuildsCallFromTransaction(t *testing.T) {
	txHash := engine.Hash{0xaa}
	to := engine.Address{0x20}
	provider := &stubProvider{
		block:         upstream.Block{Number: big.NewInt(12), GasLimit: 1_000_000, ChainID: big.NewInt(1)},
		codeByAddress: map[engine.Address][]byte{to: {0x00}},
		transactions: map[engine.Hash]upstream.Transaction{
			txHash: {
				Hash:        txHash,
				BlockNumber: big.NewInt(12),
				From:        engine.Address{0x01},
				To:          &to,
				Gas:         55000,
				GasPrice:    big.NewInt(17),
				Input:       []byte{0xde, 0xad},
				Value:       big.NewInt(9),
			},
		},
		receipts: map[engine.Hash]upstream.Receipt{
			txHash: {TransactionHash: txHash, BlockNumber: big.NewInt(12), GasUsed: 21000},
		},
	}
	engineRef, err := New(Config{Provider: provider, Fork: engine.ForkLondon, Block: upstream.BlockNumber(12)})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	prepared, tx, receipt, err := engineRef.PrepareReplay(context.Background(), txHash)
	if err != nil {
		t.Fatalf("PrepareReplay() error = %v", err)
	}
	if prepared.Config.GasLimit != 55000 {
		t.Fatalf("prepared gas limit = %d, want 55000", prepared.Config.GasLimit)
	}
	if prepared.Config.TxContext.GasPrice().Cmp(big.NewInt(17)) != 0 {
		t.Fatalf("prepared gas price = %s, want 17", prepared.Config.TxContext.GasPrice().String())
	}
	if tx.Hash != txHash || receipt.TransactionHash != txHash {
		t.Fatalf("replay tx/receipt hash mismatch")
	}
}

func TestPrepareReplaySupportsContractCreation(t *testing.T) {
	txHash := engine.Hash{0xbb}
	created := engine.Address{0x44}
	provider := &stubProvider{
		block: upstream.Block{Number: big.NewInt(15), GasLimit: 1_000_000, ChainID: big.NewInt(1)},
		transactions: map[engine.Hash]upstream.Transaction{
			txHash: {
				Hash:        txHash,
				BlockNumber: big.NewInt(15),
				From:        engine.Address{0x01},
				To:          nil,
				Gas:         90000,
				GasPrice:    big.NewInt(7),
				Input:       []byte{0x60, 0x00, 0x60, 0x00, 0xf3},
				Value:       big.NewInt(0),
			},
		},
		receipts: map[engine.Hash]upstream.Receipt{
			txHash: {TransactionHash: txHash, BlockNumber: big.NewInt(15), GasUsed: 53000, ContractAddress: &created},
		},
	}
	engineRef, err := New(Config{Provider: provider, Fork: engine.ForkLondon, Block: upstream.BlockNumber(15)})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	prepared, tx, receipt, err := engineRef.PrepareReplay(context.Background(), txHash)
	if err != nil {
		t.Fatalf("PrepareReplay() error = %v", err)
	}
	if !prepared.Create {
		t.Fatalf("prepared.Create = false, want true")
	}
	if prepared.Target == nil || *prepared.Target != created {
		t.Fatalf("prepared.Target = %#v, want %#v", prepared.Target, created)
	}
	if prepared.Config.ContractAddress != tx.From {
		t.Fatalf("prepared.Config.ContractAddress = %#v, want caller %#v", prepared.Config.ContractAddress, tx.From)
	}
	if receipt.ContractAddress == nil || *receipt.ContractAddress != created {
		t.Fatalf("receipt.ContractAddress = %#v, want %#v", receipt.ContractAddress, created)
	}
	result, err := engineRef.ExecutePreparedCall(prepared)
	if err != nil {
		t.Fatalf("ExecutePreparedCall() error = %v", err)
	}
	if result.Status != engine.StatusSuccess {
		t.Fatalf("result.Status = %v, want success", result.Status)
	}
	if result.CreatedAddress == nil || *result.CreatedAddress != created {
		t.Fatalf("result.CreatedAddress = %#v, want %#v", result.CreatedAddress, created)
	}
	if result.GasUsed == 0 {
		t.Fatalf("result.GasUsed = 0, want non-zero")
	}
}

func TestReplayTransactionReconstructsPriorTransactions(t *testing.T) {
	priorHash := engine.Hash{0x10}
	targetHash := engine.Hash{0x11}
	from := engine.Address{0x01}
	mid := engine.Address{0x02}
	to := engine.Address{0x03}
	blockRef := upstream.BlockNumber(21)
	indexZero := uint64(0)
	indexOne := uint64(1)
	status := uint64(1)
	priorTx := upstream.Transaction{Hash: priorHash, BlockNumber: big.NewInt(21), From: from, To: &mid, Gas: 21000, GasPrice: big.NewInt(1), TransactionIndex: &indexZero, Value: big.NewInt(5)}
	targetTx := upstream.Transaction{Hash: targetHash, BlockNumber: big.NewInt(21), From: mid, To: &to, Gas: 21000, GasPrice: big.NewInt(1), TransactionIndex: &indexOne, Value: big.NewInt(5)}
	provider := &stubProvider{
		block: upstream.Block{Number: big.NewInt(21), GasLimit: 1_000_000, ChainID: big.NewInt(1)},
		balanceByAddress: map[engine.Address]*big.Int{
			from: big.NewInt(10),
			mid:  big.NewInt(0),
			to:   big.NewInt(0),
		},
		transactions: map[engine.Hash]upstream.Transaction{
			priorHash:  priorTx,
			targetHash: targetTx,
		},
		receipts: map[engine.Hash]upstream.Receipt{
			priorHash:  {TransactionHash: priorHash, BlockNumber: big.NewInt(21), TransactionIndex: 0, GasUsed: 21000, Status: &status},
			targetHash: {TransactionHash: targetHash, BlockNumber: big.NewInt(21), TransactionIndex: 1, GasUsed: 21000, Status: &status},
		},
		blockTransactions: map[string][]upstream.Transaction{
			blockRef.CacheKey(): {priorTx, targetTx},
		},
	}
	engineRef, err := New(Config{Provider: provider, Fork: engine.ForkLondon, Block: blockRef})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	replay, err := engineRef.ReplayTransaction(context.Background(), targetHash)
	if err != nil {
		t.Fatalf("ReplayTransaction() error = %v", err)
	}
	if !replay.Exact {
		t.Fatalf("replay.Exact = false, limitation = %q", replay.Limitation)
	}
	if replay.Result == nil || replay.Result.Status != engine.StatusSuccess {
		t.Fatalf("replay.Result = %#v, want success", replay.Result)
	}
	if len(replay.AppliedPriorTransactions) != 1 || replay.AppliedPriorTransactions[0] != priorHash {
		t.Fatalf("applied prior txs = %#v, want [%x]", replay.AppliedPriorTransactions, priorHash)
	}
	if replay.Prepared == nil || replay.Prepared.ReplayState == nil {
		t.Fatalf("prepared replay state missing")
	}
	if replay.Prepared.StateSourceBlockRef.CacheKey() != upstream.BlockNumber(20).CacheKey() {
		t.Fatalf("state source block = %s, want parent block", replay.Prepared.StateSourceBlockRef.CacheKey())
	}
	if replay.Prepared.StateBlockRef.CacheKey() == replay.Prepared.StateSourceBlockRef.CacheKey() {
		t.Fatalf("state block ref should be synthetic after reconstruction")
	}
	comparison := CompareReplayToReceipt(replay.Transaction, replay.Receipt, replay.Result)
	if !comparison.Match {
		t.Fatalf("receipt comparison mismatch = %#v", comparison.FirstMismatch)
	}
}

func TestExportSnapshotIncludesReplayMetadata(t *testing.T) {
	txHash := engine.Hash{0x33}
	replayState := ReplayState{
		TargetTransactionHash:    txHash,
		ExecutionBlockRef:        upstream.BlockNumber(30),
		StateBlockRef:            upstream.BlockRef{Tag: upstream.BlockTag("replay:30:test:1")},
		StateSourceBlockRef:      upstream.BlockNumber(29),
		AppliedPriorTransactions: []engine.Hash{{0x31}, {0x32}},
		Exact:                    true,
	}
	engineRef, err := New(Config{Provider: &stubProvider{block: upstream.Block{Number: big.NewInt(30)}}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	engineRef.recordReplayState(&replayState)
	snapshot := engineRef.ExportSnapshot()
	if len(snapshot.ReplayStates) != 1 {
		t.Fatalf("len(snapshot.ReplayStates) = %d, want 1", len(snapshot.ReplayStates))
	}
	if snapshot.ReplayStates[0].TargetTransactionHash != txHash {
		t.Fatalf("exported target tx = %#v, want %#v", snapshot.ReplayStates[0].TargetTransactionHash, txHash)
	}
	restored, err := New(Config{Provider: &stubProvider{block: upstream.Block{Number: big.NewInt(30)}}})
	if err != nil {
		t.Fatalf("New() restore error = %v", err)
	}
	restored.ImportSnapshot(snapshot)
	cached, ok := restored.lookupReplayState(txHash)
	if !ok {
		t.Fatalf("restored replay metadata missing")
	}
	if len(cached.AppliedPriorTransactions) != 2 {
		t.Fatalf("restored applied tx count = %d, want 2", len(cached.AppliedPriorTransactions))
	}
}
