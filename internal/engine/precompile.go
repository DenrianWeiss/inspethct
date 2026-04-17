package engine

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math/big"
	"math/bits"
	"sync"

	gokzg4844 "github.com/crate-crypto/go-kzg-4844"
	secp256k1ecdsa "github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"golang.org/x/crypto/bn256"
	"golang.org/x/crypto/ripemd160"
)

var (
	errInvalidPrecompileInput = errors.New("invalid precompile input")
	kzgContextOnce            sync.Once
	kzgContext                *gokzg4844.Context
	kzgContextErr             error
	blake2IV                  = [8]uint64{
		0x6A09E667F3BCC908,
		0xBB67AE8584CAA73B,
		0x3C6EF372FE94F82B,
		0xA54FF53A5F1D36F1,
		0x510E527FADE682D1,
		0x9B05688C2B3E6C1F,
		0x1F83D9ABFB41BD6B,
		0x5BE0CD19137E2179,
	}
	blake2Sigma = [10][16]uint8{
		{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15},
		{14, 10, 4, 8, 9, 15, 13, 6, 1, 12, 0, 2, 11, 7, 5, 3},
		{11, 8, 12, 0, 5, 2, 15, 13, 10, 14, 3, 6, 7, 1, 9, 4},
		{7, 9, 3, 1, 13, 12, 11, 14, 2, 6, 5, 10, 4, 0, 15, 8},
		{9, 0, 5, 7, 2, 4, 10, 15, 14, 1, 11, 12, 6, 8, 3, 13},
		{2, 12, 6, 10, 0, 11, 8, 3, 4, 13, 7, 5, 15, 14, 1, 9},
		{12, 5, 1, 15, 14, 13, 4, 10, 0, 7, 6, 3, 9, 2, 8, 11},
		{13, 11, 7, 14, 12, 1, 3, 9, 5, 0, 15, 4, 8, 6, 2, 10},
		{6, 15, 14, 9, 11, 3, 0, 8, 12, 2, 13, 7, 1, 4, 10, 5},
		{10, 2, 8, 4, 7, 6, 1, 5, 15, 11, 9, 14, 3, 12, 13, 0},
	}
)

//go:embed trusted_setup.json
var embeddedKZGTrustedSetup []byte

type Precompile interface {
	RequiredGas(input []byte, fork Fork) (uint64, error)
	Run(input []byte, fork Fork) ([]byte, error)
}

type PrecompileRegistry struct {
	mu      sync.RWMutex
	entries map[Address]Precompile
}

func NewPrecompileRegistry() *PrecompileRegistry {
	return &PrecompileRegistry{entries: make(map[Address]Precompile)}
}

func (r *PrecompileRegistry) Clone() *PrecompileRegistry {
	if r == nil {
		return NewPrecompileRegistry()
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	clone := NewPrecompileRegistry()
	for addr, precompile := range r.entries {
		clone.entries[addr] = precompile
	}
	return clone
}

func (r *PrecompileRegistry) Register(addr Address, precompile Precompile) {
	if r == nil || precompile == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[addr] = precompile
}

func (r *PrecompileRegistry) Unregister(addr Address) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, addr)
}

func (r *PrecompileRegistry) Resolve(addr Address) (Precompile, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	precompile, ok := r.entries[addr]
	return precompile, ok
}

func (r *PrecompileRegistry) Addresses() []Address {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	addrs := make([]Address, 0, len(r.entries))
	for addr := range r.entries {
		addrs = append(addrs, addr)
	}
	return addrs
}

func MainnetPrecompilesForFork(fork Fork) *PrecompileRegistry {
	registry := NewPrecompileRegistry()
	registry.Register(precompileAddress(0x01), fixedGasPrecompile{gas: 3000, run: runECRecover})
	registry.Register(precompileAddress(0x02), dynamicGasPrecompile{gas: gasSha256, run: runSha256})
	registry.Register(precompileAddress(0x03), dynamicGasPrecompile{gas: gasRipemd160, run: runRipemd160})
	registry.Register(precompileAddress(0x04), dynamicGasPrecompile{gas: gasIdentity, run: runIdentity})
	registry.Register(precompileAddress(0x05), dynamicGasPrecompile{gas: gasModExp, run: runModExp})
	registry.Register(precompileAddress(0x06), fixedGasPrecompile{gas: 150, run: runBn256Add})
	registry.Register(precompileAddress(0x07), fixedGasPrecompile{gas: 6000, run: runBn256ScalarMul})
	registry.Register(precompileAddress(0x08), dynamicGasPrecompile{gas: gasBn256Pairing, run: runBn256Pairing})
	registry.Register(precompileAddress(0x09), dynamicGasPrecompile{gas: gasBlake2F, run: runBlake2F})
	if forkGTE(fork, ForkCancun) {
		registry.Register(precompileAddress(0x0a), newPointEvaluationPrecompile())
	}
	if forkGTE(fork, ForkAmsterdam) {
		registry.Register(precompileAddressU16(0x0100), fixedGasPrecompile{gas: 6900, run: runP256Verify})
	}
	return registry
}

func MainnetPrecompileAddresses(fork Fork) []Address {
	return MainnetPrecompilesForFork(fork).Addresses()
}

func (evm *EVM) resolvePrecompile(addr Address) (Precompile, bool) {
	if evm == nil || evm.precompiles == nil {
		return nil, false
	}
	return evm.precompiles.Resolve(addr)
}

func (evm *EVM) isPrecompileAddress(addr Address) bool {
	_, ok := evm.resolvePrecompile(addr)
	return ok
}

func (evm *EVM) executePrecompileMessage(msg *Message, precompile Precompile) (*ExecutionResult, error) {
	requiredGas, err := precompile.RequiredGas(msg.Input, evm.fork)
	if err != nil {
		return &ExecutionResult{Status: StatusHalt, GasUsed: msg.Gas, Err: err}, err
	}
	if requiredGas > msg.Gas {
		return &ExecutionResult{Status: StatusOutOfGas, GasUsed: msg.Gas, Err: ErrOutOfGas}, ErrOutOfGas
	}
	output, err := precompile.Run(msg.Input, evm.fork)
	if err != nil {
		return &ExecutionResult{Status: StatusHalt, GasUsed: msg.Gas, Err: err}, err
	}
	return &ExecutionResult{
		Status:       StatusSuccess,
		GasUsed:      requiredGas,
		GasRemaining: msg.Gas - requiredGas,
		ReturnData:   output,
	}, nil
}

type fixedGasPrecompile struct {
	gas uint64
	run func([]byte, Fork) ([]byte, error)
}

func (p fixedGasPrecompile) RequiredGas([]byte, Fork) (uint64, error) {
	return p.gas, nil
}

func (p fixedGasPrecompile) Run(input []byte, fork Fork) ([]byte, error) {
	return p.run(input, fork)
}

type dynamicGasPrecompile struct {
	gas func([]byte, Fork) (uint64, error)
	run func([]byte, Fork) ([]byte, error)
}

type pointEvaluationPrecompile struct {
	ctx *gokzg4844.Context
	err error
}

func (p dynamicGasPrecompile) RequiredGas(input []byte, fork Fork) (uint64, error) {
	return p.gas(input, fork)
}

func (p dynamicGasPrecompile) Run(input []byte, fork Fork) ([]byte, error) {
	return p.run(input, fork)
}

func newPointEvaluationPrecompile() Precompile {
	ctx, err := getKZGContext()
	return pointEvaluationPrecompile{ctx: ctx, err: err}
}

func (p pointEvaluationPrecompile) RequiredGas([]byte, Fork) (uint64, error) {
	if p.err != nil {
		return 0, p.err
	}
	return 50000, nil
}

func (p pointEvaluationPrecompile) Run(input []byte, _ Fork) ([]byte, error) {
	if p.err != nil {
		return nil, p.err
	}
	return verifyPointEvaluationWithContext(p.ctx, input)
}

func precompileAddress(id byte) Address {
	var addr Address
	addr[19] = id
	return addr
}

func precompileAddressU16(id uint16) Address {
	var addr Address
	binary.BigEndian.PutUint16(addr[18:], id)
	return addr
}

func gasSha256(input []byte, _ Fork) (uint64, error) {
	return 60 + 12*wordCount(uint64(len(input))), nil
}

func gasRipemd160(input []byte, _ Fork) (uint64, error) {
	return 600 + 120*wordCount(uint64(len(input))), nil
}

func gasIdentity(input []byte, _ Fork) (uint64, error) {
	return 15 + 3*wordCount(uint64(len(input))), nil
}

func gasModExp(input []byte, _ Fork) (uint64, error) {
	baseLen := readLengthWord(input, 0)
	expLen := readLengthWord(input, 32)
	modLen := readLengthWord(input, 64)
	maxLen := maxUint64(baseLen, modLen)
	words := wordCountBy(maxLen, 8)
	adjExpLen := adjustedExponentLength(expLen, readInputSegment(input, 96+baseLen, minUint64(expLen, 32)))
	if adjExpLen == 0 {
		adjExpLen = 1
	}
	if words == 0 {
		return 200, nil
	}
	product, overflow := safeMulUint64(words, words)
	if overflow == false {
		return ^uint64(0), nil
	}
	product2, overflow := safeMulUint64(product, adjExpLen)
	if overflow == false {
		return ^uint64(0), nil
	}
	gas := product2 / 3
	if gas < 200 {
		gas = 200
	}
	return gas, nil
}

func gasBn256Pairing(input []byte, _ Fork) (uint64, error) {
	if len(input)%192 != 0 {
		return 0, errInvalidPrecompileInput
	}
	return 45000 + uint64(len(input)/192)*34000, nil
}

func gasBlake2F(input []byte, _ Fork) (uint64, error) {
	if len(input) != 213 {
		return 0, errInvalidPrecompileInput
	}
	return uint64(binary.BigEndian.Uint32(input[:4])), nil
}

func runECRecover(input []byte, _ Fork) ([]byte, error) {
	data := rightPadBytes(input, 128)
	hash := data[:32]
	vWord := new(big.Int).SetBytes(data[32:64])
	r := data[64:96]
	s := data[96:128]
	if !vWord.IsUint64() {
		return nil, nil
	}
	v := vWord.Uint64()
	if v == 27 || v == 28 {
		v -= 27
	}
	if v > 1 {
		return nil, nil
	}
	compactSig := make([]byte, 65)
	compactSig[0] = byte(27 + v)
	copy(compactSig[1:33], r)
	copy(compactSig[33:], s)
	pubKey, _, err := secp256k1ecdsa.RecoverCompact(compactSig, hash)
	if err != nil {
		return nil, nil
	}
	serialized := pubKey.SerializeUncompressed()
	addrHash := keccak256(serialized[1:])
	output := make([]byte, 32)
	copy(output[12:], addrHash[12:])
	return output, nil
}

func runSha256(input []byte, _ Fork) ([]byte, error) {
	sum := sha256.Sum256(input)
	return sum[:], nil
}

func runRipemd160(input []byte, _ Fork) ([]byte, error) {
	h := ripemd160.New()
	_, _ = h.Write(input)
	sum := h.Sum(nil)
	output := make([]byte, 32)
	copy(output[12:], sum)
	return output, nil
}

func runIdentity(input []byte, _ Fork) ([]byte, error) {
	output := make([]byte, len(input))
	copy(output, input)
	return output, nil
}

func runModExp(input []byte, _ Fork) ([]byte, error) {
	baseLen := readLengthWord(input, 0)
	expLen := readLengthWord(input, 32)
	modLen := readLengthWord(input, 64)
	base := new(big.Int).SetBytes(readInputSegment(input, 96, baseLen))
	exp := new(big.Int).SetBytes(readInputSegment(input, 96+baseLen, expLen))
	modulusBytes := readInputSegment(input, 96+baseLen+expLen, modLen)
	if modLen == 0 {
		return nil, nil
	}
	modulus := new(big.Int).SetBytes(modulusBytes)
	result := new(big.Int)
	if modulus.Sign() == 0 {
		return make([]byte, modLen), nil
	}
	result.Exp(base, exp, modulus)
	return leftPadBytes(result.Bytes(), int(modLen)), nil
}

func runBn256Add(input []byte, _ Fork) ([]byte, error) {
	data := rightPadBytes(input, 128)
	p1, ok := new(bn256.G1).Unmarshal(data[:64])
	if !ok {
		return nil, errInvalidPrecompileInput
	}
	p2, ok := new(bn256.G1).Unmarshal(data[64:128])
	if !ok {
		return nil, errInvalidPrecompileInput
	}
	return new(bn256.G1).Add(p1, p2).Marshal(), nil
}

func runBn256ScalarMul(input []byte, _ Fork) ([]byte, error) {
	data := rightPadBytes(input, 96)
	p, ok := new(bn256.G1).Unmarshal(data[:64])
	if !ok {
		return nil, errInvalidPrecompileInput
	}
	scalar := new(big.Int).SetBytes(data[64:96])
	return new(bn256.G1).ScalarMult(p, scalar).Marshal(), nil
}

func runBn256Pairing(input []byte, _ Fork) ([]byte, error) {
	if len(input)%192 != 0 {
		return nil, errInvalidPrecompileInput
	}
	accum := newGTIdentity()
	for offset := 0; offset < len(input); offset += 192 {
		g1, ok := new(bn256.G1).Unmarshal(input[offset : offset+64])
		if !ok {
			return nil, errInvalidPrecompileInput
		}
		g2, ok := new(bn256.G2).Unmarshal(input[offset+64 : offset+192])
		if !ok {
			return nil, errInvalidPrecompileInput
		}
		accum.Add(accum, bn256.Pair(g1, g2))
	}
	output := make([]byte, 32)
	if string(accum.Marshal()) == string(newGTIdentity().Marshal()) {
		output[31] = 1
	}
	return output, nil
}

func runBlake2F(input []byte, _ Fork) ([]byte, error) {
	if len(input) != 213 {
		return nil, errInvalidPrecompileInput
	}
	if input[212] != 0 && input[212] != 1 {
		return nil, errInvalidPrecompileInput
	}
	rounds := binary.BigEndian.Uint32(input[:4])
	var h [8]uint64
	var m [16]uint64
	for i := 0; i < 8; i++ {
		h[i] = binary.LittleEndian.Uint64(input[4+i*8:])
	}
	for i := 0; i < 16; i++ {
		m[i] = binary.LittleEndian.Uint64(input[68+i*8:])
	}
	t0 := binary.LittleEndian.Uint64(input[196:204])
	t1 := binary.LittleEndian.Uint64(input[204:212])
	out := blake2Compress(h, m, [2]uint64{t0, t1}, input[212] == 1, rounds)
	result := make([]byte, 64)
	for i := 0; i < 8; i++ {
		binary.LittleEndian.PutUint64(result[i*8:], out[i])
	}
	return result, nil
}

func verifyPointEvaluationWithContext(ctx *gokzg4844.Context, input []byte) ([]byte, error) {
	if len(input) != 192 {
		return nil, errInvalidPrecompileInput
	}
	var versionedHash [32]byte
	copy(versionedHash[:], input[:32])
	var z gokzg4844.Scalar
	copy(z[:], input[32:64])
	var y gokzg4844.Scalar
	copy(y[:], input[64:96])
	var commitment gokzg4844.KZGCommitment
	copy(commitment[:], input[96:144])
	var proof gokzg4844.KZGProof
	copy(proof[:], input[144:192])
	computedHash := kzgVersionedHash(commitment)
	if computedHash != versionedHash {
		return nil, errInvalidPrecompileInput
	}
	if err := ctx.VerifyKZGProof(commitment, z, y, proof); err != nil {
		return nil, errInvalidPrecompileInput
	}
	output := make([]byte, 64)
	output[30] = 0x10
	output[31] = 0x00
	copy(output[32:], gokzg4844.BlsModulus[:])
	return output, nil
}

func runP256Verify(input []byte, _ Fork) ([]byte, error) {
	if len(input) != 160 {
		return nil, nil
	}
	hash := input[:32]
	r := new(big.Int).SetBytes(input[32:64])
	s := new(big.Int).SetBytes(input[64:96])
	qx := new(big.Int).SetBytes(input[96:128])
	qy := new(big.Int).SetBytes(input[128:160])
	pubKey := ecdsa.PublicKey{Curve: elliptic.P256(), X: qx, Y: qy}
	output := make([]byte, 32)
	if pubKey.Curve.IsOnCurve(qx, qy) && ecdsa.Verify(&pubKey, hash, r, s) {
		output[31] = 1
	}
	return output, nil
}

func getKZGContext() (*gokzg4844.Context, error) {
	kzgContextOnce.Do(func() {
		params := new(gokzg4844.JSONTrustedSetup)
		if err := json.Unmarshal(embeddedKZGTrustedSetup, params); err != nil {
			kzgContextErr = err
			return
		}
		kzgContext, kzgContextErr = gokzg4844.NewContext4096(params)
	})
	return kzgContext, kzgContextErr
}

func kzgVersionedHash(commitment gokzg4844.KZGCommitment) [32]byte {
	hash := sha256.Sum256(commitment[:])
	hash[0] = 0x01
	return hash
}

func newGTIdentity() *bn256.GT {
	zeroG1 := make([]byte, 64)
	zeroG2 := make([]byte, 128)
	g1, _ := new(bn256.G1).Unmarshal(zeroG1)
	g2, _ := new(bn256.G2).Unmarshal(zeroG2)
	return bn256.Pair(g1, g2)
}

func adjustedExponentLength(expLen uint64, head []byte) uint64 {
	if expLen == 0 {
		return 0
	}
	if expLen <= 32 {
		return uint64(bitLen(head)) - 1
	}
	return 8*(expLen-32) + uint64(bitLen(head)) - 1
}

func bitLen(data []byte) int {
	for i, b := range data {
		if b == 0 {
			continue
		}
		return (len(data)-i-1)*8 + bitsLen8(b)
	}
	return 0
}

func bitsLen8(value byte) int {
	length := 0
	for value > 0 {
		length++
		value >>= 1
	}
	return length
}

func readLengthWord(input []byte, offset int) uint64 {
	if len(input) < offset+32 {
		return 0
	}
	length := new(big.Int).SetBytes(input[offset : offset+32])
	if !length.IsUint64() {
		return ^uint64(0)
	}
	return length.Uint64()
}

func readInputSegment(input []byte, offset uint64, size uint64) []byte {
	if size == 0 {
		return nil
	}
	if offset >= uint64(len(input)) {
		return make([]byte, size)
	}
	end := offset + size
	if end <= uint64(len(input)) {
		segment := make([]byte, size)
		copy(segment, input[offset:end])
		return segment
	}
	segment := make([]byte, size)
	copy(segment, input[offset:])
	return segment
}

func rightPadBytes(input []byte, size int) []byte {
	if len(input) >= size {
		return input[:size]
	}
	output := make([]byte, size)
	copy(output, input)
	return output
}

func leftPadBytes(input []byte, size int) []byte {
	if len(input) >= size {
		return input[len(input)-size:]
	}
	output := make([]byte, size)
	copy(output[size-len(input):], input)
	return output
}

func maxUint64(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}

func minUint64(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}

func wordCountBy(size uint64, divisor uint64) uint64 {
	if size == 0 {
		return 0
	}
	return (size + divisor - 1) / divisor
}

func blake2Compress(h [8]uint64, m [16]uint64, counter [2]uint64, final bool, rounds uint32) [8]uint64 {
	v := [16]uint64{
		h[0], h[1], h[2], h[3], h[4], h[5], h[6], h[7],
		blake2IV[0], blake2IV[1], blake2IV[2], blake2IV[3],
		blake2IV[4], blake2IV[5], blake2IV[6], blake2IV[7],
	}
	v[12] ^= counter[0]
	v[13] ^= counter[1]
	if final {
		v[14] = ^v[14]
	}
	for i := uint32(0); i < rounds; i++ {
		s := blake2Sigma[i%10]
		blake2G(&v, 0, 4, 8, 12, m[s[0]], m[s[1]])
		blake2G(&v, 1, 5, 9, 13, m[s[2]], m[s[3]])
		blake2G(&v, 2, 6, 10, 14, m[s[4]], m[s[5]])
		blake2G(&v, 3, 7, 11, 15, m[s[6]], m[s[7]])
		blake2G(&v, 0, 5, 10, 15, m[s[8]], m[s[9]])
		blake2G(&v, 1, 6, 11, 12, m[s[10]], m[s[11]])
		blake2G(&v, 2, 7, 8, 13, m[s[12]], m[s[13]])
		blake2G(&v, 3, 4, 9, 14, m[s[14]], m[s[15]])
	}
	for i := 0; i < 8; i++ {
		h[i] ^= v[i] ^ v[i+8]
	}
	return h
}

func blake2G(v *[16]uint64, a, b, c, d int, x, y uint64) {
	v[a] = v[a] + v[b] + x
	v[d] = bits.RotateLeft64(v[d]^v[a], -32)
	v[c] += v[d]
	v[b] = bits.RotateLeft64(v[b]^v[c], -24)
	v[a] = v[a] + v[b] + y
	v[d] = bits.RotateLeft64(v[d]^v[a], -16)
	v[c] += v[d]
	v[b] = bits.RotateLeft64(v[b]^v[c], -63)
}
