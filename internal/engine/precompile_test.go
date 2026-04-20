package engine

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestMainnetPrecompilesByFork(t *testing.T) {
	if _, ok := MainnetPrecompilesForFork(ForkLondon).Resolve(precompileAddress(0x0a)); ok {
		t.Fatalf("london should not expose point evaluation precompile")
	}
	if _, ok := MainnetPrecompilesForFork(ForkCancun).Resolve(precompileAddress(0x0a)); !ok {
		t.Fatalf("cancun should expose point evaluation precompile")
	}
	if _, ok := MainnetPrecompilesForFork(ForkPrague).Resolve(precompileAddressU16(0x0100)); ok {
		t.Fatalf("prague should not expose p256 precompile")
	}
	if _, ok := MainnetPrecompilesForFork(ForkOsaka).Resolve(precompileAddressU16(0x0100)); !ok {
		t.Fatalf("osaka should expose p256 precompile")
	}
}

func TestWarmAddressesFollowCustomRegistry(t *testing.T) {
	custom := NewPrecompileRegistry()
	customAddr := precompileAddressU16(0x0200)
	custom.Register(customAddr, fixedGasPrecompile{gas: 1, run: func(input []byte, fork Fork) ([]byte, error) { return input, nil }})
	customWarm := precompileAddressU16(0x0201)
	cfg := &ExecutionConfig{Fork: ForkOsaka, Precompiles: custom, WarmAddresses: []Address{customWarm}}
	addrs := warmAddressesForConfig(cfg)
	if len(addrs) != 2 {
		t.Fatalf("expected two warmed addresses, got %d", len(addrs))
	}
	var seenCustom, seenWarm bool
	for _, addr := range addrs {
		if addr == customAddr {
			seenCustom = true
		}
		if addr == customWarm {
			seenWarm = true
		}
	}
	if !seenCustom || !seenWarm {
		t.Fatalf("warm address collection omitted custom registry or explicit warm addresses")
	}
}

func TestEmbeddedKZGContextLoads(t *testing.T) {
	ctx, err := getKZGContext()
	if err != nil {
		t.Fatalf("embedded kzg context failed to load: %v", err)
	}
	if ctx == nil {
		t.Fatalf("embedded kzg context is nil")
	}
}

func TestRunECRecoverMatchesMainnetVector(t *testing.T) {
	inputHex := "ee8f543390b5647e7774898d06db49a45323d918bb96a24df6943ae3cb6a0807" +
		"000000000000000000000000000000000000000000000000000000000000001c" +
		"977cd7acc7e4837f5afe54f395b276fad8c4144a738d4d817c0a88d9d4b91144" +
		"5dcde20164af184639b85fb80d9f8cc6e65fb80640328fc69236982b7dc07f25"
	input, err := hex.DecodeString(inputHex)
	if err != nil {
		t.Fatalf("hex.DecodeString() error = %v", err)
	}
	want, err := hex.DecodeString("00000000000000000000000057ed531a9da5b77c504cfd9b1c76b49e8dd76d05")
	if err != nil {
		t.Fatalf("hex.DecodeString() want error = %v", err)
	}
	got, err := runECRecover(input, ForkCancun)
	if err != nil {
		t.Fatalf("runECRecover() error = %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("runECRecover() = %x, want %x", got, want)
	}
}