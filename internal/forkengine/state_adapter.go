package forkengine

import (
	"context"
	"fmt"
	"math/big"

	"inspethct/internal/engine"
	"inspethct/internal/forkengine/cache"
	"inspethct/internal/forkengine/upstream"

	"golang.org/x/crypto/sha3"
)

type forkAccountState struct {
	ctx            context.Context
	provider       upstream.Provider
	cache          cache.Store
	cacheBlockRef  upstream.BlockRef
	sourceBlockRef upstream.BlockRef
	base           *engine.InMemoryAccountState
	loaded         map[engine.Address]struct{}
	snapshots      []map[engine.Address]struct{}
}

type forkStorage struct {
	ctx            context.Context
	provider       upstream.Provider
	cache          cache.Store
	cacheBlockRef  upstream.BlockRef
	sourceBlockRef upstream.BlockRef
	base           *engine.InMemoryStorage
	loaded         map[engine.Address]map[engine.Hash]struct{}
	snapshots      []map[engine.Address]map[engine.Hash]struct{}
}

func newForkAccountState(ctx context.Context, provider upstream.Provider, store cache.Store, cacheBlockRef upstream.BlockRef, sourceBlockRef upstream.BlockRef) *forkAccountState {
	return &forkAccountState{
		ctx:            ctx,
		provider:       provider,
		cache:          store,
		cacheBlockRef:  cacheBlockRef,
		sourceBlockRef: sourceBlockRef,
		base:           engine.NewInMemoryAccountState(),
		loaded:         make(map[engine.Address]struct{}),
	}
}

func newForkStorage(ctx context.Context, provider upstream.Provider, store cache.Store, cacheBlockRef upstream.BlockRef, sourceBlockRef upstream.BlockRef) *forkStorage {
	return &forkStorage{
		ctx:            ctx,
		provider:       provider,
		cache:          store,
		cacheBlockRef:  cacheBlockRef,
		sourceBlockRef: sourceBlockRef,
		base:           engine.NewInMemoryStorage(),
		loaded:         make(map[engine.Address]map[engine.Hash]struct{}),
	}
}

func (state *forkAccountState) Balance(addr engine.Address) *big.Int {
	state.mustLoad(addr)
	return state.base.Balance(addr)
}

func (state *forkAccountState) Nonce(addr engine.Address) uint64 {
	state.mustLoad(addr)
	return state.base.Nonce(addr)
}

func (state *forkAccountState) Code(addr engine.Address) []byte {
	state.mustLoad(addr)
	return state.base.Code(addr)
}

func (state *forkAccountState) CodeHash(addr engine.Address) engine.Hash {
	state.mustLoad(addr)
	return state.base.CodeHash(addr)
}

func (state *forkAccountState) CodeSize(addr engine.Address) int {
	state.mustLoad(addr)
	return state.base.CodeSize(addr)
}

func (state *forkAccountState) StorageRoot(addr engine.Address) engine.Hash {
	state.mustLoad(addr)
	return state.base.StorageRoot(addr)
}

func (state *forkAccountState) Exists(addr engine.Address) bool {
	state.mustLoad(addr)
	return state.base.Exists(addr)
}

func (state *forkAccountState) IsEmpty(addr engine.Address) bool {
	state.mustLoad(addr)
	return state.base.IsEmpty(addr)
}

func (state *forkAccountState) HasCodeOrNonce(addr engine.Address) bool {
	state.mustLoad(addr)
	return state.base.HasCodeOrNonce(addr)
}

func (state *forkAccountState) SetBalance(addr engine.Address, balance *big.Int) {
	state.mustLoad(addr)
	state.base.SetBalance(addr, balance)
}

func (state *forkAccountState) AddBalance(addr engine.Address, amount *big.Int) {
	state.mustLoad(addr)
	state.base.AddBalance(addr, amount)
}

func (state *forkAccountState) SubBalance(addr engine.Address, amount *big.Int) {
	state.mustLoad(addr)
	state.base.SubBalance(addr, amount)
}

func (state *forkAccountState) SetNonce(addr engine.Address, nonce uint64) {
	state.mustLoad(addr)
	state.base.SetNonce(addr, nonce)
}

func (state *forkAccountState) IncrementNonce(addr engine.Address) {
	state.mustLoad(addr)
	state.base.IncrementNonce(addr)
}

func (state *forkAccountState) SetCode(addr engine.Address, code []byte) {
	state.mustLoad(addr)
	state.base.SetCode(addr, code)
}

func (state *forkAccountState) SelfDestruct(addr engine.Address) {
	state.mustLoad(addr)
	state.base.SelfDestruct(addr)
}

func (state *forkAccountState) SelfDestructed(addr engine.Address) bool {
	state.mustLoad(addr)
	return state.base.SelfDestructed(addr)
}

func (state *forkAccountState) CreateAccount(addr engine.Address) {
	state.mustLoad(addr)
	state.base.CreateAccount(addr)
}

func (state *forkAccountState) Snapshot() int {
	index := state.base.Snapshot()
	snapshot := cloneLoadedAccounts(state.loaded)
	if index == len(state.snapshots) {
		state.snapshots = append(state.snapshots, snapshot)
	} else {
		state.snapshots[index] = snapshot
	}
	return index
}

func (state *forkAccountState) RevertToSnapshot(revid int) {
	state.base.RevertToSnapshot(revid)
	if revid < 0 || revid >= len(state.snapshots) {
		return
	}
	state.loaded = cloneLoadedAccounts(state.snapshots[revid])
	state.snapshots = state.snapshots[:revid]
}

func (state *forkAccountState) mustLoad(addr engine.Address) {
	if err := state.ensureLoaded(addr); err != nil {
		panic(err)
	}
}

func (state *forkAccountState) ensureLoaded(addr engine.Address) error {
	if _, ok := state.loaded[addr]; ok {
		return nil
	}
	snapshot, ok := state.cache.GetAccount(addr, state.cacheBlockRef)
	if !ok {
		balance, err := state.provider.GetBalance(state.ctx, addr, state.sourceBlockRef)
		if err != nil {
			return fmt.Errorf("forkengine: load balance for %x: %w", addr, err)
		}
		nonce, err := state.provider.GetNonce(state.ctx, addr, state.sourceBlockRef)
		if err != nil {
			return fmt.Errorf("forkengine: load nonce for %x: %w", addr, err)
		}
		code, err := state.provider.GetCode(state.ctx, addr, state.sourceBlockRef)
		if err != nil {
			return fmt.Errorf("forkengine: load code for %x: %w", addr, err)
		}
		if code == nil {
			code = []byte{}
		}
		codeHash := hashBytes(code)
		exists := (balance != nil && balance.Sign() != 0) || nonce != 0 || len(code) != 0
		snapshot = upstream.AccountSnapshot{Balance: cloneBigInt(balance), Nonce: nonce, Code: append([]byte(nil), code...), CodeHash: codeHash, Exists: exists}
		state.cache.PutAccount(addr, state.cacheBlockRef, snapshot)
		if len(code) != 0 {
			state.cache.PutCode(addr, state.cacheBlockRef, code)
		}
	}
	seedAccount(state.base, addr, snapshot)
	state.loaded[addr] = struct{}{}
	return nil
}

func (storage *forkStorage) Get(addr engine.Address, slot engine.Hash) engine.Hash {
	storage.mustLoad(addr, slot)
	return storage.base.Get(addr, slot)
}

func (storage *forkStorage) Set(addr engine.Address, slot engine.Hash, value engine.Hash) {
	storage.mustLoad(addr, slot)
	storage.base.Set(addr, slot, value)
}

func (storage *forkStorage) Original(addr engine.Address, slot engine.Hash) engine.Hash {
	storage.mustLoad(addr, slot)
	return storage.base.Original(addr, slot)
}

func (storage *forkStorage) Snapshot() int {
	index := storage.base.Snapshot()
	snapshot := cloneLoadedSlots(storage.loaded)
	if index == len(storage.snapshots) {
		storage.snapshots = append(storage.snapshots, snapshot)
	} else {
		storage.snapshots[index] = snapshot
	}
	return index
}

func (storage *forkStorage) RevertToSnapshot(revid int) {
	storage.base.RevertToSnapshot(revid)
	if revid < 0 || revid >= len(storage.snapshots) {
		return
	}
	storage.loaded = cloneLoadedSlots(storage.snapshots[revid])
	storage.snapshots = storage.snapshots[:revid]
}

func (storage *forkStorage) mustLoad(addr engine.Address, slot engine.Hash) {
	if err := storage.ensureLoaded(addr, slot); err != nil {
		panic(err)
	}
}

func (storage *forkStorage) ensureLoaded(addr engine.Address, slot engine.Hash) error {
	if slots, ok := storage.loaded[addr]; ok {
		if _, loaded := slots[slot]; loaded {
			return nil
		}
	}
	value, ok := storage.cache.GetStorage(addr, slot, storage.cacheBlockRef)
	if !ok {
		fetched, err := storage.provider.GetStorageAt(storage.ctx, addr, slot, storage.sourceBlockRef)
		if err != nil {
			return fmt.Errorf("forkengine: load storage for %x slot %x: %w", addr, slot, err)
		}
		value = fetched
		storage.cache.PutStorage(addr, slot, storage.cacheBlockRef, value)
	}
	storage.base.Seed(addr, slot, value)
	if storage.loaded[addr] == nil {
		storage.loaded[addr] = make(map[engine.Hash]struct{})
	}
	storage.loaded[addr][slot] = struct{}{}
	return nil
}

func seedAccount(state *engine.InMemoryAccountState, addr engine.Address, snapshot upstream.AccountSnapshot) {
	if snapshot.Exists {
		state.CreateAccount(addr)
	}
	if snapshot.Balance != nil {
		state.SetBalance(addr, snapshot.Balance)
	}
	if snapshot.Nonce != 0 {
		state.SetNonce(addr, snapshot.Nonce)
	}
	if len(snapshot.Code) != 0 {
		state.SetCode(addr, snapshot.Code)
	}
}

func cloneLoadedAccounts(input map[engine.Address]struct{}) map[engine.Address]struct{} {
	clone := make(map[engine.Address]struct{}, len(input))
	for addr := range input {
		clone[addr] = struct{}{}
	}
	return clone
}

func cloneLoadedSlots(input map[engine.Address]map[engine.Hash]struct{}) map[engine.Address]map[engine.Hash]struct{} {
	clone := make(map[engine.Address]map[engine.Hash]struct{}, len(input))
	for addr, slots := range input {
		inner := make(map[engine.Hash]struct{}, len(slots))
		for slot := range slots {
			inner[slot] = struct{}{}
		}
		clone[addr] = inner
	}
	return clone
}

func hashBytes(code []byte) engine.Hash {
	hasher := sha3.NewLegacyKeccak256()
	_, _ = hasher.Write(code)
	var hash engine.Hash
	copy(hash[:], hasher.Sum(nil))
	return hash
}
