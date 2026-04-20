package upstream

import "testing"

func TestParseStructuredTrace(t *testing.T) {
	trace, err := ParseStructuredTrace(TraceTransactionResult{
		"gas":         float64(21000),
		"failed":      false,
		"returnValue": "0x2a",
		"structLogs": []any{
			map[string]any{"pc": float64(0), "op": "PUSH1", "gas": float64(50000), "gasCost": float64(3), "depth": float64(1)},
		},
	})
	if err != nil {
		t.Fatalf("ParseStructuredTrace() error = %v", err)
	}
	if trace.Gas == nil || *trace.Gas != 21000 {
		t.Fatalf("trace.Gas = %v, want 21000", trace.Gas)
	}
	if trace.Failed {
		t.Fatalf("trace.Failed = true, want false")
	}
	if trace.ReturnValue != "0x2a" {
		t.Fatalf("trace.ReturnValue = %q, want 0x2a", trace.ReturnValue)
	}
	if len(trace.StructLogs) != 1 {
		t.Fatalf("len(trace.StructLogs) = %d, want 1", len(trace.StructLogs))
	}
	if trace.StructLogs[0].PC != 0 || trace.StructLogs[0].Op != "PUSH1" || trace.StructLogs[0].Depth != 1 {
		t.Fatalf("struct log = %#v", trace.StructLogs[0])
	}
}
