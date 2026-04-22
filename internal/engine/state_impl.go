package engine

import (
	"math/big"
	"sync"
)

type accountSnapshot struct {
	balances map[Address]*big.Int
	nonces   map[Address]uint64
	codes    map[Address][]byte
	codeHash map[Address]Hash
	exists   map[Address]bool
	deleted  map[Address]bool
}

// InMemoryAccountState implements MutableAccountState using in-memory maps.
type InMemoryAccountState struct {
	mu        sync.RWMutex
	balances  map[Address]*big.Int
	nonces    map[Address]uint64
	codes     map[Address][]byte
	codeHash  map[Address]Hash
	exists    map[Address]bool
	deleted   map[Address]bool
	snapshots []accountSnapshot
}

// NewInMemoryAccountState creates a new in-memory account state.
func NewInMemoryAccountState() *InMemoryAccountState {
	return &InMemoryAccountState{
		balances: make(map[Address]*big.Int),
		nonces:   make(map[Address]uint64),
		codes:    make(map[Address][]byte),
		codeHash: make(map[Address]Hash),
		exists:   make(map[Address]bool),
		deleted:  make(map[Address]bool),
	}
}

func (s *InMemoryAccountState) Snapshot() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := accountSnapshot{
		balances: make(map[Address]*big.Int),
		nonces:   make(map[Address]uint64),
		codes:    make(map[Address][]byte),
		codeHash: make(map[Address]Hash),
		exists:   make(map[Address]bool),
		deleted:  make(map[Address]bool),
	}
	for k, v := range s.balances {
		snap.balances[k] = new(big.Int).Set(v)
	}
	for k, v := range s.nonces {
		snap.nonces[k] = v
	}
	for k, v := range s.codes {
		cp := make([]byte, len(v))
		copy(cp, v)
		snap.codes[k] = cp
	}
	for k, v := range s.codeHash {
		snap.codeHash[k] = v
	}
	for k, v := range s.exists {
		snap.exists[k] = v
	}
	for k, v := range s.deleted {
		snap.deleted[k] = v
	}
	s.snapshots = append(s.snapshots, snap)
	return len(s.snapshots) - 1
}

func (s *InMemoryAccountState) RevertToSnapshot(revid int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if revid < 0 || revid >= len(s.snapshots) {
		return
	}
	snap := s.snapshots[revid]
	s.balances = snap.balances
	s.nonces = snap.nonces
	s.codes = snap.codes
	s.codeHash = snap.codeHash
	s.exists = snap.exists
	s.deleted = snap.deleted
	s.snapshots = s.snapshots[:revid]
}

func (s *InMemoryAccountState) Balance(addr Address) *big.Int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if bal, ok := s.balances[addr]; ok {
		return new(big.Int).Set(bal)
	}
	return big.NewInt(0)
}

func (s *InMemoryAccountState) Nonce(addr Address) uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.nonces[addr]
}

func (s *InMemoryAccountState) Code(addr Address) []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if code, ok := s.codes[addr]; ok {
		out := make([]byte, len(code))
		copy(out, code)
		return out
	}
	return nil
}

func (s *InMemoryAccountState) CodeHash(addr Address) Hash {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if hash, ok := s.codeHash[addr]; ok {
		return hash
	}
	return Hash{}
}

func (s *InMemoryAccountState) CodeSize(addr Address) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.codes[addr])
}

func (s *InMemoryAccountState) StorageRoot(addr Address) Hash {
	return Hash{}
}

func (s *InMemoryAccountState) Exists(addr Address) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.exists[addr]
}

func (s *InMemoryAccountState) IsEmpty(addr Address) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	bal := s.balances[addr]
	return (bal == nil || bal.Sign() == 0) && s.nonces[addr] == 0 && len(s.codes[addr]) == 0
}

func (s *InMemoryAccountState) HasCodeOrNonce(addr Address) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.nonces[addr] > 0 || len(s.codes[addr]) > 0
}

func (s *InMemoryAccountState) SetBalance(addr Address, balance *big.Int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.balances[addr] = new(big.Int).Set(balance)
	s.exists[addr] = true
}

func (s *InMemoryAccountState) AddBalance(addr Address, amount *big.Int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.balances[addr] == nil {
		s.balances[addr] = new(big.Int)
	}
	s.balances[addr].Add(s.balances[addr], amount)
	s.exists[addr] = true
}

func (s *InMemoryAccountState) SubBalance(addr Address, amount *big.Int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.balances[addr] == nil {
		s.balances[addr] = new(big.Int)
	}
	s.balances[addr].Sub(s.balances[addr], amount)
	s.exists[addr] = true
}

func (s *InMemoryAccountState) SetNonce(addr Address, nonce uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nonces[addr] = nonce
	s.exists[addr] = true
}

func (s *InMemoryAccountState) IncrementNonce(addr Address) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nonces[addr]++
	s.exists[addr] = true
}

func (s *InMemoryAccountState) SetCode(addr Address, code []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.codes[addr] = make([]byte, len(code))
	copy(s.codes[addr], code)
	s.codeHash[addr] = hashCode(code)
	s.exists[addr] = true
}

func (s *InMemoryAccountState) SelfDestruct(addr Address) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleted[addr] = true
}

func (s *InMemoryAccountState) SelfDestructed(addr Address) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.deleted[addr]
}

func (s *InMemoryAccountState) CreateAccount(addr Address) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.exists[addr] = true
	s.balances[addr] = big.NewInt(0)
}

func hashCode(code []byte) Hash {
	h := keccak256(code)
	var hash Hash
	copy(hash[:], h)
	return hash
}

type storageSnapshot struct {
	data     map[Address]map[Hash]Hash
	original map[Address]map[Hash]Hash
}

// InMemoryStorage implements Storage using in-memory maps.
type InMemoryStorage struct {
	mu        sync.RWMutex
	data      map[Address]map[Hash]Hash
	original  map[Address]map[Hash]Hash
	snapshots []storageSnapshot
}

// NewInMemoryStorage creates a new in-memory storage backend.
func NewInMemoryStorage() *InMemoryStorage {
	return &InMemoryStorage{
		data:     make(map[Address]map[Hash]Hash),
		original: make(map[Address]map[Hash]Hash),
	}
}

func (s *InMemoryStorage) Snapshot() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := storageSnapshot{
		data:     make(map[Address]map[Hash]Hash),
		original: make(map[Address]map[Hash]Hash),
	}
	for addr, slots := range s.data {
		snap.data[addr] = make(map[Hash]Hash)
		for k, v := range slots {
			snap.data[addr][k] = v
		}
	}
	for addr, slots := range s.original {
		snap.original[addr] = make(map[Hash]Hash)
		for k, v := range slots {
			snap.original[addr][k] = v
		}
	}
	s.snapshots = append(s.snapshots, snap)
	return len(s.snapshots) - 1
}

func (s *InMemoryStorage) RevertToSnapshot(revid int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if revid < 0 || revid >= len(s.snapshots) {
		return
	}
	snap := s.snapshots[revid]
	s.data = snap.data
	s.original = snap.original
	s.snapshots = s.snapshots[:revid]
}

func (s *InMemoryStorage) Get(addr Address, slot Hash) Hash {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if slots, ok := s.data[addr]; ok {
		return slots[slot]
	}
	return Hash{}
}

func (s *InMemoryStorage) Set(addr Address, slot Hash, value Hash) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data[addr] == nil {
		s.data[addr] = make(map[Hash]Hash)
	}
	if s.original[addr] == nil {
		s.original[addr] = make(map[Hash]Hash)
	}
	if _, ok := s.original[addr][slot]; !ok {
		s.original[addr][slot] = s.data[addr][slot]
	}
	s.data[addr][slot] = value
}

// Seed initializes a storage slot from pre-state so Original reflects the
// transaction-start value rather than recording a mutation from zero.
func (s *InMemoryStorage) Seed(addr Address, slot Hash, value Hash) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data[addr] == nil {
		s.data[addr] = make(map[Hash]Hash)
	}
	if s.original[addr] == nil {
		s.original[addr] = make(map[Hash]Hash)
	}
	s.data[addr][slot] = value
	s.original[addr][slot] = value
}

func (s *InMemoryStorage) Original(addr Address, slot Hash) Hash {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if slots, ok := s.original[addr]; ok {
		return slots[slot]
	}
	return Hash{}
}

// InMemoryTransientStorage implements TransientStorage.
type InMemoryTransientStorage struct {
	mu   sync.RWMutex
	data map[Address]map[Hash]Hash
}

// NewInMemoryTransientStorage creates a new transient storage backend.
func NewInMemoryTransientStorage() *InMemoryTransientStorage {
	return &InMemoryTransientStorage{
		data: make(map[Address]map[Hash]Hash),
	}
}

func (s *InMemoryTransientStorage) Get(addr Address, slot Hash) Hash {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if slots, ok := s.data[addr]; ok {
		return slots[slot]
	}
	return Hash{}
}

func (s *InMemoryTransientStorage) Set(addr Address, slot Hash, value Hash) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data[addr] == nil {
		s.data[addr] = make(map[Hash]Hash)
	}
	s.data[addr][slot] = value
}

// SimpleAccessList implements AccessList.
type SimpleAccessList struct {
	mu      sync.RWMutex
	addrs   map[Address]bool
	storage map[Address]map[Hash]bool
}

// NewSimpleAccessList creates a new access list.
func NewSimpleAccessList() *SimpleAccessList {
	return &SimpleAccessList{
		addrs:   make(map[Address]bool),
		storage: make(map[Address]map[Hash]bool),
	}
}

func (a *SimpleAccessList) IsAddressWarmed(addr Address) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.addrs[addr]
}

func (a *SimpleAccessList) WarmAddress(addr Address) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.addrs[addr] = true
}

func (a *SimpleAccessList) IsSlotWarmed(addr Address, slot Hash) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if slots, ok := a.storage[addr]; ok {
		return slots[slot]
	}
	return false
}

func (a *SimpleAccessList) WarmSlot(addr Address, slot Hash) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.storage[addr] == nil {
		a.storage[addr] = make(map[Hash]bool)
	}
	a.storage[addr][slot] = true
}

// SimpleBlockContext implements BlockContext.
type SimpleBlockContext struct {
	CoinbaseVal    Address
	TimestampVal   uint64
	NumberVal      *big.Int
	DifficultyVal  *big.Int
	GasLimitVal    uint64
	BlockHashFn    func(uint64) Hash
	BaseFeeVal     *big.Int
	BlobBaseFeeVal *big.Int
	BlobHashFn     func(uint64) Hash
	RandomVal      Hash
	ChainIDVal     *big.Int
}

func (b *SimpleBlockContext) Coinbase() Address    { return b.CoinbaseVal }
func (b *SimpleBlockContext) Timestamp() uint64    { return b.TimestampVal }
func (b *SimpleBlockContext) Number() *big.Int     { return b.NumberVal }
func (b *SimpleBlockContext) Difficulty() *big.Int { return b.DifficultyVal }
func (b *SimpleBlockContext) GasLimit() uint64     { return b.GasLimitVal }
func (b *SimpleBlockContext) BlockHash(n uint64) Hash {
	if b.BlockHashFn != nil {
		return b.BlockHashFn(n)
	}
	return Hash{}
}
func (b *SimpleBlockContext) BaseFee() *big.Int     { return b.BaseFeeVal }
func (b *SimpleBlockContext) BlobBaseFee() *big.Int { return b.BlobBaseFeeVal }
func (b *SimpleBlockContext) BlobHash(idx uint64) Hash {
	if b.BlobHashFn != nil {
		return b.BlobHashFn(idx)
	}
	return Hash{}
}
func (b *SimpleBlockContext) Random() Hash      { return b.RandomVal }
func (b *SimpleBlockContext) ChainID() *big.Int { return b.ChainIDVal }

// SimpleTxContext implements TxContext.
type SimpleTxContext struct {
	OriginVal     Address
	GasPriceVal   *big.Int
	BlobHashesVal []Hash
	BlobGasFeeVal *big.Int
}

func (t *SimpleTxContext) Origin() Address      { return t.OriginVal }
func (t *SimpleTxContext) GasPrice() *big.Int   { return t.GasPriceVal }
func (t *SimpleTxContext) BlobHashes() []Hash   { return t.BlobHashesVal }
func (t *SimpleTxContext) BlobGasFee() *big.Int { return t.BlobGasFeeVal }

// SimpleContract implements Contract.
type SimpleContract struct {
	AddressVal   Address
	CallerVal    Address
	CallValueVal *big.Int
	CallInputVal []byte
	CodeVal      []byte
	CodeHashVal  Hash
	CodeAddrVal  Address
	IsStaticVal  bool
}

func (c *SimpleContract) Address() Address    { return c.AddressVal }
func (c *SimpleContract) Caller() Address     { return c.CallerVal }
func (c *SimpleContract) CallValue() *big.Int { return c.CallValueVal }
func (c *SimpleContract) CallInput() []byte   { return c.CallInputVal }
func (c *SimpleContract) Code() []byte        { return c.CodeVal }
func (c *SimpleContract) CodeHash() Hash      { return c.CodeHashVal }
func (c *SimpleContract) CodeAddr() Address   { return c.CodeAddrVal }
func (c *SimpleContract) IsStatic() bool      { return c.IsStaticVal }

// evmState implements EVMState by composing concrete sub-types.
type evmState struct {
	stack            Stack
	memory           Memory
	storage          Storage
	transientStorage TransientStorage
	account          MutableAccountState
	gasMeter         GasMeter
	blockCtx         BlockContext
	txCtx            TxContext
	contract         Contract
	accessList       AccessList
	pcVal            uint64
	returnData       []byte
	callDepth        int
	logs             []Log
}

// NewEVMState creates a concrete EVMState from components.
func NewEVMState(
	stack Stack,
	memory Memory,
	storage Storage,
	transientStorage TransientStorage,
	account MutableAccountState,
	gasMeter GasMeter,
	blockCtx BlockContext,
	txCtx TxContext,
	contract Contract,
	accessList AccessList,
) EVMState {
	return &evmState{
		stack:            stack,
		memory:           memory,
		storage:          storage,
		transientStorage: transientStorage,
		account:          account,
		gasMeter:         gasMeter,
		blockCtx:         blockCtx,
		txCtx:            txCtx,
		contract:         contract,
		accessList:       accessList,
	}
}

func (s *evmState) Stack() Stack                       { return s.stack }
func (s *evmState) Memory() Memory                     { return s.memory }
func (s *evmState) Storage() Storage                   { return s.storage }
func (s *evmState) TransientStorage() TransientStorage { return s.transientStorage }
func (s *evmState) Account() MutableAccountState       { return s.account }
func (s *evmState) GasMeter() GasMeter                 { return s.gasMeter }
func (s *evmState) BlockContext() BlockContext         { return s.blockCtx }
func (s *evmState) TxContext() TxContext               { return s.txCtx }
func (s *evmState) Contract() Contract                 { return s.contract }
func (s *evmState) AccessList() AccessList             { return s.accessList }
func (s *evmState) PC() uint64                         { return s.pcVal }
func (s *evmState) ReturnData() []byte                 { return s.returnData }
func (s *evmState) SetReturnData(data []byte)          { s.returnData = data }
func (s *evmState) CallDepth() int                     { return s.callDepth }
func (s *evmState) Logs() []Log                        { return s.logs }
func (s *evmState) AddLog(log Log)                     { s.logs = append(s.logs, log) }

// readOnlyState wraps an EVMState to provide read-only access.
type readOnlyState struct {
	inner EVMState
}

// NewReadOnlyState creates a read-only view of an EVMState.
func NewReadOnlyState(state EVMState) ReadOnlyState {
	return &readOnlyState{inner: state}
}

func (r *readOnlyState) Inner() EVMState { return r.inner }

func (r *readOnlyState) StackLen() int         { return r.inner.Stack().Len() }
func (r *readOnlyState) StackPeekN(n int) Word { return r.inner.Stack().PeekN(n) }
func (r *readOnlyState) MemoryLen() int        { return r.inner.Memory().Len() }
func (r *readOnlyState) MemoryGet(offset, size uint64) []byte {
	return r.inner.Memory().Get(offset, size)
}
func (r *readOnlyState) StorageGet(addr Address, slot Hash) Hash {
	return r.inner.Storage().Get(addr, slot)
}
func (r *readOnlyState) TransientStorageGet(addr Address, slot Hash) Hash {
	return r.inner.TransientStorage().Get(addr, slot)
}
func (r *readOnlyState) Balance(addr Address) *big.Int { return r.inner.Account().Balance(addr) }
func (r *readOnlyState) Nonce(addr Address) uint64     { return r.inner.Account().Nonce(addr) }
func (r *readOnlyState) Code(addr Address) []byte      { return r.inner.Account().Code(addr) }
func (r *readOnlyState) CodeSize(addr Address) int     { return r.inner.Account().CodeSize(addr) }
func (r *readOnlyState) CodeHash(addr Address) Hash    { return r.inner.Account().CodeHash(addr) }
func (r *readOnlyState) Exists(addr Address) bool      { return r.inner.Account().Exists(addr) }
func (r *readOnlyState) GasRemaining() uint64          { return r.inner.GasMeter().Gas() }
func (r *readOnlyState) GasRefund() uint64             { return r.inner.GasMeter().Refund() }
func (r *readOnlyState) PC() uint64                    { return r.inner.PC() }
func (r *readOnlyState) ReturnData() []byte            { return r.inner.ReturnData() }
func (r *readOnlyState) CallDepth() int                { return r.inner.CallDepth() }
func (r *readOnlyState) IsStatic() bool                { return r.inner.Contract().IsStatic() }
func (r *readOnlyState) BlockContext() BlockContext    { return r.inner.BlockContext() }
func (r *readOnlyState) TxContext() TxContext          { return r.inner.TxContext() }
func (r *readOnlyState) ContractAddress() Address      { return r.inner.Contract().Address() }
func (r *readOnlyState) ContractCaller() Address       { return r.inner.Contract().Caller() }
func (r *readOnlyState) ContractCallValue() *big.Int   { return r.inner.Contract().CallValue() }
func (r *readOnlyState) ContractCallInput() []byte     { return r.inner.Contract().CallInput() }
func (r *readOnlyState) ContractCode() []byte          { return r.inner.Contract().Code() }
func (r *readOnlyState) ContractCodeAddr() Address     { return r.inner.Contract().CodeAddr() }
func (r *readOnlyState) Logs() []Log                   { return r.inner.Logs() }
func (r *readOnlyState) IsAddressWarmed(addr Address) bool {
	return r.inner.AccessList().IsAddressWarmed(addr)
}
func (r *readOnlyState) IsSlotWarmed(addr Address, slot Hash) bool {
	return r.inner.AccessList().IsSlotWarmed(addr, slot)
}
