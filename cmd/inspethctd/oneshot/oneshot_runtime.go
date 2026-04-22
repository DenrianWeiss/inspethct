package oneshot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"inspethct/internal/contractmeta"
	"inspethct/internal/engine"
	"inspethct/internal/forkengine"
	"inspethct/internal/forkengine/upstream"
	"inspethct/internal/srcmap"
)

var errOneShotPause = errors.New("oneshot pause")

func (session *oneshotSession) run(ctx context.Context, mode oneshotExecutionMode) error {
	prepared, err := session.prepare(ctx)
	if err != nil {
		return err
	}
	session.pendingPause = nil
	session.rootInput = append(session.rootInput[:0], prepared.Config.Input...)
	stepHook := &oneshotStepHook{session: session, mode: mode}
	prepared.Config.Hooks = mergeMainHookRegistries(prepared.Config.Hooks, stepHook.registry())
	prepared.Config.Hooks = mergeMainHookRegistries(prepared.Config.Hooks, newCallHook(session, engine.HookTypeExternalCall, "oneshot-call").registry())
	prepared.Config.Hooks = mergeMainHookRegistries(prepared.Config.Hooks, newCallHook(session, engine.HookTypeDelegateCall, "oneshot-delegate").registry())
	prepared.Config.Hooks = mergeMainHookRegistries(prepared.Config.Hooks, newCallHook(session, engine.HookTypeStaticCall, "oneshot-static").registry())
	prepared.Config.Hooks = mergeMainHookRegistries(prepared.Config.Hooks, newCallHook(session, engine.HookTypeCallCode, "oneshot-callcode").registry())
	prepared.Config.Hooks = mergeMainHookRegistries(prepared.Config.Hooks, newStorageHook(session, engine.HookTypeStorageRead, engine.HookTypeTransientLoad, "oneshot-storage-read", false).registry())
	prepared.Config.Hooks = mergeMainHookRegistries(prepared.Config.Hooks, newStorageHook(session, engine.HookTypeStorageWrite, engine.HookTypeTransientStore, "oneshot-storage-write", true).registry())
	prepared.Config.Hooks = mergeMainHookRegistries(prepared.Config.Hooks, newMemoryHook(session, engine.HookTypeMemoryRead, "oneshot-memory-read", false).registry())
	prepared.Config.Hooks = mergeMainHookRegistries(prepared.Config.Hooks, newMemoryHook(session, engine.HookTypeMemoryWrite, "oneshot-memory-write", true).registry())
	result, execErr := session.engineRef.ExecutePreparedCall(prepared)
	if session.pendingPause != nil {
		session.current = session.pendingPause
		session.position = session.pendingPause.StepIndex
		session.done = false
		session.result = nil
		return nil
	}
	if execErr != nil && result == nil {
		return execErr
	}
	session.current = nil
	session.result = result
	session.done = true
	return nil
}

func (session *oneshotSession) prepare(ctx context.Context) (*forkengine.PreparedCall, error) {
	if err := session.ensureEngine(); err != nil {
		return nil, err
	}
	switch session.kind {
	case "replay":
		prepared, tx, receipt, err := session.engineRef.PrepareReplay(ctx, session.txHash)
		if err != nil {
			return nil, err
		}
		if tx.To != nil {
			addr := *tx.To
			session.targetAddr = &addr
			session.codeAddr = &addr
		} else if receipt.ContractAddress != nil {
			addr := *receipt.ContractAddress
			session.targetAddr = &addr
			session.codeAddr = &addr
		}
		if prepared.ReplayState != nil {
			session.exact = prepared.ReplayState.Exact
			session.limitation = prepared.ReplayState.Limitation
		}
		return prepared, nil
	case "call":
		session.callReq.Block = session.config.BlockRef
		prepared, err := session.engineRef.PrepareCall(ctx, session.callReq)
		if err != nil {
			return nil, err
		}
		addr := session.callReq.To
		session.targetAddr = &addr
		session.codeAddr = &addr
		return prepared, nil
	default:
		return nil, fmt.Errorf("unsupported session kind %q", session.kind)
	}
}

func (session *oneshotSession) ensureEngine() error {
	if session.engineRef != nil {
		return nil
	}
	if strings.TrimSpace(session.config.UpstreamURL) == "" {
		return fmt.Errorf("upstream RPC is not configured. Add it with: set config upstream <url>")
	}
	provider, err := upstream.NewJSONRPCProvider(upstream.JSONRPCConfig{Endpoint: session.config.UpstreamURL})
	if err != nil {
		return err
	}
	engineRef, err := forkengine.New(forkengine.Config{
		Provider:        provider,
		Fork:            session.config.Fork,
		ChainIDOverride: cloneBigInt(session.config.ChainIDOverride),
		Mode:            session.config.Mode,
		Block:           session.config.BlockRef,
	})
	if err != nil {
		return err
	}
	session.engineRef = engineRef
	return nil
}

func (session *oneshotSession) loadExplorerBundle(ctx context.Context, addr engine.Address, contractName string, sourceName string) (*contractmeta.Bundle, error) {
	manager, err := contractmeta.NewSolcManager()
	if err != nil {
		return nil, err
	}
	return contractmeta.LoadExplorerBundle(ctx, contractmeta.ExplorerClient{
		APIBase: session.currentExplorerAPIBase(),
		APIKey:  session.config.ExplorerAPIKey,
		RPCURL:  session.config.UpstreamURL,
		ChainID: session.currentExplorerChainID(),
	}, manager, contractmeta.ExplorerLoadOptions{
		Address: addr,
		RPCURL:  session.config.UpstreamURL,
		LoadOptions: contractmeta.LoadOptions{
			ContractName: strings.TrimSpace(contractName),
			SourceName:   strings.TrimSpace(sourceName),
			Runtime:      true,
			Address:      cloneAddress(&addr),
		},
	})
}

func (session *oneshotSession) currentExplorerAPIBase() string {
	if strings.TrimSpace(session.config.ExplorerAPIBase) == "" {
		return defaultExplorerAPIBase
	}
	return session.config.ExplorerAPIBase
}

func (session *oneshotSession) currentExplorerChainID() string {
	if strings.TrimSpace(session.config.ExplorerChainID) == "" {
		return "1"
	}
	return session.config.ExplorerChainID
}

func (session *oneshotSession) reset() {
	session.resetExecutionProgress()
	session.result = nil
	session.current = nil
}

func (session *oneshotSession) resetExecutionProgress() {
	session.position = -1
	session.lastStep = -1
	session.done = false
	session.result = nil
	session.current = nil
	session.pendingPause = nil
	session.rootInput = nil
	session.exact = false
	session.limitation = ""
}

func (hook *oneshotStepHook) registry() engine.HookRegistry {
	registry := engine.NewSimpleHookRegistry()
	_ = registry.Register(hook)
	return registry
}

func (hook *oneshotStepHook) Type() engine.HookType { return engine.HookTypeStep }
func (hook *oneshotStepHook) OneTime() bool         { return false }
func (hook *oneshotStepHook) ID() string            { return "oneshot-step" }

func (hook *oneshotStepHook) Fire(ctx *engine.HookContext) (*engine.HookResult, error) {
	stepIndex := hook.seen
	hook.seen++
	hook.session.lastStep = stepIndex
	hook.session.applyMutations(ctx, stepIndex)
	if stepIndex <= hook.session.position {
		return &engine.HookResult{Action: engine.ActionContinue}, nil
	}
	if hook.mode == oneshoModeNext && stepIndex == hook.session.position+1 {
		hook.session.pendingPause = captureOneShotPause(hook.session, ctx, stepIndex, "step", "")
		return &engine.HookResult{Action: engine.ActionHalt, Err: errOneShotPause}, nil
	}
	if breakpoint := hook.session.matchRootFunctionBreakpoint(ctx); breakpoint != nil {
		hook.session.pendingPause = captureOneShotPause(hook.session, ctx, stepIndex, "function_breakpoint", breakpoint.Display)
		return &engine.HookResult{Action: engine.ActionHalt, Err: errOneShotPause}, nil
	}
	if breakpoint := hook.session.matchSourceBreakpoint(ctx); breakpoint != nil {
		hook.session.pendingPause = captureOneShotPause(hook.session, ctx, stepIndex, "source_breakpoint", breakpoint.Display)
		return &engine.HookResult{Action: engine.ActionHalt, Err: errOneShotPause}, nil
	}
	return &engine.HookResult{Action: engine.ActionContinue}, nil
}

func newCallHook(session *oneshotSession, hookType engine.HookType, hookID string) *oneshotAccessHook {
	return &oneshotAccessHook{
		session: session,
		kind:    hookType,
		hookID:  hookID,
		reason:  "call_breakpoint",
		matcher: func(ctx *engine.HookContext, breakpoint oneshotBreakpoint) bool {
			if breakpoint.Kind != breakpointKindCall && breakpoint.Kind != breakpointKindFunction {
				return false
			}
			if ctx == nil || ctx.Call == nil {
				return false
			}
			if breakpoint.Kind == breakpointKindCall && breakpoint.AddressFilter != nil && *breakpoint.AddressFilter != ctx.Call.Callee && *breakpoint.AddressFilter != ctx.Call.CodeAddr {
				return false
			}
			if len(breakpoint.Selector) > 0 && !selectorMatches(ctx.Call.Input, breakpoint.Selector) {
				return false
			}
			return true
		},
		builder: func(ctx *engine.HookContext) any {
			if ctx == nil || ctx.Call == nil {
				return nil
			}
			value := "<nil>"
			if ctx.Call.Value != nil {
				value = ctx.Call.Value.String()
			}
			return &oneshotCallAccess{
				Kind:     hookTypeLabel(hookType),
				Caller:   formatAddress(ctx.Call.Caller),
				Callee:   formatAddress(ctx.Call.Callee),
				CodeAddr: formatAddress(ctx.Call.CodeAddr),
				Input:    encodeCLIBytes(ctx.Call.Input),
				Value:    value,
				Gas:      ctx.Call.Gas,
			}
		},
	}
}

func newStorageHook(session *oneshotSession, hookType engine.HookType, alternate engine.HookType, hookID string, write bool) *oneshotAccessHook {
	return &oneshotAccessHook{
		session: session,
		kind:    hookType,
		hookID:  hookID,
		reason:  map[bool]string{true: "storage_write_breakpoint", false: "storage_read_breakpoint"}[write],
		matcher: func(ctx *engine.HookContext, breakpoint oneshotBreakpoint) bool {
			if breakpoint.Kind != breakpointKindStorage || ctx == nil || ctx.Storage == nil {
				return false
			}
			if breakpoint.AccessMode == accessModeRead && ctx.Storage.IsWrite {
				return false
			}
			if breakpoint.AccessMode == accessModeWrite && !ctx.Storage.IsWrite {
				return false
			}
			if breakpoint.Slot != nil && *breakpoint.Slot != ctx.Storage.Slot {
				return false
			}
			return true
		},
		builder: func(ctx *engine.HookContext) any {
			if ctx == nil || ctx.Storage == nil {
				return nil
			}
			scope := "storage"
			if ctx.Storage.IsWrite && (hookType == engine.HookTypeTransientStore || alternate == engine.HookTypeTransientStore) {
				scope = "transient"
			}
			if !ctx.Storage.IsWrite && (hookType == engine.HookTypeTransientLoad || alternate == engine.HookTypeTransientLoad) {
				scope = "transient"
			}
			return &oneshotStorageAccess{Slot: formatHash(ctx.Storage.Slot), Scope: scope, IsWrite: ctx.Storage.IsWrite, Value: formatHash(ctx.Storage.Value)}
		},
	}
}

func newMemoryHook(session *oneshotSession, hookType engine.HookType, hookID string, write bool) *oneshotAccessHook {
	return &oneshotAccessHook{
		session: session,
		kind:    hookType,
		hookID:  hookID,
		reason:  map[bool]string{true: "memory_write_breakpoint", false: "memory_read_breakpoint"}[write],
		matcher: func(ctx *engine.HookContext, breakpoint oneshotBreakpoint) bool {
			if breakpoint.Kind != breakpointKindMemory || ctx == nil || ctx.Memory == nil {
				return false
			}
			if len(breakpoint.PCs) > 0 {
				if ctx.Opcode == nil || !containsPC(breakpoint.PCs, ctx.Opcode.PC) {
					return false
				}
			}
			if breakpoint.AccessMode == accessModeRead && ctx.Memory.IsWrite {
				return false
			}
			if breakpoint.AccessMode == accessModeWrite && !ctx.Memory.IsWrite {
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
			return &oneshotMemoryAccess{Offset: ctx.Memory.Offset, Size: ctx.Memory.Size, IsWrite: ctx.Memory.IsWrite, Data: encodeCLIBytes(ctx.Memory.Data)}
		},
	}
}

func (session *oneshotSession) applyMutations(ctx *engine.HookContext, stepIndex int) {
	if ctx == nil || ctx.State == nil {
		return
	}
	state, ok := extractMutableState(ctx)
	if !ok {
		return
	}
	for _, mutation := range session.mutations {
		if mutation.StepIndex != stepIndex {
			continue
		}
		switch mutation.Kind {
		case "memory":
			_ = state.Memory().Set(mutation.Offset, mutation.Data)
		}
	}
}

func (hook *oneshotAccessHook) registry() engine.HookRegistry {
	registry := engine.NewSimpleHookRegistry()
	_ = registry.Register(hook)
	return registry
}

func (hook *oneshotAccessHook) Type() engine.HookType { return hook.kind }
func (hook *oneshotAccessHook) OneTime() bool         { return false }
func (hook *oneshotAccessHook) ID() string            { return hook.hookID }

func (hook *oneshotAccessHook) Fire(ctx *engine.HookContext) (*engine.HookResult, error) {
	if hook.session.lastStep <= hook.session.position {
		return &engine.HookResult{Action: engine.ActionContinue}, nil
	}
	for _, breakpoint := range hook.session.breakpoints {
		if !hook.matcher(ctx, breakpoint) {
			continue
		}
		pause := captureOneShotPause(hook.session, ctx, hook.session.lastStep, hook.reason, breakpoint.Display)
		switch value := hook.builder(ctx).(type) {
		case *oneshotMemoryAccess:
			pause.MemoryAccess = value
		case *oneshotStorageAccess:
			pause.StorageAccess = value
		case *oneshotCallAccess:
			pause.CallAccess = value
		}
		if breakpoint.PartialMessage != "" {
			pause.Breakpoint = breakpoint.Display + " [" + breakpoint.PartialMessage + "]"
		}
		hook.session.pendingPause = pause
		return &engine.HookResult{Action: engine.ActionHalt, Err: errOneShotPause}, nil
	}
	return &engine.HookResult{Action: engine.ActionContinue}, nil
}

func captureOneShotPause(session *oneshotSession, ctx *engine.HookContext, stepIndex int, reason string, breakpointDisplay string) *oneshotPause {
	contractAddr := ctx.State.ContractAddress()
	codeAddr := ctx.State.ContractCodeAddr()
	memorySnapshot := ctx.State.MemoryGet(0, uint64(ctx.State.MemoryLen()))
	pause := &oneshotPause{
		Reason:          reason,
		StepIndex:       stepIndex,
		Breakpoint:      breakpointDisplay,
		ContractAddress: formatAddress(contractAddr),
		CodeAddress:     formatAddress(codeAddr),
		MemorySize:      ctx.State.MemoryLen(),
		MemorySnapshot:  memorySnapshot,
		Step:            oneshotStep{PC: ctx.Opcode.PC, Op: strings.ToUpper(engine.OpcodeName(ctx.Opcode.Op)), Depth: ctx.State.CallDepth(), GasRemaining: ctx.Opcode.GasRemaining, GasCost: ctx.Opcode.GasCost},
	}
	if session.bundle != nil {
		pause.Source = sourceForPCMain(session.bundle.Index, ctx.Opcode.PC)
		pause.Storage = variablesForScopeMain(ctx.State, contractAddr, session.bundle.Metadata.PersistentStorage, string(srcmap.StorageScopePersistent))
		pause.Transient = variablesForScopeMain(ctx.State, contractAddr, session.bundle.Metadata.TransientStorage, string(srcmap.StorageScopeTransient))
	}
	return pause
}

func printSessionStatus(writer io.Writer, session *oneshotSession) {
	if session.current != nil {
		pause := session.current
		_, _ = fmt.Fprintf(writer, "Paused at step %d: pc=%d op=%s depth=%d reason=%s\n", pause.StepIndex, pause.Step.PC, pause.Step.Op, pause.Step.Depth, pause.Reason)
		if pause.Breakpoint != "" {
			_, _ = fmt.Fprintf(writer, "Breakpoint: %s\n", pause.Breakpoint)
		}
		if pause.Source != nil {
			_, _ = fmt.Fprintf(writer, "Source: %v:%v:%v\n", pause.Source["sourceName"], pause.Source["line"], pause.Source["column"])
		}
		if pause.CallAccess != nil {
			_, _ = fmt.Fprintf(writer, "Call: kind=%s callee=%s code=%s input=%s\n", pause.CallAccess.Kind, pause.CallAccess.Callee, pause.CallAccess.CodeAddr, pause.CallAccess.Input)
		}
		if pause.StorageAccess != nil {
			_, _ = fmt.Fprintf(writer, "StorageAccess: scope=%s slot=%s value=%s write=%t\n", pause.StorageAccess.Scope, pause.StorageAccess.Slot, pause.StorageAccess.Value, pause.StorageAccess.IsWrite)
		}
		if pause.MemoryAccess != nil {
			_, _ = fmt.Fprintf(writer, "MemoryAccess: offset=%#x size=%d write=%t data=%s\n", pause.MemoryAccess.Offset, pause.MemoryAccess.Size, pause.MemoryAccess.IsWrite, pause.MemoryAccess.Data)
		}
		return
	}
	if session.done {
		if session.result == nil {
			_, _ = fmt.Fprintln(writer, "Execution finished with no result.")
			return
		}
		_, _ = fmt.Fprintf(writer, "Execution finished. status=%v gasUsed=%d returnData=%s\n", session.result.Status, session.result.GasUsed, encodeCLIBytes(session.result.ReturnData))
		if session.result.Err != nil {
			_, _ = fmt.Fprintf(writer, "Error: %v\n", session.result.Err)
		}
		if session.exact || session.limitation != "" {
			_, _ = fmt.Fprintf(writer, "Replay exact=%t limitation=%s\n", session.exact, displayOrDefault(session.limitation, "<none>"))
		}
		return
	}
	_, _ = fmt.Fprintln(writer, "Session is ready. Use run to start execution.")
}

func printStorageVariables(writer io.Writer, session *oneshotSession) error {
	if session.current == nil {
		return fmt.Errorf("no paused state")
	}
	if len(session.current.Storage) == 0 && len(session.current.Transient) == 0 {
		_, _ = fmt.Fprintln(writer, "No storage variables available for current frame.")
		return nil
	}
	for _, item := range session.current.Storage {
		_, _ = fmt.Fprintf(writer, "storage %s slot=%s value=%s type=%s\n", item.Name, item.Slot, item.Value, item.Type)
	}
	for _, item := range session.current.Transient {
		_, _ = fmt.Fprintf(writer, "transient %s slot=%s value=%s type=%s\n", item.Name, item.Slot, item.Value, item.Type)
	}
	return nil
}

func printStorageValue(writer io.Writer, session *oneshotSession, identifier string) error {
	if session.current == nil {
		return fmt.Errorf("no paused state")
	}
	items := append(append([]oneshotVariableValue(nil), session.current.Storage...), session.current.Transient...)
	for _, item := range items {
		if item.Name == identifier || item.Slot == identifier {
			_, _ = fmt.Fprintf(writer, "%s %s = %s (%s)\n", item.Scope, item.Name, item.Value, item.Type)
			return nil
		}
	}
	if session.current.StorageAccess != nil && (session.current.StorageAccess.Slot == identifier || strings.EqualFold(session.current.StorageAccess.Slot, identifier)) {
		_, _ = fmt.Fprintf(writer, "last-access slot %s = %s\n", session.current.StorageAccess.Slot, session.current.StorageAccess.Value)
		return nil
	}
	return fmt.Errorf("storage variable or slot %q not found in current pause", identifier)
}

func printMemoryWords(writer io.Writer, session *oneshotSession, limit int) error {
	if session.current == nil {
		return fmt.Errorf("no paused state")
	}
	if limit <= 0 {
		limit = 16
	}
	count := 0
	for offset := 0; offset < len(session.current.MemorySnapshot); offset += 32 {
		end := offset + 32
		if end > len(session.current.MemorySnapshot) {
			end = len(session.current.MemorySnapshot)
		}
		chunk := session.current.MemorySnapshot[offset:end]
		if isZeroBytes(chunk) {
			continue
		}
		_, _ = fmt.Fprintf(writer, "%#x:\t%s\n", offset, encodeCLIBytes(chunk))
		count++
		if count >= limit {
			break
		}
	}
	if count == 0 {
		_, _ = fmt.Fprintln(writer, "No non-zero memory words.")
	}
	return nil
}

func printMemoryRange(writer io.Writer, session *oneshotSession, offset uint64, size uint64) error {
	if session.current == nil {
		return fmt.Errorf("no paused state")
	}
	data := sliceMemory(session.current.MemorySnapshot, offset, size)
	_, _ = fmt.Fprintf(writer, "%#x:\t%s\n", offset, encodeCLIBytes(data))
	return nil
}

func applyMemoryMutationToPause(pause *oneshotPause, mutation oneshotMutation) {
	if pause == nil {
		return
	}
	end := int(mutation.Offset) + len(mutation.Data)
	if end > len(pause.MemorySnapshot) {
		expanded := make([]byte, end)
		copy(expanded, pause.MemorySnapshot)
		pause.MemorySnapshot = expanded
	}
	copy(pause.MemorySnapshot[int(mutation.Offset):], mutation.Data)
	pause.MemorySize = len(pause.MemorySnapshot)
	if pause.MemoryAccess != nil && rangesOverlap(mutation.Offset, uint64(len(mutation.Data)), pause.MemoryAccess.Offset, pause.MemoryAccess.Size) {
		pause.MemoryAccess.Data = encodeCLIBytes(sliceMemory(pause.MemorySnapshot, pause.MemoryAccess.Offset, pause.MemoryAccess.Size))
	}
}
