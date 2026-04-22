# DBGSERVER Protocol Guide

## Purpose

`inspethctd dbgserver` is the machine-facing debug endpoint for IDE plugins.

It provides:

- step/continue debugging over JSON-RPC
- replay sessions from an on-chain transaction
- call sessions from synthetic `eth_call`-style input
- source, function, call, storage, and memory breakpoints
- paused-state mutation (storage and memory patching)

It does not expose the interactive REPL layer. The REPL is implemented by `oneshot`.

## Start Server

```bash
./inspethctd dbgserver \
  -upstream http://127.0.0.1:8545 \
  -listen 127.0.0.1:8548 \
  -block latest \
  -fork cancun
```

Important flags:

- `-upstream` upstream RPC endpoint (required)
- `-listen` dbgserver bind address
- `-block` fork block (`latest`, hex number, etc.)
- `-fork` execution fork (`london|paris|shanghai|cancun|prague|amsterdam|osaka`)
- `-mode` fork mode (`diff|pinned`)

## Transport Model

- JSON-RPC 2.0 over HTTP POST
- no websocket stream; polling is done via `gdb.state`
- each debug target is represented by a session id
- state is server-side and mutable until server restart

## Method Availability

`dbgserver` runs in `gdb-only` mode.

Allowed methods:

- `dbgserver.capabilities`
- `eth_chainId`
- `gdb.startReplaySession`
- `gdb.startCallSession`
- `gdb.next`
- `gdb.continue`
- `gdb.state`
- `gdb.loadSourceBundle`
- `gdb.setSourceBreakpoint`
- `gdb.setFunctionBreakpoint`
- `gdb.setCallBreakpoint`
- `gdb.setStorageBreakpoint`
- `gdb.setMemoryBreakpoint`
- `gdb.listBreakpoints`
- `gdb.deleteBreakpoint`
- `gdb.writeStorage`
- `gdb.writeMemory`

Calls outside this set return `-32601`.

## Session Lifecycle

### 1) Create Session

- replay session: `gdb.startReplaySession`
- call session: `gdb.startCallSession`

### 2) Optional Source Load

- load metadata with `gdb.loadSourceBundle`

### 3) Configure Breakpoints

- install breakpoint methods as needed

### 4) Drive Execution

- one-step: `gdb.next`
- run-until-pause-or-end: `gdb.continue`

### 5) Mutate While Paused

- patch storage: `gdb.writeStorage`
- patch memory: `gdb.writeMemory`

### 6) Read State

- use `gdb.state` for current server-side session snapshot

## Session Object

Most gdb methods return a session-shaped object:

- `kind` `replay` or `call`
- `id` session id
- `position` current stepped index
- `done` whether execution is completed
- `traceLength` trace length if available
- `result` final execution result when done
- `current` pause payload when paused
- `breakpoints` current breakpoints list
- `mutations` applied mutation history
- `bundles` loaded source bundles summary

For call sessions, a `call` object is included:

- `from`, `to`, `input`, `value`, `gas`, `block`

## Pause Payload

`current` contains pause details:

- `reason` pause reason
- `breakpoint` matched breakpoint display string (when applicable)
- `stepIndex`
- `contractAddress`
- `codeAddress`
- `memory`, `memorySize`
- `step` `{ pc, op, depth, gasRemaining, gasCost }`
- optional `source` source-map location
- optional decoded `storage` and `transient` variables
- optional access payloads:
  - `callAccess`
  - `storageAccess`
  - `memoryAccess`

Typical pause reasons:

- `step`
- `source_breakpoint`
- `function_breakpoint`
- `call_breakpoint`
- `storage_read_breakpoint`
- `storage_write_breakpoint`
- `memory_read_breakpoint`
- `memory_write_breakpoint`

## API Reference

### dbgserver.capabilities

Params: `[]`

Returns mode, method list, and server notes.

### gdb.startReplaySession

Params:

```json
["0x<tx_hash_32bytes>"]
```

Returns a new replay session. For non-empty traces, server performs an initial advance and may already be paused.

### gdb.startCallSession

Params:

```json
[
  {
    "from": "0x...",
    "to": "0x...",
    "input": "0x...",
    "data": "0x...",
    "value": "0x...",
    "gas": "0x..."
  },
  "latest"
]
```

Notes:

- `input` and `data` are both accepted; non-empty one is used.
- second parameter is optional block tag/number.
- `gasPrice` is accepted by parser but currently ignored for call session semantics.

### gdb.next

Params:

```json
["<session_id>"]
```

Advances by one logical step from current position.

### gdb.continue

Params:

```json
["<session_id>"]
```

Runs until next pause condition or completion.

### gdb.state

Params:

```json
["<session_id>"]
```

Returns latest session snapshot without progressing execution.

### gdb.loadSourceBundle

Params:

```json
[
  "<session_id>",
  {
    "kind": "local-project|standard-json|manual|explorer",
    "contractName": "MyContract",
    "sourceName": "src/MyContract.sol",
    "runtime": true,
    "projectRoot": "/path/to/project",
    "standardJsonPath": "/path/to/combined.json",
    "abiPath": "/path/to/abi.json",
    "address": "0x...",
    "codeAddress": "0x...",
    "apiBase": "https://api.etherscan.io/v2/api",
    "apiKey": "...",
    "rpcUrl": "https://..."
  }
]
```

Rules:

- `contractName` is required
- `kind` selects loading pipeline
- for `explorer`, contract address must be inferable from request or session context

### gdb.setSourceBreakpoint

Params:

```json
[
  "<session_id>",
  {
    "id": "optional-id",
    "address": "0x...",
    "sourceName": "src/A.sol",
    "line": 42,
    "column": 1
  }
]
```

Requires loaded source bundle.

### gdb.setFunctionBreakpoint

Params:

```json
[
  "<session_id>",
  {
    "id": "optional-id",
    "signature": "transfer(address,uint256)"
  }
]
```

### gdb.setCallBreakpoint

Params:

```json
[
  "<session_id>",
  {
    "id": "optional-id",
    "address": "0x...",
    "signature": "transfer(address,uint256)"
  }
]
```

At least one of `address` or `signature` is required.

### gdb.setStorageBreakpoint

Params:

```json
[
  "<session_id>",
  {
    "id": "optional-id",
    "address": "0x...",
    "slot": "0x...",
    "name": "owner",
    "access": "read|write|rw"
  }
]
```

At least one of `slot` or `name` is required.

### gdb.setMemoryBreakpoint

Params:

```json
[
  "<session_id>",
  {
    "id": "optional-id",
    "address": "0x...",
    "offset": 0,
    "size": 32,
    "access": "read|write|rw",
    "sourceName": "src/A.sol",
    "line": 42,
    "column": 1
  }
]
```

If `sourceName/line` are provided, breakpoint is source-scoped and requires bundle resolution.

### gdb.listBreakpoints

Params:

```json
["<session_id>"]
```

Returns flat array of breakpoint objects.

### gdb.deleteBreakpoint

Params:

```json
[
  "<session_id>",
  {
    "id": "breakpoint-id"
  }
]
```

### gdb.writeStorage

Params:

```json
[
  "<session_id>",
  {
    "address": "0x...",
    "scope": "persistent|transient",
    "slot": "0x...",
    "value": "0x..."
  }
]
```

Rules:

- session must be paused
- mutation is stored and replay-applied at the same step index in future runs

### gdb.writeMemory

Params:

```json
[
  "<session_id>",
  {
    "offset": 0,
    "data": "0xdeadbeef"
  }
]
```

Rules:

- session must be paused
- mutation is stored and replay-applied at the same step index in future runs

## Example: Minimal IDE Flow

1. call `dbgserver.capabilities`
2. create call session with `gdb.startCallSession`
3. optionally load source with `gdb.loadSourceBundle`
4. set breakpoints
5. call `gdb.continue` in loop
6. if paused, render `current` payload and allow `writeStorage/writeMemory`
7. call `gdb.continue` again until `done=true`

## Error Handling

Common JSON-RPC error codes:

- `-32601` method not found / not allowed
- `-32602` invalid params or session state (unknown session, not paused, etc.)
- `-32603` internal execution error

Plugin recommendations:

- treat `-32602` as user-actionable validation errors
- on server restart, discard cached session ids
- always refresh using `gdb.state` after any mutating method
