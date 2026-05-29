package objects

import (
	"math/cmplx"
	"testing"
)

func TestComplexRepr(t *testing.T) {
	cases := []struct {
		re, im float64
		want   string
	}{
		{0, 0, "0j"},
		{0, 1, "1j"},
		{1, 0, "(1+0j)"},
		{1, 2, "(1+2j)"},
		{1, -2, "(1-2j)"},
		{-1.5, 2.5, "(-1.5+2.5j)"},
	}
	for _, c := range cases {
		got, err := complexRepr(NewComplex(c.re, c.im))
		if err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Errorf("complexRepr(%v+%vj) = %q, want %q", c.re, c.im, got, c.want)
		}
	}
}

func TestComplexArith(t *testing.T) {
	a := NewComplex(1, 2)
	b := NewComplex(3, 4)
	got, err := complexAdd(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if got.(*Complex).v != complex(4, 6) {
		t.Errorf("add: got %v, want 4+6j", got.(*Complex).v)
	}
	got, err = complexMul(a, b)
	if err != nil {
		t.Fatal(err)
	}
	want := complex(1, 2) * complex(3, 4)
	if got.(*Complex).v != want {
		t.Errorf("mul: got %v, want %v", got.(*Complex).v, want)
	}
	got, err = complexSub(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if got.(*Complex).v != complex(-2, -2) {
		t.Errorf("sub: got %v", got.(*Complex).v)
	}
}

func TestComplexAbs(t *testing.T) {
	got, err := complexAbs(NewComplex(3, 4))
	if err != nil {
		t.Fatal(err)
	}
	if got.(*Float).v != 5 {
		t.Errorf("abs(3+4j) = %v, want 5", got.(*Float).v)
	}
}

func TestComplexDivByZero(t *testing.T) {
	_, err := complexTrueDiv(NewComplex(1, 0), NewComplex(0, 0))
	if err == nil {
		t.Error("complex /0 should error")
	}
}

func TestComplexNoOrdering(t *testing.T) {
	a := NewComplex(1, 0)
	b := NewComplex(2, 0)
	// complexRichCmp returns NotImplemented per CPython's slot contract;
	// RichCmp (do_richcompare) is the function that raises TypeError.
	if _, err := RichCmp(a, b, CompareLT); err == nil {
		t.Error("complex < should error")
	}
}

func TestComplexEqIntFloat(t *testing.T) {
	c := NewComplex(3, 0)
	got, err := complexRichCmp(c, NewInt(3), CompareEQ)
	if err != nil {
		t.Fatal(err)
	}
	if got != True() {
		t.Error("complex(3,0) == 3 should be true")
	}
	got, err = complexRichCmp(c, NewFloat(3.0), CompareEQ)
	if err != nil {
		t.Fatal(err)
	}
	if got != True() {
		t.Error("complex(3,0) == 3.0 should be true")
	}
}

func TestComplexPow(t *testing.T) {
	a := NewComplex(0, 1)
	got, err := complexPower(a, NewInt(2), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := cmplx.Pow(complex(0, 1), complex(2, 0))
	if cmplx.Abs(got.(*Complex).v-want) > 1e-9 {
		t.Errorf("(0+1j)**2 = %v, want %v", got.(*Complex).v, want)
	}
}
