package engine

import (
	"errors"
	"math/big"
)

// ExecutionStatus indicates the final outcome of an EVM execution.
type ExecutionStatus int

const (
	// StatusSuccess indicates the execution completed without error.
	StatusSuccess ExecutionStatus = iota
	// StatusRevert indicates the execution reverted.
	StatusRevert
	// StatusHalt indicates the execution halted abnormally.
	StatusHalt
	// StatusOutOfGas indicates the execution ran out of gas.
	StatusOutOfGas
	// StatusInvalidJump indicates an invalid jump destination.
	StatusInvalidJump
	// StatusStackOverflow indicates the stack exceeded 1024 items.
	StatusStackOverflow
	// StatusStackUnderflow indicates an opcode required more stack items than available.
	StatusStackUnderflow
	// StatusInvalidOpcode indicates an unrecognized opcode.
	StatusInvalidOpcode
	// StatusCallDepthExceeded indicates the call depth exceeded 1024.
	StatusCallDepthExceeded
	// StatusInsufficientBalance indicates a call tried to transfer more than the caller had.
	StatusInsufficientBalance
	// StatusAddressCollision indicates a CREATE target already exists.
	StatusAddressCollision
	// StatusCodeStoreOutOfGas indicates insufficient gas to pay for code deposit.
	StatusCodeStoreOutOfGas
	// StatusMaxCodeSizeExceeded indicates deployed code exceeded MAX_CODE_SIZE.
	StatusMaxCodeSizeExceeded
	// StatusInvalidContractPrefix indicates code started with 0xEF (EIP-3541, London+).
	StatusInvalidContractPrefix
)

// ExecutionResult captures the complete outcome of an EVM message execution.
type ExecutionResult struct {
	// Status is the final execution status.
	Status ExecutionStatus
	// GasUsed is the total gas consumed by the execution.
	GasUsed uint64
	// GasRemaining is the gas left at the end of execution.
	GasRemaining uint64
	// GasRefund is the accumulated refund counter.
	GasRefund uint64
	// ReturnData is the output buffer (empty on failure unless REVERT).
	ReturnData []byte
	// Logs are the logs emitted during execution.
	Logs []Log
	// CreatedAddress is the address of a newly created contract (if any).
	CreatedAddress *Address
	// StateChanges is a record of all state mutations performed.
	StateChanges *StateDiff
	// Trace is the optional execution trace (if tracing was enabled).
	Trace []TraceStep
	// Err is the error that caused failure, if any.
	Err error
}

// StateDiff records mutations made to the world state during execution.
type StateDiff struct {
	// BalanceChanges maps address -> delta.
	BalanceChanges map[Address]*big.Int
	// NonceChanges maps address -> new nonce.
	NonceChanges map[Address]uint64
	// CodeChanges maps address -> new code.
	CodeChanges map[Address][]byte
	// StorageChanges maps (address, slot) -> new value.
	StorageChanges map[Address]map[Hash]Hash
	// CreatedAccounts lists addresses of accounts created during execution.
	CreatedAccounts []Address
	// DeletedAccounts lists addresses self-destructed during execution.
	DeletedAccounts []Address
}

// TraceStep represents a single step in an execution trace.
type TraceStep struct {
	// PC is the program counter.
	PC uint64
	// Op is the opcode byte.
	Op byte
	// GasRemaining is the gas left before executing this step.
	GasRemaining uint64
	// GasCost is the cost of this step.
	GasCost uint64
	// Stack is the stack contents before this step (optional, for deep traces).
	Stack []Word
	// MemorySize is the size of memory before this step.
	MemorySize uint64
	// Depth is the current call depth.
	Depth int
	// Err is set if this step caused an error.
	Err error
	// OpName is the human-readable opcode name.
	OpName string
}

// ResultAccessor provides structured access to an execution result.
type ResultAccessor interface {
	// Success returns true if the execution succeeded.
	Success() bool
	// Reverted returns true if the execution reverted.
	Reverted() bool
	// Failed returns true if the execution failed for any reason.
	Failed() bool
	// Output returns the return data.
	Output() []byte
	// OutputAsBigInt interprets the return data as a big.Int.
	OutputAsBigInt() *big.Int
	// OutputAsAddr interprets the last 20 bytes as an address.
	OutputAsAddr() Address
	// GasInfo returns (used, remaining, refund).
	GasInfo() (used, remaining, refund uint64)
	// Logs returns emitted logs.
	Logs() []Log
	// CreatedContract returns the created contract address, if any.
	CreatedContract() (*Address, bool)
	// StateDiff returns the recorded state mutations.
	StateDiff() *StateDiff
	// Trace returns the execution trace, if collected.
	Trace() []TraceStep
	// Error returns the failure error, if any.
	Error() error
}

// executionResult implements ResultAccessor.
type executionResult struct {
	inner *ExecutionResult
}

// NewResultAccessor wraps an ExecutionResult for programmatic access.
func NewResultAccessor(r *ExecutionResult) ResultAccessor {
	return &executionResult{inner: r}
}

func (r *executionResult) Success() bool         { return r.inner.Status == StatusSuccess }
func (r *executionResult) Reverted() bool        { return r.inner.Status == StatusRevert }
func (r *executionResult) Failed() bool          { return r.inner.Status != StatusSuccess }
func (r *executionResult) Output() []byte        { return r.inner.ReturnData }
func (r *executionResult) Logs() []Log           { return r.inner.Logs }
func (r *executionResult) StateDiff() *StateDiff { return r.inner.StateChanges }
func (r *executionResult) Trace() []TraceStep    { return r.inner.Trace }
func (r *executionResult) Error() error          { return r.inner.Err }

func (r *executionResult) OutputAsBigInt() *big.Int {
	return new(big.Int).SetBytes(r.inner.ReturnData)
}

func (r *executionResult) OutputAsAddr() Address {
	data := r.inner.ReturnData
	if len(data) < 20 {
		var addr Address
		copy(addr[20-len(data):], data)
		return addr
	}
	var addr Address
	copy(addr[:], data[len(data)-20:])
	return addr
}

func (r *executionResult) GasInfo() (uint64, uint64, uint64) {
	return r.inner.GasUsed, r.inner.GasRemaining, r.inner.GasRefund
}

func (r *executionResult) CreatedContract() (*Address, bool) {
	if r.inner.CreatedAddress == nil {
		return nil, false
	}
	return r.inner.CreatedAddress, true
}

// ExecutionResultBuilder constructs ExecutionResult instances.
type ExecutionResultBuilder struct {
	result *ExecutionResult
}

// NewExecutionResultBuilder starts building a new result.
func NewExecutionResultBuilder() *ExecutionResultBuilder {
	return &ExecutionResultBuilder{
		result: &ExecutionResult{
			StateChanges: &StateDiff{
				BalanceChanges: make(map[Address]*big.Int),
				NonceChanges:   make(map[Address]uint64),
				CodeChanges:    make(map[Address][]byte),
				StorageChanges: make(map[Address]map[Hash]Hash),
			},
		},
	}
}

// SetStatus sets the execution status.
func (b *ExecutionResultBuilder) SetStatus(s ExecutionStatus) *ExecutionResultBuilder {
	b.result.Status = s
	return b
}

// SetGasUsed sets the gas used.
func (b *ExecutionResultBuilder) SetGasUsed(g uint64) *ExecutionResultBuilder {
	b.result.GasUsed = g
	return b
}

// SetGasRemaining sets the remaining gas.
func (b *ExecutionResultBuilder) SetGasRemaining(g uint64) *ExecutionResultBuilder {
	b.result.GasRemaining = g
	return b
}

// SetGasRefund sets the gas refund.
func (b *ExecutionResultBuilder) SetGasRefund(g uint64) *ExecutionResultBuilder {
	b.result.GasRefund = g
	return b
}

// SetReturnData sets the return data.
func (b *ExecutionResultBuilder) SetReturnData(d []byte) *ExecutionResultBuilder {
	b.result.ReturnData = d
	return b
}

// SetLogs sets the logs.
func (b *ExecutionResultBuilder) SetLogs(l []Log) *ExecutionResultBuilder {
	b.result.Logs = l
	return b
}

// SetCreatedAddress sets the created contract address.
func (b *ExecutionResultBuilder) SetCreatedAddress(a *Address) *ExecutionResultBuilder {
	b.result.CreatedAddress = a
	return b
}

// SetStateChanges sets the state diff.
func (b *ExecutionResultBuilder) SetStateChanges(d *StateDiff) *ExecutionResultBuilder {
	b.result.StateChanges = d
	return b
}

// SetTrace sets the execution trace.
func (b *ExecutionResultBuilder) SetTrace(t []TraceStep) *ExecutionResultBuilder {
	b.result.Trace = t
	return b
}

// SetError sets the failure error.
func (b *ExecutionResultBuilder) SetError(e error) *ExecutionResultBuilder {
	b.result.Err = e
	return b
}

// Build finalizes and returns the ExecutionResult.
func (b *ExecutionResultBuilder) Build() *ExecutionResult {
	return b.result
}

// Common errors returned by the engine.
var (
	ErrOutOfGas              = errors.New("out of gas")
	ErrStackOverflow         = errors.New("stack overflow")
	ErrStackUnderflow        = errors.New("stack underflow")
	ErrInvalidJump           = errors.New("invalid jump destination")
	ErrInvalidOpcode         = errors.New("invalid opcode")
	ErrCallDepthExceeded     = errors.New("call depth exceeded")
	ErrInsufficientBalance   = errors.New("insufficient balance")
	ErrAddressCollision      = errors.New("address collision")
	ErrCodeStoreOutOfGas     = errors.New("code store out of gas")
	ErrMaxCodeSizeExceeded   = errors.New("max code size exceeded")
	ErrInvalidContractPrefix = errors.New("invalid contract prefix")
	ErrWriteProtection       = errors.New("write protection")
	ErrReturnDataOutOfBounds = errors.New("return data out of bounds")
	ErrExecutionReverted     = errors.New("execution reverted")
	ErrHalt                  = errors.New("halt")
)
