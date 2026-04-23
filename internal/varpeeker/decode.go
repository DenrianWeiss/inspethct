package varpeeker

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"inspethct/internal/engine"
)

// hexWord renders a 32-byte word as a 0x-prefixed hex string.
func hexWord(w engine.Word) string {
	return "0x" + hex.EncodeToString(w[:])
}

// decodeStackValue maps a 32-byte stack word to a human-readable value
// for the given Solidity typeString. storageLoc selects the
// reference-vs-value interpretation.
//
// Returns (value, optional memory pointer, note). The note carries
// caveats like "memory pointer", "storage slot", "raw stack word".
func decodeStackValue(word engine.Word, typeStr, storageLoc string, state engine.ReadOnlyState) (value string, ptr uint64, note string) {
	t := strings.TrimSpace(strings.ToLower(typeStr))
	wordHex := hexWord(word)

	switch storageLoc {
	case "memory":
		offset := word.ToBig().Uint64()
		if t == "bytes" || strings.HasPrefix(t, "bytes ") || strings.HasPrefix(t, "string") {
			return decodeMemoryDynamic(state, offset, strings.HasPrefix(t, "string"))
		}
		return wordHex, offset, "memory pointer"
	case "storage", "storage pointer", "storage ref":
		return wordHex, 0, "storage slot"
	case "calldata":
		return wordHex, 0, "calldata offset"
	}

	switch {
	case strings.HasPrefix(t, "address"):
		return "0x" + hex.EncodeToString(word[12:]), 0, ""
	case t == "bool":
		for _, b := range word {
			if b != 0 {
				return "true", 0, ""
			}
		}
		return "false", 0, ""
	case strings.HasPrefix(t, "uint"):
		return word.ToBig().String() + " (" + wordHex + ")", 0, ""
	case strings.HasPrefix(t, "int"):
		bits := parseIntBits(t)
		return signedFromWord(word, bits).String() + " (" + wordHex + ")", 0, ""
	case strings.HasPrefix(t, "bytes") && len(t) > len("bytes"):
		n := parseBytesN(t)
		if n > 0 && n <= 32 {
			return "0x" + hex.EncodeToString(word[:n]), 0, ""
		}
	case strings.HasPrefix(t, "contract"):
		return "0x" + hex.EncodeToString(word[12:]), 0, "contract reference"
	case strings.HasPrefix(t, "function"):
		return wordHex, 0, "function pointer"
	case strings.HasPrefix(t, "enum"):
		return word.ToBig().String(), 0, ""
	}
	return wordHex, 0, "raw stack word"
}

// decodeMemoryDynamic reads a Solidity dynamic bytes/string from memory
// at offset. Layout: [length:32][data:length].
func decodeMemoryDynamic(state engine.ReadOnlyState, offset uint64, isString bool) (string, uint64, string) {
	if state == nil {
		return "", offset, ""
	}
	memLen := uint64(state.MemoryLen())
	if offset+32 > memLen {
		return "", offset, "pointer beyond memory"
	}
	header := state.MemoryGet(offset, 32)
	length := new(big.Int).SetBytes(header).Uint64()
	const maxRead uint64 = 4096
	read := length
	if read > maxRead {
		read = maxRead
	}
	if offset+32+read > memLen {
		if memLen > offset+32 {
			read = memLen - (offset + 32)
		} else {
			read = 0
		}
	}
	data := state.MemoryGet(offset+32, read)
	if isString {
		s := string(data)
		if length > read {
			s += fmt.Sprintf("…(+%d bytes)", length-read)
		}
		return strconv.Quote(s) + fmt.Sprintf(" (len=%d)", length), offset, "memory string"
	}
	return "0x" + hex.EncodeToString(data) + fmt.Sprintf(" (len=%d)", length), offset, "memory bytes"
}

// decodeBytesValue decodes raw bytes (already aligned) as the given
// Solidity primitive type. Used by storage/immutable readers where the
// value is already loaded into a 32-byte slot or contiguous span.
func decodeBytesValue(data []byte, typeStr string) string {
	if len(data) == 0 {
		return ""
	}
	t := strings.TrimSpace(strings.ToLower(typeStr))
	// Pad to 32 if needed (left-padded, big-endian).
	var word [32]byte
	if len(data) >= 32 {
		copy(word[:], data[len(data)-32:])
	} else {
		copy(word[32-len(data):], data)
	}
	w := engine.Word(word)
	hexAll := "0x" + hex.EncodeToString(word[:])

	switch {
	case strings.HasPrefix(t, "address"), strings.HasPrefix(t, "contract"):
		return "0x" + hex.EncodeToString(word[12:])
	case t == "bool":
		for _, b := range word {
			if b != 0 {
				return "true"
			}
		}
		return "false"
	case strings.HasPrefix(t, "uint"):
		return w.ToBig().String() + " (" + hexAll + ")"
	case strings.HasPrefix(t, "int"):
		return signedFromWord(w, parseIntBits(t)).String() + " (" + hexAll + ")"
	case strings.HasPrefix(t, "bytes") && len(t) > len("bytes"):
		n := parseBytesN(t)
		if n > 0 && n <= 32 {
			// bytesN is right-padded, so take the leading n bytes when
			// the input was a full 32-byte word; if input was shorter,
			// just hex it.
			if len(data) == 32 {
				return "0x" + hex.EncodeToString(data[:n])
			}
			return "0x" + hex.EncodeToString(data)
		}
	case strings.HasPrefix(t, "enum"):
		return w.ToBig().String()
	}
	return hexAll
}

func parseIntBits(t string) int {
	rest := strings.TrimPrefix(t, "int")
	if rest == "" {
		return 256
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n <= 0 || n > 256 {
		return 256
	}
	return n
}

func parseBytesN(t string) int {
	rest := strings.TrimPrefix(t, "bytes")
	n, err := strconv.Atoi(rest)
	if err != nil {
		return 0
	}
	return n
}

func signedFromWord(word engine.Word, bits int) *big.Int {
	v := new(big.Int).SetBytes(word[:])
	if bits <= 0 || bits > 256 {
		bits = 256
	}
	signBit := new(big.Int).Lsh(big.NewInt(1), uint(bits-1))
	if v.Cmp(signBit) >= 0 {
		mod := new(big.Int).Lsh(big.NewInt(1), uint(bits))
		v.Sub(v, mod)
	}
	return v
}

// isReferenceTypeString is true when the given Solidity type denotes a
// reference (string/bytes/array/struct/mapping). Used by callers that
// treat the stack word as a memory or storage pointer rather than a
// scalar.
func isReferenceTypeString(t string) bool {
	low := strings.TrimSpace(strings.ToLower(t))
	if low == "" {
		return false
	}
	switch {
	case low == "bytes" || strings.HasPrefix(low, "string"):
		return true
	case strings.HasPrefix(low, "struct "):
		return true
	case strings.HasPrefix(low, "mapping"):
		return true
	case strings.Contains(low, "[]"), strings.Contains(low, "["):
		return true
	}
	return false
}
