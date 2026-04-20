package upstream

import (
	"fmt"
	"strconv"
	"strings"
)

type StructuredTrace struct {
	Gas         *uint64
	Failed      bool
	ReturnValue string
	StructLogs  []StructuredTraceStep
}

type StructuredTraceStep struct {
	PC      uint64
	Op      string
	Gas     *uint64
	GasCost *uint64
	Depth   int
	Error   string
}

func ParseStructuredTrace(result TraceTransactionResult) (StructuredTrace, error) {
	trace := StructuredTrace{}
	gas, err := optionalUint64(result["gas"])
	if err != nil {
		return StructuredTrace{}, fmt.Errorf("upstream: parse trace gas: %w", err)
	}
	trace.Gas = gas
	if failed, ok := result["failed"]; ok {
		parsed, err := boolValue(failed)
		if err != nil {
			return StructuredTrace{}, fmt.Errorf("upstream: parse trace failed flag: %w", err)
		}
		trace.Failed = parsed
	}
	if returnValue, ok := result["returnValue"]; ok {
		text, err := stringValue(returnValue)
		if err != nil {
			return StructuredTrace{}, fmt.Errorf("upstream: parse trace return value: %w", err)
		}
		trace.ReturnValue = strings.ToLower(strings.TrimSpace(text))
	}
	steps, err := parseStructuredTraceSteps(result["structLogs"])
	if err != nil {
		return StructuredTrace{}, err
	}
	trace.StructLogs = steps
	return trace, nil
}

func parseStructuredTraceSteps(raw any) ([]StructuredTraceStep, error) {
	if raw == nil {
		return nil, nil
	}
	values, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("upstream: structLogs has type %T, want []any", raw)
	}
	steps := make([]StructuredTraceStep, 0, len(values))
	for index, entry := range values {
		mapped, ok := entry.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("upstream: structLog[%d] has type %T, want map[string]any", index, entry)
		}
		pc, err := uint64Value(mapped["pc"])
		if err != nil {
			return nil, fmt.Errorf("upstream: parse structLog[%d].pc: %w", index, err)
		}
		op, err := stringValue(mapped["op"])
		if err != nil {
			return nil, fmt.Errorf("upstream: parse structLog[%d].op: %w", index, err)
		}
		depth := 0
		if rawDepth, ok := mapped["depth"]; ok {
			parsedDepth, err := intValue(rawDepth)
			if err != nil {
				return nil, fmt.Errorf("upstream: parse structLog[%d].depth: %w", index, err)
			}
			depth = parsedDepth
		}
		gas, err := optionalUint64(mapped["gas"])
		if err != nil {
			return nil, fmt.Errorf("upstream: parse structLog[%d].gas: %w", index, err)
		}
		gasCost, err := optionalUint64(mapped["gasCost"])
		if err != nil {
			return nil, fmt.Errorf("upstream: parse structLog[%d].gasCost: %w", index, err)
		}
		errorText := ""
		if rawError, ok := mapped["error"]; ok && rawError != nil {
			parsedError, err := stringValue(rawError)
			if err != nil {
				return nil, fmt.Errorf("upstream: parse structLog[%d].error: %w", index, err)
			}
			errorText = parsedError
		}
		steps = append(steps, StructuredTraceStep{PC: pc, Op: op, Depth: depth, Gas: gas, GasCost: gasCost, Error: errorText})
	}
	return steps, nil
}

func uint64Value(raw any) (uint64, error) {
	parsed, err := optionalUint64(raw)
	if err != nil {
		return 0, err
	}
	if parsed == nil {
		return 0, fmt.Errorf("missing uint64 value")
	}
	return *parsed, nil
}

func intValue(raw any) (int, error) {
	value, err := uint64Value(raw)
	if err != nil {
		return 0, err
	}
	return int(value), nil
}

func optionalUint64(raw any) (*uint64, error) {
	if raw == nil {
		return nil, nil
	}
	switch value := raw.(type) {
	case float64:
		parsed := uint64(value)
		return &parsed, nil
	case float32:
		parsed := uint64(value)
		return &parsed, nil
	case int:
		parsed := uint64(value)
		return &parsed, nil
	case int32:
		parsed := uint64(value)
		return &parsed, nil
	case int64:
		parsed := uint64(value)
		return &parsed, nil
	case uint:
		parsed := uint64(value)
		return &parsed, nil
	case uint32:
		parsed := uint64(value)
		return &parsed, nil
	case uint64:
		parsed := value
		return &parsed, nil
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return nil, nil
		}
		base := 10
		if strings.HasPrefix(trimmed, "0x") || strings.HasPrefix(trimmed, "0X") {
			trimmed = trimmed[2:]
			base = 16
		}
		parsed, err := strconv.ParseUint(trimmed, base, 64)
		if err != nil {
			return nil, err
		}
		return &parsed, nil
	default:
		return nil, fmt.Errorf("unsupported numeric type %T", raw)
	}
}

func boolValue(raw any) (bool, error) {
	switch value := raw.(type) {
	case bool:
		return value, nil
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return false, err
		}
		return parsed, nil
	default:
		return false, fmt.Errorf("unsupported bool type %T", raw)
	}
}

func stringValue(raw any) (string, error) {
	value, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("unsupported string type %T", raw)
	}
	return value, nil
}

func EncodeHex(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	const hexAlphabet = "0123456789abcdef"
	encoded := make([]byte, 2+len(data)*2)
	encoded[0] = '0'
	encoded[1] = 'x'
	for index, value := range data {
		encoded[2+index*2] = hexAlphabet[value>>4]
		encoded[3+index*2] = hexAlphabet[value&0x0f]
	}
	return string(encoded)
}
