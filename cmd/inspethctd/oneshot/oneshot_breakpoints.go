package oneshot

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"inspethct/internal/contractmeta"
	"inspethct/internal/engine"
)

func (session *oneshotSession) addBreakpointSpec(spec string) error {
	trimmed := strings.TrimSpace(spec)
	if trimmed == "" {
		return fmt.Errorf("breakpoint spec is required")
	}
	if looksLikeFunctionSignature(trimmed) {
		return session.addFunctionBreakpoint(trimmed)
	}
	return session.addSourceBreakpoint(trimmed)
}

func (session *oneshotSession) addSourceBreakpoint(spec string) error {
	if session.bundle == nil || session.bundle.Index == nil {
		return fmt.Errorf("%s", sourceBundleRequiredMessage("source breakpoints"))
	}
	sourceName, line, column, err := parseBreakpointSpec(spec, session.bundle.SourceName)
	if err != nil {
		return err
	}
	pcs, err := resolveBreakpointPCs(session.bundle, sourceName, line, column)
	if err != nil {
		return err
	}
	session.breakpoints = append(session.breakpoints, oneshotBreakpoint{
		ID:         len(session.breakpoints) + 1,
		Kind:       breakpointKindSource,
		Display:    fmt.Sprintf("%s:%d:%d", sourceName, line, column),
		SourceName: sourceName,
		Line:       line,
		Column:     column,
		PCs:        pcs,
	})
	return nil
}

func (session *oneshotSession) addFunctionBreakpoint(signature string) error {
	method, canonical, err := buildMethodFromSignature(signature)
	if err != nil {
		return err
	}
	session.breakpoints = append(session.breakpoints, oneshotBreakpoint{
		ID:        len(session.breakpoints) + 1,
		Kind:      breakpointKindFunction,
		Display:   fmt.Sprintf("function %s", canonical),
		Signature: canonical,
		Selector:  append([]byte(nil), method.ID...),
	})
	return nil
}

func (session *oneshotSession) addCallBreakpoint(args []string) error {
	if len(args) == 0 {
		return oneshotUsageError("break", "call")
	}
	addr, err := parseCLIAddress(args[0])
	if err != nil {
		return err
	}
	breakpoint := oneshotBreakpoint{
		ID:            len(session.breakpoints) + 1,
		Kind:          breakpointKindCall,
		Display:       fmt.Sprintf("call %s", formatAddress(addr)),
		AddressFilter: cloneAddress(&addr),
	}
	if len(args) > 1 {
		method, canonical, err := buildMethodFromSignature(strings.Join(args[1:], " "))
		if err != nil {
			return err
		}
		breakpoint.Signature = canonical
		breakpoint.Selector = append([]byte(nil), method.ID...)
		breakpoint.Display = fmt.Sprintf("call %s %s", formatAddress(addr), canonical)
	}
	session.breakpoints = append(session.breakpoints, breakpoint)
	return nil
}

func (session *oneshotSession) addStorageBreakpoint(args []string) error {
	if len(args) == 0 {
		return oneshotUsageError("break", "storage")
	}
	mode := accessModeBoth
	if len(args) > 1 {
		parsed, err := parseAccessMode(args[1])
		if err != nil {
			return err
		}
		mode = parsed
	}
	identifier := args[0]
	var slot *engine.Hash
	label := identifier
	if parsedSlot, ok := session.resolveStorageIdentifier(identifier); ok {
		slot = &parsedSlot
		label = formatHash(parsedSlot)
	}
	session.breakpoints = append(session.breakpoints, oneshotBreakpoint{
		ID:         len(session.breakpoints) + 1,
		Kind:       breakpointKindStorage,
		Display:    fmt.Sprintf("storage %s %s", identifier, mode),
		Slot:       slot,
		SlotLabel:  label,
		AccessMode: mode,
	})
	return nil
}

func (session *oneshotSession) addMemoryBreakpoint(args []string) error {
	if len(args) == 0 {
		return oneshotUsageError("break", "memory")
	}
	if strings.EqualFold(args[0], "line") {
		if len(args) < 2 {
			return oneshotUsageError("break", "memory")
		}
		mode := accessModeBoth
		if len(args) > 2 {
			parsed, err := parseAccessMode(args[2])
			if err != nil {
				return err
			}
			mode = parsed
		}
		return session.addLineScopedMemoryBreakpoint(args[1], mode, 0, 0)
	}

	lineSpec := ""
	baseArgs := args
	for index := 0; index < len(args); index++ {
		if !strings.EqualFold(args[index], "at") {
			continue
		}
		if index+1 >= len(args) {
			return fmt.Errorf("memory breakpoint 'at' requires a source location")
		}
		lineSpec = args[index+1]
		baseArgs = args[:index]
		break
	}
	if len(baseArgs) == 0 {
		return oneshotUsageError("break", "memory")
	}
	offset, err := parseUint64Flexible(baseArgs[0])
	if err != nil {
		return err
	}
	size := uint64(1)
	mode := accessModeBoth
	if len(baseArgs) > 1 {
		if parsed, parseErr := parseUint64Flexible(baseArgs[1]); parseErr == nil {
			size = parsed
			if len(baseArgs) > 2 {
				mode, err = parseAccessMode(baseArgs[2])
				if err != nil {
					return err
				}
			}
		} else {
			mode, err = parseAccessMode(baseArgs[1])
			if err != nil {
				return err
			}
		}
	}
	breakpoint := oneshotBreakpoint{
		ID:         len(session.breakpoints) + 1,
		Kind:       breakpointKindMemory,
		Display:    fmt.Sprintf("memory %#x %d %s", offset, size, mode),
		Offset:     offset,
		Size:       size,
		AccessMode: mode,
	}
	if lineSpec != "" {
		if err := session.attachLineFilter(&breakpoint, lineSpec); err != nil {
			return err
		}
		breakpoint.Display += fmt.Sprintf(" at %s", lineSpec)
	}
	session.breakpoints = append(session.breakpoints, breakpoint)
	return nil
}

func (session *oneshotSession) addLineScopedMemoryBreakpoint(spec string, mode breakpointAccessMode, offset uint64, size uint64) error {
	breakpoint := oneshotBreakpoint{
		ID:         len(session.breakpoints) + 1,
		Kind:       breakpointKindMemory,
		Display:    fmt.Sprintf("memory line %s %s", spec, mode),
		Offset:     offset,
		Size:       size,
		AccessMode: mode,
	}
	if err := session.attachLineFilter(&breakpoint, spec); err != nil {
		return err
	}
	session.breakpoints = append(session.breakpoints, breakpoint)
	return nil
}

func (session *oneshotSession) attachLineFilter(breakpoint *oneshotBreakpoint, spec string) error {
	if session.bundle == nil || session.bundle.Index == nil {
		return fmt.Errorf("%s", sourceBundleRequiredMessage("line-scoped memory breakpoints"))
	}
	sourceName, line, column, err := parseBreakpointSpec(spec, session.bundle.SourceName)
	if err != nil {
		return err
	}
	pcs, err := resolveBreakpointPCs(session.bundle, sourceName, line, column)
	if err != nil {
		return err
	}
	breakpoint.SourceName = sourceName
	breakpoint.Line = line
	breakpoint.Column = column
	breakpoint.PCs = pcs
	return nil
}

func (session *oneshotSession) resolveStorageIdentifier(identifier string) (engine.Hash, bool) {
	if parsed, err := decodeStorageSlot(identifier); err == nil {
		return parsed, true
	}
	if session.bundle == nil {
		return engine.Hash{}, false
	}
	for _, mapping := range session.bundle.Metadata.PersistentStorage {
		if mapping.Entry.Label == identifier {
			parsed, err := decodeStorageSlot(mapping.Entry.Slot)
			return parsed, err == nil
		}
	}
	for _, mapping := range session.bundle.Metadata.TransientStorage {
		if mapping.Entry.Label == identifier {
			parsed, err := decodeStorageSlot(mapping.Entry.Slot)
			return parsed, err == nil
		}
	}
	return engine.Hash{}, false
}

func (session *oneshotSession) matchSourceBreakpoint(ctx *engine.HookContext) *oneshotBreakpoint {
	if ctx == nil || ctx.Opcode == nil {
		return nil
	}
	for index := range session.breakpoints {
		breakpoint := &session.breakpoints[index]
		if breakpoint.Kind != breakpointKindSource {
			continue
		}
		for _, pc := range breakpoint.PCs {
			if pc == ctx.Opcode.PC {
				return breakpoint
			}
		}
	}
	return nil
}

func (session *oneshotSession) matchRootFunctionBreakpoint(ctx *engine.HookContext) *oneshotBreakpoint {
	if ctx == nil || ctx.State == nil || ctx.Opcode == nil {
		return nil
	}
	if ctx.State.CallDepth() != 0 || ctx.Opcode.PC != 0 {
		return nil
	}
	for index := range session.breakpoints {
		breakpoint := &session.breakpoints[index]
		if breakpoint.Kind != breakpointKindFunction {
			continue
		}
		if selectorMatches(session.rootInput, breakpoint.Selector) {
			return breakpoint
		}
	}
	return nil
}

func resolveBreakpointPCs(bundle *contractmeta.Bundle, sourceName string, line int, column int) ([]uint64, error) {
	if bundle == nil || bundle.Index == nil {
		return nil, fmt.Errorf("source bundle does not contain source maps")
	}
	file, ok := bundle.Index.SourceFileByName(sourceName)
	if !ok {
		return nil, fmt.Errorf("source %q not found", sourceName)
	}
	if column <= 0 {
		column = 1
	}
	offset, ok := file.OffsetForLineColumn(line, column)
	if !ok {
		return nil, fmt.Errorf("invalid line/column %d:%d", line, column)
	}
	instructions := bundle.Index.InstructionsForSource(file.ID, offset, offset+1)
	if len(instructions) == 0 {
		for _, instruction := range bundle.Index.Instructions {
			if instruction.Source.SourceID != file.ID {
				continue
			}
			if instruction.Source.Start >= offset {
				instructions = append(instructions, instruction)
				break
			}
		}
	}
	if len(instructions) == 0 {
		return nil, fmt.Errorf("no instruction mapping found for requested source location")
	}
	pcs := make([]uint64, 0, len(instructions))
	seen := make(map[uint64]struct{})
	for _, instruction := range instructions {
		if _, ok := seen[instruction.PC]; ok {
			continue
		}
		seen[instruction.PC] = struct{}{}
		pcs = append(pcs, instruction.PC)
	}
	sort.Slice(pcs, func(i, j int) bool { return pcs[i] < pcs[j] })
	return pcs, nil
}

func parseBreakpointSpec(spec string, defaultSource string) (string, int, int, error) {
	trimmed := strings.TrimSpace(spec)
	if trimmed == "" {
		return "", 0, 0, fmt.Errorf("breakpoint spec is required")
	}
	parts := strings.Split(trimmed, ":")
	switch len(parts) {
	case 1:
		if defaultSource == "" {
			return "", 0, 0, fmt.Errorf("source name is required when no default source is loaded")
		}
		line, err := strconv.Atoi(parts[0])
		if err != nil {
			return "", 0, 0, fmt.Errorf("invalid line %q", parts[0])
		}
		return defaultSource, line, 1, nil
	case 2:
		if line, err := strconv.Atoi(parts[0]); err == nil {
			if defaultSource == "" {
				return "", 0, 0, fmt.Errorf("source name is required when no default source is loaded")
			}
			column, err := strconv.Atoi(parts[1])
			if err != nil {
				return "", 0, 0, fmt.Errorf("invalid column %q", parts[1])
			}
			return defaultSource, line, column, nil
		}
		line, err := strconv.Atoi(parts[1])
		if err != nil {
			return "", 0, 0, fmt.Errorf("invalid line %q", parts[1])
		}
		return parts[0], line, 1, nil
	default:
		column, err := strconv.Atoi(parts[len(parts)-1])
		if err != nil {
			return "", 0, 0, fmt.Errorf("invalid column %q", parts[len(parts)-1])
		}
		line, err := strconv.Atoi(parts[len(parts)-2])
		if err != nil {
			return "", 0, 0, fmt.Errorf("invalid line %q", parts[len(parts)-2])
		}
		source := strings.Join(parts[:len(parts)-2], ":")
		return source, line, column, nil
	}
}
