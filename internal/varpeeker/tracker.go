package varpeeker

import (
	"fmt"
	"sync"

	"inspethct/internal/engine"
	"inspethct/internal/srcmap"
)

// MemoryRegion describes a contiguous memory write attributed to a
// particular source range.
type MemoryRegion struct {
	Offset    uint64
	Size      uint64
	PC        uint64
	StepIndex int
	Src       srcmap.SourceRange
}

// Tracker observes per-step memory writes and attributes them to the
// source range of the writing instruction. It implements the
// engine.Hook contract for HookTypeMemoryWrite. Callers register one
// Tracker per ReplaySession and consult it when building Snapshots.
//
// Tracker is safe for sequential use from a single replay goroutine; it
// is not safe for concurrent reads while writes are happening.
type Tracker struct {
	id         string
	index      *srcmap.Index
	mu         sync.Mutex
	stepIndex  int
	bySource   map[srcmap.SourceRange]MemoryRegion
	byOffset   map[uint64]MemoryRegion
	allRegions []MemoryRegion
}

// NewTracker builds a Tracker bound to a source-map index.
func NewTracker(index *srcmap.Index) *Tracker {
	return &Tracker{
		id:       fmt.Sprintf("varpeeker-tracker-%p", index),
		index:    index,
		bySource: make(map[srcmap.SourceRange]MemoryRegion),
		byOffset: make(map[uint64]MemoryRegion),
	}
}

// Type implements engine.Hook.
func (t *Tracker) Type() engine.HookType { return engine.HookTypeMemoryWrite }

// Fire implements engine.Hook. Records the memory region tied to the
// PC's source mapping.
func (t *Tracker) Fire(ctx *engine.HookContext) (*engine.HookResult, error) {
	if t == nil || ctx == nil || ctx.Memory == nil || ctx.State == nil {
		return &engine.HookResult{Action: engine.ActionContinue}, nil
	}
	if !ctx.Memory.IsWrite || ctx.Memory.Size == 0 {
		return &engine.HookResult{Action: engine.ActionContinue}, nil
	}
	t.mu.Lock()
	t.stepIndex++
	step := t.stepIndex
	pc := ctx.State.PC()
	t.mu.Unlock()

	var src srcmap.SourceRange
	src.SourceID = -1
	if t.index != nil {
		if mapping, ok := t.index.InstructionAtPC(pc); ok {
			src = mapping.Source
		}
	}
	region := MemoryRegion{
		Offset:    ctx.Memory.Offset,
		Size:      ctx.Memory.Size,
		PC:        pc,
		StepIndex: step,
		Src:       src,
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if src.SourceID >= 0 {
		t.bySource[src] = region
	}
	t.byOffset[region.Offset] = region
	t.allRegions = append(t.allRegions, region)
	const maxRetained = 4096
	if len(t.allRegions) > maxRetained {
		drop := len(t.allRegions) - maxRetained
		t.allRegions = append([]MemoryRegion(nil), t.allRegions[drop:]...)
	}
	return &engine.HookResult{Action: engine.ActionContinue}, nil
}

// OneTime implements engine.Hook.
func (t *Tracker) OneTime() bool { return false }

// ID implements engine.Hook.
func (t *Tracker) ID() string { return t.id }

// MemoryRegionForSource returns the latest memory region whose writing
// instruction's source range contains src (or matches it).
func (t *Tracker) MemoryRegionForSource(src srcmap.SourceRange) (MemoryRegion, bool) {
	if t == nil {
		return MemoryRegion{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if region, ok := t.bySource[src]; ok {
		return region, true
	}
	// Fallback: any recorded write whose Src contains src (so a
	// declaration inside a larger expression's source range still
	// matches when only the outer expression is recorded).
	var best MemoryRegion
	found := false
	for recordedSrc, region := range t.bySource {
		if recordedSrc.SourceID != src.SourceID {
			continue
		}
		if recordedSrc.Start > src.Start || recordedSrc.End() < src.End() {
			continue
		}
		if !found || region.StepIndex > best.StepIndex {
			best = region
			found = true
		}
	}
	return best, found
}

// MemoryRegionAt returns the most recent memory region exactly at the
// given offset (typically used to confirm a stack-derived memory
// pointer).
func (t *Tracker) MemoryRegionAt(offset uint64) (MemoryRegion, bool) {
	if t == nil {
		return MemoryRegion{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	region, ok := t.byOffset[offset]
	return region, ok
}

// Reset clears all recorded regions. Intended to be called between
// replay runs of the same session.
func (t *Tracker) Reset() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stepIndex = 0
	t.bySource = make(map[srcmap.SourceRange]MemoryRegion)
	t.byOffset = make(map[uint64]MemoryRegion)
	t.allRegions = nil
}

// Regions returns a copy of all recorded regions in step order.
func (t *Tracker) Regions() []MemoryRegion {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]MemoryRegion, len(t.allRegions))
	copy(out, t.allRegions)
	return out
}
