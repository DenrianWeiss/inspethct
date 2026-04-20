package upstream

import (
	"context"
	"fmt"
	"math/big"

	"inspethct/internal/engine"
)

type BlockTag string

const (
	BlockTagLatest    BlockTag = "latest"
	BlockTagEarliest  BlockTag = "earliest"
	BlockTagPending   BlockTag = "pending"
	BlockTagSafe      BlockTag = "safe"
	BlockTagFinalized BlockTag = "finalized"
)

type BlockRef struct {
	Number *big.Int
	Tag    BlockTag
}

func LatestBlock() BlockRef {
	return BlockRef{Tag: BlockTagLatest}
}

func BlockNumber(number uint64) BlockRef {
	return BlockRef{Number: new(big.Int).SetUint64(number)}
}

func (ref BlockRef) Normalize() BlockRef {
	if ref.Number != nil {
		return BlockRef{Number: new(big.Int).Set(ref.Number)}
	}
	if ref.Tag == "" {
		return LatestBlock()
	}
	return ref
}

func (ref BlockRef) Validate() error {
	if ref.Number != nil && ref.Tag != "" {
		return fmt.Errorf("block ref cannot contain both number and tag")
	}
	return nil
}

func (ref BlockRef) IsPinned() bool {
	return ref.Normalize().Number != nil
}

func (ref BlockRef) CacheKey() string {
	normalized := ref.Normalize()
	if normalized.Number != nil {
		return "num:" + normalized.Number.String()
	}
	return "tag:" + string(normalized.Tag)
}

type AccountSnapshot struct {
	Balance  *big.Int
	Nonce    uint64
	Code     []byte
	CodeHash engine.Hash
	Exists   bool
}

func (snapshot AccountSnapshot) Clone() AccountSnapshot {
	clone := AccountSnapshot{
		Nonce:    snapshot.Nonce,
		CodeHash: snapshot.CodeHash,
		Exists:   snapshot.Exists,
	}
	if snapshot.Balance != nil {
		clone.Balance = new(big.Int).Set(snapshot.Balance)
	}
	if snapshot.Code != nil {
		clone.Code = append([]byte(nil), snapshot.Code...)
	}
	return clone
}

type Block struct {
	Number      *big.Int
	Hash        engine.Hash
	ParentHash  engine.Hash
	Timestamp   uint64
	GasLimit    uint64
	BaseFee     *big.Int
	BlobBaseFee *big.Int
	Coinbase    engine.Address
	PrevRandao  engine.Hash
	ChainID     *big.Int
}

func (block Block) Clone() Block {
	clone := block
	if block.Number != nil {
		clone.Number = new(big.Int).Set(block.Number)
	}
	if block.BaseFee != nil {
		clone.BaseFee = new(big.Int).Set(block.BaseFee)
	}
	if block.BlobBaseFee != nil {
		clone.BlobBaseFee = new(big.Int).Set(block.BlobBaseFee)
	}
	if block.ChainID != nil {
		clone.ChainID = new(big.Int).Set(block.ChainID)
	}
	return clone
}

type CallOverrides struct {
	From     engine.Address
	To       engine.Address
	GasLimit uint64
	Value    *big.Int
	Input    []byte
	Block    BlockRef
}

type Provider interface {
	ChainID(ctx context.Context) (*big.Int, error)
	GetBalance(ctx context.Context, addr engine.Address, block BlockRef) (*big.Int, error)
	GetNonce(ctx context.Context, addr engine.Address, block BlockRef) (uint64, error)
	GetCode(ctx context.Context, addr engine.Address, block BlockRef) ([]byte, error)
	GetStorageAt(ctx context.Context, addr engine.Address, slot engine.Hash, block BlockRef) (engine.Hash, error)
	GetBlock(ctx context.Context, block BlockRef) (Block, error)
}

type Transaction struct {
	Hash             engine.Hash
	BlockHash        engine.Hash
	BlockNumber      *big.Int
	From             engine.Address
	To               *engine.Address
	Type             uint64
	Gas              uint64
	GasPrice         *big.Int
	BlobGasFeeCap    *big.Int
	BlobHashes       []engine.Hash
	Input            []byte
	Nonce            uint64
	TransactionIndex *uint64
	Value            *big.Int
}

func (tx Transaction) Clone() Transaction {
	clone := tx
	if tx.BlockNumber != nil {
		clone.BlockNumber = new(big.Int).Set(tx.BlockNumber)
	}
	if tx.To != nil {
		to := *tx.To
		clone.To = &to
	}
	if tx.GasPrice != nil {
		clone.GasPrice = new(big.Int).Set(tx.GasPrice)
	}
	if tx.BlobGasFeeCap != nil {
		clone.BlobGasFeeCap = new(big.Int).Set(tx.BlobGasFeeCap)
	}
	if tx.BlobHashes != nil {
		clone.BlobHashes = append([]engine.Hash(nil), tx.BlobHashes...)
	}
	if tx.Input != nil {
		clone.Input = append([]byte(nil), tx.Input...)
	}
	if tx.TransactionIndex != nil {
		index := *tx.TransactionIndex
		clone.TransactionIndex = &index
	}
	if tx.Value != nil {
		clone.Value = new(big.Int).Set(tx.Value)
	}
	return clone
}

type Receipt struct {
	TransactionHash   engine.Hash
	TransactionIndex  uint64
	BlockHash         engine.Hash
	BlockNumber       *big.Int
	From              engine.Address
	To                *engine.Address
	GasUsed           uint64
	CumulativeGasUsed uint64
	EffectiveGasPrice *big.Int
	ContractAddress   *engine.Address
	Status            *uint64
	Logs              []engine.Log
}

func (receipt Receipt) Clone() Receipt {
	clone := receipt
	if receipt.BlockNumber != nil {
		clone.BlockNumber = new(big.Int).Set(receipt.BlockNumber)
	}
	if receipt.To != nil {
		to := *receipt.To
		clone.To = &to
	}
	if receipt.EffectiveGasPrice != nil {
		clone.EffectiveGasPrice = new(big.Int).Set(receipt.EffectiveGasPrice)
	}
	if receipt.ContractAddress != nil {
		addr := *receipt.ContractAddress
		clone.ContractAddress = &addr
	}
	if receipt.Status != nil {
		status := *receipt.Status
		clone.Status = &status
	}
	if receipt.Logs != nil {
		clone.Logs = append([]engine.Log(nil), receipt.Logs...)
	}
	return clone
}

type TraceTransactionConfig struct {
	Tracer         string
	Timeout        string
	Reexec         *uint64
	DisableStack   bool
	DisableMemory  bool
	DisableStorage bool
}

type TraceTransactionResult map[string]any

type TransactionProvider interface {
	GetTransactionByHash(ctx context.Context, txHash engine.Hash) (Transaction, error)
	GetTransactionReceipt(ctx context.Context, txHash engine.Hash) (Receipt, error)
}

type BlockTransactionsProvider interface {
	GetBlockTransactions(ctx context.Context, block BlockRef) ([]Transaction, error)
}

type TraceProvider interface {
	DebugTraceTransaction(ctx context.Context, txHash engine.Hash, cfg TraceTransactionConfig) (TraceTransactionResult, error)
}
