package contractmeta

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadLocalProjectBundleFromBuildInfo(t *testing.T) {
	root := t.TempDir()
	buildInfoDir := filepath.Join(root, "artifacts", "build-info")
	if err := os.MkdirAll(buildInfoDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	path := filepath.Join(buildInfoDir, "sample.json")
	data := `{
		"solcVersion": "0.8.20",
		"input": {"language": "Solidity", "sources": {"A.sol": {"content": "contract C { uint256 x; }"}}},
		"output": {
			"sources": {"A.sol": {"id": 0, "ast": {"id": 1, "nodeType": "SourceUnit", "src": "0:25:0", "nodes": []}}},
			"contracts": {"A.sol": {"C": {
				"abi": [{"type": "function", "name": "f", "inputs": [], "outputs": []}],
				"storageLayout": {"storage": [{"astId": 1, "contract": "A.sol:C", "label": "x", "offset": 0, "slot": "0", "type": "t_uint256"}], "types": {"t_uint256": {"encoding": "inplace", "label": "uint256", "numberOfBytes": "32"}}},
				"transientStorageLayout": {"storage": [], "types": {}},
				"evm": {
					"bytecode": {"object": "60005400", "sourceMap": "0:10:0;0:10:0;", "generatedSources": []},
					"deployedBytecode": {"object": "60005400", "sourceMap": "0:10:0;0:10:0;", "generatedSources": []}
				}
			}}}
		}
	}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	bundle, err := LoadLocalProjectBundle(root, LoadOptions{ContractName: "C", Runtime: true})
	if err != nil {
		t.Fatalf("LoadLocalProjectBundle() error = %v", err)
	}
	if bundle.SourceName != "A.sol" {
		t.Fatalf("bundle.SourceName = %q, want A.sol", bundle.SourceName)
	}
	if len(bundle.Metadata.ABI) != 1 {
		t.Fatalf("len(bundle.Metadata.ABI) = %d, want 1", len(bundle.Metadata.ABI))
	}
	if len(bundle.Metadata.PersistentStorage) != 1 {
		t.Fatalf("len(bundle.Metadata.PersistentStorage) = %d, want 1", len(bundle.Metadata.PersistentStorage))
	}
}
