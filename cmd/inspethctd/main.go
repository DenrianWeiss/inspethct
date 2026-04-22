package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"os"
	"strings"

	"inspethct/internal/clihelp"
	"inspethct/internal/contractmeta"
	"inspethct/internal/engine"
	"inspethct/internal/forkengine"
	"inspethct/internal/forkengine/upstream"
	jsonrpcserver "inspethct/internal/jsonrpc"
	"inspethct/internal/openchain"
	"inspethct/internal/srcmap"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-h", "--help", "help":
			if len(os.Args) > 2 {
				switch os.Args[2] {
				case "serve":
					if err := runServe([]string{"-h"}); err != nil {
						log.Fatal(err)
					}
					return
				case "source":
					if err := runSource([]string{}); err != nil {
						fmt.Fprintln(os.Stdout, "Usage: inspethctd source <local|standard-json|manual|explorer|inspect> [flags]")
						return
					}
					return
				case "openchain":
					if err := runOpenChain([]string{}); err != nil {
						fmt.Fprintln(os.Stdout, "Usage: inspethctd openchain <lookup|decode> [flags]")
						return
					}
					return
				case "oneshot", "one-shot":
					if err := runOneShot([]string{"-h"}); err != nil {
						log.Fatal(err)
					}
					return
				case "dbgserver", "dbg-server":
					if err := runDBGServer([]string{"-h"}); err != nil {
						log.Fatal(err)
					}
					return
				}
			}
			printMainUsage(os.Stdout)
			return
		}
	}
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		switch os.Args[1] {
		case "serve":
			if err := runServe(os.Args[2:]); err != nil {
				log.Fatal(err)
			}
			return
		case "source":
			if err := runSource(os.Args[2:]); err != nil {
				log.Fatal(err)
			}
			return
		case "openchain":
			if err := runOpenChain(os.Args[2:]); err != nil {
				log.Fatal(err)
			}
			return
		case "oneshot", "one-shot":
			if err := runOneShot(os.Args[2:]); err != nil {
				log.Fatal(err)
			}
			return
		case "dbgserver", "dbg-server":
			if err := runDBGServer(os.Args[2:]); err != nil {
				log.Fatal(err)
			}
			return
		}
	}
	if err := runServe(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func printMainUsage(writer io.Writer) {
	clihelp.Render(writer, clihelp.Doc{
		Usage:   "inspethctd <command> [flags]",
		Summary: []string{"CLI entrypoint for local EVM replay, interactive debugging, source inspection, and OpenChain helpers."},
		Sections: []clihelp.Section{
			{
				Title: "Commands",
				Entries: []clihelp.Entry{
					{Label: "serve", Summary: "Start the downstream JSON-RPC server."},
					{Label: "dbgserver", Summary: "Start the replay-oriented gdb-like JSON-RPC server."},
					{Label: "oneshot", Summary: "Start the interactive one-shot debugger."},
					{Label: "source", Summary: "Inspect or load source metadata."},
					{Label: "openchain", Summary: "Lookup or decode selectors via OpenChain."},
				},
			},
			{
				Title: "Help",
				Entries: []clihelp.Entry{
					{Label: "inspethctd help", Summary: "Show top-level help."},
					{Label: "inspethctd help oneshot", Summary: "Show oneshot command help."},
					{Label: "inspethctd help dbgserver", Summary: "Show dbgserver command help."},
				},
			},
		},
	})
}

func runServe(args []string) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(os.Stdout)
	flags.Usage = func() {
		clihelp.Render(flags.Output(), clihelp.Doc{Usage: "inspethctd serve [flags]", Summary: []string{"Start the downstream JSON-RPC server with Ethereum-compatible and debug RPC methods."}})
		clihelp.RenderFlagSet(flags.Output(), "Flags", flags)
	}
	listenAddr := flags.String("listen", ":8547", "listen address for downstream JSON-RPC")
	upstreamURL := flags.String("upstream", "", "upstream Ethereum JSON-RPC endpoint")
	blockValue := flags.String("block", "latest", "fork block number or tag")
	forkValue := flags.String("fork", string(engine.ForkCancun), "execution fork: london|paris|shanghai|cancun|prague|amsterdam|osaka")
	chainIDValue := flags.String("chain-id", "", "optional chain ID override (decimal or 0x-prefixed hex)")
	modeValue := flags.String("mode", string(forkengine.ModeDiff), "fork mode: diff|pinned")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if strings.TrimSpace(*upstreamURL) == "" {
		return fmt.Errorf("-upstream is required")
	}
	provider, err := upstream.NewJSONRPCProvider(upstream.JSONRPCConfig{Endpoint: *upstreamURL})
	if err != nil {
		return fmt.Errorf("create upstream provider: %w", err)
	}
	blockRef, err := parseBlockRef(*blockValue)
	if err != nil {
		return fmt.Errorf("parse block ref: %w", err)
	}
	chainIDOverride, err := parseOptionalBigInt(*chainIDValue)
	if err != nil {
		return fmt.Errorf("parse chain id: %w", err)
	}
	engineRef, err := forkengine.New(forkengine.Config{Provider: provider, Fork: engine.Fork(strings.ToLower(*forkValue)), ChainIDOverride: chainIDOverride, Mode: forkengine.Mode(strings.ToLower(*modeValue)), Block: blockRef})
	if err != nil {
		return fmt.Errorf("create forkengine: %w", err)
	}
	server := jsonrpcserver.NewServer(engineRef)
	log.Printf("listening on %s with upstream %s at block %s", *listenAddr, *upstreamURL, blockRef.CacheKey())
	return http.ListenAndServe(*listenAddr, server)
}

func runDBGServer(args []string) error {
	flags := flag.NewFlagSet("dbgserver", flag.ContinueOnError)
	flags.SetOutput(os.Stdout)
	flags.Usage = func() {
		clihelp.Render(flags.Output(), clihelp.Doc{
			Usage: "inspethctd dbgserver [flags]",
			Summary: []string{
				"Start a replay-oriented debug server exposing gdb-like JSON-RPC methods.",
				"This server reuses the stepping and source-bundle session model behind local debugging, but it currently exports replay sessions only.",
			},
			Sections: []clihelp.Section{
				{
					Title: "RPC Methods",
					Entries: []clihelp.Entry{
						{Label: "dbgserver.capabilities", Summary: "Describe available methods and current limitations."},
						{Label: "gdb.startReplaySession", Summary: "Create a replay debugging session for a transaction hash."},
						{Label: "gdb.next", Summary: "Advance one step in the session."},
						{Label: "gdb.continue", Summary: "Run until a breakpoint or completion."},
						{Label: "gdb.state", Summary: "Fetch the current session state."},
						{Label: "gdb.loadSourceBundle", Summary: "Attach local, standard-json, manual, or explorer metadata to a session."},
						{Label: "gdb.setSourceBreakpoint", Summary: "Install source-map breakpoints for a loaded bundle."},
						{Label: "gdb.writeStorage", Summary: "Apply a storage mutation before continuing."},
						{Label: "gdb.writeMemory", Summary: "Apply a memory mutation before continuing."},
					},
				},
				{
					Title: "Current Limitations",
					Entries: []clihelp.Entry{
						{Label: "replay sessions only", Summary: "Call-mode sessions from oneshot are not yet exported over RPC."},
						{Label: "source breakpoints only", Summary: "Function, call, storage-access, and memory-access breakpoints remain REPL-only for now."},
					},
				},
			},
		})
		clihelp.RenderFlagSet(flags.Output(), "Flags", flags)
	}
	listenAddr := flags.String("listen", ":8548", "listen address for dbgserver JSON-RPC")
	upstreamURL := flags.String("upstream", "", "upstream Ethereum JSON-RPC endpoint")
	blockValue := flags.String("block", "latest", "fork block number or tag")
	forkValue := flags.String("fork", string(engine.ForkCancun), "execution fork: london|paris|shanghai|cancun|prague|amsterdam|osaka")
	chainIDValue := flags.String("chain-id", "", "optional chain ID override (decimal or 0x-prefixed hex)")
	modeValue := flags.String("mode", string(forkengine.ModeDiff), "fork mode: diff|pinned")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if strings.TrimSpace(*upstreamURL) == "" {
		return fmt.Errorf("-upstream is required")
	}
	provider, err := upstream.NewJSONRPCProvider(upstream.JSONRPCConfig{Endpoint: *upstreamURL})
	if err != nil {
		return fmt.Errorf("create upstream provider: %w", err)
	}
	blockRef, err := parseBlockRef(*blockValue)
	if err != nil {
		return fmt.Errorf("parse block ref: %w", err)
	}
	chainIDOverride, err := parseOptionalBigInt(*chainIDValue)
	if err != nil {
		return fmt.Errorf("parse chain id: %w", err)
	}
	engineRef, err := forkengine.New(forkengine.Config{Provider: provider, Fork: engine.Fork(strings.ToLower(*forkValue)), ChainIDOverride: chainIDOverride, Mode: forkengine.Mode(strings.ToLower(*modeValue)), Block: blockRef})
	if err != nil {
		return fmt.Errorf("create forkengine: %w", err)
	}
	server := jsonrpcserver.NewGDBServer(engineRef)
	log.Printf("dbgserver listening on %s with upstream %s at block %s", *listenAddr, *upstreamURL, blockRef.CacheKey())
	return http.ListenAndServe(*listenAddr, server)
}

func runSource(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("source subcommand is required: local|standard-json|manual|explorer|inspect")
	}
	switch args[0] {
	case "local":
		flags := flag.NewFlagSet("source local", flag.ContinueOnError)
		project := flags.String("project", ".", "foundry/hardhat project root")
		contractName := flags.String("contract", "", "contract name")
		sourceName := flags.String("source", "", "source name")
		runtime := flags.Bool("runtime", true, "use deployed bytecode source map")
		address := flags.String("address", "", "logical contract address")
		codeAddress := flags.String("code-address", "", "code address for proxy/delegatecall debugging")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		bundle, err := contractmeta.LoadLocalProjectBundle(*project, newLoadOptions(*sourceName, *contractName, *runtime, *address, *codeAddress))
		if err != nil {
			return err
		}
		return printJSON(bundleSummary(bundle))
	case "standard-json":
		flags := flag.NewFlagSet("source standard-json", flag.ContinueOnError)
		file := flags.String("file", "", "standard-json output file path")
		contractName := flags.String("contract", "", "contract name")
		sourceName := flags.String("source", "", "source name")
		runtime := flags.Bool("runtime", true, "use deployed bytecode source map")
		address := flags.String("address", "", "logical contract address")
		codeAddress := flags.String("code-address", "", "code address")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		bundle, err := contractmeta.LoadStandardJSONFile(*file, newLoadOptions(*sourceName, *contractName, *runtime, *address, *codeAddress))
		if err != nil {
			return err
		}
		return printJSON(bundleSummary(bundle))
	case "manual":
		flags := flag.NewFlagSet("source manual", flag.ContinueOnError)
		standardJSONPath := flags.String("standard-json", "", "standard-json output file path")
		abiPath := flags.String("abi", "", "ABI file path")
		contractName := flags.String("contract", "", "contract name")
		sourceName := flags.String("source", "", "source name")
		runtime := flags.Bool("runtime", true, "use deployed bytecode source map")
		address := flags.String("address", "", "logical contract address")
		codeAddress := flags.String("code-address", "", "code address")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		bundle, err := contractmeta.LoadManualBundle(*standardJSONPath, *abiPath, newLoadOptions(*sourceName, *contractName, *runtime, *address, *codeAddress))
		if err != nil {
			return err
		}
		return printJSON(bundleSummary(bundle))
	case "explorer":
		flags := flag.NewFlagSet("source explorer", flag.ContinueOnError)
		apiBase := flags.String("api-base", "", "block explorer API base URL")
		apiKey := flags.String("api-key", "", "block explorer API key")
		chainID := flags.String("chain-id", "", "optional explorer chain id for Etherscan v2 style APIs")
		rpcURL := flags.String("rpc-url", "", "optional Ethereum RPC URL for proxy resolution")
		address := flags.String("address", "", "contract address")
		contractName := flags.String("contract", "", "contract name override")
		sourceName := flags.String("source", "", "source name override")
		runtime := flags.Bool("runtime", true, "use deployed bytecode source map")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		parsedAddress, err := parseCLIAddress(*address)
		if err != nil {
			return err
		}
		manager, err := contractmeta.NewSolcManager()
		if err != nil {
			return err
		}
		bundle, err := contractmeta.LoadExplorerBundle(context.Background(), contractmeta.ExplorerClient{APIBase: *apiBase, APIKey: *apiKey, RPCURL: *rpcURL, ChainID: *chainID}, manager, contractmeta.ExplorerLoadOptions{Address: parsedAddress, LoadOptions: newLoadOptions(*sourceName, *contractName, *runtime, *address, ""), RPCURL: *rpcURL})
		if err != nil {
			return err
		}
		return printJSON(bundleSummary(bundle))
	case "inspect":
		flags := flag.NewFlagSet("source inspect", flag.ContinueOnError)
		file := flags.String("file", "", "standard-json output file path")
		contractName := flags.String("contract", "", "contract name")
		sourceName := flags.String("source", "", "source name")
		runtime := flags.Bool("runtime", true, "use deployed bytecode source map")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		metadata, err := contractmeta.InspectStandardJSON(*file, srcmap.BuildConfig{SourceName: *sourceName, ContractName: *contractName, Runtime: *runtime})
		if err != nil {
			return err
		}
		return printJSON(metadata)
	default:
		return fmt.Errorf("unsupported source subcommand %q", args[0])
	}
}

func runOpenChain(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("openchain subcommand is required: lookup|decode")
	}
	client := openchain.Client{}
	switch args[0] {
	case "lookup":
		flags := flag.NewFlagSet("openchain lookup", flag.ContinueOnError)
		selector := flags.String("selector", "", "4-byte function selector")
		topic := flags.String("topic", "", "32-byte event topic")
		baseURL := flags.String("base-url", "", "openchain API base URL")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		client.BaseURL = *baseURL
		if *selector != "" {
			signatures, err := client.LookupFunction(context.Background(), *selector)
			if err != nil {
				return err
			}
			return printJSON(map[string]any{"kind": "function", "selector": *selector, "signatures": signatures})
		}
		if *topic != "" {
			signatures, err := client.LookupEvent(context.Background(), *topic)
			if err != nil {
				return err
			}
			return printJSON(map[string]any{"kind": "event", "topic": *topic, "signatures": signatures})
		}
		return fmt.Errorf("lookup requires --selector or --topic")
	case "decode":
		flags := flag.NewFlagSet("openchain decode", flag.ContinueOnError)
		calldata := flags.String("calldata", "", "hex calldata to decode")
		baseURL := flags.String("base-url", "", "openchain API base URL")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		client.BaseURL = *baseURL
		decoded, err := client.DecodeCalldata(context.Background(), *calldata)
		if err != nil {
			return err
		}
		return printJSON(decoded)
	default:
		return fmt.Errorf("unsupported openchain subcommand %q", args[0])
	}
}

func newLoadOptions(sourceName string, contractName string, runtime bool, address string, codeAddress string) contractmeta.LoadOptions {
	options := contractmeta.LoadOptions{SourceName: sourceName, ContractName: contractName, Runtime: runtime}
	if parsed, err := parseCLIAddress(address); err == nil {
		options.Address = &parsed
	}
	if parsed, err := parseCLIAddress(codeAddress); err == nil {
		options.CodeAddress = &parsed
	}
	return options
}

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

func bundleSummary(bundle *contractmeta.Bundle) map[string]any {
	result := map[string]any{
		"sourceName":      bundle.SourceName,
		"contractName":    bundle.ContractName,
		"compilerVersion": bundle.CompilerVersion,
		"metadata":        bundle.Metadata,
		"proxy":           bundle.Proxy,
	}
	if bundle.Address != nil {
		result["address"] = "0x" + hex.EncodeToString(bundle.Address[:])
	}
	if bundle.CodeAddress != nil {
		result["codeAddress"] = "0x" + hex.EncodeToString(bundle.CodeAddress[:])
	}
	return result
}

func printJSON(value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(os.Stdout, string(encoded))
	return err
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
