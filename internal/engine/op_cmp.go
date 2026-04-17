package engine

import "math/big"

func opLt(evm *EVM) error {
	x := evm.stack.Pop().ToBig()
	y := evm.stack.Pop().ToBig()
	if x.Cmp(y) < 0 {
		evm.stack.Push(BigToWord(big.NewInt(1)))
	} else {
		evm.stack.Push(Word{})
	}
	evm.pc++
	return nil
}

func opGt(evm *EVM) error {
	x := evm.stack.Pop().ToBig()
	y := evm.stack.Pop().ToBig()
	if x.Cmp(y) > 0 {
		evm.stack.Push(BigToWord(big.NewInt(1)))
	} else {
		evm.stack.Push(Word{})
	}
	evm.pc++
	return nil
}

func opSlt(evm *EVM) error {
	x := toSigned(evm.stack.Pop().ToBig())
	y := toSigned(evm.stack.Pop().ToBig())
	if x.Cmp(y) < 0 {
		evm.stack.Push(BigToWord(big.NewInt(1)))
	} else {
		evm.stack.Push(Word{})
	}
	evm.pc++
	return nil
}

func opSgt(evm *EVM) error {
	x := toSigned(evm.stack.Pop().ToBig())
	y := toSigned(evm.stack.Pop().ToBig())
	if x.Cmp(y) > 0 {
		evm.stack.Push(BigToWord(big.NewInt(1)))
	} else {
		evm.stack.Push(Word{})
	}
	evm.pc++
	return nil
}

func opEq(evm *EVM) error {
	x := evm.stack.Pop().ToBig()
	y := evm.stack.Pop().ToBig()
	if x.Cmp(y) == 0 {
		evm.stack.Push(BigToWord(big.NewInt(1)))
	} else {
		evm.stack.Push(Word{})
	}
	evm.pc++
	return nil
}

func opIszero(evm *EVM) error {
	x := evm.stack.Pop().ToBig()
	if x.Sign() == 0 {
		evm.stack.Push(BigToWord(big.NewInt(1)))
	} else {
		evm.stack.Push(Word{})
	}
	evm.pc++
	return nil
}

func opAnd(evm *EVM) error {
	x := evm.stack.Pop().ToBig()
	y := evm.stack.Pop().ToBig()
	evm.stack.Push(BigToWord(u256(new(big.Int).And(x, y))))
	evm.pc++
	return nil
}

func opOr(evm *EVM) error {
	x := evm.stack.Pop().ToBig()
	y := evm.stack.Pop().ToBig()
	evm.stack.Push(BigToWord(u256(new(big.Int).Or(x, y))))
	evm.pc++
	return nil
}

func opXor(evm *EVM) error {
	x := evm.stack.Pop().ToBig()
	y := evm.stack.Pop().ToBig()
	evm.stack.Push(BigToWord(u256(new(big.Int).Xor(x, y))))
	evm.pc++
	return nil
}

func opNot(evm *EVM) error {
	x := evm.stack.Pop().ToBig()
	mask := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	evm.stack.Push(BigToWord(u256(new(big.Int).Xor(x, mask))))
	evm.pc++
	return nil
}

func opByte(evm *EVM) error {
	nBig := evm.stack.Pop().ToBig()
	x := evm.stack.Pop()
	if !nBig.IsUint64() || nBig.Uint64() >= 32 {
		evm.stack.Push(Word{})
	} else {
		n := nBig.Uint64()
		var w Word
		w[31] = x[n]
		evm.stack.Push(w)
	}
	evm.pc++
	return nil
}

func opShl(evm *EVM) error {
	shiftBig := evm.stack.Pop().ToBig()
	x := evm.stack.Pop().ToBig()
	if !shiftBig.IsUint64() || shiftBig.Uint64() >= 256 {
		evm.stack.Push(Word{})
	} else {
		evm.stack.Push(BigToWord(u256(new(big.Int).Lsh(x, uint(shiftBig.Uint64())))))
	}
	evm.pc++
	return nil
}

func opShr(evm *EVM) error {
	shiftBig := evm.stack.Pop().ToBig()
	x := evm.stack.Pop().ToBig()
	if !shiftBig.IsUint64() || shiftBig.Uint64() >= 256 {
		evm.stack.Push(Word{})
	} else {
		evm.stack.Push(BigToWord(u256(new(big.Int).Rsh(x, uint(shiftBig.Uint64())))))
	}
	evm.pc++
	return nil
}

func opSar(evm *EVM) error {
	shiftBig := evm.stack.Pop().ToBig()
	x := evm.stack.Pop().ToBig()
	if !shiftBig.IsUint64() || shiftBig.Uint64() >= 256 {
		if x.Bit(255) == 1 {
			evm.stack.Push(BigToWord(uint256Max))
		} else {
			evm.stack.Push(Word{})
		}
	} else {
		x = toSigned(x)
		evm.stack.Push(BigToWord(toUnsigned(new(big.Int).Rsh(x, uint(shiftBig.Uint64())))))
	}
	evm.pc++
	return nil
}

func opClz(evm *EVM) error {
	x := evm.stack.Pop().ToBig()
	count := 0
	for i := 255; i >= 0; i-- {
		if x.Bit(i) == 1 {
			break
		}
		count++
	}
	evm.stack.Push(BigToWord(big.NewInt(int64(count))))
	evm.pc++
	return nil
}
