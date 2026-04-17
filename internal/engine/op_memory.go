package engine

import "math/big"

func opMload(evm *EVM) error {
	offset := wordToUint64(evm.stack.Pop())
	access := &MemoryAccessInfo{Offset: offset, Size: 32, IsWrite: false}
	replaced, handled, err := evm.dispatchMemoryAccessHook(0x51, HookTypeMemoryRead, access)
	if err != nil {
		return err
	}
	data := normalizeAccessData(replaced, 32)
	if !handled {
		evm.memory.Resize(offset + 32)
		data = normalizeAccessData(evm.memory.Get(offset, 32), 32)
	}
	var w Word
	copy(w[:], data)
	evm.stack.Push(w)
	evm.pc++
	return nil
}

func opMstore(evm *EVM) error {
	offset := wordToUint64(evm.stack.Pop())
	value := evm.stack.Pop()
	data := normalizeAccessData(value[:], 32)
	access := &MemoryAccessInfo{Offset: offset, Size: 32, Data: data, IsWrite: true}
	if replaced, handled, err := evm.dispatchMemoryAccessHook(0x52, HookTypeMemoryWrite, access); err != nil {
		return err
	} else if handled {
		data = normalizeAccessData(replaced, 32)
	}
	evm.memory.Set(offset, data)
	evm.pc++
	return nil
}

func opMstore8(evm *EVM) error {
	offset := wordToUint64(evm.stack.Pop())
	value := evm.stack.Pop()
	data := []byte{value[31]}
	access := &MemoryAccessInfo{Offset: offset, Size: 1, Data: data, IsWrite: true}
	if replaced, handled, err := evm.dispatchMemoryAccessHook(0x53, HookTypeMemoryWrite, access); err != nil {
		return err
	} else if handled {
		data = normalizeAccessData(replaced, 1)
	}
	evm.memory.Set(offset, data)
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
	readAccess := &MemoryAccessInfo{Offset: src, Size: length, IsWrite: false}
	replaced, handled, err := evm.dispatchMemoryAccessHook(0x5E, HookTypeMemoryRead, readAccess)
	if err != nil {
		return err
	}
	data := normalizeAccessData(replaced, length)
	if !handled {
		data = normalizeAccessData(evm.memory.Get(src, length), length)
	}
	writeAccess := &MemoryAccessInfo{Offset: dst, Size: length, Data: data, IsWrite: true}
	if replaced, handled, err := evm.dispatchMemoryAccessHook(0x5E, HookTypeMemoryWrite, writeAccess); err != nil {
		return err
	} else if handled {
		data = normalizeAccessData(replaced, length)
	}
	evm.memory.Set(dst, data)
	evm.pc++
	return nil
}
