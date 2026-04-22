package oneshot

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"inspethct/internal/contractmeta"
	"inspethct/internal/engine"
	"inspethct/internal/forkengine"
	"inspethct/internal/forkengine/upstream"
)

func Run(args []string) error {
	flags := flag.NewFlagSet("oneshot", flag.ContinueOnError)
	flags.SetOutput(os.Stdout)
	flags.Usage = func() {
		printOneShotCLIHelp(flags.Output(), flags)
	}
	projectPath := flags.String("project", ".", "project root or a directory inside the target foundry/hardhat project")
	upstreamURL := flags.String("upstream", "", "upstream Ethereum JSON-RPC endpoint")
	explorerAPIBase := flags.String("explorer-api-base", defaultExplorerAPIBase, "block explorer API base URL")
	explorerAPIKey := flags.String("explorer-api-key", "", "block explorer API key")
	explorerChainID := flags.String("explorer-chain-id", "1", "explorer chain id for Etherscan v2 style APIs")
	blockValue := flags.String("block", "latest", "fork block number or tag")
	forkValue := flags.String("fork", string(engine.ForkCancun), "execution fork: london|paris|shanghai|cancun|prague|amsterdam|osaka")
	chainIDValue := flags.String("chain-id", "", "optional chain ID override (decimal or 0x-prefixed hex)")
	modeValue := flags.String("mode", string(forkengine.ModeDiff), "fork mode: diff|pinned")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	detection, err := detectProject(*projectPath)
	if err != nil {
		return err
	}
	blockRef, err := parseBlockRef(*blockValue)
	if err != nil {
		return fmt.Errorf("parse block ref: %w", err)
	}
	chainIDOverride, err := parseOptionalBigInt(*chainIDValue)
	if err != nil {
		return fmt.Errorf("parse chain id: %w", err)
	}

	reader := bufio.NewReader(os.Stdin)
	writer := os.Stdout
	_, _ = fmt.Fprintf(writer, "Detected project: %s (%s)\n", detection.Root, detection.Kind)

	session := &oneshotSession{
		config: oneshotConfig{
			ProjectRoot:         detection.Root,
			UpstreamURL:         strings.TrimSpace(*upstreamURL),
			ExplorerAPIBase:     strings.TrimSpace(*explorerAPIBase),
			ExplorerAPIKey:      strings.TrimSpace(*explorerAPIKey),
			ExplorerChainID:     strings.TrimSpace(*explorerChainID),
			BlockRef:            blockRef,
			Fork:                engine.Fork(strings.ToLower(*forkValue)),
			Mode:                forkengine.Mode(strings.ToLower(*modeValue)),
			ChainIDOverride:     chainIDOverride,
			ChainIDOverrideText: strings.TrimSpace(*chainIDValue),
		},
		position: -1,
		lastStep: -1,
	}
	if err := configureOneShotSession(reader, writer, session, detection); err != nil {
		return err
	}
	return runOneShotREPL(context.Background(), reader, writer, session)
}

func configureOneShotSession(reader *bufio.Reader, writer io.Writer, session *oneshotSession, detection projectDetection) error {
	txInput, err := promptLine(reader, writer, "Transaction hash (leave blank to simulate a call): ")
	if err != nil {
		return err
	}
	if strings.TrimSpace(txInput) != "" {
		txHash, err := parseCLIHash(txInput)
		if err != nil {
			return err
		}
		session.kind = "replay"
		session.txHash = txHash
	} else {
		callReq, err := promptCallRequest(reader, writer, session.config.BlockRef)
		if err != nil {
			return err
		}
		session.kind = "call"
		session.callReq = callReq
		addr := callReq.To
		session.targetAddr = &addr
		session.codeAddr = &addr
	}
	if detection.Kind != projectUnknown {
		bundle, err := promptSourceBundle(reader, writer, session)
		if err != nil {
			return err
		}
		session.bundle = bundle
		if bundle != nil {
			_, _ = fmt.Fprintf(writer, "Loaded source bundle: %s (%s)\n", bundle.ContractName, bundle.SourceName)
		}
	}
	if session.bundle == nil {
		_, _ = fmt.Fprintln(writer, noSourceBundleMessage())
	}
	printOneShotHelp(writer)
	return nil
}

func runOneShotREPL(ctx context.Context, reader *bufio.Reader, writer io.Writer, session *oneshotSession) error {
	for {
		line, err := promptLine(reader, writer, "oneshot> ")
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		commandLine := strings.TrimSpace(line)
		if commandLine == "" {
			continue
		}
		fields := strings.Fields(commandLine)
		switch strings.ToLower(fields[0]) {
		case "help", "h", "?":
			printOneShotHelp(writer, fields[1:]...)
		case "run", "r":
			if maybePrintCommandHelp(writer, fields) {
				continue
			}
			session.resetExecutionProgress()
			if err := session.run(ctx, oneshoModeContinue); err != nil {
				_, _ = fmt.Fprintf(writer, "run failed: %v\n", err)
				continue
			}
			printSessionStatus(writer, session)
		case "continue", "c":
			if maybePrintCommandHelp(writer, fields) {
				continue
			}
			if err := session.run(ctx, oneshoModeContinue); err != nil {
				_, _ = fmt.Fprintf(writer, "continue failed: %v\n", err)
				continue
			}
			printSessionStatus(writer, session)
		case "next", "n", "step", "s":
			if maybePrintCommandHelp(writer, fields) {
				continue
			}
			if err := session.run(ctx, oneshoModeNext); err != nil {
				_, _ = fmt.Fprintf(writer, "step failed: %v\n", err)
				continue
			}
			printSessionStatus(writer, session)
		case "break", "b":
			if err := handleBreakCommand(reader, writer, session, fields); err != nil {
				_, _ = fmt.Fprintf(writer, "break failed: %v\n", err)
			}
		case "info", "i":
			if err := handleInfoCommand(writer, session, fields); err != nil {
				_, _ = fmt.Fprintf(writer, "info failed: %v\n", err)
			}
		case "print", "p":
			if err := handlePrintCommand(writer, session, fields); err != nil {
				_, _ = fmt.Fprintf(writer, "print failed: %v\n", err)
			}
		case "x":
			if maybePrintCommandHelp(writer, fields) {
				continue
			}
			if err := handleMemoryExamineCommand(writer, session, fields); err != nil {
				_, _ = fmt.Fprintf(writer, "x failed: %v\n", err)
			}
		case "set":
			if err := handleSetCommand(reader, writer, session, fields); err != nil {
				_, _ = fmt.Fprintf(writer, "set failed: %v\n", err)
			}
		case "load":
			if err := handleLoadCommand(ctx, reader, writer, session, fields); err != nil {
				_, _ = fmt.Fprintf(writer, "load failed: %v\n", err)
			}
		case "encode":
			if err := handleEncodeCommand(reader, writer, session, fields); err != nil {
				_, _ = fmt.Fprintf(writer, "encode failed: %v\n", err)
			}
		case "state":
			if maybePrintCommandHelp(writer, fields) {
				continue
			}
			printSessionStatus(writer, session)
		case "restart":
			if maybePrintCommandHelp(writer, fields) {
				continue
			}
			session.reset()
			_, _ = fmt.Fprintln(writer, "Session reset.")
		case "quit", "q", "exit":
			if maybePrintCommandHelp(writer, fields) {
				continue
			}
			return nil
		default:
			_, _ = fmt.Fprintf(writer, "Unknown command %q. Use 'help' to list commands.\n", fields[0])
		}
	}
}

func handleBreakCommand(reader *bufio.Reader, writer io.Writer, session *oneshotSession, fields []string) error {
	if maybePrintCommandHelp(writer, fields) {
		return nil
	}
	if len(fields) == 1 {
		spec, err := promptLine(reader, writer, "Breakpoint: ")
		if err != nil {
			return err
		}
		return session.addBreakpointSpec(spec)
	}
	switch strings.ToLower(fields[1]) {
	case "line":
		if len(fields) < 3 {
			return oneshotUsageError("break", "line")
		}
		return session.addSourceBreakpoint(strings.Join(fields[2:], " "))
	case "storage":
		return session.addStorageBreakpoint(fields[2:])
	case "memory":
		return session.addMemoryBreakpoint(fields[2:])
	case "call":
		return session.addCallBreakpoint(fields[2:])
	case "func", "function":
		if len(fields) < 3 {
			return oneshotUsageError("break", "function")
		}
		return session.addFunctionBreakpoint(strings.Join(fields[2:], " "))
	default:
		return session.addBreakpointSpec(strings.Join(fields[1:], " "))
	}
}

func handleInfoCommand(writer io.Writer, session *oneshotSession, fields []string) error {
	if maybePrintCommandHelp(writer, fields) {
		return nil
	}
	if len(fields) < 2 {
		return oneshotUsageError("info")
	}
	switch strings.ToLower(fields[1]) {
	case "breakpoints", "b":
		for _, breakpoint := range session.breakpoints {
			_, _ = fmt.Fprintf(writer, "%d\t%s\n", breakpoint.ID, breakpoint.Display)
		}
		if len(session.breakpoints) == 0 {
			_, _ = fmt.Fprintln(writer, "No breakpoints.")
		}
	case "config":
		printSessionConfig(writer, session)
	case "bundle":
		if session.bundle == nil {
			_, _ = fmt.Fprintln(writer, noSourceBundleMessage())
			return nil
		}
		_, _ = fmt.Fprintf(writer, "contract=%s source=%s compiler=%s\n", session.bundle.ContractName, session.bundle.SourceName, session.bundle.CompilerVersion)
	case "storage":
		return printStorageVariables(writer, session)
	case "memory":
		limit := 16
		if len(fields) > 2 {
			parsed, err := parseUint64Flexible(fields[2])
			if err != nil {
				return err
			}
			limit = int(parsed)
		}
		return printMemoryWords(writer, session, limit)
	default:
		return fmt.Errorf("unsupported info subject %q\nTry: help info", fields[1])
	}
	return nil
}

func handlePrintCommand(writer io.Writer, session *oneshotSession, fields []string) error {
	if maybePrintCommandHelp(writer, fields) {
		return nil
	}
	if len(fields) < 3 {
		return oneshotUsageError("print")
	}
	switch strings.ToLower(fields[1]) {
	case "storage":
		return printStorageValue(writer, session, fields[2])
	case "memory":
		if len(fields) < 4 {
			return oneshotUsageError("print", "memory")
		}
		offset, err := parseUint64Flexible(fields[2])
		if err != nil {
			return err
		}
		size, err := parseUint64Flexible(fields[3])
		if err != nil {
			return err
		}
		return printMemoryRange(writer, session, offset, size)
	default:
		return fmt.Errorf("unsupported print subject %q\nTry: help print", fields[1])
	}
}

func handleMemoryExamineCommand(writer io.Writer, session *oneshotSession, fields []string) error {
	if len(fields) < 3 {
		return oneshotUsageError("x")
	}
	offset, err := parseUint64Flexible(fields[1])
	if err != nil {
		return err
	}
	size, err := parseUint64Flexible(fields[2])
	if err != nil {
		return err
	}
	return printMemoryRange(writer, session, offset, size)
}

func handleSetCommand(reader *bufio.Reader, writer io.Writer, session *oneshotSession, fields []string) error {
	if maybePrintCommandHelp(writer, fields) {
		return nil
	}
	if len(fields) < 2 {
		return oneshotUsageError("set")
	}
	switch strings.ToLower(fields[1]) {
	case "config":
		if len(fields) < 4 {
			return oneshotUsageError("set", "config")
		}
		return session.applyConfigUpdate(strings.ToLower(fields[2]), strings.Join(fields[3:], " "), false)
	case "memory":
		return handleSetMemoryCommand(writer, session, fields[2:])
	case "call":
		if session.kind != "call" {
			return fmt.Errorf("set call is only available in call mode")
		}
		return handleEncodeCommand(reader, writer, session, append([]string{"encode"}, fields[2:]...))
	default:
		return fmt.Errorf("unsupported set subject %q\nTry: help set", fields[1])
	}
}

func handleLoadCommand(ctx context.Context, reader *bufio.Reader, writer io.Writer, session *oneshotSession, fields []string) error {
	if maybePrintCommandHelp(writer, fields) {
		return nil
	}
	if len(fields) < 2 {
		return oneshotUsageError("load")
	}
	switch strings.ToLower(fields[1]) {
	case "clear":
		session.bundle = nil
		_, _ = fmt.Fprintln(writer, "Cleared source bundle.")
		return nil
	case "local":
		contractName := ""
		sourceName := ""
		if len(fields) > 2 {
			contractName = fields[2]
		}
		if len(fields) > 3 {
			sourceName = fields[3]
		}
		if contractName == "" {
			value, err := promptLine(reader, writer, "Contract name: ")
			if err != nil {
				return err
			}
			contractName = value
		}
		if sourceName == "" {
			value, err := promptLine(reader, writer, "Source override (blank to auto-detect): ")
			if err != nil {
				return err
			}
			sourceName = value
		}
		bundle, err := contractmeta.LoadLocalProjectBundle(session.config.ProjectRoot, contractmeta.LoadOptions{ContractName: strings.TrimSpace(contractName), SourceName: strings.TrimSpace(sourceName), Runtime: true})
		if err != nil {
			return err
		}
		session.bundle = bundle
		_, _ = fmt.Fprintf(writer, "Loaded local bundle: %s (%s)\n", bundle.ContractName, bundle.SourceName)
		return nil
	case "explorer":
		addressText := ""
		contractName := ""
		sourceName := ""
		if len(fields) > 2 {
			addressText = fields[2]
		}
		if len(fields) > 3 {
			contractName = fields[3]
		}
		if len(fields) > 4 {
			sourceName = fields[4]
		}
		if addressText == "" {
			value, err := promptLine(reader, writer, "Address: ")
			if err != nil {
				return err
			}
			addressText = value
		}
		addr, err := parseCLIAddress(addressText)
		if err != nil {
			return err
		}
		bundle, err := session.loadExplorerBundle(ctx, addr, contractName, sourceName)
		if err != nil {
			return err
		}
		session.bundle = bundle
		_, _ = fmt.Fprintf(writer, "Loaded explorer bundle: %s (%s)\n", bundle.ContractName, bundle.SourceName)
		return nil
	default:
		return fmt.Errorf("unsupported load mode %q\nTry: help load", fields[1])
	}
}

func handleEncodeCommand(reader *bufio.Reader, writer io.Writer, session *oneshotSession, fields []string) error {
	if maybePrintCommandHelp(writer, fields) {
		return nil
	}
	if len(fields) < 2 {
		return oneshotUsageError("encode")
	}
	method, canonical, err := buildMethodFromSignature(fields[1])
	if err != nil {
		return err
	}
	args := append([]string(nil), fields[2:]...)
	if len(args) == 0 && len(method.Inputs) > 0 {
		args, err = promptMethodArguments(reader, writer, method)
		if err != nil {
			return err
		}
	}
	calldata, err := encodeMethodCall(method, args)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(writer, "%s => %s\n", canonical, encodeCLIBytes(calldata))
	if session.kind == "call" {
		session.callReq.Input = calldata
		session.resetExecutionProgress()
		_, _ = fmt.Fprintln(writer, "Updated current call input.")
	}
	return nil
}

func detectProject(start string) (projectDetection, error) {
	root, kind, err := findProjectRootAndKind(start)
	if err != nil {
		return projectDetection{}, err
	}
	return projectDetection{Root: root, Kind: kind}, nil
}

func findProjectRootAndKind(start string) (string, projectKind, error) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", projectUnknown, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", projectUnknown, err
	}
	if !info.IsDir() {
		abs = filepath.Dir(abs)
	}
	current := abs
	for {
		kind := detectProjectKind(current)
		if kind != projectUnknown {
			return current, kind, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return abs, projectUnknown, nil
		}
		current = parent
	}
}

func detectProjectKind(dir string) projectKind {
	hasFoundry := fileExists(filepath.Join(dir, "foundry.toml")) || dirExists(filepath.Join(dir, "out", "build-info"))
	hasHardhat := fileExists(filepath.Join(dir, "hardhat.config.ts")) || fileExists(filepath.Join(dir, "hardhat.config.js")) || fileExists(filepath.Join(dir, "hardhat.config.cjs")) || fileExists(filepath.Join(dir, "hardhat.config.mjs")) || dirExists(filepath.Join(dir, "artifacts", "build-info"))
	switch {
	case hasFoundry && hasHardhat:
		return projectMixed
	case hasFoundry:
		return projectFoundry
	case hasHardhat:
		return projectHardhat
	default:
		return projectUnknown
	}
}

func promptSourceBundle(reader *bufio.Reader, writer io.Writer, session *oneshotSession) (*contractmeta.Bundle, error) {
	for {
		contractName, err := promptLine(reader, writer, "Contract name for source bundle (blank to skip): ")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(contractName) == "" {
			return nil, nil
		}
		sourceName, err := promptLine(reader, writer, "Source override (blank to auto-detect): ")
		if err != nil {
			return nil, err
		}
		bundle, err := contractmeta.LoadLocalProjectBundle(session.config.ProjectRoot, contractmeta.LoadOptions{ContractName: strings.TrimSpace(contractName), SourceName: strings.TrimSpace(sourceName), Runtime: true})
		if err != nil {
			_, _ = fmt.Fprintf(writer, "Failed to load source bundle: %v\n", err)
			continue
		}
		return bundle, nil
	}
}

func promptCallRequest(reader *bufio.Reader, writer io.Writer, blockRef upstream.BlockRef) (forkengine.CallRequest, error) {
	fromText, err := promptLine(reader, writer, "From address [default 0x0000000000000000000000000000000000000001]: ")
	if err != nil {
		return forkengine.CallRequest{}, err
	}
	if strings.TrimSpace(fromText) == "" {
		fromText = "0x0000000000000000000000000000000000000001"
	}
	from, err := parseCLIAddress(fromText)
	if err != nil {
		return forkengine.CallRequest{}, err
	}
	toText, err := promptLine(reader, writer, "To address: ")
	if err != nil {
		return forkengine.CallRequest{}, err
	}
	to, err := parseCLIAddress(toText)
	if err != nil {
		return forkengine.CallRequest{}, err
	}
	calldataText, err := promptLine(reader, writer, "Calldata [default 0x]: ")
	if err != nil {
		return forkengine.CallRequest{}, err
	}
	if strings.TrimSpace(calldataText) == "" {
		calldataText = "0x"
	}
	input, err := decodeCLIBytes(calldataText)
	if err != nil {
		return forkengine.CallRequest{}, err
	}
	valueText, err := promptLine(reader, writer, "Value [default 0]: ")
	if err != nil {
		return forkengine.CallRequest{}, err
	}
	value, err := parseOptionalBigInt(valueText)
	if err != nil {
		return forkengine.CallRequest{}, err
	}
	gasText, err := promptLine(reader, writer, "Gas limit [default 0 = block gas limit]: ")
	if err != nil {
		return forkengine.CallRequest{}, err
	}
	gasLimit, err := parseOptionalUint64(gasText)
	if err != nil {
		return forkengine.CallRequest{}, err
	}
	return forkengine.CallRequest{From: from, To: to, Input: input, Value: value, GasLimit: gasLimit, Block: blockRef}, nil
}

func promptLine(reader *bufio.Reader, writer io.Writer, label string) (string, error) {
	if _, err := fmt.Fprint(writer, label); err != nil {
		return "", err
	}
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func (session *oneshotSession) applyConfigUpdate(key string, value string, unset bool) error {
	switch key {
	case "upstream":
		if unset {
			session.config.UpstreamURL = ""
		} else {
			session.config.UpstreamURL = strings.TrimSpace(value)
		}
		session.engineRef = nil
	case "explorer.api-base":
		if unset {
			session.config.ExplorerAPIBase = defaultExplorerAPIBase
		} else {
			session.config.ExplorerAPIBase = strings.TrimSpace(value)
		}
	case "explorer.api-key":
		if unset {
			session.config.ExplorerAPIKey = ""
		} else {
			session.config.ExplorerAPIKey = strings.TrimSpace(value)
		}
	case "explorer.chain-id":
		if unset {
			session.config.ExplorerChainID = "1"
		} else {
			session.config.ExplorerChainID = strings.TrimSpace(value)
		}
	case "engine.chain-id":
		if unset || strings.TrimSpace(value) == "" {
			session.config.ChainIDOverride = nil
			session.config.ChainIDOverrideText = ""
		} else {
			parsed, err := parseOptionalBigInt(strings.TrimSpace(value))
			if err != nil {
				return err
			}
			session.config.ChainIDOverride = parsed
			session.config.ChainIDOverrideText = strings.TrimSpace(value)
		}
		session.engineRef = nil
	default:
		return fmt.Errorf("unsupported config key %q\nSupported keys: upstream, explorer.api-base, explorer.api-key, explorer.chain-id, engine.chain-id\nTry: help set config", key)
	}
	session.resetExecutionProgress()
	return nil
}

func printSessionConfig(writer io.Writer, session *oneshotSession) {
	_, _ = fmt.Fprintf(writer, "project=%s\n", session.config.ProjectRoot)
	_, _ = fmt.Fprintf(writer, "upstream=%s\n", displayOrDefault(session.config.UpstreamURL, "<unset>"))
	_, _ = fmt.Fprintf(writer, "explorer.api-base=%s\n", displayOrDefault(session.currentExplorerAPIBase(), defaultExplorerAPIBase))
	_, _ = fmt.Fprintf(writer, "explorer.api-key=%s\n", maskSecret(session.config.ExplorerAPIKey))
	_, _ = fmt.Fprintf(writer, "explorer.chain-id=%s\n", displayOrDefault(session.config.ExplorerChainID, "1"))
	_, _ = fmt.Fprintf(writer, "engine.chain-id=%s\n", displayOrDefault(session.config.ChainIDOverrideText, "<auto>"))
	_, _ = fmt.Fprintf(writer, "block=%s fork=%s mode=%s\n", session.config.BlockRef.CacheKey(), session.config.Fork, session.config.Mode)
}

func handleSetMemoryCommand(writer io.Writer, session *oneshotSession, args []string) error {
	if session.current == nil {
		return fmt.Errorf("memory writes require a paused state")
	}
	if len(args) < 2 {
		return oneshotUsageError("set", "memory")
	}
	switch strings.ToLower(args[0]) {
	case "last":
		if session.current.MemoryAccess == nil {
			return fmt.Errorf("no memory access is associated with the current pause")
		}
		data, err := decodeCLIBytes(args[1])
		if err != nil {
			return err
		}
		return session.recordMemoryMutation(session.current.MemoryAccess.Offset, data, "last memory access", writer)
	case "line":
		if len(args) < 3 {
			return oneshotUsageError("set", "memory")
		}
		if session.current.MemoryAccess == nil {
			return fmt.Errorf("line-based memory write requires pausing on a memory access for that line")
		}
		if session.bundle == nil || session.bundle.Index == nil {
			return fmt.Errorf("line-based memory write requires a loaded source bundle")
		}
		sourceName, line, column, err := parseBreakpointSpec(args[1], session.bundle.SourceName)
		if err != nil {
			return err
		}
		pcs, err := resolveBreakpointPCs(session.bundle, sourceName, line, column)
		if err != nil {
			return err
		}
		matched := false
		for _, pc := range pcs {
			if pc == session.current.Step.PC {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("current pause is not at the requested source line; pause on a memory access at %s first", args[1])
		}
		data, err := decodeCLIBytes(args[2])
		if err != nil {
			return err
		}
		return session.recordMemoryMutation(session.current.MemoryAccess.Offset, data, fmt.Sprintf("line %s", args[1]), writer)
	default:
		offset, err := parseUint64Flexible(args[0])
		if err != nil {
			return err
		}
		data, err := decodeCLIBytes(args[1])
		if err != nil {
			return err
		}
		return session.recordMemoryMutation(offset, data, fmt.Sprintf("offset %#x", offset), writer)
	}
}

func (session *oneshotSession) recordMemoryMutation(offset uint64, data []byte, label string, writer io.Writer) error {
	mutation := oneshotMutation{Kind: "memory", StepIndex: session.position, Offset: offset, Data: append([]byte(nil), data...)}
	session.mutations = append(session.mutations, mutation)
	applyMemoryMutationToPause(session.current, mutation)
	_, _ = fmt.Fprintf(writer, "Updated memory at %s with %s\n", label, encodeCLIBytes(data))
	return nil
}
