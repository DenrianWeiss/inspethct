package engine

import (
	"fmt"
	"math/big"
)

// SimpleEngine implements the Engine interface.
type SimpleEngine struct{}

func intrinsicTxGas(data []byte) uint64 {
	gas := uint64(21000)
	for _, b := range data {
		if b == 0 {
			gas += 4
		} else {
			gas += 16
		}
	}
	return gas
}

func warmInitialAccessList(accessList AccessList, cfg *ExecutionConfig) {
	if accessList == nil || cfg == nil {
		return
	}
	accessList.WarmAddress(cfg.Origin)
	accessList.WarmAddress(cfg.Caller)
	accessList.WarmAddress(cfg.ContractAddress)
	if forkGTE(cfg.Fork, ForkShanghai) && cfg.BlockContext != nil {
		accessList.WarmAddress(cfg.BlockContext.Coinbase())
	}
	for _, addr := range warmAddressesForConfig(cfg) {
		accessList.WarmAddress(addr)
	}
}

func warmAddressesForConfig(cfg *ExecutionConfig) []Address {
	if cfg == nil {
		return nil
	}
	seen := make(map[Address]struct{})
	addrs := make([]Address, 0)
	appendUnique := func(values []Address) {
		for _, addr := range values {
			if _, ok := seen[addr]; ok {
				continue
			}
			seen[addr] = struct{}{}
			addrs = append(addrs, addr)
		}
	}
	if cfg.Precompiles != nil {
		appendUnique(cfg.Precompiles.Addresses())
	} else {
		appendUnique(MainnetPrecompileAddresses(cfg.Fork))
	}
	appendUnique(cfg.WarmAddresses)
	return addrs
}

func applyBaseFeeBurn(state EVMState, cfg *ExecutionConfig, res *ExecutionResult) {
	if state == nil || cfg == nil || res == nil || !forkGTE(cfg.Fork, ForkLondon) || cfg.BlockContext == nil {
		return
	}
	baseFee := cfg.BlockContext.BaseFee()
	if baseFee == nil || baseFee.Sign() == 0 {
		return
	}
	chargedGas := intrinsicTxGas(cfg.Input) + (cfg.GasLimit - res.GasRemaining)
	refund := res.GasRefund
	maxRefund := chargedGas / 5
	if refund > maxRefund {
		refund = maxRefund
	}
	if refund >= chargedGas {
		return
	}
	chargedGas -= refund
	burn := new(big.Int).Mul(new(big.Int).SetUint64(chargedGas), baseFee)
	if burn.Sign() == 0 {
		return
	}
	state.Account().SubBalance(cfg.BlockContext.Coinbase(), burn)
}

// NewSimpleEngine creates a new simple EVM engine.
func NewSimpleEngine() Engine {
	return &SimpleEngine{}
}

func (e *SimpleEngine) Run(cfg *ExecutionConfig) (*ExecutionResult, error) {
	if err := e.ValidateConfig(cfg); err != nil {
		return nil, err
	}
	state, err := e.NewState(cfg)
	if err != nil {
		return nil, err
	}
	accSnap, stoSnap, result := applyTopLevelCallValue(state, cfg)
	if result != nil {
		return result, nil
	}
	res, err := e.runWithConfig(state, cfg)
	revertTopLevelCallValue(state, accSnap, stoSnap, res)
	applyBaseFeeBurn(state, cfg, res)
	return res, err
}


func applyTopLevelCallValue(state EVMState, cfg *ExecutionConfig) (int, int, *ExecutionResult) {
	if state == nil || cfg == nil || cfg.Value == nil || cfg.Value.Sign() == 0 {
		return -1, -1, nil
	}
	if cfg.Caller == cfg.ContractAddress {
		return -1, -1, nil
	}
	account := state.Account()
	if account.Balance(cfg.Caller).Cmp(cfg.Value) < 0 {
		return -1, -1, &ExecutionResult{Status: StatusInsufficientBalance, GasRemaining: cfg.GasLimit}
	}
	accSnap := account.Snapshot()
	stoSnap := state.Storage().Snapshot()
	account.SubBalance(cfg.Caller, cfg.Value)
	account.AddBalance(cfg.ContractAddress, cfg.Value)
	return accSnap, stoSnap, nil
}

func revertTopLevelCallValue(state EVMState, accSnap int, stoSnap int, result *ExecutionResult) {
	if state == nil || accSnap < 0 || stoSnap < 0 {
		return
	}
	if result != nil && result.Status == StatusSuccess {
		return
	}
	state.Account().RevertToSnapshot(accSnap)
	state.Storage().RevertToSnapshot(stoSnap)
}

func (e *SimpleEngine) RunWithState(state EVMState, code []byte) (*ExecutionResult, error) {
	return e.runWithFork(state, code, ForkLondon, nil)
}

func (e *SimpleEngine) runWithConfig(state EVMState, cfg *ExecutionConfig) (*ExecutionResult, error) {
	fork := cfg.Fork
	if fork == "" {
		fork = ForkLondon
	}
	evm := NewEVM(state, fork, cfg.Precompiles)
	if cfg.Hooks != nil {
		evm.SetHooks(cfg.Hooks)
	}
	res, err := evm.Run(cfg.Code)
	if res != nil && cfg.GasLimit >= res.GasRemaining {
		res.GasUsed = cfg.GasLimit - res.GasRemaining
	}
	return res, err
}

func (e *SimpleEngine) runWithFork(state EVMState, code []byte, fork Fork, precompiles *PrecompileRegistry) (*ExecutionResult, error) {
	if fork == "" {
		fork = ForkLondon
	}
	evm := NewEVM(state, fork, precompiles)
	res, err := evm.Run(code)
	if res != nil {
		gasLimit := state.GasMeter().Gas() + res.GasUsed
		if gasLimit >= res.GasRemaining {
			res.GasUsed = gasLimit - res.GasRemaining
		}
	}
	return res, err
}

func (e *SimpleEngine) NewState(cfg *ExecutionConfig) (EVMState, error) {
	stack := NewStack()
	memory := NewMemory()
	gasMeter := NewGasMeter(cfg.GasLimit)

	var storage Storage
	if cfg.Storage != nil {
		storage = cfg.Storage
	} else {
		storage = NewInMemoryStorage()
	}

	var transientStorage TransientStorage
	if cfg.TransientStorage != nil {
		transientStorage = cfg.TransientStorage
	} else {
		transientStorage = NewInMemoryTransientStorage()
	}

	var account MutableAccountState
	if cfg.State != nil {
		account = cfg.State
	} else {
		account = NewInMemoryAccountState()
	}

	var accessList AccessList
	if cfg.AccessList != nil {
		accessList = cfg.AccessList
	} else {
		accessList = NewSimpleAccessList()
	}
	warmInitialAccessList(accessList, cfg)

	contract := &SimpleContract{
		AddressVal:   cfg.ContractAddress,
		CallerVal:    cfg.Caller,
		CallValueVal: cfg.Value,
		CallInputVal: cfg.Input,
		CodeVal:      cfg.Code,
		CodeHashVal:  cfg.CodeHash,
		CodeAddrVal:  cfg.ContractAddress,
		IsStaticVal:  cfg.IsStatic,
	}

	var blockCtx BlockContext
	if cfg.BlockContext != nil {
		blockCtx = cfg.BlockContext
	} else {
		blockCtx = &SimpleBlockContext{NumberVal: big.NewInt(0), ChainIDVal: big.NewInt(1)}
	}

	var txCtx TxContext
	if cfg.TxContext != nil {
		txCtx = cfg.TxContext
	} else {
		txCtx = &SimpleTxContext{OriginVal: cfg.Origin, GasPriceVal: big.NewInt(0)}
	}

	state := NewEVMState(stack, memory, storage, transientStorage, account, gasMeter, blockCtx, txCtx, contract, accessList)
	if concrete, ok := state.(*evmState); ok {
		concrete.callDepth = cfg.CallDepth
	}
	return state, nil
}

func (e *SimpleEngine) SupportedForks() []Fork {
	return []Fork{ForkLondon, ForkParis, ForkShanghai, ForkCancun, ForkPrague, ForkAmsterdam, ForkOsaka}
}

func (e *SimpleEngine) ValidateConfig(cfg *ExecutionConfig) error {
	if cfg.Code == nil {
		return fmt.Errorf("code is nil")
	}
	if cfg.GasLimit == 0 {
		return fmt.Errorf("gas limit is zero")
	}
	return nil
}
