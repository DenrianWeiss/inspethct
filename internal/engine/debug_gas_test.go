package engine

import (
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"testing"
)

func TestDebugGasAddAll(t *testing.T) {
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
				if entry.Indexes.Gas != 0 || entry.Indexes.Value != 0 {
					continue
				}
				if entry.Indexes.Data != 2 {
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
					CoinbaseVal:    hexToAddr(fix.Env.CurrentCoinbase),
					TimestampVal:   hexToBig(fix.Env.CurrentTimestamp).Uint64(),
					NumberVal:      hexToBig(fix.Env.CurrentNumber),
					DifficultyVal:  hexToBig(fix.Env.CurrentDifficulty),
					GasLimitVal:    hexToBig(fix.Env.CurrentGasLimit).Uint64(),
					BaseFeeVal:     hexToBig(fix.Env.CurrentBaseFee),
					ChainIDVal:     big.NewInt(1),
				}
				txCtx := &SimpleTxContext{OriginVal: sender, GasPriceVal: gasPrice}
				code := acc.Code(to)

				intrinsic := intrinsicGas(dataBytes)
				evmGasLimit := gasLimit - intrinsic

				cfg := &ExecutionConfig{
					Fork:            ForkLondon,
					GasLimit:        evmGasLimit,
					Value:           value,
					Input:           dataBytes,
					Origin:          sender,
					Caller:          sender,
					ContractAddress: to,
					Code:            code,
					BlockContext:    blockCtx,
					TxContext:       txCtx,
					State:           acc,
					Storage:         sto,
					TransientStorage: NewInMemoryTransientStorage(),
					AccessList:      NewSimpleAccessList(),
				}

				state, _ := NewSimpleEngine().NewState(cfg)
				evm := NewEVM(state, ForkLondon)

				reg := NewSimpleHookRegistry()
				hook := &testHook{
					hookType: HookTypeStep,
					id:       "trace",
					fireFn: func(ctx *HookContext) (*HookResult, error) {
						fmt.Printf("  [d=%d pc=%d] %s gas=%d\n", ctx.State.CallDepth(), ctx.State.PC(), OpcodeName(ctx.Opcode.Op), ctx.State.GasRemaining())
						return &HookResult{Action: ActionContinue}, nil
					},
				}
				reg.Register(hook)
				evm.SetHooks(reg)

				res, _ := evm.Run(code)

				gasUsed := gasLimit - res.GasRemaining
				refund := res.GasRefund
				maxRefund := gasUsed / 5
				if refund > maxRefund {
					refund = maxRefund
				}
				gasUsed -= refund

				fmt.Printf("d=%d: GasUsed=%d Refund=%d GasRem=%d\n",
					entry.Indexes.Data,
					gasUsed,
					res.GasRefund,
					res.GasRemaining,
				)
			}
			break
		}
		break
	}
}
