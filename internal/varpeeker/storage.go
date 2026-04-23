package varpeeker

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"golang.org/x/crypto/sha3"

	"inspethct/internal/engine"
	"inspethct/internal/srcmap"
)

// peekStorageVars walks every declared storage variable for the
// requested scope and resolves a value (and, for collections, child
// elements). Mappings are returned with no children: callers must use
// PeekStorageKey to resolve a specific key.
func peekStorageVars(index *srcmap.Index, state engine.ReadOnlyState, contractAddr engine.Address, scope srcmap.StorageScope) []Variable {
	if index == nil || state == nil {
		return nil
	}
	mappings := index.StorageVariables
	if scope == srcmap.StorageScopeTransient {
		mappings = index.TransientVariables
	}
	if len(mappings) == 0 {
		return nil
	}
	out := make([]Variable, 0, len(mappings))
	kind := KindStorage
	if scope == srcmap.StorageScopeTransient {
		kind = KindTransient
	}
	for _, mapping := range mappings {
		v := decodeStorageVariable(index, state, contractAddr, mapping, scope, kind)
		out = append(out, v)
	}
	return out
}

func decodeStorageVariable(index *srcmap.Index, state engine.ReadOnlyState, contractAddr engine.Address, mapping srcmap.StorageVariableMapping, scope srcmap.StorageScope, kind VariableKind) Variable {
	rootSlot := slotFromString(mapping.Entry.Slot)
	rootHex := slotHex(rootSlot)
	v := Variable{
		Name:           mapping.Entry.Label,
		Kind:           kind,
		Type:           mapping.Type.Label,
		StorageLoc:     string(scope),
		DeclaredAtLine: 0,
		Confidence:     ConfidenceMedium,
		Location: Location{
			Kind: locationKindForScope(scope),
			Slot: rootHex,
		},
	}
	if mapping.AST != nil {
		if file := index.Sources[mapping.Root.SourceID]; file != nil {
			line, _ := file.LineColumnForOffset(mapping.Root.Start)
			v.DeclaredAtLine = line
			v.SourceID = mapping.Root.SourceID
		}
	}
	v.Value, v.Children, v.Note = decodeStorageType(index, state, contractAddr, rootSlot, uint(mapping.Entry.Offset), mapping.Type, scope)
	return v
}

// decodeStorageType returns (value, children, note) for the given
// StorageType anchored at rootSlot/offset within the slot.
func decodeStorageType(index *srcmap.Index, state engine.ReadOnlyState, contractAddr engine.Address, rootSlot *big.Int, offsetInSlot uint, t srcmap.StorageType, scope srcmap.StorageScope) (string, []Variable, string) {
	switch t.Encoding {
	case "inplace":
		return decodeInplace(index, state, contractAddr, rootSlot, offsetInSlot, t, scope)
	case "bytes":
		return decodeStorageBytes(state, contractAddr, rootSlot, t, scope)
	case "mapping":
		return "", nil, fmt.Sprintf("mapping(%s => %s); use peekStorageKey", t.Key, t.Value)
	case "dynamic_array":
		return decodeDynamicArray(index, state, contractAddr, rootSlot, t, scope)
	}
	return "", nil, "unknown encoding: " + t.Encoding
}

func decodeInplace(index *srcmap.Index, state engine.ReadOnlyState, contractAddr engine.Address, rootSlot *big.Int, offsetInSlot uint, t srcmap.StorageType, scope srcmap.StorageScope) (string, []Variable, string) {
	// Struct: walk Members.
	if len(t.Members) > 0 {
		children := make([]Variable, 0, len(t.Members))
		for _, member := range t.Members {
			memberSlot := new(big.Int).Add(rootSlot, slotFromString(member.Slot))
			memberType := index.PersistentLayout.Types[member.Type]
			if scope == srcmap.StorageScopeTransient {
				memberType = index.TransientLayout.Types[member.Type]
			}
			child := Variable{
				Name:       member.Label,
				Kind:       KindLocal,
				Type:       memberType.Label,
				StorageLoc: string(scope),
				Confidence: ConfidenceMedium,
				Location:   Location{Kind: locationKindForScope(scope), Slot: slotHex(memberSlot)},
			}
			child.Value, child.Children, child.Note = decodeStorageType(index, state, contractAddr, memberSlot, uint(member.Offset), memberType, scope)
			children = append(children, child)
		}
		return "", children, "struct"
	}
	// Fixed-size array (numberOfBytes is multiple of 32 and Base set).
	if t.Base != "" {
		baseType := index.PersistentLayout.Types[t.Base]
		if scope == srcmap.StorageScopeTransient {
			baseType = index.TransientLayout.Types[t.Base]
		}
		baseSize := parseUint(baseType.NumberOfBytes)
		total := parseUint(t.NumberOfBytes)
		if baseSize == 0 {
			return "", nil, "fixed array: unknown element size"
		}
		count := total / baseSize
		if total%baseSize != 0 {
			count = total / baseSize
		}
		children := make([]Variable, 0, count)
		// Elements packed into slots: slotsPerElement = ceil(baseSize/32)
		slotsPerElement := (baseSize + 31) / 32
		for i := uint64(0); i < count; i++ {
			elemSlot := new(big.Int).Add(rootSlot, big.NewInt(int64(i*slotsPerElement)))
			child := Variable{
				Name:       fmt.Sprintf("[%d]", i),
				Kind:       KindLocal,
				Type:       baseType.Label,
				StorageLoc: string(scope),
				Confidence: ConfidenceMedium,
				Location:   Location{Kind: locationKindForScope(scope), Slot: slotHex(elemSlot)},
			}
			child.Value, child.Children, child.Note = decodeStorageType(index, state, contractAddr, elemSlot, 0, baseType, scope)
			children = append(children, child)
		}
		return "", children, fmt.Sprintf("fixed array len=%d", count)
	}
	// Primitive at (rootSlot, offsetInSlot).
	slot := bigIntToHash(rootSlot)
	word := loadStorage(state, contractAddr, slot, scope)
	value := decodeSlotPrimitive(word, t, offsetInSlot)
	return value, nil, ""
}

func decodeStorageBytes(state engine.ReadOnlyState, contractAddr engine.Address, rootSlot *big.Int, t srcmap.StorageType, scope srcmap.StorageScope) (string, []Variable, string) {
	rootHash := bigIntToHash(rootSlot)
	word := loadStorage(state, contractAddr, rootHash, scope)
	// Solidity short/long bytes & string layout:
	//   short: [data:31][len*2 in low byte] (lowest bit of last byte = 0)
	//   long:  [length*2+1 in slot]; data starts at keccak256(slot)
	last := word[31]
	if last&1 == 0 {
		length := uint64(last) / 2
		if length > 31 {
			length = 31
		}
		data := word[:length]
		isString := strings.HasPrefix(strings.ToLower(t.Label), "string")
		if isString {
			return fmt.Sprintf("%q (len=%d)", string(data), length), nil, "short bytes"
		}
		return "0x" + hex.EncodeToString(data) + fmt.Sprintf(" (len=%d)", length), nil, "short bytes"
	}
	length := new(big.Int).SetBytes(word[:]).Uint64()
	length = (length - 1) / 2
	const maxRead uint64 = 4096
	read := length
	truncated := false
	if read > maxRead {
		read = maxRead
		truncated = true
	}
	chunkBase := keccakBig(rootSlot)
	buf := make([]byte, 0, read)
	for offset := uint64(0); offset < read; offset += 32 {
		slotIdx := new(big.Int).Add(chunkBase, big.NewInt(int64(offset/32)))
		chunk := loadStorage(state, contractAddr, bigIntToHash(slotIdx), scope)
		end := offset + 32
		if end > read {
			end = read
		}
		buf = append(buf, chunk[:end-offset]...)
	}
	suffix := ""
	if truncated {
		suffix = fmt.Sprintf("…(+%d bytes)", length-read)
	}
	isString := strings.HasPrefix(strings.ToLower(t.Label), "string")
	if isString {
		return fmt.Sprintf("%q%s (len=%d)", string(buf), suffix, length), nil, "long bytes"
	}
	return "0x" + hex.EncodeToString(buf) + suffix + fmt.Sprintf(" (len=%d)", length), nil, "long bytes"
}

func decodeDynamicArray(index *srcmap.Index, state engine.ReadOnlyState, contractAddr engine.Address, rootSlot *big.Int, t srcmap.StorageType, scope srcmap.StorageScope) (string, []Variable, string) {
	rootHash := bigIntToHash(rootSlot)
	header := loadStorage(state, contractAddr, rootHash, scope)
	length := new(big.Int).SetBytes(header[:]).Uint64()
	if length == 0 {
		return "len=0", nil, ""
	}
	const maxElements uint64 = 256
	render := length
	truncated := false
	if render > maxElements {
		render = maxElements
		truncated = true
	}
	baseType := index.PersistentLayout.Types[t.Base]
	if scope == srcmap.StorageScopeTransient {
		baseType = index.TransientLayout.Types[t.Base]
	}
	baseSize := parseUint(baseType.NumberOfBytes)
	if baseSize == 0 {
		baseSize = 32
	}
	slotsPerElement := (baseSize + 31) / 32
	dataBase := keccakBig(rootSlot)
	children := make([]Variable, 0, render)
	for i := uint64(0); i < render; i++ {
		elemSlot := new(big.Int).Add(dataBase, big.NewInt(int64(i*slotsPerElement)))
		child := Variable{
			Name:       fmt.Sprintf("[%d]", i),
			Kind:       KindLocal,
			Type:       baseType.Label,
			StorageLoc: string(scope),
			Confidence: ConfidenceMedium,
			Location:   Location{Kind: locationKindForScope(scope), Slot: slotHex(elemSlot)},
		}
		child.Value, child.Children, child.Note = decodeStorageType(index, state, contractAddr, elemSlot, 0, baseType, scope)
		children = append(children, child)
	}
	note := fmt.Sprintf("dynamic array len=%d", length)
	if truncated {
		note += fmt.Sprintf(" (showing first %d)", render)
	}
	return note, children, ""
}

// PeekStorageKey resolves mapping[key] for the given top-level mapping
// variable. The caller supplies the raw key bytes (already padded to 32
// for value types or RLP-style for bytes/string keys).
func PeekStorageKey(index *srcmap.Index, state engine.ReadOnlyState, contractAddr engine.Address, varName string, key []byte) (Variable, bool) {
	if index == nil {
		return Variable{}, false
	}
	for _, m := range index.StorageVariables {
		if m.Entry.Label != varName {
			continue
		}
		if m.Type.Encoding != "mapping" {
			return Variable{}, false
		}
		valueType := index.PersistentLayout.Types[m.Type.Value]
		root := slotFromString(m.Entry.Slot)
		buf := make([]byte, 0, 64)
		// Pad key to 32.
		if len(key) > 32 {
			buf = append(buf, key...)
		} else {
			pad := make([]byte, 32)
			copy(pad[32-len(key):], key)
			buf = append(buf, pad...)
		}
		buf = append(buf, padTo32(root.Bytes())...)
		h := sha3.NewLegacyKeccak256()
		_, _ = h.Write(buf)
		entrySlot := new(big.Int).SetBytes(h.Sum(nil))
		v := Variable{
			Name:       fmt.Sprintf("%s[0x%s]", varName, hex.EncodeToString(key)),
			Kind:       KindStorage,
			Type:       valueType.Label,
			StorageLoc: "storage",
			Confidence: ConfidenceMedium,
			Location:   Location{Kind: LocationStorage, Slot: slotHex(entrySlot)},
		}
		v.Value, v.Children, v.Note = decodeStorageType(index, state, contractAddr, entrySlot, 0, valueType, srcmap.StorageScopePersistent)
		return v, true
	}
	return Variable{}, false
}

// decodeSlotPrimitive picks the right bytes from a 32-byte slot for a
// primitive type with packing offset/size.
func decodeSlotPrimitive(word engine.Hash, t srcmap.StorageType, offsetInSlot uint) string {
	size := parseUint(t.NumberOfBytes)
	if size == 0 || size > 32 {
		size = 32
	}
	// Solidity packs LSB-first: a variable at offset O of size S occupies
	// bytes [32-O-S : 32-O] of the big-endian slot.
	if uint64(offsetInSlot)+size > 32 {
		return "0x" + hex.EncodeToString(word[:])
	}
	start := 32 - uint64(offsetInSlot) - size
	end := 32 - uint64(offsetInSlot)
	slice := word[start:end]
	return decodeBytesValue(slice, t.Label)
}

func loadStorage(state engine.ReadOnlyState, addr engine.Address, slot engine.Hash, scope srcmap.StorageScope) engine.Hash {
	if scope == srcmap.StorageScopeTransient {
		return state.TransientStorageGet(addr, slot)
	}
	return state.StorageGet(addr, slot)
}

func locationKindForScope(scope srcmap.StorageScope) LocationKind {
	if scope == srcmap.StorageScopeTransient {
		return LocationTransient
	}
	return LocationStorage
}

func slotFromString(s string) *big.Int {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return new(big.Int)
	}
	base := 10
	if strings.HasPrefix(trimmed, "0x") || strings.HasPrefix(trimmed, "0X") {
		trimmed = trimmed[2:]
		base = 16
	}
	v, ok := new(big.Int).SetString(trimmed, base)
	if !ok {
		return new(big.Int)
	}
	return v
}

func slotHex(v *big.Int) string {
	if v == nil {
		return "0x0"
	}
	b := v.Bytes()
	if len(b) == 0 {
		return "0x0"
	}
	return "0x" + hex.EncodeToString(b)
}

func bigIntToHash(v *big.Int) engine.Hash {
	var h engine.Hash
	if v == nil {
		return h
	}
	b := v.Bytes()
	if len(b) >= 32 {
		copy(h[:], b[len(b)-32:])
		return h
	}
	copy(h[32-len(b):], b)
	return h
}

func padTo32(b []byte) []byte {
	if len(b) >= 32 {
		return b
	}
	out := make([]byte, 32)
	copy(out[32-len(b):], b)
	return out
}

func keccakBig(slot *big.Int) *big.Int {
	h := sha3.NewLegacyKeccak256()
	_, _ = h.Write(padTo32(slot.Bytes()))
	return new(big.Int).SetBytes(h.Sum(nil))
}

func parseUint(s string) uint64 {
	if s == "" {
		return 0
	}
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return 0
	}
	return v.Uint64()
}
