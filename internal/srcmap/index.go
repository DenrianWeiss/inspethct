package srcmap

import (
	"math/big"
	"sort"
	"strings"
)

func (idx *Index) indexASTs() {
	for _, file := range idx.Sources {
		if file.AST == nil {
			continue
		}
		walkAST(file.AST, func(node *ASTNode) {
			idx.NodesByID[node.ID] = node
			idx.NodesBySourceID[node.Src.SourceID] = append(idx.NodesBySourceID[node.Src.SourceID], node)
		})
	}
}

func walkAST(node *ASTNode, visit func(*ASTNode)) {
	visit(node)
	for _, child := range node.Children {
		walkAST(child, visit)
	}
}

func (idx *Index) deepestNodeAt(src SourceRange) *ASTNode {
	nodes := idx.NodesBySourceID[src.SourceID]
	var best *ASTNode
	for _, node := range nodes {
		if node.Src.SourceID != src.SourceID {
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

func (idx *Index) buildStorageVariables(layout StorageLayout, scope StorageScope) []StorageVariableMapping {
	result := make([]StorageVariableMapping, 0, len(layout.Storage))
	for _, entry := range layout.Storage {
		typeInfo := layout.Types[entry.Type]
		ast := idx.NodesByID[entry.ASTID]
		root := SourceRange{SourceID: -1}
		if ast != nil {
			root = ast.Src
		}
		result = append(result, StorageVariableMapping{
			Entry: entry,
			Type:  typeInfo,
			AST:   ast,
			Root:  root,
			Scope: scope,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		left := parseSlotValue(result[i].Entry.Slot)
		right := parseSlotValue(result[j].Entry.Slot)
		return left.Cmp(right) < 0
	})
	return result
}

// InstructionAtPC returns the instruction mapping for a concrete EVM PC.
func (idx *Index) InstructionAtPC(pc uint64) (*InstructionMapping, bool) {
	mapping, ok := idx.InstructionByPC[pc]
	return mapping, ok
}

// InstructionsForSource returns all instruction mappings overlapping the given source range.
func (idx *Index) InstructionsForSource(sourceID, start, end int) []InstructionMapping {
	target := SourceRange{SourceID: sourceID, Start: start, Length: end - start}
	result := make([]InstructionMapping, 0)
	for _, instruction := range idx.Instructions {
		if instruction.Source.Overlaps(target) {
			result = append(result, instruction)
		}
	}
	return result
}

// NodesForSource returns AST nodes overlapping the given source range.
func (idx *Index) NodesForSource(sourceID, start, end int) []*ASTNode {
	target := SourceRange{SourceID: sourceID, Start: start, Length: end - start}
	nodes := idx.NodesBySourceID[sourceID]
	result := make([]*ASTNode, 0)
	for _, node := range nodes {
		if node.Src.Overlaps(target) {
			result = append(result, node)
		}
	}
	return result
}

// StorageByASTID returns storage variables rooted at the given AST node.
func (idx *Index) StorageByASTID(astID int) []StorageVariableMapping {
	result := make([]StorageVariableMapping, 0)
	for _, mapping := range idx.StorageVariables {
		if mapping.Entry.ASTID == astID {
			result = append(result, mapping)
		}
	}
	for _, mapping := range idx.TransientVariables {
		if mapping.Entry.ASTID == astID {
			result = append(result, mapping)
		}
	}
	return result
}

// StorageBySlot returns declared variables whose root slot equals the provided slot.
func (idx *Index) StorageBySlot(slot string, scope StorageScope) []StorageVariableMapping {
	all := idx.StorageVariables
	if scope == StorageScopeTransient {
		all = idx.TransientVariables
	}
	target := parseSlotValue(slot)
	result := make([]StorageVariableMapping, 0)
	for _, mapping := range all {
		if parseSlotValue(mapping.Entry.Slot).Cmp(target) == 0 {
			result = append(result, mapping)
		}
	}
	return result
}

func parseSlotValue(slot string) *big.Int {
	trimmed := strings.TrimSpace(slot)
	if trimmed == "" {
		return new(big.Int)
	}
	base := 10
	if strings.HasPrefix(trimmed, "0x") || strings.HasPrefix(trimmed, "0X") {
		trimmed = trimmed[2:]
		base = 16
	}
	value, ok := new(big.Int).SetString(trimmed, base)
	if !ok {
		return new(big.Int)
	}
	return value
}
