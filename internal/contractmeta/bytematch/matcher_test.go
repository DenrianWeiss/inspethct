package bytematch

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestFindMatches_ImmutableReferencesAndMetadata(t *testing.T) {
	root := t.TempDir()
	artifact := makeTestRuntime(96)
	onChain := append([]byte(nil), artifact...)

	for i := 20; i < 24; i++ {
		artifact[i] = 0x00
		onChain[i] = byte(0xa0 + i)
	}

	artifact = appendMetadataWithLength(artifact, 0xa1, []byte("ipfs-artifact"))
	onChain = appendMetadataWithLength(onChain, 0xa1, []byte("ipfs-onchain"))

	writeBuildInfo(t, root, "A.sol", "C", artifact, map[string][]immutableReference{
		"0": []immutableReference{{Start: 20, Length: 4}},
	})

	matches, err := FindMatches(root, onChain)
	if err != nil {
		t.Fatalf("FindMatches() error = %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("len(matches) = %d, want 1", len(matches))
	}
	if matches[0].SourceName != "A.sol" || matches[0].ContractName != "C" {
		t.Fatalf("unexpected match = %#v", matches[0])
	}
	if !matches[0].Exact {
		t.Fatalf("matches[0].Exact = false, want true")
	}
}

func TestFindMatches_RejectMismatchOutsideImmutable(t *testing.T) {
	root := t.TempDir()
	artifact := appendMetadataWithLength(makeTestRuntime(96), 0xa1, []byte("ipfs-meta"))
	onChain := append([]byte(nil), artifact...)
	onChain[10] ^= 0xff

	writeBuildInfo(t, root, "A.sol", "C", artifact, nil)

	matches, err := FindMatches(root, onChain)
	if err != nil {
		t.Fatalf("FindMatches() error = %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("len(matches) = %d, want 0", len(matches))
	}
}

func TestFindMatches_LooseMetadataTrim(t *testing.T) {
	root := t.TempDir()
	artifact := makeTestRuntime(80)
	onChain := append([]byte(nil), artifact...)

	artifact = append(artifact, 0xb8)
	artifact = append(artifact, []byte("ipfs-broken-footer")...)
	artifact = append(artifact, 0x01, 0x02) // invalid length for strict footer detection

	onChain = append(onChain, 0xb8)
	onChain = append(onChain, []byte("ipfs-other-broken")...)
	onChain = append(onChain, 0x01, 0x02)

	writeBuildInfo(t, root, "B.sol", "D", artifact, nil)

	matches, err := FindMatches(root, onChain)
	if err != nil {
		t.Fatalf("FindMatches() error = %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("len(matches) = %d, want 1", len(matches))
	}
}

func writeBuildInfo(t *testing.T, root, sourceName, contractName string, artifactCode []byte, refs map[string][]immutableReference) {
	t.Helper()
	buildInfoDir := filepath.Join(root, "artifacts", "build-info")
	if err := os.MkdirAll(buildInfoDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if refs == nil {
		refs = map[string][]immutableReference{}
	}
	objectHex := "0x" + hex.EncodeToString(artifactCode)
	doc := map[string]any{
		"output": map[string]any{
			"contracts": map[string]any{
				sourceName: map[string]any{
					contractName: map[string]any{
						"evm": map[string]any{
							"deployedBytecode": map[string]any{
								"object":              objectHex,
								"immutableReferences": refs,
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

func makeTestRuntime(size int) []byte {
	code := make([]byte, size)
	for i := range code {
		code[i] = byte((i % 251) + 1)
	}
	return code
}

func appendMetadataWithLength(code []byte, mapPrefix byte, payload []byte) []byte {
	footer := make([]byte, 0, len(payload)+1)
	footer = append(footer, mapPrefix)
	footer = append(footer, payload...)
	out := append([]byte(nil), code...)
	out = append(out, footer...)
	length := len(footer)
	out = append(out, byte(length>>8), byte(length))
	return out
}
