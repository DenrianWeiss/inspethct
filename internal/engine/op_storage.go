package engine

func opSload(evm *EVM) error {
	slot := WordToHash(evm.stack.Pop())
	addr := evm.state.Contract().Address()
	value := evm.state.Storage().Get(addr, slot)
	evm.stack.Push(HashToWord(value))
	evm.pc++
	return nil
}

func opSstore(evm *EVM) error {
	slot := WordToHash(evm.stack.Pop())
	value := WordToHash(evm.stack.Pop())
	addr := evm.state.Contract().Address()
	if evm.state.Contract().IsStatic() {
		return ErrWriteProtection
	}

	gas := evm.gasMeter.Gas()
	if gas <= GasCallStipend {
		return ErrOutOfGas
	}

	storage := evm.state.Storage()
	current := storage.Get(addr, slot)
	original := storage.Original(addr, slot)

	var cost uint64
	// Cold/warm access (Berlin+)
	al := evm.state.AccessList()
	if !al.IsSlotWarmed(addr, slot) {
		cost += GasColdSload
		al.WarmSlot(addr, slot)
	}

	if original == current {
		if current != value {
			if original == (Hash{}) {
				cost += GasSstoreSet
			} else {
				cost += GasSstoreReset
			}
		} else {
			// No-op: charge warm access (EIP-2200)
			cost += GasWarmAccess
		}
	} else {
		cost += GasWarmAccess
	}

	if err := evm.gasMeter.ConsumeGas(cost); err != nil {
		return err
	}

	// Refund logic
	if current != value {
		if original != (Hash{}) && current != (Hash{}) && value == (Hash{}) {
			evm.gasMeter.RefundGas(RefundSstoreClear)
		}
		if original != (Hash{}) && current == (Hash{}) {
			evm.gasMeter.DeductRefund(RefundSstoreClear)
		}
		if value == original {
			if original == (Hash{}) {
				evm.gasMeter.RefundGas(GasSstoreSet - GasWarmAccess)
			} else {
				evm.gasMeter.RefundGas(GasSstoreReset - GasWarmAccess)
			}
		}
	}

	storage.Set(addr, slot, value)
	evm.pc++
	return nil
}

func opTload(evm *EVM) error {
	slot := WordToHash(evm.stack.Pop())
	addr := evm.state.Contract().Address()
	value := evm.state.TransientStorage().Get(addr, slot)
	evm.stack.Push(HashToWord(value))
	evm.pc++
	return nil
}

func opTstore(evm *EVM) error {
	slot := WordToHash(evm.stack.Pop())
	value := WordToHash(evm.stack.Pop())
	addr := evm.state.Contract().Address()
	if evm.state.Contract().IsStatic() {
		return ErrWriteProtection
	}
	evm.state.TransientStorage().Set(addr, slot, value)
	evm.pc++
	return nil
}
