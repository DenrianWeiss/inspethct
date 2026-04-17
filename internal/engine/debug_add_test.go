package engine

import (
	"fmt"
	"math/big"
	"testing"
)

func TestDebugAdd(t *testing.T) {
	acc := NewInMemoryAccountState()
	sto := NewInMemoryStorage()

	// Setup accounts from add.json pre-state
	accounts := map[string]struct {
		bal   string
		code  string
		nonce string
	}{
		"0x0000000000000000000000000000000000001000": {"0x0ba1a9ce0ba1a9ce", "0x7fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff7fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff0160005500", "0x00"},
		"0x0000000000000000000000000000000000001001": {"0x0ba1a9ce0ba1a9ce", "0x60047fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff0160005500", "0x00"},
		"0x0000000000000000000000000000000000001002": {"0x0ba1a9ce0ba1a9ce", "0x60017fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff0160005500", "0x00"},
		"0x0000000000000000000000000000000000001003": {"0x0ba1a9ce0ba1a9ce", "0x600060000160005500", "0x00"},
		"0x0000000000000000000000000000000000001004": {"0x0ba1a9ce0ba1a9ce", "0x7fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff60010160005500", "0x00"},
		"0xa94f5374fce5edbc8e2a8697c15331677e6ebf0b": {"0x0ba1a9ce0ba1a9ce", "0x", "0x00"},
		"0xcccccccccccccccccccccccccccccccccccccccc": {"0x0ba1a9ce0ba1a9ce", "0x600060006000600060006004356110000162fffffff100", "0x00"},
	}

	for addrStr, a := range accounts {
		addr := hexToAddr(addrStr)
		acc.SetBalance(addr, hexToBig(a.bal))
		acc.SetNonce(addr, hexToBig(a.nonce).Uint64())
		acc.SetCode(addr, hexToBytes(a.code))
	}

	sender := hexToAddr("0xa94f5374fce5edbc8e2a8697c15331677e6ebf0b")
	to := hexToAddr("0xcccccccccccccccccccccccccccccccccccccccc")
	dataBytes := hexToBytes("0x693c61390000000000000000000000000000000000000000000000000000000000000000")
	gasLimit := hexToBig("0x04c4b400").Uint64()
	value := hexToBig("0x01")
	gasPrice := hexToBig("0x0a")

	acc.IncrementNonce(sender)
	acc.SubBalance(sender, value)
	acc.AddBalance(to, value)

	blockCtx := &SimpleBlockContext{
		CoinbaseVal:   hexToAddr("0x2adc25665018aa1fe0e6bc666dac8fc2697ff9ba"),
		TimestampVal:  hexToBig("0x03e8").Uint64(),
		NumberVal:     hexToBig("0x01"),
		DifficultyVal: hexToBig("0x020000"),
		GasLimitVal:   hexToBig("0x05f5e100").Uint64(),
		BaseFeeVal:    hexToBig("0x0a"),
		ChainIDVal:    big.NewInt(1),
		RandomVal:     hexToHash("0x0000000000000000000000000000000000000000000000000000000000020000"),
	}

	txCtx := &SimpleTxContext{
		OriginVal:   sender,
		GasPriceVal: gasPrice,
	}

	code := acc.Code(to)

	cfg := &ExecutionConfig{
		Fork:             ForkCancun,
		GasLimit:         gasLimit,
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

	fmt.Printf("Result: status=%d gasRemaining=%d err=%v\n", res.Status, res.GasRemaining, err)
	fmt.Printf("Storage 0x1000[0x00] = 0x%x\n", sto.Get(hexToAddr("0x0000000000000000000000000000000000001000"), hexToHash("0x00")))
	fmt.Printf("Sender balance = 0x%s\n", acc.Balance(sender).Text(16))
}
