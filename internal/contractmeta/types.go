package contractmeta

import (
	"encoding/json"
	"fmt"

	"inspethct/internal/engine"
	"inspethct/internal/srcmap"
)

// LoadOptions selects a contract artifact and whether runtime source maps should be used.
type LoadOptions struct {
	SourceName   string
	ContractName string
	Runtime      bool
	Address      *engine.Address
	CodeAddress  *engine.Address
}

// ProxyInfo records proxy detection details attached to a source bundle.
type ProxyInfo struct {
	ProxyAddress          *engine.Address `json:"proxyAddress,omitempty"`
	ImplementationAddress *engine.Address `json:"implementationAddress,omitempty"`
	BeaconAddress         *engine.Address `json:"beaconAddress,omitempty"`
	DetectedBy            []string        `json:"detectedBy,omitempty"`
}

// Bundle is the normalized contract metadata consumed by the debugger and CLI.
type Bundle struct {
	SourceName      string                  `json:"sourceName"`
	ContractName    string                  `json:"contractName"`
	CompilerVersion string                  `json:"compilerVersion,omitempty"`
	Address         *engine.Address         `json:"address,omitempty"`
	CodeAddress     *engine.Address         `json:"codeAddress,omitempty"`
	StandardJSON    json.RawMessage         `json:"standardJson,omitempty"`
	ABIJSON         json.RawMessage         `json:"abiJson,omitempty"`
	Metadata        srcmap.ContractMetadata `json:"metadata"`
	Proxy           *ProxyInfo              `json:"proxy,omitempty"`
	Index           *srcmap.Index           `json:"-"`
}

func buildBundle(standardJSON []byte, abiJSON []byte, compilerVersion string, opts LoadOptions, proxy *ProxyInfo) (*Bundle, error) {
	if len(standardJSON) != 0 {
		resolvedSourceName, err := resolveSourceNameFromCompiledOutput(standardJSON, opts.SourceName, opts.ContractName)
		if err != nil {
			return nil, err
		}
		opts.SourceName = resolvedSourceName
	}
	bundle := &Bundle{
		SourceName:      opts.SourceName,
		ContractName:    opts.ContractName,
		CompilerVersion: compilerVersion,
		Address:         cloneAddressPtr(opts.Address),
		CodeAddress:     cloneAddressPtr(opts.CodeAddress),
		StandardJSON:    append(json.RawMessage(nil), standardJSON...),
		ABIJSON:         append(json.RawMessage(nil), abiJSON...),
		Proxy:           proxy,
	}
	if len(standardJSON) == 0 {
		if len(bundle.ABIJSON) == 0 {
			bundle.ABIJSON = []byte("[]")
		}
		return bundle, nil
	}
	index, err := srcmap.BuildIndexFromStandardJSON(standardJSON, srcmap.BuildConfig{
		SourceName:   opts.SourceName,
		ContractName: opts.ContractName,
		Runtime:      opts.Runtime,
	})
	if err != nil {
		return nil, err
	}
	bundle.Index = index
	bundle.Metadata = index.Metadata()
	if len(bundle.ABIJSON) == 0 {
		bundle.ABIJSON, err = index.ABIJSON()
		if err != nil {
			return nil, err
		}
	}
	return bundle, nil
}

func cloneAddressPtr(addr *engine.Address) *engine.Address {
	if addr == nil {
		return nil
	}
	clone := *addr
	return &clone
}

func requireContractName(opts LoadOptions) error {
	if opts.ContractName == "" {
		return fmt.Errorf("contract name is required")
	}
	return nil
}

func resolveSourceNameFromCompiledOutput(standardJSON []byte, preferredSourceName string, contractName string) (string, error) {
	if contractName == "" {
		return preferredSourceName, nil
	}
	var doc struct {
		Contracts map[string]map[string]json.RawMessage `json:"contracts"`
	}
	if err := json.Unmarshal(standardJSON, &doc); err != nil {
		return "", fmt.Errorf("decode compiled standard-json contracts: %w", err)
	}
	if preferredSourceName != "" {
		if contractsBySource, ok := doc.Contracts[preferredSourceName]; ok {
			if _, ok := contractsBySource[contractName]; ok {
				return preferredSourceName, nil
			}
		}
	}
	for sourceName, contractsBySource := range doc.Contracts {
		if _, ok := contractsBySource[contractName]; ok {
			return sourceName, nil
		}
	}
	if preferredSourceName != "" {
		return "", fmt.Errorf("contract %q not found under source %q", contractName, preferredSourceName)
	}
	return "", fmt.Errorf("contract %q not found in compiled standard-json output", contractName)
}
