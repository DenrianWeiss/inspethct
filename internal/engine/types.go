package engine

import (
	"math/big"
)

// Word is a 256-bit EVM word.
type Word [32]byte

// Address is a 20-byte Ethereum address.
type Address [20]byte

// Hash is a 32-byte hash.
type Hash [32]byte

// BytesToWord converts a byte slice to a Word, left-padding with zeroes.
func BytesToWord(b []byte) Word {
	var w Word
	copy(w[32-len(b):], b)
	return w
}

// WordToHash converts a Word to a Hash.
func WordToHash(w Word) Hash {
	var h Hash
	copy(h[:], w[:])
	return h
}

// HashToWord converts a Hash to a Word.
func HashToWord(h Hash) Word {
	var w Word
	copy(w[:], h[:])
	return w
}

// WordToBig converts a Word to a big.Int.
func (w Word) ToBig() *big.Int {
	return new(big.Int).SetBytes(w[:])
}

// BigToWord converts a big.Int to a Word.
func BigToWord(i *big.Int) Word {
	var w Word
	b := i.Bytes()
	copy(w[32-len(b):], b)
	return w
}

// Fork identifies the Ethereum hard-fork rules to apply during execution.
type Fork string

const (
	ForkLondon    Fork = "london"
	ForkParis     Fork = "paris"
	ForkShanghai  Fork = "shanghai"
	ForkCancun    Fork = "cancun"
	ForkPrague    Fork = "prague"
	ForkAmsterdam Fork = "amsterdam"
	ForkOsaka     Fork = "osaka"
)

// Log represents an EVM LOG operation output.
type Log struct {
	Address Address
	Topics  []Hash
	Data    []byte
}
