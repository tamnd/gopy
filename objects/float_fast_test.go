package objects

import (
	"math"
	"testing"
)

// TestFloatSingletonsAreShared verifies the cache returns the same
// pointer for repeated calls to NewFloat on the well-known constants.
// The hot path in raytrace / spectralnorm etc. bounces on these values
// every iteration; sharing the singleton turns each NewFloat from "1
// alloc" into "0 allocs".
func TestFloatSingletonsAreShared(t *testing.T) {
	cases := []float64{
		0,
		math.Copysign(0, -1),
		1,
		-1,
		math.Inf(1),
		math.Inf(-1),
		math.NaN(),
	}
	for _, v := range cases {
		a := NewFloat(v)
		b := NewFloat(v)
		if a != b {
			// NaN bit-pattern equality: the math.NaN() singleton matches
			// itself; a different NaN payload (e.g. constructed via
			// math.Float64frombits with a non-canonical mantissa) should
			// fall through to the alloc path.
			t.Errorf("NewFloat(%v) returned distinct pointers (%p vs %p), cache miss",
				v, a, b)
		}
	}
}

// TestFloatNonCachedAllocates makes sure the cache does NOT collide
// for values it should not match. Two NewFloat(2.5) calls must be
// distinct *Float pointers because 2.5 is not a singleton.
func TestFloatNonCachedAllocates(t *testing.T) {
	a := NewFloat(2.5)
	b := NewFloat(2.5)
	if a == b {
		t.Errorf("NewFloat(2.5) shared a pointer (%p == %p); cache fired on a non-singleton value", a, b)
	}
}

// TestFloatNonCanonicalNaNFallsThrough confirms a NaN with a different
// bit pattern than math.NaN() does not hit the singleton. Important
// because struct decoders and bit-twiddling code can produce arbitrary
// NaN payloads, and silently collapsing them would erase information.
func TestFloatNonCanonicalNaNFallsThrough(t *testing.T) {
	nonCanon := math.Float64frombits(0x7ff0000000000123)
	if !math.IsNaN(nonCanon) {
		t.Fatalf("test setup: 0x7ff0000000000123 is not a NaN bit pattern")
	}
	a := NewFloat(nonCanon)
	b := NewFloat(nonCanon)
	if a == b {
		t.Errorf("non-canonical NaN collapsed to singleton")
	}
	if a == floatNaN {
		t.Errorf("non-canonical NaN hit canonical NaN singleton")
	}
}

// BenchmarkFloatNewZero measures the singleton cache hit on the most
// common float literal (0.0). Should be 0 allocs / very few ns.
func BenchmarkFloatNewZero(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = NewFloat(0)
	}
}

// BenchmarkFloatNewOne measures the singleton cache hit on 1.0, the
// other ubiquitous literal (loop step, increment, multiplicative
// identity).
func BenchmarkFloatNewOne(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = NewFloat(1)
	}
}

// BenchmarkFloatNewArbitrary measures the alloc path. Should be 1
// alloc / op (Float struct), confirming the cache miss falls through
// to newFloatRaw without extra overhead.
func BenchmarkFloatNewArbitrary(b *testing.B) {
	b.ReportAllocs()
	var x float64 = 2.5
	for b.Loop() {
		_ = NewFloat(x)
		x += 0.1
	}
}
