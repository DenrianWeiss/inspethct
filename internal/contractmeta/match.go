package contractmeta

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"

	"inspethct/internal/contractmeta/bytematch"
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

// Length and similarity thresholds for considering an artifact a candidate.
// We require the artifact to be roughly the same size as the on-chain code
// (to avoid matching arbitrary contracts that happen to share a long PUSH/RETURN
// preamble) and require a meaningful prefix match.
const (
	localMatchLengthRatio = 0.9 // |a-b|/max(a,b) must be <= 1 - this
	localMatchPrefixRatio = 0.6 // matched/total must be >= this to be a candidate
	localMatchMinBytes    = 64  // additionally require at least this many matched bytes
	matchEngineEnvKey     = "INSPETHCT_MATCH_ENGINE"
)

// FindLocalBytecodeMatches scans local Foundry/Hardhat build-info under projectRoot looking for a
// contract whose deployedBytecode matches onChain. Returns matches sorted by best score first.
func FindLocalBytecodeMatches(projectRoot string, onChain []byte) ([]BytecodeMatch, error) {
	if strings.EqualFold(strings.TrimSpace(os.Getenv(matchEngineEnvKey)), "legacy") {
		return findLocalBytecodeMatchesLegacy(projectRoot, onChain)
	}
	return findLocalBytecodeMatchesV2(projectRoot, onChain)
}

func findLocalBytecodeMatchesV2(projectRoot string, onChain []byte) ([]BytecodeMatch, error) {
	results, err := bytematch.FindMatches(projectRoot, onChain)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}
	out := make([]BytecodeMatch, 0, len(results))
	for _, match := range results {
		out = append(out, BytecodeMatch{
			SourceName:    match.SourceName,
			ContractName:  match.ContractName,
			BuildInfoPath: match.BuildInfoPath,
			MatchedBytes:  match.MatchedBytes,
			TotalBytes:    match.TotalBytes,
			Exact:         match.Exact,
		})
	}
	return out, nil
}

func findLocalBytecodeMatchesLegacy(projectRoot string, onChain []byte) ([]BytecodeMatch, error) {
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
				normalizedArtifact := normalizeBytecode(artifactCode)
				if len(normalizedArtifact) == 0 {
					continue
				}
				// Length sanity check: the artifact and on-chain code must be
				// roughly the same size. A contract whose normalized bytecode
				// length differs significantly is almost certainly not the same
				// program.
				a, b := len(target), len(normalizedArtifact)
				lo, hi := a, b
				if lo > hi {
					lo, hi = hi, lo
				}
				if float64(lo)/float64(hi) < localMatchLengthRatio {
					continue
				}
				matched, total := bytecodeSimilarity(target, artifactCode)
				if total == 0 || matched < localMatchMinBytes {
					continue
				}
				if float64(matched)/float64(total) < localMatchPrefixRatio {
					continue
				}
				exact := matched == total && total > 0 && len(target) == len(normalizedArtifact)
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
