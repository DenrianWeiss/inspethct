package contractmeta

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlatformDirectoryFor(t *testing.T) {
	tests := []struct {
		goos   string
		goarch string
		want   string
	}{
		{goos: "darwin", goarch: "amd64", want: "macosx-amd64"},
		{goos: "darwin", goarch: "arm64", want: "macosx-amd64"},
		{goos: "linux", goarch: "amd64", want: "linux-amd64"},
		{goos: "linux", goarch: "arm64", want: "linux-arm64"},
		{goos: "windows", goarch: "amd64", want: "windows-amd64"},
	}
	for _, test := range tests {
		if got := platformDirectoryFor(test.goos, test.goarch); got != test.want {
			t.Fatalf("platformDirectoryFor(%q, %q) = %q, want %q", test.goos, test.goarch, got, test.want)
		}
	}
}

func TestGlobalSolcCacheDir_EnvOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("INSPETHCT_SOLC_CACHE", dir)
	got, err := globalSolcCacheDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != dir {
		t.Fatalf("globalSolcCacheDir() = %q, want %q", got, dir)
	}
}

func TestGlobalSolcCacheDir_Default(t *testing.T) {
	t.Setenv("INSPETHCT_SOLC_CACHE", "") // ensure env var is cleared
	got, err := globalSolcCacheDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "inspethct") {
		t.Fatalf("globalSolcCacheDir() = %q, expected path to contain 'inspethct'", got)
	}
	if !strings.HasSuffix(got, filepath.Join("inspethct", "solc")) {
		t.Fatalf("globalSolcCacheDir() = %q, expected suffix 'inspethct/solc'", got)
	}
}

// TestEnsureVersionForPlatform verifies that EnsureVersionForPlatform stores
// binaries under <cacheDir>/<platform>/<filename> and re-uses the cache on a
// second call without hitting the network again.
func TestEnsureVersionForPlatform(t *testing.T) {
	const fakeContent = "#!/bin/sh\necho fake solc"
	const platform = "linux-amd64"
	const binaryName = "solc-linux-amd64-v0.8.24+commit.e11b9ed9"

	// Build a minimal list.json + binary endpoint.
	mux := http.NewServeMux()
	downloadCount := 0
	mux.HandleFunc("/"+platform+"/list.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{
			"builds": [{"path":"`+binaryName+`","version":"0.8.24","longVersion":"0.8.24+commit.e11b9ed9","sha256":""}],
			"releases": {"0.8.24":"`+binaryName+`"}
		}`)
	})
	mux.HandleFunc("/"+platform+"/"+binaryName, func(w http.ResponseWriter, _ *http.Request) {
		downloadCount++
		_, _ = io.WriteString(w, fakeContent)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cacheDir := t.TempDir()
	manager := &SolcManager{CacheDir: cacheDir, BaseURL: srv.URL, Client: srv.Client()}

	// First call — should download.
	path, err := manager.EnsureVersionForPlatform(context.Background(), "0.8.24", "linux", "amd64")
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	wantPath := filepath.Join(cacheDir, platform, binaryName)
	if path != wantPath {
		t.Fatalf("path = %q, want %q", path, wantPath)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != fakeContent {
		t.Fatalf("binary content mismatch: err=%v, got=%q", err, data)
	}
	if downloadCount != 1 {
		t.Fatalf("expected 1 download, got %d", downloadCount)
	}

	// Second call — should hit cache, no further download.
	_, err = manager.EnsureVersionForPlatform(context.Background(), "0.8.24", "linux", "amd64")
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if downloadCount != 1 {
		t.Fatalf("expected still 1 download after cache hit, got %d", downloadCount)
	}
}

// TestEnsureVersionForPlatform_MultiPlatform verifies that different platforms
// are cached in separate subdirectories and do not overwrite each other.
func TestEnsureVersionForPlatform_MultiPlatform(t *testing.T) {
	const binaryLinux = "solc-linux-amd64-v0.8.24+commit.e11b9ed9"
	const binaryMac = "solc-macosx-amd64-v0.8.24+commit.e11b9ed9"

	mux := http.NewServeMux()
	mux.HandleFunc("/linux-amd64/list.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"builds":[{"path":"`+binaryLinux+`","version":"0.8.24","longVersion":"0.8.24+commit.e11b9ed9","sha256":""}],"releases":{"0.8.24":"`+binaryLinux+`"}}`)
	})
	mux.HandleFunc("/linux-amd64/"+binaryLinux, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "linux-binary")
	})
	mux.HandleFunc("/macosx-amd64/list.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"builds":[{"path":"`+binaryMac+`","version":"0.8.24","longVersion":"0.8.24+commit.e11b9ed9","sha256":""}],"releases":{"0.8.24":"`+binaryMac+`"}}`)
	})
	mux.HandleFunc("/macosx-amd64/"+binaryMac, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "macos-binary")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cacheDir := t.TempDir()
	manager := &SolcManager{CacheDir: cacheDir, BaseURL: srv.URL, Client: srv.Client()}

	linuxPath, err := manager.EnsureVersionForPlatform(context.Background(), "0.8.24", "linux", "amd64")
	if err != nil {
		t.Fatalf("linux: %v", err)
	}
	macPath, err := manager.EnsureVersionForPlatform(context.Background(), "0.8.24", "darwin", "amd64")
	if err != nil {
		t.Fatalf("macos: %v", err)
	}

	if linuxPath == macPath {
		t.Fatalf("linux and macos binaries should be stored at different paths")
	}
	linuxData, _ := os.ReadFile(linuxPath)
	macData, _ := os.ReadFile(macPath)
	if string(linuxData) != "linux-binary" {
		t.Fatalf("linux binary content = %q", linuxData)
	}
	if string(macData) != "macos-binary" {
		t.Fatalf("macos binary content = %q", macData)
	}
}

// TestSolcBinaryName verifies that zip paths are normalised to .exe.
func TestSolcBinaryName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"solc-windows-amd64-v0.7.1+commit.f4a555be.zip", "solc-windows-amd64-v0.7.1+commit.f4a555be.exe"},
		{"solc-windows-amd64-v0.8.24+commit.e11b9ed9.exe", "solc-windows-amd64-v0.8.24+commit.e11b9ed9.exe"},
		{"solc-linux-amd64-v0.8.24+commit.e11b9ed9", "solc-linux-amd64-v0.8.24+commit.e11b9ed9"},
	}
	for _, tc := range tests {
		if got := solcBinaryName(tc.in); got != tc.want {
			t.Errorf("solcBinaryName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestEnsureVersionForPlatform_WindowsZip tests that old-style Windows .zip
// archives are extracted so the cached file is a directly executable .exe.
func TestEnsureVersionForPlatform_WindowsZip(t *testing.T) {
	const platform = "windows-amd64"
	const zipName = "solc-windows-amd64-v0.7.1+commit.f4a555be.zip"
	const exeName = "solc-windows-amd64-v0.7.1+commit.f4a555be.exe"
	const innerContent = "fake solc exe inside zip"

	// Build a minimal zip payload in memory.
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	w, err := zw.Create("solc.exe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(w, innerContent); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zipBytes := zipBuf.Bytes()

	mux := http.NewServeMux()
	mux.HandleFunc("/"+platform+"/list.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{
			"builds":[{"path":"`+zipName+`","version":"0.7.1","longVersion":"0.7.1+commit.f4a555be","sha256":""}],
			"releases":{"0.7.1":"`+zipName+`"}
		}`)
	})
	mux.HandleFunc("/"+platform+"/"+zipName, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(zipBytes)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cacheDir := t.TempDir()
	manager := &SolcManager{CacheDir: cacheDir, BaseURL: srv.URL, Client: srv.Client()}

	path, err := manager.EnsureVersionForPlatform(context.Background(), "0.7.1", "windows", "amd64")
	if err != nil {
		t.Fatalf("EnsureVersionForPlatform: %v", err)
	}

	// Path must point to the .exe, not the .zip.
	if !strings.HasSuffix(path, ".exe") {
		t.Fatalf("expected .exe path, got %q", path)
	}
	if filepath.Base(path) != exeName {
		t.Fatalf("expected filename %q, got %q", exeName, filepath.Base(path))
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read cached binary: %v", err)
	}
	if string(data) != innerContent {
		t.Fatalf("binary content = %q, want %q", data, innerContent)
	}

	// .zip artifact must NOT remain in cache dir.
	zipPath := filepath.Join(cacheDir, platform, zipName)
	if _, err := os.Stat(zipPath); err == nil {
		t.Fatalf("zip artifact %q should have been removed", zipPath)
	}
}
