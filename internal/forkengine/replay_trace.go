package forkengine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"inspethct/internal/engine"
	"inspethct/internal/forkengine/upstream"
)

type ReplayTraceStep struct {
	PC           uint64
	Op           string
	Depth        int
	GasRemaining uint64
	GasCost      uint64
	Error        string
}

type TraceMismatch struct {
	Index    int
	Field    string
	Local    string
	Upstream string
}

type TraceComparison struct {
	Match           bool
	StatusMatch     bool
	ReturnDataMatch bool
	StepCountMatch  bool
	GasMatch        bool
	GasCostMatch    bool
	ErrorMatch      bool
	FirstMismatch   *TraceMismatch
}

type ReceiptComparison struct {
	Match         bool
	StatusMatch   bool
	GasUsedMatch  bool
	LogsMatch     bool
	FirstMismatch *TraceMismatch
}

type ReplayTraceComparison struct {
	Prepared      *PreparedCall
	Transaction   upstream.Transaction
	Receipt       upstream.Receipt
	LocalResult   *engine.ExecutionResult
	LocalTrace    []ReplayTraceStep
	UpstreamTrace upstream.StructuredTrace
	Comparison    TraceComparison
}

func (engineRef *Engine) ReplayTransactionWithTraceComparison(ctx context.Context, txHash engine.Hash, traceCfg upstream.TraceTransactionConfig) (*ReplayTraceComparison, error) {
	traceProvider, ok := engineRef.provider.(upstream.TraceProvider)
	if !ok {
		return nil, errors.New("forkengine: provider does not implement debug trace capabilities")
	}

	prepared, tx, receipt, err := engineRef.PrepareReplay(ctx, txHash)
	if err != nil {
		return nil, err
	}

	collector := newReplayTraceCollector()
	prepared.Config.Hooks = mergeHookRegistries(prepared.Config.Hooks, collector.registry())
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
	collector.applyResult(result)

	upstreamResult, err := traceProvider.DebugTraceTransaction(ctx, txHash, traceCfg)
	if err != nil {
		return nil, err
	}
	structuredTrace, err := upstream.ParseStructuredTrace(upstreamResult)
	if err != nil {
		return nil, err
	}

	comparison := CompareReplayTrace(collector.steps, result, structuredTrace)
	return &ReplayTraceComparison{
		Prepared:      prepared,
		Transaction:   tx,
		Receipt:       receipt,
		LocalResult:   result,
		LocalTrace:    append([]ReplayTraceStep(nil), collector.steps...),
		UpstreamTrace: structuredTrace,
		Comparison:    comparison,
	}, nil
}

func CompareReplayTrace(local []ReplayTraceStep, localResult *engine.ExecutionResult, upstreamTrace upstream.StructuredTrace) TraceComparison {
	comparison := TraceComparison{Match: true, StatusMatch: true, ReturnDataMatch: true, StepCountMatch: true, GasMatch: true, GasCostMatch: true, ErrorMatch: true}

	localFailed := localResult != nil && localResult.Status != engine.StatusSuccess
	if localFailed != upstreamTrace.Failed {
		comparison.StatusMatch = false
		comparison.Match = false
		comparison.FirstMismatch = &TraceMismatch{
			Index:    -1,
			Field:    "status",
			Local:    executionStatusString(localResult),
			Upstream: upstreamFailureString(upstreamTrace.Failed),
		}
		return comparison
	}

	localReturn := ""
	if localResult != nil && len(localResult.ReturnData) > 0 {
		localReturn = upstream.EncodeHex(localResult.ReturnData)
	}
	upstreamReturn := normalizeTraceHex(upstreamTrace.ReturnValue)
	if localReturn != upstreamReturn {
		comparison.ReturnDataMatch = false
		comparison.Match = false
		comparison.FirstMismatch = &TraceMismatch{
			Index:    -1,
			Field:    "returnData",
			Local:    localReturn,
			Upstream: upstreamReturn,
		}
		return comparison
	}

	localNormalized := normalizeReplayTrace(local)
	upstreamNormalized := normalizeUpstreamTrace(upstreamTrace.StructLogs)
	if len(localNormalized) != len(upstreamNormalized) {
		comparison.StepCountMatch = false
		comparison.Match = false
		comparison.FirstMismatch = &TraceMismatch{
			Index:    -1,
			Field:    "step-count",
			Local:    fmt.Sprintf("%d", len(localNormalized)),
			Upstream: fmt.Sprintf("%d", len(upstreamNormalized)),
		}
		return comparison
	}

	for index := range localNormalized {
		if localNormalized[index].PC != upstreamNormalized[index].PC {
			comparison.Match = false
			comparison.FirstMismatch = &TraceMismatch{
				Index:    index,
				Field:    "pc",
				Local:    fmt.Sprintf("%d", localNormalized[index].PC),
				Upstream: fmt.Sprintf("%d", upstreamNormalized[index].PC),
			}
			return comparison
		}
		if localNormalized[index].Op != upstreamNormalized[index].Op {
			comparison.Match = false
			comparison.FirstMismatch = &TraceMismatch{
				Index:    index,
				Field:    "op",
				Local:    localNormalized[index].Op,
				Upstream: upstreamNormalized[index].Op,
			}
			return comparison
		}
		if localNormalized[index].Depth != upstreamNormalized[index].Depth {
			comparison.Match = false
			comparison.FirstMismatch = &TraceMismatch{
				Index:    index,
				Field:    "depth",
				Local:    fmt.Sprintf("%d", localNormalized[index].Depth),
				Upstream: fmt.Sprintf("%d", upstreamNormalized[index].Depth),
			}
			return comparison
		}
		if upstreamNormalized[index].GasRemaining != nil && (localNormalized[index].GasRemaining == nil || *localNormalized[index].GasRemaining != *upstreamNormalized[index].GasRemaining) {
			comparison.Match = false
			comparison.GasMatch = false
			comparison.FirstMismatch = &TraceMismatch{
				Index:    index,
				Field:    "gas",
				Local:    formatOptionalUint64(localNormalized[index].GasRemaining),
				Upstream: fmt.Sprintf("%d", *upstreamNormalized[index].GasRemaining),
			}
			return comparison
		}
		if upstreamNormalized[index].GasCost != nil && (localNormalized[index].GasCost == nil || *localNormalized[index].GasCost != *upstreamNormalized[index].GasCost) {
			comparison.Match = false
			comparison.GasCostMatch = false
			comparison.FirstMismatch = &TraceMismatch{
				Index:    index,
				Field:    "gasCost",
				Local:    formatOptionalUint64(localNormalized[index].GasCost),
				Upstream: fmt.Sprintf("%d", *upstreamNormalized[index].GasCost),
			}
			return comparison
		}
		if normalizeStepError(localNormalized[index].Error) != normalizeStepError(upstreamNormalized[index].Error) {
			comparison.Match = false
			comparison.ErrorMatch = false
			comparison.FirstMismatch = &TraceMismatch{
				Index:    index,
				Field:    "error",
				Local:    localNormalized[index].Error,
				Upstream: upstreamNormalized[index].Error,
			}
			return comparison
		}
	}

	return comparison
}

func CompareReplayToReceipt(tx upstream.Transaction, receipt upstream.Receipt, result *engine.ExecutionResult) ReceiptComparison {
	comparison := ReceiptComparison{Match: true, StatusMatch: true, GasUsedMatch: true, LogsMatch: true}
	if result == nil {
		comparison.Match = false
		comparison.FirstMismatch = &TraceMismatch{Index: -1, Field: "result", Local: "nil", Upstream: "non-nil receipt"}
		return comparison
	}

	localSuccess := result.Status == engine.StatusSuccess
	upstreamSuccess := receipt.Status == nil || *receipt.Status == 1
	if localSuccess != upstreamSuccess {
		comparison.Match = false
		comparison.StatusMatch = false
		comparison.FirstMismatch = &TraceMismatch{Index: -1, Field: "status", Local: executionStatusString(result), Upstream: upstreamFailureString(!upstreamSuccess)}
		return comparison
	}

	localGasUsed := replayGasUsed(tx, result)
	if localGasUsed != receipt.GasUsed {
		comparison.Match = false
		comparison.GasUsedMatch = false
		comparison.FirstMismatch = &TraceMismatch{Index: -1, Field: "gasUsed", Local: fmt.Sprintf("%d", localGasUsed), Upstream: fmt.Sprintf("%d", receipt.GasUsed)}
		return comparison
	}

	if len(result.Logs) != len(receipt.Logs) {
		comparison.Match = false
		comparison.LogsMatch = false
		comparison.FirstMismatch = &TraceMismatch{Index: -1, Field: "logs-count", Local: fmt.Sprintf("%d", len(result.Logs)), Upstream: fmt.Sprintf("%d", len(receipt.Logs))}
		return comparison
	}
	for index := range result.Logs {
		if result.Logs[index].Address != receipt.Logs[index].Address {
			comparison.Match = false
			comparison.LogsMatch = false
			comparison.FirstMismatch = &TraceMismatch{Index: index, Field: "log-address", Local: fmt.Sprintf("%x", result.Logs[index].Address), Upstream: fmt.Sprintf("%x", receipt.Logs[index].Address)}
			return comparison
		}
		if !equalTopics(result.Logs[index].Topics, receipt.Logs[index].Topics) {
			comparison.Match = false
			comparison.LogsMatch = false
			comparison.FirstMismatch = &TraceMismatch{Index: index, Field: "log-topics", Local: fmt.Sprintf("%d", len(result.Logs[index].Topics)), Upstream: fmt.Sprintf("%d", len(receipt.Logs[index].Topics))}
			return comparison
		}
		if upstream.EncodeHex(result.Logs[index].Data) != upstream.EncodeHex(receipt.Logs[index].Data) {
			comparison.Match = false
			comparison.LogsMatch = false
			comparison.FirstMismatch = &TraceMismatch{Index: index, Field: "log-data", Local: upstream.EncodeHex(result.Logs[index].Data), Upstream: upstream.EncodeHex(receipt.Logs[index].Data)}
			return comparison
		}
	}

	return comparison
}

type replayTraceCollector struct {
	steps []ReplayTraceStep
	reg   *engine.SimpleHookRegistry
}

func newReplayTraceCollector() *replayTraceCollector {
	collector := &replayTraceCollector{reg: engine.NewSimpleHookRegistry()}
	_ = collector.reg.Register(&traceCaptureHook{collector: collector})
	return collector
}

func (collector *replayTraceCollector) applyResult(result *engine.ExecutionResult) {
	if result == nil || len(collector.steps) == 0 {
		return
	}
	if result.Status == engine.StatusSuccess {
		return
	}
	if result.Err != nil {
		collector.steps[len(collector.steps)-1].Error = result.Err.Error()
	} else if result.Status != engine.StatusSuccess {
		collector.steps[len(collector.steps)-1].Error = executionStatusString(result)
	}
}

func (collector *replayTraceCollector) registry() engine.HookRegistry {
	return collector.reg
}

type traceCaptureHook struct {
	collector *replayTraceCollector
}

func (hook *traceCaptureHook) Type() engine.HookType { return engine.HookTypeStep }
func (hook *traceCaptureHook) OneTime() bool         { return false }
func (hook *traceCaptureHook) ID() string            { return "forkengine-replay-trace" }

func (hook *traceCaptureHook) Fire(ctx *engine.HookContext) (*engine.HookResult, error) {
	if ctx == nil || ctx.Opcode == nil || ctx.State == nil {
		return &engine.HookResult{Action: engine.ActionContinue}, nil
	}
	hook.collector.steps = append(hook.collector.steps, ReplayTraceStep{
		PC:           ctx.Opcode.PC,
		Op:           strings.ToUpper(engine.OpcodeName(ctx.Opcode.Op)),
		Depth:        ctx.State.CallDepth(),
		GasRemaining: ctx.Opcode.GasRemaining,
		GasCost:      ctx.Opcode.GasCost,
	})
	return &engine.HookResult{Action: engine.ActionContinue}, nil
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
		return errors.New("forkengine: no hook registry available")
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

type normalizedTraceStep struct {
	PC           uint64
	Op           string
	Depth        int
	GasRemaining *uint64
	GasCost      *uint64
	Error        string
}

func normalizeReplayTrace(steps []ReplayTraceStep) []normalizedTraceStep {
	if len(steps) == 0 {
		return nil
	}
	baseDepth := steps[0].Depth
	normalized := make([]normalizedTraceStep, 0, len(steps))
	for _, step := range steps {
		gasRemaining := step.GasRemaining
		gasCost := step.GasCost
		normalized = append(normalized, normalizedTraceStep{
			PC:           step.PC,
			Op:           strings.ToUpper(step.Op),
			Depth:        step.Depth - baseDepth,
			GasRemaining: &gasRemaining,
			GasCost:      &gasCost,
			Error:        step.Error,
		})
	}
	return normalized
}

func normalizeUpstreamTrace(steps []upstream.StructuredTraceStep) []normalizedTraceStep {
	if len(steps) == 0 {
		return nil
	}
	baseDepth := steps[0].Depth
	normalized := make([]normalizedTraceStep, 0, len(steps))
	for _, step := range steps {
		normalized = append(normalized, normalizedTraceStep{
			PC:           step.PC,
			Op:           strings.ToUpper(step.Op),
			Depth:        step.Depth - baseDepth,
			GasRemaining: step.Gas,
			GasCost:      step.GasCost,
			Error:        step.Error,
		})
	}
	return normalized
}

func executionStatusString(result *engine.ExecutionResult) string {
	if result == nil {
		return "unknown"
	}
	if result.Status == engine.StatusSuccess {
		return "success"
	}
	return fmt.Sprintf("failed(%d)", result.Status)
}

func upstreamFailureString(failed bool) string {
	if failed {
		return "failed"
	}
	return "success"
}

func normalizeTraceHex(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed == "0x" {
		return ""
	}
	if strings.HasPrefix(trimmed, "0x") || strings.HasPrefix(trimmed, "0X") {
		return strings.ToLower(trimmed)
	}
	return "0x" + strings.ToLower(trimmed)
}

func normalizeStepError(value string) string {
	return strings.TrimSpace(strings.ToLower(value))
}

func replayGasUsed(tx upstream.Transaction, result *engine.ExecutionResult) uint64 {
	if result == nil {
		return 0
	}
	intrinsic := uint64(21000)
	for _, item := range tx.Input {
		if item == 0 {
			intrinsic += 4
		} else {
			intrinsic += 16
		}
	}
	spent := intrinsic + result.GasUsed
	refund := result.GasRefund
	refundCap := spent / 5
	if refund > refundCap {
		refund = refundCap
	}
	return spent - refund
}

func equalTopics(left []engine.Hash, right []engine.Hash) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func formatOptionalUint64(value *uint64) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%d", *value)
}
