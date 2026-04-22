package engine

import (
	"math/big"
)

// AccountState provides access to an Ethereum account's persistent state.
type AccountState interface {
	// Balance returns the current balance of the account.
	Balance(addr Address) *big.Int
	// Nonce returns the current nonce of the account.
	Nonce(addr Address) uint64
	// Code returns the deployed bytecode of the account.
	Code(addr Address) []byte
	// CodeHash returns the keccak256 hash of the account code.
	CodeHash(addr Address) Hash
	// CodeSize returns the length of the deployed bytecode.
	CodeSize(addr Address) int
	// StorageRoot returns the Merkle root of the account storage.
	StorageRoot(addr Address) Hash
	// Exists returns true if the account exists in state.
	Exists(addr Address) bool
	// IsEmpty returns true if the account has zero balance, nonce, and no code.
	IsEmpty(addr Address) bool
	// HasCodeOrNonce returns true if the account has non-zero nonce or deployed code.
	HasCodeOrNonce(addr Address) bool
}

// MutableAccountState extends AccountState with write operations.
type MutableAccountState interface {
	AccountState
	// SetBalance sets the account balance.
	SetBalance(addr Address, balance *big.Int)
	// AddBalance adds amount to the account balance.
	AddBalance(addr Address, amount *big.Int)
	// SubBalance subtracts amount from the account balance.
	SubBalance(addr Address, amount *big.Int)
	// SetNonce sets the account nonce.
	SetNonce(addr Address, nonce uint64)
	// IncrementNonce increments the account nonce by one.
	IncrementNonce(addr Address)
	// SetCode sets the deployed bytecode of the account.
	SetCode(addr Address, code []byte)
	// SelfDestruct marks the account for deletion at the end of the transaction.
	SelfDestruct(addr Address)
	// SelfDestructed returns true if the account has been self-destructed.
	SelfDestructed(addr Address) bool
	// CreateAccount creates a new account at the given address.
	CreateAccount(addr Address)
	// Snapshot returns an identifier for the current revision of the state.
	Snapshot() int
	// RevertToSnapshot reverts all state changes made since the given revision.
	RevertToSnapshot(revid int)
}

// Storage provides read/write access to contract storage.
type Storage interface {
	// Get returns the value stored at the given slot.
	Get(addr Address, slot Hash) Hash
	// Set stores a value at the given slot.
	Set(addr Address, slot Hash, value Hash)
	// Original returns the original value of a slot at transaction start.
	Original(addr Address, slot Hash) Hash
	// Snapshot returns an identifier for the current revision of the storage.
	Snapshot() int
	// RevertToSnapshot reverts all storage changes made since the given revision.
	RevertToSnapshot(revid int)
}

// TransientStorage provides access to transient storage (EIP-1153, Cancun+).
type TransientStorage interface {
	// Get returns the transient value for the given slot.
	Get(addr Address, slot Hash) Hash
	// Set stores a transient value at the given slot.
	Set(addr Address, slot Hash, value Hash)
}

// Stack represents the EVM operand stack.
type Stack interface {
	// Len returns the current number of items on the stack.
	Len() int
	// Push pushes a word onto the stack.
	Push(word Word)
	// Pop removes and returns the top word from the stack.
	Pop() Word
	// Peek returns the top word without removing it.
	Peek() Word
	// PeekN returns the nth item from the top (0 = top).
	PeekN(n int) Word
	// Swap exchanges the top item with the nth item from the top.
	Swap(n int)
	// Dup duplicates the nth item from the top and pushes it.
	Dup(n int)
}

// Memory represents the EVM memory.
type Memory interface {
	// Len returns the current allocated memory size in bytes.
	Len() int
	// Get returns a slice of memory from offset with the given size.
	// Reads beyond allocated memory return zero-padded data.
	Get(offset, size uint64) []byte
	// Set copies data into memory at the given offset.
	// Expands memory if necessary, charging gas via the provided gas meter.
	Set(offset uint64, data []byte) error
	// Resize expands memory to at least the given size.
	Resize(size uint64) error
	// Word returns the 32-byte word at the given word offset.
	Word(offset uint64) Word
	// ExpandSize returns the gas cost and new memory size needed to access
	// memory from offset to offset+size.
	ExpandSize(offset, size uint64) (gasCost uint64, newSize uint64)
}

// BlockContext provides read-only access to block-level information.
type BlockContext interface {
	// Coinbase returns the block's beneficiary address.
	Coinbase() Address
	// Timestamp returns the block's unix timestamp.
	Timestamp() uint64
	// Number returns the block number.
	Number() *big.Int
	// Difficulty returns the block difficulty (PREVRANDAO after The Merge).
	Difficulty() *big.Int
	// GasLimit returns the block gas limit.
	GasLimit() uint64
	// BlockHash returns the hash of the given block number.
	BlockHash(number uint64) Hash
	// BaseFee returns the base fee per gas (EIP-1559, London+).
	BaseFee() *big.Int
	// BlobBaseFee returns the blob base fee (EIP-4844, Cancun+).
	BlobBaseFee() *big.Int
	// BlobHash returns the versioned hash of the nth blob in the transaction (Cancun+).
	BlobHash(index uint64) Hash
	// Random returns the prevRandao value (Paris+).
	Random() Hash
	// ChainID returns the chain ID.
	ChainID() *big.Int
}

// TxContext provides read-only access to transaction-level information.
type TxContext interface {
	// Origin returns the transaction sender.
	Origin() Address
	// GasPrice returns the gas price provided by the transaction.
	GasPrice() *big.Int
	// BlobHashes returns the list of blob hashes for blob transactions (Cancun+).
	BlobHashes() []Hash
	// BlobGasFee returns the blob gas fee cap.
	BlobGasFee() *big.Int
}

// Contract represents the currently executing contract context.
type Contract interface {
	// Address returns the address of the currently executing contract.
	Address() Address
	// Caller returns the address that initiated the current call.
	Caller() Address
	// CallValue returns the value transferred in the current call.
	CallValue() *big.Int
	// CallInput returns the input data of the current call.
	CallInput() []byte
	// Code returns the code currently being executed.
	Code() []byte
	// CodeHash returns the hash of the code being executed.
	CodeHash() Hash
	// CodeAddr returns the code address (may differ from Address for DELEGATECALL).
	CodeAddr() Address
	// IsStatic returns true if the current call is a STATICCALL.
	IsStatic() bool
}

// GasMeter tracks gas consumption and remaining gas.
type GasMeter interface {
	// Gas returns the remaining gas.
	Gas() uint64
	// ConsumeGas deducts gas from the remaining pool.
	ConsumeGas(amount uint64) error
	// RefundGas adds gas to the refund counter.
	RefundGas(amount uint64)
	// DeductRefund subtracts gas from the refund counter.
	DeductRefund(amount uint64)
	// Refund returns the current refund amount.
	Refund() uint64
	// ReturnGas returns unused gas to the remaining pool (e.g. from child calls).
	ReturnGas(amount uint64)
}

// AccessList tracks warmed addresses and storage slots (EIP-2929, Berlin+).
type AccessList interface {
	// IsAddressWarmed returns true if the address has been accessed.
	IsAddressWarmed(addr Address) bool
	// WarmAddress marks an address as accessed.
	WarmAddress(addr Address)
	// IsSlotWarmed returns true if the storage slot has been accessed.
	IsSlotWarmed(addr Address, slot Hash) bool
	// WarmSlot marks a storage slot as accessed.
	WarmSlot(addr Address, slot Hash)
}

// EVMState aggregates all mutable state accessible during EVM execution.
type EVMState interface {
	// Stack provides access to the operand stack.
	Stack() Stack
	// Memory provides access to the EVM memory.
	Memory() Memory
	// Storage provides access to persistent storage.
	Storage() Storage
	// TransientStorage provides access to transient storage (Cancun+).
	TransientStorage() TransientStorage
	// Account provides access to account state.
	Account() MutableAccountState
	// GasMeter provides access to the gas tracker.
	GasMeter() GasMeter
	// BlockContext provides access to block information.
	BlockContext() BlockContext
	// TxContext provides access to transaction information.
	TxContext() TxContext
	// Contract provides access to the current contract context.
	Contract() Contract
	// AccessList provides access to the warmed access list (Berlin+).
	AccessList() AccessList
	// PC returns the current program counter.
	PC() uint64
	// ReturnData returns the output from the previous call.
	ReturnData() []byte
	// SetReturnData sets the return data buffer.
	SetReturnData(data []byte)
	// CallDepth returns the current call depth.
	CallDepth() int
	// Logs returns the logs emitted so far.
	Logs() []Log
	// AddLog appends a log to the log list.
	AddLog(log Log)
}

// ReadOnlyState provides a read-only view of EVM state for hooks and tracers.
type ReadOnlyState interface {
	// StackLen returns the number of items on the stack.
	StackLen() int
	// StackPeekN returns the nth item from the top without modifying the stack.
	StackPeekN(n int) Word
	// MemoryLen returns the current allocated memory size.
	MemoryLen() int
	// MemoryGet returns a copy of memory at the given offset/size.
	MemoryGet(offset, size uint64) []byte
	// StorageGet returns the storage value at the given slot for the given address.
	StorageGet(addr Address, slot Hash) Hash
	// TransientStorageGet returns the transient storage value (Cancun+).
	TransientStorageGet(addr Address, slot Hash) Hash
	// Balance returns the balance of the given address.
	Balance(addr Address) *big.Int
	// Nonce returns the nonce of the given address.
	Nonce(addr Address) uint64
	// Code returns the code at the given address.
	Code(addr Address) []byte
	// CodeSize returns the code size at the given address.
	CodeSize(addr Address) int
	// CodeHash returns the code hash at the given address.
	CodeHash(addr Address) Hash
	// Exists returns true if the account exists.
	Exists(addr Address) bool
	// GasRemaining returns the remaining gas.
	GasRemaining() uint64
	// GasRefund returns the current refund counter.
	GasRefund() uint64
	// PC returns the current program counter.
	PC() uint64
	// ReturnData returns the output from the previous call.
	ReturnData() []byte
	// CallDepth returns the current call depth.
	CallDepth() int
	// IsStatic returns true if in a static call context.
	IsStatic() bool
	// BlockContext returns the current block context.
	BlockContext() BlockContext
	// TxContext returns the current transaction context.
	TxContext() TxContext
	// ContractAddress returns the address of the currently executing contract.
	ContractAddress() Address
	// ContractCaller returns the caller of the current contract.
	ContractCaller() Address
	// ContractCallValue returns the value sent in the current call.
	ContractCallValue() *big.Int
	// ContractCode returns the code currently being executed.
	ContractCode() []byte
	// ContractCodeAddr returns the address whose code is currently executing.
	ContractCodeAddr() Address
	// Logs returns the logs emitted so far.
	Logs() []Log
	// IsAddressWarmed returns true if the address is in the access list.
	IsAddressWarmed(addr Address) bool
	// IsSlotWarmed returns true if the slot is in the access list.
	IsSlotWarmed(addr Address, slot Hash) bool
}
