package compile

import (
	"bytes"
	"testing"

	"github.com/tamnd/gopy/ast"
)

// TestAssembleEmitsCodeUnits verifies the basic packing: each
// instruction lands as one (opcode, oparg) pair.
func TestAssembleEmitsCodeUnits(t *testing.T) {
	seq := &Sequence{}
	seq.Addop(LOAD_CONST, 0, ast.Pos{})
	seq.Addop(RETURN_VALUE, 0, ast.Pos{})
	unit := &Unit{Name: "<m>", FirstLineno: 1, FastHidden: map[string]bool{}}
	co, err := Assemble(seq, &Info{MaxStackDepth: 1}, unit, "<test>")
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	want := []byte{byte(LOAD_CONST), 0, byte(RETURN_VALUE), 0}
	if !bytes.Equal(co.Code, want) {
		t.Errorf("co.Code = %v, want %v", co.Code, want)
	}
	if co.Stacksize != 1 {
		t.Errorf("co.Stacksize = %d, want 1", co.Stacksize)
	}
}

// TestAssembleExtendedArg widens opargs > 255 with EXTENDED_ARG.
func TestAssembleExtendedArg(t *testing.T) {
	seq := &Sequence{}
	seq.Addop(LOAD_CONST, 0x1234, ast.Pos{})
	unit := &Unit{Name: "<m>", FirstLineno: 1, FastHidden: map[string]bool{}}
	co, err := Assemble(seq, nil, unit, "<test>")
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	want := []byte{byte(EXTENDED_ARG), 0x12, byte(LOAD_CONST), 0x34}
	if !bytes.Equal(co.Code, want) {
		t.Errorf("co.Code = %v, want %v", co.Code, want)
	}
}

// TestVarint verifies the 6-bit varint encoding.
func TestVarint(t *testing.T) {
	cases := []struct {
		v    uint32
		want []byte
	}{
		{0, []byte{0x00}},
		{1, []byte{0x01}},
		{0x3f, []byte{0x3f}},
		{0x40, []byte{0x40, 0x01}},
		{0xff, []byte{0x7f, 0x03}},
	}
	for _, tc := range cases {
		got := writeVarint(nil, tc.v)
		if !bytes.Equal(got, tc.want) {
			t.Errorf("writeVarint(%d) = %v, want %v", tc.v, got, tc.want)
		}
	}
}

// TestBuildLocalsPlus exercises the flat layout: args+locals first,
// then cells, then frees.
func TestBuildLocalsPlus(t *testing.T) {
	unit := &Unit{
		VarNames:   []string{"a", "b"},
		CellVars:   []string{"c"},
		FreeVars:   []string{"f"},
		FastHidden: map[string]bool{"b": true},
	}
	names, kinds := buildLocalsPlus(unit)
	wantNames := []string{"a", "b", "c", "f"}
	wantKinds := []uint8{FastLocal, FastLocal | FastHidden, FastCell, FastFree}
	if len(names) != len(wantNames) {
		t.Fatalf("names len = %d, want %d", len(names), len(wantNames))
	}
	for i := range names {
		if names[i] != wantNames[i] || kinds[i] != wantKinds[i] {
			t.Errorf("[%d] = (%s, %#x), want (%s, %#x)", i, names[i], kinds[i], wantNames[i], wantKinds[i])
		}
	}
}
