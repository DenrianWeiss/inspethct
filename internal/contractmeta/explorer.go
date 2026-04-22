package contractmeta

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"

	"inspethct/internal/engine"
)

var (
	eip1967ImplementationSlot = mustHash("360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc")
	eip1967BeaconSlot         = mustHash("a3f0ad74e5423aebfd80d3ef4346578335a9a72aeaee59ff6cb3582b35133d50")
	proxiableSlot             = mustHash("c5f16f0fcc639fa48a6947836d9850f504798523bf8c9a3a87d5876b9f9cf622")
)

type ExplorerClient struct {
	APIBase string
	APIKey  string
	Client  *http.Client
	RPCURL  string
	ChainID string
}

type ExplorerLoadOptions struct {
	Address engine.Address
	LoadOptions
	RPCURL string
}

type explorerResponse struct {
	Status  string          `json:"status"`
	Message string          `json:"message"`
	Result  json.RawMessage `json:"result"`
}

type explorerResult struct {
	SourceCode           string `json:"SourceCode"`
	ABI                  string `json:"ABI"`
	ContractName         string `json:"ContractName"`
	CompilerVersion      string `json:"CompilerVersion"`
	OptimizationUsed     string `json:"OptimizationUsed"`
	Runs                 string `json:"Runs"`
	EVMVersion           string `json:"EVMVersion"`
	Proxy                string `json:"Proxy"`
	Implementation       string `json:"Implementation"`
	SwarmSource          string `json:"SwarmSource"`
	ConstructorArguments string `json:"ConstructorArguments"`
	Library              string `json:"Library"`
	ContractFileName     string `json:"ContractFileName"`
	FileName             string `json:"FileName"`
}

type standardInput struct {
	Language string                         `json:"language"`
	Sources  map[string]standardInputSource `json:"sources"`
	Settings map[string]any                 `json:"settings,omitempty"`
}

type standardInputSource struct {
	Content string `json:"content,omitempty"`
}

// LoadExplorerBundle fetches verified source from an Etherscan-compatible API, compiles it with solc, and returns a normalized bundle.
func LoadExplorerBundle(ctx context.Context, client ExplorerClient, manager *SolcManager, opts ExplorerLoadOptions) (*Bundle, error) {
	if err := requireContractName(LoadOptions{ContractName: opts.ContractName}); err != nil && opts.ContractName != "" {
		return nil, err
	}
	result, err := client.fetchSource(ctx, opts.Address)
	if err != nil {
		return nil, err
	}
	if opts.ContractName == "" {
		opts.ContractName = result.ContractName
	}
	input, sourceName, err := buildExplorerStandardInput(result, opts)
	if err != nil {
		return nil, err
	}
	if opts.SourceName == "" {
		opts.SourceName = sourceName
	}
	inputBytes, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	standardJSON, err := manager.CompileStandardInput(ctx, result.CompilerVersion, inputBytes)
	if err != nil {
		return nil, err
	}
	proxyInfo, err := client.detectProxy(ctx, opts.Address, result, opts.RPCURL)
	if err != nil {
		return nil, err
	}
	loadOpts := opts.LoadOptions
	loadOpts.Address = &opts.Address
	if proxyInfo != nil && proxyInfo.ImplementationAddress != nil {
		loadOpts.CodeAddress = proxyInfo.ImplementationAddress
	}
	bundle, err := buildBundle(standardJSON, []byte(result.ABI), result.CompilerVersion, loadOpts, proxyInfo)
	if err != nil {
		return nil, err
	}
	if proxyInfo != nil && proxyInfo.ImplementationAddress != nil {
		implementationOpts := opts
		implementationOpts.Address = *proxyInfo.ImplementationAddress
		implementationOpts.LoadOptions.Address = cloneAddressPtr(loadOpts.Address)
		implementationOpts.LoadOptions.CodeAddress = proxyInfo.ImplementationAddress
		implementationResult, err := client.fetchSource(ctx, *proxyInfo.ImplementationAddress)
		if err == nil {
			input, sourceName, buildErr := buildExplorerStandardInput(implementationResult, implementationOpts)
			if buildErr == nil {
				if implementationOpts.SourceName == "" {
					implementationOpts.SourceName = sourceName
				}
				if implementationOpts.ContractName == "" {
					implementationOpts.ContractName = implementationResult.ContractName
				}
				inputBytes, marshalErr := json.Marshal(input)
				if marshalErr == nil {
					if implementationJSON, compileErr := manager.CompileStandardInput(ctx, implementationResult.CompilerVersion, inputBytes); compileErr == nil {
						implementationBundle, bundleErr := buildBundle(implementationJSON, []byte(implementationResult.ABI), implementationResult.CompilerVersion, implementationOpts.LoadOptions, proxyInfo)
						if bundleErr == nil {
							return implementationBundle, nil
						}
					}
				}
			}
		}
	}
	return bundle, nil
}

func (client ExplorerClient) fetchSource(ctx context.Context, addr engine.Address) (explorerResult, error) {
	endpoint, err := client.buildAPIURL("contract", "getsourcecode", map[string]string{"address": encodeAddress(addr)})
	if err != nil {
		return explorerResult{}, err
	}
	request, err := http.NewRequestWithContext(ctx, "GET", endpoint.String(), nil)
	if err != nil {
		return explorerResult{}, err
	}
	response, err := client.httpClient().Do(request)
	if err != nil {
		return explorerResult{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return explorerResult{}, fmt.Errorf("explorer getsourcecode failed: %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	var decoded explorerResponse
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		return explorerResult{}, err
	}
	if message, ok := decodeExplorerStringResult(decoded.Result); ok {
		if strings.TrimSpace(message) == "" {
			message = decoded.Message
		}
		if strings.TrimSpace(message) == "" {
			message = "explorer returned an empty result"
		}
		return explorerResult{}, fmt.Errorf("explorer getsourcecode failed: %s", message)
	}
	var results []explorerResult
	if err := json.Unmarshal(decoded.Result, &results); err != nil {
		return explorerResult{}, fmt.Errorf("decode explorer result: %w", err)
	}
	if len(results) == 0 {
		return explorerResult{}, fmt.Errorf("explorer returned no results for %s", encodeAddress(addr))
	}
	return results[0], nil
}

func (client ExplorerClient) buildAPIURL(module string, action string, extraQuery map[string]string) (*url.URL, error) {
	endpoint, err := url.Parse(strings.TrimSpace(client.APIBase))
	if err != nil {
		return nil, err
	}
	if endpoint.Scheme == "" || endpoint.Host == "" {
		return nil, fmt.Errorf("invalid explorer api base %q", client.APIBase)
	}
	if shouldUseEtherscanV2(endpoint) {
		endpoint.Path = "/v2/api"
	}
	query := endpoint.Query()
	query.Set("module", module)
	query.Set("action", action)
	for key, value := range extraQuery {
		if strings.TrimSpace(value) != "" {
			query.Set(key, value)
		}
	}
	if strings.TrimSpace(client.APIKey) != "" {
		query.Set("apikey", client.APIKey)
	}
	if shouldUseEtherscanV2(endpoint) && query.Get("chainid") == "" {
		chainID := strings.TrimSpace(client.ChainID)
		if chainID == "" {
			chainID = "1"
		}
		query.Set("chainid", chainID)
	}
	endpoint.RawQuery = query.Encode()
	return endpoint, nil
}

func shouldUseEtherscanV2(endpoint *url.URL) bool {
	if endpoint == nil {
		return false
	}
	host := strings.ToLower(endpoint.Host)
	if !strings.Contains(host, "etherscan.io") {
		return false
	}
	path := strings.TrimSpace(endpoint.Path)
	return path == "" || path == "/" || path == "/api" || path == "/v2/api"
}

func decodeExplorerStringResult(raw json.RawMessage) (string, bool) {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	return value, true
}

func buildExplorerStandardInput(result explorerResult, opts ExplorerLoadOptions) (standardInput, string, error) {
	raw := strings.TrimSpace(result.SourceCode)
	if strings.HasPrefix(raw, "{{") && strings.HasSuffix(raw, "}}") {
		raw = raw[1 : len(raw)-1]
	}
	if strings.HasPrefix(raw, "{") {
		var input standardInput
		if err := json.Unmarshal([]byte(raw), &input); err == nil && len(input.Sources) > 0 {
			if input.Settings == nil {
				input.Settings = make(map[string]any)
			}
			input.Settings["outputSelection"] = defaultOutputSelection()
			sourceName := opts.SourceName
			if sourceName == "" {
				sourceName = result.sourceFileName()
			}
			if sourceName == "" {
				for name := range input.Sources {
					sourceName = name
					break
				}
			}
			return input, sourceName, nil
		}
	}
	sourceName := opts.SourceName
	if sourceName == "" {
		if result.sourceFileName() != "" {
			sourceName = result.sourceFileName()
		} else {
			sourceName = result.ContractName + ".sol"
		}
	}
	input := standardInput{
		Language: "Solidity",
		Sources: map[string]standardInputSource{
			sourceName: {Content: raw},
		},
		Settings: singleSourceSettings(result),
	}
	return input, sourceName, nil
}

func defaultOutputSelection() map[string]map[string][]string {
	return map[string]map[string][]string{
		"*": {
			"":  {"ast"},
			"*": {"abi", "storageLayout", "transientStorageLayout", "evm.bytecode.object", "evm.bytecode.sourceMap", "evm.bytecode.generatedSources", "evm.deployedBytecode.object", "evm.deployedBytecode.sourceMap", "evm.deployedBytecode.generatedSources"},
		},
	}
}

func (client ExplorerClient) detectProxy(ctx context.Context, proxyAddr engine.Address, result explorerResult, rpcURL string) (*ProxyInfo, error) {
	proxy := &ProxyInfo{ProxyAddress: &proxyAddr}
	if strings.TrimSpace(result.Implementation) != "" {
		if implementation, ok := parseAddress(result.Implementation); ok {
			proxy.ImplementationAddress = &implementation
			proxy.DetectedBy = append(proxy.DetectedBy, "explorer-implementation-field")
		}
	}
	if strings.TrimSpace(result.Proxy) == "1" {
		proxy.DetectedBy = append(proxy.DetectedBy, "explorer-proxy-flag")
	}
	endpoint := strings.TrimSpace(rpcURL)
	if endpoint == "" {
		endpoint = strings.TrimSpace(client.RPCURL)
	}
	if endpoint != "" {
		if implementation, ok, err := rpcReadImplementation(ctx, endpoint, proxyAddr, eip1967ImplementationSlot); err != nil {
			return nil, err
		} else if ok {
			proxy.ImplementationAddress = &implementation
			proxy.DetectedBy = append(proxy.DetectedBy, "eip1967-implementation-slot")
		}
		if beacon, ok, err := rpcReadImplementation(ctx, endpoint, proxyAddr, eip1967BeaconSlot); err != nil {
			return nil, err
		} else if ok {
			proxy.BeaconAddress = &beacon
			proxy.DetectedBy = append(proxy.DetectedBy, "eip1967-beacon-slot")
		}
		if implementation, ok, err := rpcReadImplementation(ctx, endpoint, proxyAddr, proxiableSlot); err != nil {
			return nil, err
		} else if ok && proxy.ImplementationAddress == nil {
			proxy.ImplementationAddress = &implementation
			proxy.DetectedBy = append(proxy.DetectedBy, "eip1822-proxiable-slot")
		}
	}
	if len(proxy.DetectedBy) == 0 && proxy.ImplementationAddress == nil && proxy.BeaconAddress == nil {
		return nil, nil
	}
	return proxy, nil
}

func (client ExplorerClient) httpClient() *http.Client {
	if client.Client != nil {
		return client.Client
	}
	return http.DefaultClient
}

func rpcReadImplementation(ctx context.Context, endpoint string, addr engine.Address, slot engine.Hash) (engine.Address, bool, error) {
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "eth_getStorageAt",
		"params":  []any{encodeAddress(addr), encodeHash(slot), "latest"},
	}
	requestBody, err := json.Marshal(payload)
	if err != nil {
		return engine.Address{}, false, err
	}
	request, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(string(requestBody)))
	if err != nil {
		return engine.Address{}, false, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return engine.Address{}, false, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return engine.Address{}, false, fmt.Errorf("eth_getStorageAt returned %s", response.Status)
	}
	var decoded struct {
		Result string `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		return engine.Address{}, false, err
	}
	if decoded.Error != nil {
		return engine.Address{}, false, fmt.Errorf("eth_getStorageAt: %s", decoded.Error.Message)
	}
	implementation, ok := parseImplementationFromStorage(decoded.Result)
	return implementation, ok, nil
}

func parseImplementationFromStorage(value string) (engine.Address, bool) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(value), "0x")
	if len(trimmed) != 64 {
		return engine.Address{}, false
	}
	bytesValue, err := hex.DecodeString(trimmed)
	if err != nil {
		return engine.Address{}, false
	}
	zero := true
	for _, item := range bytesValue[12:] {
		if item != 0 {
			zero = false
			break
		}
	}
	if zero {
		return engine.Address{}, false
	}
	var addr engine.Address
	copy(addr[:], bytesValue[12:])
	return addr, true
}

func parseAddress(value string) (engine.Address, bool) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(value), "0x")
	if len(trimmed) != 40 {
		return engine.Address{}, false
	}
	decoded, err := hex.DecodeString(trimmed)
	if err != nil {
		return engine.Address{}, false
	}
	var addr engine.Address
	copy(addr[:], decoded)
	return addr, true
}

func encodeAddress(addr engine.Address) string {
	return "0x" + hex.EncodeToString(addr[:])
}

func encodeHash(hash engine.Hash) string {
	return "0x" + hex.EncodeToString(hash[:])
}

func mustHash(value string) engine.Hash {
	decoded, err := hex.DecodeString(value)
	if err != nil {
		panic(err)
	}
	var hash engine.Hash
	copy(hash[:], decoded)
	return hash
}

func parseDecimalInt(value string) int {
	if value == "" {
		return 0
	}
	parsed, ok := new(big.Int).SetString(value, 10)
	if !ok {
		return 0
	}
	return int(parsed.Int64())
}

func normalizeEVMVersion(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "default" {
		return ""
	}
	return trimmed
}

func (result explorerResult) sourceFileName() string {
	if name := strings.TrimSpace(result.ContractFileName); name != "" {
		return name
	}
	return strings.TrimSpace(result.FileName)
}

func singleSourceSettings(result explorerResult) map[string]any {
	settings := map[string]any{
		"outputSelection": defaultOutputSelection(),
	}
	settings["optimizer"] = map[string]any{"enabled": result.OptimizationUsed == "1", "runs": parseDecimalInt(result.Runs)}
	if evmVersion := normalizeEVMVersion(result.EVMVersion); evmVersion != "" {
		settings["evmVersion"] = evmVersion
	}
	return settings
}
