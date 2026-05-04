package hash_test

import (
	"math"
	"testing"
	"unsafe"

	"github.com/tamnd/gopy/hash"
)

func unsafePtr(p any) unsafe.Pointer {
	if v, ok := p.(*int); ok {
		return unsafe.Pointer(v)
	}
	return nil
}

func zeroSecret(t *testing.T) {
	t.Helper()
	for i := range hash.Secret {
		hash.Secret[i] = 0
	}
}

func TestBufferEmpty(t *testing.T) {
	zeroSecret(t)
	if got := hash.Buffer(nil); got != 0 {
		t.Fatalf("Buffer(nil) = %d, want 0", got)
	}
	if got := hash.Buffer([]byte{}); got != 0 {
		t.Fatalf("Buffer([]) = %d, want 0", got)
	}
}

func TestBufferReferenceVectors(t *testing.T) {
	zeroSecret(t)
	cases := []struct {
		in   []byte
		want int64
	}{
		{[]byte("hello"), -2096571579003691106},
		{[]byte("a"), 4644417185603328019},
		{[]byte("01234567"), -2720791140458926906},
		{[]byte("012345678"), -5215866000161560749},
		{[]byte("the quick brown fox jumps over the lazy dog"), -9176553165463234003},
	}
	for _, c := range cases {
		if got := hash.Buffer(c.in); got != c.want {
			t.Errorf("Buffer(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestDoubleReferenceVectors(t *testing.T) {
	cases := []struct {
		in   float64
		want int64
	}{
		{1.5, 1152921504606846977},
		{-1.5, -1152921504606846977},
		{1e100, 1822893315824342674},
		{3.141592653589793, 326490430436040707},
	}
	for _, c := range cases {
		if got := hash.Double(c.in, 0); got != c.want {
			t.Errorf("Double(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestDoubleSpecial(t *testing.T) {
	if got := hash.Double(math.Inf(1), 0); got != hash.HashInf {
		t.Errorf("Double(+inf) = %d, want %d", got, hash.HashInf)
	}
	if got := hash.Double(math.Inf(-1), 0); got != -hash.HashInf {
		t.Errorf("Double(-inf) = %d, want %d", got, -hash.HashInf)
	}
	if got := hash.Double(0, 0); got != 0 {
		t.Errorf("Double(0) = %d, want 0", got)
	}
}

func TestKeyedHashDeterministic(t *testing.T) {
	a := hash.KeyedHash(42, []byte("payload"))
	b := hash.KeyedHash(42, []byte("payload"))
	if a != b {
		t.Fatalf("KeyedHash not deterministic: %d vs %d", a, b)
	}
	c := hash.KeyedHash(43, []byte("payload"))
	if a == c {
		t.Fatalf("KeyedHash collapsed across keys: %d", a)
	}
}

func TestGetFuncDef(t *testing.T) {
	d := hash.GetFuncDef()
	if d.Name != "siphash13" || d.HashBits != 64 || d.SeedBits != 128 {
		t.Errorf("FuncDef = %+v", d)
	}
}

func TestPointerSentinel(t *testing.T) {
	// Different pointer values should rarely collide on the -1 sentinel,
	// but the API must never return -1.
	var x, y int
	if h := hash.Pointer(unsafePtr(&x)); h == -1 {
		t.Fatal("Pointer returned -1 sentinel")
	}
	if hash.Pointer(unsafePtr(&x)) == hash.Pointer(unsafePtr(&y)) {
		t.Fatal("Pointer collided two distinct addresses")
	}
}
