package engine

import "fmt"

func (evm *EVM) hookContextForOpcode(op byte) *HookContext {
	return &HookContext{
		State: NewReadOnlyState(evm.state),
		Opcode: &OpcodeInfo{
			PC:           evm.pc,
			Op:           op,
			GasRemaining: evm.gasMeter.Gas(),
			GasCost:      GasCosts[op],
		},
	}
}

func (evm *EVM) dispatchMemoryAccessHook(op byte, hookType HookType, access *MemoryAccessInfo) ([]byte, bool, error) {
	if evm.hooks == nil {
		return nil, false, nil
	}
	ctx := evm.hookContextForOpcode(op)
	ctx.Memory = access
	for _, hook := range evm.hooks.HooksFor(hookType) {
		res, err := hook.Fire(ctx)
		if err != nil {
			return nil, false, err
		}
		if res == nil {
			continue
		}
		switch res.Action {
		case ActionContinue:
			continue
		case ActionReplaceResult:
			data, ok := res.ResultData.([]byte)
			if !ok {
				return nil, false, fmt.Errorf("memory hook replacement must be []byte, got %T", res.ResultData)
			}
			return data, true, nil
		case ActionHalt:
			return nil, false, res.Err
		case ActionRevert:
			return nil, false, ErrExecutionReverted
		}
	}
	return nil, false, nil
}

func (evm *EVM) dispatchStorageAccessHook(op byte, hookType HookType, access *StorageAccessInfo) (Hash, bool, error) {
	if evm.hooks == nil {
		return Hash{}, false, nil
	}
	ctx := evm.hookContextForOpcode(op)
	ctx.Storage = access
	for _, hook := range evm.hooks.HooksFor(hookType) {
		res, err := hook.Fire(ctx)
		if err != nil {
			return Hash{}, false, err
		}
		if res == nil {
			continue
		}
		switch res.Action {
		case ActionContinue:
			continue
		case ActionReplaceResult:
			value, ok := res.ResultData.(Hash)
			if !ok {
				return Hash{}, false, fmt.Errorf("storage hook replacement must be Hash, got %T", res.ResultData)
			}
			return value, true, nil
		case ActionHalt:
			return Hash{}, false, res.Err
		case ActionRevert:
			return Hash{}, false, ErrExecutionReverted
		}
	}
	return Hash{}, false, nil
}

func (evm *EVM) dispatchCallHook(op byte, hookType HookType, call *CallInfo) error {
	if evm.hooks == nil {
		return nil
	}
	ctx := evm.hookContextForOpcode(op)
	ctx.Call = call
	for _, hook := range evm.hooks.HooksFor(hookType) {
		res, err := hook.Fire(ctx)
		if err != nil {
			return err
		}
		if res == nil {
			continue
		}
		switch res.Action {
		case ActionContinue:
			continue
		case ActionHalt:
			return res.Err
		case ActionReplaceResult:
			return fmt.Errorf("call hook replacement is not supported")
		case ActionRevert:
			return ErrExecutionReverted
		}
	}
	return nil
}

func normalizeAccessData(data []byte, size uint64) []byte {
	if size == 0 {
		return nil
	}
	if uint64(len(data)) == size {
		copied := make([]byte, len(data))
		copy(copied, data)
		return copied
	}
	out := make([]byte, size)
	copy(out, data)
	return out
}
