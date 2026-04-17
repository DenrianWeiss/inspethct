package srcmap

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type standardJSON struct {
	Sources   map[string]standardJSONSource                 `json:"sources"`
	Contracts map[string]map[string]standardJSONContract    `json:"contracts"`
}

type standardJSONSource struct {
	ID      int             `json:"id"`
	AST     map[string]any  `json:"ast"`
	Content string          `json:"content"`
}

type standardJSONContract struct {
	StorageLayout          StorageLayout     `json:"storageLayout"`
	TransientStorageLayout StorageLayout     `json:"transientStorageLayout"`
	EVM                    standardJSONEVM   `json:"evm"`
}

type standardJSONEVM struct {
	Bytecode         standardJSONBytecode `json:"bytecode"`
	DeployedBytecode standardJSONBytecode `json:"deployedBytecode"`
}

type standardJSONBytecode struct {
	Object           string               `json:"object"`
	SourceMap        string               `json:"sourceMap"`
	GeneratedSources []generatedSource    `json:"generatedSources"`
}

type generatedSource struct {
	ID       int            `json:"id"`
	Name     string         `json:"name"`
	Language string         `json:"language"`
	Contents string         `json:"contents"`
	AST      map[string]any `json:"ast"`
}

// BuildIndexFromStandardJSON creates a bidirectional mapping index for one contract.
func BuildIndexFromStandardJSON(data []byte, cfg BuildConfig) (*Index, error) {
	var doc standardJSON
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("decode standard-json: %w", err)
	}

	contractsByName, ok := doc.Contracts[cfg.SourceName]
	if !ok {
		return nil, fmt.Errorf("source %q not found in contracts output", cfg.SourceName)
	}
	artifact, ok := contractsByName[cfg.ContractName]
	if !ok {
		return nil, fmt.Errorf("contract %q not found under source %q", cfg.ContractName, cfg.SourceName)
	}

	idx := &Index{
		Build:              cfg,
		Sources:            make(map[int]*SourceFile),
		InstructionByPC:    make(map[uint64]*InstructionMapping),
		NodesByID:          make(map[int]*ASTNode),
		NodesBySourceID:    make(map[int][]*ASTNode),
		GeneratedSourceIDs: make(map[int]struct{}),
		PersistentLayout:   artifact.StorageLayout,
		TransientLayout:    artifact.TransientStorageLayout,
	}

	for name, src := range doc.Sources {
		file := &SourceFile{
			ID:      src.ID,
			Name:    name,
			Content: src.Content,
			AST:     parseAST(src.AST),
		}
		idx.Sources[file.ID] = file
	}

	for _, generated := range artifact.EVM.Bytecode.GeneratedSources {
		idx.addGeneratedSource(generated)
	}
	for _, generated := range artifact.EVM.DeployedBytecode.GeneratedSources {
		idx.addGeneratedSource(generated)
	}

	idx.indexASTs()
	idx.CreationBytecode = decodeHexBytecode(artifact.EVM.Bytecode.Object)
	idx.RuntimeBytecode = decodeHexBytecode(artifact.EVM.DeployedBytecode.Object)

	selected := artifact.EVM.Bytecode
	selectedCode := idx.CreationBytecode
	if cfg.Runtime {
		selected = artifact.EVM.DeployedBytecode
		selectedCode = idx.RuntimeBytecode
	}

	instructions, err := ParseInstructionMappings(selectedCode, selected.SourceMap)
	if err != nil {
		return nil, err
	}
	for i := range instructions {
		entry := &instructions[i]
		entry.AST = idx.deepestNodeAt(entry.Source)
		idx.InstructionByPC[entry.PC] = entry
	}
	idx.Instructions = instructions
	idx.StorageVariables = idx.buildStorageVariables(artifact.StorageLayout, StorageScopePersistent)
	idx.TransientVariables = idx.buildStorageVariables(artifact.TransientStorageLayout, StorageScopeTransient)

	for sourceID, nodes := range idx.NodesBySourceID {
		sort.Slice(nodes, func(i, j int) bool {
			if nodes[i].Src.Start == nodes[j].Src.Start {
				return nodes[i].Src.Length < nodes[j].Src.Length
			}
			return nodes[i].Src.Start < nodes[j].Src.Start
		})
		idx.NodesBySourceID[sourceID] = nodes
	}

	return idx, nil
}

func decodeHexBytecode(value string) []byte {
	trimmed := strings.TrimPrefix(value, "0x")
	if trimmed == "" {
		return nil
	}
	bytes, err := hex.DecodeString(trimmed)
	if err != nil {
		return nil
	}
	return bytes
}

func (idx *Index) addGeneratedSource(g generatedSource) {
	idx.GeneratedSourceIDs[g.ID] = struct{}{}
	idx.Sources[g.ID] = &SourceFile{
		ID:        g.ID,
		Name:      g.Name,
		Content:   g.Contents,
		Generated: true,
		Language:  g.Language,
		AST:       parseAST(g.AST),
	}
}