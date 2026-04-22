package oneshot

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/big"
	"reflect"
	"strconv"
	"strings"

	gethabi "github.com/ethereum/go-ethereum/accounts/abi"
	gethcommon "github.com/ethereum/go-ethereum/common"

	"inspethct/internal/engine"
)

func buildMethodFromSignature(signature string) (gethabi.Method, string, error) {
	normalized := normalizeFunctionSignature(signature)
	name, types, err := parseSignature(normalized)
	if err != nil {
		return gethabi.Method{}, "", err
	}
	inputs := make([]gethabi.ArgumentMarshaling, 0, len(types))
	for index, typeName := range types {
		if strings.Contains(typeName, "tuple") {
			return gethabi.Method{}, "", fmt.Errorf("tuple signatures are not supported")
		}
		inputs = append(inputs, gethabi.ArgumentMarshaling{Name: fmt.Sprintf("arg%d", index), Type: typeName})
	}
	definition, err := json.Marshal([]map[string]any{{"type": "function", "name": name, "inputs": inputs}})
	if err != nil {
		return gethabi.Method{}, "", err
	}
	parsed, err := gethabi.JSON(strings.NewReader(string(definition)))
	if err != nil {
		return gethabi.Method{}, "", err
	}
	method, ok := parsed.Methods[name]
	if !ok {
		return gethabi.Method{}, "", fmt.Errorf("method %q not found after parsing", name)
	}
	return method, method.Sig, nil
}

func normalizeFunctionSignature(signature string) string {
	trimmed := strings.TrimSpace(signature)
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "function ") {
		trimmed = strings.TrimSpace(trimmed[9:])
	}
	if strings.HasPrefix(lower, "sig ") {
		trimmed = strings.TrimSpace(trimmed[4:])
	}
	open := strings.IndexByte(trimmed, '(')
	if open > 0 {
		prefix := trimmed[:open]
		if dot := strings.LastIndexByte(prefix, '.'); dot >= 0 {
			trimmed = prefix[dot+1:] + trimmed[open:]
		}
	}
	return trimmed
}

func parseSignature(signature string) (string, []string, error) {
	open := strings.IndexByte(signature, '(')
	close := strings.LastIndexByte(signature, ')')
	if open <= 0 || close <= open {
		return "", nil, fmt.Errorf("invalid signature %q", signature)
	}
	name := strings.TrimSpace(signature[:open])
	inside := signature[open+1 : close]
	if strings.TrimSpace(inside) == "" {
		return name, nil, nil
	}
	parts := splitSignatureArguments(inside)
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

func promptMethodArguments(reader *bufio.Reader, writer io.Writer, method gethabi.Method) ([]string, error) {
	values := make([]string, 0, len(method.Inputs))
	for index, input := range method.Inputs {
		label := input.Name
		if label == "" {
			label = fmt.Sprintf("arg%d", index)
		}
		value, err := promptLine(reader, writer, fmt.Sprintf("%s (%s): ", label, input.Type.String()))
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

func encodeMethodCall(method gethabi.Method, args []string) ([]byte, error) {
	if len(args) != len(method.Inputs) {
		return nil, fmt.Errorf("function %s expects %d args, got %d", method.Sig, len(method.Inputs), len(args))
	}
	values := make([]any, 0, len(args))
	for index, input := range method.Inputs {
		value, err := coerceABIValue(input.Type, args[index])
		if err != nil {
			return nil, fmt.Errorf("arg %d: %w", index, err)
		}
		values = append(values, value)
	}
	payload, err := method.Inputs.Pack(values...)
	if err != nil {
		return nil, err
	}
	return append(append([]byte(nil), method.ID...), payload...), nil
}

func coerceABIValue(typ gethabi.Type, raw string) (any, error) {
	switch typ.T {
	case gethabi.AddressTy:
		addr, err := parseCLIAddress(raw)
		if err != nil {
			return nil, err
		}
		return gethcommon.BytesToAddress(addr[:]), nil
	case gethabi.BoolTy:
		return strconv.ParseBool(strings.ToLower(strings.TrimSpace(raw)))
	case gethabi.StringTy:
		return parseStringToken(raw), nil
	case gethabi.BytesTy:
		if strings.HasPrefix(strings.TrimSpace(raw), "0x") {
			return decodeCLIBytes(raw)
		}
		return []byte(parseStringToken(raw)), nil
	case gethabi.FixedBytesTy:
		decoded, err := decodeCLIBytes(raw)
		if err != nil {
			return nil, err
		}
		if len(decoded) != typ.Size {
			return nil, fmt.Errorf("expected %d bytes, got %d", typ.Size, len(decoded))
		}
		value := reflect.New(typ.GetType()).Elem()
		reflect.Copy(value, reflect.ValueOf(decoded))
		return value.Interface(), nil
	case gethabi.UintTy:
		return coerceABIInteger(typ.GetType(), raw, false)
	case gethabi.IntTy:
		return coerceABIInteger(typ.GetType(), raw, true)
	case gethabi.SliceTy:
		return coerceABISlice(typ, raw)
	case gethabi.ArrayTy:
		return coerceABIArray(typ, raw)
	default:
		return nil, fmt.Errorf("unsupported ABI type %s", typ.String())
	}
}

func coerceABIInteger(targetType reflect.Type, raw string, signed bool) (any, error) {
	value, err := parseCLIInteger(raw)
	if err != nil {
		return nil, err
	}
	if !signed && value.Sign() < 0 {
		return nil, fmt.Errorf("unsigned integer cannot be negative")
	}
	if targetType == reflect.TypeOf(&big.Int{}) {
		return value, nil
	}
	if targetType == reflect.TypeOf(big.Int{}) {
		return *value, nil
	}
	result := reflect.New(targetType).Elem()
	switch targetType.Kind() {
	case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uint:
		if value.Sign() < 0 || value.BitLen() > targetType.Bits() {
			return nil, fmt.Errorf("value %s overflows %s", value.String(), targetType.String())
		}
		result.SetUint(value.Uint64())
		return result.Interface(), nil
	case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Int:
		if !value.IsInt64() {
			return nil, fmt.Errorf("value %s does not fit in %s", value.String(), targetType.String())
		}
		intValue := value.Int64()
		bits := targetType.Bits()
		min := -int64(1 << (bits - 1))
		max := int64((1 << (bits - 1)) - 1)
		if bits == 64 {
			min = math.MinInt64
			max = math.MaxInt64
		}
		if intValue < min || intValue > max {
			return nil, fmt.Errorf("value %s overflows %s", value.String(), targetType.String())
		}
		result.SetInt(intValue)
		return result.Interface(), nil
	default:
		return nil, fmt.Errorf("unsupported integer target type %s", targetType.String())
	}
}

func coerceABISlice(typ gethabi.Type, raw string) (any, error) {
	items, err := parseJSONArrayTokens(raw)
	if err != nil {
		return nil, err
	}
	value := reflect.MakeSlice(typ.GetType(), len(items), len(items))
	for index, item := range items {
		converted, err := coerceABIValue(*typ.Elem, item)
		if err != nil {
			return nil, fmt.Errorf("item %d: %w", index, err)
		}
		child := reflect.ValueOf(converted)
		if !child.Type().AssignableTo(value.Index(index).Type()) {
			if !child.Type().ConvertibleTo(value.Index(index).Type()) {
				return nil, fmt.Errorf("item %d type mismatch", index)
			}
			child = child.Convert(value.Index(index).Type())
		}
		value.Index(index).Set(child)
	}
	return value.Interface(), nil
}

func coerceABIArray(typ gethabi.Type, raw string) (any, error) {
	items, err := parseJSONArrayTokens(raw)
	if err != nil {
		return nil, err
	}
	if len(items) != typ.Size {
		return nil, fmt.Errorf("expected %d items, got %d", typ.Size, len(items))
	}
	value := reflect.New(typ.GetType()).Elem()
	for index, item := range items {
		converted, err := coerceABIValue(*typ.Elem, item)
		if err != nil {
			return nil, fmt.Errorf("item %d: %w", index, err)
		}
		child := reflect.ValueOf(converted)
		if !child.Type().AssignableTo(value.Index(index).Type()) {
			if !child.Type().ConvertibleTo(value.Index(index).Type()) {
				return nil, fmt.Errorf("item %d type mismatch", index)
			}
			child = child.Convert(value.Index(index).Type())
		}
		value.Index(index).Set(child)
	}
	return value.Interface(), nil
}

func parseJSONArrayTokens(raw string) ([]string, error) {
	var values []json.RawMessage
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &values); err != nil {
		return nil, fmt.Errorf("expected JSON array: %w", err)
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, jsonTokenToString(value))
	}
	return result, nil
}

func jsonTokenToString(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trimmed, "\"") {
		var decoded string
		if err := json.Unmarshal(raw, &decoded); err == nil {
			return decoded
		}
	}
	return trimmed
}

func parseStringToken(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if len(trimmed) >= 2 && strings.HasPrefix(trimmed, "\"") && strings.HasSuffix(trimmed, "\"") {
		decoded, err := strconv.Unquote(trimmed)
		if err == nil {
			return decoded
		}
	}
	return trimmed
}

func parseCLIInteger(raw string) (*big.Int, error) {
	trimmed := strings.TrimSpace(strings.ToLower(raw))
	if trimmed == "" {
		return nil, fmt.Errorf("integer value is required")
	}
	negative := false
	if strings.HasPrefix(trimmed, "-") {
		negative = true
		trimmed = strings.TrimPrefix(trimmed, "-")
	}
	base := 10
	if strings.HasPrefix(trimmed, "0x") {
		trimmed = strings.TrimPrefix(trimmed, "0x")
		base = 16
	}
	value, ok := new(big.Int).SetString(trimmed, base)
	if !ok {
		return nil, fmt.Errorf("invalid integer %q", raw)
	}
	if negative {
		value.Neg(value)
	}
	return value, nil
}

func parseCLIHash(value string) (engine.Hash, error) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(value), "0x")
	decoded, err := hex.DecodeString(trimmed)
	if err != nil {
		return engine.Hash{}, err
	}
	if len(decoded) != 32 {
		return engine.Hash{}, fmt.Errorf("hash must be 32 bytes")
	}
	var hash engine.Hash
	copy(hash[:], decoded)
	return hash, nil
}

func parseOptionalUint64(value string) (uint64, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, nil
	}
	return parseUint64Flexible(trimmed)
}

func parseUint64Flexible(value string) (uint64, error) {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	base := 10
	if strings.HasPrefix(trimmed, "0x") {
		trimmed = strings.TrimPrefix(trimmed, "0x")
		base = 16
	}
	return strconv.ParseUint(trimmed, base, 64)
}

func parseAccessMode(value string) (breakpointAccessMode, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "read", "r":
		return accessModeRead, nil
	case "write", "w":
		return accessModeWrite, nil
	case "rw", "both", "readwrite":
		return accessModeBoth, nil
	default:
		return "", fmt.Errorf("unsupported access mode %q", value)
	}
}

func decodeCLIBytes(value string) ([]byte, error) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(value), "0x")
	if trimmed == "" {
		return nil, nil
	}
	if len(trimmed)%2 == 1 {
		trimmed = "0" + trimmed
	}
	return hex.DecodeString(trimmed)
}

func encodeCLIBytes(data []byte) string {
	if len(data) == 0 {
		return "0x"
	}
	return "0x" + hex.EncodeToString(data)
}

func looksLikeFunctionSignature(spec string) bool {
	trimmed := strings.TrimSpace(spec)
	open := strings.IndexByte(trimmed, '(')
	close := strings.LastIndexByte(trimmed, ')')
	if open <= 0 || close <= open {
		return false
	}
	return !strings.Contains(trimmed[:open], ":")
}

func selectorMatches(input []byte, selector []byte) bool {
	return len(selector) == 4 && len(input) >= 4 && input[0] == selector[0] && input[1] == selector[1] && input[2] == selector[2] && input[3] == selector[3]
}

func rangesOverlap(offsetA uint64, sizeA uint64, offsetB uint64, sizeB uint64) bool {
	endA := offsetA + maxUint64(sizeA, 1)
	endB := offsetB + maxUint64(sizeB, 1)
	return offsetA < endB && offsetB < endA
}

func maxUint64(a uint64, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}
