package engine

import "math/big"

func opMload(evm *EVM) error {
	offset := wordToUint64(evm.stack.Pop())
	evm.memory.Resize(offset + 32)
	w := evm.memory.Word(offset)
	evm.stack.Push(w)
	evm.pc++
	return nil
}

func opMstore(evm *EVM) error {
	offset := wordToUint64(evm.stack.Pop())
	value := evm.stack.Pop()
	evm.memory.Set(offset, value[:])
	evm.pc++
	return nil
}

func opMstore8(evm *EVM) error {
	offset := wordToUint64(evm.stack.Pop())
	value := evm.stack.Pop()
	evm.memory.Set(offset, []byte{value[31]})
	evm.pc++
	return nil
}

func opMsize(evm *EVM) error {
	size := evm.memory.Len()
	evm.stack.Push(BigToWord(big.NewInt(int64(size))))
	evm.pc++
	return nil
}

func opMcopy(evm *EVM) error {
	dst := wordToUint64(evm.stack.Pop())
	src := wordToUint64(evm.stack.Pop())
	length := wordToUint64(evm.stack.Pop())
	data := evm.memory.Get(src, length)
	evm.memory.Set(dst, data)
	evm.pc++
	return nil
}
