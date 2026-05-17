// Round-trip parity tests. These are the safety net for spec 1713 phase 1
// and every later phase that touches the marshal layer. A fixture
// goes Dump -> Load and every observable field on the recovered Code
// must equal the input. Fixture coverage grows phase by phase; this
// file is the home for "if marshal regresses, this gate is what
// catches it".
//
// CPython: Python/marshal.c r_object / w_object
package marshal

import (
	"bytes"
	"math/big"
	"reflect"
	"testing"

	"github.com/tamnd/gopy/objects"
)

// codeParityFixture exercises every public field on objects.Code that
// marshal touches today, with at least one non-zero / non-empty value
// per field. The intent is "if a future marshal port drops a field on
// the floor, this fixture trips first".
func codeParityFixture() *objects.Code {
	c := objects.NewCode()
	c.Argcount = 3
	c.PosonlyArgcount = 1
	c.KwonlyArgcount = 1
	c.Stacksize = 5
	c.Flags = 0x43 // CO_OPTIMIZED | CO_NEWLOCALS | CO_NOFREE
	// LOAD_CONST 0, LOAD_CONST 1, BINARY_OP +, RETURN_VALUE.
	// None of these are adaptive, so marshal's Quicken pass leaves them be.
	c.Code = []byte{
		0x52, 0x00, // LOAD_CONST 0
		0x52, 0x01, // LOAD_CONST 1
		0x4d, 0x00, // BINARY_OP 0
		0x23, 0x00, // RETURN_VALUE
	}
	c.Consts = []any{
		nil,
		true,
		false,
		int64(42),
		int64(-7),
		3.14,
		"hello",
		"hello", // duplicate to exercise TYPE_REF reuse
		big.NewInt(1).Lsh(big.NewInt(1), 128),
	}
	c.Names = []string{"alpha", "beta", "alpha"} // duplicate for TYPE_REF
	c.Varnames = []string{"x", "y", "z", "kwonly"}
	c.Freevars = []string{"f1"}
	c.Cellvars = []string{"c1", "c2"}
	c.Filename = "fixture.py"
	c.Name = "fixture"
	c.Qualname = "module.fixture"
	c.Firstlineno = 17
	c.Linetable = []byte{0x80, 0x04, 0xd8, 0x00, 0x05}
	c.ExceptionTable = []byte{0x82, 0x40, 0x02, 0x00}
	return c
}

// TestCodeParityRoundTrip pins that every field round-trips through
// Dump / Load byte-for-byte. The fixture covers ints (short and long),
// floats, strings, duplicates, bools, None, and the line / exception
// table blobs.
//
// CPython: Python/marshal.c w_object / r_object
func TestCodeParityRoundTrip(t *testing.T) {
	want := codeParityFixture()
	var buf bytes.Buffer
	if err := Dump(&buf, want); err != nil {
		t.Fatalf("Dump: %v", err)
	}
	v, err := Load(&buf)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got, ok := v.(*objects.Code)
	if !ok {
		t.Fatalf("Load returned %T, want *objects.Code", v)
	}

	cases := []struct {
		field     string
		want, got any
	}{
		{"Argcount", want.Argcount, got.Argcount},
		{"PosonlyArgcount", want.PosonlyArgcount, got.PosonlyArgcount},
		{"KwonlyArgcount", want.KwonlyArgcount, got.KwonlyArgcount},
		{"Stacksize", want.Stacksize, got.Stacksize},
		{"Flags", want.Flags, got.Flags},
		{"Code", want.Code, got.Code},
		{"Names", want.Names, got.Names},
		{"Varnames", want.Varnames, got.Varnames},
		{"Freevars", want.Freevars, got.Freevars},
		{"Cellvars", want.Cellvars, got.Cellvars},
		{"Filename", want.Filename, got.Filename},
		{"Name", want.Name, got.Name},
		{"Qualname", want.Qualname, got.Qualname},
		{"Firstlineno", want.Firstlineno, got.Firstlineno},
		{"Linetable", want.Linetable, got.Linetable},
		{"ExceptionTable", want.ExceptionTable, got.ExceptionTable},
	}
	for _, c := range cases {
		if !reflect.DeepEqual(c.want, c.got) {
			t.Errorf("%s: got %#v, want %#v", c.field, c.got, c.want)
		}
	}

	if len(got.Consts) != len(want.Consts) {
		t.Fatalf("len(Consts) = %d, want %d", len(got.Consts), len(want.Consts))
	}
	for i := range want.Consts {
		if !constsEqual(want.Consts[i], got.Consts[i]) {
			t.Errorf("Consts[%d]: got %#v, want %#v", i, got.Consts[i], want.Consts[i])
		}
	}
}

// constsEqual handles the *big.Int identity gap: reflect.DeepEqual on
// big.Int compares pointers when one side allocates a fresh value, so
// fall back to (*big.Int).Cmp for that type.
func constsEqual(want, got any) bool {
	if wb, ok := want.(*big.Int); ok {
		gb, ok := got.(*big.Int)
		return ok && wb.Cmp(gb) == 0
	}
	return reflect.DeepEqual(want, got)
}
