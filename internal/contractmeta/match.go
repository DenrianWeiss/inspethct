package contractmeta

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
)

// stripMetadataFooter removes the trailing Solidity CBOR metadata blob (ipfs/bzzr1 hash + 2 length bytes)
// if present. Returns the bytecode without the footer and a boolean indicating whether a footer was found.
func stripMetadataFooter(code []byte) ([]byte, bool) {
	if len(code) < 4 {
		return code, false
	}
	footerLen := int(binary.BigEndian.Uint16(code[len(code)-2:]))
	if footerLen <= 0 || footerLen+2 > len(code) {
		return code, false
	}
	footerStart := len(code) - 2 - footerLen
	if footerStart < 0 {
		return code, false
	}
	// Sanity-check that the footer starts with a CBOR map prefix (0xa1 / 0xa2 / 0xa3).
	first := code[footerStart]
	if first&0xf0 != 0xa0 {
		return code, false
	}
	return code[:footerStart], true
}

// normalizeBytecode strips the CBOR metadata footer and zeroes any trailing bytes that are commonly
// patched at deploy time (constructor immutables remain at zero in artifacts).
func normalizeBytecode(code []byte) []byte {
	stripped, _ := stripMetadataFooter(code)
	return stripped
}

// bytecodeSimilarity returns the longest matching prefix length between two normalized bytecodes,
// treating zero bytes in the artifact as wildcard immutable holes.
func bytecodeSimilarity(onChain, artifact []byte) (matched int, total int) {
	a := normalizeBytecode(onChain)
	b := normalizeBytecode(artifact)
	limit := len(a)
	if len(b) < limit {
		limit = len(b)
	}
	for i := 0; i < limit; i++ {
		if a[i] == b[i] {
			matched++
			continue
		}
		// Solidity emits PUSH32 0x000... placeholders for immutables in artifacts.
		// Tolerate those bytes (the artifact byte will be 0).
		if b[i] == 0 {
			matched++
			continue
		}
		break
	}
	return matched, limit
}

// BytecodeMatch describes how strongly an on-chain bytecode matches a local artifact.
type BytecodeMatch struct {
	SourceName    string
	ContractName  string
	BuildInfoPath string
	MatchedBytes  int
	TotalBytes    int
	Exact         bool
}

// FindLocalBytecodeMatches scans local Foundry/Hardhat build-info under projectRoot looking for a
// contract whose deployedBytecode matches onChain. Returns matches sorted by best score first.
func FindLocalBytecodeMatches(projectRoot string, onChain []byte) ([]BytecodeMatch, error) {
	if len(onChain) == 0 {
		return nil, nil
	}
	paths, err := findBuildInfoFiles(projectRoot)
	if err != nil {
		return nil, err
	}
	target := normalizeBytecode(onChain)
	if len(target) == 0 {
		return nil, nil
	}
	var results []BytecodeMatch
	for _, path := range paths {
		data, err := readFileQuietly(path)
		if err != nil {
			continue
		}
		var doc buildInfoDocument
		if err := json.Unmarshal(data, &doc); err != nil {
			continue
		}
		for sourceName, contractsBySource := range doc.Output.Contracts {
			for contractName, raw := range contractsBySource {
				artifactCode := extractDeployedBytecode(raw)
				if len(artifactCode) == 0 {
					continue
				}
				matched, total := bytecodeSimilarity(target, artifactCode)
				if matched == 0 {
					continue
				}
				if total < len(target)/2 && matched < len(target)/2 {
					continue
				}
				exact := matched == total && total > 0 && len(target) == len(normalizeBytecode(artifactCode))
				results = append(results, BytecodeMatch{
					SourceName:    sourceName,
					ContractName:  contractName,
					BuildInfoPath: path,
					MatchedBytes:  matched,
					TotalBytes:    total,
					Exact:         exact,
				})
			}
		}
	}
	// Sort by exact first, then by matched bytes desc.
	for i := 0; i < len(results); i++ {
		best := i
		for j := i + 1; j < len(results); j++ {
			if betterMatch(results[j], results[best]) {
				best = j
			}
		}
		results[i], results[best] = results[best], results[i]
	}
	return results, nil
}

func betterMatch(a, b BytecodeMatch) bool {
	if a.Exact != b.Exact {
		return a.Exact
	}
	return a.MatchedBytes > b.MatchedBytes
}

func extractDeployedBytecode(raw json.RawMessage) []byte {
	var artifact struct {
		EVM struct {
			DeployedBytecode struct {
				Object string `json:"object"`
			} `json:"deployedBytecode"`
		} `json:"evm"`
	}
	if err := json.Unmarshal(raw, &artifact); err != nil {
		return nil
	}
	hexStr := strings.TrimPrefix(artifact.EVM.DeployedBytecode.Object, "0x")
	if hexStr == "" {
		return nil
	}
	out, err := hex.DecodeString(hexStr)
	if err != nil {
		return nil
	}
	return out
}

// LoadBundleFromBuildInfoMatch loads a bundle for a previously-discovered match.
func LoadBundleFromBuildInfoMatch(match BytecodeMatch, opts LoadOptions) (*Bundle, error) {
	if opts.SourceName == "" {
		opts.SourceName = match.SourceName
	}
	if opts.ContractName == "" {
		opts.ContractName = match.ContractName
	}
	return loadBundleFromBuildInfo(match.BuildInfoPath, opts)
}

func readFileQuietly(path string) ([]byte, error) {
	return os.ReadFile(path)
}
