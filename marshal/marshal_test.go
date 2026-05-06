package marshal

import (
	"bytes"
	"math/big"
	"reflect"
	"testing"

	"github.com/tamnd/gopy/objects"
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
	err := Dump(&buf, make(chan int))
	if err == nil {
		t.Fatal("expected error for chan value")
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

// TestTypeCodeRoundtrip pins *objects.Code encode/decode through the
// TYPE_CODE wire format, including nested code objects in consts.
func TestTypeCodeRoundtrip(t *testing.T) {
	c := objects.NewCode()
	c.Argcount = 2
	c.PosonlyArgcount = 1
	c.KwonlyArgcount = 0
	c.Stacksize = 4
	c.Flags = 0x43
	c.Code = []byte{0x64, 0x00, 0x53, 0x00}
	c.Consts = []any{nil, int64(42), "hello"}
	c.Names = []string{"x", "y"}
	c.Varnames = []string{"a", "b"}
	c.Cellvars = []string{"cell"}
	c.Freevars = []string{"free"}
	c.Filename = "test.py"
	c.Name = "testfunc"
	c.Qualname = "module.testfunc"
	c.Firstlineno = 10
	c.Linetable = []byte{0x80, 0x04}
	c.ExceptionTable = []byte{}

	var buf bytes.Buffer
	if err := Dump(&buf, c); err != nil {
		t.Fatalf("Dump code: %v", err)
	}
	got, err := Load(&buf)
	if err != nil {
		t.Fatalf("Load code: %v", err)
	}
	rc, ok := got.(*objects.Code)
	if !ok {
		t.Fatalf("Load returned %T, want *objects.Code", got)
	}
	if rc.Argcount != c.Argcount {
		t.Errorf("Argcount = %d, want %d", rc.Argcount, c.Argcount)
	}
	if rc.Flags != c.Flags {
		t.Errorf("Flags = %d, want %d", rc.Flags, c.Flags)
	}
	if !bytes.Equal(rc.Code, c.Code) {
		t.Errorf("Code = %v, want %v", rc.Code, c.Code)
	}
	if rc.Name != c.Name {
		t.Errorf("Name = %q, want %q", rc.Name, c.Name)
	}
	if rc.Firstlineno != c.Firstlineno {
		t.Errorf("Firstlineno = %d, want %d", rc.Firstlineno, c.Firstlineno)
	}
	if !reflect.DeepEqual(rc.Varnames, c.Varnames) {
		t.Errorf("Varnames = %v, want %v", rc.Varnames, c.Varnames)
	}
	if !reflect.DeepEqual(rc.Cellvars, c.Cellvars) {
		t.Errorf("Cellvars = %v, want %v", rc.Cellvars, c.Cellvars)
	}
	if !reflect.DeepEqual(rc.Freevars, c.Freevars) {
		t.Errorf("Freevars = %v, want %v", rc.Freevars, c.Freevars)
	}
}

// TestTypeCodeNestedConst pins that a code object nested inside
// consts is decoded recursively as *objects.Code.
func TestTypeCodeNestedConst(t *testing.T) {
	inner := objects.NewCode()
	inner.Name = "inner"
	inner.Code = []byte{0x64, 0x00, 0x53, 0x00}
	inner.Consts = []any{nil}

	outer := objects.NewCode()
	outer.Name = "outer"
	outer.Code = []byte{0x64, 0x00, 0x53, 0x00}
	outer.Consts = []any{inner}

	var buf bytes.Buffer
	if err := Dump(&buf, outer); err != nil {
		t.Fatalf("Dump: %v", err)
	}
	got, err := Load(&buf)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	rc := got.(*objects.Code)
	if rc.Name != "outer" {
		t.Errorf("outer.Name = %q, want outer", rc.Name)
	}
	ri, ok := rc.Consts[0].(*objects.Code)
	if !ok {
		t.Fatalf("consts[0] = %T, want *objects.Code", rc.Consts[0])
	}
	if ri.Name != "inner" {
		t.Errorf("inner.Name = %q, want inner", ri.Name)
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

// TestTypeSetRoundtrip pins that a *objects.Set round-trips through
// TYPE_SET and comes back as a *objects.Set with the same elements.
func TestTypeSetRoundtrip(t *testing.T) {
	s := objects.NewSet()
	for _, v := range []any{int64(1), int64(2), int64(3)} {
		obj, _ := toObject(v)
		if err := s.Add(obj); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
	var buf bytes.Buffer
	if err := Dump(&buf, s); err != nil {
		t.Fatalf("Dump: %v", err)
	}
	got, err := Load(&buf)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	rs, ok := got.(*objects.Set)
	if !ok {
		t.Fatalf("Load returned %T, want *objects.Set", got)
	}
	if rs.Len() != s.Len() {
		t.Errorf("Len = %d, want %d", rs.Len(), s.Len())
	}
	for _, item := range s.Items() {
		ok, err := rs.Contains(item)
		if err != nil {
			t.Fatalf("Contains: %v", err)
		}
		if !ok {
			t.Errorf("missing element %v", item)
		}
	}
}

// TestTypeFrozensetRoundtrip pins that a frozenset round-trips through
// TYPE_FROZENSET and comes back as a frozen *objects.Set.
func TestTypeFrozensetRoundtrip(t *testing.T) {
	items := []objects.Object{
		objects.NewStr("a"),
		objects.NewStr("b"),
	}
	fs, err := objects.NewFrozenset(items)
	if err != nil {
		t.Fatalf("NewFrozenset: %v", err)
	}
	var buf bytes.Buffer
	if err := Dump(&buf, fs); err != nil {
		t.Fatalf("Dump: %v", err)
	}
	got, err := Load(&buf)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	rfs, ok := got.(*objects.Set)
	if !ok {
		t.Fatalf("Load returned %T, want *objects.Set", got)
	}
	if rfs.Len() != fs.Len() {
		t.Errorf("Len = %d, want %d", rfs.Len(), fs.Len())
	}
	if rfs.Type() != objects.FrozensetType {
		t.Errorf("Type = %v, want FrozensetType", rfs.Type())
	}
}

// TestTypeDictRoundtrip pins that a map[any]any round-trips through
// TYPE_DICT and comes back with matching key/value pairs.
func TestTypeDictRoundtrip(t *testing.T) {
	m := map[any]any{
		"key1": int64(42),
		"key2": true,
		"key3": nil,
	}
	var buf bytes.Buffer
	if err := Dump(&buf, m); err != nil {
		t.Fatalf("Dump: %v", err)
	}
	got, err := Load(&buf)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	rm, ok := got.(map[any]any)
	if !ok {
		t.Fatalf("Load returned %T, want map[any]any", got)
	}
	if len(rm) != len(m) {
		t.Errorf("len = %d, want %d", len(rm), len(m))
	}
	for k, want := range m {
		got, exists := rm[k]
		if !exists {
			t.Errorf("missing key %v", k)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("key %v: got %#v, want %#v", k, got, want)
		}
	}
}

// TestTypeComplexRoundtrip pins that complex128 round-trips through
// TYPE_BINARY_COMPLEX with correct real and imaginary parts.
func TestTypeComplexRoundtrip(t *testing.T) {
	cases := []complex128{
		complex(0, 0),
		complex(1.5, -2.5),
		complex(3.14159, 2.71828),
	}
	for _, c := range cases {
		var buf bytes.Buffer
		if err := Dump(&buf, c); err != nil {
			t.Fatalf("Dump(%v): %v", c, err)
		}
		got, err := Load(&buf)
		if err != nil {
			t.Fatalf("Load(%v): %v", c, err)
		}
		rc, ok := got.(complex128)
		if !ok {
			t.Fatalf("Load returned %T, want complex128", got)
		}
		if rc != c {
			t.Errorf("got %v, want %v", rc, c)
		}
	}
}
