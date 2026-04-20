package overlay

import (
	"math/big"

	"inspethct/internal/engine"
)

type Account struct {
	Balance        *big.Int
	Nonce          uint64
	Code           []byte
	Exists         bool
	Created        bool
	SelfDestructed bool
	BalanceDirty   bool
	NonceDirty     bool
	CodeDirty      bool
}

func (account Account) Clone() Account {
	clone := Account{
		Nonce:          account.Nonce,
		Exists:         account.Exists,
		Created:        account.Created,
		SelfDestructed: account.SelfDestructed,
		BalanceDirty:   account.BalanceDirty,
		NonceDirty:     account.NonceDirty,
		CodeDirty:      account.CodeDirty,
	}
	if account.Balance != nil {
		clone.Balance = new(big.Int).Set(account.Balance)
	}
	if account.Code != nil {
		clone.Code = append([]byte(nil), account.Code...)
	}
	return clone
}

type snapshot struct {
	accounts map[engine.Address]Account
	storage  map[engine.Address]map[engine.Hash]engine.Hash
}

type State struct {
	accounts  map[engine.Address]Account
	storage   map[engine.Address]map[engine.Hash]engine.Hash
	snapshots []snapshot
}

func NewState() *State {
	return &State{
		accounts: make(map[engine.Address]Account),
		storage:  make(map[engine.Address]map[engine.Hash]engine.Hash),
	}
}

func (state *State) GetAccount(addr engine.Address) (Account, bool) {
	account, ok := state.accounts[addr]
	if !ok {
		return Account{}, false
	}
	return account.Clone(), true
}

func (state *State) UpsertAccount(addr engine.Address, account Account) {
	state.accounts[addr] = account.Clone()
}

func (state *State) SetBalance(addr engine.Address, balance *big.Int) {
	account := state.accounts[addr]
	account.Exists = true
	account.BalanceDirty = true
	if balance != nil {
		account.Balance = new(big.Int).Set(balance)
	} else {
		account.Balance = nil
	}
	state.accounts[addr] = account
}

func (state *State) SetNonce(addr engine.Address, nonce uint64) {
	account := state.accounts[addr]
	account.Exists = true
	account.Nonce = nonce
	account.NonceDirty = true
	state.accounts[addr] = account
}

func (state *State) SetCode(addr engine.Address, code []byte) {
	account := state.accounts[addr]
	account.Exists = true
	account.Code = append([]byte(nil), code...)
	account.CodeDirty = true
	state.accounts[addr] = account
}

func (state *State) MarkCreated(addr engine.Address) {
	account := state.accounts[addr]
	account.Exists = true
	account.Created = true
	state.accounts[addr] = account
}

func (state *State) SelfDestruct(addr engine.Address) {
	account := state.accounts[addr]
	account.Exists = true
	account.SelfDestructed = true
	state.accounts[addr] = account
}

func (state *State) GetStorage(addr engine.Address, slot engine.Hash) (engine.Hash, bool) {
	values, ok := state.storage[addr]
	if !ok {
		return engine.Hash{}, false
	}
	value, ok := values[slot]
	return value, ok
}

func (state *State) SetStorage(addr engine.Address, slot engine.Hash, value engine.Hash) {
	values, ok := state.storage[addr]
	if !ok {
		values = make(map[engine.Hash]engine.Hash)
		state.storage[addr] = values
	}
	values[slot] = value
}

func (state *State) AccountsSnapshot() map[engine.Address]Account {
	return cloneAccounts(state.accounts)
}

func (state *State) StorageSnapshot() map[engine.Address]map[engine.Hash]engine.Hash {
	return cloneStorage(state.storage)
}

func (state *State) Snapshot() int {
	snap := snapshot{
		accounts: cloneAccounts(state.accounts),
		storage:  cloneStorage(state.storage),
	}
	state.snapshots = append(state.snapshots, snap)
	return len(state.snapshots) - 1
}

func (state *State) RevertToSnapshot(id int) {
	if id < 0 || id >= len(state.snapshots) {
		return
	}
	snap := state.snapshots[id]
	state.accounts = cloneAccounts(snap.accounts)
	state.storage = cloneStorage(snap.storage)
	state.snapshots = state.snapshots[:id]
}

func cloneAccounts(accounts map[engine.Address]Account) map[engine.Address]Account {
	clone := make(map[engine.Address]Account, len(accounts))
	for addr, account := range accounts {
		clone[addr] = account.Clone()
	}
	return clone
}

func cloneStorage(storage map[engine.Address]map[engine.Hash]engine.Hash) map[engine.Address]map[engine.Hash]engine.Hash {
	clone := make(map[engine.Address]map[engine.Hash]engine.Hash, len(storage))
	for addr, values := range storage {
		inner := make(map[engine.Hash]engine.Hash, len(values))
		for slot, value := range values {
			inner[slot] = value
		}
		clone[addr] = inner
	}
	return clone
}
