# DBGSERVER Upgrade Draft for IDE Sequence Debugging

## Background

Current `dbgserver` gdb-only API is sufficient for single replay/call interactive debugging,
but sequence debugging with strict state carry-over is limited.

Today, the plugin can only carry user mutation journal (`gdb.writeStorage`, `gdb.writeMemory`).
This does not represent full EVM state transition between sequence steps.

## Goal

Provide protocol primitives so IDE plugins can run multi-step call/replay sequences where
step N+1 starts from the full post-state of step N.

## Non-Goals

- Replacing current single-session APIs.
- Introducing websocket requirement.
- Exposing full database internals.

## Proposed Additions

### 1) gdb.exportStatePatch

Export a compact patch after current paused or completed session.

Params:

```json
["<session_id>", {"scope": "all|storage|transient|memory|accesses"}]
```

Result:

```json
{
  "patchId": "patch-1",
  "baseBlock": "latest",
  "stepIndex": 123,
  "storageWrites": [{"address":"0x..","scope":"persistent","slot":"0x..","value":"0x.."}],
  "transientWrites": [{"address":"0x..","slot":"0x..","value":"0x.."}],
  "memoryWrites": [{"offset":0,"data":"0x..."}],
  "metadata": {"originSession":"call-1"}
}
```

### 2) gdb.importStatePatch

Apply one or many patches before first execution step of target session.

Params:

```json
["<session_id>", {"patches":["patch-1", "patch-2"], "merge":"append|replace"}]
```

Result:

```json
{
  "sessionId": "call-2",
  "applied": ["patch-1", "patch-2"],
  "skipped": []
}
```

### 3) gdb.startSequenceSession

Optional high-level API to reduce client orchestration complexity.

Params:

```json
[
  {
    "steps": [
      {"kind":"call","request":{"from":"0x..","to":"0x..","input":"0x..","value":"0x0","gas":"0x0"},"block":"latest"},
      {"kind":"call","request":{"from":"0x..","to":"0x..","input":"0x..","value":"0x0","gas":"0x0"},"block":"latest"}
    ],
    "stateCarry": "full|mutation-only|none"
  }
]
```

Result returns sequence id and active step session summary.

### 4) gdb.nextStepSession

Advance sequence pointer from completed step to next step while preserving configured carry mode.

Params:

```json
["<sequence_id>"]
```

Result includes new active session summary and step index.

## Compatibility

- Existing methods remain unchanged.
- New methods are optional capabilities; clients should gate by `dbgserver.capabilities`.

## Capability Extension

Add structured capabilities payload:

```json
{
  "mode":"gdb-only",
  "methods":["..."],
  "features": {
    "statePatch": true,
    "sequenceSession": true,
    "tupleAbiAssist": false
  }
}
```

## Error Model

- `-32602` for invalid patch/session relation.
- `-32603` for internal merge failures.
- New domain errors in `data` field:
  - `PATCH_NOT_FOUND`
  - `PATCH_BASE_MISMATCH`
  - `SEQUENCE_STEP_NOT_DONE`

## Minimal Backend Implementation Path

1. Add in-memory patch store keyed by patchId.
2. Reuse existing mutation apply mechanism by translating patch writes into replay mutation objects.
3. Add apply-at-step-0 hook for imported patches in call/replay execution prep.
4. Add capabilities flags.

## Plugin Integration Plan

1. On each completed sequence step, call `gdb.exportStatePatch`.
2. For next step, call `gdb.importStatePatch` before first `gdb.next`/`gdb.continue`.
3. Keep current mutation-only fallback when feature flag is absent.
