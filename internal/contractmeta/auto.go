package contractmeta

import (
	"context"
	"errors"
	"fmt"

	"inspethct/internal/engine"
)

// AutoSourceProvider returns deployed bytecode and (optionally) storage slots for an address.
// The plugin/jsonrpc layer adapts the forkengine to satisfy this interface so contractmeta does
// not need to import forkengine directly.
type AutoSourceProvider interface {
	GetCodeAt(ctx context.Context, addr engine.Address) ([]byte, error)
	GetStorageSlot(ctx context.Context, addr engine.Address, slot engine.Hash) (engine.Hash, error)
}

// AutoLoadOptions controls the auto-detect pipeline.
type AutoLoadOptions struct {
	// Address that the call/transaction targets. Required.
	Address engine.Address
	// CodeAddress (optional) overrides where bytecode is fetched from when caller already knows
	// the implementation address (e.g. proxy resolved upstream).
	CodeAddress *engine.Address
	// ProjectRoot to scan local Foundry/Hardhat build-info for matches. Empty disables local match.
	ProjectRoot string
	// Explorer client used for verified-source fallback. May be zero-valued to disable.
	Explorer ExplorerClient
	// Solc manager for explorer compilation. Required if explorer fallback is desired.
	SolcManager *SolcManager
	// PreferredContractName, if non-empty, restricts local matching to contracts with this name.
	PreferredContractName string
}

// AutoLoadResult is the structured outcome of an auto-detect attempt.
type AutoLoadResult struct {
	Bundle      *Bundle
	Source      string // "local-bytecode" | "local-name" | "explorer"
	Match       *BytecodeMatch
	Diagnostics []string
}

// AutoLoadBundle attempts to resolve a contract source bundle for an address using:
//  1. proxy detection via storage slots (EIP-1967),
//  2. local build-info bytecode matching,
//  3. explorer verified source fallback.
//
// Returns a structured result on success or a descriptive error explaining each fallback that was
// attempted so the caller can surface guidance.
func AutoLoadBundle(ctx context.Context, provider AutoSourceProvider, opts AutoLoadOptions) (*AutoLoadResult, error) {
	if provider == nil {
		return nil, errors.New("AutoLoadBundle: provider is required")
	}
	runtime := true

	codeAddr := opts.Address
	if opts.CodeAddress != nil {
		codeAddr = *opts.CodeAddress
	}

	var (
		diagnostics []string
		proxyInfo   *ProxyInfo
	)

	// 1. detect proxy implementation (best-effort).
	proxyInfo = detectProxyViaStorage(ctx, provider, opts.Address)
	if proxyInfo != nil && proxyInfo.ImplementationAddress != nil {
		codeAddr = *proxyInfo.ImplementationAddress
		diagnostics = append(diagnostics, fmt.Sprintf("detected proxy: implementation=%s via=%v", encodeAddress(codeAddr), proxyInfo.DetectedBy))
	}

	onChainCode, err := provider.GetCodeAt(ctx, codeAddr)
	if err != nil {
		return nil, fmt.Errorf("fetch on-chain code for %s: %w", encodeAddress(codeAddr), err)
	}
	if len(onChainCode) == 0 {
		return nil, fmt.Errorf("no bytecode found at %s (EOA or self-destructed?)", encodeAddress(codeAddr))
	}

	loadOpts := LoadOptions{
		Runtime:      runtime,
		Address:      &opts.Address,
		CodeAddress:  &codeAddr,
		ContractName: opts.PreferredContractName,
	}

	// 2. local bytecode match.
	if opts.ProjectRoot != "" {
		matches, scanErr := FindLocalBytecodeMatches(opts.ProjectRoot, onChainCode)
		if scanErr != nil {
			diagnostics = append(diagnostics, fmt.Sprintf("local bytecode scan failed: %v", scanErr))
		} else if len(matches) > 0 {
			best := matches[0]
			diagnostics = append(diagnostics, fmt.Sprintf("local match %s/%s (%d/%d bytes, exact=%t)",
				best.SourceName, best.ContractName, best.MatchedBytes, best.TotalBytes, best.Exact))
			localOpts := loadOpts
			localOpts.ContractName = best.ContractName
			localOpts.SourceName = best.SourceName
			bundle, loadErr := LoadBundleFromBuildInfoMatch(best, localOpts)
			if loadErr == nil {
				if proxyInfo != nil && bundle.Proxy == nil {
					bundle.Proxy = proxyInfo
				}
				return &AutoLoadResult{
					Bundle:      bundle,
					Source:      "local-bytecode",
					Match:       &best,
					Diagnostics: diagnostics,
				}, nil
			}
			diagnostics = append(diagnostics, fmt.Sprintf("loading matched local bundle failed: %v", loadErr))
		} else {
			diagnostics = append(diagnostics, "no local bytecode match in build-info")
		}
	}

	// 3. explorer fallback.
	if opts.Explorer.APIBase != "" && opts.Explorer.APIKey != "" && opts.SolcManager != nil {
		explorerOpts := ExplorerLoadOptions{
			Address:     codeAddr,
			LoadOptions: loadOpts,
			RPCURL:      opts.Explorer.RPCURL,
		}
		bundle, explorerErr := LoadExplorerBundle(ctx, opts.Explorer, opts.SolcManager, explorerOpts)
		if explorerErr == nil {
			diagnostics = append(diagnostics, "loaded verified source from explorer")
			if proxyInfo != nil && bundle.Proxy == nil {
				bundle.Proxy = proxyInfo
			}
			return &AutoLoadResult{
				Bundle:      bundle,
				Source:      "explorer",
				Diagnostics: diagnostics,
			}, nil
		}
		diagnostics = append(diagnostics, fmt.Sprintf("explorer fallback failed: %v", explorerErr))
	} else if opts.Explorer.APIBase != "" && opts.Explorer.APIKey == "" {
		diagnostics = append(diagnostics, "explorer fallback skipped: API key not configured")
	}

	return nil, &AutoLoadError{Address: codeAddr, Diagnostics: diagnostics}
}

// AutoLoadError surfaces every fallback that was attempted so the UI can guide the user.
type AutoLoadError struct {
	Address     engine.Address
	Diagnostics []string
}

func (err *AutoLoadError) Error() string {
	if len(err.Diagnostics) == 0 {
		return fmt.Sprintf("could not resolve source for %s", encodeAddress(err.Address))
	}
	return fmt.Sprintf("could not resolve source for %s; tried: %v", encodeAddress(err.Address), err.Diagnostics)
}

// detectProxyViaStorage probes EIP-1967 implementation/beacon slots to find the implementation
// address without requiring an explorer roundtrip.
func detectProxyViaStorage(ctx context.Context, provider AutoSourceProvider, addr engine.Address) *ProxyInfo {
	implSlot, err := provider.GetStorageSlot(ctx, addr, eip1967ImplementationSlot)
	if err == nil {
		if implAddr, ok := nonZeroAddressFromHash(implSlot); ok {
			info := &ProxyInfo{ProxyAddress: cloneAddressPtr(&addr), ImplementationAddress: cloneAddressPtr(&implAddr), DetectedBy: []string{"eip1967-implementation"}}
			return info
		}
	}
	beaconSlotValue, err := provider.GetStorageSlot(ctx, addr, eip1967BeaconSlot)
	if err == nil {
		if beaconAddr, ok := nonZeroAddressFromHash(beaconSlotValue); ok {
			info := &ProxyInfo{ProxyAddress: cloneAddressPtr(&addr), BeaconAddress: cloneAddressPtr(&beaconAddr), DetectedBy: []string{"eip1967-beacon"}}
			return info
		}
	}
	return nil
}

func nonZeroAddressFromHash(value engine.Hash) (engine.Address, bool) {
	var addr engine.Address
	copy(addr[:], value[12:])
	for _, b := range addr {
		if b != 0 {
			return addr, true
		}
	}
	return engine.Address{}, false
}
