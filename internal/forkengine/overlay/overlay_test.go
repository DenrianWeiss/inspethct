package overlay

import (
	"math/big"
	"testing"

	"inspethct/internal/engine"
)

func TestStateSnapshotRevertRestoresAccountsAndStorage(t *testing.T) {
	state := NewState()
	addr := engine.Address{0x01}
	slot := engine.Hash{0x02}
	state.SetBalance(addr, big.NewInt(5))
	state.SetStorage(addr, slot, engine.Hash{0xaa})

	snap := state.Snapshot()
	state.SetBalance(addr, big.NewInt(9))
	state.SetStorage(addr, slot, engine.Hash{0xbb})
	state.SelfDestruct(addr)
	state.RevertToSnapshot(snap)

	account, ok := state.GetAccount(addr)
	if !ok {
		t.Fatalf("account missing after revert")
	}
	if account.Balance.Cmp(big.NewInt(5)) != 0 {
		t.Fatalf("balance after revert = %s, want 5", account.Balance.String())
	}
	if !account.BalanceDirty {
		t.Fatalf("balance dirty flag lost after revert")
	}
	if account.SelfDestructed {
		t.Fatalf("selfdestruct flag persisted after revert")
	}
	value, ok := state.GetStorage(addr, slot)
	if !ok {
		t.Fatalf("storage missing after revert")
	}
	if value != (engine.Hash{0xaa}) {
		t.Fatalf("storage after revert = %#v, want %#v", value, engine.Hash{0xaa})
	}
}

func TestStateTracksDirtyAccountFields(t *testing.T) {
	state := NewState()
	addr := engine.Address{0x03}
	state.SetBalance(addr, big.NewInt(7))
	state.SetNonce(addr, 2)
	state.SetCode(addr, []byte{0x60, 0x00})
	account, ok := state.GetAccount(addr)
	if !ok {
		t.Fatalf("account missing")
	}
	if !account.BalanceDirty || !account.NonceDirty || !account.CodeDirty {
		t.Fatalf("dirty flags = %#v, want all true", account)
	}
}
