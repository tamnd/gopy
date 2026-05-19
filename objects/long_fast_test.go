package objects

import (
	"math"
	"math/big"
	"math/rand"
	"testing"
)

// TestLongFastOverflowHelpers cross-checks the int64 overflow predicates
// against the big.Int reference. The fast path only fires when the
// helper returns ok==false (no overflow), so the test focuses on the
// boundary values where overflow would otherwise corrupt the result.
//
// CPython equivalent: bytecodes.c _BINARY_OP_ADD_INT et al rely on
// __builtin_add_overflow, so the contract under test is "fast path
// agrees with big.Int on every value that does not overflow."
func TestLongFastOverflowHelpers(t *testing.T) {
	cases := []int64{
		0, 1, -1, 2, -2, 10, -10, 100, -100,
		math.MaxInt32, math.MinInt32,
		math.MaxInt32 + 1, math.MinInt32 - 1,
		math.MaxInt64, math.MinInt64,
		math.MaxInt64 - 1, math.MinInt64 + 1,
		1 << 30, -(1 << 30), 1 << 62, -(1 << 62),
	}
	for _, a := range cases {
		for _, b := range cases {
			checkAdd(t, a, b)
			checkSub(t, a, b)
			checkMul(t, a, b)
		}
		checkNeg(t, a)
		checkAbs(t, a)
	}
}

func checkAdd(t *testing.T, a, b int64) {
	r, over := addOverflow(a, b)
	want := new(big.Int).Add(big.NewInt(a), big.NewInt(b))
	if !over {
		if got := big.NewInt(r); got.Cmp(want) != 0 {
			t.Errorf("addOverflow(%d, %d) = %d; big.Int says %s", a, b, r, want)
		}
		return
	}
	if want.IsInt64() {
		t.Errorf("addOverflow(%d, %d) flagged overflow but %s fits int64", a, b, want)
	}
}

func checkSub(t *testing.T, a, b int64) {
	r, over := subOverflow(a, b)
	want := new(big.Int).Sub(big.NewInt(a), big.NewInt(b))
	if !over {
		if got := big.NewInt(r); got.Cmp(want) != 0 {
			t.Errorf("subOverflow(%d, %d) = %d; big.Int says %s", a, b, r, want)
		}
		return
	}
	if want.IsInt64() {
		t.Errorf("subOverflow(%d, %d) flagged overflow but %s fits int64", a, b, want)
	}
}

func checkMul(t *testing.T, a, b int64) {
	r, over := mulOverflow(a, b)
	want := new(big.Int).Mul(big.NewInt(a), big.NewInt(b))
	if !over {
		if got := big.NewInt(r); got.Cmp(want) != 0 {
			t.Errorf("mulOverflow(%d, %d) = %d; big.Int says %s", a, b, r, want)
		}
		return
	}
	if want.IsInt64() {
		t.Errorf("mulOverflow(%d, %d) flagged overflow but %s fits int64", a, b, want)
	}
}

func checkNeg(t *testing.T, a int64) {
	r, over := negOverflow(a)
	want := new(big.Int).Neg(big.NewInt(a))
	if over {
		if want.IsInt64() {
			t.Errorf("negOverflow(%d) flagged overflow but %s fits", a, want)
		}
		return
	}
	if got := big.NewInt(r); got.Cmp(want) != 0 {
		t.Errorf("negOverflow(%d) = %d; big.Int says %s", a, r, want)
	}
}

func checkAbs(t *testing.T, a int64) {
	r, over := absOverflow(a)
	want := new(big.Int).Abs(big.NewInt(a))
	if over {
		if want.IsInt64() {
			t.Errorf("absOverflow(%d) flagged overflow but %s fits", a, want)
		}
		return
	}
	if got := big.NewInt(r); got.Cmp(want) != 0 {
		t.Errorf("absOverflow(%d) = %d; big.Int says %s", a, r, want)
	}
}

// TestLongFastSlotsAgreeWithBigInt drives the actual slot wiring
// (intAdd, intSub, intMul, intAnd, intOr, intXor, intInvert, intNeg,
// intAbs) on a randomized table and confirms the fast-path answer
// matches the big.Int slow path.
func TestLongFastSlotsAgreeWithBigInt(t *testing.T) {
	rng := rand.New(rand.NewSource(0x5172))
	for range 5000 {
		a := rng.Int63() - rng.Int63()
		b := rng.Int63() - rng.Int63()
		ai, bi := NewInt(a), NewInt(b)

		checkBinaryOp(t, "add", ai, bi, intAdd,
			new(big.Int).Add(big.NewInt(a), big.NewInt(b)))
		checkBinaryOp(t, "sub", ai, bi, intSub,
			new(big.Int).Sub(big.NewInt(a), big.NewInt(b)))
		checkBinaryOp(t, "mul", ai, bi, intMul,
			new(big.Int).Mul(big.NewInt(a), big.NewInt(b)))
		checkBinaryOp(t, "and", ai, bi, intAnd,
			new(big.Int).And(big.NewInt(a), big.NewInt(b)))
		checkBinaryOp(t, "or", ai, bi, intOr,
			new(big.Int).Or(big.NewInt(a), big.NewInt(b)))
		checkBinaryOp(t, "xor", ai, bi, intXor,
			new(big.Int).Xor(big.NewInt(a), big.NewInt(b)))

		checkUnaryOp(t, "neg", ai, intNeg, new(big.Int).Neg(big.NewInt(a)))
		checkUnaryOp(t, "abs", ai, intAbs, new(big.Int).Abs(big.NewInt(a)))
		checkUnaryOp(t, "invert", ai, intInvert, new(big.Int).Not(big.NewInt(a)))
	}
}

func checkBinaryOp(t *testing.T, name string, a, b *Int,
	op func(Object, Object) (Object, error), want *big.Int,
) {
	got, err := op(a, b)
	if err != nil {
		t.Fatalf("%s(%v, %v) errored: %v", name, a.v.String(), b.v.String(), err)
	}
	gotI, ok := got.(*Int)
	if !ok {
		t.Fatalf("%s returned %T, want *Int", name, got)
	}
	if gotI.v.Cmp(want) != 0 {
		t.Errorf("%s(%s, %s) = %s; want %s",
			name, a.v.String(), b.v.String(), gotI.v.String(), want.String())
	}
}

func checkUnaryOp(t *testing.T, name string, a *Int,
	op func(Object) (Object, error), want *big.Int,
) {
	got, err := op(a)
	if err != nil {
		t.Fatalf("%s(%s) errored: %v", name, a.v.String(), err)
	}
	gotI, ok := got.(*Int)
	if !ok {
		t.Fatalf("%s returned %T, want *Int", name, got)
	}
	if gotI.v.Cmp(want) != 0 {
		t.Errorf("%s(%s) = %s; want %s",
			name, a.v.String(), gotI.v.String(), want.String())
	}
}

// TestLongFastOverflowFallsBack confirms each binary op falls through
// to the big.Int slow path when the int64 result would overflow.
func TestLongFastOverflowFallsBack(t *testing.T) {
	// int64 max + 1 forces add fallback.
	maxI := NewInt(math.MaxInt64)
	one := NewInt(1)
	got, err := intAdd(maxI, one)
	if err != nil {
		t.Fatalf("intAdd overflow path errored: %v", err)
	}
	want := new(big.Int).Add(big.NewInt(math.MaxInt64), big.NewInt(1))
	if got.(*Int).v.Cmp(want) != 0 {
		t.Errorf("intAdd(MaxInt64, 1) = %s; want %s", got.(*Int).v.String(), want.String())
	}

	// int64 min - 1 forces sub fallback.
	minI := NewInt(math.MinInt64)
	got2, err := intSub(minI, one)
	if err != nil {
		t.Fatalf("intSub overflow path errored: %v", err)
	}
	want2 := new(big.Int).Sub(big.NewInt(math.MinInt64), big.NewInt(1))
	if got2.(*Int).v.Cmp(want2) != 0 {
		t.Errorf("intSub(MinInt64, 1) = %s; want %s", got2.(*Int).v.String(), want2.String())
	}

	// (1 << 40) * (1 << 40) forces mul fallback.
	big1 := NewInt(1 << 40)
	got3, err := intMul(big1, big1)
	if err != nil {
		t.Fatalf("intMul overflow path errored: %v", err)
	}
	want3 := new(big.Int).Mul(big.NewInt(1<<40), big.NewInt(1<<40))
	if got3.(*Int).v.Cmp(want3) != 0 {
		t.Errorf("intMul((1<<40)^2) = %s; want %s", got3.(*Int).v.String(), want3.String())
	}

	// -MinInt64 forces neg / abs fallback.
	got4, err := intNeg(minI)
	if err != nil {
		t.Fatalf("intNeg overflow path errored: %v", err)
	}
	want4 := new(big.Int).Neg(big.NewInt(math.MinInt64))
	if got4.(*Int).v.Cmp(want4) != 0 {
		t.Errorf("intNeg(MinInt64) = %s; want %s", got4.(*Int).v.String(), want4.String())
	}
	got5, err := intAbs(minI)
	if err != nil {
		t.Fatalf("intAbs overflow path errored: %v", err)
	}
	if got5.(*Int).v.Cmp(want4) != 0 {
		t.Errorf("intAbs(MinInt64) = %s; want %s", got5.(*Int).v.String(), want4.String())
	}
}

// BenchmarkLongAddSmall measures the alloc count on the small-int hot
// path. The fast path keeps the result inside the [-5, 256] cache, so
// every iteration should be zero allocs.
func BenchmarkLongAddSmall(b *testing.B) {
	a := NewInt(1)
	one := NewInt(1)
	b.ReportAllocs()
	for b.Loop() {
		_, _ = intAdd(a, one)
	}
}

func BenchmarkLongMulSmall(b *testing.B) {
	a := NewInt(7)
	two := NewInt(2)
	b.ReportAllocs()
	for b.Loop() {
		_, _ = intMul(a, two)
	}
}

func BenchmarkLongAddLarge(b *testing.B) {
	a := NewIntFromBig(new(big.Int).Lsh(big.NewInt(1), 200))
	one := NewInt(1)
	b.ReportAllocs()
	for b.Loop() {
		_, _ = intAdd(a, one)
	}
}
