package contractmeta

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestFindLocalBytecodeMatches_DefaultUsesV2AndEnvAllowsLegacy(t *testing.T) {
	root := t.TempDir()
	artifact := make([]byte, 96)
	onChain := make([]byte, 96)
	for i := range artifact {
		artifact[i] = byte((i % 251) + 1)
		onChain[i] = artifact[i]
	}
	artifact[10] = 0x00
	onChain[10] = 0xab

	writeMatchEngineBuildInfo(t, root, "A.sol", "C", artifact)

	t.Setenv(matchEngineEnvKey, "")
	matches, err := FindLocalBytecodeMatches(root, onChain)
	if err != nil {
		t.Fatalf("FindLocalBytecodeMatches(default) error = %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("default engine len(matches) = %d, want 0", len(matches))
	}

	t.Setenv(matchEngineEnvKey, "legacy")
	matches, err = FindLocalBytecodeMatches(root, onChain)
	if err != nil {
		t.Fatalf("FindLocalBytecodeMatches(legacy) error = %v", err)
	}
	if len(matches) == 0 {
		t.Fatalf("legacy engine len(matches) = 0, want > 0")
	}
}

func writeMatchEngineBuildInfo(t *testing.T, root, sourceName, contractName string, artifactCode []byte) {
	t.Helper()
	buildInfoDir := filepath.Join(root, "artifacts", "build-info")
	if err := os.MkdirAll(buildInfoDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	objectHex := "0x" + hex.EncodeToString(artifactCode)
	doc := map[string]any{
		"output": map[string]any{
			"contracts": map[string]any{
				sourceName: map[string]any{
					contractName: map[string]any{
						"evm": map[string]any{
							"deployedBytecode": map[string]any{
								"object": objectHex,
							},
						},
					},
				},
			},
		},
	}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	path := filepath.Join(buildInfoDir, "sample.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}
