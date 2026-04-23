package varpeeker

import (
	"math/big"
	"testing"

	"golang.org/x/crypto/sha3"

	"inspethct/internal/engine"
	"inspethct/internal/srcmap"
)

func keccakBuf(b []byte) *big.Int {
	h := sha3.NewLegacyKeccak256()
	_, _ = h.Write(b)
	return new(big.Int).SetBytes(h.Sum(nil))
}

func buildStorageVarsForTest(layout srcmap.StorageLayout, scope srcmap.StorageScope) []srcmap.StorageVariableMapping {
	out := make([]srcmap.StorageVariableMapping, 0, len(layout.Storage))
	for _, entry := range layout.Storage {
		out = append(out, srcmap.StorageVariableMapping{
			Entry: entry,
			Type:  layout.Types[entry.Type],
			Scope: scope,
		})
	}
	return out
}

// fakeState is the minimum engine.ReadOnlyState we need for varpeeker
// tests. All unused methods return zero values.
type fakeState struct {
	pc       uint64
	stack    []engine.Word // index 0 is the bottom, len-1 is the top
	memory   []byte
	storage  map[engine.Address]map[engine.Hash]engine.Hash
	tstorage map[engine.Address]map[engine.Hash]engine.Hash
	contract engine.Address
	code     []byte
}

func (s *fakeState) StackLen() int { return len(s.stack) }
func (s *fakeState) StackPeekN(n int) engine.Word {
	if n < 0 || n >= len(s.stack) {
		return engine.Word{}
	}
	return s.stack[len(s.stack)-1-n]
}
func (s *fakeState) MemoryLen() int { return len(s.memory) }
func (s *fakeState) MemoryGet(offset, size uint64) []byte {
	if offset >= uint64(len(s.memory)) {
		return make([]byte, size)
	}
	end := offset + size
	if end > uint64(len(s.memory)) {
		out := make([]byte, size)
		copy(out, s.memory[offset:])
		return out
	}
	out := make([]byte, size)
	copy(out, s.memory[offset:end])
	return out
}
func (s *fakeState) StorageGet(addr engine.Address, slot engine.Hash) engine.Hash {
	if s.storage == nil {
		return engine.Hash{}
	}
	return s.storage[addr][slot]
}
func (s *fakeState) TransientStorageGet(addr engine.Address, slot engine.Hash) engine.Hash {
	if s.tstorage == nil {
		return engine.Hash{}
	}
	return s.tstorage[addr][slot]
}
func (s *fakeState) Balance(engine.Address) *big.Int        { return new(big.Int) }
func (s *fakeState) Nonce(engine.Address) uint64            { return 0 }
func (s *fakeState) Code(engine.Address) []byte             { return s.code }
func (s *fakeState) CodeSize(engine.Address) int            { return len(s.code) }
func (s *fakeState) CodeHash(engine.Address) engine.Hash    { return engine.Hash{} }
func (s *fakeState) Exists(engine.Address) bool             { return true }
func (s *fakeState) GasRemaining() uint64                   { return 0 }
func (s *fakeState) GasRefund() uint64                      { return 0 }
func (s *fakeState) PC() uint64                             { return s.pc }
func (s *fakeState) ReturnData() []byte                     { return nil }
func (s *fakeState) CallDepth() int                         { return 0 }
func (s *fakeState) IsStatic() bool                         { return false }
func (s *fakeState) BlockContext() engine.BlockContext      { return nil }
func (s *fakeState) TxContext() engine.TxContext            { return nil }
func (s *fakeState) ContractAddress() engine.Address        { return s.contract }
func (s *fakeState) ContractCaller() engine.Address         { return engine.Address{} }
func (s *fakeState) ContractCallValue() *big.Int            { return new(big.Int) }
func (s *fakeState) ContractCallInput() []byte              { return nil }
func (s *fakeState) ContractCode() []byte                   { return s.code }
func (s *fakeState) ContractCodeAddr() engine.Address       { return s.contract }
func (s *fakeState) Logs() []engine.Log                     { return nil }
func (s *fakeState) IsAddressWarmed(engine.Address) bool    { return false }
func (s *fakeState) IsSlotWarmed(engine.Address, engine.Hash) bool {
	return false
}

func wordFromUint64(v uint64) engine.Word {
	var w engine.Word
	big.NewInt(0).SetUint64(v).FillBytes(w[:])
	return w
}

func hashFromUint64(v uint64) engine.Hash {
	var h engine.Hash
	big.NewInt(0).SetUint64(v).FillBytes(h[:])
	return h
}

func TestDecodeStackValuePrimitives(t *testing.T) {
	type tc struct {
		typeStr string
		word    engine.Word
		want    string
	}
	addrWord := engine.Word{}
	addrWord[12] = 0xde
	addrWord[31] = 0xad

	cases := []tc{
		{"uint256", wordFromUint64(42), "42 (0x000000000000000000000000000000000000000000000000000000000000002a)"},
		{"bool", wordFromUint64(1), "true"},
		{"bool", engine.Word{}, "false"},
		{"address", addrWord, "0xde000000000000000000000000000000000000ad"},
	}
	for _, c := range cases {
		got, _, _ := decodeStackValue(c.word, c.typeStr, "stack", nil)
		if got != c.want {
			t.Errorf("decodeStackValue(%q, %x) = %q, want %q", c.typeStr, c.word[:], got, c.want)
		}
	}
}

func TestPeekStorageInplaceAndDynamicArray(t *testing.T) {
	addr := engine.Address{0x01}
	idx := &srcmap.Index{
		PersistentLayout: srcmap.StorageLayout{
			Storage: []srcmap.StorageEntry{
				{Label: "x", Slot: "0", Offset: 0, Type: "t_uint256"},
				{Label: "arr", Slot: "1", Offset: 0, Type: "t_array_uint256_dyn"},
			},
			Types: map[string]srcmap.StorageType{
				"t_uint256":           {Encoding: "inplace", Label: "uint256", NumberOfBytes: "32"},
				"t_array_uint256_dyn": {Encoding: "dynamic_array", Label: "uint256[]", NumberOfBytes: "32", Base: "t_uint256"},
			},
		},
	}
	idx.StorageVariables = buildStorageVarsForTest(idx.PersistentLayout, srcmap.StorageScopePersistent)

	state := &fakeState{
		storage:  map[engine.Address]map[engine.Hash]engine.Hash{},
		contract: addr,
	}
	state.storage[addr] = map[engine.Hash]engine.Hash{}
	// x at slot 0 = 99
	state.storage[addr][hashFromUint64(0)] = hashFromUint64(99)
	// arr length at slot 1 = 2
	state.storage[addr][hashFromUint64(1)] = hashFromUint64(2)
	// elements at keccak(slot=1) and +1
	dataBase := keccakBig(big.NewInt(1))
	state.storage[addr][bigIntToHash(dataBase)] = hashFromUint64(11)
	state.storage[addr][bigIntToHash(new(big.Int).Add(dataBase, big.NewInt(1)))] = hashFromUint64(22)

	vars := peekStorageVars(idx, state, addr, srcmap.StorageScopePersistent)
	if len(vars) != 2 {
		t.Fatalf("len(vars) = %d, want 2", len(vars))
	}
	if vars[0].Name != "x" || vars[0].Value == "" {
		t.Fatalf("x not decoded: %+v", vars[0])
	}
	if vars[1].Name != "arr" || len(vars[1].Children) != 2 {
		t.Fatalf("arr not decoded: %+v", vars[1])
	}
	if vars[1].Children[0].Value == "" || vars[1].Children[1].Value == "" {
		t.Fatalf("arr children empty: %+v", vars[1].Children)
	}
}

func TestPeekStorageBytesShortAndLong(t *testing.T) {
	addr := engine.Address{0x02}
	idx := &srcmap.Index{
		PersistentLayout: srcmap.StorageLayout{
			Storage: []srcmap.StorageEntry{
				{Label: "name", Slot: "0", Type: "t_string"},
				{Label: "blob", Slot: "1", Type: "t_bytes"},
			},
			Types: map[string]srcmap.StorageType{
				"t_string": {Encoding: "bytes", Label: "string"},
				"t_bytes":  {Encoding: "bytes", Label: "bytes"},
			},
		},
	}
	idx.StorageVariables = buildStorageVarsForTest(idx.PersistentLayout, srcmap.StorageScopePersistent)

	state := &fakeState{storage: map[engine.Address]map[engine.Hash]engine.Hash{}, contract: addr}
	state.storage[addr] = map[engine.Hash]engine.Hash{}

	// short string "hi" (length 2): bytes [0..30] = data right-padded?
	// Solidity short layout: data stored in HIGH bytes, length*2 in last byte.
	var shortSlot engine.Hash
	shortSlot[0] = 'h'
	shortSlot[1] = 'i'
	shortSlot[31] = byte(2 * 2) // 2*2=4, low bit 0
	state.storage[addr][hashFromUint64(0)] = shortSlot

	// long bytes: length 40, so slot value = 40*2+1 = 81
	state.storage[addr][hashFromUint64(1)] = hashFromUint64(40*2 + 1)
	dataBase := keccakBig(big.NewInt(1))
	var chunk0, chunk1 engine.Hash
	for i := 0; i < 32; i++ {
		chunk0[i] = byte(i + 1)
	}
	for i := 0; i < 8; i++ {
		chunk1[i] = byte(0xa0 + i)
	}
	state.storage[addr][bigIntToHash(dataBase)] = chunk0
	state.storage[addr][bigIntToHash(new(big.Int).Add(dataBase, big.NewInt(1)))] = chunk1

	vars := peekStorageVars(idx, state, addr, srcmap.StorageScopePersistent)
	if len(vars) != 2 {
		t.Fatalf("len(vars) = %d", len(vars))
	}
	if vars[0].Value == "" {
		t.Fatalf("short string not decoded: %+v", vars[0])
	}
	if vars[1].Value == "" {
		t.Fatalf("long bytes not decoded: %+v", vars[1])
	}
}

func TestPeekImmutables(t *testing.T) {
	idx := &srcmap.Index{
		NodesByID: map[int]*srcmap.ASTNode{
			42: {ID: 42, NodeType: "VariableDeclaration", Name: "OWNER", Raw: map[string]any{
				"name": "OWNER",
				"typeDescriptions": map[string]any{
					"typeString": "address",
				},
			}},
		},
		ImmutableReferences: map[string][]srcmap.ImmutableReferenceSegment{
			"42": {{Start: 4, Length: 32}},
		},
	}
	runtime := make([]byte, 64)
	runtime[4+12] = 0xab // address byte 0
	runtime[4+31] = 0xcd // address last byte
	out := peekImmutables(idx, runtime)
	if len(out) != 1 {
		t.Fatalf("len(out) = %d", len(out))
	}
	if out[0].Name != "OWNER" || out[0].Value == "" {
		t.Fatalf("immutable not decoded: %+v", out[0])
	}
}

func TestPeekStorageKeyMapping(t *testing.T) {
	addr := engine.Address{0x03}
	idx := &srcmap.Index{
		PersistentLayout: srcmap.StorageLayout{
			Storage: []srcmap.StorageEntry{
				{Label: "balances", Slot: "0", Type: "t_mapping_addr_uint"},
			},
			Types: map[string]srcmap.StorageType{
				"t_mapping_addr_uint": {Encoding: "mapping", Label: "mapping(address => uint256)", Key: "t_address", Value: "t_uint256"},
				"t_uint256":           {Encoding: "inplace", Label: "uint256", NumberOfBytes: "32"},
				"t_address":           {Encoding: "inplace", Label: "address", NumberOfBytes: "20"},
			},
		},
	}
	idx.StorageVariables = buildStorageVarsForTest(idx.PersistentLayout, srcmap.StorageScopePersistent)

	keyAddr := make([]byte, 20)
	keyAddr[19] = 0x42
	state := &fakeState{storage: map[engine.Address]map[engine.Hash]engine.Hash{addr: {}}, contract: addr}

	root := big.NewInt(0)
	keyPadded := padTo32(keyAddr)
	rootPadded := padTo32(root.Bytes())
	buf := append(append([]byte(nil), keyPadded...), rootPadded...)
	bufHashSlot := keccakBuf(buf)
	state.storage[addr][bigIntToHash(bufHashSlot)] = hashFromUint64(123)

	v, ok := PeekStorageKey(idx, state, addr, "balances", keyAddr)
	if !ok {
		t.Fatalf("PeekStorageKey returned false")
	}
	if v.Value == "" {
		t.Fatalf("mapping value not decoded: %+v", v)
	}
}
