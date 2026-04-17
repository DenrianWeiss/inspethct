package srcmap

import (
	"fmt"
	"strconv"
	"strings"
)

type sourceMapState struct {
	start         int
	length        int
	fileID        int
	jump          JumpType
	modifierDepth int
}

// ParseInstructionMappings decodes a Solidity bytecode source map into instruction mappings.
func ParseInstructionMappings(bytecode []byte, sourceMap string) ([]InstructionMapping, error) {
	pcs := decodeInstructionPCs(bytecode)
	if sourceMap == "" {
		mappings := make([]InstructionMapping, len(pcs))
		for i, pc := range pcs {
			mappings[i] = InstructionMapping{InstructionIndex: i, PC: pc, Opcode: bytecode[pc], Source: SourceRange{SourceID: -1}, Jump: JumpNone}
		}
		return mappings, nil
	}
	parts := strings.Split(sourceMap, ";")
	for len(parts) > len(pcs) && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	if len(parts) > len(pcs) {
		return nil, fmt.Errorf("source map has %d entries but bytecode has %d instructions", len(parts), len(pcs))
	}
	state := sourceMapState{fileID: -1, jump: JumpNone}
	mappings := make([]InstructionMapping, 0, len(pcs))
	for index, pc := range pcs {
		if index < len(parts) {
			next, err := decodeSourceMapEntry(parts[index], state)
			if err != nil {
				return nil, fmt.Errorf("decode source map entry %d: %w", index, err)
			}
			state = next
		}
		op := bytecode[pc]
		pushBytes := 0
		if op >= 0x60 && op <= 0x7f {
			pushBytes = int(op - 0x5f)
		}
		mappings = append(mappings, InstructionMapping{
			InstructionIndex: index,
			PC:               pc,
			Opcode:           op,
			PushBytes:        pushBytes,
			Source: SourceRange{
				SourceID: state.fileID,
				Start:    state.start,
				Length:   state.length,
			},
			Jump:          state.jump,
			ModifierDepth: state.modifierDepth,
		})
	}
	return mappings, nil
}

func decodeInstructionPCs(bytecode []byte) []uint64 {
	pcs := make([]uint64, 0)
	for pc := 0; pc < len(bytecode); {
		pcs = append(pcs, uint64(pc))
		op := bytecode[pc]
		pc++
		if op >= 0x60 && op <= 0x7f {
			pc += int(op - 0x5f)
		}
	}
	return pcs
}

func decodeSourceMapEntry(entry string, prev sourceMapState) (sourceMapState, error) {
	if entry == "" {
		return prev, nil
	}
	parts := strings.Split(entry, ":")
	next := prev
	if len(parts) > 0 && parts[0] != "" {
		value, err := strconv.Atoi(parts[0])
		if err != nil {
			return prev, err
		}
		next.start = value
	}
	if len(parts) > 1 && parts[1] != "" {
		value, err := strconv.Atoi(parts[1])
		if err != nil {
			return prev, err
		}
		next.length = value
	}
	if len(parts) > 2 && parts[2] != "" {
		value, err := strconv.Atoi(parts[2])
		if err != nil {
			return prev, err
		}
		next.fileID = value
	}
	if len(parts) > 3 && parts[3] != "" {
		next.jump = JumpType(parts[3])
	}
	if len(parts) > 4 && parts[4] != "" {
		value, err := strconv.Atoi(parts[4])
		if err != nil {
			return prev, err
		}
		next.modifierDepth = value
	}
	return next, nil
}