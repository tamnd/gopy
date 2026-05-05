package string

import (
	"strings"
	"testing"
)

func TestScanFStringPlainAndExpr(t *testing.T) {
	segs, err := ScanFString("a{x}b")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(segs) != 3 {
		t.Fatalf("segs = %d, want 3 (%v)", len(segs), segs)
	}
	if segs[0].Kind != SegLiteral || segs[0].Literal != "a" {
		t.Errorf("seg0 = %+v", segs[0])
	}
	if segs[1].Kind != SegExpr || segs[1].ExprText != "x" {
		t.Errorf("seg1 = %+v", segs[1])
	}
	if segs[2].Kind != SegLiteral || segs[2].Literal != "b" {
		t.Errorf("seg2 = %+v", segs[2])
	}
}

func TestScanFStringDoubleBrace(t *testing.T) {
	segs, err := ScanFString("{{x}}")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(segs) != 1 || segs[0].Kind != SegLiteral || segs[0].Literal != "{x}" {
		t.Errorf("doubled-brace fold lost data: %+v", segs)
	}
}

func TestScanFStringConversionAndFormat(t *testing.T) {
	segs, err := ScanFString("{x!r:0.2f}")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(segs) != 1 {
		t.Fatalf("len = %d", len(segs))
	}
	s := segs[0]
	if s.ExprText != "x" || s.Conversion != 'r' || s.FormatSpec != "0.2f" {
		t.Errorf("seg = %+v", s)
	}
}

func TestScanFStringDebug(t *testing.T) {
	segs, _ := ScanFString("{x=}")
	if len(segs) != 1 || !segs[0].IsDebug || segs[0].ExprText != "x" {
		t.Errorf("debug flag lost: %+v", segs)
	}
}

func TestScanFStringSingleClose(t *testing.T) {
	_, err := ScanFString("a}b")
	if err == nil || !strings.Contains(err.Error(), "single '}'") {
		t.Errorf("err = %v, want single brace error", err)
	}
}

func TestScanFStringUnterminated(t *testing.T) {
	_, err := ScanFString("a{x")
	if err == nil || !strings.Contains(err.Error(), "expecting '}'") {
		t.Errorf("err = %v", err)
	}
}

func TestScanTStringMatchesFString(t *testing.T) {
	a, _ := ScanFString("a{x}b")
	b, _ := ScanTString("a{x}b")
	if len(a) != len(b) {
		t.Errorf("t-string scan diverged: %d vs %d", len(a), len(b))
	}
}
