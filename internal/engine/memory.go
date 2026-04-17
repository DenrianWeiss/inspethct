package engine

import (
	"math"
)

func ceil32(size uint64) uint64 {
	if size == 0 {
		return 0
	}
	return ((size + 31) / 32) * 32
}

// EVMMemory implements the Memory interface with gas-aware expansion.
type EVMMemory struct {
	store []byte
}

// NewMemory creates a new EVM memory.
func NewMemory() *EVMMemory {
	return &EVMMemory{}
}

func (m *EVMMemory) Len() int {
	return len(m.store)
}

// Get returns memory from offset with the given size, zero-padding if needed.
func (m *EVMMemory) Get(offset, size uint64) []byte {
	if size == 0 {
		return nil
	}
	if offset >= uint64(len(m.store)) {
		return make([]byte, size)
	}
	end := offset + size
	if end > uint64(len(m.store)) {
		out := make([]byte, size)
		copy(out, m.store[offset:])
		return out
	}
	return m.store[offset : offset+size]
}

// Set copies data into memory at the given offset, expanding if necessary.
func (m *EVMMemory) Set(offset uint64, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	end := offset + uint64(len(data))
	if end > uint64(len(m.store)) {
		m.Resize(end)
	}
	copy(m.store[offset:], data)
	return nil
}

// Resize expands memory to at least the given size.
func (m *EVMMemory) Resize(size uint64) error {
	size = ceil32(size)
	if uint64(len(m.store)) < size {
		newStore := make([]byte, size)
		copy(newStore, m.store)
		m.store = newStore
	}
	return nil
}

// Word returns the 32-byte word at the given word offset.
func (m *EVMMemory) Word(offset uint64) Word {
	if offset >= uint64(len(m.store)) {
		return Word{}
	}
	if offset+32 > uint64(len(m.store)) {
		var w Word
		copy(w[:], m.store[offset:])
		return w
	}
	var w Word
	copy(w[:], m.store[offset:offset+32])
	return w
}

// MemoryGasCost calculates the gas cost for expanding memory to a new size.
// Formula: words*3 + words^2/512 where words = ceil32(size)/32.
func MemoryGasCost(size uint64) uint64 {
	if size == 0 {
		return 0
	}
	words := (size + 31) / 32
	linear := words * 3
	quadratic := (words * words) / 512
	return linear + quadratic
}

// MemoryExpansionGas returns the delta gas for expanding from currentSize to newSize.
func MemoryExpansionGas(currentSize, newSize uint64) uint64 {
	if newSize <= currentSize {
		return 0
	}
	return MemoryGasCost(newSize) - MemoryGasCost(currentSize)
}

// ExpandSize returns the gas cost and the new memory size needed to access
// memory from offset to offset+size. If size is 0, returns (0, currentSize).
func (m *EVMMemory) ExpandSize(offset, size uint64) (gasCost uint64, newSize uint64) {
	if size == 0 {
		return 0, uint64(len(m.store))
	}
	end := offset + size
	// Check overflow
	if end < offset {
		return math.MaxUint64, math.MaxUint64
	}
	end = ceil32(end)
	currentSize := uint64(len(m.store))
	if end > currentSize {
		return MemoryExpansionGas(currentSize, end), end
	}
	return 0, currentSize
}
