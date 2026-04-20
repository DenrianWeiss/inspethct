package forkengine

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"inspethct/internal/engine"
	"inspethct/internal/forkengine/cache"
	"inspethct/internal/forkengine/overlay"
	"inspethct/internal/forkengine/upstream"
)

type Mode string

const (
	ModeDiff   Mode = "diff"
	ModePinned Mode = "pinned"
)

type Config struct {
	Mode            Mode
	Fork            engine.Fork
	ChainIDOverride *big.Int
	Provider        upstream.Provider
	Cache           cache.Store
	Block           upstream.BlockRef
	CachePolicy     CachePolicy
}

type CachePolicy struct {
	CacheLocalWritesInDiff   bool
	CacheLocalWritesInPinned bool
}

type Engine struct {
	mode            Mode
	fork            engine.Fork
	chainIDOverride *big.Int
	provider        upstream.Provider
	cache           cache.Store
	block           upstream.BlockRef
	cachePolicy     CachePolicy
	replayStates    map[string]ReplayState
}

type ReplayState struct {
	TargetTransactionHash    engine.Hash
	ExecutionBlockRef        upstream.BlockRef
	StateBlockRef            upstream.BlockRef
	StateSourceBlockRef      upstream.BlockRef
	AppliedPriorTransactions []engine.Hash
	Exact                    bool
	Limitation               string
}

type ExportedSnapshot struct {
	Cache        cache.ExportedState
	ReplayStates []ReplayState
}

type preparedStateView struct {
	CacheBlockRef  upstream.BlockRef
	SourceBlockRef upstream.BlockRef
}

type StateRequest struct {
	Block upstream.BlockRef
}

type CallRequest struct {
	From          engine.Address
	To            engine.Address
	Input         []byte
	Value         *big.Int
	GasLimit      uint64
	GasPrice      *big.Int
	BlobGasFeeCap *big.Int
	BlobHashes    []engine.Hash
	Block         upstream.BlockRef
}

type PreparedCall struct {
	Block               upstream.Block
	BlockRef            upstream.BlockRef
	StateBlockRef       upstream.BlockRef
	StateSourceBlockRef upstream.BlockRef
	Overlay             *overlay.State
	Create              bool
	Target              *engine.Address
	ReplayState         *ReplayState
	Config              engine.ExecutionConfig
}

type ReplayExecution struct {
	Prepared                 *PreparedCall
	Transaction              upstream.Transaction
	Receipt                  upstream.Receipt
	Result                   *engine.ExecutionResult
	Exact                    bool
	Limitation               string
	AppliedPriorTransactions []engine.Hash
}

func New(cfg Config) (*Engine, error) {
	if cfg.Provider == nil {
		return nil, errors.New("forkengine: provider is required")
	}
	mode := cfg.Mode
	if mode == "" {
		mode = ModeDiff
	}
	if mode != ModeDiff && mode != ModePinned {
		return nil, fmt.Errorf("forkengine: unsupported mode %q", mode)
	}
	if err := cfg.Block.Validate(); err != nil {
		return nil, fmt.Errorf("forkengine: invalid block ref: %w", err)
	}
	block := cfg.Block
	if isZeroBlockRef(block) {
		block = upstream.LatestBlock()
	}
	if mode == ModePinned && !block.IsPinned() {
		return nil, errors.New("forkengine: pinned mode requires a pinned block number")
	}
	policy := cfg.CachePolicy
	if !policy.CacheLocalWritesInDiff && !policy.CacheLocalWritesInPinned {
		policy.CacheLocalWritesInPinned = true
	}
	store := cfg.Cache
	if store == nil {
		store = cache.NewMemoryStore()
	}
	return &Engine{
		mode:            mode,
		fork:            cfg.Fork,
		chainIDOverride: cloneBigInt(cfg.ChainIDOverride),
		provider:        cfg.Provider,
		cache:           store,
		block:           block.Normalize(),
		cachePolicy:     policy,
		replayStates:    make(map[string]ReplayState),
	}, nil
}

func (engineRef *Engine) Mode() Mode {
	return engineRef.mode
}

func (engineRef *Engine) ResolveBlock(ctx context.Context, ref upstream.BlockRef) (upstream.Block, error) {
	resolvedRef, err := engineRef.resolveBlockRef(ref)
	if err != nil {
		return upstream.Block{}, err
	}
	if block, ok := engineRef.cache.GetBlock(resolvedRef); ok {
		return engineRef.populateBlockChainID(ctx, resolvedRef, block)
	}
	block, err := engineRef.provider.GetBlock(ctx, resolvedRef)
	if err != nil {
		return upstream.Block{}, err
	}
	return engineRef.populateBlockChainID(ctx, resolvedRef, block)
}

func (engineRef *Engine) populateBlockChainID(ctx context.Context, ref upstream.BlockRef, block upstream.Block) (upstream.Block, error) {
	if block.ChainID != nil {
		engineRef.cache.PutBlock(ref, block)
		return block, nil
	}
	chainID := cloneBigInt(engineRef.chainIDOverride)
	if chainID == nil {
		var err error
		chainID, err = engineRef.provider.ChainID(ctx)
		if err != nil {
			return upstream.Block{}, err
		}
	}
	if chainID != nil {
		block.ChainID = new(big.Int).Set(chainID)
	}
	engineRef.cache.PutBlock(ref, block)
	return block, nil
}

func (engineRef *Engine) PrepareCall(ctx context.Context, req CallRequest) (*PreparedCall, error) {
	resolvedRef, err := engineRef.resolveBlockRef(req.Block)
	if err != nil {
		return nil, err
	}
	return engineRef.prepareCallAgainstState(ctx, req, preparedStateView{CacheBlockRef: resolvedRef, SourceBlockRef: resolvedRef}, nil)
}

func (engineRef *Engine) prepareCallAgainstState(ctx context.Context, req CallRequest, stateView preparedStateView, replayState *ReplayState) (*PreparedCall, error) {
	block, err := engineRef.ResolveBlock(ctx, req.Block)
	if err != nil {
		return nil, err
	}
	resolvedRef, err := engineRef.resolveBlockRef(req.Block)
	if err != nil {
		return nil, err
	}
	accountState := newForkAccountState(ctx, engineRef.provider, engineRef.cache, stateView.CacheBlockRef, stateView.SourceBlockRef)
	storageState := newForkStorage(ctx, engineRef.provider, engineRef.cache, stateView.CacheBlockRef, stateView.SourceBlockRef)
	code, err := engineRef.resolveCode(ctx, req.To, stateView)
	if err != nil {
		return nil, err
	}
	gasLimit := req.GasLimit
	if gasLimit == 0 {
		gasLimit = block.GasLimit
	}
	prepared := &PreparedCall{
		Block:               block,
		BlockRef:            resolvedRef,
		StateBlockRef:       stateView.CacheBlockRef,
		StateSourceBlockRef: stateView.SourceBlockRef,
		Overlay:             overlay.NewState(),
		Target:              &req.To,
		ReplayState:         cloneReplayState(replayState),
		Config: engine.ExecutionConfig{
			Fork:             engineRef.fork,
			GasLimit:         gasLimit,
			Value:            cloneBigInt(req.Value),
			Input:            cloneBytes(req.Input),
			Code:             cloneBytes(code),
			CodeHash:         hashBytes(code),
			Origin:           req.From,
			Caller:           req.From,
			ContractAddress:  req.To,
			BlockContext:     newBlockContext(block),
			TxContext:        newTxContext(req.From, req.GasPrice, req.BlobHashes, req.BlobGasFeeCap),
			State:            accountState,
			Storage:          storageState,
			TransientStorage: engine.NewInMemoryTransientStorage(),
			AccessList:       engine.NewSimpleAccessList(),
		},
	}
	return prepared, nil
}

func (engineRef *Engine) ExecuteCall(ctx context.Context, req CallRequest) (*PreparedCall, *engine.ExecutionResult, error) {
	prepared, err := engineRef.PrepareCall(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	result, err := engineRef.ExecutePreparedCall(prepared)
	if commitErr := engineRef.CommitLocalWrites(prepared, result); commitErr != nil {
		if err == nil {
			err = commitErr
		}
	}
	return prepared, result, err
}

func (engineRef *Engine) PrepareReplay(ctx context.Context, txHash engine.Hash) (*PreparedCall, upstream.Transaction, upstream.Receipt, error) {
	txProvider, ok := engineRef.provider.(upstream.TransactionProvider)
	if !ok {
		return nil, upstream.Transaction{}, upstream.Receipt{}, errors.New("forkengine: provider does not implement transaction replay capabilities")
	}
	tx, err := txProvider.GetTransactionByHash(ctx, txHash)
	if err != nil {
		return nil, upstream.Transaction{}, upstream.Receipt{}, err
	}
	receipt, err := txProvider.GetTransactionReceipt(ctx, txHash)
	if err != nil {
		return nil, upstream.Transaction{}, upstream.Receipt{}, err
	}
	if tx.To == nil {
		if receipt.ContractAddress == nil {
			return nil, upstream.Transaction{}, upstream.Receipt{}, errors.New("forkengine: contract-creation replay requires receipt contract address")
		}
		return engineRef.prepareCreateReplay(ctx, tx, receipt, txProvider)
	}
	executionBlockRef := engineRef.block
	if tx.BlockNumber != nil && tx.BlockNumber.IsUint64() {
		executionBlockRef = upstream.BlockNumber(tx.BlockNumber.Uint64())
	}
	replayState, stateView, err := engineRef.prepareReplayState(ctx, tx, executionBlockRef, txProvider)
	if err != nil {
		return nil, upstream.Transaction{}, upstream.Receipt{}, err
	}
	prepared, err := engineRef.prepareCallAgainstState(ctx, CallRequest{
		From:          tx.From,
		To:            *tx.To,
		Input:         tx.Input,
		Value:         tx.Value,
		GasLimit:      tx.Gas,
		GasPrice:      tx.GasPrice,
		BlobGasFeeCap: tx.BlobGasFeeCap,
		BlobHashes:    tx.BlobHashes,
		Block:         executionBlockRef,
	}, stateView, replayState)
	if err != nil {
		return nil, upstream.Transaction{}, upstream.Receipt{}, err
	}
	engineRef.recordReplayState(replayState)
	return prepared, tx.Clone(), receipt.Clone(), nil
}

func (engineRef *Engine) ReplayTransaction(ctx context.Context, txHash engine.Hash) (*ReplayExecution, error) {
	prepared, tx, receipt, err := engineRef.PrepareReplay(ctx, txHash)
	if err != nil {
		return nil, err
	}
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
	replayState := cloneReplayState(prepared.ReplayState)
	exact := true
	limitation := ""
	appliedPrior := []engine.Hash(nil)
	if replayState != nil {
		exact = replayState.Exact
		limitation = replayState.Limitation
		appliedPrior = append([]engine.Hash(nil), replayState.AppliedPriorTransactions...)
	}
	return &ReplayExecution{Prepared: prepared, Transaction: tx, Receipt: receipt, Result: result, Exact: exact, Limitation: limitation, AppliedPriorTransactions: appliedPrior}, nil
}

func (engineRef *Engine) ExecutePreparedCall(prepared *PreparedCall) (*engine.ExecutionResult, error) {
	if prepared == nil {
		return nil, errors.New("forkengine: prepared call is required")
	}
	if prepared.Create {
		return engineRef.executePreparedCreate(prepared)
	}
	runner := engine.NewSimpleEngine()
	return runner.Run(&prepared.Config)
}

func (engineRef *Engine) CommitLocalWrites(prepared *PreparedCall, result *engine.ExecutionResult) error {
	if prepared == nil {
		return errors.New("forkengine: prepared call is required")
	}
	if !engineRef.shouldCacheLocalWrites() {
		return nil
	}
	if prepared.Create && prepared.Target != nil && result != nil && result.Status == engine.StatusSuccess {
		createDiff := &engine.StateDiff{
			CodeChanges:     map[engine.Address][]byte{*prepared.Target: append([]byte(nil), result.ReturnData...)},
			CreatedAccounts: []engine.Address{*prepared.Target},
		}
		engineRef.cache.ApplyStateDiff(prepared.StateBlockRef, createDiff)
	}
	engineRef.cache.ApplyOverlay(prepared.StateBlockRef, prepared.Overlay)
	if result != nil {
		engineRef.cache.ApplyStateDiff(prepared.StateBlockRef, result.StateChanges)
	}
	return nil
}

func (engineRef *Engine) ExportState() cache.ExportedState {
	return engineRef.cache.ExportState()
}

func (engineRef *Engine) ExportSnapshot() ExportedSnapshot {
	states := make([]ReplayState, 0, len(engineRef.replayStates))
	for _, replayState := range engineRef.replayStates {
		states = append(states, replayState.Clone())
	}
	return ExportedSnapshot{Cache: engineRef.cache.ExportState(), ReplayStates: states}
}

func (engineRef *Engine) ImportState(state cache.ExportedState) {
	engineRef.cache.ImportState(state)
}

func (engineRef *Engine) ImportSnapshot(snapshot ExportedSnapshot) {
	engineRef.cache.ImportState(snapshot.Cache)
	engineRef.replayStates = make(map[string]ReplayState, len(snapshot.ReplayStates))
	for _, replayState := range snapshot.ReplayStates {
		engineRef.replayStates[replayStateKey(replayState.TargetTransactionHash)] = replayState.Clone()
	}
}

func (engineRef *Engine) shouldCacheLocalWrites() bool {
	if engineRef.mode == ModePinned {
		return engineRef.cachePolicy.CacheLocalWritesInPinned
	}
	return engineRef.cachePolicy.CacheLocalWritesInDiff
}

func (engineRef *Engine) resolveBlockRef(ref upstream.BlockRef) (upstream.BlockRef, error) {
	if err := ref.Validate(); err != nil {
		return upstream.BlockRef{}, err
	}
	if engineRef.mode == ModePinned {
		if isZeroBlockRef(ref) || ref.Normalize().Tag == upstream.BlockTagLatest {
			return engineRef.block, nil
		}
		if ref.Normalize().CacheKey() != engineRef.block.CacheKey() {
			return upstream.BlockRef{}, errors.New("forkengine: pinned mode cannot resolve a different block")
		}
		return engineRef.block, nil
	}
	if isZeroBlockRef(ref) {
		return engineRef.block, nil
	}
	return ref.Normalize(), nil
}

func isZeroBlockRef(ref upstream.BlockRef) bool {
	return ref.Number == nil && ref.Tag == ""
}

func cloneBigInt(value *big.Int) *big.Int {
	if value == nil {
		return nil
	}
	return new(big.Int).Set(value)
}

func cloneBytes(value []byte) []byte {
	if len(value) == 0 {
		return []byte{}
	}
	return append([]byte(nil), value...)
}

func (engineRef *Engine) prepareCreateReplay(ctx context.Context, tx upstream.Transaction, receipt upstream.Receipt, txProvider upstream.TransactionProvider) (*PreparedCall, upstream.Transaction, upstream.Receipt, error) {
	executionBlockRef := engineRef.block
	if tx.BlockNumber != nil && tx.BlockNumber.IsUint64() {
		executionBlockRef = upstream.BlockNumber(tx.BlockNumber.Uint64())
	}
	block, err := engineRef.ResolveBlock(ctx, executionBlockRef)
	if err != nil {
		return nil, upstream.Transaction{}, upstream.Receipt{}, err
	}
	resolvedRef, err := engineRef.resolveBlockRef(executionBlockRef)
	if err != nil {
		return nil, upstream.Transaction{}, upstream.Receipt{}, err
	}
	replayState, stateView, err := engineRef.prepareReplayState(ctx, tx, executionBlockRef, txProvider)
	if err != nil {
		return nil, upstream.Transaction{}, upstream.Receipt{}, err
	}
	accountState := newForkAccountState(ctx, engineRef.provider, engineRef.cache, stateView.CacheBlockRef, stateView.SourceBlockRef)
	storageState := newForkStorage(ctx, engineRef.provider, engineRef.cache, stateView.CacheBlockRef, stateView.SourceBlockRef)
	createdAddress := *receipt.ContractAddress
	prepared := &PreparedCall{
		Block:               block,
		BlockRef:            resolvedRef,
		StateBlockRef:       stateView.CacheBlockRef,
		StateSourceBlockRef: stateView.SourceBlockRef,
		Overlay:             overlay.NewState(),
		Create:              true,
		Target:              &createdAddress,
		ReplayState:         cloneReplayState(replayState),
		Config: engine.ExecutionConfig{
			Fork:             engineRef.fork,
			GasLimit:         tx.Gas,
			Value:            cloneBigInt(tx.Value),
			Input:            cloneBytes(tx.Input),
			Code:             cloneBytes(tx.Input),
			CodeHash:         hashBytes(tx.Input),
			Origin:           tx.From,
			Caller:           tx.From,
			ContractAddress:  tx.From,
			BlockContext:     newBlockContext(block),
			TxContext:        newTxContext(tx.From, tx.GasPrice, tx.BlobHashes, tx.BlobGasFeeCap),
			State:            accountState,
			Storage:          storageState,
			TransientStorage: engine.NewInMemoryTransientStorage(),
			AccessList:       engine.NewSimpleAccessList(),
		},
	}
	engineRef.recordReplayState(replayState)
	return prepared, tx.Clone(), receipt.Clone(), nil
}

func (engineRef *Engine) executePreparedCreate(prepared *PreparedCall) (*engine.ExecutionResult, error) {
	state, err := engine.NewSimpleEngine().NewState(&prepared.Config)
	if err != nil {
		return nil, err
	}
	evm := engine.NewEVM(state, prepared.Config.Fork)
	if prepared.Config.Hooks != nil {
		evm.SetHooks(prepared.Config.Hooks)
	}
	if prepared.Target == nil {
		return nil, errors.New("forkengine: prepared create replay requires target address")
	}
	result, err := evm.ExecuteMessage(&engine.Message{
		Caller:    prepared.Config.Caller,
		Callee:    *prepared.Target,
		Value:     cloneBigInt(prepared.Config.Value),
		Gas:       prepared.Config.GasLimit,
		Input:     append([]byte(nil), prepared.Config.Input...),
		Code:      append([]byte(nil), prepared.Config.Code...),
		CodeAddr:  *prepared.Target,
		CallDepth: prepared.Config.CallDepth,
		Kind:      engine.CallKindCall,
		IsCreate:  true,
	})
	if result != nil {
		if result.Status == engine.StatusSuccess {
			deployedCode := append([]byte(nil), result.ReturnData...)
			if len(deployedCode) > 0 && deployedCode[0] == 0xEF {
				result.Status = engine.StatusInvalidContractPrefix
				result.Err = engine.ErrInvalidOpcode
				result.GasRemaining = 0
			} else if len(deployedCode) > 0x6000 {
				result.Status = engine.StatusMaxCodeSizeExceeded
				result.GasRemaining = 0
			} else {
				depositGas := uint64(len(deployedCode)) * engine.GasCodeDeposit
				if result.GasRemaining < depositGas {
					result.Status = engine.StatusCodeStoreOutOfGas
					result.GasRemaining = 0
				} else {
					result.GasRemaining -= depositGas
					state.Account().SetCode(*prepared.Target, deployedCode)
					result.CreatedAddress = prepared.Target
				}
			}
		}
		result.GasUsed = prepared.Config.GasLimit - result.GasRemaining
	}
	return result, err
}

func (engineRef *Engine) resolveCode(ctx context.Context, addr engine.Address, stateView preparedStateView) ([]byte, error) {
	if code, ok := engineRef.cache.GetCode(addr, stateView.CacheBlockRef); ok {
		if code == nil {
			return []byte{}, nil
		}
		return code, nil
	}
	code, err := engineRef.provider.GetCode(ctx, addr, stateView.SourceBlockRef)
	if err != nil {
		return nil, err
	}
	if code == nil {
		code = []byte{}
	}
	engineRef.cache.PutCode(addr, stateView.CacheBlockRef, code)
	return code, nil
}

func replayStateBlockRef(tx upstream.Transaction) upstream.BlockRef {
	if tx.BlockNumber != nil && tx.BlockNumber.Sign() > 0 {
		parent := new(big.Int).Sub(tx.BlockNumber, big.NewInt(1))
		if parent.Sign() >= 0 && parent.IsUint64() {
			return upstream.BlockNumber(parent.Uint64())
		}
	}
	if tx.BlockNumber != nil && tx.BlockNumber.IsUint64() {
		return upstream.BlockNumber(tx.BlockNumber.Uint64())
	}
	return upstream.LatestBlock()
}

func (state ReplayState) Clone() ReplayState {
	clone := state
	clone.ExecutionBlockRef = state.ExecutionBlockRef.Normalize()
	clone.StateBlockRef = state.StateBlockRef.Normalize()
	clone.StateSourceBlockRef = state.StateSourceBlockRef.Normalize()
	clone.AppliedPriorTransactions = append([]engine.Hash(nil), state.AppliedPriorTransactions...)
	return clone
}

func (engineRef *Engine) prepareReplayState(ctx context.Context, tx upstream.Transaction, executionBlockRef upstream.BlockRef, txProvider upstream.TransactionProvider) (*ReplayState, preparedStateView, error) {
	if cached, ok := engineRef.lookupReplayState(tx.Hash); ok {
		return cached, preparedStateView{CacheBlockRef: cached.StateBlockRef, SourceBlockRef: cached.StateSourceBlockRef}, nil
	}
	sourceRef, err := engineRef.resolveBlockRef(replayStateBlockRef(tx))
	if err != nil {
		return nil, preparedStateView{}, err
	}
	replayState := &ReplayState{
		TargetTransactionHash: tx.Hash,
		ExecutionBlockRef:     executionBlockRef.Normalize(),
		StateBlockRef:         sourceRef,
		StateSourceBlockRef:   sourceRef,
		Exact:                 true,
	}
	if tx.TransactionIndex == nil || *tx.TransactionIndex == 0 {
		return replayState, preparedStateView{CacheBlockRef: sourceRef, SourceBlockRef: sourceRef}, nil
	}
	if err := engineRef.reconstructPriorTransactions(ctx, executionBlockRef, tx, txProvider, replayState); err != nil {
		return nil, preparedStateView{}, err
	}
	return replayState, preparedStateView{CacheBlockRef: replayState.StateBlockRef, SourceBlockRef: replayState.StateSourceBlockRef}, nil
}

func (engineRef *Engine) reconstructPriorTransactions(ctx context.Context, executionBlockRef upstream.BlockRef, target upstream.Transaction, txProvider upstream.TransactionProvider, replayState *ReplayState) error {
	blockProvider, ok := engineRef.provider.(upstream.BlockTransactionsProvider)
	if !ok {
		replayState.Exact = false
		replayState.Limitation = "provider does not expose block transaction lists for prior-transaction reconstruction"
		return nil
	}
	transactions, err := blockProvider.GetBlockTransactions(ctx, executionBlockRef)
	if err != nil {
		replayState.Exact = false
		replayState.Limitation = fmt.Sprintf("failed to load block transactions: %v", err)
		return nil
	}
	syntheticStateRef := syntheticReplayStateBlockRef(target)
	stateView := preparedStateView{CacheBlockRef: syntheticStateRef, SourceBlockRef: replayState.StateSourceBlockRef}
	applied := make([]engine.Hash, 0)
	foundTarget := false
	for _, transaction := range transactions {
		if transaction.Hash == target.Hash {
			foundTarget = true
			break
		}
		receipt, err := txProvider.GetTransactionReceipt(ctx, transaction.Hash)
		if err != nil {
			replayState.Exact = false
			replayState.Limitation = fmt.Sprintf("failed to load receipt for prior transaction %x: %v", transaction.Hash, err)
			return nil
		}
		prepared, err := engineRef.prepareReplayTransactionWithState(ctx, transaction, receipt, executionBlockRef, stateView, nil)
		if err != nil {
			replayState.Exact = false
			replayState.Limitation = fmt.Sprintf("failed to prepare prior transaction %x: %v", transaction.Hash, err)
			return nil
		}
		result, execErr := engineRef.ExecutePreparedCall(prepared)
		if execErr != nil && result == nil {
			replayState.Exact = false
			replayState.Limitation = fmt.Sprintf("failed to execute prior transaction %x: %v", transaction.Hash, execErr)
			return nil
		}
		if commitErr := engineRef.CommitLocalWrites(prepared, result); commitErr != nil {
			replayState.Exact = false
			replayState.Limitation = fmt.Sprintf("failed to commit prior transaction %x: %v", transaction.Hash, commitErr)
			return nil
		}
		comparison := CompareReplayToReceipt(transaction, receipt, result)
		if !comparison.Match {
			replayState.Exact = false
			replayState.Limitation = fmt.Sprintf("prior transaction %x replay mismatch on %s", transaction.Hash, comparison.FirstMismatch.Field)
			return nil
		}
		applied = append(applied, transaction.Hash)
	}
	if !foundTarget {
		replayState.Exact = false
		replayState.Limitation = "target transaction was not found in the fetched block transaction list"
		return nil
	}
	replayState.StateBlockRef = syntheticStateRef
	replayState.AppliedPriorTransactions = applied
	return nil
}

func (engineRef *Engine) prepareReplayTransactionWithState(ctx context.Context, tx upstream.Transaction, receipt upstream.Receipt, executionBlockRef upstream.BlockRef, stateView preparedStateView, replayState *ReplayState) (*PreparedCall, error) {
	if tx.To == nil {
		block, err := engineRef.ResolveBlock(ctx, executionBlockRef)
		if err != nil {
			return nil, err
		}
		resolvedRef, err := engineRef.resolveBlockRef(executionBlockRef)
		if err != nil {
			return nil, err
		}
		createdAddress := *receipt.ContractAddress
		accountState := newForkAccountState(ctx, engineRef.provider, engineRef.cache, stateView.CacheBlockRef, stateView.SourceBlockRef)
		storageState := newForkStorage(ctx, engineRef.provider, engineRef.cache, stateView.CacheBlockRef, stateView.SourceBlockRef)
		return &PreparedCall{
			Block:               block,
			BlockRef:            resolvedRef,
			StateBlockRef:       stateView.CacheBlockRef,
			StateSourceBlockRef: stateView.SourceBlockRef,
			Overlay:             overlay.NewState(),
			Create:              true,
			Target:              &createdAddress,
			ReplayState:         cloneReplayState(replayState),
			Config: engine.ExecutionConfig{
				Fork:             engineRef.fork,
				GasLimit:         tx.Gas,
				Value:            cloneBigInt(tx.Value),
				Input:            cloneBytes(tx.Input),
				Code:             cloneBytes(tx.Input),
				CodeHash:         hashBytes(tx.Input),
				Origin:           tx.From,
				Caller:           tx.From,
				ContractAddress:  tx.From,
				BlockContext:     newBlockContext(block),
				TxContext:        newTxContext(tx.From, tx.GasPrice, tx.BlobHashes, tx.BlobGasFeeCap),
				State:            accountState,
				Storage:          storageState,
				TransientStorage: engine.NewInMemoryTransientStorage(),
				AccessList:       engine.NewSimpleAccessList(),
			},
		}, nil
	}
	return engineRef.prepareCallAgainstState(ctx, CallRequest{
		From:          tx.From,
		To:            *tx.To,
		Input:         tx.Input,
		Value:         tx.Value,
		GasLimit:      tx.Gas,
		GasPrice:      tx.GasPrice,
		BlobGasFeeCap: tx.BlobGasFeeCap,
		BlobHashes:    tx.BlobHashes,
		Block:         executionBlockRef,
	}, stateView, replayState)
}

func (engineRef *Engine) recordReplayState(replayState *ReplayState) {
	if replayState == nil {
		return
	}
	engineRef.replayStates[replayStateKey(replayState.TargetTransactionHash)] = replayState.Clone()
}

func (engineRef *Engine) lookupReplayState(txHash engine.Hash) (*ReplayState, bool) {
	replayState, ok := engineRef.replayStates[replayStateKey(txHash)]
	if !ok {
		return nil, false
	}
	clone := replayState.Clone()
	return &clone, true
}

func replayStateKey(txHash engine.Hash) string {
	return fmt.Sprintf("%x", txHash)
}

func cloneReplayState(replayState *ReplayState) *ReplayState {
	if replayState == nil {
		return nil
	}
	clone := replayState.Clone()
	return &clone
}

func syntheticReplayStateBlockRef(tx upstream.Transaction) upstream.BlockRef {
	index := uint64(0)
	if tx.TransactionIndex != nil {
		index = *tx.TransactionIndex
	}
	blockPart := "latest"
	if tx.BlockNumber != nil {
		blockPart = tx.BlockNumber.String()
	}
	return upstream.BlockRef{Tag: upstream.BlockTag(fmt.Sprintf("replay:%s:%x:%d", blockPart, tx.Hash, index))}
}
