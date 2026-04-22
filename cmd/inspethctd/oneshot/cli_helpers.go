package oneshot

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"inspethct/internal/engine"
	"inspethct/internal/forkengine/upstream"
)

func parseCLIAddress(value string) (engine.Address, error) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(value), "0x")
	if trimmed == "" {
		return engine.Address{}, fmt.Errorf("address is required")
	}
	decoded, err := hex.DecodeString(trimmed)
	if err != nil {
		return engine.Address{}, err
	}
	if len(decoded) != 20 {
		return engine.Address{}, fmt.Errorf("address must be 20 bytes")
	}
	var addr engine.Address
	copy(addr[:], decoded)
	return addr, nil
}

func parseBlockRef(input string) (upstream.BlockRef, error) {
	input = strings.TrimSpace(strings.ToLower(input))
	switch input {
	case "", "latest":
		return upstream.LatestBlock(), nil
	case "earliest", "pending", "safe", "finalized":
		return upstream.BlockRef{Tag: upstream.BlockTag(input)}, nil
	}
	var number uint64
	if _, err := fmt.Sscanf(strings.TrimPrefix(input, "0x"), "%x", &number); err == nil && strings.HasPrefix(input, "0x") {
		return upstream.BlockNumber(number), nil
	}
	if _, err := fmt.Sscanf(input, "%d", &number); err != nil {
		return upstream.BlockRef{}, fmt.Errorf("unsupported block ref %q", input)
	}
	return upstream.BlockNumber(number), nil
}

func parseOptionalBigInt(input string) (*big.Int, error) {
	trimmed := strings.TrimSpace(strings.ToLower(input))
	if trimmed == "" {
		return nil, nil
	}
	base := 10
	if strings.HasPrefix(trimmed, "0x") {
		trimmed = strings.TrimPrefix(trimmed, "0x")
		base = 16
	}
	value, ok := new(big.Int).SetString(trimmed, base)
	if !ok {
		return nil, fmt.Errorf("unsupported big integer %q", input)
	}
	return value, nil
}
