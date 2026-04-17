package engine

import "testing"

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