package engine

import (
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"testing"
)

func TestSstoreInFixture(t *testing.T) {
	path := filepath.Join("evm-tests", "GeneralStateTests", "VMTests", "vmArithmeticTest", "add.json")
	data, _ := os.ReadFile(path)
	var fixtures map[string]stFixture
	json.Unmarshal(data, &fixtures)

	for _, fix := range fixtures {
		for forkName, entries := range fix.Post {
			if forkName != "Cancun" {
				continue
			}
			for _, entry := range entries {
				if entry.Indexes.Data != 2 || entry.Indexes.Gas != 0 || entry.Indexes.Value != 0 {
					continue
				}

				acc, sto := clonePreState(fix.Pre)
				sender := hexToAddr(fix.Transaction.Sender)
				to := hexToAddr(fix.Transaction.To)
				dataBytes := hexToBytes(fix.Transaction.Data[entry.Indexes.Data])
				gasLimit := hexToBig(fix.Transaction.GasLimit[entry.Indexes.Gas]).Uint64()
				value := hexToBig(fix.Transaction.Value[entry.Indexes.Value])
				gasPrice := hexToBig(fix.Transaction.GasPrice)

				acc.IncrementNonce(sender)
				if value.Sign() > 0 {
					acc.SubBalance(sender, value)
					acc.AddBalance(to, value)
				}

				blockCtx := &SimpleBlockContext{
					CoinbaseVal:   hexToAddr(fix.Env.CurrentCoinbase),
					TimestampVal:  hexToBig(fix.Env.CurrentTimestamp).Uint64(),
					NumberVal:     hexToBig(fix.Env.CurrentNumber),
					DifficultyVal: hexToBig(fix.Env.CurrentDifficulty),
					GasLimitVal:   hexToBig(fix.Env.CurrentGasLimit).Uint64(),
					BaseFeeVal:    hexToBig(fix.Env.CurrentBaseFee),
					ChainIDVal:    big.NewInt(1),
				}
				txCtx := &SimpleTxContext{OriginVal: sender, GasPriceVal: gasPrice}
				code := acc.Code(to)

				intrinsic := intrinsicGas(dataBytes)
				evmGasLimit := gasLimit - intrinsic

				cfg := &ExecutionConfig{
					Fork:             ForkLondon,
					GasLimit:         evmGasLimit,
					Value:            value,
					Input:            dataBytes,
					Origin:           sender,
					Caller:           sender,
					ContractAddress:  to,
					Code:             code,
					BlockContext:     blockCtx,
					TxContext:        txCtx,
					State:            acc,
					Storage:          sto,
					TransientStorage: NewInMemoryTransientStorage(),
					AccessList:       NewSimpleAccessList(),
				}

				state, _ := NewSimpleEngine().NewState(cfg)
				evm := NewEVM(state, ForkLondon)

				// Run until CALL, then inspect child
				reg := NewSimpleHookRegistry()
				hook := &testHook{
					hookType: HookTypeStep,
					id:       "trace",
					fireFn: func(ctx *HookContext) (*HookResult, error) {
						op := ctx.Opcode.Op
						if op == 0x55 { // SSTORE
							addr := ctx.State.ContractAddress()
							slot := ctx.State.StackPeekN(0)
							val := ctx.State.StackPeekN(1)
							fmt.Printf("SSTORE addr=%x slot=%x val=%x\n", addr, slot, val)
							fmt.Printf("  storage current=%x\n", sto.Get(addr, WordToHash(slot)))
							fmt.Printf("  storage original=%x\n", sto.Original(addr, WordToHash(slot)))
							fmt.Printf("  slotWarmed=%v\n", ctx.State.IsSlotWarmed(addr, WordToHash(slot)))
						}
						return &HookResult{Action: ActionContinue}, nil
					},
				}
				reg.Register(hook)
				evm.SetHooks(reg)

				res, _ := evm.Run(code)
				fmt.Printf("Result status=%d gasRem=%d\n", res.Status, res.GasRemaining)
				return
			}
		}
	}
}
