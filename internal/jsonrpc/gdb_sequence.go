package jsonrpc

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
)

type SequenceSession struct {
	ID               string             `json:"id"`
	StateCarry       string             `json:"stateCarry"`
	RequestedCarry   string             `json:"requestedStateCarry,omitempty"`
	Steps            []SequenceStepSpec `json:"steps"`
	CurrentStepIndex int                `json:"currentStepIndex"`
	ActiveSessionID  string             `json:"activeSessionId,omitempty"`
	LastCompletedID  string             `json:"lastCompletedSessionId,omitempty"`
	Done             bool               `json:"done"`
	ExportedPatchIDs []string           `json:"exportedPatchIds,omitempty"`
}

type SequenceStepSpec struct {
	Kind    string                 `json:"kind"`
	Request map[string]any         `json:"request,omitempty"`
	Block   any                    `json:"block,omitempty"`
	TxHash  string                 `json:"txHash,omitempty"`
	Label   string                 `json:"label,omitempty"`
	Meta    map[string]interface{} `json:"meta,omitempty"`
}

type sequenceStartRequest struct {
	Steps      []SequenceStepSpec `json:"steps"`
	StateCarry string             `json:"stateCarry"`
}

func (server *Server) startSequenceSession(ctx context.Context, params []json.RawMessage) (any, *respError) {
	if len(params) == 0 {
		return nil, &respError{Code: -32602, Message: "missing sequence configuration"}
	}
	var request sequenceStartRequest
	if err := json.Unmarshal(params[0], &request); err != nil {
		return nil, &respError{Code: -32602, Message: err.Error()}
	}
	if len(request.Steps) == 0 {
		return nil, &respError{Code: -32602, Message: "sequence requires at least one step"}
	}
	requestedCarry := normalizeCarryMode(request.StateCarry)
	if requestedCarry == "" {
		return nil, &respError{Code: -32602, Message: fmt.Sprintf("unsupported stateCarry %q", request.StateCarry)}
	}
	effectiveCarry := requestedCarry
	if requestedCarry == "full" {
		// Current backend carries state through patch primitives; keep explicit mode metadata.
		effectiveCarry = "mutation-only"
	}
	sequenceID := fmt.Sprintf("sequence-%d", atomic.AddUint64(&server.sequenceID, 1))
	sequence := &SequenceSession{
		ID:               sequenceID,
		StateCarry:       effectiveCarry,
		RequestedCarry:   requestedCarry,
		Steps:            normalizeSequenceSteps(request.Steps),
		CurrentStepIndex: 0,
	}
	session, rpcErr := server.startSequenceStep(ctx, sequence, 0)
	if rpcErr != nil {
		return nil, rpcErr
	}
	sequence.ActiveSessionID = session.ID

	server.mu.Lock()
	server.sequences[sequenceID] = sequence
	server.mu.Unlock()
	return server.describeSequence(sequence), nil
}

func (server *Server) nextStepSession(ctx context.Context, params []json.RawMessage) (any, *respError) {
	sequenceID, rpcErr := decodeStringParam(params)
	if rpcErr != nil {
		return nil, rpcErr
	}
	server.mu.Lock()
	sequence := server.sequences[sequenceID]
	server.mu.Unlock()
	if sequence == nil {
		return nil, &respError{Code: -32602, Message: "unknown sequence session"}
	}
	if sequence.Done {
		return nil, &respError{Code: -32602, Message: "sequence already completed"}
	}
	if sequence.ActiveSessionID == "" {
		return nil, &respError{Code: -32603, Message: "sequence has no active session"}
	}

	server.mu.Lock()
	active := server.sessions[sequence.ActiveSessionID]
	server.mu.Unlock()
	if active == nil {
		return nil, &respError{Code: -32603, Message: "active sequence session not found"}
	}
	if !active.Done {
		return nil, &respError{Code: -32602, Message: "active step session is not completed", Data: map[string]any{"reason": "SEQUENCE_STEP_NOT_DONE", "sessionId": active.ID}}
	}

	sequence.LastCompletedID = active.ID
	patchIDs := make([]string, 0, 1)
	if sequence.StateCarry != "none" {
		patchID, patchErr := server.exportPatchForSession(active.ID, "all")
		if patchErr != nil {
			return nil, patchErr
		}
		if patchID != "" {
			patchIDs = append(patchIDs, patchID)
			sequence.ExportedPatchIDs = append(sequence.ExportedPatchIDs, patchID)
		}
	}

	nextIndex := sequence.CurrentStepIndex + 1
	if nextIndex >= len(sequence.Steps) {
		sequence.Done = true
		sequence.ActiveSessionID = ""
		server.mu.Lock()
		server.sequences[sequence.ID] = sequence
		server.mu.Unlock()
		return server.describeSequence(sequence), nil
	}

	nextSession, startErr := server.startSequenceStep(ctx, sequence, nextIndex)
	if startErr != nil {
		return nil, startErr
	}
	if len(patchIDs) > 0 {
		if importErr := server.importPatchesIntoSession(nextSession.ID, patchIDs, "append"); importErr != nil {
			return nil, importErr
		}
	}
	sequence.CurrentStepIndex = nextIndex
	sequence.ActiveSessionID = nextSession.ID
	server.mu.Lock()
	server.sequences[sequence.ID] = sequence
	server.mu.Unlock()
	return server.describeSequence(sequence), nil
}

func (server *Server) startSequenceStep(ctx context.Context, sequence *SequenceSession, index int) (*ReplaySession, *respError) {
	if sequence == nil || index < 0 || index >= len(sequence.Steps) {
		return nil, &respError{Code: -32602, Message: "invalid sequence step index"}
	}
	step := sequence.Steps[index]
	switch step.Kind {
	case "call":
		params, rpcErr := encodeSequenceCallParams(step)
		if rpcErr != nil {
			return nil, rpcErr
		}
		result, callErr := server.startCallSession(ctx, params)
		if callErr != nil {
			return nil, callErr
		}
		sessionID, ok := result.(map[string]any)["id"].(string)
		if !ok || sessionID == "" {
			return nil, &respError{Code: -32603, Message: "failed to create call step session"}
		}
		server.mu.Lock()
		session := server.sessions[sessionID]
		server.mu.Unlock()
		if session == nil {
			return nil, &respError{Code: -32603, Message: "created call session not found"}
		}
		return session, nil
	case "replay":
		txHashText := strings.TrimSpace(step.TxHash)
		if txHashText == "" {
			if value, ok := step.Request["txHash"].(string); ok {
				txHashText = strings.TrimSpace(value)
			}
		}
		txHash, decodeErr := decodeHashParam([]json.RawMessage{mustMarshalJSON(txHashText)})
		if decodeErr != nil {
			return nil, decodeErr
		}
		result, replayErr := server.startReplaySession(ctx, txHash)
		if replayErr != nil {
			return nil, replayErr
		}
		sessionID, ok := result.(map[string]any)["id"].(string)
		if !ok || sessionID == "" {
			return nil, &respError{Code: -32603, Message: "failed to create replay step session"}
		}
		server.mu.Lock()
		session := server.sessions[sessionID]
		server.mu.Unlock()
		if session == nil {
			return nil, &respError{Code: -32603, Message: "created replay session not found"}
		}
		return session, nil
	default:
		return nil, &respError{Code: -32602, Message: fmt.Sprintf("unsupported step kind %q", step.Kind)}
	}
}

func encodeSequenceCallParams(step SequenceStepSpec) ([]json.RawMessage, *respError) {
	request := make(map[string]any)
	for key, value := range step.Request {
		request[key] = value
	}
	if _, ok := request["input"]; !ok {
		if data, ok := request["data"]; ok {
			request["input"] = data
		}
	}
	callPayload, err := json.Marshal(request)
	if err != nil {
		return nil, &respError{Code: -32602, Message: err.Error()}
	}
	block := step.Block
	if block == nil {
		block = "latest"
	}
	blockPayload, err := json.Marshal(block)
	if err != nil {
		return nil, &respError{Code: -32602, Message: err.Error()}
	}
	return []json.RawMessage{callPayload, blockPayload}, nil
}

func normalizeCarryMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "mutation-only":
		return "mutation-only"
	case "full":
		return "full"
	case "none":
		return "none"
	default:
		return ""
	}
}

func normalizeSequenceSteps(steps []SequenceStepSpec) []SequenceStepSpec {
	normalized := make([]SequenceStepSpec, 0, len(steps))
	for _, step := range steps {
		next := step
		next.Kind = strings.ToLower(strings.TrimSpace(next.Kind))
		normalized = append(normalized, next)
	}
	return normalized
}

func (server *Server) describeSequence(sequence *SequenceSession) map[string]any {
	if sequence == nil {
		return map[string]any{}
	}
	stepStates := make([]map[string]any, 0, len(sequence.Steps))
	for index, step := range sequence.Steps {
		status := "pending"
		if sequence.Done {
			if index < len(sequence.Steps) {
				status = "done"
			}
		} else if index < sequence.CurrentStepIndex {
			status = "done"
		} else if index == sequence.CurrentStepIndex {
			status = "active"
		}
		stepStates = append(stepStates, map[string]any{"index": index, "kind": step.Kind, "label": step.Label, "status": status})
	}
	state := map[string]any{
		"sequenceId":             sequence.ID,
		"requestedStateCarry":    sequence.RequestedCarry,
		"stateCarry":             sequence.StateCarry,
		"currentStepIndex":       sequence.CurrentStepIndex,
		"done":                   sequence.Done,
		"steps":                  stepStates,
		"activeSessionId":        sequence.ActiveSessionID,
		"lastCompletedSessionId": sequence.LastCompletedID,
		"exportedPatchIds":       append([]string(nil), sequence.ExportedPatchIDs...),
	}
	if sequence.ActiveSessionID != "" {
		server.mu.Lock()
		active := server.sessions[sequence.ActiveSessionID]
		server.mu.Unlock()
		if active != nil {
			state["activeSession"] = server.describeSession(active)
		}
	}
	if sequence.RequestedCarry == "full" && sequence.StateCarry == "mutation-only" {
		state["carryNotice"] = "full carry requested; current backend uses mutation patch carry"
	}
	return state
}

func mustMarshalJSON(value any) json.RawMessage {
	encoded, _ := json.Marshal(value)
	return encoded
}

func (server *Server) exportPatchForSession(sessionID string, scope string) (string, *respError) {
	result, rpcErr := server.exportStatePatch([]json.RawMessage{mustMarshalJSON(sessionID), mustMarshalJSON(map[string]any{"scope": scope})})
	if rpcErr != nil {
		return "", rpcErr
	}
	patch, ok := result.(*StatePatch)
	if !ok || patch == nil {
		return "", &respError{Code: -32603, Message: "unexpected exportStatePatch result"}
	}
	return patch.PatchID, nil
}

func (server *Server) importPatchesIntoSession(sessionID string, patchIDs []string, merge string) *respError {
	_, rpcErr := server.importStatePatch([]json.RawMessage{mustMarshalJSON(sessionID), mustMarshalJSON(map[string]any{"patches": patchIDs, "merge": merge})})
	return rpcErr
}
