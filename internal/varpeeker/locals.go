package varpeeker

import (
	"strconv"
	"strings"

	"inspethct/internal/engine"
	"inspethct/internal/srcmap"
)

// peekLocals is the entry point for the locals category. It mirrors the
// behaviour of jsonrpc.collectLocalsAtPC but produces Variable values
// directly and consults the optional Tracker for memory-pointer
// confirmation.
func peekLocals(index *srcmap.Index, tracker *Tracker, state engine.ReadOnlyState, pc uint64) []Variable {
	if index == nil || state == nil {
		return nil
	}
	mapping, ok := index.InstructionAtPC(pc)
	if !ok || mapping.Source.SourceID < 0 {
		return nil
	}
	enclosing := findEnclosingFunctionLikeNode(index, mapping.AST)
	if enclosing == nil {
		enclosing = findEnclosingFunctionLikeBySource(index, mapping.Source)
	}
	if enclosing == nil {
		return nil
	}
	currentStart := mapping.Source.Start
	file := index.Sources[mapping.Source.SourceID]

	paramDecls := paramListFrom(enclosing.Raw, "parameters")
	returnDecls := paramListFrom(enclosing.Raw, "returnParameters")
	paramCount := len(paramDecls)
	returnCount := len(returnDecls)
	confidence := ConfidenceLow

	if dbg, found := index.FunctionDebugByID[enclosing.ID]; found && dbg != nil {
		if dbg.ParameterSlots > 0 || dbg.ReturnSlots > 0 {
			paramCount = dbg.ParameterSlots
			returnCount = dbg.ReturnSlots
			confidence = ConfidenceMedium
		}
	}

	out := make([]Variable, 0, len(paramDecls)+len(returnDecls)+4)

	for i, decl := range paramDecls {
		depth := returnCount + (paramCount - 1 - i)
		v := buildDeclaration(decl, KindParameter, file)
		v = decodeIfStackResolvable(v, state, depth, tracker)
		if v.Confidence == ConfidenceLow || v.Confidence == "" {
			v.Confidence = confidence
		}
		out = append(out, v)
	}

	for i, decl := range returnDecls {
		name, _ := decl["name"].(string)
		if name == "" {
			continue
		}
		depth := returnCount - 1 - i
		v := buildDeclaration(decl, KindReturn, file)
		v = decodeIfStackResolvable(v, state, depth, tracker)
		if v.Confidence == ConfidenceLow || v.Confidence == "" {
			v.Confidence = confidence
		}
		out = append(out, v)
	}

	walkASTRaw(enclosing.Raw, func(node map[string]any) bool {
		nodeType, _ := node["nodeType"].(string)
		if nodeType != "VariableDeclaration" {
			return true
		}
		src, ok := parseSrcAttr(node["src"])
		if !ok {
			return true
		}
		if src.Start >= currentStart {
			return false
		}
		if isInsideParameterList(enclosing.Raw, node) {
			return true
		}
		v := buildDeclaration(node, KindLocal, file)
		if v.Name == "" {
			return true
		}
		// Tracker enrichment: if we recorded a write whose source range
		// contains this declaration's range, surface its memory region.
		if tracker != nil {
			if region, found := tracker.MemoryRegionForSource(src); found {
				v.Location = Location{Kind: LocationMemory, Offset: region.Offset, Length: region.Size}
				if v.Confidence == ConfidenceUnavailable || v.Confidence == "" {
					v.Confidence = ConfidenceHigh
				}
			}
		}
		out = append(out, v)
		return true
	})
	return out
}

func decodeIfStackResolvable(v Variable, state engine.ReadOnlyState, depth int, tracker *Tracker) Variable {
	if state == nil || depth < 0 || depth >= state.StackLen() {
		return v
	}
	v.Location = Location{Kind: LocationStack, StackIndex: depth + 1}
	word := state.StackPeekN(depth)
	value, ptr, note := decodeStackValue(word, v.Type, v.StorageLoc, state)
	if value != "" {
		v.Value = value
		v.Confidence = ConfidenceLow
	}
	if ptr != 0 {
		v.Location = Location{Kind: LocationMemory, Offset: ptr, StackIndex: depth + 1}
	}
	if note != "" {
		v.Note = note
	}
	// Tracker confirmation: if the parameter/return is a memory
	// reference and tracker has recorded a write at this offset, bump
	// confidence to high.
	if tracker != nil && ptr != 0 {
		if region, found := tracker.MemoryRegionAt(ptr); found && region.Size > 0 {
			v.Location.Length = region.Size
			v.Confidence = ConfidenceHigh
		}
	}
	return v
}

func buildDeclaration(decl map[string]any, kind VariableKind, file *srcmap.SourceFile) Variable {
	name, _ := decl["name"].(string)
	typeStr := ""
	if td, ok := decl["typeDescriptions"].(map[string]any); ok {
		typeStr, _ = td["typeString"].(string)
	}
	if typeStr == "" {
		if tn, ok := decl["typeName"].(map[string]any); ok {
			if td, ok := tn["typeDescriptions"].(map[string]any); ok {
				typeStr, _ = td["typeString"].(string)
			}
		}
	}
	storageLoc, _ := decl["storageLocation"].(string)
	if storageLoc == "" || storageLoc == "default" {
		storageLoc = "stack"
	}
	line := 0
	srcID := -1
	if file != nil {
		if src, ok := parseSrcAttr(decl["src"]); ok {
			l, _ := file.LineColumnForOffset(src.Start)
			line = l
			srcID = src.SourceID
		}
	}
	return Variable{
		Name:           name,
		Kind:           kind,
		Type:           typeStr,
		StorageLoc:     storageLoc,
		DeclaredAtLine: line,
		SourceID:       srcID,
		Confidence:     ConfidenceUnavailable,
	}
}

// --- AST traversal helpers (mirrors jsonrpc/gdb.go) ---

func findEnclosingFunctionLikeNode(index *srcmap.Index, node *srcmap.ASTNode) *srcmap.ASTNode {
	for current := node; current != nil; {
		switch current.NodeType {
		case "FunctionDefinition", "ModifierDefinition":
			return current
		}
		if current.ParentID == 0 {
			return nil
		}
		parent, ok := index.NodesByID[current.ParentID]
		if !ok || parent == current {
			return nil
		}
		current = parent
	}
	return nil
}

func findEnclosingFunctionLikeBySource(index *srcmap.Index, src srcmap.SourceRange) *srcmap.ASTNode {
	var best *srcmap.ASTNode
	for _, node := range index.NodesBySourceID[src.SourceID] {
		switch node.NodeType {
		case "FunctionDefinition", "ModifierDefinition":
		default:
			continue
		}
		if node.Src.Start > src.Start || node.Src.End() < src.End() {
			continue
		}
		if best == nil || node.Src.Length < best.Src.Length {
			best = node
		}
	}
	return best
}

func findEnclosingFunction(index *srcmap.Index, mapping *srcmap.InstructionMapping) *srcmap.ASTNode {
	if mapping == nil {
		return nil
	}
	if node := findEnclosingFunctionLikeNode(index, mapping.AST); node != nil {
		return node
	}
	return findEnclosingFunctionLikeBySource(index, mapping.Source)
}

func paramListFrom(raw map[string]any, key string) []map[string]any {
	if raw == nil {
		return nil
	}
	list, ok := raw[key].(map[string]any)
	if !ok {
		return nil
	}
	params, ok := list["parameters"].([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(params))
	for _, item := range params {
		if decl, ok := item.(map[string]any); ok {
			out = append(out, decl)
		}
	}
	return out
}

func parseSrcAttr(value any) (srcmap.SourceRange, bool) {
	str, ok := value.(string)
	if !ok {
		return srcmap.SourceRange{}, false
	}
	parts := strings.Split(str, ":")
	if len(parts) < 3 {
		return srcmap.SourceRange{}, false
	}
	start, err1 := strconv.Atoi(parts[0])
	length, err2 := strconv.Atoi(parts[1])
	fileID, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return srcmap.SourceRange{}, false
	}
	return srcmap.SourceRange{SourceID: fileID, Start: start, Length: length}, true
}

func walkASTRaw(node map[string]any, visit func(map[string]any) bool) {
	if node == nil {
		return
	}
	if !visit(node) {
		return
	}
	for _, value := range node {
		switch typed := value.(type) {
		case map[string]any:
			if _, hasID := typed["id"]; hasID {
				walkASTRaw(typed, visit)
			}
		case []any:
			for _, item := range typed {
				if child, ok := item.(map[string]any); ok {
					if _, hasID := child["id"]; hasID {
						walkASTRaw(child, visit)
					}
				}
			}
		}
	}
}

func isInsideParameterList(funcRaw, target map[string]any) bool {
	for _, key := range []string{"parameters", "returnParameters"} {
		list, ok := funcRaw[key].(map[string]any)
		if !ok {
			continue
		}
		params, ok := list["parameters"].([]any)
		if !ok {
			continue
		}
		for _, item := range params {
			if reflectSameMap(item, target) {
				return true
			}
		}
	}
	return false
}

func reflectSameMap(a any, b map[string]any) bool {
	m, ok := a.(map[string]any)
	if !ok {
		return false
	}
	idA, okA := asJSONNumber(m["id"])
	idB, okB := asJSONNumber(b["id"])
	return okA && okB && idA == idB
}

func asJSONNumber(v any) (float64, bool) {
	switch typed := v.(type) {
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	}
	return 0, false
}
