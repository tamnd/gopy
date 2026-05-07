package objects

import (
	"math"
	"strings"
	"testing"
)

func mustModulo(t *testing.T, format string, args Object) string {
	t.Helper()
	got, err := unicodeModulo(NewStr(format), args)
	if err != nil {
		t.Fatalf("%q %% %v: %v", format, args, err)
	}
	u, ok := got.(*Unicode)
	if !ok {
		t.Fatalf("result type %T, want *Unicode", got)
	}
	return u.v
}

func TestStrModuloLiteralPercent(t *testing.T) {
	got := mustModulo(t, "100%%", NewTuple(nil))
	if got != "100%" {
		t.Errorf("got %q, want 100%%", got)
	}
}

func TestStrModuloS(t *testing.T) {
	got := mustModulo(t, "hi %s", NewTuple([]Object{NewStr("there")}))
	if got != "hi there" {
		t.Errorf("got %q", got)
	}
}

func TestStrModuloSinglePositional(t *testing.T) {
	got := mustModulo(t, "%s", NewStr("solo"))
	if got != "solo" {
		t.Errorf("got %q", got)
	}
}

func TestStrModuloD(t *testing.T) {
	got := mustModulo(t, "%d-%d", NewTuple([]Object{NewInt(7), NewInt(-3)}))
	if got != "7--3" {
		t.Errorf("got %q", got)
	}
}

func TestStrModuloZeroPaddedSign(t *testing.T) {
	got := mustModulo(t, "%+05d", NewTuple([]Object{NewInt(42)}))
	if got != "+0042" {
		t.Errorf("got %q, want +0042", got)
	}
}

func TestStrModuloLeftJustify(t *testing.T) {
	got := mustModulo(t, "[%-5d]", NewTuple([]Object{NewInt(7)}))
	if got != "[7    ]" {
		t.Errorf("got %q", got)
	}
}

func TestStrModuloHex(t *testing.T) {
	got := mustModulo(t, "%#08x %X", NewTuple([]Object{NewInt(255), NewInt(255)}))
	if got != "0x0000ff FF" {
		t.Errorf("got %q", got)
	}
}

func TestStrModuloOctal(t *testing.T) {
	got := mustModulo(t, "%#o", NewTuple([]Object{NewInt(8)}))
	if got != "0o10" {
		t.Errorf("got %q", got)
	}
}

func TestStrModuloFloatF(t *testing.T) {
	got := mustModulo(t, "%.2f", NewTuple([]Object{NewFloat(3.14159)}))
	if got != "3.14" {
		t.Errorf("got %q", got)
	}
}

func TestStrModuloFloatE(t *testing.T) {
	got := mustModulo(t, "%.2e", NewTuple([]Object{NewFloat(1234.5)}))
	if got != "1.23e+03" {
		t.Errorf("got %q", got)
	}
}

func TestStrModuloFloatSpecial(t *testing.T) {
	gotInf := mustModulo(t, "%f", NewTuple([]Object{NewFloat(positiveInf())}))
	if gotInf != "inf" {
		t.Errorf("inf: got %q", gotInf)
	}
	gotNan := mustModulo(t, "%F", NewTuple([]Object{NewFloat(nan())}))
	if gotNan != "NAN" {
		t.Errorf("NaN: got %q", gotNan)
	}
}

func TestStrModuloChar(t *testing.T) {
	got := mustModulo(t, "%c%c", NewTuple([]Object{NewInt('A'), NewStr("z")}))
	if got != "Az" {
		t.Errorf("got %q", got)
	}
}

func TestStrModuloRepr(t *testing.T) {
	got := mustModulo(t, "%r", NewTuple([]Object{NewStr("x")}))
	if got != "'x'" {
		t.Errorf("got %q", got)
	}
}

func TestStrModuloAsciiEscape(t *testing.T) {
	got := mustModulo(t, "%a", NewTuple([]Object{NewStr("é")}))
	if !strings.Contains(got, "\\xe9") {
		t.Errorf("got %q, want backslash escape", got)
	}
}

func TestStrModuloMapping(t *testing.T) {
	d := NewDict()
	_ = d.SetItem(NewStr("name"), NewStr("Tam"))
	_ = d.SetItem(NewStr("n"), NewInt(7))
	got := mustModulo(t, "%(name)s=%(n)d", d)
	if got != "Tam=7" {
		t.Errorf("got %q", got)
	}
}

func TestStrModuloStarWidth(t *testing.T) {
	got := mustModulo(t, "%*d", NewTuple([]Object{NewInt(5), NewInt(7)}))
	if got != "    7" {
		t.Errorf("got %q", got)
	}
}

func TestStrModuloStarPrecision(t *testing.T) {
	got := mustModulo(t, "%.*s", NewTuple([]Object{NewInt(2), NewStr("abcdef")}))
	if got != "ab" {
		t.Errorf("got %q", got)
	}
}

func TestStrModuloNotEnoughArgs(t *testing.T) {
	_, err := unicodeModulo(NewStr("%d %d"), NewTuple([]Object{NewInt(1)}))
	if err == nil || !strings.Contains(err.Error(), "not enough") {
		t.Errorf("err = %v", err)
	}
}

func TestStrModuloTooManyArgs(t *testing.T) {
	_, err := unicodeModulo(NewStr("%d"), NewTuple([]Object{NewInt(1), NewInt(2)}))
	if err == nil || !strings.Contains(err.Error(), "not all arguments") {
		t.Errorf("err = %v", err)
	}
}

func TestStrModuloUnknownConversion(t *testing.T) {
	_, err := unicodeModulo(NewStr("%q"), NewTuple([]Object{NewInt(1)}))
	if err == nil {
		t.Error("want error on unknown conversion")
	}
}

func TestStrModuloViaNumberRemainder(t *testing.T) {
	got, err := NumberRemainder(NewStr("%d"), NewInt(99))
	if err != nil {
		t.Fatalf("NumberRemainder: %v", err)
	}
	u, ok := got.(*Unicode)
	if !ok || u.v != "99" {
		t.Errorf("got %T %v, want *Unicode 99", got, got)
	}
}

func positiveInf() float64 { return math.Inf(1) }

func nan() float64 { return math.NaN() }
