package builtins

import (
	"strings"
	"testing"

	"github.com/tamnd/gopy/objects"
)

// TestAbsInt pins abs(-3) == 3.
func TestAbsInt(t *testing.T) {
	v, err := Abs([]objects.Object{objects.NewInt(-3)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 3 {
		t.Errorf("abs(-3) = %d, want 3", got)
	}
}

// TestAbsFloat pins the float branch routes through nb_absolute.
func TestAbsFloat(t *testing.T) {
	v, err := Abs([]objects.Object{objects.NewFloat(-2.5)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := v.(*objects.Float).Float64(); got != 2.5 {
		t.Errorf("abs(-2.5) = %v, want 2.5", got)
	}
}

// TestDivmodInts pins (q, r) on Python floor / sign-of-divisor semantics.
func TestDivmodInts(t *testing.T) {
	v, err := Divmod([]objects.Object{objects.NewInt(-7), objects.NewInt(3)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	tup := v.(*objects.Tuple)
	q, _ := tup.Item(0).(*objects.Int).Int64()
	r, _ := tup.Item(1).(*objects.Int).Int64()
	if q != -3 || r != 2 {
		t.Errorf("divmod(-7, 3) = (%d, %d), want (-3, 2)", q, r)
	}
}

// TestPowTwoArg pins pow(2, 10) == 1024.
func TestPowTwoArg(t *testing.T) {
	v, err := Pow([]objects.Object{objects.NewInt(2), objects.NewInt(10)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 1024 {
		t.Errorf("pow(2, 10) = %d, want 1024", got)
	}
}

// TestPowThreeArg pins pow(3, 4, 5) == 81 % 5 == 1.
func TestPowThreeArg(t *testing.T) {
	v, err := Pow([]objects.Object{objects.NewInt(3), objects.NewInt(4), objects.NewInt(5)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 1 {
		t.Errorf("pow(3, 4, 5) = %d, want 1", got)
	}
}

// TestChrAscii pins chr(65) == 'A'.
func TestChrAscii(t *testing.T) {
	v, err := Chr([]objects.Object{objects.NewInt(65)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := objects.Str(v)
	if got != "A" {
		t.Errorf("chr(65) = %q, want A", got)
	}
}

// TestChrOutOfRange pins the ValueError shape.
func TestChrOutOfRange(t *testing.T) {
	_, err := Chr([]objects.Object{objects.NewInt(0x110000)}, nil)
	if err == nil || !strings.Contains(err.Error(), "ValueError") {
		t.Fatalf("err = %v, want ValueError", err)
	}
}

// TestOrdSingleChar pins ord('Z') == 90.
func TestOrdSingleChar(t *testing.T) {
	v, err := Ord([]objects.Object{objects.NewStr("Z")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 90 {
		t.Errorf("ord('Z') = %d, want 90", got)
	}
}

// TestOrdRejectsMultiChar pins the length-1 guard.
func TestOrdRejectsMultiChar(t *testing.T) {
	_, err := Ord([]objects.Object{objects.NewStr("ab")}, nil)
	if err == nil {
		t.Fatal("expected TypeError")
	}
}

// TestBinOctHexShape pins the prefixed forms for both signs.
func TestBinOctHexShape(t *testing.T) {
	cases := []struct {
		fn   func([]objects.Object, map[string]objects.Object) (objects.Object, error)
		in   int64
		want string
	}{
		{Bin, 5, "0b101"},
		{Bin, -5, "-0b101"},
		{Oct, 8, "0o10"},
		{Hex, 255, "0xff"},
		{Hex, -255, "-0xff"},
	}
	for _, tc := range cases {
		v, err := tc.fn([]objects.Object{objects.NewInt(tc.in)}, nil)
		if err != nil {
			t.Fatalf("%d: %v", tc.in, err)
		}
		got, _ := objects.Str(v)
		if got != tc.want {
			t.Errorf("got %q, want %q", got, tc.want)
		}
	}
}

// TestAsciiEscapesNonAscii pins the \xHH / \uHHHH / \UHHHHHHHH escapes.
func TestAsciiEscapesNonAscii(t *testing.T) {
	in := "café"
	v, err := ASCII([]objects.Object{objects.NewStr(in)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := objects.Str(v)
	if !strings.Contains(got, `\xe9`) {
		t.Errorf("ascii(%q) = %q, want backslash escape", in, got)
	}
}

// TestFormatEmptySpec pins the str(value) shortcut.
func TestFormatEmptySpec(t *testing.T) {
	v, err := Format([]objects.Object{objects.NewInt(42)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := objects.Str(v)
	if got != "42" {
		t.Errorf("format(42) = %q, want '42'", got)
	}
}

// TestFormatIntSpec pins the format-spec mini-language for int.
func TestFormatIntSpec(t *testing.T) {
	v, err := Format([]objects.Object{objects.NewInt(255), objects.NewStr("x")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := objects.Str(v)
	if got != "ff" {
		t.Errorf("format(255, 'x') = %q, want ff", got)
	}
}
