package forkengine

import (
	"math/big"

	"inspethct/internal/engine"
	"inspethct/internal/forkengine/upstream"
)

type forkBlockContext struct {
	block upstream.Block
}

func newBlockContext(block upstream.Block) engine.BlockContext {
	return &forkBlockContext{block: block.Clone()}
}

func (ctx *forkBlockContext) Coinbase() engine.Address { return ctx.block.Coinbase }
func (ctx *forkBlockContext) Timestamp() uint64        { return ctx.block.Timestamp }

func (ctx *forkBlockContext) Number() *big.Int {
	if ctx.block.Number == nil {
		return big.NewInt(0)
	}
	return new(big.Int).Set(ctx.block.Number)
}

func (ctx *forkBlockContext) Difficulty() *big.Int {
	return big.NewInt(0)
}

func (ctx *forkBlockContext) GasLimit() uint64 { return ctx.block.GasLimit }

func (ctx *forkBlockContext) BlockHash(number uint64) engine.Hash {
	if ctx.block.Number != nil && ctx.block.Number.IsUint64() && ctx.block.Number.Uint64() == number {
		return ctx.block.Hash
	}
	return engine.Hash{}
}

func (ctx *forkBlockContext) BaseFee() *big.Int {
	if ctx.block.BaseFee == nil {
		return big.NewInt(0)
	}
	return new(big.Int).Set(ctx.block.BaseFee)
}

func (ctx *forkBlockContext) BlobBaseFee() *big.Int {
	if ctx.block.BlobBaseFee == nil {
		return big.NewInt(0)
	}
	return new(big.Int).Set(ctx.block.BlobBaseFee)
}

func (ctx *forkBlockContext) BlobHash(index uint64) engine.Hash {
	return engine.Hash{}
}

func (ctx *forkBlockContext) Random() engine.Hash { return ctx.block.PrevRandao }

func (ctx *forkBlockContext) ChainID() *big.Int {
	if ctx.block.ChainID == nil {
		return big.NewInt(0)
	}
	return new(big.Int).Set(ctx.block.ChainID)
}

type forkTxContext struct {
	origin   engine.Address
	gasPrice *big.Int
}

func newTxContext(origin engine.Address, gasPrice *big.Int) engine.TxContext {
	if gasPrice == nil {
		gasPrice = big.NewInt(0)
	}
	return &forkTxContext{origin: origin, gasPrice: new(big.Int).Set(gasPrice)}
}

func (ctx *forkTxContext) Origin() engine.Address    { return ctx.origin }
func (ctx *forkTxContext) BlobHashes() []engine.Hash { return nil }

func (ctx *forkTxContext) GasPrice() *big.Int {
	return new(big.Int).Set(ctx.gasPrice)
}

func (ctx *forkTxContext) BlobGasFee() *big.Int {
	return big.NewInt(0)
}
