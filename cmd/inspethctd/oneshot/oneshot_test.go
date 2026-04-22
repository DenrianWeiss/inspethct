package oneshot

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectProjectKind(t *testing.T) {
	root := t.TempDir()
	if got := detectProjectKind(root); got != projectUnknown {
		t.Fatalf("detectProjectKind() = %q, want %q", got, projectUnknown)
	}
	if err := os.WriteFile(filepath.Join(root, "foundry.toml"), []byte("[profile.default]\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(foundry.toml) error = %v", err)
	}
	if got := detectProjectKind(root); got != projectFoundry {
		t.Fatalf("detectProjectKind(foundry) = %q, want %q", got, projectFoundry)
	}
	if err := os.WriteFile(filepath.Join(root, "hardhat.config.ts"), []byte("export default {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(hardhat.config.ts) error = %v", err)
	}
	if got := detectProjectKind(root); got != projectMixed {
		t.Fatalf("detectProjectKind(mixed) = %q, want %q", got, projectMixed)
	}
}

func TestFindProjectRootAndKindWalksParents(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub", "child"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "foundry.toml"), []byte("[profile.default]\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(foundry.toml) error = %v", err)
	}
	resolved, kind, err := findProjectRootAndKind(filepath.Join(root, "sub", "child"))
	if err != nil {
		t.Fatalf("findProjectRootAndKind() error = %v", err)
	}
	if resolved != root {
		t.Fatalf("resolved root = %q, want %q", resolved, root)
	}
	if kind != projectFoundry {
		t.Fatalf("kind = %q, want %q", kind, projectFoundry)
	}
}

func TestParseBreakpointSpec(t *testing.T) {
	source, line, column, err := parseBreakpointSpec("12:7", "src/A.sol")
	if err != nil {
		t.Fatalf("parseBreakpointSpec() error = %v", err)
	}
	if source != "src/A.sol" || line != 12 || column != 7 {
		t.Fatalf("parseBreakpointSpec default source = %q %d %d", source, line, column)
	}
	source, line, column, err = parseBreakpointSpec("src/B.sol:9:2", "src/A.sol")
	if err != nil {
		t.Fatalf("parseBreakpointSpec explicit source error = %v", err)
	}
	if source != "src/B.sol" || line != 9 || column != 2 {
		t.Fatalf("parseBreakpointSpec explicit source = %q %d %d", source, line, column)
	}
}

func TestApplyMemoryMutationToPauseExpandsSnapshot(t *testing.T) {
	pause := &oneshotPause{MemorySnapshot: []byte{0xaa}, MemorySize: 1}
	applyMemoryMutationToPause(pause, oneshotMutation{Kind: "memory", Offset: 2, Data: []byte{0xbb, 0xcc}})
	if pause.MemorySize != 4 {
		t.Fatalf("MemorySize = %d, want 4", pause.MemorySize)
	}
	if got := encodeCLIBytes(pause.MemorySnapshot); got != "0xaa0000bbcc" && got != "0xaa00bbcc" {
		t.Fatalf("MemorySnapshot = %s, want padded mutation applied", got)
	}
	if len(pause.MemorySnapshot) != 4 || pause.MemorySnapshot[2] != 0xbb || pause.MemorySnapshot[3] != 0xcc {
		t.Fatalf("mutation not applied correctly: %#v", pause.MemorySnapshot)
	}
}

func TestHandleSetMemoryCommandRecordsMutation(t *testing.T) {
	buffer := &bytes.Buffer{}
	session := &oneshotSession{
		position: 7,
		current:  &oneshotPause{MemorySnapshot: make([]byte, 4), MemorySize: 4},
	}
	if err := handleSetMemoryCommand(buffer, session, []string{"0x1", "0xaabb"}); err != nil {
		t.Fatalf("handleSetMemoryCommand() error = %v", err)
	}
	if len(session.mutations) != 1 {
		t.Fatalf("len(mutations) = %d, want 1", len(session.mutations))
	}
	mutation := session.mutations[0]
	if mutation.StepIndex != 7 || mutation.Offset != 1 {
		t.Fatalf("mutation = %#v, want stepIndex=7 offset=1", mutation)
	}
	if got := encodeCLIBytes(session.current.MemorySnapshot); got != "0x00aabb00" {
		t.Fatalf("updated snapshot = %s, want 0x00aabb00", got)
	}
}

func TestConfigureOneShotSessionSkipsInitialBreakpointPrompt(t *testing.T) {
	input := bytes.NewBufferString("0x1111111111111111111111111111111111111111111111111111111111111111\n")
	output := &bytes.Buffer{}
	session := &oneshotSession{}
	if err := configureOneShotSession(bufio.NewReader(input), output, session, projectDetection{Kind: projectUnknown}); err != nil {
		t.Fatalf("configureOneShotSession() error = %v", err)
	}
	text := output.String()
	if strings.Contains(text, "Initial breakpoint") {
		t.Fatalf("unexpected initial breakpoint prompt in output: %q", text)
	}
	if !strings.Contains(text, "Still available without source") {
		t.Fatalf("expected no-source guidance in output: %q", text)
	}
	if !strings.Contains(text, "Usage: help [command]") {
		t.Fatalf("expected repl help in output: %q", text)
	}
}

func TestHandleBreakHelpIncludesSourceFreeBreakpoints(t *testing.T) {
	output := &bytes.Buffer{}
	if err := handleBreakCommand(nil, output, &oneshotSession{}, []string{"break", "help"}); err != nil {
		t.Fatalf("handleBreakCommand() error = %v", err)
	}
	text := output.String()
	if !strings.Contains(text, "Works Without Source") {
		t.Fatalf("expected source-free section in output: %q", text)
	}
	if !strings.Contains(text, "break call <address> [signature]") {
		t.Fatalf("expected call breakpoint help in output: %q", text)
	}
	if !strings.Contains(text, "Requires Source Bundle") {
		t.Fatalf("expected source-required section in output: %q", text)
	}
}

func TestAddSourceBreakpointWithoutBundleMentionsAlternatives(t *testing.T) {
	session := &oneshotSession{}
	err := session.addSourceBreakpoint("src/A.sol:1")
	if err == nil {
		t.Fatal("expected error")
	}
	text := err.Error()
	if !strings.Contains(text, "Load source:") {
		t.Fatalf("expected load hint in error: %q", text)
	}
	if !strings.Contains(text, "Still available without source") {
		t.Fatalf("expected alternative breakpoint hint in error: %q", text)
	}
}

func TestHandleSetConfigHelpPrintsSupportedKeys(t *testing.T) {
	output := &bytes.Buffer{}
	if err := handleSetCommand(nil, output, &oneshotSession{}, []string{"set", "config", "help"}); err != nil {
		t.Fatalf("handleSetCommand() error = %v", err)
	}
	text := output.String()
	if !strings.Contains(text, "explorer.api-key") {
		t.Fatalf("expected explorer.api-key in help output: %q", text)
	}
	if !strings.Contains(text, "engine.chain-id") {
		t.Fatalf("expected engine.chain-id in help output: %q", text)
	}
}
