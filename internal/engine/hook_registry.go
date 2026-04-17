package engine

import (
	"fmt"
	"sync"
)

// SimpleHookRegistry implements HookRegistry with thread-safe operations.
type SimpleHookRegistry struct {
	mu     sync.RWMutex
	hooks  map[string]Hook
	byType map[HookType][]Hook
}

// NewSimpleHookRegistry creates a new hook registry.
func NewSimpleHookRegistry() *SimpleHookRegistry {
	return &SimpleHookRegistry{
		hooks:  make(map[string]Hook),
		byType: make(map[HookType][]Hook),
	}
}

func (r *SimpleHookRegistry) Register(hook Hook) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.hooks[hook.ID()]; exists {
		return fmt.Errorf("hook with ID %s already registered", hook.ID())
	}
	r.hooks[hook.ID()] = hook
	r.byType[hook.Type()] = append(r.byType[hook.Type()], hook)
	return nil
}

func (r *SimpleHookRegistry) Unregister(hookID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	hook, exists := r.hooks[hookID]
	if !exists {
		return fmt.Errorf("hook with ID %s not found", hookID)
	}
	delete(r.hooks, hookID)

	// Rebuild type index
	hooks := r.byType[hook.Type()]
	newHooks := make([]Hook, 0, len(hooks)-1)
	for _, h := range hooks {
		if h.ID() != hookID {
			newHooks = append(newHooks, h)
		}
	}
	r.byType[hook.Type()] = newHooks
	return nil
}

func (r *SimpleHookRegistry) HooksFor(hookType HookType) []Hook {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Hook, len(r.byType[hookType]))
	copy(out, r.byType[hookType])
	return out
}

func (r *SimpleHookRegistry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hooks = make(map[string]Hook)
	r.byType = make(map[HookType][]Hook)
}
