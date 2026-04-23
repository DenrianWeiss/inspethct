package contractmeta

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"inspethct/internal/engine"
)

// MappingEntry describes one entry in deployment-mapping.json.
// Format: {"0xADDR": {"name": "MyToken", "contract": "src/Token.sol", "pull": true}}
// If contract contains a colon (e.g. "src/Token.sol:Token"), the part after the colon
// is used as the contract name hint for local matching.
type MappingEntry struct {
	Name     string `json:"name"`
	Contract string `json:"contract"` // source path, optionally "path.sol:ContractName"
	Pull     bool   `json:"pull"`     // if true, allow explorer fetch when local match fails
}

// DeploymentMapping maps contract addresses to their mapping entries.
type DeploymentMapping map[engine.Address]MappingEntry

// LoadDeploymentMapping reads deployment-mapping.json from projectRoot.
// Returns an empty map (not an error) if the file does not exist.
func LoadDeploymentMapping(projectRoot string) (DeploymentMapping, error) {
	if projectRoot == "" {
		return DeploymentMapping{}, nil
	}
	data, err := os.ReadFile(filepath.Join(projectRoot, "deployment-mapping.json"))
	if os.IsNotExist(err) {
		return DeploymentMapping{}, nil
	}
	if err != nil {
		return nil, err
	}
	var raw map[string]MappingEntry
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	result := make(DeploymentMapping, len(raw))
	for addrStr, entry := range raw {
		addr, ok := parseAddressHex(addrStr)
		if !ok {
			continue
		}
		result[addr] = entry
	}
	return result, nil
}

// parseAddressHex parses a hex address string (with or without 0x prefix).
func parseAddressHex(s string) (engine.Address, bool) {
	s = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(s)), "0x")
	if len(s) != 40 {
		return engine.Address{}, false
	}
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 20 {
		return engine.Address{}, false
	}
	var addr engine.Address
	copy(addr[:], b)
	return addr, true
}

// splitContractPath splits "src/Token.sol:TokenContract" into (sourceName, contractName).
// If no colon, returns (contract, "") leaving contractName empty (auto-detect).
func splitContractPath(contract string) (sourceName, contractName string) {
	idx := strings.LastIndex(contract, ":")
	if idx < 0 {
		return contract, ""
	}
	return contract[:idx], contract[idx+1:]
}
