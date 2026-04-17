package engine

import (
	"bytes"
	"encoding/binary"
	"math/big"
)

// CallKind identifies the type of message call.
type CallKind int

const (
	CallKindCall CallKind = iota
	CallKindCallCode
	CallKindDelegateCall
	CallKindStaticCall
)

// Message represents a message call or contract creation.
type Message struct {
	Caller    Address
	Callee    Address
	Value     *big.Int
	Gas       uint64
	Input     []byte
	Code      []byte
	CodeAddr  Address
	IsStatic  bool
	CallDepth int
	Kind      CallKind
	IsCreate  bool
	Salt      Hash // For CREATE2
}

// maxMessageCallGas applies the EIP-150 all-but-1/64th rule.
func maxMessageCallGas(gas uint64) uint64 {
	return gas - (gas / 64)
}

// callGas returns the gas available for a child call.
func callGas(availableGas, requestedGas uint64) uint64 {
	max := maxMessageCallGas(availableGas)
	if requestedGas > max {
		return max
	}
	return requestedGas
}

// ExecuteMessage executes a message call and returns the result.
func (evm *EVM) ExecuteMessage(msg *Message) (*ExecutionResult, error) {
	if msg.CallDepth >= 1024 {
		return &ExecutionResult{Status: StatusCallDepthExceeded, GasRemaining: msg.Gas}, nil
	}

	if msg.IsStatic && msg.Value != nil && msg.Value.Sign() > 0 {
		return &ExecutionResult{Status: StatusHalt, Err: ErrWriteProtection}, nil
	}

	account := evm.state.Account()
	if msg.Value != nil && msg.Value.Sign() > 0 {
		if account.Balance(msg.Caller).Cmp(msg.Value) < 0 {
			return &ExecutionResult{Status: StatusInsufficientBalance, GasRemaining: msg.Gas}, nil
		}
	}

	// Take snapshots before any revertible state changes.
	accSnap := account.Snapshot()
	stoSnap := evm.state.Storage().Snapshot()

	if msg.Value != nil && msg.Value.Sign() > 0 {
		account.SubBalance(msg.Caller, msg.Value)
		account.AddBalance(msg.Callee, msg.Value)
	}

	// For contract creation, create the target account if it does not exist.
	if msg.IsCreate && !account.Exists(msg.Callee) {
		account.CreateAccount(msg.Callee)
	}

	// Load code
	code := msg.Code
	if len(code) == 0 {
		code = account.Code(msg.CodeAddr)
		msg.Code = code // FIX: populate msg.Code so childState uses the loaded code
	}

	// Build child state
	childState := evm.newChildState(msg)
	childEVM := NewEVM(childState, evm.fork)
	childEVM.returnData = evm.returnData // inherit return data buffer initially
	childEVM.SetHooks(evm.hooks)         // propagate hooks/breakpoints to child

	res, err := childEVM.Run(code)
	if err != nil && res == nil {
		res = &ExecutionResult{Status: StatusHalt, Err: err}
	}

	if res != nil {
		if res.Status == StatusSuccess {
			// Merge child logs into parent.
			for _, log := range childState.Logs() {
				evm.state.AddLog(log)
			}
		} else {
			// Revert all state changes on failure or revert.
			account.RevertToSnapshot(accSnap)
			evm.state.Storage().RevertToSnapshot(stoSnap)
			// Non-revert errors consume all gas given to the child.
			if res.Status != StatusRevert {
				res.GasRemaining = 0
			}
		}
	}

	return res, err
}

// newChildState creates a child EVMState from a message.
func (evm *EVM) newChildState(msg *Message) EVMState {
	parent := evm.state

	var calleeAddr Address
	var callerAddr Address
	var value *big.Int

	switch msg.Kind {
	case CallKindDelegateCall:
		calleeAddr = parent.Contract().Address()
		callerAddr = parent.Contract().Caller()
		value = parent.Contract().CallValue()
	case CallKindCallCode:
		calleeAddr = parent.Contract().Address()
		callerAddr = parent.Contract().Address()
		value = msg.Value
	default:
		calleeAddr = msg.Callee
		callerAddr = msg.Caller
		value = msg.Value
	}

	if value == nil {
		value = big.NewInt(0)
	}

	contract := &SimpleContract{
		AddressVal:   calleeAddr,
		CallerVal:    callerAddr,
		CallValueVal: value,
		CallInputVal: msg.Input,
		CodeVal:      msg.Code,
		CodeHashVal:  hashCode(msg.Code),
		CodeAddrVal:  msg.CodeAddr,
		IsStaticVal:  msg.IsStatic,
	}

	childGasMeter := NewGasMeter(msg.Gas)
	childAccessList := NewSimpleAccessList()
	// Seed child access list with parent's warmed addresses/slots
	// (simplified: just share the same access list instance for correctness)
	childAccessList = parent.AccessList().(*SimpleAccessList)

	return NewEVMState(
		NewStack(),
		NewMemory(),
		parent.Storage(),
		parent.TransientStorage(),
		parent.Account(),
		childGasMeter,
		parent.BlockContext(),
		parent.TxContext(),
		contract,
		childAccessList,
	)
}

// opCallCommon handles the common logic for all CALL-family opcodes.
func opCallCommon(evm *EVM, kind CallKind) error {
	gas := wordToUint64(evm.stack.Pop())
	addr := addressFromWord(evm.stack.Pop())

	var value *big.Int
	if kind == CallKindCall || kind == CallKindCallCode {
		value = evm.stack.Pop().ToBig()
	} else {
		value = big.NewInt(0)
	}

	inOffset := wordToUint64(evm.stack.Pop())
	inSize := wordToUint64(evm.stack.Pop())
	outOffset := wordToUint64(evm.stack.Pop())
	outSize := wordToUint64(evm.stack.Pop())

	if evm.state.Contract().IsStatic() && value.Sign() > 0 {
		return ErrWriteProtection
	}

	caller := evm.state.Contract().Address()
	callee := addr
	codeAddr := addr
	if kind == CallKindCallCode || kind == CallKindDelegateCall {
		callee = caller
	}

	input := evm.memory.Get(inOffset, inSize)

	// Dynamic gas calculation
	var extraGas uint64
	al := evm.state.AccessList()
	if al.IsAddressWarmed(addr) {
		extraGas += GasWarmAccess
	} else {
		extraGas += GasColdAccountAccess
		al.WarmAddress(addr)
	}
	if kind != CallKindDelegateCall && kind != CallKindStaticCall {
		if value.Sign() > 0 {
			extraGas += GasCallValue
		}
		acc := evm.state.Account()
		if acc.IsEmpty(addr) && value.Sign() > 0 {
			extraGas += GasNewAccount
		}
	}

	// Memory expansion for input and output (charge for the larger of the two)
	memGasIn, memSizeIn := evm.memory.ExpandSize(inOffset, inSize)
	memGasOut, memSizeOut := evm.memory.ExpandSize(outOffset, outSize)
	if memSizeIn > memSizeOut {
		extraGas += memGasIn
	} else {
		extraGas += memGasOut
	}

	availableGas := evm.gasMeter.Gas()
	if availableGas < extraGas {
		return ErrOutOfGas
	}
	childGas := callGas(availableGas-extraGas, gas)
	chargedChildGas := childGas

	// Add stipend for value-transfer calls (go-ethereum adds it to child gas)
	if (kind == CallKindCall || kind == CallKindCallCode) && value.Sign() > 0 {
		childGas += GasCallStipend
	}

	// Consume both the call overhead and the child gas from the parent.
	// Unused child gas is returned after the call.
	if err := evm.gasMeter.ConsumeGas(extraGas + chargedChildGas); err != nil {
		return err
	}

	if memSizeIn > memSizeOut {
		evm.memory.Resize(memSizeIn)
	} else {
		evm.memory.Resize(memSizeOut)
	}

	msg := &Message{
		Caller:    caller,
		Callee:    callee,
		Value:     value,
		Gas:       childGas,
		Input:     input,
		CodeAddr:  codeAddr,
		IsStatic:  kind == CallKindStaticCall || evm.state.Contract().IsStatic(),
		CallDepth: evm.state.CallDepth() + 1,
		Kind:      kind,
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

	// Copy return data to parent
	if res != nil && len(res.ReturnData) > 0 {
		evm.returnData = res.ReturnData
		toCopy := outSize
		if toCopy > uint64(len(res.ReturnData)) {
			toCopy = uint64(len(res.ReturnData))
		}
		copyData := make([]byte, toCopy)
		copy(copyData, res.ReturnData[:toCopy])
		evm.memory.Set(outOffset, copyData)
	}

	success := res != nil && res.Status == StatusSuccess
	if success {
		evm.stack.Push(BigToWord(big.NewInt(1)))
	} else {
		evm.stack.Push(Word{})
	}
	evm.pc++
	return nil
}

// createAddress computes the address for CREATE.
func createAddress(caller Address, nonce uint64) Address {
	data, _ := rlpEncodeList([]interface{}{
		caller[:],
		nonce,
	})
	h := keccak256(data)
	var addr Address
	copy(addr[:], h[12:])
	return addr
}

// create2Address computes the address for CREATE2.
func create2Address(caller Address, salt Hash, initCode []byte) Address {
	initHash := keccak256(initCode)
	data := make([]byte, 1+20+32+32)
	data[0] = 0xff
	copy(data[1:21], caller[:])
	copy(data[21:53], salt[:])
	copy(data[53:85], initHash)
	h := keccak256(data)
	var addr Address
	copy(addr[:], h[12:])
	return addr
}

// rlpEncodeList performs minimal RLP encoding for [address, nonce].
func rlpEncodeList(items []interface{}) ([]byte, error) {
	buf := new(bytes.Buffer)
	for _, item := range items {
		switch v := item.(type) {
		case []byte:
			if len(v) == 1 && v[0] < 0x80 {
				buf.Write(v)
			} else {
				buf.WriteByte(0x80 + byte(len(v)))
				buf.Write(v)
			}
		case uint64:
			if v == 0 {
				buf.WriteByte(0x80)
			} else {
				b := make([]byte, 8)
				binary.BigEndian.PutUint64(b, v)
				start := 0
				for start < 8 && b[start] == 0 {
					start++
				}
				buf.WriteByte(0x80 + byte(8-start))
				buf.Write(b[start:])
			}
		}
	}
	payload := buf.Bytes()
	res := make([]byte, 1+len(payload))
	res[0] = 0xc0 + byte(len(payload))
	copy(res[1:], payload)
	return res, nil
}
