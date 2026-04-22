package openchain

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	gethabi "github.com/ethereum/go-ethereum/accounts/abi"
)

const defaultBaseURL = "https://api.openchain.xyz"

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

type DecodedCall struct {
	Signature string   `json:"signature"`
	Name      string   `json:"name"`
	Inputs    []string `json:"inputs"`
}

type lookupResponse struct {
	Result struct {
		Function map[string]struct {
			Name []string `json:"name"`
		} `json:"function"`
		Event map[string]struct {
			Name []string `json:"name"`
		} `json:"event"`
	} `json:"result"`
}

func (client Client) LookupFunction(ctx context.Context, selector string) ([]string, error) {
	return client.lookup(ctx, "function", selector)
}

func (client Client) LookupEvent(ctx context.Context, topic string) ([]string, error) {
	return client.lookup(ctx, "event", topic)
}

func (client Client) DecodeCalldata(ctx context.Context, calldata string) ([]DecodedCall, error) {
	normalized := normalizeHex(calldata)
	if len(normalized) < 10 {
		return nil, fmt.Errorf("calldata must include at least 4-byte selector")
	}
	selector := normalized[:10]
	signatures, err := client.LookupFunction(ctx, selector)
	if err != nil {
		return nil, err
	}
	decoded := make([]DecodedCall, 0, len(signatures))
	for _, signature := range signatures {
		candidate, err := decodeWithSignature(normalized, signature)
		if err == nil {
			decoded = append(decoded, candidate)
		}
	}
	if len(decoded) == 0 {
		return nil, fmt.Errorf("unable to decode calldata with guessed signatures")
	}
	return decoded, nil
}

func (client Client) lookup(ctx context.Context, kind string, value string) ([]string, error) {
	base := client.BaseURL
	if strings.TrimSpace(base) == "" {
		base = defaultBaseURL
	}
	endpoint, err := url.Parse(strings.TrimRight(base, "/") + "/signature-database/v1/lookup")
	if err != nil {
		return nil, err
	}
	query := endpoint.Query()
	query.Set(kind, normalizeHex(value))
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	response, err := client.httpClient().Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openchain lookup failed: %s", response.Status)
	}
	var decoded lookupResponse
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		return nil, err
	}
	key := normalizeHex(value)
	if kind == "function" {
		return append([]string(nil), decoded.Result.Function[key].Name...), nil
	}
	return append([]string(nil), decoded.Result.Event[key].Name...), nil
}

func decodeWithSignature(calldata string, signature string) (DecodedCall, error) {
	name, typeNames, err := parseSignature(signature)
	if err != nil {
		return DecodedCall{}, err
	}
	arguments := make([]gethabi.ArgumentMarshaling, 0, len(typeNames))
	for _, typeName := range typeNames {
		if strings.Contains(typeName, "tuple") {
			return DecodedCall{}, fmt.Errorf("tuple signatures are not supported")
		}
		arguments = append(arguments, gethabi.ArgumentMarshaling{Type: typeName})
	}
	definition, err := json.Marshal([]map[string]any{{"type": "function", "name": name, "inputs": arguments}})
	if err != nil {
		return DecodedCall{}, err
	}
	parsedABI, err := gethabi.JSON(strings.NewReader(string(definition)))
	if err != nil {
		return DecodedCall{}, err
	}
	method, ok := parsedABI.Methods[name]
	if !ok {
		return DecodedCall{}, fmt.Errorf("decoded ABI method %q missing", name)
	}
	payload, err := hexDecodeString(calldata[10:])
	if err != nil {
		return DecodedCall{}, err
	}
	values, err := method.Inputs.UnpackValues(payload)
	if err != nil {
		return DecodedCall{}, err
	}
	formatted := make([]string, 0, len(values))
	for _, value := range values {
		formatted = append(formatted, fmt.Sprintf("%v", value))
	}
	return DecodedCall{Signature: signature, Name: name, Inputs: formatted}, nil
}

func parseSignature(signature string) (string, []string, error) {
	open := strings.IndexByte(signature, '(')
	close := strings.LastIndexByte(signature, ')')
	if open <= 0 || close <= open {
		return "", nil, fmt.Errorf("invalid signature %q", signature)
	}
	name := strings.TrimSpace(signature[:open])
	body := signature[open+1 : close]
	if strings.TrimSpace(body) == "" {
		return name, nil, nil
	}
	parts := splitSignatureArguments(body)
	for index := range parts {
		parts[index] = strings.TrimSpace(parts[index])
	}
	return name, parts, nil
}

func splitSignatureArguments(value string) []string {
	parts := make([]string, 0)
	depth := 0
	start := 0
	for index, r := range value {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, value[start:index])
				start = index + 1
			}
		}
	}
	parts = append(parts, value[start:])
	return parts
}

func normalizeHex(value string) string {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	if strings.HasPrefix(trimmed, "0x") {
		return trimmed
	}
	return "0x" + trimmed
}

func hexDecodeString(value string) ([]byte, error) {
	if len(value)%2 == 1 {
		value = "0" + value
	}
	return hex.DecodeString(value)
}

func (client Client) httpClient() *http.Client {
	if client.HTTPClient != nil {
		return client.HTTPClient
	}
	return http.DefaultClient
}
