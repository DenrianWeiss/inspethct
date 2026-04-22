package jsonrpc

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
)

type statePatchExportRequest struct {
	Scope string `json:"scope"`
}

type statePatchImportRequest struct {
	Patches []string `json:"patches"`
	Merge   string   `json:"merge"`
}

type StatePatch struct {
	PatchID         string                   `json:"patchId"`
	BaseBlock       string                   `json:"baseBlock"`
	StepIndex       int                      `json:"stepIndex"`
	StorageWrites   []StatePatchStorageWrite `json:"storageWrites,omitempty"`
	TransientWrites []StatePatchStorageWrite `json:"transientWrites,omitempty"`
	MemoryWrites    []StatePatchMemoryWrite  `json:"memoryWrites,omitempty"`
	Metadata        map[string]any           `json:"metadata,omitempty"`
}

type StatePatchStorageWrite struct {
	Address string `json:"address"`
	Scope   string `json:"scope,omitempty"`
	Slot    string `json:"slot"`
	Value   string `json:"value"`
}

type StatePatchMemoryWrite struct {
	Offset uint64 `json:"offset"`
	Data   string `json:"data"`
}

func (server *Server) exportStatePatch(params []json.RawMessage) (any, *respError) {
	session, rpcErr := server.decodeSessionOnly(params)
	if rpcErr != nil {
		return nil, rpcErr
	}
	scope := "all"
	if len(params) >= 2 && string(params[1]) != "null" {
		var request statePatchExportRequest
		if err := json.Unmarshal(params[1], &request); err != nil {
			return nil, &respError{Code: -32602, Message: err.Error()}
		}
		scope = strings.ToLower(strings.TrimSpace(request.Scope))
		if scope == "" {
			scope = "all"
		}
	}
	if !isSupportedPatchScope(scope) {
		return nil, &respError{Code: -32602, Message: fmt.Sprintf("unsupported patch scope %q", scope)}
	}

	patch := &StatePatch{
		PatchID:   fmt.Sprintf("patch-%d", atomic.AddUint64(&server.patchID, 1)),
		BaseBlock: sessionBaseBlock(session),
		StepIndex: patchStepIndex(session),
		Metadata: map[string]any{
			"originSession": session.ID,
			"scope":         scope,
		},
	}

	for _, mutation := range session.Mutations {
		switch mutation.Kind {
		case "storage":
			if mutation.Scope == stringScopeTransient() {
				if scope == "all" || scope == "transient" {
					patch.TransientWrites = append(patch.TransientWrites, StatePatchStorageWrite{Address: mutation.Address, Slot: mutation.Slot, Value: mutation.Value})
				}
				continue
			}
			if scope == "all" || scope == "storage" {
				patch.StorageWrites = append(patch.StorageWrites, StatePatchStorageWrite{Address: mutation.Address, Scope: mutation.Scope, Slot: mutation.Slot, Value: mutation.Value})
			}
		case "memory":
			if scope == "all" || scope == "memory" {
				patch.MemoryWrites = append(patch.MemoryWrites, StatePatchMemoryWrite{Offset: mutation.Offset, Data: mutation.Data})
			}
		}
	}
	compactPatchWrites(patch)

	server.mu.Lock()
	server.patches[patch.PatchID] = cloneStatePatch(patch)
	server.mu.Unlock()
	return patch, nil
}

func (server *Server) importStatePatch(params []json.RawMessage) (any, *respError) {
	session, request, rpcErr := decodeSessionAndRequest[statePatchImportRequest](server, params)
	if rpcErr != nil {
		return nil, rpcErr
	}
	if session.Position >= 0 {
		return nil, &respError{Code: -32602, Message: "state patch import is only allowed before first execution step", Data: map[string]any{"reason": "SESSION_ALREADY_STARTED"}}
	}
	if len(request.Patches) == 0 {
		return nil, &respError{Code: -32602, Message: "patches list cannot be empty"}
	}
	mergeMode := strings.ToLower(strings.TrimSpace(request.Merge))
	if mergeMode == "" {
		mergeMode = "append"
	}
	if mergeMode != "append" && mergeMode != "replace" {
		return nil, &respError{Code: -32602, Message: fmt.Sprintf("unsupported merge mode %q", request.Merge)}
	}

	baseBlock := sessionBaseBlock(session)
	patches := make([]*StatePatch, 0, len(request.Patches))
	applied := make([]string, 0, len(request.Patches))
	for _, patchID := range request.Patches {
		server.mu.Lock()
		patch := cloneStatePatch(server.patches[patchID])
		server.mu.Unlock()
		if patch == nil {
			return nil, &respError{Code: -32602, Message: fmt.Sprintf("state patch %q not found", patchID), Data: map[string]any{"reason": "PATCH_NOT_FOUND", "patchId": patchID}}
		}
		if patch.BaseBlock != "" && patch.BaseBlock != "latest" && baseBlock != "" && baseBlock != patch.BaseBlock {
			return nil, &respError{Code: -32602, Message: fmt.Sprintf("state patch %q base mismatch", patchID), Data: map[string]any{"reason": "PATCH_BASE_MISMATCH", "patchId": patchID, "expectedBase": baseBlock, "patchBase": patch.BaseBlock}}
		}
		patches = append(patches, patch)
		applied = append(applied, patchID)
	}

	if mergeMode == "replace" {
		session.Mutations = nil
	}
	for _, patch := range patches {
		session.Mutations = append(session.Mutations, patchMutations(patch)...)
	}

	return map[string]any{"sessionId": session.ID, "applied": applied, "skipped": []string{}}, nil
}

func isSupportedPatchScope(scope string) bool {
	switch scope {
	case "all", "storage", "transient", "memory", "accesses":
		return true
	default:
		return false
	}
}

func sessionBaseBlock(session *ReplaySession) string {
	if session != nil && session.CallRequest != nil {
		return session.CallRequest.Block.CacheKey()
	}
	return "latest"
}

func patchStepIndex(session *ReplaySession) int {
	if session == nil {
		return -1
	}
	if session.Current != nil {
		return session.Current.StepIndex
	}
	if session.Position >= 0 {
		return session.Position
	}
	return -1
}

func compactPatchWrites(patch *StatePatch) {
	if patch == nil {
		return
	}
	patch.StorageWrites = compactStorageWrites(patch.StorageWrites)
	patch.TransientWrites = compactStorageWrites(patch.TransientWrites)
	patch.MemoryWrites = compactMemoryWrites(patch.MemoryWrites)
}

func compactStorageWrites(writes []StatePatchStorageWrite) []StatePatchStorageWrite {
	if len(writes) <= 1 {
		return writes
	}
	latest := make(map[string]StatePatchStorageWrite, len(writes))
	for _, write := range writes {
		latest[write.Address+"|"+write.Slot] = write
	}
	keys := make([]string, 0, len(latest))
	for key := range latest {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	compacted := make([]StatePatchStorageWrite, 0, len(keys))
	for _, key := range keys {
		compacted = append(compacted, latest[key])
	}
	return compacted
}

func compactMemoryWrites(writes []StatePatchMemoryWrite) []StatePatchMemoryWrite {
	if len(writes) <= 1 {
		return writes
	}
	latest := make(map[uint64]StatePatchMemoryWrite, len(writes))
	for _, write := range writes {
		latest[write.Offset] = write
	}
	offsets := make([]uint64, 0, len(latest))
	for offset := range latest {
		offsets = append(offsets, offset)
	}
	sort.Slice(offsets, func(i int, j int) bool { return offsets[i] < offsets[j] })
	compacted := make([]StatePatchMemoryWrite, 0, len(offsets))
	for _, offset := range offsets {
		compacted = append(compacted, latest[offset])
	}
	return compacted
}

func patchMutations(patch *StatePatch) []ReplayMutation {
	if patch == nil {
		return nil
	}
	mutations := make([]ReplayMutation, 0, len(patch.StorageWrites)+len(patch.TransientWrites)+len(patch.MemoryWrites))
	for _, write := range patch.StorageWrites {
		scope := strings.TrimSpace(write.Scope)
		if scope == "" {
			scope = stringScopePersistent()
		}
		mutations = append(mutations, ReplayMutation{Kind: "storage", StepIndex: 0, Address: write.Address, Scope: scope, Slot: write.Slot, Value: write.Value})
	}
	for _, write := range patch.TransientWrites {
		mutations = append(mutations, ReplayMutation{Kind: "storage", StepIndex: 0, Address: write.Address, Scope: stringScopeTransient(), Slot: write.Slot, Value: write.Value})
	}
	for _, write := range patch.MemoryWrites {
		mutations = append(mutations, ReplayMutation{Kind: "memory", StepIndex: 0, Offset: write.Offset, Data: write.Data})
	}
	return mutations
}

func cloneStatePatch(patch *StatePatch) *StatePatch {
	if patch == nil {
		return nil
	}
	clone := &StatePatch{
		PatchID:         patch.PatchID,
		BaseBlock:       patch.BaseBlock,
		StepIndex:       patch.StepIndex,
		StorageWrites:   append([]StatePatchStorageWrite(nil), patch.StorageWrites...),
		TransientWrites: append([]StatePatchStorageWrite(nil), patch.TransientWrites...),
		MemoryWrites:    append([]StatePatchMemoryWrite(nil), patch.MemoryWrites...),
	}
	if len(patch.Metadata) > 0 {
		clone.Metadata = make(map[string]any, len(patch.Metadata))
		for key, value := range patch.Metadata {
			clone.Metadata[key] = value
		}
	}
	return clone
}

func stringScopePersistent() string {
	return "persistent"
}

func stringScopeTransient() string {
	return "transient"
}
