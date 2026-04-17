package debugengine

import (
	"inspethct/internal/engine"
	"inspethct/internal/srcmap"
)

// NewRegistry returns a hook registry containing any existing hooks plus srcmap annotation hooks.
func NewRegistry(index *srcmap.Index, observer AnnotationObserver, existing engine.HookRegistry) (engine.HookRegistry, error) {
	registry := engine.NewSimpleHookRegistry()
	if existing != nil {
		for hookType := engine.HookTypeOpcode; hookType <= engine.HookTypeStep; hookType++ {
			for _, hook := range existing.HooksFor(hookType) {
				if err := registry.Register(hook); err != nil {
					return nil, err
				}
			}
		}
	}
	for _, hook := range NewAnnotationHooks("srcmap", index, observer) {
		if err := registry.Register(hook); err != nil {
			return nil, err
		}
	}
	return registry, nil
}
