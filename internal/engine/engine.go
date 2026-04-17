package engine

import "math/big"

// ExecutionConfig configures an EVM execution run.
type ExecutionConfig struct {
	// Fork specifies which hard-fork rules to apply.
	Fork Fork
	// Precompiles overrides the active precompile registry for this execution.
	Precompiles *PrecompileRegistry
	// WarmAddresses adds custom addresses to the initial warm set.
	WarmAddresses []Address
	// GasLimit is the maximum gas for this execution.
	GasLimit uint64
	// Value is the ETH value transferred in the message.
	Value *big.Int
	// Input is the call data / transaction input.
	Input []byte
	// Origin is the transaction origin address.
	Origin Address
	// Caller is the immediate caller address.
	Caller Address
	// ContractAddress is the address of the contract being executed.
	ContractAddress Address
	// Code is the bytecode to execute.
	Code []byte
	// CodeHash is the hash of the code.
	CodeHash Hash
	// BlockContext provides block-level information.
	BlockContext BlockContext
	// TxContext provides transaction-level information.
	TxContext TxContext
	// State provides the world state.
	State MutableAccountState
	// Storage provides persistent storage access.
	Storage Storage
	// TransientStorage provides transient storage access (Cancun+).
	TransientStorage TransientStorage
	// AccessList provides the initial access list (Berlin+).
	AccessList AccessList
	// Hooks is an optional hook registry.
	Hooks HookRegistry
	// CollectTrace enables step-by-step trace collection.
	CollectTrace bool
	// CallDepth is the current call depth (0 for top-level).
	CallDepth int
	// IsStatic is true if this is a STATICCALL context.
	IsStatic bool
}

// Engine is the core EVM execution engine.
type Engine interface {
	// Run executes the EVM with the given configuration.
	Run(cfg *ExecutionConfig) (*ExecutionResult, error)
	// RunWithState executes using an already initialized EVMState.
	RunWithState(state EVMState, code []byte) (*ExecutionResult, error)
	// NewState initializes a fresh EVMState from an ExecutionConfig.
	NewState(cfg *ExecutionConfig) (EVMState, error)
	// SupportedForks returns the list of forks this engine supports.
	SupportedForks() []Fork
	// ValidateConfig checks an ExecutionConfig for errors before running.
	ValidateConfig(cfg *ExecutionConfig) error
}

// Debugger provides high-level debugging controls over an Engine execution.
type Debugger interface {
	// Attach binds the debugger to an engine.
	Attach(engine Engine) error
	// Detach removes the debugger from the engine.
	Detach() error
	// SetBreakpoint adds a breakpoint.
	SetBreakpoint(bp *Breakpoint) error
	// RemoveBreakpoint removes a breakpoint by ID.
	RemoveBreakpoint(id string) error
	// Continue resumes execution until the next breakpoint or completion.
	Continue() (*ExecutionResult, error)
	// StepInto executes the next instruction and pauses.
	StepInto() (*ExecutionResult, error)
	// StepOver executes until the next instruction at the same or lower depth.
	StepOver() (*ExecutionResult, error)
	// StepOut executes until returning from the current call frame.
	StepOut() (*ExecutionResult, error)
	// Pause interrupts a running execution.
	Pause() error
	// State returns the current mutable EVM state.
	State() (EVMState, error)
	// Result returns the current execution result so far.
	Result() (*ExecutionResult, error)
}
