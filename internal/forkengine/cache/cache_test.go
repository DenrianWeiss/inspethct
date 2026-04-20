package cache

import (
	"math/big"
	"testing"

	"inspethct/internal/engine"
	"inspethct/internal/forkengine/overlay"
	"inspethct/internal/forkengine/upstream"
)

func TestMemoryStoreSeparatesPinnedAndFloatingEntries(t *testing.T) {
	store := NewMemoryStore()
	addr := engine.Address{0x01}
	store.PutAccount(addr, upstream.LatestBlock(), upstream.AccountSnapshot{Balance: big.NewInt(1), Exists: true})
	store.PutAccount(addr, upstream.BlockNumber(9), upstream.AccountSnapshot{Balance: big.NewInt(2), Exists: true})

	latest, ok := store.GetAccount(addr, upstream.LatestBlock())
	if !ok {
		t.Fatalf("latest account missing")
	}
	pinned, ok := store.GetAccount(addr, upstream.BlockNumber(9))
	if !ok {
		t.Fatalf("pinned account missing")
	}
	if latest.Balance.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("latest balance = %s, want 1", latest.Balance.String())
	}
	if pinned.Balance.Cmp(big.NewInt(2)) != 0 {
		t.Fatalf("pinned balance = %s, want 2", pinned.Balance.String())
	}
}

func TestMemoryStoreReturnsCopies(t *testing.T) {
	store := NewMemoryStore()
	addr := engine.Address{0x02}
	store.PutCode(addr, upstream.BlockNumber(1), []byte{0xaa, 0xbb})
	code, ok := store.GetCode(addr, upstream.BlockNumber(1))
	if !ok {
		t.Fatalf("code missing")
	}
	code[0] = 0xff
	reloaded, ok := store.GetCode(addr, upstream.BlockNumber(1))
	if !ok {
		t.Fatalf("reloaded code missing")
	}
	if reloaded[0] != 0xaa {
		t.Fatalf("reloaded[0] = 0x%x, want 0xaa", reloaded[0])
	}
}

func TestMemoryStoreApplyOverlayWritesBackAndMarksDirty(t *testing.T) {
	store := NewMemoryStore()
	addr := engine.Address{0x03}
	slot := engine.Hash{0x04}
	ov := overlay.NewState()
	ov.SetBalance(addr, big.NewInt(9))
	ov.SetNonce(addr, 7)
	ov.SetCode(addr, []byte{0x60, 0x01})
	ov.SetStorage(addr, slot, engine.Hash{0xaa})
	store.ApplyOverlay(upstream.LatestBlock(), ov)

	account, ok := store.GetAccount(addr, upstream.LatestBlock())
	if !ok {
		t.Fatalf("account missing after overlay writeback")
	}
	if account.Balance.Cmp(big.NewInt(9)) != 0 || account.Nonce != 7 {
		t.Fatalf("account after writeback = %#v", account)
	}
	code, ok := store.GetCode(addr, upstream.LatestBlock())
	if !ok || len(code) != 2 || code[1] != 0x01 {
		t.Fatalf("code after writeback = %x, ok=%v", code, ok)
	}
	value, ok := store.GetStorage(addr, slot, upstream.LatestBlock())
	if !ok || value != (engine.Hash{0xaa}) {
		t.Fatalf("storage after writeback = %#v, ok=%v", value, ok)
	}
	dirty := store.DirtyState(upstream.LatestBlock())
	entry, ok := dirty.Accounts[addr]
	if !ok {
		t.Fatalf("dirty entry missing")
	}
	if !entry.BalanceDirty || !entry.NonceDirty || !entry.CodeDirty {
		t.Fatalf("dirty entry = %#v, want balance/nonce/code dirty", entry)
	}
	if _, ok := entry.StorageSlots[slot]; !ok {
		t.Fatalf("dirty storage slot not recorded")
	}
}

func TestMemoryStoreApplyStateDiffWritesBack(t *testing.T) {
	store := NewMemoryStore()
	addr := engine.Address{0x05}
	slot := engine.Hash{0x06}
	store.ApplyStateDiff(upstream.BlockNumber(3), &engine.StateDiff{
		BalanceChanges: map[engine.Address]*big.Int{addr: big.NewInt(11)},
		NonceChanges:   map[engine.Address]uint64{addr: 4},
		CodeChanges:    map[engine.Address][]byte{addr: []byte{0x60, 0x02}},
		StorageChanges: map[engine.Address]map[engine.Hash]engine.Hash{addr: {slot: engine.Hash{0xbb}}},
		CreatedAccounts: []engine.Address{addr},
	})
	dirty := store.DirtyState(upstream.BlockNumber(3))
	entry := dirty.Accounts[addr]
	if !entry.Created || !entry.BalanceDirty || !entry.NonceDirty || !entry.CodeDirty {
		t.Fatalf("dirty entry = %#v", entry)
	}
	if _, ok := entry.StorageSlots[slot]; !ok {
		t.Fatalf("storage slot not marked dirty")
	}
}

func TestMemoryStoreExportImportRoundTrip(t *testing.T) {
	store := NewMemoryStore()
	addr := engine.Address{0x07}
	slot := engine.Hash{0x08}
	block := upstream.BlockNumber(11)
	store.PutAccount(addr, block, upstream.AccountSnapshot{Balance: big.NewInt(13), Nonce: 3, Exists: true})
	store.PutCode(addr, block, []byte{0x60, 0x03})
	store.PutStorage(addr, slot, block, engine.Hash{0xcc})
	store.ApplyStateDiff(block, &engine.StateDiff{StorageChanges: map[engine.Address]map[engine.Hash]engine.Hash{addr: {slot: engine.Hash{0xcc}}}})

	exported := store.ExportState()
	restored := NewMemoryStore()
	restored.ImportState(exported)

	account, ok := restored.GetAccount(addr, block)
	if !ok || account.Balance.Cmp(big.NewInt(13)) != 0 || account.Nonce != 3 {
		t.Fatalf("restored account = %#v, ok=%v", account, ok)
	}
	code, ok := restored.GetCode(addr, block)
	if !ok || len(code) != 2 || code[1] != 0x03 {
		t.Fatalf("restored code = %x, ok=%v", code, ok)
	}
	value, ok := restored.GetStorage(addr, slot, block)
	if !ok || value != (engine.Hash{0xcc}) {
		t.Fatalf("restored storage = %#v, ok=%v", value, ok)
	}
	dirty := restored.DirtyState(block)
	if _, ok := dirty.Accounts[addr]; !ok {
		t.Fatalf("restored dirty state missing")
	}
}
