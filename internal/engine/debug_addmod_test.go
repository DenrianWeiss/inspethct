package engine

import (
	"fmt"
	"math/big"
	"testing"
)

func TestDebugAddmodD0(t *testing.T) {
	// Replicate addmod.json d0 test case exactly
	acc := NewInMemoryAccountState()
	sto := NewInMemoryStorage()

	sender := hexToAddr("0xa94f5374fce5edbc8e2a8697c15331677e6ebf0b")
	to := hexToAddr("0xcccccccccccccccccccccccccccccccccccccccc")
	target := hexToAddr("0x0000000000000000000000000000000000001000")

	// Set up pre-state balances
	acc.SetBalance(sender, hexToBig("0x0ba1a9ce0ba1a9ce"))
	acc.SetBalance(to, hexToBig("0x0ba1a9ce0ba1a9ce"))
	acc.SetBalance(target, hexToBig("0x0ba1a9ce0ba1a9ce"))

	// Dispatcher code at 0xcccc...
	acc.SetCode(to, hexToBytes("0x600060006000600060006004356110000162fffffff100"))
	// Target code at 0x1000: PUSH1 2 PUSH1 2 PUSH1 1 ADDMOD PUSH1 0 SSTORE STOP
	acc.SetCode(target, hexToBytes("0x6002600260010860005500"))

	dataBytes := hexToBytes("0x693c61390000000000000000000000000000000000000000000000000000000000000000")
	gasLimit := uint64(0x04c4b400)
	value := big.NewInt(0)
	gasPrice := hexToBig("0x0a")

	acc.IncrementNonce(sender)

	blockCtx := &SimpleBlockContext{
		CoinbaseVal:   hexToAddr("0x2adc25665018aa1fe0e6bc666dac8fc2697ff9ba"),
		TimestampVal:  0x03e8,
		NumberVal:     big.NewInt(1),
		DifficultyVal: hexToBig("0x020000"),
		GasLimitVal:   0x05f5e100,
		BaseFeeVal:    hexToBig("0x0a"),
		ChainIDVal:    big.NewInt(1),
		RandomVal:     hexToHash("0x0000000000000000000000000000000000000000000000000000000000020000"),
	}
	txCtx := &SimpleTxContext{
		OriginVal:   sender,
		GasPriceVal: gasPrice,
	}

	code := acc.Code(to)

	intrinsic := intrinsicGas(dataBytes)
	evmGasLimit := gasLimit - intrinsic

	cfg := &ExecutionConfig{
		Fork:             ForkCancun,
		GasLimit:         evmGasLimit,
		Value:            value,
		Input:            dataBytes,
		Origin:           sender,
		Caller:           sender,
		ContractAddress:  to,
		Code:             code,
		CodeHash:         hashCode(code),
		BlockContext:     blockCtx,
		TxContext:        txCtx,
		State:            acc,
		Storage:          sto,
		TransientStorage: NewInMemoryTransientStorage(),
		AccessList:       NewSimpleAccessList(),
	}

	// Enable step-by-step tracing
	eng := NewSimpleEngine()
	state, _ := eng.NewState(cfg)
	evm := NewEVM(state, cfg.Fork)

	// Trace each opcode
	trace := &addmodTraceHook{t: t}
	hooks := NewSimpleHookRegistry()
	hooks.Register(trace)
	evm.SetHooks(hooks)

	res, err := evm.Run(cfg.Code)

	t.Logf("Result: status=%d gasRemaining=%d gasRefund=%d err=%v", res.Status, res.GasRemaining, res.GasRefund, err)

	// Apply transaction gas accounting
	if res != nil {
		gasUsed := gasLimit - res.GasRemaining
		refund := res.GasRefund
		maxRefund := gasUsed / 5
		if refund > maxRefund {
			refund = maxRefund
		}
		if gasUsed > refund {
			gasUsed -= refund
		} else {
			gasUsed = 0
		}
		cost := new(big.Int).Mul(new(big.Int).SetUint64(gasUsed), gasPrice)
		acc.SubBalance(sender, cost)
		acc.AddBalance(blockCtx.CoinbaseVal, cost)
	}

	t.Logf("Sender balance: %s (expected 0ba1a9ce0b9aa731)", acc.Balance(sender).Text(16))
	t.Logf("Target storage[0]: %x (expected 0x01)", sto.Get(target, Hash{}))
}

type addmodTraceHook struct {
	t *testing.T
}

func (h *addmodTraceHook) Type() HookType { return HookTypeOpcode }
func (h *addmodTraceHook) OneTime() bool  { return false }
func (h *addmodTraceHook) ID() string     { return "addmod-trace" }

func (h *addmodTraceHook) Fire(ctx *HookContext) (*HookResult, error) {
	info := ctx.Opcode
	state := ctx.State
	stackStr := ""
	for i := 0; i < state.StackLen(); i++ {
		w := state.StackPeekN(i)
		stackStr = fmt.Sprintf("%s %s", w.ToBig().Text(16), stackStr)
	}
	h.t.Logf("PC=%02X Op=%-12s Gas=%8d Stack=[%s]", info.PC, OpcodeName(info.Op), state.GasRemaining(), stackStr)
	return nil, nil
}
