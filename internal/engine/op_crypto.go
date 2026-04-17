package engine

import (
	"golang.org/x/crypto/sha3"
)

func keccak256(data []byte) []byte {
	h := sha3.NewLegacyKeccak256()
	h.Write(data)
	return h.Sum(nil)
}

func opKeccak256(evm *EVM) error {
	offset := wordToUint64(evm.stack.Pop())
	size := wordToUint64(evm.stack.Pop())
	evm.memory.Resize(offset + size)
	data := evm.memory.Get(offset, size)
	hash := keccak256(data)
	var w Word
	copy(w[32-len(hash):], hash)
	evm.stack.Push(w)
	evm.pc++
	return nil
}
