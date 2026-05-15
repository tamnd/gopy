// Package _random is the gopy port of CPython's _randommodule.c. It
// exports a single class, Random, backed by a locked Go math/rand.Rand
// source. The class mirrors CPython's RandomObject interface: seed,
// random, getrandbits, getstate, and setstate.
//
// CPython: Modules/_randommodule.c:1 (module init)

package _random

import (
	"fmt"
	"math/big"
	"math/rand"
	"sync"
	"time"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("_random", buildModule)
}

// buildModule materializes the _random module dict. Mirrors the
// PyInit__random entry point.
//
// CPython: Modules/_randommodule.c:670 _randommodule
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_random")
	d := m.Dict()
	if err := d.SetItem(objects.NewStr("Random"), RandomType); err != nil {
		return nil, err
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// mtSource is a locked math/rand source that carries the full MT state
// so getstate/setstate can round-trip it. We use Go's standard Rand with
// a custom mt19937 state block; for portability with Lib/random.py the
// state tuple layout must match CPython's N=624 + index format.
// ---------------------------------------------------------------------------

// N is the MT19937 state vector length.
// CPython: Modules/_randommodule.c:93 N
const mtN = 624

// mtSource implements rand.Source64 wrapping a pure-Go MT19937.
// A sync.Mutex protects concurrent access, mirroring CPython's
// critical-section annotations on every Random method.
//
// CPython: Modules/_randommodule.c:104 RandomObject
type mtSource struct {
	mu    sync.Mutex
	state [mtN]uint32
	index int
}

// newMTSource allocates a source seeded from the current wall clock and
// process-entropy.
//
// CPython: Modules/_randommodule.c:263 random_seed_time_pid
func newMTSource() *mtSource {
	s := &mtSource{}
	s.initGenrand(uint32(time.Now().UnixNano())) //nolint:gosec
	return s
}

// initGenrand initialises the MT state from a single 32-bit seed.
//
// CPython: Modules/_randommodule.c:196 init_genrand
func (s *mtSource) initGenrand(seed uint32) {
	s.state[0] = seed
	for i := 1; i < mtN; i++ {
		prev := s.state[i-1]
		s.state[i] = 1812433253*(prev^(prev>>30)) + uint32(i)
	}
	s.index = mtN
}

// initByArray initialises the MT state from a key array.
//
// CPython: Modules/_randommodule.c:219 init_by_array
func (s *mtSource) initByArray(key []uint32) {
	s.initGenrand(19650218)
	i, j := 1, 0
	k := mtN
	if len(key) > k {
		k = len(key)
	}
	for ; k > 0; k-- {
		prev := s.state[i-1]
		s.state[i] = (s.state[i]^((prev^(prev>>30))*1664525)) +
			key[j] + uint32(j)
		i++
		j++
		if i >= mtN {
			s.state[0] = s.state[mtN-1]
			i = 1
		}
		if j >= len(key) {
			j = 0
		}
	}
	for k = mtN - 1; k > 0; k-- {
		prev := s.state[i-1]
		s.state[i] = (s.state[i]^((prev^(prev>>30))*1566083941)) -
			uint32(i)
		i++
		if i >= mtN {
			s.state[0] = s.state[mtN-1]
			i = 1
		}
	}
	s.state[0] = 0x80000000
}

// genUint32 generates the next raw 32-bit value. Must be called with
// mu held.
//
// CPython: Modules/_randommodule.c:134 genrand_uint32
func (s *mtSource) genUint32() uint32 {
	const (
		matrixA  = 0x9908b0df
		upperMsk = 0x80000000
		lowerMsk = 0x7fffffff
	)
	mag01 := [2]uint32{0, matrixA}

	if s.index >= mtN {
		for kk := 0; kk < mtN-397; kk++ {
			y := (s.state[kk] & upperMsk) | (s.state[kk+1] & lowerMsk)
			s.state[kk] = s.state[kk+397] ^ (y >> 1) ^ mag01[y&1]
		}
		for kk := mtN - 397; kk < mtN-1; kk++ {
			y := (s.state[kk] & upperMsk) | (s.state[kk+1] & lowerMsk)
			s.state[kk] = s.state[kk+(397-mtN)] ^ (y >> 1) ^ mag01[y&1]
		}
		y := (s.state[mtN-1] & upperMsk) | (s.state[0] & lowerMsk)
		s.state[mtN-1] = s.state[396] ^ (y >> 1) ^ mag01[y&1]
		s.index = 0
	}

	y := s.state[s.index]
	s.index++

	// Tempering.
	y ^= y >> 11
	y ^= (y << 7) & 0x9d2c5680
	y ^= (y << 15) & 0xefc60000
	y ^= y >> 18
	return y
}

// Int63 satisfies rand.Source.
func (s *mtSource) Int63() int64 {
	s.mu.Lock()
	a := int64(s.genUint32()) >> 5
	b := int64(s.genUint32()) >> 6
	s.mu.Unlock()
	return (a*67108864 + b) & (1<<53 - 1)
}

// Uint64 satisfies rand.Source64.
func (s *mtSource) Uint64() uint64 {
	s.mu.Lock()
	hi := uint64(s.genUint32())
	lo := uint64(s.genUint32())
	s.mu.Unlock()
	return hi<<32 | lo
}

// Seed satisfies rand.Source.
func (s *mtSource) Seed(n int64) {
	s.mu.Lock()
	s.initGenrand(uint32(n))
	s.mu.Unlock()
}

// ---------------------------------------------------------------------------
// Random type.
// ---------------------------------------------------------------------------

// RandomType is the _random.Random class.
//
// CPython: Modules/_randommodule.c:597 Random_Type_spec
var RandomType = newRandomType()

// RandomObject backs a _random.Random instance.
//
// CPython: Modules/_randommodule.c:104 RandomObject
type RandomObject struct {
	objects.Header
	src   *mtSource
	rnd   *rand.Rand
	attrs *objects.Dict
}

// AttrDict satisfies objects.AttrDictHolder so a Python subclass of
// _random.Random (the random.Random class in Lib/random.py) can store
// per-instance attributes through GenericSetAttr.
func (o *RandomObject) AttrDict() *objects.Dict { return o.attrs }

// EnsureAttrDict allocates the per-instance attribute dict on first
// write.
func (o *RandomObject) EnsureAttrDict() *objects.Dict {
	if o.attrs == nil {
		o.attrs = objects.NewDict()
	}
	return o.attrs
}

func newRandomType() *objects.Type {
	t := objects.NewType("Random", []*objects.Type{objects.ObjectType()})
	t.TpNew = randomNew
	t.Getattro = objects.GenericGetAttr

	objects.SetTypeDescr(t, "random", objects.NewMethodDescr(t, "random", randomRandom))
	objects.SetTypeDescr(t, "seed", objects.NewMethodDescr(t, "seed", randomSeed))
	objects.SetTypeDescr(t, "getrandbits", objects.NewMethodDescr(t, "getrandbits", randomGetrandbits))
	objects.SetTypeDescr(t, "getstate", objects.NewMethodDescr(t, "getstate", randomGetstate))
	objects.SetTypeDescr(t, "setstate", objects.NewMethodDescr(t, "setstate", randomSetstate))
	return t
}

// randomNew implements Random.__new__. Creates a new instance seeded
// from the wall clock.
//
// CPython: Modules/_randommodule.c:552 random_init
func randomNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	src := newMTSource()
	o := &RandomObject{
		src: src,
		rnd: rand.New(src), //nolint:gosec
	}
	o.Init(cls)
	return o, nil
}

// randomRandom returns the next float in [0.0, 1.0).
//
// CPython: Modules/_randommodule.c:188 _random_Random_random_impl
func randomRandom(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	o, err := selfRandom(args)
	if err != nil {
		return nil, err
	}
	o.src.mu.Lock()
	a := uint64(o.src.genUint32() >> 5)
	b := uint64(o.src.genUint32() >> 6)
	o.src.mu.Unlock()
	f := (float64(a)*67108864.0 + float64(b)) * (1.0 / 9007199254740992.0)
	return objects.NewFloat(f), nil
}

// randomSeed seeds the generator from n. n may be None (use time),
// an int, or bytes.
//
// CPython: Modules/_randommodule.c:396 _random_Random_seed_impl
func randomSeed(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	o, err := selfRandom(args)
	if err != nil {
		return nil, err
	}
	var n objects.Object
	if len(args) >= 2 {
		n = args[1]
	}

	o.src.mu.Lock()
	defer o.src.mu.Unlock()

	if n == nil || objects.IsNone(n) {
		// None: re-seed from wall clock time.
		// CPython: Modules/_randommodule.c:263 random_seed_time_pid
		now := time.Now().UnixNano()
		key := []uint32{
			uint32(now & 0xffffffff),
			uint32(uint64(now) >> 32),
		}
		o.src.initByArray(key)
		return objects.None(), nil
	}

	switch v := n.(type) {
	case *objects.Int:
		// Derive a key array from the absolute value of the integer.
		// CPython: Modules/_randommodule.c:337 "split n into 32-bit chunks"
		b := v.BigInt()
		if b.Sign() < 0 {
			b.Neg(b)
		}
		if b.Sign() == 0 {
			o.src.initByArray([]uint32{0})
			return objects.None(), nil
		}
		// Extract little-endian 32-bit words.
		key := bigIntToKey(b)
		o.src.initByArray(key)
		return objects.None(), nil

	case *objects.Bytes:
		raw := v.Bytes()
		key := bytesToKey(raw)
		o.src.initByArray(key)
		return objects.None(), nil

	default:
		return nil, fmt.Errorf("TypeError: seed must be int, bytes, or None")
	}
}

// randomGetrandbits returns a k-bit non-negative integer.
//
// CPython: Modules/_randommodule.c:507 _random_Random_getrandbits_impl
func randomGetrandbits(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	o, err := selfRandom(args)
	if err != nil {
		return nil, err
	}
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: getrandbits() requires 1 argument")
	}
	kObj, ok := args[1].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: an integer is required")
	}
	k64, fits := kObj.Int64()
	if !fits || k64 < 0 {
		return nil, fmt.Errorf("OverflowError: k too large")
	}
	k := uint64(k64)

	if k == 0 {
		return objects.NewInt(0), nil
	}

	o.src.mu.Lock()
	defer o.src.mu.Unlock()

	// Fast path: fits in 32 bits.
	// CPython: Modules/_randommodule.c:518 "if (k <= 32) Fast path"
	if k <= 32 {
		v := o.src.genUint32() >> (32 - k)
		return objects.NewInt(int64(v)), nil
	}

	// Slow path: fill 32-bit words, big.Int from bytes.
	// CPython: Modules/_randommodule.c:525 words = ...
	words := (k-1)/32 + 1
	wordArr := make([]uint32, words)
	rem := k
	for i := uint64(0); i < words; i++ {
		r := o.src.genUint32()
		if rem < 32 {
			r >>= 32 - rem
		}
		wordArr[i] = r
		rem -= 32
	}
	// Pack into a little-endian byte slice and build a big.Int.
	buf := make([]byte, words*4)
	for i, w := range wordArr {
		buf[i*4+0] = byte(w)
		buf[i*4+1] = byte(w >> 8)
		buf[i*4+2] = byte(w >> 16)
		buf[i*4+3] = byte(w >> 24)
	}
	// big.Int.SetBytes is big-endian, so reverse.
	reverseBytes(buf)
	b := new(big.Int).SetBytes(buf)
	return objects.NewIntFromBig(b), nil
}

// randomGetstate returns the internal MT state as a tuple of 625 ints
// (624 state words + the index).
//
// CPython: Modules/_randommodule.c:415 _random_Random_getstate_impl
func randomGetstate(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	o, err := selfRandom(args)
	if err != nil {
		return nil, err
	}
	o.src.mu.Lock()
	defer o.src.mu.Unlock()

	items := make([]objects.Object, mtN+1)
	for i := 0; i < mtN; i++ {
		items[i] = objects.NewInt(int64(o.src.state[i]))
	}
	items[mtN] = objects.NewInt(int64(o.src.index))
	return objects.NewTuple(items), nil
}

// randomSetstate restores MT state from a tuple produced by getstate().
//
// CPython: Modules/_randommodule.c:455 _random_Random_setstate_impl
func randomSetstate(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	o, err := selfRandom(args)
	if err != nil {
		return nil, err
	}
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: setstate() requires 1 argument")
	}
	tup, ok := args[1].(*objects.Tuple)
	if !ok {
		return nil, fmt.Errorf("TypeError: state vector must be a tuple")
	}
	if tup.Len() != mtN+1 {
		return nil, fmt.Errorf("ValueError: state vector is the wrong size")
	}

	var newState [mtN]uint32
	for i := 0; i < mtN; i++ {
		item, ok2 := tup.Item(i).(*objects.Int)
		if !ok2 {
			return nil, fmt.Errorf("TypeError: state element must be int")
		}
		v, fits := item.Int64()
		if !fits {
			return nil, fmt.Errorf("OverflowError: state element too large")
		}
		newState[i] = uint32(v)
	}
	idxObj, ok := tup.Item(mtN).(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: state index must be int")
	}
	idx, fits := idxObj.Int64()
	if !fits || idx < 0 || idx > mtN {
		return nil, fmt.Errorf("ValueError: invalid state")
	}

	o.src.mu.Lock()
	o.src.state = newState
	o.src.index = int(idx)
	o.src.mu.Unlock()
	return objects.None(), nil
}

// ---------------------------------------------------------------------------
// Helpers.
// ---------------------------------------------------------------------------

// selfRandom extracts the *RandomObject from the first method argument.
func selfRandom(args []objects.Object) (*RandomObject, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: descriptor requires a Random instance")
	}
	o, ok := args[0].(*RandomObject)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor requires a Random instance, got %T", args[0])
	}
	return o, nil
}

// bigIntToKey converts b (positive) to a little-endian slice of uint32
// words for use in initByArray.
//
// CPython: Modules/_randommodule.c:337 "split n into 32-bit chunks"
func bigIntToKey(b *big.Int) []uint32 {
	raw := b.Bytes() // big-endian
	// Pad to multiple of 4.
	for len(raw)%4 != 0 {
		raw = append([]byte{0}, raw...)
	}
	words := len(raw) / 4
	key := make([]uint32, words)
	for i := 0; i < words; i++ {
		// big-endian: most significant word at raw[0..3]
		off := i * 4
		key[words-1-i] = uint32(raw[off])<<24 |
			uint32(raw[off+1])<<16 |
			uint32(raw[off+2])<<8 |
			uint32(raw[off+3])
	}
	return key
}

// bytesToKey interprets p as little-endian bytes and packs them into
// uint32 words for initByArray.
//
// CPython: Modules/_randommodule.c:346 "Convert seed to byte sequence"
func bytesToKey(p []byte) []uint32 {
	// Pad to multiple of 4.
	for len(p)%4 != 0 {
		p = append(p, 0)
	}
	out := make([]uint32, len(p)/4)
	for i := range out {
		off := i * 4
		out[i] = uint32(p[off]) |
			uint32(p[off+1])<<8 |
			uint32(p[off+2])<<16 |
			uint32(p[off+3])<<24
	}
	return out
}

// reverseBytes reverses b in place.
func reverseBytes(b []byte) {
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
}
