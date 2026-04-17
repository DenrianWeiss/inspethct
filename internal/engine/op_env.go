package engine

import (
	"math/big"
)

func opAddress(evm *EVM) error {
	addr := evm.state.Contract().Address()
	var w Word
	copy(w[12:], addr[:])
	evm.stack.Push(w)
	evm.pc++
	return nil
}

func opBalance(evm *EVM) error {
	addr := addressFromWord(evm.stack.Pop())
	bal := evm.state.Account().Balance(addr)
	evm.stack.Push(BigToWord(bal))
	evm.pc++
	return nil
}

func opOrigin(evm *EVM) error {
	addr := evm.state.TxContext().Origin()
	var w Word
	copy(w[12:], addr[:])
	evm.stack.Push(w)
	evm.pc++
	return nil
}

func opCaller(evm *EVM) error {
	addr := evm.state.Contract().Caller()
	var w Word
	copy(w[12:], addr[:])
	evm.stack.Push(w)
	evm.pc++
	return nil
}

func opCallvalue(evm *EVM) error {
	val := evm.state.Contract().CallValue()
	evm.stack.Push(BigToWord(val))
	evm.pc++
	return nil
}

func opCalldataload(evm *EVM) error {
	offset := wordToUint64(evm.stack.Pop())
	input := evm.state.Contract().CallInput()
	var w Word
	if offset < uint64(len(input)) {
		end := offset + 32
		if end > uint64(len(input)) {
			end = uint64(len(input))
		}
		copy(w[0:end-offset], input[offset:end])
	}
	evm.stack.Push(w)
	evm.pc++
	return nil
}

func opCalldatasize(evm *EVM) error {
	size := len(evm.state.Contract().CallInput())
	evm.stack.Push(BigToWord(big.NewInt(int64(size))))
	evm.pc++
	return nil
}

func opCalldatacopy(evm *EVM) error {
	memOffset := wordToUint64(evm.stack.Pop())
	dataOffset := wordToUint64(evm.stack.Pop())
	length := wordToUint64(evm.stack.Pop())
	input := evm.state.Contract().CallInput()
	data := make([]byte, length)
	if dataOffset < uint64(len(input)) {
		end := dataOffset + length
		if end > uint64(len(input)) {
			end = uint64(len(input))
		}
		copy(data, input[dataOffset:end])
	}
	evm.memory.Set(memOffset, data)
	evm.pc++
	return nil
}

func opCodesize(evm *EVM) error {
	size := len(evm.state.Contract().Code())
	evm.stack.Push(BigToWord(big.NewInt(int64(size))))
	evm.pc++
	return nil
}

func opCodecopy(evm *EVM) error {
	memOffset := wordToUint64(evm.stack.Pop())
	codeOffset := wordToUint64(evm.stack.Pop())
	length := wordToUint64(evm.stack.Pop())
	code := evm.state.Contract().Code()
	data := make([]byte, length)
	if codeOffset < uint64(len(code)) {
		end := codeOffset + length
		if end > uint64(len(code)) {
			end = uint64(len(code))
		}
		copy(data, code[codeOffset:end])
	}
	evm.memory.Set(memOffset, data)
	evm.pc++
	return nil
}

func opGasprice(evm *EVM) error {
	price := evm.state.TxContext().GasPrice()
	evm.stack.Push(BigToWord(price))
	evm.pc++
	return nil
}

func opExtcodesize(evm *EVM) error {
	addr := addressFromWord(evm.stack.Pop())
	if evm.isPrecompileAddress(addr) && !evm.state.Account().Exists(addr) {
		evm.stack.Push(Word{})
		evm.pc++
		return nil
	}
	size := evm.state.Account().CodeSize(addr)
	evm.stack.Push(BigToWord(big.NewInt(int64(size))))
	evm.pc++
	return nil
}

func opExtcodecopy(evm *EVM) error {
	addr := addressFromWord(evm.stack.Pop())
	memOffset := wordToUint64(evm.stack.Pop())
	codeOffset := wordToUint64(evm.stack.Pop())
	length := wordToUint64(evm.stack.Pop())
	code := evm.state.Account().Code(addr)
	data := make([]byte, length)
	if codeOffset < uint64(len(code)) {
		end := codeOffset + length
		if end > uint64(len(code)) {
			end = uint64(len(code))
		}
		copy(data, code[codeOffset:end])
	}
	evm.memory.Set(memOffset, data)
	evm.pc++
	return nil
}

func opReturndatasize(evm *EVM) error {
	size := len(evm.returnData)
	evm.stack.Push(BigToWord(big.NewInt(int64(size))))
	evm.pc++
	return nil
}

func opReturndatacopy(evm *EVM) error {
	memOffset := wordToUint64(evm.stack.Pop())
	dataOffset := wordToUint64(evm.stack.Pop())
	length := wordToUint64(evm.stack.Pop())
	if dataOffset+length > uint64(len(evm.returnData)) {
		return ErrReturnDataOutOfBounds
	}
	data := make([]byte, length)
	copy(data, evm.returnData[dataOffset:dataOffset+length])
	evm.memory.Set(memOffset, data)
	evm.pc++
	return nil
}

func opExtcodehash(evm *EVM) error {
	addr := addressFromWord(evm.stack.Pop())
	if evm.isPrecompileAddress(addr) && !evm.state.Account().Exists(addr) {
		evm.stack.Push(Word{})
		evm.pc++
		return nil
	}
	if !evm.state.Account().Exists(addr) {
		evm.stack.Push(Word{})
	} else {
		hash := evm.state.Account().CodeHash(addr)
		evm.stack.Push(HashToWord(hash))
	}
	evm.pc++
	return nil
}

func opBlockhash(evm *EVM) error {
	nBig := evm.stack.Pop().ToBig()
	if !nBig.IsUint64() {
		evm.stack.Push(Word{})
		evm.pc++
		return nil
	}
	n := nBig.Uint64()
	hash := evm.state.BlockContext().BlockHash(n)
	evm.stack.Push(HashToWord(hash))
	evm.pc++
	return nil
}

func opCoinbase(evm *EVM) error {
	addr := evm.state.BlockContext().Coinbase()
	var w Word
	copy(w[12:], addr[:])
	evm.stack.Push(w)
	evm.pc++
	return nil
}

func opTimestamp(evm *EVM) error {
	ts := evm.state.BlockContext().Timestamp()
	evm.stack.Push(BigToWord(big.NewInt(int64(ts))))
	evm.pc++
	return nil
}

func opNumber(evm *EVM) error {
	n := evm.state.BlockContext().Number()
	evm.stack.Push(BigToWord(n))
	evm.pc++
	return nil
}

func opPrevrandao(evm *EVM) error {
	r := evm.state.BlockContext().Random()
	evm.stack.Push(HashToWord(r))
	evm.pc++
	return nil
}

func opGaslimit(evm *EVM) error {
	limit := evm.state.BlockContext().GasLimit()
	evm.stack.Push(BigToWord(big.NewInt(int64(limit))))
	evm.pc++
	return nil
}

func opChainid(evm *EVM) error {
	id := evm.state.BlockContext().ChainID()
	evm.stack.Push(BigToWord(id))
	evm.pc++
	return nil
}

func opSelfbalance(evm *EVM) error {
	addr := evm.state.Contract().Address()
	bal := evm.state.Account().Balance(addr)
	evm.stack.Push(BigToWord(bal))
	evm.pc++
	return nil
}

func opBasefee(evm *EVM) error {
	fee := evm.state.BlockContext().BaseFee()
	evm.stack.Push(BigToWord(fee))
	evm.pc++
	return nil
}

func opBlobhash(evm *EVM) error {
	idx := wordToUint64(evm.stack.Pop())
	hashes := evm.state.TxContext().BlobHashes()
	if idx < uint64(len(hashes)) {
		evm.stack.Push(HashToWord(hashes[idx]))
	} else {
		evm.stack.Push(Word{})
	}
	evm.pc++
	return nil
}

func opBlobbasefee(evm *EVM) error {
	fee := evm.state.BlockContext().BlobBaseFee()
	evm.stack.Push(BigToWord(fee))
	evm.pc++
	return nil
}

func addressFromWord(w Word) Address {
	var addr Address
	copy(addr[:], w[12:])
	return addr
}
