# ONESHOT Debugger Guide

## Purpose

`inspethctd oneshot` is the human-facing debugger.

It provides an interactive REPL to:

- replay an existing transaction
- simulate a contract call
- debug with source maps when metadata is available
- inspect state and patch memory/config during a paused run

For IDE/plugin integration, prefer DBGSERVER. ONESHOT is still useful as the reference UX and command grammar.

## Start

```bash
./inspethctd oneshot \
  -project . \
  -upstream http://127.0.0.1:8545 \
  -block latest \
  -fork cancun
```

Important flags:

- `-project` project root or any subdirectory inside target project
- `-upstream` upstream RPC endpoint (required)
- `-block` fork block reference
- `-fork` execution fork
- `-mode` fork mode (`diff|pinned`)
- `-chain-id` optional chain id override
- `-explorer-api-base`, `-explorer-api-key`, `-explorer-chain-id` explorer source loading config

## Startup Flow

At startup, ONESHOT walks through:

1. choose replay or call mode
2. collect tx hash or call target/input/value
3. optional source bundle loading
4. enter REPL

If source is not loaded, ONESHOT prints explicit guidance for source-free breakpoint types.

## Command Groups

### Execution Control

- `run` (`r`): restart from beginning and run until pause/end
- `continue` (`c`): continue from current pause
- `next` (`n`, `step`, `s`): single-step
- `restart`: reset execution progress
- `state`: print current status summary
- `quit` (`q`, `exit`): leave session

### Help

- `help`
- `help <command>`
- `<command> help`
- `<command> <subcommand> help`

### Breakpoints

- source line:
  - `break <file:line[:column]>`
  - `break line <file:line[:column]>`
- function selector:
  - `break function <signature>`
- external call:
  - `break call <address> [signature]`
- storage access:
  - `break storage <slot|name> [read|write|rw]`
- memory access:
  - `break memory <offset> [size] [read|write|rw]`
  - `break memory <offset> [size] [read|write|rw] at <file:line[:column]>`
  - `break memory line <file:line[:column]> [read|write|rw]`

### Source Loading

- `load local [contract] [source]`
- `load explorer <address> [contract] [source]`
- `load clear`

### Inspection

- `info breakpoints`
- `info config`
- `info bundle`
- `info storage`
- `info memory [limit]`
- `print storage <name|slot>`
- `print memory <offset> <size>`
- `x <offset> <size>`

### Mutation / Config

- `set config upstream <url>`
- `set config explorer.api-base <url>`
- `set config explorer.api-key <key>`
- `set config explorer.chain-id <id>`
- `set config engine.chain-id <id>`
- `set call <signature> [args...]` (call mode)
- `set memory <offset> <hex-data>`
- `set memory last <hex-data>`
- `set memory line <file:line[:column]> <hex-data>`

### ABI Encode Utility

- `encode <signature> [args...]`

In call mode, this can also update the active call input.

## Breakpoint Capability Matrix

Works without source bundle:

- `break function`
- `break call`
- `break storage` (slot-based form is best)
- `break memory` (offset-based)

Requires source bundle:

- source line breakpoints
- line-scoped memory breakpoints (`at ...` or `memory line ...`)
- storage name resolution by variable name

## Paused-State Mutation Semantics

### Memory Patching

- immediately updates the currently paused memory snapshot
- also stored as a mutation event and re-applied on the same step during subsequent replays

### Storage Patching

- applies to persistent or transient scope depending on command context
- also persisted in mutation history for deterministic replay behavior

## Typical Workflows

### Replay + Source Line Debugging

```text
Transaction hash (leave blank to simulate a call): 0x...
oneshot> load local MyContract src/MyContract.sol
oneshot> break src/MyContract.sol:120
oneshot> run
oneshot> next
oneshot> info storage
```

### Call Mode + Selector Breakpoint

```text
Transaction hash (leave blank to simulate a call):
oneshot> set call transfer(address,uint256) 0xabc... 1000000000000000000
oneshot> break function transfer(address,uint256)
oneshot> run
```

### Memory Access Patch Loop

```text
oneshot> break memory 0x00 32 write
oneshot> run
oneshot> set memory last 0x0000000000000000000000000000000000000000000000000000000000000001
oneshot> continue
```

## REPL Output Model (for UX Design)

At pause points, ONESHOT exposes enough data for IDE-like displays:

- current opcode / pc / depth / gas
- source location and AST metadata (if bundle loaded)
- storage/transient variable snapshots
- latest memory access and current memory snapshot

This output model is the CLI counterpart of DBGSERVER `current` payload.

## Relationship to DBGSERVER

- ONESHOT: interactive local operator UX
- DBGSERVER: machine API for plugins

If implementing a plugin, use DBGSERVER as protocol source and ONESHOT as UX behavior reference.

## Code Locations

- oneshot entrypoint and REPL: [cmd/inspethctd/oneshot/oneshot.go](cmd/inspethctd/oneshot/oneshot.go)
- help and command docs: [cmd/inspethctd/oneshot/oneshot_help.go](cmd/inspethctd/oneshot/oneshot_help.go)
- runtime/session behavior: [cmd/inspethctd/oneshot/oneshot_runtime.go](cmd/inspethctd/oneshot/oneshot_runtime.go)