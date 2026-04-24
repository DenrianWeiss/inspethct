package bytematch

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Result describes a successful bytecode match against one build-info artifact.
type Result struct {
	SourceName     string
	ContractName   string
	BuildInfoPath  string
	MatchedBytes   int
	TotalBytes     int
	Exact          bool
	ImmutableBytes int
}

type immutableReference struct {
	Start  int `json:"start"`
	Length int `json:"length"`
}

type artifactCandidate struct {
	SourceName    string
	ContractName  string
	Bytecode      []byte
	Immutables    []immutableReference
	BuildInfoPath string
}

type cachedBuildInfo struct {
	modTimeNs int64
	size      int64
	entries   []artifactCandidate
}

var (
	cacheMu        sync.Mutex
	buildInfoCache = make(map[string]cachedBuildInfo)
)

// FindMatches scans local Foundry/Hardhat build-info files and returns strict
// immutable-aware matches sorted by confidence.
func FindMatches(projectRoot string, onChain []byte) ([]Result, error) {
	if len(onChain) == 0 {
		return nil, nil
	}
	onChainNormalized := normalizeForMatch(onChain)
	if len(onChainNormalized) == 0 {
		return nil, nil
	}

	paths, err := findBuildInfoFiles(projectRoot)
	if err != nil {
		return nil, err
	}

	results := make([]Result, 0)
	for _, path := range paths {
		entries, err := loadBuildInfoCandidates(path)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			matched, total, immutableBytes, ok := strictImmutableMatch(onChainNormalized, entry.Bytecode, entry.Immutables)
			if !ok || total == 0 {
				continue
			}
			results = append(results, Result{
				SourceName:     entry.SourceName,
				ContractName:   entry.ContractName,
				BuildInfoPath:  entry.BuildInfoPath,
				MatchedBytes:   matched,
				TotalBytes:     total,
				Exact:          true,
				ImmutableBytes: immutableBytes,
			})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		a := results[i]
		b := results[j]
		if a.Exact != b.Exact {
			return a.Exact
		}
		if a.TotalBytes != b.TotalBytes {
			return a.TotalBytes > b.TotalBytes
		}
		if a.ImmutableBytes != b.ImmutableBytes {
			return a.ImmutableBytes < b.ImmutableBytes
		}
		if a.MatchedBytes != b.MatchedBytes {
			return a.MatchedBytes > b.MatchedBytes
		}
		if a.SourceName != b.SourceName {
			return a.SourceName < b.SourceName
		}
		if a.ContractName != b.ContractName {
			return a.ContractName < b.ContractName
		}
		return a.BuildInfoPath < b.BuildInfoPath
	})

	return results, nil
}

func findBuildInfoFiles(projectRoot string) ([]string, error) {
	patterns := []string{
		filepath.Join(projectRoot, "out", "build-info", "*.json"),
		filepath.Join(projectRoot, "artifacts", "build-info", "*.json"),
	}
	var paths []string
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		paths = append(paths, matches...)
	}
	sort.Strings(paths)
	return paths, nil
}

func loadBuildInfoCandidates(path string) ([]artifactCandidate, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	cacheMu.Lock()
	cached, ok := buildInfoCache[path]
	cacheMu.Unlock()
	if ok && cached.modTimeNs == stat.ModTime().UnixNano() && cached.size == stat.Size() {
		return cached.entries, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Output struct {
			Contracts map[string]map[string]json.RawMessage `json:"contracts"`
		} `json:"output"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}

	entries := make([]artifactCandidate, 0)
	for sourceName, contracts := range doc.Output.Contracts {
		for contractName, raw := range contracts {
			bytecode, refs := decodeDeployedBytecode(raw)
			if len(bytecode) == 0 {
				continue
			}
			entries = append(entries, artifactCandidate{
				SourceName:    sourceName,
				ContractName:  contractName,
				Bytecode:      bytecode,
				Immutables:    refs,
				BuildInfoPath: path,
			})
		}
	}

	cacheMu.Lock()
	buildInfoCache[path] = cachedBuildInfo{
		modTimeNs: stat.ModTime().UnixNano(),
		size:      stat.Size(),
		entries:   entries,
	}
	cacheMu.Unlock()
	return entries, nil
}

func decodeDeployedBytecode(raw json.RawMessage) ([]byte, []immutableReference) {
	var artifact struct {
		EVM struct {
			DeployedBytecode struct {
				Object              string                          `json:"object"`
				ImmutableReferences map[string][]immutableReference `json:"immutableReferences"`
			} `json:"deployedBytecode"`
		} `json:"evm"`
	}
	if err := json.Unmarshal(raw, &artifact); err != nil {
		return nil, nil
	}
	hexStr := strings.TrimPrefix(artifact.EVM.DeployedBytecode.Object, "0x")
	if hexStr == "" {
		return nil, nil
	}
	code, err := hex.DecodeString(hexStr)
	if err != nil {
		return nil, nil
	}
	var refs []immutableReference
	for _, byID := range artifact.EVM.DeployedBytecode.ImmutableReferences {
		refs = append(refs, byID...)
	}
	return code, refs
}

func strictImmutableMatch(onChainNormalized, artifact []byte, refs []immutableReference) (int, int, int, bool) {
	artifactNormalized := normalizeForMatch(artifact)
	if len(artifactNormalized) == 0 || len(onChainNormalized) != len(artifactNormalized) {
		return 0, 0, 0, false
	}
	immutableMask := make([]bool, len(artifactNormalized))
	immutableBytes := 0
	for _, ref := range refs {
		if ref.Start < 0 || ref.Length <= 0 || ref.Start >= len(immutableMask) {
			continue
		}
		end := ref.Start + ref.Length
		if end > len(immutableMask) {
			end = len(immutableMask)
		}
		for i := ref.Start; i < end; i++ {
			if !immutableMask[i] {
				immutableMask[i] = true
				immutableBytes++
			}
		}
	}
	matched := 0
	for i := range artifactNormalized {
		if immutableMask[i] {
			matched++
			continue
		}
		if onChainNormalized[i] != artifactNormalized[i] {
			return matched, len(artifactNormalized), immutableBytes, false
		}
		matched++
	}
	return matched, len(artifactNormalized), immutableBytes, true
}

func normalizeForMatch(code []byte) []byte {
	if stripped, ok := stripMetadataFooterStrict(code); ok {
		return stripped
	}
	if stripped, ok := stripMetadataFooterLoose(code); ok {
		return stripped
	}
	return code
}

func stripMetadataFooterStrict(code []byte) ([]byte, bool) {
	if len(code) < 4 {
		return code, false
	}
	footerLen := int(binary.BigEndian.Uint16(code[len(code)-2:]))
	if footerLen <= 0 || footerLen+2 > len(code) {
		return code, false
	}
	start := len(code) - 2 - footerLen
	if start < 0 {
		return code, false
	}
	if !isLikelyCBORMapPrefix(code[start]) {
		return code, false
	}
	footer := code[start : len(code)-2]
	if !containsMetadataMarker(footer) {
		return code, false
	}
	return code[:start], true
}

func stripMetadataFooterLoose(code []byte) ([]byte, bool) {
	if len(code) < 8 {
		return code, false
	}
	start := len(code) - 256
	if start < 0 {
		start = 0
	}
	footerSearch := code[start:]
	markerPos := -1
	for _, marker := range metadataMarkers {
		idx := bytes.LastIndex(footerSearch, marker)
		if idx >= 0 && idx > markerPos {
			markerPos = idx
		}
	}
	if markerPos < 0 {
		return code, false
	}
	markerAbs := start + markerPos
	backtrackStart := markerAbs - 24
	if backtrackStart < 0 {
		backtrackStart = 0
	}
	for i := markerAbs; i >= backtrackStart; i-- {
		if !isLikelyCBORMapPrefix(code[i]) {
			continue
		}
		if len(code)-i < 4 {
			continue
		}
		return code[:i], true
	}
	return code, false
}

var metadataMarkers = [][]byte{
	[]byte("ipfs"),
	[]byte("bzzr1"),
	[]byte("bzzr0"),
	[]byte("solc"),
}

func containsMetadataMarker(footer []byte) bool {
	for _, marker := range metadataMarkers {
		if bytes.Contains(footer, marker) {
			return true
		}
	}
	return false
}

func isLikelyCBORMapPrefix(first byte) bool {
	// CBOR major type 5 (map): 0xa0..0xbf.
	return first&0xe0 == 0xa0
}
