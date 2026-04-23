package varpeeker

import (
	"encoding/hex"
	"strconv"

	"inspethct/internal/srcmap"
)

// peekImmutables resolves every immutable reference into a Variable
// using the runtime bytecode as the value source. The AST node id
// (decimal string) keys the ImmutableReferences map. Each immutable can
// have multiple segments (the same value written at multiple PCs); we
// take the first segment as the canonical value.
func peekImmutables(index *srcmap.Index, runtimeCode []byte) []Variable {
	if index == nil || len(index.ImmutableReferences) == 0 || len(runtimeCode) == 0 {
		return nil
	}
	out := make([]Variable, 0, len(index.ImmutableReferences))
	for key, segments := range index.ImmutableReferences {
		if len(segments) == 0 {
			continue
		}
		astID, err := strconv.Atoi(key)
		if err != nil {
			continue
		}
		node := index.NodesByID[astID]
		v := Variable{
			Kind:       KindImmutable,
			Confidence: ConfidenceMedium,
		}
		if node != nil {
			v.Name, _ = node.Raw["name"].(string)
			if td, ok := node.Raw["typeDescriptions"].(map[string]any); ok {
				v.Type, _ = td["typeString"].(string)
			}
			if file := index.Sources[node.Src.SourceID]; file != nil {
				line, _ := file.LineColumnForOffset(node.Src.Start)
				v.DeclaredAtLine = line
				v.SourceID = node.Src.SourceID
			}
		}
		if v.Name == "" {
			v.Name = "immutable_" + key
		}
		seg := segments[0]
		end := seg.Start + seg.Length
		if end > uint64(len(runtimeCode)) {
			v.Note = "segment beyond runtime code"
			v.Confidence = ConfidenceLow
			continue
		}
		raw := runtimeCode[seg.Start:end]
		v.Location = Location{Kind: LocationCode, Offset: seg.Start, Length: seg.Length}
		if v.Type != "" {
			v.Value = decodeBytesValue(raw, v.Type)
		} else {
			v.Value = "0x" + hex.EncodeToString(raw)
		}
		out = append(out, v)
	}
	return out
}
