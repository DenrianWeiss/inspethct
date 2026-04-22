package contractmeta

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"inspethct/internal/engine"
)

func TestExplorerBuildAPIURLUsesEtherscanV2Defaults(t *testing.T) {
	client := ExplorerClient{APIBase: "https://api.etherscan.io/api", APIKey: "test-key"}
	endpoint, err := client.buildAPIURL("contract", "getsourcecode", map[string]string{"address": "0x1234"})
	if err != nil {
		t.Fatalf("buildAPIURL() error = %v", err)
	}
	if endpoint.Path != "/v2/api" {
		t.Fatalf("endpoint.Path = %q, want /v2/api", endpoint.Path)
	}
	query := endpoint.Query()
	if query.Get("chainid") != "1" {
		t.Fatalf("chainid = %q, want 1", query.Get("chainid"))
	}
	if query.Get("module") != "contract" || query.Get("action") != "getsourcecode" {
		t.Fatalf("unexpected query = %q", endpoint.RawQuery)
	}
	if query.Get("address") != "0x1234" {
		t.Fatalf("address = %q, want 0x1234", query.Get("address"))
	}
}

func TestExplorerFetchSourceReturnsStringErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"status":"0","message":"NOTOK","result":"deprecated endpoint"}`))
	}))
	defer server.Close()

	_, err := (ExplorerClient{APIBase: server.URL}).fetchSource(context.Background(), engine.Address{19: 0x01})
	if err == nil {
		t.Fatal("fetchSource() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "deprecated endpoint") {
		t.Fatalf("fetchSource() error = %v, want deprecated endpoint", err)
	}
}

func TestExplorerFetchSourceParsesArrayResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"status":"1","message":"OK","result":[{"SourceCode":"contract C {}","ABI":"[]","ContractName":"C","CompilerVersion":"v0.8.27+commit.40a35a09","OptimizationUsed":"1","Runs":"200","EVMVersion":"cancun","Proxy":"0","Implementation":"","Library":"","FileName":"C.sol"}]}`))
	}))
	defer server.Close()

	result, err := (ExplorerClient{APIBase: server.URL}).fetchSource(context.Background(), engine.Address{19: 0x01})
	if err != nil {
		t.Fatalf("fetchSource() error = %v", err)
	}
	if result.ContractName != "C" {
		t.Fatalf("result.ContractName = %q, want C", result.ContractName)
	}
	if result.sourceFileName() != "C.sol" {
		t.Fatalf("result.sourceFileName() = %q, want C.sol", result.sourceFileName())
	}
}

func TestResolveSourceNameFromCompiledOutputPrefersMatchingContract(t *testing.T) {
	standardJSON := []byte(`{
		"contracts": {
			"interfaces/IThing.sol": {"IThing": {}},
			"contracts/PoolInstance.sol": {"PoolInstance": {}}
		}
	}`)
	sourceName, err := resolveSourceNameFromCompiledOutput(standardJSON, "interfaces/IThing.sol", "PoolInstance")
	if err != nil {
		t.Fatalf("resolveSourceNameFromCompiledOutput() error = %v", err)
	}
	if sourceName != "contracts/PoolInstance.sol" {
		t.Fatalf("sourceName = %q, want contracts/PoolInstance.sol", sourceName)
	}
}

func TestBuildExplorerStandardInputPreservesRawSettings(t *testing.T) {
	result := explorerResult{
		SourceCode: `{
			"language": "Solidity",
			"sources": {
				"contracts/PoolInstance.sol": {"content": "contract PoolInstance {}"}
			},
			"settings": {
				"viaIR": true,
				"remappings": ["openzeppelin-contracts/=lib/openzeppelin-contracts/"],
				"metadata": {"bytecodeHash": "ipfs"}
			}
		}`,
		ContractFileName: "contracts/PoolInstance.sol",
	}
	input, sourceName, err := buildExplorerStandardInput(result, ExplorerLoadOptions{})
	if err != nil {
		t.Fatalf("buildExplorerStandardInput() error = %v", err)
	}
	if sourceName != "contracts/PoolInstance.sol" {
		t.Fatalf("sourceName = %q, want contracts/PoolInstance.sol", sourceName)
	}
	encoded, err := json.Marshal(input.Settings)
	if err != nil {
		t.Fatalf("Marshal(settings) error = %v", err)
	}
	settingsJSON := string(encoded)
	if !strings.Contains(settingsJSON, `"viaIR":true`) {
		t.Fatalf("settings JSON = %s, want viaIR preserved", settingsJSON)
	}
	if !strings.Contains(settingsJSON, `"remappings":["openzeppelin-contracts/=lib/openzeppelin-contracts/"]`) {
		t.Fatalf("settings JSON = %s, want remappings preserved", settingsJSON)
	}
	if !strings.Contains(settingsJSON, `"bytecodeHash":"ipfs"`) {
		t.Fatalf("settings JSON = %s, want metadata preserved", settingsJSON)
	}
	if !strings.Contains(settingsJSON, `"outputSelection"`) {
		t.Fatalf("settings JSON = %s, want outputSelection added", settingsJSON)
	}
}
