package oneshot

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"sort"
	"strings"

	"inspethct/internal/engine"
	"inspethct/internal/srcmap"
)

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func formatAddress(addr engine.Address) string {
	return "0x" + hex.EncodeToString(addr[:])
}

func variablesForScopeMain(state engine.ReadOnlyState, contractAddr engine.Address, mappings []srcmap.StorageVariableMapping, scope string) []oneshotVariableValue {
	values := make([]oneshotVariableValue, 0, len(mappings))
	for _, mapping := range mappings {
		slot, err := decodeStorageSlot(mapping.Entry.Slot)
		if err != nil {
			continue
		}
		var value engine.Hash
		if scope == string(srcmap.StorageScopeTransient) {
			value = state.TransientStorageGet(contractAddr, slot)
		} else {
			value = state.StorageGet(contractAddr, slot)
		}
		values = append(values, oneshotVariableValue{Scope: scope, Name: mapping.Entry.Label, Slot: formatHash(slot), Value: formatHash(value), Type: mapping.Type.Label})
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].Scope == values[j].Scope {
			return values[i].Name < values[j].Name
		}
		return values[i].Scope < values[j].Scope
	})
	return values
}

func sourceForPCMain(index *srcmap.Index, pc uint64) map[string]any {
	if index == nil {
		return nil
	}
	mapping, ok := index.InstructionAtPC(pc)
	if !ok {
		return nil
	}
	result := map[string]any{"sourceName": index.SourceName(mapping.Source.SourceID), "line": 1, "column": 1, "pc": mapping.PC}
	if file := index.Sources[mapping.Source.SourceID]; file != nil {
		line, column := file.LineColumnForOffset(mapping.Source.Start)
		result["line"] = line
		result["column"] = column
	}
	return result
}

func decodeStorageSlot(value string) (engine.Hash, error) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(value), "0x")
	if len(trimmed)%2 == 1 {
		trimmed = "0" + trimmed
	}
	if len(trimmed) > 64 {
		return engine.Hash{}, fmt.Errorf("slot value is too long")
	}
	if len(trimmed) < 64 {
		trimmed = strings.Repeat("0", 64-len(trimmed)) + trimmed
	}
	decoded, err := hex.DecodeString(trimmed)
	if err != nil {
		return engine.Hash{}, err
	}
	var hash engine.Hash
	copy(hash[:], decoded)
	return hash, nil
}

func formatHash(value engine.Hash) string {
	return "0x" + hex.EncodeToString(value[:])
}

func cloneAddress(addr *engine.Address) *engine.Address {
	if addr == nil {
		return nil
	}
	clone := *addr
	return &clone
}

func cloneBigInt(value *big.Int) *big.Int {
	if value == nil {
		return nil
	}
	return new(big.Int).Set(value)
}

func displayOrDefault(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func maskSecret(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "<unset>"
	}
	if len(trimmed) <= 6 {
		return "***"
	}
	return trimmed[:3] + "***" + trimmed[len(trimmed)-2:]
}

func sliceMemory(memory []byte, offset uint64, size uint64) []byte {
	if size == 0 {
		return nil
	}
	end := offset + size
	if offset >= uint64(len(memory)) {
		return make([]byte, size)
	}
	if end > uint64(len(memory)) {
		out := make([]byte, size)
		copy(out, memory[offset:])
		return out
	}
	return append([]byte(nil), memory[offset:end]...)
}

func isZeroBytes(data []byte) bool {
	for _, value := range data {
		if value != 0 {
			return false
		}
	}
	return true
}

type mainMergedHookRegistry struct {
	registries []engine.HookRegistry
}

func mergeMainHookRegistries(base engine.HookRegistry, extra engine.HookRegistry) engine.HookRegistry {
	if base == nil {
		return extra
	}
	if extra == nil {
		return base
	}
	return &mainMergedHookRegistry{registries: []engine.HookRegistry{base, extra}}
}

func (registry *mainMergedHookRegistry) Register(hook engine.Hook) error {
	if len(registry.registries) == 0 {
		return fmt.Errorf("no hook registry available")
	}
	return registry.registries[len(registry.registries)-1].Register(hook)
}

func (registry *mainMergedHookRegistry) Unregister(hookID string) error {
	var lastErr error
	for _, child := range registry.registries {
		if err := child.Unregister(hookID); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	return lastErr
}

func (registry *mainMergedHookRegistry) HooksFor(hookType engine.HookType) []engine.Hook {
	var hooks []engine.Hook
	for _, child := range registry.registries {
		hooks = append(hooks, child.HooksFor(hookType)...)
	}
	return hooks
}

func (registry *mainMergedHookRegistry) Clear() {
	for _, child := range registry.registries {
		child.Clear()
	}
}

func hookTypeLabel(hookType engine.HookType) string {
	switch hookType {
	case engine.HookTypeExternalCall:
		return "call"
	case engine.HookTypeDelegateCall:
		return "delegatecall"
	case engine.HookTypeStaticCall:
		return "staticcall"
	case engine.HookTypeCallCode:
		return "callcode"
	case engine.HookTypeStorageRead:
		return "storage-read"
	case engine.HookTypeStorageWrite:
		return "storage-write"
	case engine.HookTypeTransientLoad:
		return "transient-load"
	case engine.HookTypeTransientStore:
		return "transient-store"
	case engine.HookTypeMemoryRead:
		return "memory-read"
	case engine.HookTypeMemoryWrite:
		return "memory-write"
	default:
		return fmt.Sprintf("hook-%d", hookType)
	}
}

func containsPC(pcs []uint64, pc uint64) bool {
	for _, candidate := range pcs {
		if candidate == pc {
			return true
		}
	}
	return false
}

func extractMutableState(ctx *engine.HookContext) (engine.EVMState, bool) {
	if ctx == nil || ctx.State == nil {
		return nil, false
	}
	reader, ok := ctx.State.(interface{ Inner() engine.EVMState })
	if !ok {
		return nil, false
	}
	return reader.Inner(), true
}
