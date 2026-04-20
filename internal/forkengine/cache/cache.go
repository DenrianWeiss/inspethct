package cache

import (
	"math/big"
	"sync"

	"inspethct/internal/engine"
	"inspethct/internal/forkengine/overlay"
	"inspethct/internal/forkengine/upstream"
	"golang.org/x/crypto/sha3"
)

type AccountCache interface {
	GetAccount(addr engine.Address, block upstream.BlockRef) (upstream.AccountSnapshot, bool)
	PutAccount(addr engine.Address, block upstream.BlockRef, snapshot upstream.AccountSnapshot)
}

type StorageCache interface {
	GetStorage(addr engine.Address, slot engine.Hash, block upstream.BlockRef) (engine.Hash, bool)
	PutStorage(addr engine.Address, slot engine.Hash, block upstream.BlockRef, value engine.Hash)
}

type CodeCache interface {
	GetCode(addr engine.Address, block upstream.BlockRef) ([]byte, bool)
	PutCode(addr engine.Address, block upstream.BlockRef, code []byte)
}

type BlockCache interface {
	GetBlock(block upstream.BlockRef) (upstream.Block, bool)
	PutBlock(block upstream.BlockRef, value upstream.Block)
}

type DirtyAccount struct {
	BalanceDirty   bool
	NonceDirty     bool
	CodeDirty      bool
	Created        bool
	SelfDestructed bool
	StorageSlots   map[engine.Hash]struct{}
}

type DirtyState struct {
	Accounts map[engine.Address]DirtyAccount
}

type WritebackCache interface {
	ApplyOverlay(block upstream.BlockRef, state *overlay.State)
	ApplyStateDiff(block upstream.BlockRef, diff *engine.StateDiff)
	DirtyState(block upstream.BlockRef) DirtyState
}

type StateTransfer interface {
	ExportState() ExportedState
	ImportState(state ExportedState)
}

type Store interface {
	AccountCache
	StorageCache
	CodeCache
	BlockCache
	WritebackCache
	StateTransfer
}

type ExportedState struct {
	Accounts []ExportedAccount
	Storage  []ExportedStorage
	Codes    []ExportedCode
	Blocks   []ExportedBlock
	Dirty    []ExportedDirtyAccount
}

type ExportedAccount struct {
	Address  engine.Address
	Block    upstream.BlockRef
	Snapshot upstream.AccountSnapshot
}

type ExportedStorage struct {
	Address engine.Address
	Slot    engine.Hash
	Block   upstream.BlockRef
	Value   engine.Hash
}

type ExportedCode struct {
	Address engine.Address
	Block   upstream.BlockRef
	Code    []byte
}

type ExportedBlock struct {
	BlockRef upstream.BlockRef
	Block    upstream.Block
}

type ExportedDirtyAccount struct {
	Address        engine.Address
	Block          upstream.BlockRef
	BalanceDirty   bool
	NonceDirty     bool
	CodeDirty      bool
	Created        bool
	SelfDestructed bool
	StorageSlots   []engine.Hash
}

type MemoryStore struct {
	mu       sync.RWMutex
	accounts map[accountKey]upstream.AccountSnapshot
	storage  map[storageKey]engine.Hash
	codes    map[codeKey][]byte
	blocks   map[string]upstream.Block
	dirty    map[string]DirtyState
}

type accountKey struct {
	address  engine.Address
	blockKey string
}

type storageKey struct {
	address  engine.Address
	slot     engine.Hash
	blockKey string
}

type codeKey struct {
	address  engine.Address
	blockKey string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		accounts: make(map[accountKey]upstream.AccountSnapshot),
		storage:  make(map[storageKey]engine.Hash),
		codes:    make(map[codeKey][]byte),
		blocks:   make(map[string]upstream.Block),
		dirty:    make(map[string]DirtyState),
	}
}

func (store *MemoryStore) GetAccount(addr engine.Address, block upstream.BlockRef) (upstream.AccountSnapshot, bool) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	value, ok := store.accounts[accountKey{address: addr, blockKey: block.CacheKey()}]
	if !ok {
		return upstream.AccountSnapshot{}, false
	}
	return value.Clone(), true
}

func (store *MemoryStore) PutAccount(addr engine.Address, block upstream.BlockRef, snapshot upstream.AccountSnapshot) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.accounts[accountKey{address: addr, blockKey: block.CacheKey()}] = snapshot.Clone()
}

func (store *MemoryStore) GetStorage(addr engine.Address, slot engine.Hash, block upstream.BlockRef) (engine.Hash, bool) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	value, ok := store.storage[storageKey{address: addr, slot: slot, blockKey: block.CacheKey()}]
	return value, ok
}

func (store *MemoryStore) PutStorage(addr engine.Address, slot engine.Hash, block upstream.BlockRef, value engine.Hash) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.storage[storageKey{address: addr, slot: slot, blockKey: block.CacheKey()}] = value
}

func (store *MemoryStore) GetCode(addr engine.Address, block upstream.BlockRef) ([]byte, bool) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	value, ok := store.codes[codeKey{address: addr, blockKey: block.CacheKey()}]
	if !ok {
		return nil, false
	}
	return append([]byte(nil), value...), true
}

func (store *MemoryStore) PutCode(addr engine.Address, block upstream.BlockRef, code []byte) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.codes[codeKey{address: addr, blockKey: block.CacheKey()}] = append([]byte(nil), code...)
}

func (store *MemoryStore) GetBlock(block upstream.BlockRef) (upstream.Block, bool) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	value, ok := store.blocks[block.CacheKey()]
	if !ok {
		return upstream.Block{}, false
	}
	return value.Clone(), true
}

func (store *MemoryStore) PutBlock(block upstream.BlockRef, value upstream.Block) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.blocks[block.CacheKey()] = value.Clone()
}

func (store *MemoryStore) ApplyOverlay(block upstream.BlockRef, state *overlay.State) {
	if state == nil {
		return
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	blockKey := block.CacheKey()
	for addr, account := range state.AccountsSnapshot() {
		store.applyOverlayAccountLocked(blockKey, addr, account)
	}
	for addr, slots := range state.StorageSnapshot() {
		for slot, value := range slots {
			store.storage[storageKey{address: addr, slot: slot, blockKey: blockKey}] = value
			store.markStorageDirtyLocked(blockKey, addr, slot)
		}
	}
}

func (store *MemoryStore) ApplyStateDiff(block upstream.BlockRef, diff *engine.StateDiff) {
	if diff == nil {
		return
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	blockKey := block.CacheKey()
	for addr, balance := range diff.BalanceChanges {
		store.applyBalanceLocked(blockKey, addr, balance)
	}
	for addr, nonce := range diff.NonceChanges {
		store.applyNonceLocked(blockKey, addr, nonce)
	}
	for addr, code := range diff.CodeChanges {
		store.applyCodeLocked(blockKey, addr, code)
	}
	for _, addr := range diff.CreatedAccounts {
		store.markCreatedLocked(blockKey, addr)
	}
	for _, addr := range diff.DeletedAccounts {
		store.markSelfDestructedLocked(blockKey, addr)
	}
	for addr, slots := range diff.StorageChanges {
		for slot, value := range slots {
			store.storage[storageKey{address: addr, slot: slot, blockKey: blockKey}] = value
			store.markStorageDirtyLocked(blockKey, addr, slot)
		}
	}
}

func (store *MemoryStore) DirtyState(block upstream.BlockRef) DirtyState {
	store.mu.RLock()
	defer store.mu.RUnlock()
	state, ok := store.dirty[block.CacheKey()]
	if !ok {
		return DirtyState{Accounts: map[engine.Address]DirtyAccount{}}
	}
	return state.Clone()
}

func (store *MemoryStore) ExportState() ExportedState {
	store.mu.RLock()
	defer store.mu.RUnlock()
	exported := ExportedState{
		Accounts: make([]ExportedAccount, 0, len(store.accounts)),
		Storage:  make([]ExportedStorage, 0, len(store.storage)),
		Codes:    make([]ExportedCode, 0, len(store.codes)),
		Blocks:   make([]ExportedBlock, 0, len(store.blocks)),
	}
	for key, snapshot := range store.accounts {
		exported.Accounts = append(exported.Accounts, ExportedAccount{Address: key.address, Block: decodeBlockKey(key.blockKey), Snapshot: snapshot.Clone()})
	}
	for key, value := range store.storage {
		exported.Storage = append(exported.Storage, ExportedStorage{Address: key.address, Slot: key.slot, Block: decodeBlockKey(key.blockKey), Value: value})
	}
	for key, code := range store.codes {
		exported.Codes = append(exported.Codes, ExportedCode{Address: key.address, Block: decodeBlockKey(key.blockKey), Code: append([]byte(nil), code...)})
	}
	for key, block := range store.blocks {
		exported.Blocks = append(exported.Blocks, ExportedBlock{BlockRef: decodeBlockKey(key), Block: block.Clone()})
	}
	for blockKey, dirty := range store.dirty {
		for addr, account := range dirty.Accounts {
			slots := make([]engine.Hash, 0, len(account.StorageSlots))
			for slot := range account.StorageSlots {
				slots = append(slots, slot)
			}
			exported.Dirty = append(exported.Dirty, ExportedDirtyAccount{
				Address:        addr,
				Block:          decodeBlockKey(blockKey),
				BalanceDirty:   account.BalanceDirty,
				NonceDirty:     account.NonceDirty,
				CodeDirty:      account.CodeDirty,
				Created:        account.Created,
				SelfDestructed: account.SelfDestructed,
				StorageSlots:   slots,
			})
		}
	}
	return exported
}

func (store *MemoryStore) ImportState(state ExportedState) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.accounts = make(map[accountKey]upstream.AccountSnapshot, len(state.Accounts))
	store.storage = make(map[storageKey]engine.Hash, len(state.Storage))
	store.codes = make(map[codeKey][]byte, len(state.Codes))
	store.blocks = make(map[string]upstream.Block, len(state.Blocks))
	store.dirty = make(map[string]DirtyState)
	for _, account := range state.Accounts {
		store.accounts[accountKey{address: account.Address, blockKey: account.Block.CacheKey()}] = account.Snapshot.Clone()
	}
	for _, slot := range state.Storage {
		store.storage[storageKey{address: slot.Address, slot: slot.Slot, blockKey: slot.Block.CacheKey()}] = slot.Value
	}
	for _, code := range state.Codes {
		store.codes[codeKey{address: code.Address, blockKey: code.Block.CacheKey()}] = append([]byte(nil), code.Code...)
	}
	for _, block := range state.Blocks {
		store.blocks[block.BlockRef.CacheKey()] = block.Block.Clone()
	}
	for _, dirty := range state.Dirty {
		blockKey := dirty.Block.CacheKey()
		store.markAccountDirtyLocked(blockKey, dirty.Address, func(account *DirtyAccount) {
			account.BalanceDirty = dirty.BalanceDirty
			account.NonceDirty = dirty.NonceDirty
			account.CodeDirty = dirty.CodeDirty
			account.Created = dirty.Created
			account.SelfDestructed = dirty.SelfDestructed
			if len(dirty.StorageSlots) > 0 {
				if account.StorageSlots == nil {
					account.StorageSlots = make(map[engine.Hash]struct{}, len(dirty.StorageSlots))
				}
				for _, slot := range dirty.StorageSlots {
					account.StorageSlots[slot] = struct{}{}
				}
			}
		})
	}
}

func (state DirtyState) Clone() DirtyState {
	clone := DirtyState{Accounts: make(map[engine.Address]DirtyAccount, len(state.Accounts))}
	for addr, account := range state.Accounts {
		clone.Accounts[addr] = account.Clone()
	}
	return clone
}

func (account DirtyAccount) Clone() DirtyAccount {
	clone := DirtyAccount{
		BalanceDirty:   account.BalanceDirty,
		NonceDirty:     account.NonceDirty,
		CodeDirty:      account.CodeDirty,
		Created:        account.Created,
		SelfDestructed: account.SelfDestructed,
	}
	if len(account.StorageSlots) > 0 {
		clone.StorageSlots = make(map[engine.Hash]struct{}, len(account.StorageSlots))
		for slot := range account.StorageSlots {
			clone.StorageSlots[slot] = struct{}{}
		}
	}
	return clone
}

func (store *MemoryStore) applyOverlayAccountLocked(blockKey string, addr engine.Address, account overlay.Account) {
	if account.BalanceDirty {
		store.applyBalanceLocked(blockKey, addr, account.Balance)
	}
	if account.NonceDirty {
		store.applyNonceLocked(blockKey, addr, account.Nonce)
	}
	if account.CodeDirty {
		store.applyCodeLocked(blockKey, addr, account.Code)
	}
	if account.Created {
		store.markCreatedLocked(blockKey, addr)
	}
	if account.SelfDestructed {
		store.markSelfDestructedLocked(blockKey, addr)
	}
}

func (store *MemoryStore) applyBalanceLocked(blockKey string, addr engine.Address, balance *big.Int) {
	key := accountKey{address: addr, blockKey: blockKey}
	snapshot := store.accounts[key].Clone()
	snapshot.Exists = true
	snapshot.Balance = cloneBigInt(balance)
	store.accounts[key] = snapshot
	store.markAccountDirtyLocked(blockKey, addr, func(account *DirtyAccount) {
		account.BalanceDirty = true
	})
}

func (store *MemoryStore) applyNonceLocked(blockKey string, addr engine.Address, nonce uint64) {
	key := accountKey{address: addr, blockKey: blockKey}
	snapshot := store.accounts[key].Clone()
	snapshot.Exists = true
	snapshot.Nonce = nonce
	store.accounts[key] = snapshot
	store.markAccountDirtyLocked(blockKey, addr, func(account *DirtyAccount) {
		account.NonceDirty = true
	})
}

func (store *MemoryStore) applyCodeLocked(blockKey string, addr engine.Address, code []byte) {
	key := accountKey{address: addr, blockKey: blockKey}
	snapshot := store.accounts[key].Clone()
	snapshot.Exists = true
	snapshot.Code = append([]byte(nil), code...)
	snapshot.CodeHash = codeHash(code)
	store.accounts[key] = snapshot
	store.codes[codeKey{address: addr, blockKey: blockKey}] = append([]byte(nil), code...)
	store.markAccountDirtyLocked(blockKey, addr, func(account *DirtyAccount) {
		account.CodeDirty = true
	})
}

func (store *MemoryStore) markCreatedLocked(blockKey string, addr engine.Address) {
	key := accountKey{address: addr, blockKey: blockKey}
	snapshot := store.accounts[key].Clone()
	snapshot.Exists = true
	store.accounts[key] = snapshot
	store.markAccountDirtyLocked(blockKey, addr, func(account *DirtyAccount) {
		account.Created = true
	})
}

func (store *MemoryStore) markSelfDestructedLocked(blockKey string, addr engine.Address) {
	key := accountKey{address: addr, blockKey: blockKey}
	snapshot := store.accounts[key].Clone()
	snapshot.Exists = false
	store.accounts[key] = snapshot
	store.markAccountDirtyLocked(blockKey, addr, func(account *DirtyAccount) {
		account.SelfDestructed = true
	})
}

func (store *MemoryStore) markStorageDirtyLocked(blockKey string, addr engine.Address, slot engine.Hash) {
	store.markAccountDirtyLocked(blockKey, addr, func(account *DirtyAccount) {
		if account.StorageSlots == nil {
			account.StorageSlots = make(map[engine.Hash]struct{})
		}
		account.StorageSlots[slot] = struct{}{}
	})
}

func (store *MemoryStore) markAccountDirtyLocked(blockKey string, addr engine.Address, update func(*DirtyAccount)) {
	state := store.dirty[blockKey]
	if state.Accounts == nil {
		state.Accounts = make(map[engine.Address]DirtyAccount)
	}
	account := state.Accounts[addr].Clone()
	update(&account)
	state.Accounts[addr] = account
	store.dirty[blockKey] = state
}

func cloneBigInt(value *big.Int) *big.Int {
	if value == nil {
		return nil
	}
	return new(big.Int).Set(value)
}

func codeHash(code []byte) engine.Hash {
	hasher := sha3.NewLegacyKeccak256()
	_, _ = hasher.Write(code)
	sum := hasher.Sum(nil)
	var hash engine.Hash
	copy(hash[:], sum)
	return hash
}

func decodeBlockKey(key string) upstream.BlockRef {
	if len(key) > 4 && key[:4] == "num:" {
		value := new(big.Int)
		value.SetString(key[4:], 10)
		return upstream.BlockRef{Number: value}
	}
	if len(key) > 4 && key[:4] == "tag:" {
		return upstream.BlockRef{Tag: upstream.BlockTag(key[4:])}
	}
	return upstream.LatestBlock()
}

