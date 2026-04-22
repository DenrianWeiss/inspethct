package jsonrpc

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	gethabi "github.com/ethereum/go-ethereum/accounts/abi"

	"inspethct/internal/contractmeta"
	"inspethct/internal/engine"
	"inspethct/internal/forkengine"
	"inspethct/internal/forkengine/upstream"
	"inspethct/internal/srcmap"
)

var errReplayPause = errors.New("jsonrpc: replay pause")

type ReplayPause struct {
	Reason            string                 `json:"reason"`
	Breakpoint        string                 `json:"breakpoint,omitempty"`
	StepIndex         int                    `json:"stepIndex"`
	ContractAddress   string                 `json:"contractAddress"`
	CodeAddress       string                 `json:"codeAddress"`
	Memory            string                 `json:"memory"`
	MemoryTruncated   bool                   `json:"memoryTruncated,omitempty"`
	MemorySize        int                    `json:"memorySize"`
	MemoryRegions     []MemoryRegionInfo     `json:"memoryRegions,omitempty"`
	FreeMemoryPointer uint64                 `json:"freeMemoryPointer,omitempty"`
	Stack             []string               `json:"stack,omitempty"`
	Locals            []LocalVariable        `json:"locals,omitempty"`
	Source            map[string]any         `json:"source,omitempty"`
	Storage           []VariableValue        `json:"storage,omitempty"`
	Transient         []VariableValue        `json:"transient,omitempty"`
	CallAccess        *CallAccess            `json:"callAccess,omitempty"`
	StorageAccess     *StorageAccess         `json:"storageAccess,omitempty"`
	MemoryAccess      *MemoryAccess          `json:"memoryAccess,omitempty"`
	Metadata          any                    `json:"metadata,omitempty"`
	Step              forkengineTracePayload `json:"step"`
}

// MemoryRegionInfo describes a contiguous range of EVM memory with a
// Solidity-aware label (scratch space, free memory pointer, zero slot,
// heap, temporary).
type MemoryRegionInfo struct {
	Kind   string `json:"kind"`
	Label  string `json:"label"`
	Offset uint64 `json:"offset"`
	Length uint64 `json:"length"`
}

// LocalVariable describes a local declared in scope at the current PC. The
// Value field is best-effort: it is populated only when the variable can be
// matched to a stack slot with high confidence (e.g. value-type parameter
// without intervening branches). Otherwise Value is empty and Confidence is
// "unavailable".
type LocalVariable struct {
	Name            string `json:"name"`
	Type            string `json:"type"`
	StorageLocation string `json:"storageLocation,omitempty"`
	Kind            string `json:"kind"` // parameter | return | local
	DeclaredAtLine  int    `json:"declaredAtLine,omitempty"`
	Value           string `json:"value,omitempty"`
	Confidence      string `json:"confidence,omitempty"` // resolved | unavailable
	StackDepth      int    `json:"stackDepth,omitempty"`
}

// pauseInlineMemoryCap caps the memory blob that ships in pause payloads. The
// plugin uses gdb.readMemory for paged access beyond this size to keep the
// step latency low for contracts with large memory footprints.
const pauseInlineMemoryCap = 4096

type forkengineTracePayload struct {
	PC           uint64 `json:"pc"`
	Op           string `json:"op"`
	Depth        int    `json:"depth"`
	GasRemaining uint64 `json:"gasRemaining"`
	GasCost      uint64 `json:"gasCost"`
}

type VariableValue struct {
	Scope string `json:"scope"`
	Name  string `json:"name"`
	Slot  string `json:"slot"`
	Value string `json:"value"`
	Type  string `json:"type,omitempty"`
}

type DebugBreakpoint struct {
	ID            string   `json:"id"`
	Kind          string   `json:"kind"`
	Display       string   `json:"display"`
	CodeAddress   string   `json:"codeAddress,omitempty"`
	SourceName    string   `json:"sourceName,omitempty"`
	Line          int      `json:"line,omitempty"`
	Column        int      `json:"column,omitempty"`
	PCs           []uint64 `json:"pcs,omitempty"`
	Signature     string   `json:"signature,omitempty"`
	Address       string   `json:"address,omitempty"`
	Slot          string   `json:"slot,omitempty"`
	SlotLabel     string   `json:"slotLabel,omitempty"`
	Offset        uint64   `json:"offset,omitempty"`
	Size          uint64   `json:"size,omitempty"`
	Access        string   `json:"access,omitempty"`
	selector      []byte
	addressFilter *engine.Address
	slotFilter    *engine.Hash
}

type CallAccess struct {
	Kind     string `json:"kind"`
	Caller   string `json:"caller"`
	Callee   string `json:"callee"`
	CodeAddr string `json:"codeAddress"`
	Input    string `json:"input"`
	Value    string `json:"value"`
	Gas      uint64 `json:"gas"`
}

type StorageAccess struct {
	Address string `json:"address"`
	Scope   string `json:"scope"`
	Slot    string `json:"slot"`
	Value   string `json:"value"`
	IsWrite bool   `json:"isWrite"`
}

type MemoryAccess struct {
	Offset  uint64 `json:"offset"`
	Size    uint64 `json:"size"`
	IsWrite bool   `json:"isWrite"`
	Data    string `json:"data"`
}

type ReplayMutation struct {
	Kind      string `json:"kind"`
	StepIndex int    `json:"stepIndex"`
	Address   string `json:"address,omitempty"`
	Scope     string `json:"scope,omitempty"`
	Slot      string `json:"slot,omitempty"`
	Value     string `json:"value,omitempty"`
	Offset    uint64 `json:"offset,omitempty"`
	Data      string `json:"data,omitempty"`
}

type sourceBundleRequest struct {
	Kind             string `json:"kind"`
	ProjectRoot      string `json:"projectRoot"`
	StandardJSONPath string `json:"standardJsonPath"`
	ABIPath          string `json:"abiPath"`
	SourceName       string `json:"sourceName"`
	ContractName     string `json:"contractName"`
	Runtime          *bool  `json:"runtime"`
	Address          string `json:"address"`
	CodeAddress      string `json:"codeAddress"`
	APIBase          string `json:"apiBase"`
	APIKey           string `json:"apiKey"`
	RPCURL           string `json:"rpcUrl"`
	ChainID          string `json:"chainId"`
}

type sourceBreakpointRequest struct {
	ID         string `json:"id"`
	Address    string `json:"address"`
	SourceName string `json:"sourceName"`
	Line       int    `json:"line"`
	Column     int    `json:"column"`
}

type functionBreakpointRequest struct {
	ID        string `json:"id"`
	Signature string `json:"signature"`
}

type callBreakpointRequest struct {
	ID        string `json:"id"`
	Address   string `json:"address"`
	Signature string `json:"signature"`
}

type storageBreakpointRequest struct {
	ID      string `json:"id"`
	Address string `json:"address"`
	Slot    string `json:"slot"`
	Name    string `json:"name"`
	Access  string `json:"access"`
}

type memoryBreakpointRequest struct {
	ID         string `json:"id"`
	Address    string `json:"address"`
	Offset     uint64 `json:"offset"`
	Size       uint64 `json:"size"`
	Access     string `json:"access"`
	SourceName string `json:"sourceName"`
	Line       int    `json:"line"`
	Column     int    `json:"column"`
}

type writeStorageRequest struct {
	Address string `json:"address"`
	Scope   string `json:"scope"`
	Slot    string `json:"slot"`
	Value   string `json:"value"`
}

type writeMemoryRequest struct {
	Offset uint64 `json:"offset"`
	Data   string `json:"data"`
}

type deleteBreakpointRequest struct {
	ID string `json:"id"`
}

type breakpointAccessMode string

const (
	accessModeRead  breakpointAccessMode = "read"
	accessModeWrite breakpointAccessMode = "write"
	accessModeBoth  breakpointAccessMode = "rw"
)

type debugStepHook struct {
	session      *ReplaySession
	continueMode bool
	seen         int
}

type debugAccessHook struct {
	session *ReplaySession
	kind    engine.HookType
	hookID  string
	reason  string
	matcher func(*engine.HookContext, DebugBreakpoint) bool
	builder func(*engine.HookContext) any
}

func (server *Server) replaySession(ctx context.Context, session *ReplaySession, continueMode bool) *respError {
	prepared, targetAddr, codeAddr, rpcErr := server.prepareSession(ctx, session)
	if rpcErr != nil {
		return rpcErr
	}
	session.TargetAddr = targetAddr
	session.CodeAddr = codeAddr
	session.RootInput = append(session.RootInput[:0], prepared.Config.Input...)
	session.LastStep = -1
	session.PendingPause = nil
	session.Current = nil

	debugHooks := engine.NewSimpleHookRegistry()
	_ = debugHooks.Register(&debugStepHook{session: session, continueMode: continueMode})
	_ = debugHooks.Register(newCallHook(session, engine.HookTypeExternalCall, "jsonrpc-call"))
	_ = debugHooks.Register(newCallHook(session, engine.HookTypeDelegateCall, "jsonrpc-delegatecall"))
	_ = debugHooks.Register(newCallHook(session, engine.HookTypeStaticCall, "jsonrpc-staticcall"))
	_ = debugHooks.Register(newCallHook(session, engine.HookTypeCallCode, "jsonrpc-callcode"))
	_ = debugHooks.Register(newStorageHook(session, engine.HookTypeStorageRead, engine.HookTypeTransientLoad, "jsonrpc-storage-read", false))
	_ = debugHooks.Register(newStorageHook(session, engine.HookTypeStorageWrite, engine.HookTypeTransientStore, "jsonrpc-storage-write", true))
	_ = debugHooks.Register(newMemoryHook(session, engine.HookTypeMemoryRead, "jsonrpc-memory-read", false))
	_ = debugHooks.Register(newMemoryHook(session, engine.HookTypeMemoryWrite, "jsonrpc-memory-write", true))
	prepared.Config.Hooks = mergeHookRegistries(prepared.Config.Hooks, debugHooks)
	result, execErr := server.engine.ExecutePreparedCall(prepared)
	if execErr != nil && !errors.Is(execErr, errReplayPause) && result == nil {
		return internalError(execErr)
	}
	if session.PendingPause != nil {
		session.Current = session.PendingPause
		session.Position = session.PendingPause.StepIndex
		session.Done = false
		session.Result = nil
		if parsed, ok := parseAddress(session.PendingPause.CodeAddress); ok {
			session.CodeAddr = &parsed
		}
		return nil
	}
	if execErr != nil && !errors.Is(execErr, errReplayPause) {
		return internalError(execErr)
	}
	session.Current = nil
	session.Position = session.LastStep
	session.Done = true
	session.Result = result
	return nil
}

func (server *Server) prepareSession(ctx context.Context, session *ReplaySession) (*forkengine.PreparedCall, *engine.Address, *engine.Address, *respError) {
	switch session.Kind {
	case "call":
		if session.CallRequest == nil {
			return nil, nil, nil, &respError{Code: -32602, Message: "call session is missing request parameters"}
		}
		prepared, err := server.engine.PrepareCall(ctx, *session.CallRequest)
		if err != nil {
			return nil, nil, nil, internalError(err)
		}
		target := session.CallRequest.To
		code := session.CallRequest.To
		return prepared, &target, &code, nil
	default:
		prepared, tx, receipt, err := server.engine.PrepareReplay(ctx, session.TxHash)
		if err != nil {
			return nil, nil, nil, internalError(err)
		}
		var target *engine.Address
		if tx.To != nil {
			addr := *tx.To
			target = &addr
		} else if receipt.ContractAddress != nil {
			addr := *receipt.ContractAddress
			target = &addr
		}
		code := target
		if session.CodeAddr != nil {
			code = session.CodeAddr
		}
		return prepared, target, code, nil
	}
}

func (hook *debugStepHook) registry() engine.HookRegistry {
	registry := engine.NewSimpleHookRegistry()
	_ = registry.Register(hook)
	return registry
}

func (hook *debugStepHook) Type() engine.HookType { return engine.HookTypeStep }
func (hook *debugStepHook) OneTime() bool         { return false }
func (hook *debugStepHook) ID() string            { return "jsonrpc-gdb-step" }

func (hook *debugStepHook) Fire(ctx *engine.HookContext) (*engine.HookResult, error) {
	stepIndex := hook.seen
	hook.seen++
	hook.session.LastStep = stepIndex
	hook.applyMutations(ctx, stepIndex)
	if stepIndex <= hook.session.Position {
		return &engine.HookResult{Action: engine.ActionContinue}, nil
	}
	if !hook.continueMode && stepIndex == hook.session.Position+1 {
		hook.session.PendingPause = capturePause(hook.session, ctx, stepIndex, "step", "")
		return &engine.HookResult{Action: engine.ActionHalt, Err: errReplayPause}, nil
	}
	if breakpoint := hook.session.matchRootFunctionBreakpoint(ctx); breakpoint != nil {
		hook.session.PendingPause = capturePause(hook.session, ctx, stepIndex, "function_breakpoint", breakpoint.Display)
		return &engine.HookResult{Action: engine.ActionHalt, Err: errReplayPause}, nil
	}
	if breakpoint := hook.session.matchSourceBreakpoint(ctx); breakpoint != nil {
		hook.session.PendingPause = capturePause(hook.session, ctx, stepIndex, "source_breakpoint", breakpoint.Display)
		return &engine.HookResult{Action: engine.ActionHalt, Err: errReplayPause}, nil
	}
	return &engine.HookResult{Action: engine.ActionContinue}, nil
}

func (hook *debugStepHook) applyMutations(ctx *engine.HookContext, stepIndex int) {
	if ctx == nil || ctx.State == nil {
		return
	}
	state, ok := extractMutableState(ctx)
	if !ok {
		return
	}
	for _, mutation := range hook.session.Mutations {
		if mutation.StepIndex != stepIndex {
			continue
		}
		switch mutation.Kind {
		case "storage":
			addr, ok := parseAddress(mutation.Address)
			if !ok {
				continue
			}
			slot, err := decodeHashString(mutation.Slot)
			if err != nil {
				continue
			}
			value, err := decodeHashString(mutation.Value)
			if err != nil {
				continue
			}
			if mutation.Scope == string(srcmap.StorageScopeTransient) {
				state.TransientStorage().Set(addr, slot, value)
			} else {
				state.Storage().Set(addr, slot, value)
			}
		case "memory":
			data, err := decodeBytes(mutation.Data)
			if err != nil {
				continue
			}
			_ = state.Memory().Set(mutation.Offset, data)
		}
	}
}

func newCallHook(session *ReplaySession, hookType engine.HookType, hookID string) *debugAccessHook {
	return &debugAccessHook{
		session: session,
		kind:    hookType,
		hookID:  hookID,
		reason:  "call_breakpoint",
		matcher: func(ctx *engine.HookContext, breakpoint DebugBreakpoint) bool {
			if breakpoint.Kind != "call" && breakpoint.Kind != "function" {
				return false
			}
			if ctx == nil || ctx.Call == nil {
				return false
			}
			if breakpoint.addressFilter != nil && *breakpoint.addressFilter != ctx.Call.Callee && *breakpoint.addressFilter != ctx.Call.CodeAddr {
				return false
			}
			if len(breakpoint.selector) > 0 && !selectorMatches(ctx.Call.Input, breakpoint.selector) {
				return false
			}
			return true
		},
		builder: func(ctx *engine.HookContext) any {
			if ctx == nil || ctx.Call == nil {
				return nil
			}
			value := "0"
			if ctx.Call.Value != nil {
				value = ctx.Call.Value.String()
			}
			return &CallAccess{Kind: hookTypeLabel(hookType), Caller: addressHex(ctx.Call.Caller), Callee: addressHex(ctx.Call.Callee), CodeAddr: addressHex(ctx.Call.CodeAddr), Input: encodeBytes(ctx.Call.Input), Value: value, Gas: ctx.Call.Gas}
		},
	}
}

func newStorageHook(session *ReplaySession, hookType engine.HookType, alternate engine.HookType, hookID string, write bool) *debugAccessHook {
	return &debugAccessHook{
		session: session,
		kind:    hookType,
		hookID:  hookID,
		reason:  map[bool]string{true: "storage_write_breakpoint", false: "storage_read_breakpoint"}[write],
		matcher: func(ctx *engine.HookContext, breakpoint DebugBreakpoint) bool {
			if breakpoint.Kind != "storage" || ctx == nil || ctx.Storage == nil {
				return false
			}
			if breakpoint.addressFilter != nil && *breakpoint.addressFilter != ctx.Storage.Addr {
				return false
			}
			if breakpoint.Access == string(accessModeRead) && ctx.Storage.IsWrite {
				return false
			}
			if breakpoint.Access == string(accessModeWrite) && !ctx.Storage.IsWrite {
				return false
			}
			if breakpoint.slotFilter != nil && *breakpoint.slotFilter != ctx.Storage.Slot {
				return false
			}
			return true
		},
		builder: func(ctx *engine.HookContext) any {
			if ctx == nil || ctx.Storage == nil {
				return nil
			}
			scope := string(srcmap.StorageScopePersistent)
			if hookType == engine.HookTypeTransientLoad || hookType == engine.HookTypeTransientStore || alternate == engine.HookTypeTransientLoad || alternate == engine.HookTypeTransientStore {
				scope = string(srcmap.StorageScopeTransient)
			}
			return &StorageAccess{Address: addressHex(ctx.Storage.Addr), Scope: scope, Slot: encodeHash(ctx.Storage.Slot), Value: encodeHash(ctx.Storage.Value), IsWrite: ctx.Storage.IsWrite}
		},
	}
}

func newMemoryHook(session *ReplaySession, hookType engine.HookType, hookID string, write bool) *debugAccessHook {
	return &debugAccessHook{
		session: session,
		kind:    hookType,
		hookID:  hookID,
		reason:  map[bool]string{true: "memory_write_breakpoint", false: "memory_read_breakpoint"}[write],
		matcher: func(ctx *engine.HookContext, breakpoint DebugBreakpoint) bool {
			if breakpoint.Kind != "memory" || ctx == nil || ctx.Memory == nil {
				return false
			}
			if breakpoint.CodeAddress != "" {
				if ctx.State == nil || breakpoint.CodeAddress != addressHex(ctx.State.ContractCodeAddr()) {
					return false
				}
			}
			if len(breakpoint.PCs) > 0 {
				if ctx.Opcode == nil || !containsPC(breakpoint.PCs, ctx.Opcode.PC) {
					return false
				}
			}
			if breakpoint.Access == string(accessModeRead) && ctx.Memory.IsWrite {
				return false
			}
			if breakpoint.Access == string(accessModeWrite) && !ctx.Memory.IsWrite {
				return false
			}
			if breakpoint.Offset == 0 && breakpoint.Size == 0 {
				return true
			}
			return rangesOverlap(breakpoint.Offset, breakpoint.Size, ctx.Memory.Offset, ctx.Memory.Size)
		},
		builder: func(ctx *engine.HookContext) any {
			if ctx == nil || ctx.Memory == nil {
				return nil
			}
			return &MemoryAccess{Offset: ctx.Memory.Offset, Size: ctx.Memory.Size, IsWrite: ctx.Memory.IsWrite, Data: encodeBytes(ctx.Memory.Data)}
		},
	}
}

func (hook *debugAccessHook) registry() engine.HookRegistry {
	registry := engine.NewSimpleHookRegistry()
	_ = registry.Register(hook)
	return registry
}

func (hook *debugAccessHook) Type() engine.HookType { return hook.kind }
func (hook *debugAccessHook) OneTime() bool         { return false }
func (hook *debugAccessHook) ID() string            { return hook.hookID }

func (hook *debugAccessHook) Fire(ctx *engine.HookContext) (*engine.HookResult, error) {
	if hook.session.LastStep <= hook.session.Position {
		return &engine.HookResult{Action: engine.ActionContinue}, nil
	}
	for _, breakpoint := range hook.session.Breakpoints {
		if !hook.matcher(ctx, breakpoint) {
			continue
		}
		pause := capturePause(hook.session, ctx, hook.session.LastStep, hook.reason, breakpoint.Display)
		switch value := hook.builder(ctx).(type) {
		case *CallAccess:
			pause.CallAccess = value
		case *StorageAccess:
			pause.StorageAccess = value
		case *MemoryAccess:
			pause.MemoryAccess = value
		}
		hook.session.PendingPause = pause
		return &engine.HookResult{Action: engine.ActionHalt, Err: errReplayPause}, nil
	}
	return &engine.HookResult{Action: engine.ActionContinue}, nil
}

func capturePause(session *ReplaySession, ctx *engine.HookContext, stepIndex int, reason string, breakpointDisplay string) *ReplayPause {
	step := forkengineTracePayload{}
	if stepIndex >= 0 && stepIndex < len(session.Trace) {
		traceStep := session.Trace[stepIndex]
		step = forkengineTracePayload{PC: traceStep.PC, Op: traceStep.Op, Depth: traceStep.Depth, GasRemaining: traceStep.GasRemaining, GasCost: traceStep.GasCost}
	} else if ctx != nil && ctx.Opcode != nil && ctx.State != nil {
		step = forkengineTracePayload{PC: ctx.Opcode.PC, Op: strings.ToUpper(engine.OpcodeName(ctx.Opcode.Op)), Depth: ctx.State.CallDepth(), GasRemaining: ctx.Opcode.GasRemaining, GasCost: ctx.Opcode.GasCost}
	}
	pause := &ReplayPause{Reason: reason, Breakpoint: breakpointDisplay, StepIndex: stepIndex, Memory: "0x", Step: step}
	if ctx == nil || ctx.State == nil {
		return pause
	}
	contractAddr := ctx.State.ContractAddress()
	codeAddr := ctx.State.ContractCodeAddr()
	pause.ContractAddress = addressHex(contractAddr)
	pause.CodeAddress = addressHex(codeAddr)
	pause.MemorySize = ctx.State.MemoryLen()
	if pause.MemorySize > 0 {
		session.MemorySnapshot = append(session.MemorySnapshot[:0], ctx.State.MemoryGet(0, uint64(pause.MemorySize))...)
	} else {
		session.MemorySnapshot = session.MemorySnapshot[:0]
	}
	pause.Memory, pause.MemoryTruncated = encodeMemoryCapped(ctx.State, pauseInlineMemoryCap)
	pause.MemoryRegions = describeMemoryRegions(ctx.State)
	pause.FreeMemoryPointer = readFreeMemoryPointer(ctx.State)
	pause.Stack = encodeStackSnapshot(ctx.State)
	if bundle := bundleForCodeAddress(session, codeAddr); bundle != nil {
		pause.Metadata = bundle.Metadata
		if ctx.Opcode != nil {
			pause.Source = sourceForPC(bundle.Index, ctx.Opcode.PC)
			pause.Locals = collectLocalsAtPC(bundle.Index, ctx, ctx.Opcode.PC)
		}
		pause.Storage = variablesForScope(ctx.State, contractAddr, bundle.Metadata.PersistentStorage, string(srcmap.StorageScopePersistent))
		pause.Transient = variablesForScope(ctx.State, contractAddr, bundle.Metadata.TransientStorage, string(srcmap.StorageScopeTransient))
	}
	return pause
}

func encodeMemoryCapped(state engine.ReadOnlyState, cap int) (string, bool) {
	if state == nil || state.MemoryLen() == 0 {
		return "0x", false
	}
	size := state.MemoryLen()
	if cap > 0 && size > cap {
		return encodeBytes(state.MemoryGet(0, uint64(cap))), true
	}
	return encodeBytes(state.MemoryGet(0, uint64(size))), false
}

func encodeStackSnapshot(state engine.ReadOnlyState) []string {
	if state == nil {
		return nil
	}
	n := state.StackLen()
	if n == 0 {
		return nil
	}
	out := make([]string, n)
	for i := 0; i < n; i++ {
		word := state.StackPeekN(i)
		out[i] = encodeHash(engine.Hash(word))
	}
	return out
}

func readFreeMemoryPointer(state engine.ReadOnlyState) uint64 {
	if state == nil || state.MemoryLen() < 0x60 {
		return 0
	}
	data := state.MemoryGet(0x40, 32)
	if len(data) != 32 {
		return 0
	}
	// Free memory pointer occupies the low 8 bytes of word at 0x40 in practice.
	var v uint64
	for i := 24; i < 32; i++ {
		v = (v << 8) | uint64(data[i])
	}
	return v
}

func describeMemoryRegions(state engine.ReadOnlyState) []MemoryRegionInfo {
	if state == nil {
		return nil
	}
	size := uint64(state.MemoryLen())
	if size == 0 {
		return nil
	}
	regions := make([]MemoryRegionInfo, 0, 4)
	appendRegion := func(kind srcmap.MemoryRegionKind, start, end uint64) {
		if end <= start || start >= size {
			return
		}
		if end > size {
			end = size
		}
		regions = append(regions, MemoryRegionInfo{Kind: string(kind), Label: memoryRegionLabel(kind), Offset: start, Length: end - start})
	}
	appendRegion(srcmap.MemoryRegionScratch, 0, 0x40)
	appendRegion(srcmap.MemoryRegionFreePtr, 0x40, 0x60)
	appendRegion(srcmap.MemoryRegionZeroSlot, 0x60, 0x80)
	freePtr := readFreeMemoryPointer(state)
	if freePtr > 0x80 && freePtr <= size {
		appendRegion(srcmap.MemoryRegionHeap, 0x80, freePtr)
		if freePtr < size {
			appendRegion(srcmap.MemoryRegionTemporary, freePtr, size)
		}
	} else {
		appendRegion(srcmap.MemoryRegionHeap, 0x80, size)
	}
	return regions
}

func memoryRegionLabel(kind srcmap.MemoryRegionKind) string {
	switch kind {
	case srcmap.MemoryRegionScratch:
		return "scratch space"
	case srcmap.MemoryRegionFreePtr:
		return "free memory pointer"
	case srcmap.MemoryRegionZeroSlot:
		return "zero slot"
	case srcmap.MemoryRegionHeap:
		return "heap"
	case srcmap.MemoryRegionTemporary:
		return "temporary (above free ptr)"
	}
	return string(kind)
}

func encodeMemory(state engine.ReadOnlyState) string {
	data, _ := encodeMemoryCapped(state, 0)
	return data
}

func variablesForScope(state engine.ReadOnlyState, contractAddr engine.Address, mappings []srcmap.StorageVariableMapping, scope string) []VariableValue {
	values := make([]VariableValue, 0, len(mappings))
	for _, mapping := range mappings {
		slot, err := decodeHashString(mapping.Entry.Slot)
		if err != nil {
			continue
		}
		var value engine.Hash
		if scope == string(srcmap.StorageScopeTransient) {
			value = state.TransientStorageGet(contractAddr, slot)
		} else {
			value = state.StorageGet(contractAddr, slot)
		}
		values = append(values, VariableValue{Scope: scope, Name: mapping.Entry.Label, Slot: encodeHash(slot), Value: encodeHash(value), Type: mapping.Type.Label})
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].Scope == values[j].Scope {
			return values[i].Name < values[j].Name
		}
		return values[i].Scope < values[j].Scope
	})
	return values
}

func sourceForPC(index *srcmap.Index, pc uint64) map[string]any {
	if index == nil {
		return nil
	}
	mapping, ok := index.InstructionAtPC(pc)
	if !ok {
		return nil
	}
	result := map[string]any{"pc": mapping.PC, "sourceName": index.SourceName(mapping.Source.SourceID), "start": mapping.Source.Start, "length": mapping.Source.Length, "generated": false}
	if file := index.Sources[mapping.Source.SourceID]; file != nil {
		line, column := file.LineColumnForOffset(mapping.Source.Start)
		result["line"] = line
		result["column"] = column
		result["generated"] = file.Generated
	}
	if mapping.AST != nil {
		result["nodeType"] = mapping.AST.NodeType
		result["nodeName"] = mapping.AST.Name
	}
	return result
}

// collectLocalsAtPC walks the AST upward from the deepest node covering the
// current PC, locating the enclosing FunctionDefinition / ModifierDefinition,
// and returns parameters, return parameters, and locally declared variables
// that source-textually precede the current execution point. Values are
// best-effort and currently always reported as unavailable until per-step
// stack-slot tracking is implemented.
func collectLocalsAtPC(index *srcmap.Index, ctx *engine.HookContext, pc uint64) []LocalVariable {
	if index == nil || ctx == nil {
		return nil
	}
	mapping, ok := index.InstructionAtPC(pc)
	if !ok || mapping.Source.SourceID < 0 {
		return nil
	}
	enclosing := findEnclosingFunctionLikeNode(index, mapping.AST)
	if enclosing == nil {
		// Fall back to scanning by source range when the instruction's AST
		// pointer lies outside the indexed nodes (e.g. compiler-generated).
		enclosing = findEnclosingFunctionLikeBySource(index, mapping.Source)
	}
	if enclosing == nil {
		return nil
	}
	currentStart := mapping.Source.Start
	file := index.Sources[mapping.Source.SourceID]
	locals := make([]LocalVariable, 0)

	// Parameters and return parameters.
	for _, group := range []struct {
		key  string
		kind string
	}{{"parameters", "parameter"}, {"returnParameters", "return"}} {
		params := paramListFrom(enclosing.Raw, group.key)
		for _, decl := range params {
			locals = appendLocal(locals, decl, group.kind, currentStart, file, false)
		}
	}

	// Variables declared in the body that source-textually precede the PC.
	walkASTRaw(enclosing.Raw, func(node map[string]any) bool {
		nodeType, _ := node["nodeType"].(string)
		if nodeType != "VariableDeclaration" {
			return true
		}
		// Skip the parameter / return declarations already handled above.
		src, ok := parseSrcAttr(node["src"])
		if !ok {
			return true
		}
		if src.Start >= currentStart {
			return false // declared after current PC; ignore (and stop nested walk)
		}
		// Skip declarations that belong to a parameter list of a nested
		// function or event (we only want this function's body locals).
		if isInsideParameterList(enclosing.Raw, node) {
			return true
		}
		locals = appendLocal(locals, node, "local", currentStart, file, true)
		return true
	})
	return locals
}

func findEnclosingFunctionLikeNode(index *srcmap.Index, node *srcmap.ASTNode) *srcmap.ASTNode {
	for current := node; current != nil; {
		switch current.NodeType {
		case "FunctionDefinition", "ModifierDefinition":
			return current
		}
		if current.ParentID == 0 {
			return nil
		}
		parent, ok := index.NodesByID[current.ParentID]
		if !ok || parent == current {
			return nil
		}
		current = parent
	}
	return nil
}

func findEnclosingFunctionLikeBySource(index *srcmap.Index, src srcmap.SourceRange) *srcmap.ASTNode {
	var best *srcmap.ASTNode
	for _, node := range index.NodesBySourceID[src.SourceID] {
		switch node.NodeType {
		case "FunctionDefinition", "ModifierDefinition":
		default:
			continue
		}
		if node.Src.Start > src.Start || node.Src.End() < src.End() {
			continue
		}
		if best == nil || node.Src.Length < best.Src.Length {
			best = node
		}
	}
	return best
}

func paramListFrom(raw map[string]any, key string) []map[string]any {
	if raw == nil {
		return nil
	}
	list, ok := raw[key].(map[string]any)
	if !ok {
		return nil
	}
	params, ok := list["parameters"].([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(params))
	for _, item := range params {
		if decl, ok := item.(map[string]any); ok {
			out = append(out, decl)
		}
	}
	return out
}

func appendLocal(locals []LocalVariable, decl map[string]any, kind string, currentStart int, file *srcmap.SourceFile, requireBeforePC bool) []LocalVariable {
	name, _ := decl["name"].(string)
	if name == "" {
		return locals
	}
	src, ok := parseSrcAttr(decl["src"])
	if !ok {
		return locals
	}
	if requireBeforePC && src.Start >= currentStart {
		return locals
	}
	typeStr := ""
	if td, ok := decl["typeDescriptions"].(map[string]any); ok {
		typeStr, _ = td["typeString"].(string)
	}
	if typeStr == "" {
		if tn, ok := decl["typeName"].(map[string]any); ok {
			if td, ok := tn["typeDescriptions"].(map[string]any); ok {
				typeStr, _ = td["typeString"].(string)
			}
		}
	}
	storageLoc, _ := decl["storageLocation"].(string)
	if storageLoc == "" || storageLoc == "default" {
		storageLoc = "stack"
	}
	line := 0
	if file != nil {
		l, _ := file.LineColumnForOffset(src.Start)
		line = l
	}
	return append(locals, LocalVariable{
		Name:            name,
		Type:            typeStr,
		StorageLocation: storageLoc,
		Kind:            kind,
		DeclaredAtLine:  line,
		Confidence:      "unavailable",
	})
}

func parseSrcAttr(value any) (srcmap.SourceRange, bool) {
	str, ok := value.(string)
	if !ok {
		return srcmap.SourceRange{}, false
	}
	parts := strings.Split(str, ":")
	if len(parts) < 3 {
		return srcmap.SourceRange{}, false
	}
	start, err1 := strconv.Atoi(parts[0])
	length, err2 := strconv.Atoi(parts[1])
	fileID, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return srcmap.SourceRange{}, false
	}
	return srcmap.SourceRange{SourceID: fileID, Start: start, Length: length}, true
}

func walkASTRaw(node map[string]any, visit func(map[string]any) bool) {
	if node == nil {
		return
	}
	if !visit(node) {
		return
	}
	for _, value := range node {
		switch typed := value.(type) {
		case map[string]any:
			if _, hasID := typed["id"]; hasID {
				walkASTRaw(typed, visit)
			}
		case []any:
			for _, item := range typed {
				if child, ok := item.(map[string]any); ok {
					if _, hasID := child["id"]; hasID {
						walkASTRaw(child, visit)
					}
				}
			}
		}
	}
}

func isInsideParameterList(funcRaw, target map[string]any) bool {
	for _, key := range []string{"parameters", "returnParameters"} {
		list, ok := funcRaw[key].(map[string]any)
		if !ok {
			continue
		}
		params, ok := list["parameters"].([]any)
		if !ok {
			continue
		}
		for _, item := range params {
			if reflectSameMap(item, target) {
				return true
			}
		}
	}
	return false
}

func reflectSameMap(a any, b map[string]any) bool {
	m, ok := a.(map[string]any)
	if !ok {
		return false
	}
	idA, okA := asJSONNumber(m["id"])
	idB, okB := asJSONNumber(b["id"])
	return okA && okB && idA == idB
}

func asJSONNumber(v any) (float64, bool) {
	switch typed := v.(type) {
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	}
	return 0, false
}

func bundleForCodeAddress(session *ReplaySession, codeAddr engine.Address) *contractmeta.Bundle {
	if session == nil || len(session.Bundles) == 0 {
		return nil
	}
	if bundle := session.Bundles[addressHex(codeAddr)]; bundle != nil {
		return bundle
	}
	if session.CodeAddr != nil {
		if bundle := session.Bundles[addressHex(*session.CodeAddr)]; bundle != nil {
			return bundle
		}
	}
	if len(session.Bundles) == 1 {
		for _, bundle := range session.Bundles {
			return bundle
		}
	}
	return nil
}

func (server *Server) loadSourceBundle(ctx context.Context, params []json.RawMessage) (any, *respError) {
	session, config, rpcErr := server.decodeSessionAndBundleRequest(params)
	if rpcErr != nil {
		return nil, rpcErr
	}
	runtime := true
	if config.Runtime != nil {
		runtime = *config.Runtime
	}
	loadOptions := contractmeta.LoadOptions{SourceName: config.SourceName, ContractName: config.ContractName, Runtime: runtime}
	if config.Kind != "auto" && loadOptions.ContractName == "" {
		return nil, &respError{Code: -32602, Message: "contractName is required"}
	}
	if addr, ok := parseAddress(config.Address); ok {
		loadOptions.Address = &addr
	}
	if addr, ok := parseAddress(config.CodeAddress); ok {
		loadOptions.CodeAddress = &addr
	}
	var (
		bundle      *contractmeta.Bundle
		diagnostics []string
		sourceLabel string
		err         error
	)
	switch config.Kind {
	case "local-project":
		bundle, err = contractmeta.LoadLocalProjectBundle(config.ProjectRoot, loadOptions)
	case "standard-json":
		bundle, err = contractmeta.LoadStandardJSONFile(config.StandardJSONPath, loadOptions)
	case "manual":
		bundle, err = contractmeta.LoadManualBundle(config.StandardJSONPath, config.ABIPath, loadOptions)
	case "explorer":
		manager, managerErr := contractmeta.NewSolcManager()
		if managerErr != nil {
			return nil, internalError(managerErr)
		}
		address := session.TargetAddr
		if loadOptions.Address != nil {
			address = loadOptions.Address
		}
		if address == nil {
			return nil, &respError{Code: -32602, Message: "explorer source bundle requires address or an active contract session"}
		}
		bundle, err = contractmeta.LoadExplorerBundle(ctx, contractmeta.ExplorerClient{APIBase: config.APIBase, APIKey: config.APIKey, RPCURL: config.RPCURL, ChainID: config.ChainID}, manager, contractmeta.ExplorerLoadOptions{Address: *address, LoadOptions: loadOptions, RPCURL: config.RPCURL})
	case "auto":
		result, autoErr := server.autoLoadBundle(ctx, session, config, loadOptions)
		if autoErr != nil {
			return nil, autoErr
		}
		bundle = result.Bundle
		diagnostics = result.Diagnostics
		sourceLabel = result.Source
	default:
		return nil, &respError{Code: -32602, Message: "unsupported source bundle kind"}
	}
	if err != nil {
		return nil, internalError(err)
	}
	keyAddr := bundle.CodeAddress
	if keyAddr == nil {
		keyAddr = bundle.Address
	}
	if keyAddr == nil && session.TargetAddr != nil {
		keyAddr = session.TargetAddr
	}
	if keyAddr == nil {
		return nil, &respError{Code: -32602, Message: "source bundle does not resolve to an address"}
	}
	if session.Bundles == nil {
		session.Bundles = make(map[string]*contractmeta.Bundle)
	}
	session.Bundles[addressHex(*keyAddr)] = bundle
	if bundle.CodeAddress != nil {
		session.CodeAddr = bundle.CodeAddress
	}
	if bundle.Address != nil {
		session.TargetAddr = bundle.Address
	}
	described := server.describeSession(session)
	if sourceLabel != "" {
		described["bundleSource"] = sourceLabel
	}
	if len(diagnostics) > 0 {
		described["bundleDiagnostics"] = diagnostics
	}
	return described, nil
}

// engineAutoSourceProvider adapts the forkengine to contractmeta.AutoSourceProvider so the auto
// loader can fetch deployed bytecode and read EIP-1967 storage slots without importing forkengine.
type engineAutoSourceProvider struct {
	server  *Server
	session *ReplaySession
}

func (p engineAutoSourceProvider) GetCodeAt(ctx context.Context, addr engine.Address) ([]byte, error) {
	return p.server.engine.GetCodeAt(ctx, addr, p.blockRef())
}

func (p engineAutoSourceProvider) GetStorageSlot(ctx context.Context, addr engine.Address, slot engine.Hash) (engine.Hash, error) {
	return p.server.engine.GetStorageSlot(ctx, addr, slot, p.blockRef())
}

func (p engineAutoSourceProvider) blockRef() upstream.BlockRef {
	if p.session != nil && p.session.CallRequest != nil {
		return p.session.CallRequest.Block
	}
	return upstream.BlockRef{}
}

func (server *Server) autoLoadBundle(ctx context.Context, session *ReplaySession, config sourceBundleRequest, loadOpts contractmeta.LoadOptions) (*contractmeta.AutoLoadResult, *respError) {
	address := loadOpts.Address
	if address == nil {
		address = session.TargetAddr
	}
	if address == nil {
		return nil, &respError{Code: -32602, Message: "auto source bundle requires an address or an active contract session"}
	}
	provider := engineAutoSourceProvider{server: server, session: session}
	autoOpts := contractmeta.AutoLoadOptions{
		Address:               *address,
		CodeAddress:           loadOpts.CodeAddress,
		ProjectRoot:           config.ProjectRoot,
		PreferredContractName: loadOpts.ContractName,
	}
	if config.APIBase != "" {
		autoOpts.Explorer = contractmeta.ExplorerClient{APIBase: config.APIBase, APIKey: config.APIKey, RPCURL: config.RPCURL, ChainID: config.ChainID}
		manager, managerErr := contractmeta.NewSolcManager()
		if managerErr == nil {
			autoOpts.SolcManager = manager
		}
	}
	result, err := contractmeta.AutoLoadBundle(ctx, provider, autoOpts)
	if err != nil {
		var autoErr *contractmeta.AutoLoadError
		if errors.As(err, &autoErr) {
			return nil, &respError{Code: -32004, Message: autoErr.Error(), Data: map[string]any{"diagnostics": autoErr.Diagnostics}}
		}
		return nil, internalError(err)
	}
	return result, nil
}

func (server *Server) setSourceBreakpoint(params []json.RawMessage) (any, *respError) {
	session, request, rpcErr := decodeSessionAndRequest[sourceBreakpointRequest](server, params)
	if rpcErr != nil {
		return nil, rpcErr
	}
	bundle, codeAddress, rpcErr := resolveBundleForBreakpoint(session, request.Address)
	if rpcErr != nil {
		return nil, rpcErr
	}
	pcs, rpcErr := resolveSourceBreakpointPCs(bundle, request.SourceName, request.Line, request.Column)
	if rpcErr != nil {
		return nil, rpcErr
	}
	id := request.ID
	if id == "" {
		id = fmt.Sprintf("bp-source-%s-%d-%d", request.SourceName, request.Line, request.Column)
	}
	breakpoint := DebugBreakpoint{ID: id, Kind: "source", Display: fmt.Sprintf("%s:%d:%d", request.SourceName, request.Line, request.Column), CodeAddress: codeAddress, SourceName: request.SourceName, Line: request.Line, Column: request.Column, PCs: pcs}
	upsertBreakpoint(session, breakpoint)
	return server.describeSession(session), nil
}

func (server *Server) setFunctionBreakpoint(params []json.RawMessage) (any, *respError) {
	session, request, rpcErr := decodeSessionAndRequest[functionBreakpointRequest](server, params)
	if rpcErr != nil {
		return nil, rpcErr
	}
	method, canonical, err := buildMethodFromSignature(request.Signature)
	if err != nil {
		return nil, &respError{Code: -32602, Message: err.Error()}
	}
	id := request.ID
	if id == "" {
		id = fmt.Sprintf("bp-function-%s", canonical)
	}
	breakpoint := DebugBreakpoint{ID: id, Kind: "function", Display: fmt.Sprintf("function %s", canonical), Signature: canonical, selector: append([]byte(nil), method.ID...)}
	upsertBreakpoint(session, breakpoint)
	return server.describeSession(session), nil
}

func (server *Server) setCallBreakpoint(params []json.RawMessage) (any, *respError) {
	session, request, rpcErr := decodeSessionAndRequest[callBreakpointRequest](server, params)
	if rpcErr != nil {
		return nil, rpcErr
	}
	var filter *engine.Address
	if addr, ok := parseAddress(request.Address); ok {
		filter = &addr
	}
	if filter == nil && strings.TrimSpace(request.Signature) == "" {
		return nil, &respError{Code: -32602, Message: "call breakpoint requires address or signature"}
	}
	breakpoint := DebugBreakpoint{Kind: "call"}
	if filter != nil {
		breakpoint.addressFilter = filter
		breakpoint.Address = addressHex(*filter)
		breakpoint.Display = fmt.Sprintf("call %s", breakpoint.Address)
	}
	if request.Signature != "" {
		method, canonical, err := buildMethodFromSignature(request.Signature)
		if err != nil {
			return nil, &respError{Code: -32602, Message: err.Error()}
		}
		breakpoint.Signature = canonical
		breakpoint.selector = append([]byte(nil), method.ID...)
		if breakpoint.Display == "" {
			breakpoint.Display = fmt.Sprintf("call %s", canonical)
		} else {
			breakpoint.Display += " " + canonical
		}
	}
	if request.ID != "" {
		breakpoint.ID = request.ID
	} else {
		breakpoint.ID = strings.ReplaceAll("bp-"+breakpoint.Display, " ", "-")
	}
	upsertBreakpoint(session, breakpoint)
	return server.describeSession(session), nil
}

func (server *Server) setStorageBreakpoint(params []json.RawMessage) (any, *respError) {
	session, request, rpcErr := decodeSessionAndRequest[storageBreakpointRequest](server, params)
	if rpcErr != nil {
		return nil, rpcErr
	}
	access, err := parseAccessMode(request.Access)
	if err != nil {
		return nil, &respError{Code: -32602, Message: err.Error()}
	}
	breakpoint := DebugBreakpoint{Kind: "storage", Access: string(access)}
	if addr, ok := parseAddress(request.Address); ok {
		breakpoint.addressFilter = &addr
		breakpoint.Address = addressHex(addr)
	}
	identifier := strings.TrimSpace(request.Slot)
	if identifier == "" {
		identifier = strings.TrimSpace(request.Name)
	}
	if identifier == "" {
		return nil, &respError{Code: -32602, Message: "storage breakpoint requires slot or name"}
	}
	slot, label, rpcErr := resolveStorageIdentifier(session, request.Address, request.Slot, request.Name)
	if rpcErr != nil {
		return nil, rpcErr
	}
	breakpoint.slotFilter = slot
	if slot != nil {
		breakpoint.Slot = encodeHash(*slot)
	}
	breakpoint.SlotLabel = label
	breakpoint.Display = fmt.Sprintf("storage %s %s", label, breakpoint.Access)
	if breakpoint.Address != "" {
		breakpoint.Display += " @ " + breakpoint.Address
	}
	if request.ID != "" {
		breakpoint.ID = request.ID
	} else {
		breakpoint.ID = strings.ReplaceAll("bp-"+breakpoint.Display, " ", "-")
	}
	upsertBreakpoint(session, breakpoint)
	return server.describeSession(session), nil
}

func (server *Server) setMemoryBreakpoint(params []json.RawMessage) (any, *respError) {
	session, request, rpcErr := decodeSessionAndRequest[memoryBreakpointRequest](server, params)
	if rpcErr != nil {
		return nil, rpcErr
	}
	access, err := parseAccessMode(request.Access)
	if err != nil {
		return nil, &respError{Code: -32602, Message: err.Error()}
	}
	breakpoint := DebugBreakpoint{Kind: "memory", Offset: request.Offset, Size: request.Size, Access: string(access)}
	if request.SourceName != "" || request.Line > 0 {
		bundle, codeAddress, rpcErr := resolveBundleForBreakpoint(session, request.Address)
		if rpcErr != nil {
			return nil, rpcErr
		}
		pcs, rpcErr := resolveSourceBreakpointPCs(bundle, request.SourceName, request.Line, request.Column)
		if rpcErr != nil {
			return nil, rpcErr
		}
		breakpoint.CodeAddress = codeAddress
		breakpoint.SourceName = request.SourceName
		breakpoint.Line = request.Line
		breakpoint.Column = request.Column
		breakpoint.PCs = pcs
	}
	if breakpoint.CodeAddress == "" {
		if addr, ok := parseAddress(request.Address); ok {
			breakpoint.CodeAddress = addressHex(addr)
		}
	}
	breakpoint.Display = fmt.Sprintf("memory %#x %d %s", breakpoint.Offset, max(breakpoint.Size, uint64(1)), breakpoint.Access)
	if breakpoint.SourceName != "" {
		breakpoint.Display += fmt.Sprintf(" at %s:%d:%d", breakpoint.SourceName, breakpoint.Line, max(breakpoint.Column, 1))
	}
	if request.ID != "" {
		breakpoint.ID = request.ID
	} else {
		breakpoint.ID = strings.ReplaceAll("bp-"+breakpoint.Display, " ", "-")
	}
	upsertBreakpoint(session, breakpoint)
	return server.describeSession(session), nil
}

func (server *Server) listBreakpoints(params []json.RawMessage) (any, *respError) {
	session, rpcErr := server.decodeSessionOnly(params)
	if rpcErr != nil {
		return nil, rpcErr
	}
	return append([]DebugBreakpoint(nil), session.Breakpoints...), nil
}

func (server *Server) deleteBreakpoint(params []json.RawMessage) (any, *respError) {
	session, request, rpcErr := decodeSessionAndRequest[deleteBreakpointRequest](server, params)
	if rpcErr != nil {
		return nil, rpcErr
	}
	if strings.TrimSpace(request.ID) == "" {
		return nil, &respError{Code: -32602, Message: "breakpoint id is required"}
	}
	removed := false
	filtered := session.Breakpoints[:0]
	for _, breakpoint := range session.Breakpoints {
		if breakpoint.ID == request.ID {
			removed = true
			continue
		}
		filtered = append(filtered, breakpoint)
	}
	if !removed {
		return nil, &respError{Code: -32602, Message: fmt.Sprintf("unknown breakpoint %q", request.ID)}
	}
	session.Breakpoints = filtered
	return server.describeSession(session), nil
}

func (server *Server) writeStorage(params []json.RawMessage) (any, *respError) {
	session, request, rpcErr := decodeSessionAndRequest[writeStorageRequest](server, params)
	if rpcErr != nil {
		return nil, rpcErr
	}
	if session.Current == nil {
		return nil, &respError{Code: -32602, Message: "session is not paused"}
	}
	addr := session.TargetAddr
	if parsed, ok := parseAddress(request.Address); ok {
		addr = &parsed
	}
	if addr == nil {
		return nil, &respError{Code: -32602, Message: "storage write requires an address"}
	}
	if _, err := decodeHashString(request.Slot); err != nil {
		return nil, &respError{Code: -32602, Message: err.Error()}
	}
	if _, err := decodeHashString(request.Value); err != nil {
		return nil, &respError{Code: -32602, Message: err.Error()}
	}
	mutation := ReplayMutation{Kind: "storage", StepIndex: session.Position, Address: addressHex(*addr), Slot: normalizeHex32(request.Slot), Value: normalizeHex32(request.Value), Scope: request.Scope}
	if mutation.Scope == "" {
		mutation.Scope = string(srcmap.StorageScopePersistent)
	}
	session.Mutations = append(session.Mutations, mutation)
	applyStorageMutationToPause(session.Current, mutation)
	return server.describeSession(session), nil
}

func (server *Server) writeMemory(params []json.RawMessage) (any, *respError) {
	session, request, rpcErr := decodeSessionAndRequest[writeMemoryRequest](server, params)
	if rpcErr != nil {
		return nil, rpcErr
	}
	if session.Current == nil {
		return nil, &respError{Code: -32602, Message: "session is not paused"}
	}
	data, err := decodeBytes(request.Data)
	if err != nil {
		return nil, &respError{Code: -32602, Message: err.Error()}
	}
	mutation := ReplayMutation{Kind: "memory", StepIndex: session.Position, Offset: request.Offset, Data: encodeBytes(data)}
	session.Mutations = append(session.Mutations, mutation)
	applyMemoryMutationToPause(session.Current, mutation)
	applyMemoryMutationToSnapshot(session, mutation)
	return server.describeSession(session), nil
}

type readMemoryRequest struct {
	Offset uint64 `json:"offset"`
	Length uint64 `json:"length"`
}

func (server *Server) readMemory(params []json.RawMessage) (any, *respError) {
	session, request, rpcErr := decodeSessionAndRequest[readMemoryRequest](server, params)
	if rpcErr != nil {
		return nil, rpcErr
	}
	if session.Current == nil {
		return nil, &respError{Code: -32602, Message: "session is not paused"}
	}
	total := uint64(len(session.MemorySnapshot))
	if request.Offset > total {
		return map[string]any{"offset": request.Offset, "length": 0, "data": "0x", "memorySize": total, "truncated": false}, nil
	}
	end := request.Offset + request.Length
	if request.Length == 0 || end > total {
		end = total
	}
	chunk := append([]byte(nil), session.MemorySnapshot[request.Offset:end]...)
	return map[string]any{
		"offset":     request.Offset,
		"length":     uint64(len(chunk)),
		"data":       encodeBytes(chunk),
		"memorySize": total,
		"truncated":  end < request.Offset+request.Length,
	}, nil
}

func applyMemoryMutationToSnapshot(session *ReplaySession, mutation ReplayMutation) {
	if session == nil {
		return
	}
	data, err := decodeBytes(mutation.Data)
	if err != nil || len(data) == 0 {
		return
	}
	end := int(mutation.Offset) + len(data)
	if end > len(session.MemorySnapshot) {
		expanded := make([]byte, end)
		copy(expanded, session.MemorySnapshot)
		session.MemorySnapshot = expanded
	}
	copy(session.MemorySnapshot[int(mutation.Offset):], data)
}

func applyStorageMutationToPause(pause *ReplayPause, mutation ReplayMutation) {
	if pause == nil {
		return
	}
	target := &pause.Storage
	if mutation.Scope == string(srcmap.StorageScopeTransient) {
		target = &pause.Transient
	}
	for index := range *target {
		if (*target)[index].Slot == mutation.Slot {
			(*target)[index].Value = mutation.Value
			return
		}
	}
	*target = append(*target, VariableValue{Scope: mutation.Scope, Name: mutation.Slot, Slot: mutation.Slot, Value: mutation.Value})
}

func applyMemoryMutationToPause(pause *ReplayPause, mutation ReplayMutation) {
	if pause == nil {
		return
	}
	buffer, err := decodeBytes(pause.Memory)
	if err != nil {
		buffer = nil
	}
	data, err := decodeBytes(mutation.Data)
	if err != nil {
		return
	}
	end := int(mutation.Offset) + len(data)
	if end > len(buffer) {
		expanded := make([]byte, end)
		copy(expanded, buffer)
		buffer = expanded
	}
	copy(buffer[int(mutation.Offset):], data)
	pause.Memory = encodeBytes(buffer)
	pause.MemorySize = len(buffer)
}

func resolveBundleForBreakpoint(session *ReplaySession, requestedAddress string) (*contractmeta.Bundle, string, *respError) {
	if session == nil || len(session.Bundles) == 0 {
		return nil, "", &respError{Code: -32602, Message: "no source bundles loaded"}
	}
	if parsed, ok := parseAddress(requestedAddress); ok {
		key := addressHex(parsed)
		bundle := session.Bundles[key]
		if bundle == nil {
			return nil, "", &respError{Code: -32602, Message: "source bundle for requested address not found"}
		}
		return bundle, key, nil
	}
	if len(session.Bundles) == 1 {
		for key, bundle := range session.Bundles {
			return bundle, key, nil
		}
	}
	if session.CodeAddr != nil {
		key := addressHex(*session.CodeAddr)
		if bundle := session.Bundles[key]; bundle != nil {
			return bundle, key, nil
		}
	}
	return nil, "", &respError{Code: -32602, Message: "multiple source bundles loaded; address is required"}
}

func resolveSourceBreakpointPCs(bundle *contractmeta.Bundle, sourceName string, line int, column int) ([]uint64, *respError) {
	if bundle == nil || bundle.Index == nil {
		return nil, &respError{Code: -32602, Message: "source bundle does not contain source maps"}
	}
	if line <= 0 {
		return nil, &respError{Code: -32602, Message: "line must be >= 1"}
	}
	file, ok := bundle.Index.SourceFileByName(sourceName)
	if !ok {
		return nil, &respError{Code: -32602, Message: fmt.Sprintf("source %q not found", sourceName)}
	}
	if column <= 0 {
		column = 1
	}
	// Compute the byte range of the requested line, [lineStart, lineEnd).
	lineStart, ok := file.OffsetForLineColumn(line, 1)
	if !ok {
		return nil, &respError{Code: -32602, Message: fmt.Sprintf("invalid line %d", line)}
	}
	lineEnd, ok := file.OffsetForLineColumn(line+1, 1)
	if !ok {
		lineEnd = len(file.Content)
	}
	if lineEnd <= lineStart {
		lineEnd = lineStart + 1
	}

	// Strict match: instruction's source range must begin on this line and
	// fit within the remaining tail of the line. This filters out the wide
	// dispatcher / contract-definition mappings (e.g. `s=0,l=<file size>,f=id`)
	// that otherwise overlap every line and would resolve every breakpoint to
	// PC=0. Compiler-generated instructions (SourceID == -1) are skipped per
	// the source_mapping.rst convention.
	type candidate struct {
		pc     uint64
		start  int
		length int
	}
	var matches []candidate
	for _, instruction := range bundle.Index.Instructions {
		src := instruction.Source
		if src.SourceID != file.ID {
			continue
		}
		if src.Start < lineStart || src.Start >= lineEnd {
			continue
		}
		// Reject instructions whose source span extends beyond the current
		// line; those are typically function- or contract-level mappings.
		if src.End() > lineEnd {
			continue
		}
		matches = append(matches, candidate{pc: instruction.PC, start: src.Start, length: src.Length})
	}

	// Prefer instructions starting at or after the requested column; fall back
	// to the earliest match on the line otherwise.
	requestedOffset := lineStart + column - 1
	if requestedOffset < lineStart {
		requestedOffset = lineStart
	}

	if len(matches) == 0 {
		// Fallback: the requested line may not have any compiled statements
		// (blank line, comment, etc). Walk forward to the next line that does
		// have a mapping so users still get a usable breakpoint.
		var fallback *srcmap.InstructionMapping
		for index := range bundle.Index.Instructions {
			instruction := &bundle.Index.Instructions[index]
			if instruction.Source.SourceID != file.ID {
				continue
			}
			if instruction.Source.Start < lineStart {
				continue
			}
			if fallback == nil || instruction.Source.Start < fallback.Source.Start {
				fallback = instruction
			}
		}
		if fallback == nil {
			return nil, &respError{Code: -32602, Message: "no instruction mapping found for requested source location"}
		}
		return []uint64{fallback.PC}, nil
	}

	// Pick the smallest source span that starts at or after requestedOffset;
	// if none, use the smallest span on the line. This corresponds to the
	// innermost statement at the user's chosen position.
	best := -1
	for i, c := range matches {
		if c.start < requestedOffset {
			continue
		}
		if best == -1 || matches[i].length < matches[best].length || (matches[i].length == matches[best].length && matches[i].start < matches[best].start) {
			best = i
		}
	}
	if best == -1 {
		for i := range matches {
			if best == -1 || matches[i].length < matches[best].length || (matches[i].length == matches[best].length && matches[i].start < matches[best].start) {
				best = i
			}
		}
	}

	chosenStart := matches[best].start
	chosenLength := matches[best].length

	// Return every PC that shares the chosen source span — the same statement
	// is often emitted multiple times (e.g. inlined per overload/branch).
	pcs := make([]uint64, 0, 4)
	seen := make(map[uint64]struct{})
	for _, c := range matches {
		if c.start != chosenStart || c.length != chosenLength {
			continue
		}
		if _, dup := seen[c.pc]; dup {
			continue
		}
		seen[c.pc] = struct{}{}
		pcs = append(pcs, c.pc)
	}
	sort.Slice(pcs, func(i, j int) bool { return pcs[i] < pcs[j] })
	return pcs, nil
}

func (server *Server) decodeSessionAndBundleRequest(params []json.RawMessage) (*ReplaySession, sourceBundleRequest, *respError) {
	return decodeSessionAndRequest[sourceBundleRequest](server, params)
}

func (server *Server) decodeSessionOnly(params []json.RawMessage) (*ReplaySession, *respError) {
	if len(params) < 1 {
		return nil, &respError{Code: -32602, Message: "expected session ID"}
	}
	sessionID, rpcErr := decodeStringParam(params[:1])
	if rpcErr != nil {
		return nil, rpcErr
	}
	return server.lookupSession(sessionID)
}

// lookupSession resolves a session ID to its ReplaySession or returns an
// "unknown gdb session" RPC error.
func (server *Server) lookupSession(sessionID string) (*ReplaySession, *respError) {
	server.mu.Lock()
	session := server.sessions[sessionID]
	server.mu.Unlock()
	if session == nil {
		return nil, &respError{Code: -32602, Message: "unknown gdb session"}
	}
	return session, nil
}

func decodeSessionAndRequest[T any](server *Server, params []json.RawMessage) (*ReplaySession, T, *respError) {
	var zero T
	if len(params) < 2 {
		return nil, zero, &respError{Code: -32602, Message: "expected session ID and request object"}
	}
	sessionID, rpcErr := decodeStringParam(params[:1])
	if rpcErr != nil {
		return nil, zero, rpcErr
	}
	session, rpcErr := server.lookupSession(sessionID)
	if rpcErr != nil {
		return nil, zero, rpcErr
	}
	var request T
	if err := json.Unmarshal(params[1], &request); err != nil {
		return nil, zero, &respError{Code: -32602, Message: err.Error()}
	}
	return session, request, nil
}

func extractMutableState(ctx *engine.HookContext) (engine.EVMState, bool) {
	if ctx == nil || ctx.State == nil {
		return nil, false
	}
	reader, ok := ctx.State.(interface{ Inner() engine.EVMState })
	if ok {
		return reader.Inner(), true
	}
	return nil, false
}

func (session *ReplaySession) matchSourceBreakpoint(ctx *engine.HookContext) *DebugBreakpoint {
	if ctx == nil || ctx.Opcode == nil || ctx.State == nil {
		return nil
	}
	codeAddr := addressHex(ctx.State.ContractCodeAddr())
	for index := range session.Breakpoints {
		breakpoint := &session.Breakpoints[index]
		if breakpoint.Kind != "source" {
			continue
		}
		if breakpoint.CodeAddress != "" && breakpoint.CodeAddress != codeAddr {
			continue
		}
		if containsPC(breakpoint.PCs, ctx.Opcode.PC) {
			return breakpoint
		}
	}
	return nil
}

func (session *ReplaySession) matchRootFunctionBreakpoint(ctx *engine.HookContext) *DebugBreakpoint {
	if ctx == nil || ctx.State == nil || ctx.Opcode == nil {
		return nil
	}
	if ctx.State.CallDepth() != 0 || ctx.Opcode.PC != 0 {
		return nil
	}
	for index := range session.Breakpoints {
		breakpoint := &session.Breakpoints[index]
		if breakpoint.Kind != "function" {
			continue
		}
		if selectorMatches(session.RootInput, breakpoint.selector) {
			return breakpoint
		}
	}
	return nil
}

func upsertBreakpoint(session *ReplaySession, breakpoint DebugBreakpoint) {
	for index := range session.Breakpoints {
		if session.Breakpoints[index].ID == breakpoint.ID {
			session.Breakpoints[index] = breakpoint
			return
		}
	}
	session.Breakpoints = append(session.Breakpoints, breakpoint)
}

func resolveStorageIdentifier(session *ReplaySession, requestedAddress string, slotText string, name string) (*engine.Hash, string, *respError) {
	if strings.TrimSpace(slotText) != "" {
		slot, err := decodeHashString(slotText)
		if err != nil {
			return nil, "", &respError{Code: -32602, Message: err.Error()}
		}
		return &slot, encodeHash(slot), nil
	}
	if strings.TrimSpace(name) == "" {
		return nil, "", nil
	}
	bundle, _, rpcErr := resolveBundleForBreakpoint(session, requestedAddress)
	if rpcErr != nil {
		return nil, "", rpcErr
	}
	for _, mapping := range bundle.Metadata.PersistentStorage {
		if mapping.Entry.Label == name {
			slot, err := decodeHashString(mapping.Entry.Slot)
			if err != nil {
				return nil, "", &respError{Code: -32602, Message: err.Error()}
			}
			return &slot, name, nil
		}
	}
	for _, mapping := range bundle.Metadata.TransientStorage {
		if mapping.Entry.Label == name {
			slot, err := decodeHashString(mapping.Entry.Slot)
			if err != nil {
				return nil, "", &respError{Code: -32602, Message: err.Error()}
			}
			return &slot, name, nil
		}
	}
	return nil, "", &respError{Code: -32602, Message: fmt.Sprintf("storage variable %q not found", name)}
}

func containsPC(pcs []uint64, pc uint64) bool {
	for _, candidate := range pcs {
		if candidate == pc {
			return true
		}
	}
	return false
}

func parseAccessMode(value string) (breakpointAccessMode, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "rw", "both", "readwrite":
		return accessModeBoth, nil
	case "read", "r":
		return accessModeRead, nil
	case "write", "w":
		return accessModeWrite, nil
	default:
		return "", fmt.Errorf("unsupported access mode %q", value)
	}
}

func decodeHashString(input string) (engine.Hash, error) {
	bytes, err := decodeFixedBytes(normalizeHex32(input), 32)
	if err != nil {
		return engine.Hash{}, err
	}
	var hash engine.Hash
	copy(hash[:], bytes)
	return hash, nil
}

func normalizeHex32(input string) string {
	trimmed := strings.TrimPrefix(input, "0x")
	if len(trimmed) < 64 {
		trimmed = strings.Repeat("0", 64-len(trimmed)) + trimmed
	}
	return "0x" + trimmed
}

func parseAddress(input string) (engine.Address, bool) {
	if strings.TrimSpace(input) == "" {
		return engine.Address{}, false
	}
	addr, err := decodeAddress(input)
	if err != nil {
		return engine.Address{}, false
	}
	return addr, true
}

func encodeHash(hash engine.Hash) string {
	return "0x" + hex.EncodeToString(hash[:])
}

func buildMethodFromSignature(signature string) (gethabi.Method, string, error) {
	trimmed := normalizeFunctionSignature(signature)
	name, types, err := parseSignature(trimmed)
	if err != nil {
		return gethabi.Method{}, "", err
	}
	inputs := make([]gethabi.ArgumentMarshaling, 0, len(types))
	for index, typeName := range types {
		if strings.Contains(typeName, "tuple") {
			return gethabi.Method{}, "", fmt.Errorf("tuple signatures are not supported")
		}
		inputs = append(inputs, gethabi.ArgumentMarshaling{Name: fmt.Sprintf("arg%d", index), Type: typeName})
	}
	definition, err := json.Marshal([]map[string]any{{"type": "function", "name": name, "inputs": inputs}})
	if err != nil {
		return gethabi.Method{}, "", err
	}
	parsed, err := gethabi.JSON(strings.NewReader(string(definition)))
	if err != nil {
		return gethabi.Method{}, "", err
	}
	method, ok := parsed.Methods[name]
	if !ok {
		return gethabi.Method{}, "", fmt.Errorf("method %q not found after parsing", name)
	}
	return method, method.Sig, nil
}

func normalizeFunctionSignature(signature string) string {
	trimmed := strings.TrimSpace(signature)
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "function ") {
		trimmed = strings.TrimSpace(trimmed[9:])
	}
	if strings.HasPrefix(strings.ToLower(trimmed), "sig ") {
		trimmed = strings.TrimSpace(trimmed[4:])
	}
	open := strings.IndexByte(trimmed, '(')
	if open > 0 {
		prefix := trimmed[:open]
		if dot := strings.LastIndexByte(prefix, '.'); dot >= 0 {
			trimmed = prefix[dot+1:] + trimmed[open:]
		}
	}
	return trimmed
}

func parseSignature(signature string) (string, []string, error) {
	open := strings.IndexByte(signature, '(')
	close := strings.LastIndexByte(signature, ')')
	if open <= 0 || close <= open {
		return "", nil, fmt.Errorf("invalid signature %q", signature)
	}
	name := strings.TrimSpace(signature[:open])
	inside := signature[open+1 : close]
	if strings.TrimSpace(inside) == "" {
		return name, nil, nil
	}
	parts := splitSignatureArguments(inside)
	for index := range parts {
		parts[index] = strings.TrimSpace(parts[index])
	}
	return name, parts, nil
}

func splitSignatureArguments(value string) []string {
	parts := make([]string, 0)
	depth := 0
	start := 0
	for index, r := range value {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, value[start:index])
				start = index + 1
			}
		}
	}
	parts = append(parts, value[start:])
	return parts
}

func selectorMatches(input []byte, selector []byte) bool {
	return len(selector) == 4 && len(input) >= 4 && input[0] == selector[0] && input[1] == selector[1] && input[2] == selector[2] && input[3] == selector[3]
}

func rangesOverlap(offsetA uint64, sizeA uint64, offsetB uint64, sizeB uint64) bool {
	endA := offsetA + max(sizeA, uint64(1))
	endB := offsetB + max(sizeB, uint64(1))
	return offsetA < endB && offsetB < endA
}

func hookTypeLabel(hookType engine.HookType) string {
	switch hookType {
	case engine.HookTypeExternalCall:
		return "call"
	case engine.HookTypeDelegateCall:
		return "delegatecall"
	case engine.HookTypeStaticCall:
		return "staticcall"
	case engine.HookTypeCallCode:
		return "callcode"
	default:
		return fmt.Sprintf("hook-%d", hookType)
	}
}

type mergedHookRegistry struct {
	registries []engine.HookRegistry
}

func mergeHookRegistries(base engine.HookRegistry, extra engine.HookRegistry) engine.HookRegistry {
	if base == nil {
		return extra
	}
	if extra == nil {
		return base
	}
	return &mergedHookRegistry{registries: []engine.HookRegistry{base, extra}}
}

func (registry *mergedHookRegistry) Register(hook engine.Hook) error {
	if len(registry.registries) == 0 {
		return fmt.Errorf("no hook registry available")
	}
	return registry.registries[len(registry.registries)-1].Register(hook)
}

func (registry *mergedHookRegistry) Unregister(hookID string) error {
	var lastErr error
	for _, child := range registry.registries {
		if err := child.Unregister(hookID); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	return lastErr
}

func (registry *mergedHookRegistry) HooksFor(hookType engine.HookType) []engine.Hook {
	var hooks []engine.Hook
	for _, child := range registry.registries {
		hooks = append(hooks, child.HooksFor(hookType)...)
	}
	return hooks
}

func (registry *mergedHookRegistry) Clear() {
	for _, child := range registry.registries {
		child.Clear()
	}
}
