package engine

import "math/big"

func opCreate(evm *EVM, create2 bool) error {
	value := evm.stack.Pop().ToBig()
	offset := wordToUint64(evm.stack.Pop())
	size := wordToUint64(evm.stack.Pop())
	var salt Hash
	if create2 {
		salt = WordToHash(evm.stack.Pop())
	}

	if evm.state.Contract().IsStatic() {
		return ErrWriteProtection
	}

	caller := evm.state.Contract().Address()
	account := evm.state.Account()

	if value.Sign() > 0 {
		if account.Balance(caller).Cmp(value) < 0 {
			evm.stack.Push(Word{})
			evm.pc++
			return nil
		}
	}

	initCode := evm.memory.Get(offset, size)

	// Memory expansion
	memGas, _ := evm.memory.ExpandSize(offset, size)

	var gasCost uint64 = GasCreate + memGas
	if create2 {
		words := (size + 31) / 32
		gasCost += words * 6
	}

	// Init code gas (Shanghai+)
	if forkGTE(evm.fork, ForkShanghai) {
		if size > MAX_INIT_CODE_SIZE {
			evm.stack.Push(Word{})
			evm.pc++
			return nil
		}
		gasCost += (size + 31) / 32 * GasInitCodeWord
	}

	if err := evm.gasMeter.ConsumeGas(gasCost); err != nil {
		return err
	}

	// Compute address
	var addr Address
	if create2 {
		addr = create2Address(caller, salt, initCode)
	} else {
		nonce := account.Nonce(caller)
		addr = createAddress(caller, nonce)
		account.IncrementNonce(caller)
	}

	// Address collision check
	if account.HasCodeOrNonce(addr) {
		evm.stack.Push(Word{})
		evm.pc++
		return nil
	}

	// Apply EIP-150: child gas is max(availableGas - availableGas/64)
	availableGas := evm.gasMeter.Gas()
	childGas := maxMessageCallGas(availableGas)

	if err := evm.gasMeter.ConsumeGas(childGas); err != nil {
		return err
	}

	msg := &Message{
		Caller:    caller,
		Callee:    addr,
		Value:     value,
		Gas:       childGas,
		Input:     initCode,
		Code:      initCode,
		CodeAddr:  addr,
		CallDepth: evm.state.CallDepth() + 1,
		Kind:      CallKindCall,
		IsCreate:  true,
	}

	res, err := evm.ExecuteMessage(msg)
	if err != nil && res == nil {
		res = &ExecutionResult{Status: StatusHalt, Err: err}
	}

	// Return unused child gas to the parent's gas pool.
	if res != nil {
		evm.gasMeter.ReturnGas(res.GasRemaining)
	}

	// Merge child refund counter on success; discard on revert/failure.
	if res != nil && res.Status == StatusSuccess {
		evm.gasMeter.RefundGas(res.GasRefund)
	}

	if res != nil {
		if res.Status == StatusSuccess {
			// Store returned init code as deployed code
			code := res.ReturnData
			account.SetCode(addr, code)

			// Check code constraints
			if len(code) > 0 && code[0] == 0xEF && forkGTE(evm.fork, ForkLondon) {
				// Reject
				account.SetCode(addr, nil)
				evm.stack.Push(Word{})
				evm.pc++
				return nil
			}
			if len(code) > MAX_CODE_SIZE {
				account.SetCode(addr, nil)
				evm.stack.Push(Word{})
				evm.pc++
				return nil
			}
			depositGas := uint64(len(code)) * GasCodeDeposit
			if evm.gasMeter.Gas() < depositGas {
				account.SetCode(addr, nil)
				evm.stack.Push(Word{})
				evm.pc++
				return nil
			}
			evm.gasMeter.ConsumeGas(depositGas)
		}

		if res.Status == StatusSuccess {
			var w Word
			copy(w[12:], addr[:])
			evm.stack.Push(w)
		} else {
			evm.stack.Push(Word{})
		}
	} else {
		evm.stack.Push(Word{})
	}

	evm.pc++
	return nil
}

func opCall(evm *EVM, static, callcode bool) error {
	var kind CallKind
	if static {
		kind = CallKindStaticCall
	} else if callcode {
		kind = CallKindCallCode
	} else {
		kind = CallKindCall
	}
	return opCallCommon(evm, kind)
}

func opCallcode(evm *EVM) error {
	return opCall(evm, false, true)
}

func opDelegatecall(evm *EVM) error {
	return opCallCommon(evm, CallKindDelegateCall)
}

func opReturn(evm *EVM) error {
	offset := wordToUint64(evm.stack.Pop())
	size := wordToUint64(evm.stack.Pop())
	evm.returnData = evm.memory.Get(offset, size)
	return ErrHalt
}

func opRevert(evm *EVM) error {
	offset := wordToUint64(evm.stack.Pop())
	size := wordToUint64(evm.stack.Pop())
	evm.returnData = evm.memory.Get(offset, size)
	return ErrExecutionReverted
}

func opInvalid(evm *EVM) error {
	return ErrInvalidOpcode
}

func opSelfdestruct(evm *EVM) error {
	addr := addressFromWord(evm.stack.Pop())
	if evm.state.Contract().IsStatic() {
		return ErrWriteProtection
	}
	var cost uint64 = GasSelfDestruct
	al := evm.state.AccessList()
	if !al.IsAddressWarmed(addr) {
		cost += GasColdAccountAccess
		al.WarmAddress(addr)
	}
	originator := evm.state.Contract().Address()
	acc := evm.state.Account()
	if addr != originator && !acc.Exists(addr) && acc.Balance(originator).Sign() > 0 {
		cost += GasNewAccount
	}
	if err := evm.gasMeter.ConsumeGas(cost); err != nil {
		return err
	}
	balance := acc.Balance(originator)
	if addr != originator && balance.Sign() > 0 {
		acc.AddBalance(addr, balance)
		acc.SetBalance(originator, big.NewInt(0))
	}
	// Record the selfdestruct on the contract that executed it.
	acc.SelfDestruct(originator)
	evm.pc++
	return nil
}

const MAX_CODE_SIZE = 0x6000
const MAX_INIT_CODE_SIZE = 0xC000
