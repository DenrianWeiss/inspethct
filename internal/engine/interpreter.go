package engine

import (
	"fmt"
	"math/big"
)

// EVM is the core execution engine.
type EVM struct {
	state       EVMState
	fork        Fork
	precompiles *PrecompileRegistry
	gasMeter    GasMeter
	stack       Stack
	memory      Memory
	pc          uint64
	returnData  []byte
	outputData  []byte
	jumpdests   []bool // bitmap indexed by code offset
	hooks       HookRegistry
}

func safeAddUint64(values ...uint64) (uint64, bool) {
	var total uint64
	for _, value := range values {
		if value > ^uint64(0)-total {
			return 0, false
		}
		total += value
	}
	return total, true
}

func safeMulUint64(a, b uint64) (uint64, bool) {
	if a == 0 || b == 0 {
		return 0, true
	}
	if a > ^uint64(0)/b {
		return 0, false
	}
	return a * b, true
}

func wordCount(size uint64) uint64 {
	if size == 0 {
		return 0
	}
	if size > ^uint64(0)-31 {
		return ^uint64(0)
	}
	return (size + 31) / 32
}

// NewEVM creates a new EVM instance.
func NewEVM(state EVMState, fork Fork, registries ...*PrecompileRegistry) *EVM {
	var registry *PrecompileRegistry
	if len(registries) > 0 {
		registry = registries[0]
	}
	if registry == nil {
		registry = MainnetPrecompilesForFork(fork)
	}
	return &EVM{
		state:       state,
		fork:        fork,
		precompiles: registry,
		gasMeter:    state.GasMeter(),
		stack:       state.Stack(),
		memory:      state.Memory(),
	}
}

func NewMainnetEVM(state EVMState, fork Fork) *EVM {
	return NewEVM(state, fork, MainnetPrecompilesForFork(fork))
}

// SetHooks attaches a hook registry to the EVM.
func (evm *EVM) SetHooks(hooks HookRegistry) {
	evm.hooks = hooks
}

// Run executes code starting from PC 0 until halt or error.
func (evm *EVM) Run(code []byte) (res *ExecutionResult, err error) {
	// Catch panics from stack underflow/overflow and convert to errors.
	defer func() {
		if r := recover(); r != nil {
			var status ExecutionStatus
			switch r {
			case ErrStackUnderflow:
				status = StatusStackUnderflow
			case ErrStackOverflow:
				status = StatusStackOverflow
			default:
				status = StatusHalt
			}
			builder := NewExecutionResultBuilder()
			builder.SetStatus(status)
			builder.SetError(fmt.Errorf("%v", r))
			builder.SetReturnData(evm.outputData)
			res = builder.Build()
			err = fmt.Errorf("%v", r)
		}
	}()

	// Pre-compute JUMPDEST locations
	evm.analyzeJumpdests(code)

	builder := NewExecutionResultBuilder()

	for {
		if evm.pc >= uint64(len(code)) {
			builder.SetStatus(StatusSuccess)
			builder.SetGasRemaining(evm.gasMeter.Gas())
			builder.SetGasRefund(evm.gasMeter.Refund())
			builder.SetReturnData(evm.outputData)
			builder.SetLogs(evm.state.Logs())
			return builder.Build(), nil
		}

		op := code[evm.pc]

		// Check fork availability
		minFork := MinForkForOpcode(op)
		if !forkGTE(evm.fork, minFork) {
			builder.SetStatus(StatusInvalidOpcode)
			builder.SetError(ErrInvalidOpcode)
			return builder.Build(), ErrInvalidOpcode
		}

		gasBefore := evm.gasMeter.Gas()

		// Get base gas cost
		gasCost := GasCosts[op]

		// Dynamic gas calculation
		dynamicGas, err := evm.calcDynamicGas(op, code)
		if err != nil {
			builder.SetStatus(StatusOutOfGas)
			builder.SetError(err)
			return builder.Build(), err
		}

		if dynamicGas > ^uint64(0)-gasCost {
			builder.SetStatus(StatusOutOfGas)
			builder.SetError(ErrOutOfGas)
			return builder.Build(), ErrOutOfGas
		}

		totalGas := gasCost + dynamicGas
		if err := evm.gasMeter.ConsumeGas(totalGas); err != nil {
			builder.SetStatus(StatusOutOfGas)
			builder.SetError(err)
			return builder.Build(), err
		}

		// Hook dispatch
		if skip, err := evm.dispatchHooks(op, gasBefore, totalGas); err != nil {
			status := classifyError(err)
			builder.SetStatus(status)
			builder.SetError(err)
			builder.SetReturnData(evm.outputData)
			if status == StatusRevert {
				builder.SetGasRemaining(evm.gasMeter.Gas())
			}
			return builder.Build(), err
		} else if skip {
			continue
		}

		// Execute opcode
		if err := evm.execute(op, code); err != nil {
			status := classifyError(err)
			builder.SetStatus(status)
			builder.SetError(err)
			builder.SetReturnData(evm.outputData)
			if status == StatusRevert {
				builder.SetGasRemaining(evm.gasMeter.Gas())
				builder.SetGasRefund(evm.gasMeter.Refund())
				builder.SetLogs(evm.state.Logs())
			}
			if status == StatusSuccess {
				builder.SetGasRemaining(evm.gasMeter.Gas())
				builder.SetGasRefund(evm.gasMeter.Refund())
				builder.SetLogs(evm.state.Logs())
				return builder.Build(), nil
			}
			return builder.Build(), err
		}

	}
}

// execute dispatches to the appropriate opcode handler.
func (evm *EVM) execute(op byte, code []byte) error {
	switch op {
	// Arithmetic
	case 0x00:
		return opStop(evm)
	case 0x01:
		return opAdd(evm)
	case 0x02:
		return opMul(evm)
	case 0x03:
		return opSub(evm)
	case 0x04:
		return opDiv(evm)
	case 0x05:
		return opSdiv(evm)
	case 0x06:
		return opMod(evm)
	case 0x07:
		return opSmod(evm)
	case 0x08:
		return opAddmod(evm)
	case 0x09:
		return opMulmod(evm)
	case 0x0A:
		return opExp(evm)
	case 0x0B:
		return opSignextend(evm)

	// Comparison & Bitwise
	case 0x10:
		return opLt(evm)
	case 0x11:
		return opGt(evm)
	case 0x12:
		return opSlt(evm)
	case 0x13:
		return opSgt(evm)
	case 0x14:
		return opEq(evm)
	case 0x15:
		return opIszero(evm)
	case 0x16:
		return opAnd(evm)
	case 0x17:
		return opOr(evm)
	case 0x18:
		return opXor(evm)
	case 0x19:
		return opNot(evm)
	case 0x1A:
		return opByte(evm)
	case 0x1B:
		return opShl(evm)
	case 0x1C:
		return opShr(evm)
	case 0x1D:
		return opSar(evm)
	case 0x1E:
		return opClz(evm)

	// Cryptographic
	case 0x20:
		return opKeccak256(evm)

	// Environmental
	case 0x30:
		return opAddress(evm)
	case 0x31:
		return opBalance(evm)
	case 0x32:
		return opOrigin(evm)
	case 0x33:
		return opCaller(evm)
	case 0x34:
		return opCallvalue(evm)
	case 0x35:
		return opCalldataload(evm)
	case 0x36:
		return opCalldatasize(evm)
	case 0x37:
		return opCalldatacopy(evm)
	case 0x38:
		return opCodesize(evm)
	case 0x39:
		return opCodecopy(evm)
	case 0x3A:
		return opGasprice(evm)
	case 0x3B:
		return opExtcodesize(evm)
	case 0x3C:
		return opExtcodecopy(evm)
	case 0x3D:
		return opReturndatasize(evm)
	case 0x3E:
		return opReturndatacopy(evm)
	case 0x3F:
		return opExtcodehash(evm)

	// Block info
	case 0x40:
		return opBlockhash(evm)
	case 0x41:
		return opCoinbase(evm)
	case 0x42:
		return opTimestamp(evm)
	case 0x43:
		return opNumber(evm)
	case 0x44:
		return opPrevrandao(evm)
	case 0x45:
		return opGaslimit(evm)
	case 0x46:
		return opChainid(evm)
	case 0x47:
		return opSelfbalance(evm)
	case 0x48:
		return opBasefee(evm)
	case 0x49:
		return opBlobhash(evm)
	case 0x4A:
		return opBlobbasefee(evm)

	// Stack
	case 0x50:
		return opPop(evm)
	case 0x5F:
		return opPush0(evm)

	// Memory
	case 0x51:
		return opMload(evm)
	case 0x52:
		return opMstore(evm)
	case 0x53:
		return opMstore8(evm)
	case 0x59:
		return opMsize(evm)
	case 0x5E:
		return opMcopy(evm)

	// Storage
	case 0x54:
		return opSload(evm)
	case 0x55:
		return opSstore(evm)
	case 0x5C:
		return opTload(evm)
	case 0x5D:
		return opTstore(evm)

	// Control flow
	case 0x56:
		return opJump(evm)
	case 0x57:
		return opJumpi(evm)
	case 0x58:
		return opPc(evm)
	case 0x5A:
		return opGas(evm)
	case 0x5B:
		return opJumpdest(evm)

	// Log
	case 0xA0, 0xA1, 0xA2, 0xA3, 0xA4:
		return opLog(evm, int(op-0xA0))

	// System
	case 0xF0:
		return opCreate(evm, false)
	case 0xF1:
		return opCall(evm, false, false)
	case 0xF2:
		return opCallcode(evm)
	case 0xF3:
		return opReturn(evm)
	case 0xF4:
		return opDelegatecall(evm)
	case 0xF5:
		return opCreate(evm, true)
	case 0xFA:
		return opCall(evm, true, false)
	case 0xFD:
		return opRevert(evm)
	case 0xFE:
		return opInvalid(evm)
	case 0xFF:
		return opSelfdestruct(evm)

	default:
		if op >= 0x60 && op <= 0x7F {
			return opPush(evm, code, int(op-0x60)+1)
		}
		if op >= 0x80 && op <= 0x8F {
			return opDup(evm, int(op-0x80)+1)
		}
		if op >= 0x90 && op <= 0x9F {
			return opSwap(evm, int(op-0x90)+1)
		}
		return ErrInvalidOpcode
	}
}

// calcDynamicGas computes dynamic gas costs for opcodes with memory or data-dependent costs.
func (evm *EVM) calcDynamicGas(op byte, code []byte) (uint64, error) {
	switch op {
	case 0x0A: // EXP
		if evm.stack.Len() < 2 {
			return 0, nil
		}
		exp := evm.stack.PeekN(1)
		bytes := (exp.ToBig().BitLen() + 7) / 8
		return uint64(bytes) * GasExpByte, nil

	case 0x20: // KECCAK256
		if evm.stack.Len() < 2 {
			return 0, nil
		}
		offset := wordToUint64(evm.stack.PeekN(0))
		size := wordToUint64(evm.stack.PeekN(1))
		memGas, _ := evm.memory.ExpandSize(offset, size)
		words := wordCount(size)
		wordGas, ok := safeMulUint64(words, GasKeccak256Word)
		if !ok {
			return ^uint64(0), nil
		}
		total, ok := safeAddUint64(memGas, wordGas)
		if !ok {
			return ^uint64(0), nil
		}
		return total, nil

	case 0x31: // BALANCE
		if evm.stack.Len() < 1 {
			return 0, nil
		}
		addr := addressFromWord(evm.stack.PeekN(0))
		return evm.accessCostAddress(addr), nil

	case 0x3B: // EXTCODESIZE
		if evm.stack.Len() < 1 {
			return 0, nil
		}
		addr := addressFromWord(evm.stack.PeekN(0))
		return evm.accessCostAddress(addr), nil

	case 0x3C: // EXTCODECOPY
		if evm.stack.Len() < 4 {
			return 0, nil
		}
		addr := addressFromWord(evm.stack.PeekN(0))
		dstOffset := wordToUint64(evm.stack.PeekN(1))
		size := wordToUint64(evm.stack.PeekN(3))
		memGas, _ := evm.memory.ExpandSize(dstOffset, size)
		words := wordCount(size)
		copyGas, ok := safeMulUint64(words, GasCopy)
		if !ok {
			return ^uint64(0), nil
		}
		total, ok := safeAddUint64(memGas, copyGas, evm.accessCostAddress(addr))
		if !ok {
			return ^uint64(0), nil
		}
		return total, nil

	case 0x3F: // EXTCODEHASH
		if evm.stack.Len() < 1 {
			return 0, nil
		}
		addr := addressFromWord(evm.stack.PeekN(0))
		return evm.accessCostAddress(addr), nil

	case 0x37: // CALLDATACOPY
		if evm.stack.Len() < 3 {
			return 0, nil
		}
		dstOffset := wordToUint64(evm.stack.PeekN(0))
		size := wordToUint64(evm.stack.PeekN(2))
		memGas, _ := evm.memory.ExpandSize(dstOffset, size)
		words := wordCount(size)
		copyGas, ok := safeMulUint64(words, GasCopy)
		if !ok {
			return ^uint64(0), nil
		}
		total, ok := safeAddUint64(memGas, copyGas)
		if !ok {
			return ^uint64(0), nil
		}
		return total, nil

	case 0x39: // CODECOPY
		if evm.stack.Len() < 3 {
			return 0, nil
		}
		dstOffset := wordToUint64(evm.stack.PeekN(0))
		size := wordToUint64(evm.stack.PeekN(2))
		memGas, _ := evm.memory.ExpandSize(dstOffset, size)
		words := wordCount(size)
		copyGas, ok := safeMulUint64(words, GasCopy)
		if !ok {
			return ^uint64(0), nil
		}
		total, ok := safeAddUint64(memGas, copyGas)
		if !ok {
			return ^uint64(0), nil
		}
		return total, nil

	case 0x3E: // RETURNDATACOPY
		if evm.stack.Len() < 3 {
			return 0, nil
		}
		dstOffset := wordToUint64(evm.stack.PeekN(0))
		size := wordToUint64(evm.stack.PeekN(2))
		memGas, _ := evm.memory.ExpandSize(dstOffset, size)
		words := wordCount(size)
		copyGas, ok := safeMulUint64(words, GasCopy)
		if !ok {
			return ^uint64(0), nil
		}
		total, ok := safeAddUint64(memGas, copyGas)
		if !ok {
			return ^uint64(0), nil
		}
		return total, nil

	case 0x51: // MLOAD
		if evm.stack.Len() < 1 {
			return 0, nil
		}
		offset := wordToUint64(evm.stack.PeekN(0))
		memGas, _ := evm.memory.ExpandSize(offset, 32)
		return memGas, nil

	case 0x52: // MSTORE
		if evm.stack.Len() < 2 {
			return 0, nil
		}
		offset := wordToUint64(evm.stack.PeekN(0))
		memGas, _ := evm.memory.ExpandSize(offset, 32)
		return memGas, nil

	case 0x54: // SLOAD
		if evm.stack.Len() < 1 {
			return 0, nil
		}
		slot := WordToHash(evm.stack.PeekN(0))
		addr := evm.state.Contract().Address()
		return evm.accessCostStorage(addr, slot), nil

	case 0x53: // MSTORE8
		if evm.stack.Len() < 2 {
			return 0, nil
		}
		offset := wordToUint64(evm.stack.PeekN(0))
		memGas, _ := evm.memory.ExpandSize(offset, 1)
		return memGas, nil

	case 0x5E: // MCOPY
		if evm.stack.Len() < 3 {
			return 0, nil
		}
		dst := wordToUint64(evm.stack.PeekN(0))
		size := wordToUint64(evm.stack.PeekN(2))
		memGas, _ := evm.memory.ExpandSize(dst, size)
		words := wordCount(size)
		copyGas, ok := safeMulUint64(words, GasCopy)
		if !ok {
			return ^uint64(0), nil
		}
		total, ok := safeAddUint64(memGas, copyGas)
		if !ok {
			return ^uint64(0), nil
		}
		return total, nil

	case 0xA0, 0xA1, 0xA2, 0xA3, 0xA4: // LOGn
		if evm.stack.Len() < 2 {
			return 0, nil
		}
		offset := wordToUint64(evm.stack.PeekN(0))
		size := wordToUint64(evm.stack.PeekN(1))
		memGas, _ := evm.memory.ExpandSize(offset, size)
		topics := uint64(op - 0xA0)
		dataGas, ok := safeMulUint64(size, GasLogData)
		if !ok {
			return ^uint64(0), nil
		}
		topicGas, ok := safeMulUint64(topics, GasLogTopic)
		if !ok {
			return ^uint64(0), nil
		}
		total, ok := safeAddUint64(memGas, dataGas, topicGas)
		if !ok {
			return ^uint64(0), nil
		}
		return total, nil

	case 0xF3, 0xFD: // RETURN, REVERT
		if evm.stack.Len() < 2 {
			return 0, nil
		}
		offset := wordToUint64(evm.stack.PeekN(0))
		size := wordToUint64(evm.stack.PeekN(1))
		memGas, _ := evm.memory.ExpandSize(offset, size)
		return memGas, nil
	}

	return 0, nil
}

// dispatchHooks fires registered hooks for the current opcode step.
// Returns (skipExecution bool, err error). Fast path: if no opcode/step hooks
// are registered, we avoid all allocations and return immediately.
func (evm *EVM) dispatchHooks(op byte, gasRemaining uint64, gasCost uint64) (bool, error) {
	if evm.hooks == nil {
		return false, nil
	}
	opcodeHooks := evm.hooks.HooksFor(HookTypeOpcode)
	stepHooks := evm.hooks.HooksFor(HookTypeStep)
	if len(opcodeHooks) == 0 && len(stepHooks) == 0 {
		return false, nil
	}

	info := &OpcodeInfo{
		PC:           evm.pc,
		Op:           op,
		GasRemaining: gasRemaining,
		GasCost:      gasCost,
	}
	ctx := &HookContext{
		State:  NewReadOnlyState(evm.state),
		Opcode: info,
	}

	for _, hooks := range [...][]Hook{opcodeHooks, stepHooks} {
		for _, hook := range hooks {
			res, err := hook.Fire(ctx)
			if err != nil {
				return false, err
			}
			if res == nil {
				continue
			}
			switch res.Action {
			case ActionContinue:
				continue
			case ActionReplaceResult:
				if words, ok := res.ResultData.([]Word); ok {
					for _, w := range words {
						evm.stack.Push(w)
					}
				}
				evm.pc++
				return true, nil
			case ActionHalt:
				return false, res.Err
			case ActionRevert:
				return false, ErrExecutionReverted
			}
		}
	}
	return false, nil
}

// analyzeJumpdests scans code and marks all JUMPDEST locations into a bitmap
// indexed by code offset. PUSH operands are skipped so push data containing
// a 0x5B byte is not treated as a JUMPDEST.
func (evm *EVM) analyzeJumpdests(code []byte) {
	if len(code) == 0 {
		evm.jumpdests = nil
		return
	}
	dests := make([]bool, len(code))
	for i := 0; i < len(code); {
		op := code[i]
		if op == 0x5B {
			dests[i] = true
			i++
		} else if op >= 0x60 && op <= 0x7F {
			i += int(op-0x60) + 2
		} else {
			i++
		}
	}
	evm.jumpdests = dests
}

// isJumpdest reports whether dest is a valid JUMPDEST in the current code.
func (evm *EVM) isJumpdest(dest uint64) bool {
	return dest < uint64(len(evm.jumpdests)) && evm.jumpdests[dest]
}

// classifyError maps execution errors to ExecutionStatus.
func classifyError(err error) ExecutionStatus {
	switch err {
	case ErrOutOfGas:
		return StatusOutOfGas
	case ErrStackOverflow:
		return StatusStackOverflow
	case ErrStackUnderflow:
		return StatusStackUnderflow
	case ErrInvalidJump:
		return StatusInvalidJump
	case ErrInvalidOpcode:
		return StatusInvalidOpcode
	case ErrCallDepthExceeded:
		return StatusCallDepthExceeded
	case ErrExecutionReverted:
		return StatusRevert
	case ErrHalt:
		return StatusSuccess
	default:
		return StatusHalt
	}
}

// forkOrder gives a monotonic rank for each known fork. Looking up a Fork
// constant in this map is allocation-free, unlike rebuilding the map per call.
var forkOrder = map[Fork]int{
	ForkLondon:    0,
	ForkParis:     1,
	ForkShanghai:  2,
	ForkCancun:    3,
	ForkPrague:    4,
	ForkAmsterdam: 5,
	ForkOsaka:     6,
}

// forkGTE returns true if fork a is >= fork b in the fork timeline.
func forkGTE(a, b Fork) bool {
	return forkOrder[a] >= forkOrder[b]
}

// accessCostAddress returns the cold/warm access cost for an address (Berlin+).
func (evm *EVM) accessCostAddress(addr Address) uint64 {
	if !forkGTE(evm.fork, ForkLondon) {
		return 0 // pre-Berlin: no access list
	}
	al := evm.state.AccessList()
	if al.IsAddressWarmed(addr) {
		return GasWarmAccess
	}
	al.WarmAddress(addr)
	return GasColdAccountAccess
}

// accessCostStorage returns the cold/warm access cost for a storage slot (Berlin+).
func (evm *EVM) accessCostStorage(addr Address, slot Hash) uint64 {
	if !forkGTE(evm.fork, ForkLondon) {
		return 0
	}
	al := evm.state.AccessList()
	if al.IsSlotWarmed(addr, slot) {
		return GasWarmAccess
	}
	al.WarmSlot(addr, slot)
	return GasColdSload
}

// Helper functions for 256-bit arithmetic.

var uint256Max = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
var int256Max = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 255), big.NewInt(1))

func u256(x *big.Int) *big.Int {
	return new(big.Int).And(x, uint256Max)
}

func s256(x *big.Int) *big.Int {
	if x.Cmp(int256Max) > 0 {
		return new(big.Int).Sub(x, new(big.Int).Lsh(big.NewInt(1), 256))
	}
	return x
}

func toSigned(x *big.Int) *big.Int {
	if x.Bit(255) == 1 {
		return new(big.Int).Sub(x, new(big.Int).Lsh(big.NewInt(1), 256))
	}
	return x
}

func toUnsigned(x *big.Int) *big.Int {
	if x.Sign() < 0 {
		return new(big.Int).Add(x, new(big.Int).Lsh(big.NewInt(1), 256))
	}
	return x
}

// wordToUint64 safely converts a Word to uint64, capping at max.
func wordToUint64(w Word) uint64 {
	bi := w.ToBig()
	if !bi.IsUint64() {
		return ^uint64(0)
	}
	return bi.Uint64()
}
