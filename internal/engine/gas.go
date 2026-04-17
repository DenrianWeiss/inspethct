package engine

import "fmt"

// EVMGasMeter implements the GasMeter interface.
type EVMGasMeter struct {
	gas    uint64
	refund uint64
}

// NewGasMeter creates a new gas meter with the given initial gas.
func NewGasMeter(gas uint64) *EVMGasMeter {
	return &EVMGasMeter{gas: gas}
}

func (g *EVMGasMeter) Gas() uint64 {
	return g.gas
}

func (g *EVMGasMeter) ConsumeGas(amount uint64) error {
	if g.gas < amount {
		g.gas = 0
		return ErrOutOfGas
	}
	g.gas -= amount
	return nil
}

func (g *EVMGasMeter) RefundGas(amount uint64) {
	g.refund += amount
}

func (g *EVMGasMeter) DeductRefund(amount uint64) {
	if g.refund > amount {
		g.refund -= amount
	} else {
		g.refund = 0
	}
}

func (g *EVMGasMeter) Refund() uint64 {
	return g.refund
}

func (g *EVMGasMeter) ReturnGas(amount uint64) {
	g.gas += amount
}

// AvailableGas returns the current gas.
func (g *EVMGasMeter) AvailableGas() uint64 {
	return g.gas
}

// Gas constants.
const (
	GasJumpdest      uint64 = 1
	GasBase          uint64 = 2
	GasVeryLow       uint64 = 3
	GasLow           uint64 = 5
	GasMid           uint64 = 8
	GasHigh          uint64 = 10
	GasExp           uint64 = 10
	GasExpByte       uint64 = 50
	GasMemory        uint64 = 3
	GasKeccak256     uint64 = 30
	GasKeccak256Word uint64 = 6
	GasCopy          uint64 = 3
	GasBlockHash     uint64 = 20
	GasLog           uint64 = 375
	GasLogData       uint64 = 8
	GasLogTopic      uint64 = 375
	GasCreate        uint64 = 32000
	GasCodeDeposit   uint64 = 200
	GasInitCodeWord  uint64 = 2
	GasCallValue     uint64 = 9000
	GasCallStipend   uint64 = 2300
	GasNewAccount    uint64 = 25000
	GasSelfDestruct  uint64 = 5000

	// EIP-2929 cold/warm access (Berlin+)
	GasColdAccountAccess uint64 = 2600
	GasColdSload         uint64 = 2100
	GasWarmAccess        uint64 = 100
	GasFastStep          uint64 = 5

	// Storage
	GasSstoreSet      uint64 = 20000
	GasSstoreReset    uint64 = 2900
	RefundSstoreClear uint64 = 4800 // London+
)

// Gas costs per opcode (base, before memory/expansion).
// Values are for London+ unless noted.
var GasCosts = [256]uint64{
	0x00: 0,             // STOP
	0x01: GasVeryLow,    // ADD
	0x02: GasLow,        // MUL
	0x03: GasVeryLow,    // SUB
	0x04: GasLow,        // DIV
	0x05: GasLow,        // SDIV
	0x06: GasLow,        // MOD
	0x07: GasLow,        // SMOD
	0x08: GasMid,        // ADDMOD
	0x09: GasMid,        // MULMOD
	0x0A: GasExp,        // EXP (dynamic)
	0x0B: GasLow,        // SIGNEXTEND
	0x10: GasVeryLow,    // LT
	0x11: GasVeryLow,    // GT
	0x12: GasVeryLow,    // SLT
	0x13: GasVeryLow,    // SGT
	0x14: GasVeryLow,    // EQ
	0x15: GasVeryLow,    // ISZERO
	0x16: GasVeryLow,    // AND
	0x17: GasVeryLow,    // OR
	0x18: GasVeryLow,    // XOR
	0x19: GasVeryLow,    // NOT
	0x1A: GasVeryLow,    // BYTE
	0x1B: GasVeryLow,    // SHL
	0x1C: GasVeryLow,    // SHR
	0x1D: GasVeryLow,    // SAR
	0x1E: GasLow,        // CLZ
	0x20: GasKeccak256,  // KECCAK256 (dynamic)
	0x30: GasBase,       // ADDRESS
	0x31: 0,             // BALANCE (dynamic cold/warm)
	0x32: GasBase,       // ORIGIN
	0x33: GasBase,       // CALLER
	0x34: GasBase,       // CALLVALUE
	0x35: GasVeryLow,    // CALLDATALOAD
	0x36: GasBase,       // CALLDATASIZE
	0x37: GasVeryLow,    // CALLDATACOPY (dynamic)
	0x38: GasBase,       // CODESIZE
	0x39: GasVeryLow,    // CODECOPY (dynamic)
	0x3A: GasBase,       // GASPRICE
	0x3B: 0,             // EXTCODESIZE (dynamic cold/warm)
	0x3C: 0,             // EXTCODECOPY (dynamic cold/warm)
	0x3D: GasBase,       // RETURNDATASIZE
	0x3E: GasVeryLow,    // RETURNDATACOPY (dynamic)
	0x3F: 0,             // EXTCODEHASH (dynamic cold/warm)
	0x40: GasBlockHash,  // BLOCKHASH
	0x41: GasBase,       // COINBASE
	0x42: GasBase,       // TIMESTAMP
	0x43: GasBase,       // NUMBER
	0x44: GasBase,       // PREVRANDAO
	0x45: GasBase,       // GASLIMIT
	0x46: GasBase,       // CHAINID
	0x47: GasFastStep,   // SELFBALANCE
	0x48: GasBase,       // BASEFEE
	0x49: GasVeryLow,    // BLOBHASH
	0x4A: GasBase,       // BLOBBASEFEE
	0x50: GasBase,       // POP
	0x51: GasVeryLow,    // MLOAD (dynamic mem)
	0x52: GasVeryLow,    // MSTORE (dynamic mem)
	0x53: GasVeryLow,    // MSTORE8 (dynamic mem)
	0x54: 0,             // SLOAD (dynamic cold/warm)
	0x55: 0,             // SSTORE (highly dynamic)
	0x56: GasMid,        // JUMP
	0x57: GasHigh,       // JUMPI
	0x58: GasBase,       // PC
	0x59: GasBase,       // MSIZE
	0x5A: GasBase,       // GAS
	0x5B: GasJumpdest,   // JUMPDEST
	0x5C: GasWarmAccess, // TLOAD (Cancun+)
	0x5D: GasWarmAccess, // TSTORE (Cancun+)
	0x5E: GasVeryLow,    // MCOPY (dynamic mem)
	0x5F: GasBase,       // PUSH0
	0x60: GasVeryLow, 0x61: GasVeryLow, 0x62: GasVeryLow, 0x63: GasVeryLow,
	0x64: GasVeryLow, 0x65: GasVeryLow, 0x66: GasVeryLow, 0x67: GasVeryLow,
	0x68: GasVeryLow, 0x69: GasVeryLow, 0x6A: GasVeryLow, 0x6B: GasVeryLow,
	0x6C: GasVeryLow, 0x6D: GasVeryLow, 0x6E: GasVeryLow, 0x6F: GasVeryLow,
	0x70: GasVeryLow, 0x71: GasVeryLow, 0x72: GasVeryLow, 0x73: GasVeryLow,
	0x74: GasVeryLow, 0x75: GasVeryLow, 0x76: GasVeryLow, 0x77: GasVeryLow,
	0x78: GasVeryLow, 0x79: GasVeryLow, 0x7A: GasVeryLow, 0x7B: GasVeryLow,
	0x7C: GasVeryLow, 0x7D: GasVeryLow, 0x7E: GasVeryLow, 0x7F: GasVeryLow,
	// PUSH1-PUSH32
	0x80: GasVeryLow, 0x81: GasVeryLow, 0x82: GasVeryLow, 0x83: GasVeryLow,
	0x84: GasVeryLow, 0x85: GasVeryLow, 0x86: GasVeryLow, 0x87: GasVeryLow,
	0x88: GasVeryLow, 0x89: GasVeryLow, 0x8A: GasVeryLow, 0x8B: GasVeryLow,
	0x8C: GasVeryLow, 0x8D: GasVeryLow, 0x8E: GasVeryLow, 0x8F: GasVeryLow,
	// DUP1-DUP16
	0x90: GasVeryLow, 0x91: GasVeryLow, 0x92: GasVeryLow, 0x93: GasVeryLow,
	0x94: GasVeryLow, 0x95: GasVeryLow, 0x96: GasVeryLow, 0x97: GasVeryLow,
	0x98: GasVeryLow, 0x99: GasVeryLow, 0x9A: GasVeryLow, 0x9B: GasVeryLow,
	0x9C: GasVeryLow, 0x9D: GasVeryLow, 0x9E: GasVeryLow, 0x9F: GasVeryLow,
	// SWAP1-SWAP16
	0xA0: GasLog, 0xA1: GasLog, 0xA2: GasLog, 0xA3: GasLog, 0xA4: GasLog,
	// LOG0-LOG4 (dynamic)
	0xF0: GasCreate, // CREATE (dynamic)
	0xF1: 0,         // CALL (dynamic)
	0xF2: 0,         // CALLCODE (dynamic)
	0xF3: 0,         // RETURN (dynamic mem)
	0xF4: 0,         // DELEGATECALL (dynamic)
	0xF5: GasCreate, // CREATE2 (dynamic)
	0xFA: 0,         // STATICCALL (dynamic)
	0xFD: 0,         // REVERT (dynamic mem)
	0xFE: 0,         // INVALID
	0xFF: 0,         // SELFDESTRUCT (dynamic)
}

// MinForkForOpcode returns the minimum fork required for an opcode.
// Returns ForkLondon for base opcodes.
func MinForkForOpcode(op byte) Fork {
	switch op {
	case 0x3D, 0x3E, 0xFA, 0xFD:
		return ForkLondon // Actually Byzantium, but we support London+
	case 0x1B, 0x1C, 0x1D, 0x3F, 0xF5:
		return ForkLondon // Actually Constantinople
	case 0x46, 0x47:
		return ForkLondon // Actually Istanbul
	case 0x48:
		return ForkLondon // Actually London
	case 0x44:
		return ForkParis
	case 0x5F:
		return ForkShanghai
	case 0x5C, 0x5D, 0x5E, 0x49, 0x4A:
		return ForkCancun
	case 0x1E:
		return ForkAmsterdam
	default:
		return ForkLondon
	}
}

// StackDelta returns the net stack change (pops negative, pushes positive).
func StackDelta(op byte) int {
	switch {
	case op == 0x00: // STOP
		return 0
	case op >= 0x01 && op <= 0x0B: // arithmetic (2 in, 1 out)
		if op == 0x0B { // SIGNEXTEND
			return -1
		}
		return -1 // 2 pops, 1 push = -1 net
	case op >= 0x10 && op <= 0x1E: // comparison/bitwise
		if op == 0x15 || op == 0x19 { // ISZERO, NOT
			return 0
		}
		return -1
	case op == 0x20: // KECCAK256
		return -1
	case op >= 0x30 && op <= 0x48: // environmental
		switch op {
		case 0x31, 0x3B, 0x3F: // BALANCE, EXTCODESIZE, EXTCODEHASH
			return 0 // 1 pop, 1 push
		case 0x35: // CALLDATALOAD
			return 0 // 1 pop, 1 push
		case 0x37, 0x39: // CALLDATACOPY, CODECOPY
			return -3
		case 0x3C: // EXTCODECOPY
			return -4
		}
		return 1
	case op == 0x49 || op == 0x4A: // BLOBHASH, BLOBBASEFEE
		if op == 0x49 {
			return 0 // 1 pop, 1 push
		}
		return 1
	case op == 0x3D: // RETURNDATASIZE
		return 1
	case op == 0x3E: // RETURNDATACOPY
		return -3
	case op == 0x40: // BLOCKHASH
		return 0 // 1 pop, 1 push
	case op == 0x50: // POP
		return -1
	case op == 0x51 || op == 0x54 || op == 0x5C: // MLOAD, SLOAD, TLOAD
		return 0
	case op == 0x52 || op == 0x53 || op == 0x55 || op == 0x5D: // MSTORE, MSTORE8, SSTORE, TSTORE
		return -2
	case op == 0x56: // JUMP
		return -1
	case op == 0x57: // JUMPI
		return -2
	case op == 0x58 || op == 0x59 || op == 0x5A || op == 0x5B: // PC, MSIZE, GAS, JUMPDEST
		return 1
	case op == 0x5E: // MCOPY
		return -3
	case op == 0x5F: // PUSH0
		return 1
	case op >= 0x60 && op <= 0x7F: // PUSH1-PUSH32
		return 1
	case op >= 0x80 && op <= 0x8F: // DUP1-DUP16
		return 1
	case op >= 0x90 && op <= 0x9F: // SWAP1-SWAP16
		return 0
	case op >= 0xA0 && op <= 0xA4: // LOG0-LOG4
		return -(2 + int(op-0xA0)) // LOGn: pop offset, size + n topics
	case op == 0xF0 || op == 0xF5: // CREATE, CREATE2
		return -2
	case op == 0xF1 || op == 0xF2: // CALL, CALLCODE
		return -6
	case op == 0xF3 || op == 0xFD: // RETURN, REVERT
		return -2
	case op == 0xF4 || op == 0xFA: // DELEGATECALL, STATICCALL
		return -5
	case op == 0xFF: // SELFDESTRUCT
		return -1
	default:
		return 0
	}
}

var opcodeNames = map[byte]string{
	0x00: "STOP", 0x01: "ADD", 0x02: "MUL", 0x03: "SUB", 0x04: "DIV",
	0x05: "SDIV", 0x06: "MOD", 0x07: "SMOD", 0x08: "ADDMOD", 0x09: "MULMOD",
	0x0A: "EXP", 0x0B: "SIGNEXTEND", 0x10: "LT", 0x11: "GT", 0x12: "SLT",
	0x13: "SGT", 0x14: "EQ", 0x15: "ISZERO", 0x16: "AND", 0x17: "OR",
	0x18: "XOR", 0x19: "NOT", 0x1A: "BYTE", 0x1B: "SHL", 0x1C: "SHR",
	0x1D: "SAR", 0x1E: "CLZ", 0x20: "KECCAK256", 0x30: "ADDRESS",
	0x31: "BALANCE", 0x32: "ORIGIN", 0x33: "CALLER", 0x34: "CALLVALUE",
	0x35: "CALLDATALOAD", 0x36: "CALLDATASIZE", 0x37: "CALLDATACOPY",
	0x38: "CODESIZE", 0x39: "CODECOPY", 0x3A: "GASPRICE", 0x3B: "EXTCODESIZE",
	0x3C: "EXTCODECOPY", 0x3D: "RETURNDATASIZE", 0x3E: "RETURNDATACOPY",
	0x3F: "EXTCODEHASH", 0x40: "BLOCKHASH", 0x41: "COINBASE", 0x42: "TIMESTAMP",
	0x43: "NUMBER", 0x44: "PREVRANDAO", 0x45: "GASLIMIT", 0x46: "CHAINID",
	0x47: "SELFBALANCE", 0x48: "BASEFEE", 0x49: "BLOBHASH", 0x4A: "BLOBBASEFEE",
	0x50: "POP", 0x51: "MLOAD", 0x52: "MSTORE", 0x53: "MSTORE8", 0x54: "SLOAD",
	0x55: "SSTORE", 0x56: "JUMP", 0x57: "JUMPI", 0x58: "PC", 0x59: "MSIZE",
	0x5A: "GAS", 0x5B: "JUMPDEST", 0x5C: "TLOAD", 0x5D: "TSTORE", 0x5E: "MCOPY",
	0x5F: "PUSH0",
	0xF0: "CREATE", 0xF1: "CALL", 0xF2: "CALLCODE", 0xF3: "RETURN",
	0xF4: "DELEGATECALL", 0xF5: "CREATE2", 0xFA: "STATICCALL",
	0xFD: "REVERT", 0xFE: "INVALID", 0xFF: "SELFDESTRUCT",
}

var allOpcodeNames [256]string

func init() {
	for i := 0; i <= 255; i++ {
		op := byte(i)
		if name, ok := opcodeNames[op]; ok {
			allOpcodeNames[i] = name
		} else if op >= 0x60 && op <= 0x7F {
			allOpcodeNames[i] = fmt.Sprintf("PUSH%d", int(op-0x60)+1)
		} else if op >= 0x80 && op <= 0x8F {
			allOpcodeNames[i] = fmt.Sprintf("DUP%d", int(op-0x80)+1)
		} else if op >= 0x90 && op <= 0x9F {
			allOpcodeNames[i] = fmt.Sprintf("SWAP%d", int(op-0x90)+1)
		} else if op >= 0xA0 && op <= 0xA4 {
			allOpcodeNames[i] = fmt.Sprintf("LOG%d", int(op-0xA0))
		} else {
			allOpcodeNames[i] = fmt.Sprintf("UNKNOWN(0x%02X)", op)
		}
	}
}

// OpcodeName returns the human-readable name for an opcode byte.
func OpcodeName(op byte) string {
	return allOpcodeNames[op]
}
