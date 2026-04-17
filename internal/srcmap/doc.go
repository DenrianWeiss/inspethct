package srcmap
// Package srcmap builds bidirectional indexes from Solidity standard-json output.
//
// It focuses on three related mapping layers:
//   - source <-> bytecode instructions via source maps
//   - source <-> storage layout via storageLayout/transientStorageLayout
//   - source <-> runtime memory access via a bridge that combines static index data
//     with runtime memory events collected from the EVM
//
// Solidity does not expose a complete static source <-> runtime memory offset map.
// For memory, this package intentionally models best-effort annotations with an
// explicit confidence level rather than pretending the mapping is exact.