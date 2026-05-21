package _pickle

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

// TestBuildModuleAttrs verifies the Phase 1 surface: HIGHEST_PROTOCOL,
// DEFAULT_PROTOCOL, PickleError, PicklingError, UnpicklingError.
func TestBuildModuleAttrs(t *testing.T) {
	m, err := buildModule()
	if err != nil {
		t.Fatalf("buildModule: %v", err)
	}
	d := m.Dict()

	for _, c := range []struct {
		name string
		want int64
	}{
		{"HIGHEST_PROTOCOL", highestProtocol},
		{"DEFAULT_PROTOCOL", defaultProtocol},
	} {
		v, err := d.GetItem(objects.NewStr(c.name))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		i, ok := v.(*objects.Int)
		if !ok {
			t.Fatalf("%s: expected *Int, got %T", c.name, v)
		}
		got, fits := i.Int64()
		if !fits {
			t.Fatalf("%s: value does not fit in int64", c.name)
		}
		if got != c.want {
			t.Fatalf("%s: got %d, want %d", c.name, got, c.want)
		}
	}

	for _, name := range []string{"PickleError", "PicklingError", "UnpicklingError"} {
		v, err := d.GetItem(objects.NewStr(name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		tp, ok := v.(*objects.Type)
		if !ok {
			t.Fatalf("%s: expected *Type, got %T", name, v)
		}
		if tp.Name != name {
			t.Fatalf("%s: type name %q, want %q", name, tp.Name, name)
		}
	}
}

// TestExceptionHierarchy: PicklingError and UnpicklingError both
// subclass PickleError. pickle.py relies on the single base for
// isinstance checks (`except PickleError`).
//
// CPython: Modules/_pickle.c:7720 PyModule_AddType
func TestExceptionHierarchy(t *testing.T) {
	if !objects.IsSubtype(picklingErrorType, pickleErrorType) {
		t.Fatalf("PicklingError is not a subclass of PickleError")
	}
	if !objects.IsSubtype(unpicklingErrorType, pickleErrorType) {
		t.Fatalf("UnpicklingError is not a subclass of PickleError")
	}
}

// TestOpcodeTable spot-checks that the wire-protocol opcode constants
// match the byte values CPython encodes into pickle streams. If these
// drift, every later phase that writes opcodes onto a buffer would
// produce streams CPython cannot read back.
//
// CPython: Modules/_pickle.c:63 enum opcode
func TestOpcodeTable(t *testing.T) {
	cases := []struct {
		name string
		got  byte
		want byte
	}{
		{"MARK", opMark, '('},
		{"STOP", opStop, '.'},
		{"NONE", opNone, 'N'},
		{"EMPTY_DICT", opEmptyDict, '}'},
		{"EMPTY_LIST", opEmptyList, ']'},
		{"EMPTY_TUPLE", opEmptyTuple, ')'},
		{"PROTO", opProto, 0x80},
		{"NEWOBJ", opNewobj, 0x81},
		{"FRAME", opFrame, 0x95},
		{"BYTEARRAY8", opBytearray8, 0x96},
		{"NEXT_BUFFER", opNextBuffer, 0x97},
		{"READONLY_BUFFER", opReadonlyBuffer, 0x98},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("op%s: got 0x%02x, want 0x%02x", c.name, c.got, c.want)
		}
	}
}
