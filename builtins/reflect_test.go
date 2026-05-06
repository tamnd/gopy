package builtins

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

// TestTypeOfReturnsType pins type(int_value) == int_type.
func TestTypeOfReturnsType(t *testing.T) {
	v, err := TypeOf([]objects.Object{objects.NewInt(7)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if v != objects.IntType {
		t.Errorf("type(7) = %v, want IntType", v)
	}
}

// TestIsInstanceMRO pins the MRO walk: a Bool is an instance of int
// because bool's MRO includes int.
func TestIsInstanceMRO(t *testing.T) {
	v, err := IsInstance([]objects.Object{objects.True(), objects.IntType}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if v != objects.True() {
		t.Errorf("isinstance(True, int) = %v, want True", v)
	}
}

// TestIsInstanceTuple pins the tuple-of-types form.
func TestIsInstanceTuple(t *testing.T) {
	tup := objects.NewTuple([]objects.Object{objects.StrType(), objects.IntType})
	v, err := IsInstance([]objects.Object{objects.NewInt(5), tup}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if v != objects.True() {
		t.Errorf("isinstance(5, (str, int)) = %v, want True", v)
	}
}

// TestIsSubclassPositive pins issubclass(bool, int).
func TestIsSubclassPositive(t *testing.T) {
	v, err := IsSubclass([]objects.Object{objects.BoolType, objects.IntType}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if v != objects.True() {
		t.Errorf("issubclass(bool, int) = %v, want True", v)
	}
}

// TestCallableSplits pins callable() True for builtin functions and
// False for plain ints.
func TestCallableSplits(t *testing.T) {
	bf := objects.NewBuiltinFunction("noop", func([]objects.Object, map[string]objects.Object) (objects.Object, error) {
		return objects.None(), nil
	})
	v, _ := Callable([]objects.Object{bf}, nil)
	if v != objects.True() {
		t.Errorf("callable(builtin) = %v, want True", v)
	}
	v, _ = Callable([]objects.Object{objects.NewInt(1)}, nil)
	if v != objects.False() {
		t.Errorf("callable(1) = %v, want False", v)
	}
}

// TestIdSameForIdenticalObject pins id() returns the same value for
// the same Python object across calls (identity invariant).
func TestIdSameForIdenticalObject(t *testing.T) {
	x := objects.NewStr("hello")
	a, _ := ID([]objects.Object{x}, nil)
	b, _ := ID([]objects.Object{x}, nil)
	av, _ := a.(*objects.Int).Int64()
	bv, _ := b.(*objects.Int).Int64()
	if av != bv {
		t.Errorf("id(x) returned different values: %d vs %d", av, bv)
	}
}

// TestHashOnHashable pins hash returns an Int for str.
func TestHashOnHashable(t *testing.T) {
	v, err := Hash([]objects.Object{objects.NewStr("k")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := v.(*objects.Int); !ok {
		t.Errorf("hash returned %T, want *Int", v)
	}
}

// TestReprEchoesCanonical pins repr produces the str-form CPython
// would emit: '7' for int, "'hi'" for str.
func TestReprEchoesCanonical(t *testing.T) {
	cases := []struct {
		in   objects.Object
		want string
	}{
		{objects.NewInt(7), "7"},
		{objects.NewStr("hi"), "'hi'"},
	}
	for _, tc := range cases {
		v, err := Repr([]objects.Object{tc.in}, nil)
		if err != nil {
			t.Fatal(err)
		}
		got, _ := objects.Str(v)
		if got != tc.want {
			t.Errorf("repr(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestStrFromObject pins str(obj) calls the Str slot.
func TestStrFromObject(t *testing.T) {
	v, err := StrOf([]objects.Object{objects.NewInt(42)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := objects.Str(v)
	if got != "42" {
		t.Errorf("str(42) = %q, want \"42\"", got)
	}
}

// TestStrEmpty pins str() with no arguments returns "".
func TestStrEmpty(t *testing.T) {
	v, err := StrOf(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := objects.Str(v)
	if got != "" {
		t.Errorf("str() = %q, want \"\"", got)
	}
}
