# Inspethct VS Code Plugin

This extension provides interactive Solidity debugging through inspethctd dbgserver.

## Core Features

- Start dbgserver automatically with upstream RPC from launch config.
- Clean up only extension-managed processes when debug session ends.
- Replay mode and synthetic call mode with custom sender address.
- Sequence mode with optional carry-over of user-applied mutations.
- Sequence script command for repeatable multi-call debug runs.
- Visual sequence builder panel for form-based multi-step debugging.
- Solidity function entry CodeLens for one-click debug launch.
- Source breakpoints synced from VS Code editor to gdb.setSourceBreakpoint.
- Source bundle selection prompt at session launch.
- Native sequence session integration (`gdb.startSequenceSession` / `gdb.nextStepSession`) when backend supports it.
- State patch integration (`gdb.exportStatePatch` / `gdb.importStatePatch`) via backend-native sequence carry.

## Compatibility

- The extension detects `dbgserver.capabilities.features`.
- If `sequenceSession=true`, sequence debugging uses backend-native orchestration and patch carry.
- If unavailable, extension falls back to legacy per-step call sessions with mutation-journal carry.
