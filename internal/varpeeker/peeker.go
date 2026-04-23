package varpeeker

import (
	"inspethct/internal/engine"
	"inspethct/internal/srcmap"
)

// Peeker bundles the static index with an optional runtime tracker so a
// caller can produce a Snapshot at any pause point with a single call.
type Peeker struct {
	Index   *srcmap.Index
	Tracker *Tracker
}

// New constructs a Peeker. Tracker is optional; when nil only static and
// stack-based heuristics are used.
func New(index *srcmap.Index, tracker *Tracker) *Peeker {
	return &Peeker{Index: index, Tracker: tracker}
}

// Snapshot peeks all variable categories at the current PC.
//
// codeAddr is the address whose runtime bytecode is currently executing
// (used for immutable resolution); contractAddr is the storage owner
// (the persistent storage account, may differ under DELEGATECALL).
// runtimeCode is the runtime bytecode slice (used to read immutable
// values). Pass nil to skip immutables.
func (p *Peeker) Snapshot(state engine.ReadOnlyState, contractAddr engine.Address, runtimeCode []byte) Snapshot {
	if p == nil || p.Index == nil || state == nil {
		return Snapshot{}
	}
	pc := state.PC()
	snap := Snapshot{PC: pc}
	if mapping, ok := p.Index.InstructionAtPC(pc); ok {
		if fn := findEnclosingFunction(p.Index, mapping); fn != nil {
			snap.Function = fn.Name
		}
	}
	snap.Contract = p.Index.Build.ContractName
	snap.Locals = peekLocals(p.Index, p.Tracker, state, pc)
	snap.Storage = peekStorageVars(p.Index, state, contractAddr, srcmap.StorageScopePersistent)
	snap.Transient = peekStorageVars(p.Index, state, contractAddr, srcmap.StorageScopeTransient)
	if len(runtimeCode) > 0 {
		snap.Immutables = peekImmutables(p.Index, runtimeCode)
	}
	return snap
}
