# Inspethct

Interactive ETH debugger and EVM replay tool.

## Highlights

- Replay any historical transaction or simulate a fresh call against a forked state.
- Interactive debugging: Unique ability to modify state and memory on the fly while paused, without restarting the session.
- Sequence mode: chain multiple calls and carry state mutations across them.
- Cross-contract debugging: breakpoints fire inside CALL/STATICCALL/DELEGATECALL/CREATE sub-frames, with full per-frame call stack (contract address, call type, selector) surfaced through the VS Code DAP view and REPL `info frames`.
- Compiler-assisted local decoding: when solc emits `functionDebugData`, parameter/return locals are resolved via exact stack-slot counts (`confidence=medium`) instead of AST guesses (`confidence=low`).
- Foundry / Hardhat build-info ingestion: standard-json output flows through transparently — opt in to richer locals by adding `functionDebugData` (and optionally `immutableReferences`) to your project's compiler output selection.

## Compiler output for richer debugging

Add to **`foundry.toml`**:

```toml
extra_output = [
  "evm.bytecode.functionDebugData",
  "evm.deployedBytecode.functionDebugData",
  "evm.deployedBytecode.immutableReferences"
]
```

Or in **Hardhat**:

```js
solidity: {
  settings: {
    outputSelection: {
      "*": { "*": [
        "evm.bytecode.functionDebugData",
        "evm.deployedBytecode.functionDebugData",
        "evm.deployedBytecode.immutableReferences"
      ] }
    }
  }
}
```

See [plugin/README.md](plugin/README.md) and [plugin/USAGE.md](plugin/USAGE.md) for the VS Code workflow.


## Usage

### VS Code Extension

Install the vsix from the [latest release](https://github.com/DenrianWeiss/inspethct/releases) and open your settings, you need to configure at least the `inspethct.upstream` RPC URL and optionally the `inspethct.binaryPath` to the dbgserver binary.

Notice that several features do not present when running with the VS Code plugin, for example, features that patch the state during the run.

### CLI

Run `./inspethctd oneshot -h` for the CLI usage and options. The CLI provides the same core debugging experience as the VS Code extension, but provides direct access to the interactive REPL and additional commands for source bundle management.

### As a library

You could use this repo source to develop your own custom tooling on top of the core debugging engine. For example, you could only use the engine as a extensible EVM execution tracer, or build your own custom REPL on top of the JSON-RPC interface provided by the dbgserver, or add your own hooks to the execution loop for testing or monitoring purposes.