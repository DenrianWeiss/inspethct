package engine

import (
	"fmt"
	"sync"
)

// BreakpointReason explains why execution paused at a breakpoint.
type BreakpointReason string

const (
	ReasonOpcode       BreakpointReason = "opcode"
	ReasonCall         BreakpointReason = "call"
	ReasonStorageRead  BreakpointReason = "storage_read"
	ReasonStorageWrite BreakpointReason = "storage_write"
	ReasonMemoryRead   BreakpointReason = "memory_read"
	ReasonMemoryWrite  BreakpointReason = "memory_write"
	ReasonLog          BreakpointReason = "log"
	ReasonSelfDestruct BreakpointReason = "self_destruct"
	ReasonReturn       BreakpointReason = "return"
	ReasonStep         BreakpointReason = "step"
	ReasonException    BreakpointReason = "exception"
	ReasonManual       BreakpointReason = "manual"
)

// BreakpointCondition defines a predicate evaluated to decide if a breakpoint should fire.
type BreakpointCondition struct {
	// MinGas only fires when remaining gas is >= this value (0 = ignore).
	MinGas uint64
	// MaxGas only fires when remaining gas is <= this value (0 = ignore).
	MaxGas uint64
	// MinDepth only fires when call depth is >= this value (0 = ignore).
	MinDepth int
	// MaxDepth only fires when call depth is <= this value (-1 = ignore).
	MaxDepth int
	// OpFilter only fires for these opcodes (nil/empty = all).
	OpFilter []byte
	// AddrFilter only fires when the executing contract is one of these (nil/empty = all).
	AddrFilter []Address
	// SlotFilter only fires for storage hooks when the slot is in this set (nil/empty = all).
	SlotFilter []Hash
	// Custom is an optional user-defined condition.
	Custom HookCondition
}

// Match evaluates the condition against the current hook context.
func (bc *BreakpointCondition) Match(ctx *HookContext) bool {
	st := ctx.State
	if bc.MinGas > 0 && st.GasRemaining() < bc.MinGas {
		return false
	}
	if bc.MaxGas > 0 && st.GasRemaining() > bc.MaxGas {
		return false
	}
	if bc.MinDepth > 0 && st.CallDepth() < bc.MinDepth {
		return false
	}
	if bc.MaxDepth >= 0 && st.CallDepth() > bc.MaxDepth {
		return false
	}
	if len(bc.OpFilter) > 0 && ctx.Opcode != nil {
		found := false
		for _, op := range bc.OpFilter {
			if op == ctx.Opcode.Op {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if len(bc.AddrFilter) > 0 {
		found := false
		addr := st.ContractAddress()
		for _, a := range bc.AddrFilter {
			if a == addr {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if len(bc.SlotFilter) > 0 && ctx.Storage != nil {
		found := false
		for _, s := range bc.SlotFilter {
			if s == ctx.Storage.Slot {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if bc.Custom != nil && !bc.Custom(ctx) {
		return false
	}
	return true
}

// Breakpoint is a special hook that pauses execution and delegates control to a debugger.
type Breakpoint struct {
	id        string
	hookType  HookType
	condition *BreakpointCondition
	onHit     func(ctx *BreakpointContext) (BreakpointAction, error)
	once      bool
}

// BreakpointAction determines what happens after a breakpoint is hit and handled.
type BreakpointAction int

const (
	// BreakpointContinue resumes execution normally.
	BreakpointContinue BreakpointAction = iota
	// BreakpointStepInto executes the next instruction and pauses again.
	BreakpointStepInto
	// BreakpointStepOver executes until the next instruction at the same or lower call depth.
	BreakpointStepOver
	// BreakpointStepOut executes until returning from the current call frame.
	BreakpointStepOut
	// BreakpointReplaceResult uses the provided replacement and continues.
	BreakpointReplaceResult
)

// BreakpointContext provides the debugger with full context when a breakpoint is hit.
type BreakpointContext struct {
	// ID is the breakpoint identifier.
	ID string
	// Reason describes why the breakpoint fired.
	Reason BreakpointReason
	// HookCtx is the underlying hook context.
	HookCtx *HookContext
	// State is a mutable reference to the current EVM state.
	// The debugger can read and write this state before resuming.
	State EVMState
	// Result is pre-populated with the operation's natural result.
	// The debugger may modify this to replace the outcome.
	Result *HookResult
}

// BreakpointOption configures a Breakpoint.
type BreakpointOption func(*Breakpoint)

// WithCondition attaches a condition to the breakpoint.
func WithCondition(c *BreakpointCondition) BreakpointOption {
	return func(b *Breakpoint) { b.condition = c }
}

// WithOneTime makes the breakpoint fire only once.
func WithOneTime() BreakpointOption {
	return func(b *Breakpoint) { b.once = true }
}

// WithHandler sets the callback invoked when the breakpoint is hit.
func WithHandler(h func(ctx *BreakpointContext) (BreakpointAction, error)) BreakpointOption {
	return func(b *Breakpoint) { b.onHit = h }
}

// NewBreakpoint creates a new breakpoint for the given hook type.
func NewBreakpoint(id string, ht HookType, opts ...BreakpointOption) *Breakpoint {
	bp := &Breakpoint{
		id:       id,
		hookType: ht,
		once:     false,
	}
	for _, opt := range opts {
		opt(bp)
	}
	return bp
}

// Type returns the hook type.
func (b *Breakpoint) Type() HookType { return b.hookType }

// OneTime returns true if this breakpoint should fire only once.
func (b *Breakpoint) OneTime() bool { return b.once }

// ID returns the breakpoint identifier.
func (b *Breakpoint) ID() string { return b.id }

// Fire evaluates the condition and, if matched, invokes the onHit handler.
// If no handler is set, it returns ActionHalt so execution pauses.
func (b *Breakpoint) Fire(ctx *HookContext) (*HookResult, error) {
	if b.condition != nil && !b.condition.Match(ctx) {
		return &HookResult{Action: ActionContinue}, nil
	}

	if b.onHit == nil {
		return &HookResult{Action: ActionHalt, Err: fmt.Errorf("breakpoint %s hit", b.id)}, nil
	}

	// Default to continuing; handler may request replacement or further stepping.
	bpCtx := &BreakpointContext{
		ID:      b.id,
		Reason:  breakpointReasonForType(b.hookType),
		HookCtx: ctx,
		Result:  &HookResult{Action: ActionContinue},
	}

	action, err := b.onHit(bpCtx)
	if err != nil {
		return nil, err
	}

	switch action {
	case BreakpointContinue:
		return bpCtx.Result, nil
	case BreakpointReplaceResult:
		bpCtx.Result.Action = ActionReplaceResult
		return bpCtx.Result, nil
	case BreakpointStepInto, BreakpointStepOver, BreakpointStepOut:
		// These are handled by the debugger wrapping the execution loop.
		// For now, return halt with a sentinel so the engine knows to re-enter.
		return &HookResult{Action: ActionHalt, Err: fmt.Errorf("stepping action %d requested", action)}, nil
	default:
		return bpCtx.Result, nil
	}
}

func breakpointReasonForType(ht HookType) BreakpointReason {
	switch ht {
	case HookTypeOpcode, HookTypeStep:
		return ReasonOpcode
	case HookTypeExternalCall, HookTypeDelegateCall, HookTypeStaticCall, HookTypeCallCode, HookTypeCreate:
		return ReasonCall
	case HookTypeStorageRead:
		return ReasonStorageRead
	case HookTypeStorageWrite:
		return ReasonStorageWrite
	case HookTypeTransientLoad:
		return ReasonStorageRead
	case HookTypeTransientStore:
		return ReasonStorageWrite
	case HookTypeMemoryRead:
		return ReasonMemoryRead
	case HookTypeMemoryWrite:
		return ReasonMemoryWrite
	case HookTypeLog:
		return ReasonLog
	case HookTypeSelfDestruct:
		return ReasonSelfDestruct
	case HookTypeReturn:
		return ReasonReturn
	default:
		return ReasonManual
	}
}

// BreakpointManager manages active breakpoints and provides high-level debugging controls.
type BreakpointManager struct {
	mu          sync.RWMutex
	breakpoints map[string]*Breakpoint
	registry    HookRegistry

	stepping   bool
	stepTarget BreakpointAction
	stepDepth  int
	stepCallID uint64
}

// NewBreakpointManager creates a breakpoint manager backed by a hook registry.
func NewBreakpointManager(registry HookRegistry) *BreakpointManager {
	return &BreakpointManager{
		breakpoints: make(map[string]*Breakpoint),
		registry:    registry,
	}
}

// SetBreakpoint registers a new breakpoint.
func (bm *BreakpointManager) SetBreakpoint(bp *Breakpoint) error {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	bm.breakpoints[bp.ID()] = bp
	return bm.registry.Register(bp)
}

// RemoveBreakpoint unregisters a breakpoint by ID.
func (bm *BreakpointManager) RemoveBreakpoint(id string) error {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	delete(bm.breakpoints, id)
	return bm.registry.Unregister(id)
}

// Clear removes all breakpoints.
func (bm *BreakpointManager) Clear() {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	bm.breakpoints = make(map[string]*Breakpoint)
	bm.registry.Clear()
}

// Breakpoints returns a snapshot of current breakpoints.
func (bm *BreakpointManager) Breakpoints() []*Breakpoint {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	out := make([]*Breakpoint, 0, len(bm.breakpoints))
	for _, bp := range bm.breakpoints {
		out = append(out, bp)
	}
	return out
}

// StepInto configures the engine to pause at the very next instruction.
func (bm *BreakpointManager) StepInto() {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	bm.stepping = true
	bm.stepTarget = BreakpointStepInto
}

// StepOver configures the engine to pause at the next instruction in the same or shallower frame.
func (bm *BreakpointManager) StepOver(currentDepth int) {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	bm.stepping = true
	bm.stepTarget = BreakpointStepOver
	bm.stepDepth = currentDepth
}

// StepOut configures the engine to pause when returning from the current call frame.
func (bm *BreakpointManager) StepOut(currentDepth int) {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	bm.stepping = true
	bm.stepTarget = BreakpointStepOut
	bm.stepDepth = currentDepth
}

// IsStepping returns true if the manager is currently in a stepping mode.
func (bm *BreakpointManager) IsStepping() bool {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	return bm.stepping
}

// ResolveStepping marks the current step as resolved, disabling stepping mode.
func (bm *BreakpointManager) ResolveStepping() {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	bm.stepping = false
}
