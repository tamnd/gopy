package intrinsics

import (
	"errors"
	"testing"
)

func TestUnaryIDValues(t *testing.T) {
	cases := []struct {
		name string
		got  int
		want int
	}{
		{"Invalid", Unary1Invalid, 0},
		{"Print", UnaryPrintID, 1},
		{"ImportStar", UnaryImportStarID, 2},
		{"StopIterError", UnaryStopIterErrorID, 3},
		{"AsyncGenWrap", UnaryAsyncGenWrapID, 4},
		{"Positive", UnaryPositiveID, 5},
		{"ListToTuple", UnaryListToTupleID, 6},
		{"Typevar", UnaryTypevarID, 7},
		{"Paramspec", UnaryParamspecID, 8},
		{"Typevartuple", UnaryTypevartupleID, 9},
		{"SubscriptGeneric", UnarySubscriptGenericID, 10},
		{"Typealias", UnaryTypealiasID, 11},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.name, c.got, c.want)
		}
	}
	if MaxUnary != 11 {
		t.Errorf("MaxUnary = %d, want 11", MaxUnary)
	}
}

func TestBinaryIDValues(t *testing.T) {
	cases := []struct {
		name string
		got  int
		want int
	}{
		{"Invalid", Binary2Invalid, 0},
		{"PrepReraiseStar", BinaryPrepReraiseStarID, 1},
		{"TypevarWithBound", BinaryTypevarWithBoundID, 2},
		{"TypevarWithConstraints", BinaryTypevarWithConstraintsID, 3},
		{"SetFunctionTypeParams", BinarySetFunctionTypeParamsID, 4},
		{"SetTypeparamDefault", BinarySetTypeparamDefaultID, 5},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.name, c.got, c.want)
		}
	}
	if MaxBinary != 5 {
		t.Errorf("MaxBinary = %d, want 5", MaxBinary)
	}
}

func TestUnaryTablePopulated(t *testing.T) {
	for i := 0; i <= MaxUnary; i++ {
		if UnaryTable[i] == nil {
			t.Errorf("UnaryTable[%d] is nil", i)
		}
	}
}

func TestBinaryTablePopulated(t *testing.T) {
	for i := 0; i <= MaxBinary; i++ {
		if BinaryTable[i] == nil {
			t.Errorf("BinaryTable[%d] is nil", i)
		}
	}
}

func TestUnaryInvalidErrors(t *testing.T) {
	_, err := Unary1InvalidFn(nil, nil)
	if err == nil {
		t.Error("Unary1InvalidFn must return error")
	}
}

func TestBinaryInvalidErrors(t *testing.T) {
	_, err := Binary2InvalidFn(nil, nil, nil)
	if err == nil {
		t.Error("Binary2InvalidFn must return error")
	}
}

func TestStubHelpersReturnNotImplemented(t *testing.T) {
	// Implemented helpers, skip them in the stub sweep.
	implementedUnary := map[int]bool{
		UnaryListToTupleID: true,
	}
	for i := 1; i <= MaxUnary; i++ {
		if implementedUnary[i] {
			continue
		}
		_, err := UnaryTable[i](nil, nil)
		var nie *notImplementedError
		if !errors.As(err, &nie) {
			t.Errorf("UnaryTable[%d] returned %v, want notImplementedError", i, err)
		}
	}
	for i := 1; i <= MaxBinary; i++ {
		_, err := BinaryTable[i](nil, nil, nil)
		var nie *notImplementedError
		if !errors.As(err, &nie) {
			t.Errorf("BinaryTable[%d] returned %v, want notImplementedError", i, err)
		}
	}
}
