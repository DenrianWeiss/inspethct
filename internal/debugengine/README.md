# DebugEngine

DebugEngine is the engine-side composition layer for source-aware debugging.

It builds on top of the base `internal/engine` package and the static mapping model in `internal/srcmap`.
Its job is to attach runtime hooks that translate EVM memory and storage accesses into source-level annotations.

Current responsibilities:

- attach srcmap-aware hooks to an `engine.EVM`
- merge existing hook registries with debug hooks
- emit source-level memory annotations
- emit source-level storage and transient-storage annotations

Current non-goals:

- upstream RPC fetching
- fork pinning and caching
- block/state synchronization

Those forked-execution concerns belong to ForkEngine and should be implemented separately together with the later jsonRPC component.

Main entry points:

- `NewEVM`: create an EVM with debug hooks already attached
- `Attach`: attach debug hooks to an existing EVM
- `NewRegistry`: create a merged hook registry
- `NewAnnotationHooks`: build the underlying source-aware hooks