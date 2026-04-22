package contractmeta

import (
	"archive/zip"
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

// NewSolcManager constructs a SolcManager using the global user-level cache.
// The cache location is (first match wins):
//  1. INSPETHCT_SOLC_CACHE environment variable
//  2. OS user cache: ~/.cache/inspethct/solc (Linux)
//     ~/Library/Caches/inspethct/solc (macOS)
//     %LocalAppData%\inspethct\solc (Windows)
//
// Binaries are stored under <cacheDir>/<platform>/<filename> so multiple
// target platforms can coexist without conflicts.
func NewSolcManager() (*SolcManager, error) {
	cacheDir, err := globalSolcCacheDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, err
	}
	return &SolcManager{CacheDir: cacheDir, BaseURL: solcBinariesBaseURL, Client: http.DefaultClient}, nil
}

// globalSolcCacheDir returns the user-level cache directory for solc binaries.
// It honours INSPETHCT_SOLC_CACHE for explicit overrides (CI / Docker).
func globalSolcCacheDir() (string, error) {
	if override := os.Getenv("INSPETHCT_SOLC_CACHE"); strings.TrimSpace(override) != "" {
		return override, nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine user cache directory: %w", err)
	}
	return filepath.Join(base, "inspethct", "solc"), nil
}

// EnsureVersion downloads the requested solc version for the current runtime
// platform into the global cache if not already present.
// Returns the path to the cached binary.
func (manager *SolcManager) EnsureVersion(ctx context.Context, compilerVersion string) (string, error) {
	return manager.EnsureVersionForPlatform(ctx, compilerVersion, runtime.GOOS, runtime.GOARCH)
}

// EnsureVersionForPlatform downloads the requested solc version for the
// specified GOOS/GOARCH target into the global cache if not already present.
// This allows pre-populating the cache for platforms other than the running
// host (useful in CI release pipelines that need binaries for multiple OSes).
// Returns the path to the cached binary.
func (manager *SolcManager) EnsureVersionForPlatform(ctx context.Context, compilerVersion, goos, goarch string) (string, error) {
	if manager == nil {
		return "", fmt.Errorf("solc manager is nil")
	}
	platform := platformDirectoryFor(goos, goarch)
	build, err := manager.lookupBuildForPlatform(ctx, compilerVersion, platform)
	if err != nil {
		return "", err
	}

	// Windows binaries ≥ 0.7.2 are .exe; older builds are .zip archives
	// containing a single .exe.  Normalise to the executable path so the
	// cache key is always the final binary, not the archive.
	// Store at <cacheDir>/<platform>/<filename>
	binaryPath := filepath.Join(manager.CacheDir, platform, solcBinaryName(build.Path))
	if _, statErr := os.Stat(binaryPath); statErr == nil {
		return binaryPath, nil // already cached
	}
	if err := os.MkdirAll(filepath.Dir(binaryPath), 0o755); err != nil {
		return "", err
	}

	url := strings.TrimRight(manager.BaseURL, "/") + "/" + platform + "/" + build.Path
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
		return "", fmt.Errorf("download solc %s for %s/%s: unexpected status %s",
			compilerVersion, goos, goarch, response.Status)
	}

	// Write to a temp file first to avoid leaving a partial binary if interrupted.
	tmp, err := os.CreateTemp(filepath.Dir(binaryPath), ".solc-download-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	defer func() {
		tmp.Close()
		_ = os.Remove(tmpName) // no-op if rename succeeded
	}()
	if _, err := io.Copy(tmp, response.Body); err != nil {
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}

	// For zip archives (older Windows builds), extract the inner executable.
	readyName := tmpName
	if strings.HasSuffix(build.Path, ".zip") {
		extracted, err := extractSolcZip(tmpName, filepath.Dir(binaryPath))
		if err != nil {
			return "", fmt.Errorf("extract solc zip for %s: %w", build.Path, err)
		}
		_ = os.Remove(tmpName)
		readyName = extracted
	}
	if err := os.Chmod(readyName, 0o755); err != nil {
		_ = os.Remove(readyName)
		return "", err
	}
	if err := os.Rename(readyName, binaryPath); err != nil {
		_ = os.Remove(readyName)
		return "", err
	}
	return binaryPath, nil
}

// solcBinaryName returns the canonical on-disk name for a solc build path.
// For zip archives (older Windows builds) the stored name uses .exe instead
// of .zip so the cache always points to a directly executable file.
func solcBinaryName(buildPath string) string {
	if strings.HasSuffix(buildPath, ".zip") {
		return buildPath[:len(buildPath)-4] + ".exe"
	}
	return buildPath
}

// extractSolcZip extracts the first non-directory entry from the zip file at
// srcPath into a new temp file in dir and returns the temp file path.
func extractSolcZip(srcPath, dir string) (string, error) {
	zr, err := zip.OpenReader(srcPath)
	if err != nil {
		return "", err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		out, err := os.CreateTemp(dir, ".solc-extract-*")
		if err != nil {
			rc.Close()
			return "", err
		}
		_, copyErr := io.Copy(out, rc)
		rc.Close()
		out.Close()
		if copyErr != nil {
			_ = os.Remove(out.Name())
			return "", copyErr
		}
		return out.Name(), nil
	}
	return "", fmt.Errorf("no file entry found in zip")
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

func (manager *SolcManager) lookupBuildForPlatform(ctx context.Context, compilerVersion, platform string) (*solcBuild, error) {
	url := strings.TrimRight(manager.BaseURL, "/") + "/" + platform + "/list.json"
	request, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	response, err := manager.httpClient().Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("load solc version list for %s: unexpected status %s", platform, response.Status)
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
		if normalizeCompilerVersion(build.LongVersion).Full == normalized.Full ||
			normalizeCompilerVersion(build.Version).Short == normalized.Short {
			return &build, nil
		}
	}
	return nil, fmt.Errorf("solc version %q is not available for %s", compilerVersion, platform)
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

// CachedPlatforms returns the list of platform directories that have at least
// one solc binary already cached under CacheDir.
func (manager *SolcManager) CachedPlatforms() ([]string, error) {
	entries, err := os.ReadDir(manager.CacheDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	platforms := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			platforms = append(platforms, entry.Name())
		}
	}
	return platforms, nil
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
