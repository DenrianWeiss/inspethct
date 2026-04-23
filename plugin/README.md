# Inspethct VS Code Plugin

This extension provides interactive Solidity debugging through inspethctd dbgserver. Please notice that this plugin need corresponding dbgserver to work, you can download it manually.

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
- Cross-contract debugging: source and function breakpoints fire inside CALL/STATICCALL/DELEGATECALL/CREATE targets, not just the entry transaction.
- Live call stack: per-frame contract address, call type, and selector are surfaced in the VS Code Call Stack view (parent frames shown as label entries) and in the REPL via `info frames`.
- Improved local variable decoding: when solc emits `functionDebugData`, parameter and named-return values are resolved using exact stack-slot counts and reported with `confidence=medium`.

## Compiler configuration for richer locals

To enable medium-confidence local decoding, ask the Solidity compiler to emit `functionDebugData`:

- **Foundry** (`foundry.toml`):
  ```toml
  extra_output = [
    "evm.bytecode.functionDebugData",
    "evm.deployedBytecode.functionDebugData",
    "evm.deployedBytecode.immutableReferences"
  ]
  ```
- **Hardhat** (`hardhat.config.{js,ts}`):
  ```js
  solidity: {
    settings: {
      outputSelection: {
        "*": {
          "*": [
            "evm.bytecode.functionDebugData",
            "evm.deployedBytecode.functionDebugData",
            "evm.deployedBytecode.immutableReferences"
          ]
        }
      }
    }
  }
  ```

Without these the plugin falls back to AST-derived counts (`confidence=low`) — debugging still works, just less precise for via-IR / multi-slot ABI types.

## Compatibility

- The extension detects `dbgserver.capabilities.features`.
- If `sequenceSession=true`, sequence debugging uses backend-native orchestration and patch carry.
- If unavailable, extension falls back to legacy per-step call sessions with mutation-journal carry.
