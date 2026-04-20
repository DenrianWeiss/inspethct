package main

import (
	"flag"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"strings"

	"inspethct/internal/engine"
	"inspethct/internal/forkengine"
	"inspethct/internal/forkengine/upstream"
	jsonrpcserver "inspethct/internal/jsonrpc"
)

func main() {
	listenAddr := flag.String("listen", ":8547", "listen address for downstream JSON-RPC")
	upstreamURL := flag.String("upstream", "", "upstream Ethereum JSON-RPC endpoint")
	blockValue := flag.String("block", "latest", "fork block number or tag")
	forkValue := flag.String("fork", string(engine.ForkCancun), "execution fork: london|paris|shanghai|cancun|prague|amsterdam|osaka")
	chainIDValue := flag.String("chain-id", "", "optional chain ID override (decimal or 0x-prefixed hex)")
	modeValue := flag.String("mode", string(forkengine.ModeDiff), "fork mode: diff|pinned")
	flag.Parse()

	if strings.TrimSpace(*upstreamURL) == "" {
		log.Fatal("-upstream is required")
	}
	provider, err := upstream.NewJSONRPCProvider(upstream.JSONRPCConfig{Endpoint: *upstreamURL})
	if err != nil {
		log.Fatalf("create upstream provider: %v", err)
	}
	blockRef, err := parseBlockRef(*blockValue)
	if err != nil {
		log.Fatalf("parse block ref: %v", err)
	}
	chainIDOverride, err := parseOptionalBigInt(*chainIDValue)
	if err != nil {
		log.Fatalf("parse chain id: %v", err)
	}
	engineRef, err := forkengine.New(forkengine.Config{Provider: provider, Fork: engine.Fork(strings.ToLower(*forkValue)), ChainIDOverride: chainIDOverride, Mode: forkengine.Mode(strings.ToLower(*modeValue)), Block: blockRef})
	if err != nil {
		log.Fatalf("create forkengine: %v", err)
	}
	server := jsonrpcserver.NewServer(engineRef)
	log.Printf("listening on %s with upstream %s at block %s", *listenAddr, *upstreamURL, blockRef.CacheKey())
	log.Fatal(http.ListenAndServe(*listenAddr, server))
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
