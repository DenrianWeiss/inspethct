package contractmeta

import (
	"fmt"
	"os"
)

// LoadManualBundle creates a bundle from manually supplied ABI and optional standard-json output.
func LoadManualBundle(standardJSONPath string, abiPath string, opts LoadOptions) (*Bundle, error) {
	if err := requireContractName(opts); err != nil {
		return nil, err
	}
	var standardJSON []byte
	var abiJSON []byte
	var err error
	if standardJSONPath != "" {
		standardJSON, err = os.ReadFile(standardJSONPath)
		if err != nil {
			return nil, fmt.Errorf("read standard-json: %w", err)
		}
	}
	if abiPath != "" {
		abiJSON, err = os.ReadFile(abiPath)
		if err != nil {
			return nil, fmt.Errorf("read abi: %w", err)
		}
	}
	if len(standardJSON) == 0 && len(abiJSON) == 0 {
		return nil, fmt.Errorf("manual bundle requires at least one of standard-json or abi")
	}
	return buildBundle(standardJSON, abiJSON, "", opts, nil)
}
