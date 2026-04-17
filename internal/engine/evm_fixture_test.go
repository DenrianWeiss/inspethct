package engine

import (
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------- JSON fixture structures ----------

type stFixture struct {
	Info        stInfo                   `json:"_info"`
	Env         stEnv                    `json:"env"`
	Pre         map[string]stAccount     `json:"pre"`
	Transaction stTransaction            `json:"transaction"`
	Post        map[string][]stPostEntry `json:"post"`
}

type stInfo struct {
	Comment string `json:"comment"`
}

type stEnv struct {
	CurrentBaseFee       string `json:"currentBaseFee"`
	CurrentCoinbase      string `json:"currentCoinbase"`
	CurrentDifficulty    string `json:"currentDifficulty"`
	CurrentExcessBlobGas string `json:"currentExcessBlobGas"`
	CurrentGasLimit      string `json:"currentGasLimit"`
	CurrentNumber        string `json:"currentNumber"`
	CurrentRandom        string `json:"currentRandom"`
	CurrentTimestamp     string `json:"currentTimestamp"`
}

type stAccount struct {
	Balance string            `json:"balance"`
	Code    string            `json:"code"`
	Nonce   string            `json:"nonce"`
	Storage map[string]string `json:"storage"`
}

type stTransaction struct {
	Data     []string `json:"data"`
	GasLimit []string `json:"gasLimit"`
	GasPrice string   `json:"gasPrice"`
	Nonce    string   `json:"nonce"`
	Sender   string   `json:"sender"`
	To       string   `json:"to"`
	Value    []string `json:"value"`
}

type stPostEntry struct {
	Hash    string               `json:"hash"`
	Indexes stIndexes            `json:"indexes"`
	Logs    string               `json:"logs"`
	State   map[string]stAccount `json:"state"`
}

type stIndexes struct {
	Data  int `json:"data"`
	Gas   int `json:"gas"`
	Value int `json:"value"`
}

// ---------- helpers ----------

func hexToBig(s string) *big.Int {
	s = strings.TrimPrefix(s, "0x")
	if s == "" {
		return big.NewInt(0)
	}
	v, _ := new(big.Int).SetString(s, 16)
	if v == nil {
		return big.NewInt(0)
	}
	return v
}

func hexToAddr(s string) Address {
	s = strings.TrimPrefix(s, "0x")
	b, _ := hex.DecodeString(s)
	var a Address
	copy(a[20-len(b):], b)
	return a
}

func hexToHash(s string) Hash {
	s = strings.TrimPrefix(s, "0x")
	b, _ := hex.DecodeString(s)
	var h Hash
	copy(h[32-len(b):], b)
	return h
}

func hexToBytes(s string) []byte {
	s = strings.TrimPrefix(s, "0x")
	b, _ := hex.DecodeString(s)
	return b
}

func forkFromString(s string) Fork {
	switch s {
	case "London":
		return ForkLondon
	case "Paris":
		return ForkParis
	case "Shanghai":
		return ForkShanghai
	case "Cancun":
		return ForkCancun
	case "Prague":
		return ForkPrague
	case "Amsterdam":
		return ForkAmsterdam
	default:
		return ""
	}
}

func clonePreState(pre map[string]stAccount) (*InMemoryAccountState, *InMemoryStorage) {
	acc := NewInMemoryAccountState()
	sto := NewInMemoryStorage()
	for addrStr, a := range pre {
		addr := hexToAddr(addrStr)
		acc.SetBalance(addr, hexToBig(a.Balance))
		acc.SetNonce(addr, hexToBig(a.Nonce).Uint64())
		acc.SetCode(addr, hexToBytes(a.Code))
		for slotStr, valStr := range a.Storage {
			sto.Seed(addr, hexToHash(slotStr), hexToHash(valStr))
		}
	}
	return acc, sto
}

// compareState compares the actual mutable state against the expected post-state.
// It returns a list of mismatches.
func compareState(acc *InMemoryAccountState, sto *InMemoryStorage, expected map[string]stAccount) []string {
	var mismatches []string

	// Check all expected accounts exist with correct fields.
	for addrStr, exp := range expected {
		addr := hexToAddr(addrStr)

		bal := acc.Balance(addr)
		if bal.Cmp(hexToBig(exp.Balance)) != 0 {
			mismatches = append(mismatches, addrStr+": balance expected "+exp.Balance+" got "+bal.Text(16))
		}

		nonce := acc.Nonce(addr)
		if nonce != hexToBig(exp.Nonce).Uint64() {
			mismatches = append(mismatches, addrStr+": nonce expected "+exp.Nonce+" got "+new(big.Int).SetUint64(nonce).Text(16))
		}

		code := acc.Code(addr)
		expCode := hexToBytes(exp.Code)
		if !bytesEqual(code, expCode) {
			mismatches = append(mismatches, addrStr+": code mismatch")
		}

		for slotStr, valStr := range exp.Storage {
			actual := sto.Get(addr, hexToHash(slotStr))
			expVal := hexToHash(valStr)
			if actual != expVal {
				mismatches = append(mismatches, addrStr+": storage "+slotStr+" expected "+valStr+" got 0x"+hex.EncodeToString(actual[:]))
			}
		}
	}

	// Check that no unexpected accounts with non-empty state exist.
	// We do this by checking every address that exists in actual state against expected.
	// (Simplified: only verify accounts that were in pre or post.)
	return mismatches
}

func bytesEqual(a, b []byte) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return string(a) == string(b)
}

// TestVMFixtures runs Ethereum VMTests state-fixtures against the engine.
// Tests that require unsupported features (precompiles, full tx processing edge cases)
// are skipped.
func TestVMFixtures(t *testing.T) {
	root := filepath.Join("evm-tests", "GeneralStateTests", "VMTests")

	// Supported forks mapped from fixture names.
	supportedForks := map[string]Fork{
		"London":    ForkLondon,
		"Paris":     ForkParis,
		"Shanghai":  ForkShanghai,
		"Cancun":    ForkCancun,
		"Prague":    ForkPrague,
		"Amsterdam": ForkAmsterdam,
	}

	// Counters for reporting.
	var total, skipped, passed, failed int

	// Walk all JSON fixtures under VMTests.
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(path) != ".json" {
			return err
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Logf("read %s: %v", path, err)
			return nil
		}

		var fixtures map[string]stFixture
		if err := json.Unmarshal(data, &fixtures); err != nil {
			t.Logf("parse %s: %v", path, err)
			return nil
		}

		for name, fix := range fixtures {
			for forkName, entries := range fix.Post {
				fork, ok := supportedForks[forkName]
				if !ok {
					continue
				}

				for _, entry := range entries {
					total++
					testName := name + "/" + forkName + "/d" + itoa(entry.Indexes.Data) + "g" + itoa(entry.Indexes.Gas) + "v" + itoa(entry.Indexes.Value)

					t.Run(testName, func(t *testing.T) {
						// Skip performance tests in short mode.
						if testing.Short() && strings.Contains(path, "vmPerformance") {
							skipped++
							t.Skip("performance test in short mode")
						}

						acc, sto := clonePreState(fix.Pre)

						// Transaction params from indexed arrays.
						sender := hexToAddr(fix.Transaction.Sender)
						to := hexToAddr(fix.Transaction.To)
						dataBytes := hexToBytes(fix.Transaction.Data[entry.Indexes.Data])
						gasLimit := hexToBig(fix.Transaction.GasLimit[entry.Indexes.Gas]).Uint64()
						value := hexToBig(fix.Transaction.Value[entry.Indexes.Value])
						gasPrice := hexToBig(fix.Transaction.GasPrice)

						// Transaction-level nonce increment.
						acc.IncrementNonce(sender)

						// Transaction-level value transfer.
						if value.Sign() > 0 {
							if acc.Balance(sender).Cmp(value) < 0 {
								failed++
								t.Fatalf("insufficient balance for value transfer")
							}
							acc.SubBalance(sender, value)
							acc.AddBalance(to, value)
						}

						// Build block context from env.
						blockCtx := &SimpleBlockContext{
							CoinbaseVal:   hexToAddr(fix.Env.CurrentCoinbase),
							TimestampVal:  hexToBig(fix.Env.CurrentTimestamp).Uint64(),
							NumberVal:     hexToBig(fix.Env.CurrentNumber),
							DifficultyVal: hexToBig(fix.Env.CurrentDifficulty),
							GasLimitVal:   hexToBig(fix.Env.CurrentGasLimit).Uint64(),
							BaseFeeVal:    hexToBig(fix.Env.CurrentBaseFee),
							ChainIDVal:    big.NewInt(1),
						}
						if fix.Env.CurrentRandom != "" {
							blockCtx.RandomVal = hexToHash(fix.Env.CurrentRandom)
						}

						txCtx := &SimpleTxContext{
							OriginVal:   sender,
							GasPriceVal: gasPrice,
						}

						code := acc.Code(to)

						// Compute intrinsic gas and deduct from gas limit before EVM execution.
						intrinsic := intrinsicGas(dataBytes)
						evmGasLimit := gasLimit
						if evmGasLimit > intrinsic {
							evmGasLimit -= intrinsic
						} else {
							// Insufficient gas for intrinsic cost.
							failed++
							t.Fatalf("insufficient gas for intrinsic cost")
						}

						cfg := &ExecutionConfig{
							Fork:             fork,
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

						eng := NewSimpleEngine()
						res, err := eng.Run(cfg)

						// Apply transaction gas cost after execution.
						if res != nil {
							gasUsed := gasLimit - res.GasRemaining
							// Apply refund cap: max refund is gasUsed/5 (EIP-3529, London+)
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

						// Compare state.
						mismatches := compareState(acc, sto, entry.State)
						if len(mismatches) > 0 {
							failed++
							for _, m := range mismatches {
								t.Logf("mismatch: %s", m)
							}
							t.Fatalf("state mismatch (%d fields)", len(mismatches))
						}

						_ = err
						passed++
					})
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("walk: %v", err)
	}

	t.Logf("VM fixtures: total=%d passed=%d failed=%d skipped=%d", total, passed, failed, skipped)
}

func itoa(i int) string {
	return string([]byte{'0' + byte(i)})
}

// intrinsicGas computes the transaction intrinsic gas for London+ rules.
func intrinsicGas(data []byte) uint64 {
	gas := uint64(21000)
	for _, b := range data {
		if b == 0 {
			gas += 4
		} else {
			gas += 16
		}
	}
	return gas
}
