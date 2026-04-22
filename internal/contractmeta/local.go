package contractmeta

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"inspethct/internal/srcmap"
)

type buildInfoDocument struct {
	ID          string `json:"id"`
	SolcVersion string `json:"solcVersion"`
	Input       struct {
		Language string `json:"language"`
		Sources  map[string]struct {
			Content string `json:"content"`
		} `json:"sources"`
	} `json:"input"`
	Output struct {
		Sources map[string]struct {
			ID  int            `json:"id"`
			AST map[string]any `json:"ast"`
		} `json:"sources"`
		Contracts map[string]map[string]json.RawMessage `json:"contracts"`
	} `json:"output"`
}

type standardJSONDocument struct {
	Sources   map[string]map[string]any             `json:"sources"`
	Contracts map[string]map[string]json.RawMessage `json:"contracts"`
}

// LoadLocalProjectBundle loads a contract bundle from Foundry or Hardhat build-info output.
func LoadLocalProjectBundle(projectRoot string, opts LoadOptions) (*Bundle, error) {
	if err := requireContractName(opts); err != nil {
		return nil, err
	}
	paths, err := findBuildInfoFiles(projectRoot)
	if err != nil {
		return nil, err
	}
	for _, path := range paths {
		bundle, err := loadBundleFromBuildInfo(path, opts)
		if err == nil {
			return bundle, nil
		}
		if !isContractMissingError(err) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("contract %q not found in build-info under %s", opts.ContractName, projectRoot)
}

func findBuildInfoFiles(projectRoot string) ([]string, error) {
	patterns := []string{
		filepath.Join(projectRoot, "out", "build-info", "*.json"),
		filepath.Join(projectRoot, "artifacts", "build-info", "*.json"),
	}
	var paths []string
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		paths = append(paths, matches...)
	}
	sort.Strings(paths)
	return paths, nil
}

func loadBundleFromBuildInfo(path string, opts LoadOptions) (*Bundle, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc buildInfoDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("decode build-info %s: %w", path, err)
	}
	sourceName, err := selectContractSource(doc.Output.Contracts, opts)
	if err != nil {
		return nil, err
	}
	if opts.SourceName == "" {
		opts.SourceName = sourceName
	}
	standardJSON, err := marshalStandardJSON(doc, opts.SourceName)
	if err != nil {
		return nil, err
	}
	return buildBundle(standardJSON, nil, doc.SolcVersion, opts, nil)
}

func marshalStandardJSON(doc buildInfoDocument, sourceName string) ([]byte, error) {
	result := standardJSONDocument{
		Sources:   make(map[string]map[string]any, len(doc.Output.Sources)),
		Contracts: doc.Output.Contracts,
	}
	for name, outputSource := range doc.Output.Sources {
		entry := map[string]any{"id": outputSource.ID, "ast": outputSource.AST}
		if inputSource, ok := doc.Input.Sources[name]; ok {
			entry["content"] = inputSource.Content
		}
		result.Sources[name] = entry
	}
	if _, ok := result.Contracts[sourceName]; !ok {
		return nil, missingContractError{Message: fmt.Sprintf("source %q not present in reconstructed build-info", sourceName)}
	}
	return json.Marshal(result)
}

func selectContractSource(contracts map[string]map[string]json.RawMessage, opts LoadOptions) (string, error) {
	if opts.SourceName != "" {
		if sourceContracts, ok := contracts[opts.SourceName]; ok {
			if _, ok := sourceContracts[opts.ContractName]; ok {
				return opts.SourceName, nil
			}
		}
		return "", missingContractError{Message: fmt.Sprintf("contract %q not found under source %q", opts.ContractName, opts.SourceName)}
	}
	for sourceName, sourceContracts := range contracts {
		if _, ok := sourceContracts[opts.ContractName]; ok {
			return sourceName, nil
		}
	}
	return "", missingContractError{Message: fmt.Sprintf("contract %q not found", opts.ContractName)}
}

type missingContractError struct {
	Message string
}

func (err missingContractError) Error() string { return err.Message }

func isContractMissingError(err error) bool {
	_, ok := err.(missingContractError)
	return ok
}

// LoadStandardJSONFile loads a bundle directly from a saved Solidity standard-json output file.
func LoadStandardJSONFile(path string, opts LoadOptions) (*Bundle, error) {
	if err := requireContractName(opts); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if opts.SourceName == "" {
		var doc standardJSONDocument
		if err := json.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("decode standard-json %s: %w", path, err)
		}
		sourceName, err := selectContractSource(doc.Contracts, opts)
		if err != nil {
			return nil, err
		}
		opts.SourceName = sourceName
	}
	return buildBundle(data, nil, "", opts, nil)
}

// InspectStandardJSON extracts metadata from a standard-json file without loading debugger context.
func InspectStandardJSON(path string, cfg srcmap.BuildConfig) (srcmap.ContractMetadata, error) {
	index, err := srcmap.BuildIndexFromStandardJSON(mustReadFile(path), cfg)
	if err != nil {
		return srcmap.ContractMetadata{}, err
	}
	return index.Metadata(), nil
}

func mustReadFile(path string) []byte {
	data, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	return data
}
