package string

import "testing"

func TestTStringPlain(t *testing.T) {
	segs, err := ScanTString("hello")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(segs) != 1 || segs[0].Kind != SegLiteral || segs[0].Literal != "hello" {
		t.Errorf("plain t-string: %+v", segs)
	}
}

func TestTStringExpr(t *testing.T) {
	segs, err := ScanTString("a{x}b")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(segs) != 3 {
		t.Fatalf("got %d segments, want 3: %+v", len(segs), segs)
	}
	if segs[1].Kind != SegExpr || segs[1].ExprText != "x" {
		t.Errorf("expr seg = %+v", segs[1])
	}
}

func TestTStringConversion(t *testing.T) {
	segs, err := ScanTString("{x!r}")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(segs) != 1 || segs[0].Conversion != 'r' || segs[0].ExprText != "x" {
		t.Errorf("conversion seg = %+v", segs)
	}
}

func TestTStringFormatSpec(t *testing.T) {
	segs, err := ScanTString("{x:.2f}")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(segs) != 1 || segs[0].FormatSpec != ".2f" || segs[0].ExprText != "x" {
		t.Errorf("format seg = %+v", segs)
	}
}
