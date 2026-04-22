package contractmeta

import "testing"

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

func TestSolcCacheDirFor(t *testing.T) {
	got := solcCacheDirFor("/tmp/project")
	want := "/tmp/project/.inspethct/solc"
	if got != want {
		t.Fatalf("solcCacheDirFor() = %q, want %q", got, want)
	}
}
