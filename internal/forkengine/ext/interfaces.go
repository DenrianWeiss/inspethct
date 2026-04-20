package ext

import (
	"context"
	"math/big"

	"inspethct/internal/engine"
	"inspethct/internal/forkengine/upstream"
)

type EthCallRequest struct {
	From     engine.Address
	To       engine.Address
	Input    []byte
	Value    *big.Int
	GasLimit uint64
	Block    upstream.BlockRef
}

type EstimateGasRequest = EthCallRequest

type TraceCallRequest struct {
	Call  EthCallRequest
	Trace string
}

type StateInspectionRequest struct {
	Address engine.Address
	Slot    *engine.Hash
	Block   upstream.BlockRef
}

type EthRPCAdapter interface {
	EthCall(ctx context.Context, req EthCallRequest) ([]byte, error)
	EstimateGas(ctx context.Context, req EstimateGasRequest) (uint64, error)
}

type DebugRPCAdapter interface {
	TraceCall(ctx context.Context, req TraceCallRequest) (any, error)
	InspectState(ctx context.Context, req StateInspectionRequest) (any, error)
}

type HardhatAdapter interface {
	SetBalance(ctx context.Context, addr engine.Address, balance *big.Int) error
	SetCode(ctx context.Context, addr engine.Address, code []byte) error
	SetStorageAt(ctx context.Context, addr engine.Address, slot engine.Hash, value engine.Hash) error
	Snapshot(ctx context.Context) (string, error)
	Revert(ctx context.Context, snapshotID string) (bool, error)
}
