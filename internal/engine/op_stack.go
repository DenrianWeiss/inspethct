package engine

func opPop(evm *EVM) error {
	evm.stack.Pop()
	evm.pc++
	return nil
}

func opPush0(evm *EVM) error {
	evm.stack.Push(Word{})
	evm.pc++
	return nil
}

func opPush(evm *EVM, code []byte, n int) error {
	var w Word
	start := evm.pc + 1
	end := start + uint64(n)
	if start < uint64(len(code)) {
		if end > uint64(len(code)) {
			end = uint64(len(code))
		}
		copy(w[32-n:], code[start:end])
	}
	evm.stack.Push(w)
	evm.pc += uint64(1 + n)
	return nil
}

func opDup(evm *EVM, n int) error {
	evm.stack.Dup(n)
	evm.pc++
	return nil
}

func opSwap(evm *EVM, n int) error {
	evm.stack.Swap(n)
	evm.pc++
	return nil
}
