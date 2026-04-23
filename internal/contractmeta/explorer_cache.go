package contractmeta

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"

	"inspethct/internal/srcmap"
)

// CacheDir returns the path to the on-disk contract cache directory.
func CacheDir(projectRoot string) string {
	return filepath.Join(projectRoot, ".inspethct", "cache")
}

// BytecodeHash returns the hex-encoded SHA-256 of the normalized bytecode,
// used as the stable cache key. Proxy contracts should pass the implementation
// bytecode so that multiple proxy addresses sharing one impl share the cache entry.
func BytecodeHash(code []byte) string {
	h := sha256.Sum256(normalizeBytecode(code))
	return hex.EncodeToString(h[:])
}

type cacheRecord struct {
	Source   string          `json:"source"`
	Verified bool            `json:"verified"` // false = explorer returned nothing
	Bundle   json.RawMessage `json:"bundle,omitempty"`
}

// LoadCachedBundle loads a bundle from the disk cache by bytecode hash.
// Returns (bundle, source, true) on a verified hit, (nil, "", false) on any miss.
func LoadCachedBundle(cacheDir, hash string) (*Bundle, string, bool) {
	data, err := os.ReadFile(filepath.Join(cacheDir, hash+".json"))
	if err != nil {
		return nil, "", false
	}
	var rec cacheRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, "", false
	}
	if !rec.Verified || len(rec.Bundle) == 0 {
		return nil, "", false
	}
	var bundle Bundle
	if err := json.Unmarshal(rec.Bundle, &bundle); err != nil {
		return nil, "", false
	}
	// Rebuild Index from StandardJSON (Index is not marshalled).
	if len(bundle.StandardJSON) > 0 {
		index, err := srcmap.BuildIndexFromStandardJSON(bundle.StandardJSON, srcmap.BuildConfig{
			SourceName:   bundle.SourceName,
			ContractName: bundle.ContractName,
			Runtime:      true,
		})
		if err == nil {
			bundle.Index = index
			bundle.Metadata = index.Metadata()
		}
	}
	return &bundle, rec.Source, true
}

// IsNotVerifiedHash returns true if this hash has a negative cache entry (explorer
// previously returned nothing), signalling that a repeat explorer call should be skipped
// unless explicitly forced (e.g. pull=true in the mapping).
func IsNotVerifiedHash(cacheDir, hash string) bool {
	data, err := os.ReadFile(filepath.Join(cacheDir, hash+".json"))
	if err != nil {
		return false
	}
	var rec cacheRecord
	return json.Unmarshal(data, &rec) == nil && !rec.Verified
}

// SaveCachedBundle writes a bundle to the disk cache under the given bytecode hash.
// Errors are silently swallowed because caching is best-effort.
func SaveCachedBundle(cacheDir, hash string, bundle *Bundle, source string) {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return
	}
	bundleJSON, err := json.Marshal(bundle)
	if err != nil {
		return
	}
	rec := cacheRecord{Source: source, Verified: true, Bundle: bundleJSON}
	data, err := json.Marshal(rec)
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(cacheDir, hash+".json"), data, 0o644)
}

// MarkNotVerifiedHash writes a negative cache entry to skip future explorer retries.
// Errors are silently swallowed because caching is best-effort.
func MarkNotVerifiedHash(cacheDir, hash string) {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return
	}
	data, _ := json.Marshal(cacheRecord{Verified: false})
	_ = os.WriteFile(filepath.Join(cacheDir, hash+".json"), data, 0o644)
}
