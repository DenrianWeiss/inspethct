package contractmeta

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"inspethct/internal/srcmap"
)

const solcBinariesBaseURL = "https://binaries.soliditylang.org"

type SolcManager struct {
	CacheDir string
	BaseURL  string
	Client   *http.Client
}

type solcList struct {
	Builds   []solcBuild       `json:"builds"`
	Releases map[string]string `json:"releases"`
}

type solcBuild struct {
	Path        string `json:"path"`
	Version     string `json:"version"`
	LongVersion string `json:"longVersion"`
	SHA256      string `json:"sha256"`
}

type solcCompilerOutput struct {
	Errors []solcCompilerMessage `json:"errors"`
}

type solcCompilerMessage struct {
	Severity         string `json:"severity"`
	FormattedMessage string `json:"formattedMessage"`
	Message          string `json:"message"`
}

// NewSolcManager constructs a solc downloader using the user's cache directory.
func NewSolcManager() (*SolcManager, error) {
	workDir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	cacheDir := solcCacheDirFor(workDir)
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, err
	}
	return &SolcManager{CacheDir: cacheDir, BaseURL: solcBinariesBaseURL, Client: http.DefaultClient}, nil
}

// EnsureVersion downloads the requested solc version into the cache if needed.
func (manager *SolcManager) EnsureVersion(ctx context.Context, compilerVersion string) (string, error) {
	if manager == nil {
		return "", fmt.Errorf("solc manager is nil")
	}
	build, err := manager.lookupBuild(ctx, compilerVersion)
	if err != nil {
		return "", err
	}
	path := filepath.Join(manager.CacheDir, build.Path)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	url := strings.TrimRight(manager.BaseURL, "/") + "/" + platformDirectory() + "/" + build.Path
	request, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	response, err := manager.httpClient().Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download solc %s: unexpected status %s", compilerVersion, response.Status)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return "", err
	}
	defer file.Close()
	if _, err := io.Copy(file, response.Body); err != nil {
		return "", err
	}
	return path, nil
}

// CompileStandardInput compiles Solidity standard-json input via a cached solc binary.
func (manager *SolcManager) CompileStandardInput(ctx context.Context, compilerVersion string, input []byte) ([]byte, error) {
	binaryPath, err := manager.EnsureVersion(ctx, compilerVersion)
	if err != nil {
		return nil, err
	}
	command := exec.CommandContext(ctx, binaryPath, "--standard-json")
	command.Stdin = bytes.NewReader(input)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("solc %s failed: %w: %s", compilerVersion, err, strings.TrimSpace(stderr.String()))
	}
	if compileErr := compilerOutputError(stdout.Bytes()); compileErr != nil {
		return nil, compileErr
	}
	return stdout.Bytes(), nil
}

// InspectBundleMetadata loads a bundle and returns only the extracted metadata.
func InspectBundleMetadata(bundle *Bundle) srcmap.ContractMetadata {
	if bundle == nil {
		return srcmap.ContractMetadata{}
	}
	return bundle.Metadata
}

func (manager *SolcManager) lookupBuild(ctx context.Context, compilerVersion string) (*solcBuild, error) {
	request, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(manager.BaseURL, "/")+"/"+platformDirectory()+"/list.json", nil)
	if err != nil {
		return nil, err
	}
	response, err := manager.httpClient().Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("load solc version list: unexpected status %s", response.Status)
	}
	var list solcList
	if err := json.NewDecoder(response.Body).Decode(&list); err != nil {
		return nil, err
	}
	normalized := normalizeCompilerVersion(compilerVersion)
	if path, ok := list.Releases[normalized.Short]; ok {
		for _, build := range list.Builds {
			if build.Path == path {
				return &build, nil
			}
		}
		return &solcBuild{Path: path, Version: normalized.Short, LongVersion: normalized.Full}, nil
	}
	for _, build := range list.Builds {
		if normalizeCompilerVersion(build.LongVersion).Full == normalized.Full || normalizeCompilerVersion(build.Version).Short == normalized.Short {
			return &build, nil
		}
	}
	return nil, fmt.Errorf("solc version %q is not available for %s", compilerVersion, platformDirectory())
}

func (manager *SolcManager) httpClient() *http.Client {
	if manager.Client != nil {
		return manager.Client
	}
	return http.DefaultClient
}

type normalizedVersion struct {
	Full  string
	Short string
}

func normalizeCompilerVersion(value string) normalizedVersion {
	trimmed := strings.TrimSpace(strings.TrimPrefix(value, "v"))
	short := trimmed
	if index := strings.Index(short, "+"); index >= 0 {
		short = short[:index]
	}
	return normalizedVersion{Full: trimmed, Short: short}
}

func platformDirectory() string {
	return platformDirectoryFor(runtime.GOOS, runtime.GOARCH)
}

func platformDirectoryFor(goos string, goarch string) string {
	switch goos + "/" + goarch {
	case "darwin/amd64", "darwin/arm64":
		return "macosx-amd64"
	case "linux/amd64":
		return "linux-amd64"
	case "linux/arm64":
		return "linux-arm64"
	case "windows/amd64":
		return "windows-amd64"
	default:
		return "linux-amd64"
	}
}

func solcCacheDirFor(workDir string) string {
	return filepath.Join(workDir, ".inspethct", "solc")
}

func compilerOutputError(output []byte) error {
	var compiled solcCompilerOutput
	if err := json.Unmarshal(output, &compiled); err != nil {
		return nil
	}
	messages := make([]string, 0)
	for _, item := range compiled.Errors {
		if !strings.EqualFold(strings.TrimSpace(item.Severity), "error") {
			continue
		}
		message := strings.TrimSpace(item.FormattedMessage)
		if message == "" {
			message = strings.TrimSpace(item.Message)
		}
		if message != "" {
			messages = append(messages, message)
		}
		if len(messages) == 5 {
			break
		}
	}
	if len(messages) == 0 {
		return nil
	}
	return fmt.Errorf("solc reported errors: %s", strings.Join(messages, "\n---\n"))
}
