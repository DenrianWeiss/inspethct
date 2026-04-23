package contractmeta

import (
	"context"
	"fmt"

	"inspethct/internal/engine"
)

// AutoMatchConfig holds per-session configuration for call-stack contract auto-matching.
type AutoMatchConfig struct {
	ProjectRoot     string
	CacheDir        string            // usually CacheDir(ProjectRoot); empty disables disk cache
	Mapping         DeploymentMapping // loaded from deployment-mapping.json
	Explorer        ExplorerClient    // zero value disables explorer
	SolcManager     *SolcManager      // nil disables explorer compilation
	ExplorerEnabled bool              // master switch; false disables explorer even if config is set
}

// AutoMatchBundle resolves a bundle for a call-stack contract address.
//
// Pipeline (short-circuits on first success):
//  1. deployment-mapping.json exact address match → load local build-info
//  2. disk cache lookup by bytecode hash
//  3. local build-info bytecode scan
//  4. explorer fetch (only when ExplorerEnabled AND (no mapping entry OR entry.Pull==true))
//
// Successfully loaded bundles are written to the disk cache.
// Failed explorer lookups are recorded as negative entries to prevent retries.
func AutoMatchBundle(ctx context.Context, provider AutoSourceProvider, addr engine.Address, cfg AutoMatchConfig) (*AutoLoadResult, error) {
	// Step 0: proxy detection (best-effort; errors are silently ignored).
	var (
		proxyInfo *ProxyInfo
		codeAddr  = addr
	)
	proxyInfo = detectProxyViaStorage(ctx, provider, addr)
	if proxyInfo != nil && proxyInfo.ImplementationAddress != nil {
		codeAddr = *proxyInfo.ImplementationAddress
	}

	// Fetch on-chain bytecode (usually from forkengine cache → very fast).
	code, err := provider.GetCodeAt(ctx, codeAddr)
	if err != nil || len(code) == 0 {
		return nil, fmt.Errorf("no bytecode at %s", encodeAddress(codeAddr))
	}

	codeHash := BytecodeHash(code)
	baseOpts := LoadOptions{Runtime: true, Address: &addr, CodeAddress: &codeAddr}

	// Step 1: deployment-mapping.json — exact address lookup.
	mappingEntry, hasMappingEntry := cfg.Mapping[addr]
	if hasMappingEntry && mappingEntry.Contract != "" {
		sourceName, contractName := splitContractPath(mappingEntry.Contract)
		localOpts := baseOpts
		localOpts.SourceName = sourceName
		localOpts.ContractName = contractName
		bundle, loadErr := LoadLocalProjectBundle(cfg.ProjectRoot, localOpts)
		if loadErr == nil {
			applyProxy(bundle, proxyInfo)
			if cfg.CacheDir != "" {
				SaveCachedBundle(cfg.CacheDir, codeHash, bundle, "local-mapping")
			}
			return &AutoLoadResult{Bundle: bundle, Source: "local-mapping"}, nil
		}
	}

	// Step 2: disk cache by bytecode hash.
	if cfg.CacheDir != "" {
		if bundle, source, ok := LoadCachedBundle(cfg.CacheDir, codeHash); ok {
			applyProxy(bundle, proxyInfo)
			return &AutoLoadResult{Bundle: bundle, Source: source + " (cached)"}, nil
		}
		// Negative cache: skip explorer unless mapping explicitly requests pull.
		if IsNotVerifiedHash(cfg.CacheDir, codeHash) {
			if !hasMappingEntry || !mappingEntry.Pull || !cfg.ExplorerEnabled {
				return nil, fmt.Errorf("previously not verified: %s", encodeAddress(addr))
			}
		}
	}

	// Step 3: local bytecode scan (fast for small projects; cached after first run).
	if cfg.ProjectRoot != "" {
		matches, _ := FindLocalBytecodeMatches(cfg.ProjectRoot, code)
		if len(matches) > 0 {
			best := matches[0]
			ratio := 0.0
			if best.TotalBytes > 0 {
				ratio = float64(best.MatchedBytes) / float64(best.TotalBytes)
			}
			localOpts := baseOpts
			localOpts.ContractName = best.ContractName
			localOpts.SourceName = best.SourceName
			bundle, loadErr := LoadBundleFromBuildInfoMatch(best, localOpts)
			if loadErr == nil {
				applyProxy(bundle, proxyInfo)
				diagnostics := []string{
					fmt.Sprintf("local match %s/%s (%d/%d bytes, exact=%t, confidence=%.3f)",
						best.SourceName, best.ContractName, best.MatchedBytes, best.TotalBytes, best.Exact, ratio),
				}
				if !best.Exact && ratio < 0.80 {
					diagnostics = append(diagnostics,
						fmt.Sprintf("warning: local bytecode match confidence may be low (%.3f < 0.800); using local match to avoid explorer fallback", ratio))
				}
				if cfg.CacheDir != "" {
					SaveCachedBundle(cfg.CacheDir, codeHash, bundle, "local-bytecode")
				}
				return &AutoLoadResult{Bundle: bundle, Source: "local-bytecode", Match: &best, Diagnostics: diagnostics}, nil
			}
		}
	}

	// Step 4: explorer fetch.
	// Only run if: ExplorerEnabled AND (no mapping entry, OR entry has Pull=true).
	explorerAllowed := cfg.ExplorerEnabled &&
		cfg.Explorer.APIBase != "" && cfg.Explorer.APIKey != "" && cfg.SolcManager != nil &&
		(!hasMappingEntry || mappingEntry.Pull)
	if explorerAllowed {
		explorerOpts := ExplorerLoadOptions{Address: codeAddr, LoadOptions: baseOpts, RPCURL: cfg.Explorer.RPCURL}
		bundle, explorerErr := LoadExplorerBundle(ctx, cfg.Explorer, cfg.SolcManager, explorerOpts)
		if explorerErr == nil {
			applyProxy(bundle, proxyInfo)
			if cfg.CacheDir != "" {
				SaveCachedBundle(cfg.CacheDir, codeHash, bundle, "explorer")
			}
			return &AutoLoadResult{Bundle: bundle, Source: "explorer"}, nil
		}
		// Record negative entry so the next run skips the HTTP call.
		if cfg.CacheDir != "" {
			MarkNotVerifiedHash(cfg.CacheDir, codeHash)
		}
	}

	return nil, &AutoLoadError{Address: addr}
}

func applyProxy(bundle *Bundle, proxy *ProxyInfo) {
	if bundle != nil && proxy != nil && bundle.Proxy == nil {
		bundle.Proxy = proxy
	}
}
