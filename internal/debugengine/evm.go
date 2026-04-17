package debugengine

import (
	"inspethct/internal/engine"
	"inspethct/internal/srcmap"
)

// Attach merges srcmap annotation hooks into the provided registry and installs it on the EVM.
func Attach(evm *engine.EVM, index *srcmap.Index, observer AnnotationObserver, existing engine.HookRegistry) error {
	if evm == nil {
		return nil
	}
	registry, err := NewRegistry(index, observer, existing)
	if err != nil {
		return err
	}
	evm.SetHooks(registry)
	return nil
}

// NewEVM constructs an EVM and attaches srcmap annotation hooks.
func NewEVM(state engine.EVMState, fork engine.Fork, index *srcmap.Index, observer AnnotationObserver, existing engine.HookRegistry, registries ...*engine.PrecompileRegistry) (*engine.EVM, error) {
	evm := engine.NewEVM(state, fork, registries...)
	if err := Attach(evm, index, observer, existing); err != nil {
		return nil, err
	}
	return evm, nil
}
