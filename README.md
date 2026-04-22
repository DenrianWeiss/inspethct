# Inspethct

Interactive ETH debugger and EVM replay tool.

## Highlights

- Replay any historical transaction or simulate a fresh call against a forked state.
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
