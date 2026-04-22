package openchain

import "testing"

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
