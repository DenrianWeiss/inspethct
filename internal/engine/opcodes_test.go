package engine

import (
	"bytes"
	"math/big"
	"testing"
)

func makeTestEVM(code []byte) (*EVM, *EVMState) {
	stack := NewStack()
	memory := NewMemory()
	gasMeter := NewGasMeter(1_000_000)
	storage := NewInMemoryStorage()
	transient := NewInMemoryTransientStorage()
	account := NewInMemoryAccountState()
	accessList := NewSimpleAccessList()
	contract := &SimpleContract{
		AddressVal:   Address{0x42},
		CallerVal:    Address{0x11},
		CallValueVal: big.NewInt(0),
		CallInputVal: nil,
		CodeVal:      code,
		CodeAddrVal:  Address{0x42},
	}
	blockCtx := &SimpleBlockContext{
		NumberVal:  big.NewInt(1),
		ChainIDVal: big.NewInt(1),
	}
	txCtx := &SimpleTxContext{OriginVal: Address{0x11}}
	state := NewEVMState(stack, memory, storage, transient, account, gasMeter, blockCtx, txCtx, contract, accessList)
	evm := NewEVM(state, ForkLondon)
	return evm, &state
}

func TestOpcodeStop(t *testing.T) {
	code := []byte{0x00}
	evm, _ := makeTestEVM(code)
	res, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != StatusSuccess {
		t.Fatalf("expected success, got %v", res.Status)
	}
}

func TestOpcodeAdd(t *testing.T) {
	code := []byte{0x60, 0x01, 0x60, 0x02, 0x01, 0x00}
	evm, _ := makeTestEVM(code)
	_, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evm.stack.Len() != 1 {
		t.Fatalf("expected 1 stack item, got %d", evm.stack.Len())
	}
	val := evm.stack.Peek().ToBig()
	if val.Cmp(big.NewInt(3)) != 0 {
		t.Fatalf("expected 3, got %v", val)
	}
}

func TestOpcodeMul(t *testing.T) {
	code := []byte{0x60, 0x03, 0x60, 0x04, 0x02, 0x00}
	evm, _ := makeTestEVM(code)
	_, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	val := evm.stack.Peek().ToBig()
	if val.Cmp(big.NewInt(12)) != 0 {
		t.Fatalf("expected 12, got %v", val)
	}
}

func TestOpcodeSub(t *testing.T) {
	code := []byte{0x60, 0x03, 0x60, 0x05, 0x03, 0x00}
	evm, _ := makeTestEVM(code)
	_, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	val := evm.stack.Peek().ToBig()
	if val.Cmp(big.NewInt(2)) != 0 {
		t.Fatalf("expected 2, got %v", val)
	}
}

func TestOpcodeDiv(t *testing.T) {
	code := []byte{0x60, 0x03, 0x60, 0x07, 0x04, 0x00}
	evm, _ := makeTestEVM(code)
	_, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	val := evm.stack.Peek().ToBig()
	if val.Cmp(big.NewInt(2)) != 0 {
		t.Fatalf("expected 2, got %v", val)
	}
}

func TestOpcodeDivByZero(t *testing.T) {
	code := []byte{0x60, 0x07, 0x60, 0x00, 0x04, 0x00}
	evm, _ := makeTestEVM(code)
	_, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	val := evm.stack.Peek().ToBig()
	if val.Sign() != 0 {
		t.Fatalf("expected 0, got %v", val)
	}
}

func TestOpcodeMod(t *testing.T) {
	code := []byte{0x60, 0x06, 0x60, 0x07, 0x06, 0x00}
	evm, _ := makeTestEVM(code)
	_, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	val := evm.stack.Peek().ToBig()
	if val.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("expected 1, got %v", val)
	}
}

func TestOpcodeExp(t *testing.T) {
	code := []byte{0x60, 0x03, 0x60, 0x02, 0x0A, 0x00}
	evm, _ := makeTestEVM(code)
	_, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	val := evm.stack.Peek().ToBig()
	if val.Cmp(big.NewInt(8)) != 0 {
		t.Fatalf("expected 8, got %v", val)
	}
}

func TestOpcodeLtGt(t *testing.T) {
	code := []byte{0x60, 0x05, 0x60, 0x03, 0x10, 0x00}
	evm, _ := makeTestEVM(code)
	_, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	val := evm.stack.Peek().ToBig()
	if val.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("expected 1 (3 < 5), got %v", val)
	}
}

func TestOpcodeEq(t *testing.T) {
	code := []byte{0x60, 0x05, 0x60, 0x05, 0x14, 0x00}
	evm, _ := makeTestEVM(code)
	_, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	val := evm.stack.Peek().ToBig()
	if val.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("expected 1, got %v", val)
	}
}

func TestOpcodeAnd(t *testing.T) {
	code := []byte{0x60, 0x0F, 0x60, 0x0A, 0x16, 0x00}
	evm, _ := makeTestEVM(code)
	_, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	val := evm.stack.Peek().ToBig()
	if val.Cmp(big.NewInt(0x0A)) != 0 {
		t.Fatalf("expected 10, got %v", val)
	}
}

func TestOpcodeNot(t *testing.T) {
	code := []byte{0x60, 0x00, 0x19, 0x00}
	evm, _ := makeTestEVM(code)
	_, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	val := evm.stack.Peek().ToBig()
	max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	if val.Cmp(max) != 0 {
		t.Fatalf("expected max uint256, got %v", val)
	}
}

func TestOpcodeShl(t *testing.T) {
	code := []byte{0x60, 0x01, 0x60, 0x01, 0x1B, 0x00}
	evm, _ := makeTestEVM(code)
	_, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	val := evm.stack.Peek().ToBig()
	if val.Cmp(big.NewInt(2)) != 0 {
		t.Fatalf("expected 2, got %v", val)
	}
}

func TestOpcodeShr(t *testing.T) {
	code := []byte{0x60, 0x04, 0x60, 0x01, 0x1C, 0x00}
	evm, _ := makeTestEVM(code)
	_, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	val := evm.stack.Peek().ToBig()
	if val.Cmp(big.NewInt(2)) != 0 {
		t.Fatalf("expected 2, got %v", val)
	}
}

func TestOpcodePop(t *testing.T) {
	code := []byte{0x60, 0x01, 0x50, 0x00}
	evm, _ := makeTestEVM(code)
	_, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evm.stack.Len() != 0 {
		t.Fatalf("expected empty stack, got %d items", evm.stack.Len())
	}
}

func TestOpcodeDup(t *testing.T) {
	code := []byte{0x60, 0x01, 0x80, 0x00}
	evm, _ := makeTestEVM(code)
	_, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evm.stack.Len() != 2 {
		t.Fatalf("expected 2 stack items, got %d", evm.stack.Len())
	}
	if evm.stack.PeekN(0).ToBig().Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("expected top=1, got %v", evm.stack.PeekN(0).ToBig())
	}
	if evm.stack.PeekN(1).ToBig().Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("expected next=1, got %v", evm.stack.PeekN(1).ToBig())
	}
}

func TestOpcodeSwap(t *testing.T) {
	code := []byte{0x60, 0x01, 0x60, 0x02, 0x90, 0x00}
	evm, _ := makeTestEVM(code)
	_, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evm.stack.PeekN(0).ToBig().Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("expected top=1, got %v", evm.stack.PeekN(0).ToBig())
	}
	if evm.stack.PeekN(1).ToBig().Cmp(big.NewInt(2)) != 0 {
		t.Fatalf("expected next=2, got %v", evm.stack.PeekN(1).ToBig())
	}
}

func TestOpcodeMloadMstore(t *testing.T) {
	code := []byte{0x60, 0x01, 0x60, 0x20, 0x52, 0x60, 0x20, 0x51, 0x00}
	evm, _ := makeTestEVM(code)
	_, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	val := evm.stack.Peek().ToBig()
	if val.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("expected 1, got %v", val)
	}
}

func TestOpcodeMsize(t *testing.T) {
	code := []byte{0x60, 0x01, 0x60, 0x20, 0x52, 0x59, 0x00}
	evm, _ := makeTestEVM(code)
	_, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	val := evm.stack.Peek().ToBig()
	if val.Cmp(big.NewInt(64)) != 0 {
		t.Fatalf("expected 64, got %v", val)
	}
}

func TestOpcodeSloadSstore(t *testing.T) {
	code := []byte{0x60, 0x01, 0x60, 0x00, 0x55, 0x60, 0x00, 0x54, 0x00}
	evm, _ := makeTestEVM(code)
	_, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	val := evm.stack.Peek().ToBig()
	if val.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("expected 1, got %v", val)
	}
}

func TestOpcodeJump(t *testing.T) {
	code := []byte{0x60, 0x03, 0x56, 0x5B, 0x00}
	evm, _ := makeTestEVM(code)
	res, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != StatusSuccess {
		t.Fatalf("expected success, got %v", res.Status)
	}
}

func TestOpcodeJumpi(t *testing.T) {
	code := []byte{0x60, 0x01, 0x60, 0x08, 0x60, 0x08, 0x57, 0x00, 0x5B, 0x00}
	evm, _ := makeTestEVM(code)
	res, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != StatusSuccess {
		t.Fatalf("expected success, got %v", res.Status)
	}
}

func TestOpcodeJumpInvalid(t *testing.T) {
	code := []byte{0x60, 0x03, 0x56, 0x00}
	evm, _ := makeTestEVM(code)
	res, err := evm.Run(code)
	if err == nil {
		t.Fatalf("expected error for invalid jump")
	}
	if res.Status != StatusInvalidJump {
		t.Fatalf("expected invalid jump, got %v", res.Status)
	}
}

func TestOpcodeInvalidOpcode(t *testing.T) {
	code := []byte{0x0C}
	evm, _ := makeTestEVM(code)
	res, err := evm.Run(code)
	if err == nil {
		t.Fatalf("expected error for invalid opcode")
	}
	if res.Status != StatusInvalidOpcode {
		t.Fatalf("expected invalid opcode, got %v", res.Status)
	}
}

func TestOpcodeKeccak256(t *testing.T) {
	code := []byte{0x60, 0x01, 0x60, 0x00, 0x53, 0x60, 0x01, 0x60, 0x00, 0x20, 0x00}
	evm, _ := makeTestEVM(code)
	_, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evm.stack.Len() != 1 {
		t.Fatalf("expected 1 stack item, got %d", evm.stack.Len())
	}
	expected := keccak256([]byte{0x01})
	actual := evm.stack.Peek()
	expectedWord := BytesToWord(expected)
	if !bytes.Equal(actual[:], expectedWord[:]) {
		t.Fatalf("unexpected keccak256 result: got %x, want %x", actual, expectedWord)
	}
}

func TestOpcodeAddress(t *testing.T) {
	code := []byte{0x30, 0x00}
	evm, _ := makeTestEVM(code)
	_, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	val := evm.stack.Peek()
	var expected Word
	addr := Address{0x42}
	copy(expected[12:], addr[:])
	if val != expected {
		t.Fatalf("expected address, got %x", val)
	}
}

func TestOpcodeCaller(t *testing.T) {
	code := []byte{0x33, 0x00}
	evm, _ := makeTestEVM(code)
	_, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	val := evm.stack.Peek()
	var expected Word
	addr := Address{0x11}
	copy(expected[12:], addr[:])
	if val != expected {
		t.Fatalf("expected caller, got %x", val)
	}
}

func TestOpcodePush0(t *testing.T) {
	code := []byte{0x5F, 0x00}
	stack := NewStack()
	memory := NewMemory()
	gasMeter := NewGasMeter(1_000_000)
	storage := NewInMemoryStorage()
	transient := NewInMemoryTransientStorage()
	account := NewInMemoryAccountState()
	accessList := NewSimpleAccessList()
	contract := &SimpleContract{
		AddressVal:   Address{0x42},
		CallerVal:    Address{0x11},
		CallValueVal: big.NewInt(0),
		CallInputVal: nil,
		CodeVal:      code,
		CodeAddrVal:  Address{0x42},
	}
	blockCtx := &SimpleBlockContext{NumberVal: big.NewInt(1), ChainIDVal: big.NewInt(1)}
	txCtx := &SimpleTxContext{OriginVal: Address{0x11}}
	state := NewEVMState(stack, memory, storage, transient, account, gasMeter, blockCtx, txCtx, contract, accessList)
	evm := NewEVM(state, ForkShanghai)
	_, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	val := evm.stack.Peek()
	if val != (Word{}) {
		t.Fatalf("expected 0, got %x", val)
	}
}

func TestOpcodeRevert(t *testing.T) {
	code := []byte{0x60, 0x01, 0x60, 0x00, 0x53, 0x60, 0x01, 0x60, 0x00, 0xFD}
	evm, _ := makeTestEVM(code)
	res, err := evm.Run(code)
	if err == nil {
		t.Fatalf("expected revert error")
	}
	if res.Status != StatusRevert {
		t.Fatalf("expected revert status, got %v", res.Status)
	}
	if len(res.ReturnData) != 1 || res.ReturnData[0] != 0x01 {
		t.Fatalf("expected return data [0x01], got %x (len=%d)", res.ReturnData, len(res.ReturnData))
	}
}

func TestOpcodeGas(t *testing.T) {
	code := []byte{0x5A, 0x00}
	evm, _ := makeTestEVM(code)
	_, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	val := evm.stack.Peek().ToBig()
	if val.Sign() <= 0 {
		t.Fatalf("expected positive gas remaining, got %v", val)
	}
}

func TestOpcodePc(t *testing.T) {
	code := []byte{0x58, 0x00}
	evm, _ := makeTestEVM(code)
	_, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	val := evm.stack.Peek().ToBig()
	if val.Cmp(big.NewInt(0)) != 0 {
		t.Fatalf("expected PC=0, got %v", val)
	}
}

func TestOpcodeByte(t *testing.T) {
	code := append([]byte{0x7F}, bytes.Repeat([]byte{0xFF}, 32)...)
	code = append(code, 0x60, 0x1F, 0x1A, 0x00)
	evm, _ := makeTestEVM(code)
	_, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	val := evm.stack.Peek().ToBig()
	if val.Cmp(big.NewInt(0xFF)) != 0 {
		t.Fatalf("expected 0xFF, got %v", val)
	}
}

func TestOpcodeIszero(t *testing.T) {
	code := []byte{0x60, 0x00, 0x15, 0x00}
	evm, _ := makeTestEVM(code)
	_, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	val := evm.stack.Peek().ToBig()
	if val.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("expected 1, got %v", val)
	}
}

func TestOpcodeLog(t *testing.T) {
	code := []byte{
		0x60, 0x01, 0x60, 0x00, 0x52,
		0x60, 0x01, 0x60, 0x00, 0x60, 0x00, 0xA1,
		0x00,
	}
	evm, state := makeTestEVM(code)
	res, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != StatusSuccess {
		t.Fatalf("expected success, got %v", res.Status)
	}
	logs := (*state).Logs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}
	if len(logs[0].Topics) != 1 {
		t.Fatalf("expected 1 topic, got %d", len(logs[0].Topics))
	}
}

func TestOpcodeStaticCallWriteProtection(t *testing.T) {
	code := []byte{0x60, 0x01, 0x60, 0x00, 0x55, 0x00}
	evm, _ := makeTestEVM(code)
	contract := evm.state.Contract().(*SimpleContract)
	contract.IsStaticVal = true
	res, err := evm.Run(code)
	if err == nil {
		t.Fatalf("expected write protection error")
	}
	if res.Status != StatusHalt {
		t.Fatalf("expected halt, got %v", res.Status)
	}
}

func TestOpcodeMcopy(t *testing.T) {
	code := []byte{0x60, 0x01, 0x60, 0x00, 0x53, 0x60, 0x01, 0x60, 0x00, 0x60, 0x20, 0x5E, 0x00}
	stack := NewStack()
	memory := NewMemory()
	gasMeter := NewGasMeter(1_000_000)
	storage := NewInMemoryStorage()
	transient := NewInMemoryTransientStorage()
	account := NewInMemoryAccountState()
	accessList := NewSimpleAccessList()
	contract := &SimpleContract{
		AddressVal: Address{0x42}, CallerVal: Address{0x11},
		CallValueVal: big.NewInt(0), CallInputVal: nil,
		CodeVal: code, CodeAddrVal: Address{0x42},
	}
	blockCtx := &SimpleBlockContext{NumberVal: big.NewInt(1), ChainIDVal: big.NewInt(1)}
	txCtx := &SimpleTxContext{OriginVal: Address{0x11}}
	state := NewEVMState(stack, memory, storage, transient, account, gasMeter, blockCtx, txCtx, contract, accessList)
	evm := NewEVM(state, ForkCancun)
	_, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	val := evm.memory.Word(0x20)
	if val != (Word{0: 0x01}) {
		t.Fatalf("expected copied byte at 0x20, got %x", val)
	}
}

func TestOpcodeCalldataload(t *testing.T) {
	code := []byte{0x60, 0x00, 0x35, 0x00}
	evm, _ := makeTestEVM(code)
	contract := evm.state.Contract().(*SimpleContract)
	contract.CallInputVal = []byte{0x01, 0x02, 0x03, 0x04}
	_, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	val := evm.stack.Peek()
	var expected Word
	expected[0] = 0x01
	expected[1] = 0x02
	expected[2] = 0x03
	expected[3] = 0x04
	if val != expected {
		t.Fatalf("expected calldata word, got %x", val)
	}
}

func TestOpcodeBlockNumber(t *testing.T) {
	code := []byte{0x43, 0x00}
	evm, _ := makeTestEVM(code)
	_, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	val := evm.stack.Peek().ToBig()
	if val.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("expected block number 1, got %v", val)
	}
}

func TestOpcodeChainId(t *testing.T) {
	code := []byte{0x46, 0x00}
	evm, _ := makeTestEVM(code)
	_, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	val := evm.stack.Peek().ToBig()
	if val.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("expected chain id 1, got %v", val)
	}
}

func TestOpcodeTloadTstore(t *testing.T) {
	code := []byte{0x60, 0x01, 0x60, 0x00, 0x5D, 0x60, 0x00, 0x5C, 0x00}
	stack := NewStack()
	memory := NewMemory()
	gasMeter := NewGasMeter(1_000_000)
	storage := NewInMemoryStorage()
	transient := NewInMemoryTransientStorage()
	account := NewInMemoryAccountState()
	accessList := NewSimpleAccessList()
	contract := &SimpleContract{
		AddressVal: Address{0x42}, CallerVal: Address{0x11},
		CallValueVal: big.NewInt(0), CallInputVal: nil,
		CodeVal: code, CodeAddrVal: Address{0x42},
	}
	blockCtx := &SimpleBlockContext{NumberVal: big.NewInt(1), ChainIDVal: big.NewInt(1)}
	txCtx := &SimpleTxContext{OriginVal: Address{0x11}}
	state := NewEVMState(stack, memory, storage, transient, account, gasMeter, blockCtx, txCtx, contract, accessList)
	evm := NewEVM(state, ForkCancun)
	_, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	val := evm.stack.Peek().ToBig()
	if val.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("expected 1 from tload, got %v", val)
	}
}

func TestOpcodeCalldatasize(t *testing.T) {
	code := []byte{0x36, 0x00}
	evm, _ := makeTestEVM(code)
	contract := evm.state.Contract().(*SimpleContract)
	contract.CallInputVal = make([]byte, 100)
	_, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	val := evm.stack.Peek().ToBig()
	if val.Cmp(big.NewInt(100)) != 0 {
		t.Fatalf("expected calldatasize 100, got %v", val)
	}
}

func TestOpcodePush32(t *testing.T) {
	code := append([]byte{0x7F}, bytes.Repeat([]byte{0xFF}, 32)...)
	code = append(code, 0x00)
	evm, _ := makeTestEVM(code)
	_, err := evm.Run(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	val := evm.stack.Peek()
	expected := bytes.Repeat([]byte{0xFF}, 32)
	if !bytes.Equal(val[:], expected) {
		t.Fatalf("expected all 0xFF")
	}
}
