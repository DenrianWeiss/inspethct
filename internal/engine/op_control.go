package engine

import "math/big"

func opJump(evm *EVM) error {
	dest := wordToUint64(evm.stack.Pop())
	if !evm.jumpdests[dest] {
		return ErrInvalidJump
	}
	evm.pc = dest
	return nil
}

func opJumpi(evm *EVM) error {
	dest := wordToUint64(evm.stack.Pop())
	cond := evm.stack.Pop().ToBig()
	if cond.Sign() != 0 {
		if !evm.jumpdests[dest] {
			return ErrInvalidJump
		}
		evm.pc = dest
	} else {
		evm.pc++
	}
	return nil
}

func opPc(evm *EVM) error {
	evm.stack.Push(BigToWord(big.NewInt(int64(evm.pc))))
	evm.pc++
	return nil
}

func opGas(evm *EVM) error {
	gas := evm.gasMeter.Gas()
	evm.stack.Push(BigToWord(big.NewInt(int64(gas))))
	evm.pc++
	return nil
}

func opJumpdest(evm *EVM) error {
	evm.pc++
	return nil
}
