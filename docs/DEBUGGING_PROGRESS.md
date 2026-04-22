# Debugging Feature Progress

## Implemented

- GDB-style replay sessions now support:
  - loading source bundles from standard-json, local foundry/hardhat build-info, manual ABI input, and explorer APIs
  - source-map based breakpoints resolved to concrete PCs
  - replay-time storage and memory mutation before continuing execution
  - proxy-aware code-address mapping so delegatecall debugging can use implementation source maps while storage still resolves on the logical contract address
- One-shot local debugging mode now supports:
  - automatic project-root detection for foundry/hardhat style workspaces
  - interactive replay or local call simulation from a single CLI session
  - GDB-like commands including run/continue/next, break, info, print, x, set, load, encode, state, and restart
  - source line breakpoints via `break <file:line[:column]>` and `break line <file:line[:column]>`
  - function, call, storage, and memory access breakpoints
  - line-scoped memory breakpoints via `break memory line <file:line[:column]> [read|write|rw]`
  - offset-based memory breakpoints constrained to a source location via `break memory <offset> [size] [read|write|rw] at <file:line[:column]>`
  - paused-state memory mutation via `set memory <offset> <hex>`, `set memory last <hex>`, and `set memory line <file:line[:column]> <hex>`
  - explorer-backed source loading and ABI-driven calldata encoding inside the same interactive session
- Source metadata pipeline now extracts:
  - ABI
  - event definitions
  - function definitions
  - persistent and transient storage layout
  - source line/column lookup helpers
- Explorer integration now supports:
  - Etherscan-compatible getsourcecode APIs including Blockscout/Routescan-style bases
  - solc auto-download into the current working directory under .inspethct/solc
  - proxy detection via explorer metadata plus common implementation/beacon slots when an RPC URL is provided
- CLI is consolidated into one binary with subcommands:
  - serve
  - dbgserver
  - source local
  - source standard-json
  - source manual
  - source explorer
  - source inspect
  - openchain lookup
  - openchain decode
  - oneshot
- One-shot implementation is now isolated under `cmd/inspethctd/oneshot`, while the top-level command package only keeps a thin entrypoint.
- Gdb-style JSON-RPC sessions are now also available through the dedicated `dbgserver` subcommand, including replay sessions, call-mode sessions, and RPC-managed source/function/call/storage/memory breakpoints.
- OpenChain support now provides selector/topic lookup and best-effort calldata decoding from guessed function signatures.

## Verified

- Added unit coverage for:
  - source-breakpoint pause and storage mutation replay flow
  - local build-info loading
  - openchain calldata signature decoding helper
- Added one-shot coverage for:
  - project detection and project-root discovery
  - breakpoint spec parsing
  - paused-state memory mutation snapshot updates
- `go get github.com/ethereum/go-ethereum@v1.15.11`
- `go mod tidy`
- `go test ./...`

## Remaining Gaps

- OpenChain decoding is currently best-effort for function calldata and skips tuple signatures.
- Explorer proxy resolution currently uses explorer metadata and common storage-slot heuristics; richer `implementation()`/beacon call probing can be added later.
- One-shot memory-variable tracking is still best-effort: line-scoped memory commands currently operate on concrete runtime accesses observed at the paused PC rather than a full symbolic variable recovery model.
- Session memory snapshots currently expose full memory buffers directly; large-memory pagination can be added later if needed.