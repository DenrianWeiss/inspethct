# Inspethct Engine

Engine provides the core EVM functionality for Inspethct, a hookable EVM implementation for debugging and testing purposes. It is designed to be modular and extensible.

## Features

- **EVM Execution**: Executes EVM bytecode with support for all opcodes and EVM versions from London onward. The EVM version must be set before execution and cannot be changed during execution.
- **Hookable**: Supports hooks and breakpoints at specific opcodes, external calls, delegate calls, static calls, storage reads/writes, memory reads/writes, logs, and self-destructs. Hooks can inspect and replace operation results, then continue execution. Hooks may be one-time or persistent and can be removed after creation. Hooks can read and write EVM state including time, block info, storage, memory, stack, etc.
- **Tracing**: Supports tracing of EVM execution including opcode execution, external calls, delegate calls, static calls, storage reads/writes, memory reads/writes, etc. Traces can be used for debugging and testing and exported for further analysis.

---

## Architecture

The engine is organized into several interface-driven packages:

```
engine/
  types.go      — Core types (Word, Address, Hash, Fork, Log)
  state.go      — EVM state interfaces (stack, memory, storage, accounts, gas, context)
  hook.go       — Hook standard, registry, and conditional hooks
  result.go     — Execution result, status codes, builder, and accessor
  breakpoint.go — Breakpoint manager with stepping controls
  engine.go     — Engine and Debugger interfaces
```

### Design Principles

1. **Interface-first**: All major components are defined as Go interfaces, allowing custom implementations for testing, simulation, or specialized environments.
2. **Immutable reads, explicit writes**: Read-only views (`ReadOnlyState`) are passed to hooks and tracers, while mutable state (`EVMState`) is available only to the engine and debugger.
3. **Composable hooks**: Hooks are discrete units that can be combined, conditioned, and managed via a registry.
4. **First-class debugging**: Breakpoints are hooks with special semantics, integrated into a `Debugger` interface that supports step-into, step-over, and step-out.

---

## EVM State

The EVM state is modeled as a collection of specialized interfaces rather than a single monolithic struct:

### Stack
- `Len() int`
- `Push(word Word)`
- `Pop() Word`
- `Peek() Word`
- `PeekN(n int) Word`
- `Swap(n int)`
- `Dup(n int)`

### Memory
- `Len() int`
- `Get(offset, size uint64) []byte`
- `Set(offset uint64, data []byte) error`
- `Resize(size uint64) error`
- `Word(offset uint64) Word`

### Storage
- `Get(addr Address, slot Hash) Hash`
- `Set(addr Address, slot Hash, value Hash)`
- `Original(addr Address, slot Hash) Hash`

### Transient Storage (Cancun+)
- `Get(addr Address, slot Hash) Hash`
- `Set(addr Address, slot Hash, value Hash)`

### Account State
- `Balance(addr Address) *big.Int`
- `Nonce(addr Address) uint64`
- `Code(addr Address) []byte`
- `CodeHash(addr Address) Hash`
- `Exists(addr Address) bool`
- `SetBalance`, `AddBalance`, `SubBalance`
- `SetNonce`, `IncrementNonce`
- `SetCode`
- `SelfDestruct(addr Address)`
- `CreateAccount(addr Address)`

### Block Context
- `Coinbase() Address`
- `Timestamp() uint64`
- `Number() *big.Int`
- `Difficulty() *big.Int`
- `GasLimit() uint64`
- `BlockHash(number uint64) Hash`
- `BaseFee() *big.Int`
- `BlobBaseFee() *big.Int`
- `BlobHash(index uint64) Hash`
- `Random() Hash`
- `ChainID() *big.Int`

### Transaction Context
- `Origin() Address`
- `GasPrice() *big.Int`
- `BlobHashes() []Hash`
- `BlobGasFee() *big.Int`

### Gas Meter
- `Gas() uint64`
- `ConsumeGas(amount uint64) error`
- `RefundGas(amount uint64)`
- `Refund() uint64`

### Access List (Berlin+)
- `IsAddressWarmed(addr Address) bool`
- `WarmAddress(addr Address)`
- `IsSlotWarmed(addr Address, slot Hash) bool`
- `WarmSlot(addr Address, slot Hash)`

### Contract Context
- `Address() Address`
- `Caller() Address`
- `CallValue() *big.Int`
- `CallInput() []byte`
- `Code() []byte`
- `CodeHash() Hash`
- `CodeAddr() Address`
- `IsStatic() bool`

### Aggregated State

`EVMState` exposes all of the above, plus:
- `PC() uint64`
- `ReturnData() []byte`
- `SetReturnData(data []byte)`
- `CallDepth() int`
- `Logs() []Log`
- `AddLog(log Log)`

`ReadOnlyState` provides a safe, read-only snapshot suitable for hooks and tracers.

---

## Hook Standard

Hooks allow external logic to intercept and influence EVM execution at well-defined points.

### Hook Types

| Type | Trigger |
|------|---------|
| `HookTypeOpcode` | Before any opcode executes |
| `HookTypeExternalCall` | On `CALL` |
| `HookTypeDelegateCall` | On `DELEGATECALL` |
| `HookTypeStaticCall` | On `STATICCALL` |
| `HookTypeCallCode` | On `CALLCODE` |
| `HookTypeCreate` | On `CREATE` / `CREATE2` |
| `HookTypeStorageRead` | On `SLOAD` |
| `HookTypeStorageWrite` | On `SSTORE` |
| `HookTypeTransientLoad` | On `TLOAD` (Cancun+) |
| `HookTypeTransientStore` | On `TSTORE` (Cancun+) |
| `HookTypeMemoryRead` | On any memory read |
| `HookTypeMemoryWrite` | On any memory write |
| `HookTypeLog` | On `LOG0`–`LOG4` |
| `HookTypeSelfDestruct` | On `SELFDESTRUCT` |
| `HookTypeReturn` | On `RETURN` / `REVERT` |
| `HookTypeStep` | Every instruction step |

### Hook Actions

A hook returns a `HookResult` with one of these actions:

| Action | Behavior |
|--------|----------|
| `ActionContinue` | Resume normal execution |
| `ActionReplaceResult` | Skip the original operation and use `ResultData` instead |
| `ActionHalt` | Stop execution with `Err` |
| `ActionRevert` | Revert the current call frame with `ResultData` |

### Hook Interface

```go
type Hook interface {
    Type() HookType
    Fire(ctx *HookContext) (*HookResult, error)
    OneTime() bool
    ID() string
}
```

### Conditional Hooks

Hooks can be wrapped with a `HookCondition` predicate so they only fire when specific criteria are met (e.g., specific opcode, address, gas range).

### Hook Registry

`HookRegistry` manages the lifecycle of hooks:
- `Register(hook Hook) error`
- `Unregister(hookID string) error`
- `HooksFor(hookType HookType) []Hook`
- `Clear()`

---

## Result and State Access Interface

### Execution Result

`ExecutionResult` captures the full outcome of a message execution:

- `Status` — `StatusSuccess`, `StatusRevert`, `StatusHalt`, `StatusOutOfGas`, etc.
- `GasUsed`, `GasRemaining`, `GasRefund`
- `ReturnData`
- `Logs`
- `CreatedAddress`
- `StateChanges` — `StateDiff` recording all mutations
- `Trace` — optional `[]TraceStep`
- `Err`

### Result Accessor

`ResultAccessor` provides a programmatic interface over `ExecutionResult`:
- `Success()`, `Reverted()`, `Failed()`
- `Output()`, `OutputAsBigInt()`, `OutputAsAddr()`
- `GasInfo()` — returns `(used, remaining, refund)`
- `Logs()`, `CreatedContract()`, `StateDiff()`, `Trace()`, `Error()`

### State Diff

`StateDiff` records all world-state mutations:
- `BalanceChanges map[Address]*big.Int`
- `NonceChanges map[Address]uint64`
- `CodeChanges map[Address][]byte`
- `StorageChanges map[Address]map[Hash]Hash`
- `CreatedAccounts []Address`
- `DeletedAccounts []Address`

### Execution Result Builder

`ExecutionResultBuilder` constructs `ExecutionResult` in a fluent style:

```go
result := engine.NewExecutionResultBuilder().
    SetStatus(engine.StatusSuccess).
    SetGasUsed(21000).
    SetReturnData(output).
    SetLogs(logs).
    Build()
```

---

## Breakpoint Implementation

Breakpoints are first-class hooks designed for interactive debugging.

### Breakpoint Types

Breakpoints reuse the same `HookType` taxonomy as regular hooks but add stepping semantics:

| Reason | Description |
|--------|-------------|
| `ReasonOpcode` | Opcode breakpoint / single step |
| `ReasonCall` | Call-family breakpoint |
| `ReasonStorageRead` | Storage read breakpoint |
| `ReasonStorageWrite` | Storage write breakpoint |
| `ReasonMemoryRead` | Memory read breakpoint |
| `ReasonMemoryWrite` | Memory write breakpoint |
| `ReasonLog` | Log breakpoint |
| `ReasonSelfDestruct` | Self-destruct breakpoint |
| `ReasonReturn` | Return/revert breakpoint |
| `ReasonStep` | Generic step breakpoint |
| `ReasonException` | Exception breakpoint |
| `ReasonManual` | Explicit pause |

### Breakpoint Conditions

Breakpoints support rich conditions:
- `MinGas` / `MaxGas`
- `MinDepth` / `MaxDepth`
- `OpFilter` — list of matching opcodes
- `AddrFilter` — list of matching contract addresses
- `SlotFilter` — list of matching storage slots
- `Custom` — arbitrary `HookCondition`

### Breakpoint Manager

`BreakpointManager` manages active breakpoints and stepping state:
- `SetBreakpoint(bp *Breakpoint) error`
- `RemoveBreakpoint(id string) error`
- `Clear()`
- `StepInto()` — pause at next instruction
- `StepOver(currentDepth int)` — pause at next instruction in same/shallower frame
- `StepOut(currentDepth int)` — pause when returning from current frame
- `IsStepping()` — check if in stepping mode
- `ResolveStepping()` — clear stepping state

### Debugger Interface

`Debugger` provides high-level control over an `Engine`:
- `Attach(engine Engine) error`
- `Detach() error`
- `SetBreakpoint(bp *Breakpoint) error`
- `RemoveBreakpoint(id string) error`
- `Continue() (*ExecutionResult, error)`
- `StepInto() (*ExecutionResult, error)`
- `StepOver() (*ExecutionResult, error)`
- `StepOut() (*ExecutionResult, error)`
- `Pause() error`
- `State() (EVMState, error)`
- `Result() (*ExecutionResult, error)`

### Breakpoint Handler

When a breakpoint fires, the engine invokes its `onHit` handler with a `BreakpointContext` containing:
- Breakpoint ID and reason
- `HookCtx` (read-only state + event metadata)
- `State` (mutable EVM state for inspection/modification)
- `Result` (pre-populated with the natural operation result)

The handler returns a `BreakpointAction`:
- `BreakpointContinue`
- `BreakpointReplaceResult`
- `BreakpointStepInto`
- `BreakpointStepOver`
- `BreakpointStepOut`

---

## Engine Interface

```go
type Engine interface {
    Run(cfg *ExecutionConfig) (*ExecutionResult, error)
    RunWithState(state EVMState, code []byte) (*ExecutionResult, error)
    NewState(cfg *ExecutionConfig) (EVMState, error)
    SupportedForks() []Fork
    ValidateConfig(cfg *ExecutionConfig) error
}
```

### ExecutionConfig

`ExecutionConfig` bundles all parameters for a single execution:
- `Fork`, `GasLimit`, `Value`, `Input`
- `Origin`, `Caller`, `ContractAddress`
- `Code`, `CodeHash`
- `BlockContext`, `TxContext`
- `State` (account state), `Storage`, `TransientStorage`
- `AccessList`
- `Hooks`
- `CollectTrace`
- `CallDepth`, `IsStatic`

---

## Fork Support

Supported forks (London onward):
- `london`
- `paris`
- `shanghai`
- `cancun`
- `prague`
- `amsterdam`

Fork-specific behaviors include:
- London: `BASEFEE`, EIP-1559, EIP-3529 refund reduction, `0xEF` prefix rejection
- Paris: `DIFFICULTY` → `PREVRANDAO`
- Shanghai: `PUSH0`, max init code size, init code gas
- Cancun: `TLOAD`, `TSTORE`, `MCOPY`, `BLOBHASH`, `BLOBBASEFEE`, transient storage, blob transactions
- Prague: BLS12-381 precompiles, EOA delegation (EIP-7702)
- Amsterdam: `P256VERIFY`, `CLZ`, blob schedule mechanics, block access lists

---

## Error Codes

The engine returns specific errors for common failure modes:
- `ErrOutOfGas`
- `ErrStackOverflow` / `ErrStackUnderflow`
- `ErrInvalidJump`
- `ErrInvalidOpcode`
- `ErrCallDepthExceeded`
- `ErrInsufficientBalance`
- `ErrAddressCollision`
- `ErrCodeStoreOutOfGas`
- `ErrMaxCodeSizeExceeded`
- `ErrInvalidContractPrefix`
- `ErrWriteProtection`
- `ErrReturnDataOutOfBounds`
- `ErrExecutionReverted`
