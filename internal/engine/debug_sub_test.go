package engine

import (
	"fmt"
	"math/big"
	"testing"
)

func TestDebugSubOrder(t *testing.T) {
	tests := []struct {
		name     string
		code     []byte
		expected string
	}{
		{
			name:     "PUSH0 PUSH6 SUB SSTORE",
			code:     []byte{0x60, 0x00, 0x60, 0x06, 0x03, 0x60, 0x00, 0x55, 0x00},
			expected: "0000000000000000000000000000000000000000000000000000000000000006",
		},
		{
			name:     "PUSH6 PUSH0 SUB SSTORE",
			code:     []byte{0x60, 0x06, 0x60, 0x00, 0x03, 0x60, 0x00, 0x55, 0x00},
			expected: "fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffa",
		},
		{
			name:     "PUSH2 PUSH2 PUSH1 ADDMOD SSTORE",
			code:     []byte{0x60, 0x02, 0x60, 0x02, 0x60, 0x01, 0x08, 0x60, 0x00, 0x55, 0x00},
			expected: "0000000000000000000000000000000000000000000000000000000000000001",
		},
		{
			name:     "PUSH1 PUSH2 PUSH2 ADDMOD SSTORE",
			code:     []byte{0x60, 0x01, 0x60, 0x02, 0x60, 0x02, 0x08, 0x60, 0x00, 0x55, 0x00},
			expected: "0000000000000000000000000000000000000000000000000000000000000000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			acc := NewInMemoryAccountState()
			sto := NewInMemoryStorage()
			addr := Address{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x10, 0x00}
			acc.SetBalance(addr, big.NewInt(1e18))
			acc.SetNonce(addr, 0)
			acc.SetCode(addr, tt.code)

			cfg := &ExecutionConfig{
				Fork:            ForkCancun,
				GasLimit:        1000000,
				Value:           big.NewInt(0),
				Input:           nil,
				Origin:          addr,
				Caller:          addr,
				ContractAddress: addr,
				Code:            tt.code,
				CodeHash:        hashCode(tt.code),
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
					OriginVal:   addr,
					GasPriceVal: big.NewInt(0),
				},
				State:            acc,
				Storage:          sto,
				TransientStorage: NewInMemoryTransientStorage(),
				AccessList:       NewSimpleAccessList(),
			}

			eng := NewSimpleEngine()
			_, err := eng.Run(cfg)
			if err != nil {
				t.Fatalf("execution error: %v", err)
			}

			result := sto.Get(addr, Hash{})
			resultHex := fmt.Sprintf("%064x", result)
			fmt.Printf("Test %s: result=0x%s expected=0x%s\n", tt.name, resultHex, tt.expected)
			if resultHex != tt.expected {
				t.Errorf("expected 0x%s, got 0x%s", tt.expected, resultHex)
			}
		})
	}
}
