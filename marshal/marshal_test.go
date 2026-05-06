package marshal

import (
	"bytes"
	"math/big"
	"reflect"
	"testing"
)

// TestRoundtrip walks each value supported by the v0.5 skeleton
// through Dump then Load and asserts the result equals the input.
func TestRoundtrip(t *testing.T) {
	cases := []struct {
		name string
		in   any
		out  any // expected after Load (may differ by widening)
	}{
		{"None", nil, nil},
		{"True", true, true},
		{"False", false, false},
		{"int small", int64(42), int64(42)},
		{"int neg", int64(-1), int64(-1)},
		{"int max32", int64(0x7FFFFFFF), int64(0x7FFFFFFF)},
		{"int wider", int64(0x100000000), int64(0x100000000)},
		{"float", 3.14, 3.14},
		{"ascii short", "hello", "hello"},
		{"unicode long", string(make([]byte, 300)), string(make([]byte, 300))},
		{"bytes", []byte{0x00, 0xff, 0x7f}, []byte{0x00, 0xff, 0x7f}},
		{"tuple small", []any{int64(1), "two", nil}, []any{int64(1), "two", nil}},
		{"tuple nested", []any{[]any{int64(1)}, true}, []any{[]any{int64(1)}, true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := Dump(&buf, tc.in); err != nil {
				t.Fatalf("Dump: %v", err)
			}
			got, err := Load(&buf)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if !reflect.DeepEqual(got, tc.out) {
				t.Errorf("got %#v, want %#v", got, tc.out)
			}
		})
	}
}

// TestUnmarshallable: a value whose type is outside the accepted subset
// surfaces ErrUnmarshallable rather than panicking.
func TestUnmarshallable(t *testing.T) {
	var buf bytes.Buffer
	err := Dump(&buf, complex(1, 2))
	if err == nil {
		t.Fatal("expected error for complex value")
	}
}

// TestVersionConstant pins the wire-format version against
// CPython 3.14's Py_MARSHAL_VERSION.
func TestVersionConstant(t *testing.T) {
	if Version != 5 {
		t.Errorf("Version = %d, want 5", Version)
	}
}

// TestTypeLongRoundtrip pins *big.Int encode/decode for values that
// don't fit in int64.
func TestTypeLongRoundtrip(t *testing.T) {
	cases := []*big.Int{
		big.NewInt(0),
		big.NewInt(1),
		big.NewInt(-1),
		big.NewInt(0x7FFFFFFFFFFFFFFF),
		new(big.Int).Lsh(big.NewInt(1), 100), // 2^100
		new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 100)),
	}
	for _, v := range cases {
		var buf bytes.Buffer
		if err := Dump(&buf, v); err != nil {
			t.Fatalf("Dump(%v): %v", v, err)
		}
		got, err := Load(&buf)
		if err != nil {
			t.Fatalf("Load(%v): %v", v, err)
		}
		bi, ok := got.(*big.Int)
		if !ok {
			t.Fatalf("Load returned %T, want *big.Int", got)
		}
		if bi.Cmp(v) != 0 {
			t.Errorf("got %v, want %v", bi, v)
		}
	}
}

// TestFlagRefRoundtrip pins that a decoder with FLAG_REF objects can
// decode TYPE_REF back-references. We hand-craft a byte stream that
// encodes "hello" with FLAG_REF, then references it.
func TestFlagRefRoundtrip(t *testing.T) {
	// Hand-craft: [typeShortASCII|flagRef, 5, "hello", typeRef, 0,0,0,0]
	var buf bytes.Buffer
	buf.WriteByte(typeShortASCII | flagRef)
	buf.WriteByte(5)
	buf.WriteString("hello")
	// TYPE_REF pointing to index 0
	buf.WriteByte(typeRef)
	buf.Write([]byte{0, 0, 0, 0})

	dec := decoder{r: byteReaderOf(&buf)}
	first, err := dec.read()
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	if first.(string) != "hello" {
		t.Errorf("first = %q, want hello", first)
	}
	second, err := dec.read()
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	if second.(string) != "hello" {
		t.Errorf("second = %q, want hello", second)
	}
}

// TestInternedStringDecodes pins that TYPE_SHORT_ASCII_INTERNED
// decodes to the same string value as TYPE_SHORT_ASCII.
func TestInternedStringDecodes(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteByte(typeShortASCIIInterned)
	buf.WriteByte(3)
	buf.WriteString("abc")

	got, err := Load(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if got.(string) != "abc" {
		t.Errorf("got %q, want abc", got)
	}
}

// TestTypeLongLimbArithmetic pins the limb encode/decode for a value
// that exercises multiple 15-bit limbs.
func TestTypeLongLimbArithmetic(t *testing.T) {
	// 2^30 = 1073741824; needs at least two 15-bit limbs.
	v := new(big.Int).Lsh(big.NewInt(1), 30)
	var buf bytes.Buffer
	if err := Dump(&buf, v); err != nil {
		t.Fatal(err)
	}
	got, err := Load(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if got.(*big.Int).Cmp(v) != 0 {
		t.Errorf("got %v, want %v", got, v)
	}
}
