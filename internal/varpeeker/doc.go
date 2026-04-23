// Package varpeeker decodes Solidity local, storage, and immutable
// variable values at a given EVM execution point.
//
// It works by combining the static index built by srcmap (source maps,
// AST, storage layout, function debug data, immutable references) with
// the runtime EVM state exposed via engine.ReadOnlyState, plus optional
// per-step access events collected by Tracker.
//
// The package is intentionally free of JSON-RPC and DAP types so it can
// be reused from the dbgserver, the oneshot CLI, and tests. Callers are
// responsible for marshalling Snapshot into their preferred wire format.
//
// Confidence levels reflect how strong each guess is:
//
//	high       resolved against tracker evidence (e.g. MSTORE of this
//	           VariableDeclaration's source range was observed and the
//	           memory still holds the expected layout)
//	medium     resolved purely from static info that the compiler
//	           guarantees (storage layout, immutable refs, solc
//	           functionDebugData parameter/return slots)
//	low        best-effort from parameter ordering on the stack; the
//	           assumption breaks down once the function body has
//	           reordered the stack
//	unavailable  declaration only, no value attempted
package varpeeker
