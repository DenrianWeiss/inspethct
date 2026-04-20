# ForkEngine

ForkEngine is the orchestration layer for executing the local EVM against upstream chain data.

It is intentionally separate from DebugEngine.

- DebugEngine is responsible for source-aware debugging hooks and srcmap annotations.
- ForkEngine is responsible for upstream reads, cache policy, overlay state, and preparing engine execution contexts from forked chain data.

ForkEngine now includes the execution-side JSON-RPC integration needed to talk to upstream Ethereum nodes. It still does not expose an external RPC server transport of its own.

## Current package split

- `internal/forkengine`: top-level configuration, mode selection, replay preparation, and execution entry points
- `internal/forkengine/cache`: cache contracts, writeback, dirty-state tracking, and state import/export
- `internal/forkengine/upstream`: abstract upstream provider contracts plus a concrete JSON-RPC client/provider
- `internal/forkengine/overlay`: local writable overlay with snapshot and revert semantics
- `internal/forkengine/ext`: reserved adapter interfaces for standard Ethereum RPC, debug RPC, and Hardhat-style extensions

## Modes

### Diff mode

- upstream data can remain live
- local writes stay in the overlay
- default block selection may float unless explicitly pinned
- suitable for exploratory debugging and non-deterministic simulations

### Pinned mode

- execution is anchored to a concrete block number
- cache keys are tied to the pinned block
- local writes still stay in the overlay unless explicitly persisted elsewhere
- suitable for deterministic reproduction and repeatable tests

## Current implementation status

Implemented now:

- upstream block selectors and provider contracts
- bounded-retry, paced, optional-batch JSON-RPC upstream client
- concrete upstream JSON-RPC provider for core `eth_*` methods and `debug_traceTransaction`
- in-memory caches with pin-aware keys
- cache writeback from local execution results
- diff-mode dirty tracking for balances, nonces, code, addresses, and storage slots
- cache state import/export interfaces for persistence or transfer
- overlay state with snapshot and revert
- top-level `forkengine.New` constructor
- cached block resolution through the upstream provider
- fork-backed account and storage adapters for engine execution
- prepared call assembly with fork-owned block and tx contexts
- direct local execution against fork-backed state through `ExecuteCall`
- replay preparation from upstream transaction and receipt data through `PrepareReplay`
- contract-creation replay preparation and execution through `PrepareReplay` plus `ExecutePreparedCall`
- prior-transaction reconstruction for mid-block replay when the upstream can return full block transaction lists
- reserved interfaces for later `eth_*`, `debug_*`, and `hardhat_*` adapters

Not implemented yet:

- Hardhat compatibility method handlers
- snapshot registry exposed through an RPC server
- source-aware debug integration between ForkEngine and DebugEngine

## Main entry points

- `New`: create a fork engine with upstream provider, cache, fork mode, and cache policy
- `PrepareCall`: build an `engine.ExecutionConfig` backed by fork state, cache, and upstream reads
- `ExecuteCall`: run the local engine directly against fork-backed account and storage adapters
- `PrepareReplay`: fetch transaction and receipt data from upstream and convert them into a replayable prepared call
- `ReplayTransaction`: prepare and execute a transaction replay, returning the local result plus exactness metadata
- `ReplayTransactionWithTraceComparison`: replay a transaction locally, fetch upstream `debug_traceTransaction`, and compare the normalized step sequence
- `CommitLocalWrites`: write local execution effects back into the fork cache and dirty-state tracker
- `ExportState` and `ImportState`: transfer cache state in and out for persistence, test fixtures, or session restore
- `ExportSnapshot` and `ImportSnapshot`: transfer cache state together with replay reconstruction metadata so prepared replay baselines can be reused

## Cache behavior

ForkEngine cache now has two layers of responsibility:

- upstream cache: stores account snapshots, contract code, storage slots, and block data keyed by block reference
- local writeback cache: records the effects of local execution and tracks which addresses and slots became dirty

In pinned mode, local writeback is enabled by default so repeated replay or simulation runs can reuse locally materialized state.

In diff mode, local writeback is configurable. When enabled, it is suitable for workflows that need to keep track of contracts, addresses, balances, code, and storage slots mutated by local transactions.

## Execution model

Fork-backed execution currently works by combining:

- upstream reads from Ethereum JSON-RPC
- pin-aware cache lookups
- lazy hydration into fork-backed account and storage adapters
- normal execution through `internal/engine`

This allows the local engine to execute contract code while resolving account code and storage data from a forked upstream environment.

For transaction replay, ForkEngine now separates:

- execution block context: the block that originally included the transaction
- state read block: currently the parent block, which avoids reading post-state from the transaction's own block

When the upstream also exposes the full transaction list for the block, ForkEngine can now reconstruct earlier transactions in order and materialize a synthetic replay state for the target transaction.

## Replay accuracy

Current replay exactness rules:

- first transaction in a block: strict receipt comparison can be meaningful with plain `eth_*` data
- mid-block transaction with block transaction list support and successful prior replay reconstruction: strict receipt comparison can be meaningful
- mid-block transaction without reconstruction support, or when a prior transaction replay diverges: replay is marked approximate and carries a limitation reason

Replay reconstruction metadata is exportable through `ExportSnapshot`, so a prepared synthetic state can be persisted and restored later without rebuilding the same prefix again.

## Engine boundary

Phase 1 does not require changes to `internal/engine`.

If later work needs additional engine capabilities, that should be proposed explicitly before changing engine interfaces.

## Replay trace tests

ForkEngine now includes two layers of replay trace testing:

- stub-backed unit tests that replay a transaction locally and compare the normalized opcode sequence against a synthetic upstream `debug_traceTransaction` result
- optional live RPC integration test that connects to a real upstream node when environment variables are set

To run the live integration test, set:

- `FORKENGINE_RPC_URL`: upstream Ethereum JSON-RPC endpoint
- `FORKENGINE_REPLAY_TX_HASH`: a non-creation transaction hash that your upstream node can serve to `eth_getTransactionByHash`, `eth_getTransactionReceipt`, and `debug_traceTransaction`

Then run:

```bash
go test ./internal/forkengine -run TestReplayTransactionWithTraceComparisonAgainstRPC -count=1
```