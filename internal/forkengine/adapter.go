package forkengine

import (
	"context"
	"fmt"
	"math/big"

	"inspethct/internal/engine"
	"inspethct/internal/forkengine/ext"
	"inspethct/internal/forkengine/upstream"
)

func (engineRef *Engine) ChainID(ctx context.Context) (*big.Int, error) {
	return engineRef.provider.ChainID(ctx)
}

// GetCodeAt returns the deployed bytecode at the given address as of the supplied block ref.
// The block ref may be nil to fall back to the engine's pinned block.
func (engineRef *Engine) GetCodeAt(ctx context.Context, addr engine.Address, block upstream.BlockRef) ([]byte, error) {
	resolvedRef := block
	if resolvedRef.Number == nil && resolvedRef.Tag == "" {
		resolvedRef = engineRef.block
	}
	if code, ok := engineRef.cache.GetCode(addr, resolvedRef); ok && code != nil {
		return append([]byte(nil), code...), nil
	}
	code, err := engineRef.provider.GetCode(ctx, addr, resolvedRef)
	if err != nil {
		return nil, err
	}
	if code == nil {
		code = []byte{}
	}
	engineRef.cache.PutCode(addr, resolvedRef, code)
	return append([]byte(nil), code...), nil
}

// GetStorageSlot returns the storage value at the supplied slot for an address at the given block.
// The block ref may be nil to fall back to the engine's pinned block.
func (engineRef *Engine) GetStorageSlot(ctx context.Context, addr engine.Address, slot engine.Hash, block upstream.BlockRef) (engine.Hash, error) {
	resolvedRef := block
	if resolvedRef.Number == nil && resolvedRef.Tag == "" {
		resolvedRef = engineRef.block
	}
	if value, ok := engineRef.cache.GetStorage(addr, slot, resolvedRef); ok {
		return value, nil
	}
	value, err := engineRef.provider.GetStorageAt(ctx, addr, slot, resolvedRef)
	if err != nil {
		return engine.Hash{}, err
	}
	engineRef.cache.PutStorage(addr, slot, resolvedRef, value)
	return value, nil
}

func (engineRef *Engine) EthCall(ctx context.Context, req ext.EthCallRequest) ([]byte, error) {
	_, result, err := engineRef.ExecuteCall(ctx, CallRequest{
		From:     req.From,
		To:       req.To,
		Input:    req.Input,
		Value:    req.Value,
		GasLimit: req.GasLimit,
		Block:    req.Block,
	})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, fmt.Errorf("forkengine: eth_call returned no execution result")
	}
	return append([]byte(nil), result.ReturnData...), nil
}

func (engineRef *Engine) EstimateGas(ctx context.Context, req ext.EstimateGasRequest) (uint64, error) {
	_, result, err := engineRef.ExecuteCall(ctx, CallRequest{
		From:     req.From,
		To:       req.To,
		Input:    req.Input,
		Value:    req.Value,
		GasLimit: req.GasLimit,
		Block:    req.Block,
	})
	if err != nil {
		return 0, err
	}
	if result == nil {
		return 0, fmt.Errorf("forkengine: estimateGas returned no execution result")
	}
	return result.GasUsed, nil
}

func (engineRef *Engine) TraceCall(ctx context.Context, req ext.TraceCallRequest) (any, error) {
	prepared, err := engineRef.PrepareCall(ctx, CallRequest{
		From:     req.Call.From,
		To:       req.Call.To,
		Input:    req.Call.Input,
		Value:    req.Call.Value,
		GasLimit: req.Call.GasLimit,
		Block:    req.Call.Block,
	})
	if err != nil {
		return nil, err
	}
	collector := newReplayTraceCollector()
	prepared.Config.Hooks = mergeHookRegistries(prepared.Config.Hooks, collector.registry())
	result, err := engineRef.ExecutePreparedCall(prepared)
	if err != nil && result == nil {
		return nil, err
	}
	collector.applyResult(result)
	return collector.steps, nil
}

func (engineRef *Engine) InspectState(ctx context.Context, req ext.StateInspectionRequest) (any, error) {
	balance, err := engineRef.provider.GetBalance(ctx, req.Address, req.Block)
	if err != nil {
		return nil, err
	}
	nonce, err := engineRef.provider.GetNonce(ctx, req.Address, req.Block)
	if err != nil {
		return nil, err
	}
	code, err := engineRef.provider.GetCode(ctx, req.Address, req.Block)
	if err != nil {
		return nil, err
	}
	inspection := map[string]any{
		"address": req.Address,
		"balance": balance.String(),
		"nonce":   nonce,
		"code":    upstream.EncodeHex(code),
	}
	if req.Slot != nil {
		value, err := engineRef.provider.GetStorageAt(ctx, req.Address, *req.Slot, req.Block)
		if err != nil {
			return nil, err
		}
		inspection["slot"] = *req.Slot
		inspection["value"] = value
		return inspection, nil
	}
	inspection["storage"] = map[string]any{}
	return inspection, nil
}

var _ ext.EthRPCAdapter = (*Engine)(nil)
var _ ext.DebugRPCAdapter = (*Engine)(nil)
