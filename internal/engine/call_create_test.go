package engine

import (
	"math/big"
	"testing"
)

// TestCallSuccess: CALL to a contract that returns 42.
func TestCallSuccess(t *testing.T) {
	// Callee code: PUSH1 0x2A PUSH1 0x00 MSTORE PUSH1 0x20 PUSH1 0x00 RETURN
	calleeCode := []byte{0x60, 0x2A, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xF3}
	// Caller code:
	//   PUSH1 0x00 (retSize)
	//   PUSH1 0x00 (retOffset)
	//   PUSH1 0x00 (inSize)
	//   PUSH1 0x00 (inOffset)
	//   PUSH1 0x00 (value)
	//   PUSH20 callee
	//   PUSH2 0xFFFF (gas)
	//   CALL
	//   STOP
	calleeAddr := Address{0xAB}
	callerCode := []byte{
		0x60, 0x00, // retSize
		0x60, 0x00, // retOffset
		0x60, 0x00, // inSize
		0x60, 0x00, // inOffset
		0x60, 0x00, // value
		0x73,       // PUSH20
	}
	callerCode = append(callerCode, calleeAddr[:]...)
	callerCode = append(callerCode, 0x61, 0xFF, 0xFF, 0xF1, 0x00)

	account := NewInMemoryAccountState()
	account.SetCode(calleeAddr, calleeCode)
	account.SetBalance(Address{0x11}, big.NewInt(1_000_000))

	stack := NewStack()
	memory := NewMemory()
	gasMeter := NewGasMeter(1_000_000)
	storage := NewInMemoryStorage()
	transient := NewInMemoryTransientStorage()
	accessList := NewSimpleAccessList()
	contract := &SimpleContract{
		AddressVal:   Address{0x42},
		CallerVal:    Address{0x11},
		CallValueVal: big.NewInt(0),
		CodeVal:      callerCode,
		CodeAddrVal:  Address{0x42},
	}
	blockCtx := &SimpleBlockContext{NumberVal: big.NewInt(1), ChainIDVal: big.NewInt(1)}
	txCtx := &SimpleTxContext{OriginVal: Address{0x11}}
	state := NewEVMState(stack, memory, storage, transient, account, gasMeter, blockCtx, txCtx, contract, accessList)
	evm := NewEVM(state, ForkLondon)
	_, err := evm.Run(callerCode)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evm.stack.Len() != 1 {
		t.Fatalf("expected 1 stack item, got %d", evm.stack.Len())
	}
	if evm.stack.Peek().ToBig().Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("expected success (1), got %v", evm.stack.Peek().ToBig())
	}
	// Return data should be 0x2A padded to 32 bytes
	retData := evm.returnData
	if len(retData) != 32 || retData[31] != 0x2A {
		t.Fatalf("unexpected return data: %x", retData)
	}
}

// TestCallWithValue verifies value transfer during CALL.
func TestCallWithValue(t *testing.T) {
	// Callee: RETURN(0,0)
	calleeCode := []byte{0x60, 0x00, 0x60, 0x00, 0xF3}
	calleeAddr := Address{0xAB}
	// Caller code: CALL(gas=0xFFFF, addr=callee, value=1000, in=0,0, out=0,0)
	callerCode := []byte{
		0x60, 0x00, // retSize
		0x60, 0x00, // retOffset
		0x60, 0x00, // inSize
		0x60, 0x00, // inOffset
	}
	// value 1000 = 0x03E8
	callerCode = append(callerCode, 0x61, 0x03, 0xE8)
	callerCode = append(callerCode, 0x73)
	callerCode = append(callerCode, calleeAddr[:]...)
	callerCode = append(callerCode, 0x61, 0xFF, 0xFF, 0xF1, 0x00)

	account := NewInMemoryAccountState()
	account.SetCode(calleeAddr, calleeCode)
	callerAddr := Address{0x11}
	contractAddr := Address{0x42}
	account.SetBalance(contractAddr, big.NewInt(5000))

	stack := NewStack()
	memory := NewMemory()
	gasMeter := NewGasMeter(1_000_000)
	storage := NewInMemoryStorage()
	transient := NewInMemoryTransientStorage()
	accessList := NewSimpleAccessList()
	contract := &SimpleContract{
		AddressVal:   contractAddr,
		CallerVal:    callerAddr,
		CallValueVal: big.NewInt(0),
		CodeVal:      callerCode,
		CodeAddrVal:  contractAddr,
	}
	blockCtx := &SimpleBlockContext{NumberVal: big.NewInt(1), ChainIDVal: big.NewInt(1)}
	txCtx := &SimpleTxContext{OriginVal: Address{0x11}}
	state := NewEVMState(stack, memory, storage, transient, account, gasMeter, blockCtx, txCtx, contract, accessList)
	evm := NewEVM(state, ForkLondon)
	_, err := evm.Run(callerCode)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if account.Balance(calleeAddr).Cmp(big.NewInt(1000)) != 0 {
		t.Fatalf("expected callee balance 1000, got %v", account.Balance(calleeAddr))
	}
	if account.Balance(contractAddr).Cmp(big.NewInt(4000)) != 0 {
		t.Fatalf("expected contract balance 4000, got %v", account.Balance(contractAddr))
	}
}

// TestCallDepthLimit verifies failure at 1024 depth.
func TestCallDepthLimit(t *testing.T) {
	// Recursive self-call: CALL(gas=0xFFFF, self, value=0, in=0,0, out=0,0)
	self := Address{0x42}
	code := []byte{
		0x60, 0x00, // retSize
		0x60, 0x00, // retOffset
		0x60, 0x00, // inSize
		0x60, 0x00, // inOffset
		0x60, 0x00, // value
		0x73,       // PUSH20 self
	}
	code = append(code, self[:]...)
	code = append(code, 0x61, 0xFF, 0xFF, 0xF1, 0x00)

	account := NewInMemoryAccountState()
	account.SetCode(self, code)
	account.SetBalance(Address{0x11}, big.NewInt(1_000_000))

	stack := NewStack()
	memory := NewMemory()
	gasMeter := NewGasMeter(100_000_000)
	storage := NewInMemoryStorage()
	transient := NewInMemoryTransientStorage()
	accessList := NewSimpleAccessList()
	contract := &SimpleContract{
		AddressVal:   self,
		CallerVal:    Address{0x11},
		CallValueVal: big.NewInt(0),
		CodeVal:      code,
		CodeAddrVal:  self,
	}
	blockCtx := &SimpleBlockContext{NumberVal: big.NewInt(1), ChainIDVal: big.NewInt(1)}
	txCtx := &SimpleTxContext{OriginVal: Address{0x11}}
	state := NewEVMState(stack, memory, storage, transient, account, gasMeter, blockCtx, txCtx, contract, accessList)
	evm := NewEVM(state, ForkLondon)
	// Manually set call depth near limit
	state.(*evmState).callDepth = 1023
	res, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != StatusSuccess {
		t.Fatalf("expected success at halt, got %v", res.Status)
	}
	// Stack should have 0 (call failed due to depth)
	if evm.stack.Len() != 1 || evm.stack.Peek().ToBig().Sign() != 0 {
		t.Fatalf("expected 0 on stack for depth failure, got %v", evm.stack.Peek().ToBig())
	}
}

// TestStaticCallWriteProtection verifies SSTORE inside STATICCALL reverts.
func TestStaticCallWriteProtectionInCall(t *testing.T) {
	// Callee: SSTORE(0,1) then RETURN(0,0)
	calleeCode := []byte{0x60, 0x01, 0x60, 0x00, 0x55, 0x60, 0x00, 0x60, 0x00, 0xF3}
	calleeAddr := Address{0xAB}
	// Caller: STATICCALL then STOP
	callerCode := []byte{
		0x60, 0x00, // retSize
		0x60, 0x00, // retOffset
		0x60, 0x00, // inSize
		0x60, 0x00, // inOffset
		0x73,       // PUSH20 callee
	}
	callerCode = append(callerCode, calleeAddr[:]...)
	callerCode = append(callerCode, 0x61, 0xFF, 0xFF, 0xFA, 0x00)

	account := NewInMemoryAccountState()
	account.SetCode(calleeAddr, calleeCode)
	account.SetBalance(Address{0x11}, big.NewInt(1_000_000))

	stack := NewStack()
	memory := NewMemory()
	gasMeter := NewGasMeter(1_000_000)
	storage := NewInMemoryStorage()
	transient := NewInMemoryTransientStorage()
	accessList := NewSimpleAccessList()
	contract := &SimpleContract{
		AddressVal:   Address{0x42},
		CallerVal:    Address{0x11},
		CallValueVal: big.NewInt(0),
		CodeVal:      callerCode,
		CodeAddrVal:  Address{0x42},
	}
	blockCtx := &SimpleBlockContext{NumberVal: big.NewInt(1), ChainIDVal: big.NewInt(1)}
	txCtx := &SimpleTxContext{OriginVal: Address{0x11}}
	state := NewEVMState(stack, memory, storage, transient, account, gasMeter, blockCtx, txCtx, contract, accessList)
	evm := NewEVM(state, ForkLondon)
	_, err := evm.Run(callerCode)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// STATICCALL should fail (push 0) because callee hits write protection
	if evm.stack.Peek().ToBig().Cmp(big.NewInt(0)) != 0 {
		t.Fatalf("expected STATICCALL failure (0), got %v", evm.stack.Peek().ToBig())
	}
}

// TestDelegateCallValue verifies DELEGATECALL inherits parent call value.
func TestDelegateCallValue(t *testing.T) {
	// Callee: CALLVALUE -> RETURN(0,32)
	calleeCode := []byte{0x34, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xF3}
	calleeAddr := Address{0xAB}
	// Caller: DELEGATECALL then STOP
	callerCode := []byte{
		0x60, 0x20, // retSize
		0x60, 0x00, // retOffset
		0x60, 0x00, // inSize
		0x60, 0x00, // inOffset
		0x73,       // PUSH20 callee
	}
	callerCode = append(callerCode, calleeAddr[:]...)
	callerCode = append(callerCode, 0x61, 0xFF, 0xFF, 0xF4, 0x00)

	account := NewInMemoryAccountState()
	account.SetCode(calleeAddr, calleeCode)
	account.SetBalance(Address{0x11}, big.NewInt(1_000_000))

	stack := NewStack()
	memory := NewMemory()
	gasMeter := NewGasMeter(1_000_000)
	storage := NewInMemoryStorage()
	transient := NewInMemoryTransientStorage()
	accessList := NewSimpleAccessList()
	contract := &SimpleContract{
		AddressVal:   Address{0x42},
		CallerVal:    Address{0x11},
		CallValueVal: big.NewInt(1234),
		CodeVal:      callerCode,
		CodeAddrVal:  Address{0x42},
	}
	blockCtx := &SimpleBlockContext{NumberVal: big.NewInt(1), ChainIDVal: big.NewInt(1)}
	txCtx := &SimpleTxContext{OriginVal: Address{0x11}}
	state := NewEVMState(stack, memory, storage, transient, account, gasMeter, blockCtx, txCtx, contract, accessList)
	evm := NewEVM(state, ForkLondon)
	_, err := evm.Run(callerCode)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Return data should contain 1234
	if len(evm.returnData) != 32 {
		t.Fatalf("expected 32 bytes return data, got %d", len(evm.returnData))
	}
	retVal := new(big.Int).SetBytes(evm.returnData)
	if retVal.Cmp(big.NewInt(1234)) != 0 {
		t.Fatalf("expected callvalue 1234 in return data, got %v", retVal)
	}
}

// TestCreateAddress verifies CREATE address from sender nonce.
func TestCreateAddress(t *testing.T) {
	caller := Address{0x11}
	// Init code: RETURN(0,0)
	initCode := []byte{0x60, 0x00, 0x60, 0x00, 0xF3}
	offset := 15
	code := []byte{
		0x60, byte(len(initCode)), // size
		0x60, byte(offset),        // codeOffset
		0x60, 0x00,                // destOffset
		0x39,                      // CODECOPY
		0x60, byte(len(initCode)), // size
		0x60, 0x00,                // offset
		0x60, 0x00,                // value
		0xF0,                      // CREATE
		0x00,                      // STOP
	}
	code = append(code, initCode...)

	account := NewInMemoryAccountState()
	account.SetNonce(caller, 5)
	account.SetBalance(caller, big.NewInt(1_000_000))

	stack := NewStack()
	memory := NewMemory()
	gasMeter := NewGasMeter(1_000_000)
	storage := NewInMemoryStorage()
	transient := NewInMemoryTransientStorage()
	accessList := NewSimpleAccessList()
	contract := &SimpleContract{
		AddressVal:   caller,
		CallerVal:    caller,
		CallValueVal: big.NewInt(0),
		CodeVal:      code,
		CodeAddrVal:  caller,
	}
	blockCtx := &SimpleBlockContext{NumberVal: big.NewInt(1), ChainIDVal: big.NewInt(1)}
	txCtx := &SimpleTxContext{OriginVal: Address{0x11}}
	state := NewEVMState(stack, memory, storage, transient, account, gasMeter, blockCtx, txCtx, contract, accessList)
	evm := NewEVM(state, ForkLondon)
	_, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expectedAddr := createAddress(caller, 5)
	var expectedWord Word
	copy(expectedWord[12:], expectedAddr[:])
	if evm.stack.Peek() != expectedWord {
		t.Fatalf("unexpected CREATE address: got %x, want %x", evm.stack.Peek(), expectedWord)
	}
}

// TestCreate2Address verifies CREATE2 deterministic address.
func TestCreate2Address(t *testing.T) {
	caller := Address{0x11}
	// Init code: RETURN(0,0)
	initCode := []byte{0x60, 0x00, 0x60, 0x00, 0xF3}
	offset := 17
	code := []byte{
		0x60, byte(len(initCode)), // size
		0x60, byte(offset),        // codeOffset
		0x60, 0x00,                // destOffset
		0x39,                      // CODECOPY
		0x60, 0x42,                // salt
		0x60, byte(len(initCode)), // size
		0x60, 0x00,                // offset
		0x60, 0x00,                // value
		0xF5,                      // CREATE2
		0x00,                      // STOP
	}
	code = append(code, initCode...)

	account := NewInMemoryAccountState()
	account.SetBalance(caller, big.NewInt(1_000_000))

	stack := NewStack()
	memory := NewMemory()
	gasMeter := NewGasMeter(1_000_000)
	storage := NewInMemoryStorage()
	transient := NewInMemoryTransientStorage()
	accessList := NewSimpleAccessList()
	contract := &SimpleContract{
		AddressVal:   caller,
		CallerVal:    caller,
		CallValueVal: big.NewInt(0),
		CodeVal:      code,
		CodeAddrVal:  caller,
	}
	blockCtx := &SimpleBlockContext{NumberVal: big.NewInt(1), ChainIDVal: big.NewInt(1)}
	txCtx := &SimpleTxContext{OriginVal: Address{0x11}}
	state := NewEVMState(stack, memory, storage, transient, account, gasMeter, blockCtx, txCtx, contract, accessList)
	evm := NewEVM(state, ForkLondon)
	_, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expectedAddr := create2Address(caller, Hash{31: 0x42}, initCode)
	var expectedWord Word
	copy(expectedWord[12:], expectedAddr[:])
	if evm.stack.Peek() != expectedWord {
		t.Fatalf("unexpected CREATE2 address: got %x, want %x", evm.stack.Peek(), expectedWord)
	}
}

// TestCreateInvalidPrefix rejects code starting with 0xEF (London+).
func TestCreateInvalidPrefix(t *testing.T) {
	caller := Address{0x11}
	// Init code returns 0xEF00 (1 byte)
	initCode := []byte{0x60, 0xEF, 0x60, 0x00, 0x53, 0x60, 0x01, 0x60, 0x00, 0xF3}
	offset := 15
	code := []byte{
		0x60, byte(len(initCode)),
		0x60, byte(offset),
		0x60, 0x00,
		0x39,
		0x60, byte(len(initCode)),
		0x60, 0x00,
		0x60, 0x00,
		0xF0,
		0x00,
	}
	code = append(code, initCode...)

	account := NewInMemoryAccountState()
	account.SetBalance(caller, big.NewInt(1_000_000))

	stack := NewStack()
	memory := NewMemory()
	gasMeter := NewGasMeter(1_000_000)
	storage := NewInMemoryStorage()
	transient := NewInMemoryTransientStorage()
	accessList := NewSimpleAccessList()
	contract := &SimpleContract{
		AddressVal:   caller,
		CallerVal:    caller,
		CallValueVal: big.NewInt(0),
		CodeVal:      code,
		CodeAddrVal:  caller,
	}
	blockCtx := &SimpleBlockContext{NumberVal: big.NewInt(1), ChainIDVal: big.NewInt(1)}
	txCtx := &SimpleTxContext{OriginVal: Address{0x11}}
	state := NewEVMState(stack, memory, storage, transient, account, gasMeter, blockCtx, txCtx, contract, accessList)
	evm := NewEVM(state, ForkLondon)
	_, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should push 0 because of invalid prefix
	if evm.stack.Peek().ToBig().Sign() != 0 {
		t.Fatalf("expected 0 for invalid prefix, got %v", evm.stack.Peek().ToBig())
	}
}

// TestCreateCollision fails if address already exists.
func TestCreateCollision(t *testing.T) {
	caller := Address{0x11}
	initCode := []byte{0x60, 0x00, 0x60, 0x00, 0xF3}
	offset := 15
	code := []byte{
		0x60, byte(len(initCode)),
		0x60, byte(offset),
		0x60, 0x00,
		0x39,
		0x60, 0x00,
		0x60, 0x00,
		0x60, byte(len(initCode)),
		0xF0,
		0x00,
	}
	code = append(code, initCode...)

	account := NewInMemoryAccountState()
	account.SetBalance(caller, big.NewInt(1_000_000))
	account.SetNonce(caller, 0)

	expectedAddr := createAddress(caller, 0)
	account.SetNonce(expectedAddr, 1) // make it exist

	stack := NewStack()
	memory := NewMemory()
	gasMeter := NewGasMeter(1_000_000)
	storage := NewInMemoryStorage()
	transient := NewInMemoryTransientStorage()
	accessList := NewSimpleAccessList()
	contract := &SimpleContract{
		AddressVal:   caller,
		CallerVal:    caller,
		CallValueVal: big.NewInt(0),
		CodeVal:      code,
		CodeAddrVal:  caller,
	}
	blockCtx := &SimpleBlockContext{NumberVal: big.NewInt(1), ChainIDVal: big.NewInt(1)}
	txCtx := &SimpleTxContext{OriginVal: Address{0x11}}
	state := NewEVMState(stack, memory, storage, transient, account, gasMeter, blockCtx, txCtx, contract, accessList)
	evm := NewEVM(state, ForkLondon)
	_, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evm.stack.Peek().ToBig().Sign() != 0 {
		t.Fatalf("expected 0 for collision, got %v", evm.stack.Peek().ToBig())
	}
}

// TestOpcodeHookReplaceResult verifies a hook can replace opcode result.
func TestOpcodeHookReplaceResult(t *testing.T) {
	code := []byte{0x60, 0x01, 0x60, 0x02, 0x01, 0x00} // PUSH1 1 PUSH1 2 ADD STOP
	evm, _ := makeTestEVM(code)
	reg := NewSimpleHookRegistry()
	hook := &testHook{
		hookType: HookTypeOpcode,
		id:       "replace-add",
		fireFn: func(ctx *HookContext) (*HookResult, error) {
			if ctx.Opcode != nil && ctx.Opcode.Op == 0x01 { // ADD
				return &HookResult{
					Action:     ActionReplaceResult,
					ResultData: []Word{BigToWord(big.NewInt(99))},
				}, nil
			}
			return &HookResult{Action: ActionContinue}, nil
		},
	}
	reg.Register(hook)
	evm.SetHooks(reg)

	_, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evm.stack.Peek().ToBig().Cmp(big.NewInt(99)) != 0 {
		t.Fatalf("expected hooked result 99, got %v", evm.stack.Peek().ToBig())
	}
}

// TestStepHook verifies step hook fires on every instruction.
func TestStepHook(t *testing.T) {
	code := []byte{0x60, 0x01, 0x00} // PUSH1 1 STOP
	evm, _ := makeTestEVM(code)
	reg := NewSimpleHookRegistry()
	count := 0
	hook := &testHook{
		hookType: HookTypeStep,
		id:       "step-counter",
		fireFn: func(ctx *HookContext) (*HookResult, error) {
			count++
			return &HookResult{Action: ActionContinue}, nil
		},
	}
	reg.Register(hook)
	evm.SetHooks(reg)

	_, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected step hook to fire 2 times, got %d", count)
	}
}

// TestExtcodehashEmpty returns 0 for non-existent account.
func TestExtcodehashEmpty(t *testing.T) {
	evm, _ := makeTestEVM([]byte{})
	addr := Address{0xAB}
	var addrWord Word
	copy(addrWord[12:], addr[:])
	evm.stack.Push(addrWord)
	err := opExtcodehash(evm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evm.stack.Peek().ToBig().Sign() != 0 {
		t.Fatalf("expected 0 for non-existent account, got %v", evm.stack.Peek().ToBig())
	}
}

// TestExtcodehashExisting returns keccak256 of code.
func TestExtcodehashExisting(t *testing.T) {
	addr := Address{0xAB}
	code := []byte{0x60, 0x01, 0x00}
	account := NewInMemoryAccountState()
	account.SetCode(addr, code)
	account.CreateAccount(addr)

	stack := NewStack()
	memory := NewMemory()
	gasMeter := NewGasMeter(1_000_000)
	storage := NewInMemoryStorage()
	transient := NewInMemoryTransientStorage()
	accessList := NewSimpleAccessList()
	contract := &SimpleContract{AddressVal: Address{0x42}, CodeAddrVal: Address{0x42}}
	blockCtx := &SimpleBlockContext{NumberVal: big.NewInt(1), ChainIDVal: big.NewInt(1)}
	txCtx := &SimpleTxContext{OriginVal: Address{0x11}}
	state := NewEVMState(stack, memory, storage, transient, account, gasMeter, blockCtx, txCtx, contract, accessList)
	evm := NewEVM(state, ForkLondon)

	var addrWord Word
	copy(addrWord[12:], addr[:])
	evm.stack.Push(addrWord)
	err := opExtcodehash(evm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := hashCode(code)
	actual := WordToHash(evm.stack.Peek())
	if actual != expected {
		t.Fatalf("expected hash %x, got %x", expected, actual)
	}
}

// TestExtcodesizeNonExistent returns 0.
func TestExtcodesizeNonExistent(t *testing.T) {
	evm, _ := makeTestEVM([]byte{})
	addr := Address{0xAB}
	var addrWord Word
	copy(addrWord[12:], addr[:])
	evm.stack.Push(addrWord)
	err := opExtcodesize(evm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evm.stack.Peek().ToBig().Sign() != 0 {
		t.Fatalf("expected 0, got %v", evm.stack.Peek().ToBig())
	}
}

// testHook is a test helper implementing Hook.
type testHook struct {
	hookType HookType
	id       string
	once     bool
	fireFn   func(ctx *HookContext) (*HookResult, error)
}

func (h *testHook) Type() HookType   { return h.hookType }
func (h *testHook) OneTime() bool    { return h.once }
func (h *testHook) ID() string       { return h.id }
func (h *testHook) Fire(ctx *HookContext) (*HookResult, error) {
	return h.fireFn(ctx)
}
