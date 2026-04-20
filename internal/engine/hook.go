package engine

import "math/big"

// HookType identifies the category of execution event a hook subscribes to.
type HookType int

const (
	// HookTypeOpcode fires before/after any opcode execution.
	HookTypeOpcode HookType = iota
	// HookTypeExternalCall fires on CALL opcode.
	HookTypeExternalCall
	// HookTypeDelegateCall fires on DELEGATECALL opcode.
	HookTypeDelegateCall
	// HookTypeStaticCall fires on STATICCALL opcode.
	HookTypeStaticCall
	// HookTypeCallCode fires on CALLCODE opcode.
	HookTypeCallCode
	// HookTypeCreate fires on CREATE/CREATE2 opcodes.
	HookTypeCreate
	// HookTypeStorageRead fires on SLOAD.
	HookTypeStorageRead
	// HookTypeStorageWrite fires on SSTORE.
	HookTypeStorageWrite
	// HookTypeTransientLoad fires on TLOAD (Cancun+).
	HookTypeTransientLoad
	// HookTypeTransientStore fires on TSTORE (Cancun+).
	HookTypeTransientStore
	// HookTypeMemoryRead fires on any memory read (MLOAD, CALLDATACOPY, etc.).
	HookTypeMemoryRead
	// HookTypeMemoryWrite fires on any memory write (MSTORE, MSTORE8, etc.).
	HookTypeMemoryWrite
	// HookTypeLog fires on LOG0-LOG4.
	HookTypeLog
	// HookTypeSelfDestruct fires on SELFDESTRUCT.
	HookTypeSelfDestruct
	// HookTypeReturn fires on RETURN/REVERT.
	HookTypeReturn
	// HookTypeStep fires on every instruction step (convenience for single-stepping).
	HookTypeStep
)

// HookAction determines what the engine should do after a hook fires.
type HookAction int

const (
	// ActionContinue resumes normal execution.
	ActionContinue HookAction = iota
	// ActionReplaceResult uses the result provided by the hook and skips the original operation.
	ActionReplaceResult
	// ActionHalt stops execution with the provided error/reason.
	ActionHalt
	// ActionRevert reverts the current call frame with the provided data.
	ActionRevert
)

// HookResult carries the decision and optional replacement data from a hook.
type HookResult struct {
	// Action tells the engine what to do next.
	Action HookAction
	// ResultData holds replacement data for ActionReplaceResult.
	// Interpretation depends on the hook type:
	//   Opcode:  []Word pushed to stack
	//   Call:    []byte return data + success flag
	//   Storage: Hash replacement value
	//   Memory:  []byte replacement data
	ResultData interface{}
	// Err is set when Action is ActionHalt.
	Err error
}

// OpcodeInfo provides context about the current opcode being executed.
type OpcodeInfo struct {
	// PC is the program counter.
	PC uint64
	// Op is the opcode byte.
	Op byte
	// GasRemaining is the gas left before executing this opcode.
	GasRemaining uint64
	// GasCost is the gas cost of this opcode (pre-execution).
	GasCost uint64
	// StackPopCount is the number of items this opcode pops.
	StackPopCount int
	// StackPushCount is the number of items this opcode pushes.
	StackPushCount int
}

// CallInfo provides context about an ongoing call operation.
type CallInfo struct {
	// Kind is the call type: CALL, CALLCODE, DELEGATECALL, STATICCALL.
	Kind HookType
	// Caller is the address initiating the call.
	Caller Address
	// Callee is the target address.
	Callee Address
	// Input is the call input data.
	Input []byte
	// Value is the ETH value transferred (zero for STATICCALL/DELEGATECALL).
	Value *big.Int
	// Gas is the gas allocated for the call.
	Gas uint64
	// CodeAddr is the address of the code to execute (differs from Callee for DELEGATECALL).
	CodeAddr Address
}

// StorageAccessInfo provides context about a storage access.
type StorageAccessInfo struct {
	// Addr is the contract address.
	Addr Address
	// Slot is the storage slot.
	Slot Hash
	// Value is the current value (read) or the value being written.
	Value Hash
	// IsWrite is true for SSTORE/TSTORE.
	IsWrite bool
	// IsCold is true if this is the first access to the slot in this transaction (Berlin+).
	IsCold bool
}

// MemoryAccessInfo provides context about a memory access.
type MemoryAccessInfo struct {
	// Offset is the memory offset.
	Offset uint64
	// Size is the number of bytes accessed.
	Size uint64
	// Data is the data being written (nil for reads).
	Data []byte
	// IsWrite is true for MSTORE/MSTORE8/MCOPY destination, etc.
	IsWrite bool
}

// HookContext provides hooks with read access to EVM state and metadata about the current event.
type HookContext struct {
	// State is a read-only view of the current EVM state.
	State ReadOnlyState
	// Opcode is set for HookTypeOpcode/HookTypeStep hooks.
	Opcode *OpcodeInfo
	// Call is set for call-related hooks.
	Call *CallInfo
	// Storage is set for storage access hooks.
	Storage *StorageAccessInfo
	// Memory is set for memory access hooks.
	Memory *MemoryAccessInfo
}

// Hook is the interface implemented by all hook types.
type Hook interface {
	// Type returns the hook type this hook subscribes to.
	Type() HookType
	// Fire is called when the subscribed event occurs.
	// The hook inspects the context and returns a HookResult to influence execution.
	Fire(ctx *HookContext) (*HookResult, error)
	// OneTime returns true if the hook should be removed after firing once.
	OneTime() bool
	// ID returns a unique identifier for this hook instance.
	ID() string
}

// HookRegistry manages the collection of active hooks.
type HookRegistry interface {
	// Register adds a hook to the registry.
	Register(hook Hook) error
	// Unregister removes a hook by its ID.
	Unregister(hookID string) error
	// HooksFor returns all registered hooks of the given type.
	HooksFor(hookType HookType) []Hook
	// Clear removes all hooks.
	Clear()
}

// HookCondition is a predicate that can be attached to a hook to limit when it fires.
type HookCondition func(ctx *HookContext) bool

// ConditionalHook wraps a hook with an optional condition.
type ConditionalHook struct {
	Inner     Hook
	Condition HookCondition
}

func (c *ConditionalHook) Type() HookType { return c.Inner.Type() }
func (c *ConditionalHook) Fire(ctx *HookContext) (*HookResult, error) {
	if c.Condition != nil && !c.Condition(ctx) {
		return &HookResult{Action: ActionContinue}, nil
	}
	return c.Inner.Fire(ctx)
}
func (c *ConditionalHook) OneTime() bool { return c.Inner.OneTime() }
func (c *ConditionalHook) ID() string    { return c.Inner.ID() }
