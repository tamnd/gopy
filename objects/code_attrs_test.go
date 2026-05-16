// Coverage for the co_* attribute surface added in code_attrs.go.
// dis.py reads these on every code object it inspects; the test pins
// type and value for the slice-backed fields plus the method trio.
//
// CPython: Objects/codeobject.c:2724 code_memberlist
// CPython: Objects/codeobject.c:2967 code_methods

package objects

import (
	"testing"
)

func TestCodeAttrLookupReadOnly(t *testing.T) {
	c := NewCode()
	c.Code = []byte{0x52, 0x00, 0x23, 0x00}
	c.Consts = []any{nil, int64(42), "hi"}
	c.Names = []string{"alpha", "beta"}
	c.Varnames = []string{"x", "y"}
	c.Freevars = []string{"f"}
	c.Cellvars = []string{"c"}
	c.Linetable = []byte{0x80, 0x57}
	c.ExceptionTable = []byte{0x82, 0x40}

	cases := []struct {
		name      string
		wantType  string
		wantLen   int
	}{
		{"co_code", "bytes", 4},
		{"_co_code_adaptive", "bytes", 4},
		{"co_consts", "tuple", 3},
		{"co_names", "tuple", 2},
		{"co_varnames", "tuple", 2},
		{"co_freevars", "tuple", 1},
		{"co_cellvars", "tuple", 1},
		{"co_linetable", "bytes", 2},
		{"co_exceptiontable", "bytes", 2},
	}
	for _, tc := range cases {
		o, err := codeGetAttr(c, NewStr(tc.name))
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if o.Type().Name != tc.wantType {
			t.Errorf("%s: type = %s, want %s", tc.name, o.Type().Name, tc.wantType)
		}
		switch v := o.(type) {
		case *Bytes:
			if len(v.v) != tc.wantLen {
				t.Errorf("%s: len = %d, want %d", tc.name, len(v.v), tc.wantLen)
			}
		case *Tuple:
			if len(v.items) != tc.wantLen {
				t.Errorf("%s: len = %d, want %d", tc.name, len(v.items), tc.wantLen)
			}
		}
	}

	o, err := codeGetAttr(c, NewStr("co_nlocals"))
	if err != nil {
		t.Fatalf("co_nlocals: %v", err)
	}
	if i, ok := o.(*Int); !ok {
		t.Errorf("co_nlocals: type = %T, want *Int", o)
	} else if v, _ := i.Int64(); v != 2 {
		t.Errorf("co_nlocals: got %d, want 2", v)
	}
}

func TestCodeVarnameFromOparg(t *testing.T) {
	c := NewCode()
	c.Varnames = []string{"x", "y"}
	c.Cellvars = []string{"c"}
	c.Freevars = []string{"f"}

	check := func(arg int64, want string) {
		t.Helper()
		o, err := codeVarnameFromOpargMethod([]Object{c, NewInt(arg)}, nil)
		if err != nil {
			t.Fatalf("oparg=%d: %v", arg, err)
		}
		got, _ := o.(*Unicode)
		if got == nil || got.v != want {
			t.Errorf("oparg=%d: got %v, want %q", arg, o, want)
		}
	}
	check(0, "x")
	check(1, "y")
	check(2, "c")
	check(3, "f")

	if _, err := codeVarnameFromOpargMethod([]Object{c, NewInt(99)}, nil); err == nil {
		t.Errorf("oparg=99: expected IndexError")
	}
}
