package openchain

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDecodeWithSignature(t *testing.T) {
	decoded, err := decodeWithSignature(
		"0xa9059cbb0000000000000000000000001111111111111111111111111111111111111111000000000000000000000000000000000000000000000000000000000000002a",
		"transfer(address,uint256)",
	)
	if err != nil {
		t.Fatalf("decodeWithSignature() error = %v", err)
	}
	if decoded.Name != "transfer" {
		t.Fatalf("decoded.Name = %q, want transfer", decoded.Name)
	}
	if len(decoded.Inputs) != 2 {
		t.Fatalf("len(decoded.Inputs) = %d, want 2", len(decoded.Inputs))
	}
}

func TestSplitSignatureArguments(t *testing.T) {
	parts := splitSignatureArguments("address,(uint256,bytes32),uint256[]")
	if len(parts) != 3 {
		t.Fatalf("len(parts) = %d, want 3", len(parts))
	}
}

func TestLookupFunction_CurrentPayloadShape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		selector := request.URL.Query().Get("function")
		if selector == "" {
			http.Error(writer, "missing selector", http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(writer, `{"result":{"function":{"%s":[{"name":"flashLoan(address,address[],uint256[],uint256[],address,bytes,uint16)"}]}}}`, selector)
	}))
	defer server.Close()

	client := Client{BaseURL: server.URL}
	names, err := client.LookupFunction(context.Background(), "0xab9c4b5d")
	if err != nil {
		t.Fatalf("LookupFunction() error = %v", err)
	}
	if len(names) != 1 {
		t.Fatalf("len(names) = %d, want 1", len(names))
	}
	if names[0] != "flashLoan(address,address[],uint256[],uint256[],address,bytes,uint16)" {
		t.Fatalf("names[0] = %q, want flashLoan(address,address[],uint256[],uint256[],address,bytes,uint16)", names[0])
	}
}

func TestLookupFunction_LegacyPayloadShape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		selector := request.URL.Query().Get("function")
		if selector == "" {
			http.Error(writer, "missing selector", http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(writer, `{"result":{"function":{"%s":{"name":["transfer(address,uint256)"]}}}}`, selector)
	}))
	defer server.Close()

	client := Client{BaseURL: server.URL}
	names, err := client.LookupFunction(context.Background(), "0xa9059cbb")
	if err != nil {
		t.Fatalf("LookupFunction() error = %v", err)
	}
	if len(names) != 1 {
		t.Fatalf("len(names) = %d, want 1", len(names))
	}
	if names[0] != "transfer(address,uint256)" {
		t.Fatalf("names[0] = %q, want transfer(address,uint256)", names[0])
	}
}
