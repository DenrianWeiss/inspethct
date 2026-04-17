package engine

import (
	"fmt"
	"math/big"
	"testing"
)

type traceHook struct {
	id string
}

func (h *traceHook) Type() HookType { return HookTypeStep }
func (h *traceHook) OneTime() bool  { return false }
func (h *traceHook) ID() string     { return h.id }
func (h *traceHook) Fire(ctx *HookContext) (*HookResult, error) {
	stackStr := ""
	for i := 0; i < ctx.State.StackLen(); i++ {
		if i > 0 {
			stackStr += ","
		}
		stackStr += fmt.Sprintf("%x", ctx.State.StackPeekN(i).ToBig().Text(16))
	}
	fmt.Printf("PC=%d OP=%s Stack=[%s]\n", ctx.Opcode.PC, OpcodeName(ctx.Opcode.Op), stackStr)
	return &HookResult{Action: ActionContinue}, nil
}

func TestDebugTraceAddmod(t *testing.T) {
	childCode := []byte{0x60, 0x03, 0x60, 0x01, 0x60, 0x06, 0x60, 0x00, 0x03, 0x08, 0x60, 0x00, 0x55, 0x00}

	acc := NewInMemoryAccountState()
	sto := NewInMemoryStorage()

	childAddr := Address{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x10, 0x02}

	acc.SetBalance(childAddr, big.NewInt(1e18))
	acc.SetNonce(childAddr, 0)
	acc.SetCode(childAddr, childCode)

	cfg := &ExecutionConfig{
		Fork:            ForkCancun,
		GasLimit:        1000000,
		Value:           big.NewInt(0),
		Input:           nil,
		Origin:          childAddr,
		Caller:          childAddr,
		ContractAddress: childAddr,
		Code:            childCode,
		CodeHash:        hashCode(childCode),
		BlockContext: &SimpleBlockContext{
			CoinbaseVal:   Address{},
			TimestampVal:  0,
			NumberVal:     big.NewInt(0),
			DifficultyVal: big.NewInt(0),
			GasLimitVal:   10000000,
			BaseFeeVal:    big.NewInt(0),
			ChainIDVal:    big.NewInt(1),
		},
		TxContext: &SimpleTxContext{
			OriginVal:   childAddr,
			GasPriceVal: big.NewInt(0),
		},
		State:            acc,
		Storage:          sto,
		TransientStorage: NewInMemoryTransientStorage(),
		AccessList:       NewSimpleAccessList(),
	}

	reg := NewSimpleHookRegistry()
	reg.Register(&traceHook{id: "trace"})

	evm := NewEVM(NewEVMState(NewStack(), NewMemory(), sto, NewInMemoryTransientStorage(), acc, NewGasMeter(cfg.GasLimit), cfg.BlockContext, cfg.TxContext, &SimpleContract{AddressVal: childAddr, CallerVal: childAddr, CallValueVal: big.NewInt(0), CallInputVal: nil, CodeVal: childCode, CodeHashVal: hashCode(childCode), CodeAddrVal: childAddr, IsStaticVal: false}, NewSimpleAccessList()), cfg.Fork)
	evm.SetHooks(reg)

	res, err := evm.Run(childCode)

	fmt.Printf("Execution: status=%d gasRemaining=%d err=%v\n", res.Status, res.GasRemaining, err)

	childStorage := sto.Get(childAddr, Hash{})
	fmt.Printf("Storage[0]: 0x%x\n", childStorage)
	fmt.Printf("Expected: 0x02\n")
}
