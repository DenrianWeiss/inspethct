package engine

import (
	"math/big"
)

func opStop(evm *EVM) error {
	return ErrHalt
}

func opAdd(evm *EVM) error {
	x := evm.stack.Pop().ToBig()
	y := evm.stack.Pop().ToBig()
	z := new(big.Int).Add(x, y)
	evm.stack.Push(BigToWord(u256(z)))
	evm.pc++
	return nil
}

func opMul(evm *EVM) error {
	x := evm.stack.Pop().ToBig()
	y := evm.stack.Pop().ToBig()
	z := new(big.Int).Mul(x, y)
	evm.stack.Push(BigToWord(u256(z)))
	evm.pc++
	return nil
}

func opSub(evm *EVM) error {
	x := evm.stack.Pop().ToBig() // top
	y := evm.stack.Pop().ToBig() // second
	z := new(big.Int).Sub(x, y)
	evm.stack.Push(BigToWord(u256(z)))
	evm.pc++
	return nil
}

func opDiv(evm *EVM) error {
	x := evm.stack.Pop().ToBig()
	y := evm.stack.Pop().ToBig()
	if y.Sign() == 0 {
		evm.stack.Push(Word{})
	} else {
		z := new(big.Int).Div(x, y)
		evm.stack.Push(BigToWord(u256(z)))
	}
	evm.pc++
	return nil
}

func opSdiv(evm *EVM) error {
	x := toSigned(evm.stack.Pop().ToBig())
	y := toSigned(evm.stack.Pop().ToBig())
	if y.Sign() == 0 {
		evm.stack.Push(Word{})
	} else {
		z := new(big.Int).Quo(x, y)
		evm.stack.Push(BigToWord(toUnsigned(z)))
	}
	evm.pc++
	return nil
}

func opMod(evm *EVM) error {
	x := evm.stack.Pop().ToBig()
	y := evm.stack.Pop().ToBig()
	if y.Sign() == 0 {
		evm.stack.Push(Word{})
	} else {
		z := new(big.Int).Mod(x, y)
		evm.stack.Push(BigToWord(u256(z)))
	}
	evm.pc++
	return nil
}

func opSmod(evm *EVM) error {
	x := toSigned(evm.stack.Pop().ToBig())
	y := toSigned(evm.stack.Pop().ToBig())
	if y.Sign() == 0 {
		evm.stack.Push(Word{})
	} else {
		z := new(big.Int).Mod(x, y)
		z = z.Rem(x, y)
		evm.stack.Push(BigToWord(toUnsigned(z)))
	}
	evm.pc++
	return nil
}

func opAddmod(evm *EVM) error {
	x := evm.stack.Pop().ToBig()
	y := evm.stack.Pop().ToBig()
	z := evm.stack.Pop().ToBig()
	if z.Sign() == 0 {
		evm.stack.Push(Word{})
	} else {
		res := new(big.Int).Add(x, y)
		res.Mod(res, z)
		evm.stack.Push(BigToWord(u256(res)))
	}
	evm.pc++
	return nil
}

func opMulmod(evm *EVM) error {
	x := evm.stack.Pop().ToBig()
	y := evm.stack.Pop().ToBig()
	z := evm.stack.Pop().ToBig()
	if z.Sign() == 0 {
		evm.stack.Push(Word{})
	} else {
		res := new(big.Int).Mul(x, y)
		res.Mod(res, z)
		evm.stack.Push(BigToWord(u256(res)))
	}
	evm.pc++
	return nil
}

func opExp(evm *EVM) error {
	base := evm.stack.Pop().ToBig() // top
	exp := evm.stack.Pop().ToBig()  // second
	mod := new(big.Int).Lsh(big.NewInt(1), 256)
	z := new(big.Int).Exp(base, exp, mod)
	evm.stack.Push(BigToWord(z))
	evm.pc++
	return nil
}

func opSignextend(evm *EVM) error {
	back := evm.stack.Pop().ToBig()
	x := evm.stack.Pop().ToBig()
	if back.Cmp(big.NewInt(31)) > 0 {
		evm.stack.Push(BigToWord(x))
		evm.pc++
		return nil
	}
	bitpos := uint(back.Uint64()*8 + 7)
	mask := new(big.Int).Lsh(big.NewInt(1), bitpos)
	mask.Sub(mask, big.NewInt(1))
	if x.Bit(int(bitpos)) == 1 {
		x.Or(x, new(big.Int).Not(mask))
		x.And(x, uint256Max)
	} else {
		x.And(x, mask)
	}
	evm.stack.Push(BigToWord(u256(x)))
	evm.pc++
	return nil
}
