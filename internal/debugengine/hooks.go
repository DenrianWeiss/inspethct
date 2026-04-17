package debugengine

import (
	"encoding/hex"
	"fmt"

	"inspethct/internal/engine"
	"inspethct/internal/srcmap"
)

// AnnotationObserver receives source-level runtime annotations.
type AnnotationObserver interface {
	OnMemory(srcmap.MemoryAnnotation)
	OnStorage(srcmap.StorageAnnotation)
}

// ObserverFuncs adapts functions to AnnotationObserver.
type ObserverFuncs struct {
	Memory  func(srcmap.MemoryAnnotation)
	Storage func(srcmap.StorageAnnotation)
}

// OnMemory handles memory annotations.
func (o ObserverFuncs) OnMemory(annotation srcmap.MemoryAnnotation) {
	if o.Memory != nil {
		o.Memory(annotation)
	}
}

// OnStorage handles storage annotations.
func (o ObserverFuncs) OnStorage(annotation srcmap.StorageAnnotation) {
	if o.Storage != nil {
		o.Storage(annotation)
	}
}

type annotationHook struct {
	id       string
	hookType engine.HookType
	bridge   *srcmap.RuntimeBridge
	observe  AnnotationObserver
}

// NewAnnotationHooks creates engine hooks that emit source-level annotations.
func NewAnnotationHooks(prefix string, index *srcmap.Index, observer AnnotationObserver) []engine.Hook {
	bridge := srcmap.NewRuntimeBridge(index)
	return []engine.Hook{
		&annotationHook{id: prefix + ".memory.read", hookType: engine.HookTypeMemoryRead, bridge: bridge, observe: observer},
		&annotationHook{id: prefix + ".memory.write", hookType: engine.HookTypeMemoryWrite, bridge: bridge, observe: observer},
		&annotationHook{id: prefix + ".storage.read", hookType: engine.HookTypeStorageRead, bridge: bridge, observe: observer},
		&annotationHook{id: prefix + ".storage.write", hookType: engine.HookTypeStorageWrite, bridge: bridge, observe: observer},
		&annotationHook{id: prefix + ".transient.read", hookType: engine.HookTypeTransientLoad, bridge: bridge, observe: observer},
		&annotationHook{id: prefix + ".transient.write", hookType: engine.HookTypeTransientStore, bridge: bridge, observe: observer},
	}
}

func (h *annotationHook) Type() engine.HookType { return h.hookType }
func (h *annotationHook) OneTime() bool         { return false }
func (h *annotationHook) ID() string            { return h.id }

func (h *annotationHook) Fire(ctx *engine.HookContext) (*engine.HookResult, error) {
	if h.bridge == nil || h.observe == nil {
		return &engine.HookResult{Action: engine.ActionContinue}, nil
	}
	if ctx.Opcode == nil {
		return &engine.HookResult{Action: engine.ActionContinue}, nil
	}
	if ctx.Memory != nil {
		h.observe.OnMemory(h.bridge.AnnotateMemoryAccess(srcmap.MemoryAccess{
			PC:      ctx.Opcode.PC,
			Opcode:  ctx.Opcode.Op,
			Offset:  ctx.Memory.Offset,
			Size:    ctx.Memory.Size,
			IsWrite: ctx.Memory.IsWrite,
			Data:    append([]byte(nil), ctx.Memory.Data...),
		}))
	}
	if ctx.Storage != nil {
		scope, err := scopeForHookType(h.hookType)
		if err != nil {
			return nil, err
		}
		h.observe.OnStorage(h.bridge.AnnotateStorageAccess(srcmap.StorageAccess{
			PC:      ctx.Opcode.PC,
			Opcode:  ctx.Opcode.Op,
			Slot:    hashString(ctx.Storage.Slot[:]),
			IsWrite: ctx.Storage.IsWrite,
			Value:   hashString(ctx.Storage.Value[:]),
			Scope:   scope,
		}))
	}
	return &engine.HookResult{Action: engine.ActionContinue}, nil
}

func scopeForHookType(hookType engine.HookType) (srcmap.StorageScope, error) {
	switch hookType {
	case engine.HookTypeStorageRead, engine.HookTypeStorageWrite:
		return srcmap.StorageScopePersistent, nil
	case engine.HookTypeTransientLoad, engine.HookTypeTransientStore:
		return srcmap.StorageScopeTransient, nil
	default:
		return "", fmt.Errorf("hook type %d does not map to storage scope", hookType)
	}
}

func hashString(value []byte) string {
	return "0x" + hex.EncodeToString(value)
}
