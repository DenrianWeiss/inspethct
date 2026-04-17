package srcmap

import "testing"

func TestParseInstructionMappingsCompressed(t *testing.T) {
	bytecode := []byte{0x60, 0x01, 0x60, 0x02, 0x01, 0x00}
	mappings, err := ParseInstructionMappings(bytecode, "1:2:1;:9;2:1:2;;")
	if err != nil {
		t.Fatalf("ParseInstructionMappings() error = %v", err)
	}
	if len(mappings) != 4 {
		t.Fatalf("len(mappings) = %d, want 4", len(mappings))
	}
	if mappings[0].PC != 0 || mappings[1].PC != 2 || mappings[2].PC != 4 || mappings[3].PC != 5 {
		t.Fatalf("unexpected PCs: %#v", mappings)
	}
	if mappings[1].Source.Start != 1 || mappings[1].Source.Length != 9 || mappings[1].Source.SourceID != 1 {
		t.Fatalf("unexpected compressed inheritance result: %+v", mappings[1].Source)
	}
	if mappings[3].Source.Start != 2 || mappings[3].Source.Length != 1 || mappings[3].Source.SourceID != 2 {
		t.Fatalf("unexpected repeated entry result: %+v", mappings[3].Source)
	}
}