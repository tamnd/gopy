package objects

import "testing"

// TestIntFormatSpec drives the int.__format__ slot through the cases
// pyperformance benchmarks (and CPython's Lib/test/test_format.py)
// actually reach. Each row mirrors a CPython interactive transcript so
// any future regression shows up as a byte diff against the canonical
// answer.
//
// CPython: Lib/test/test_format.py format_strings sweep
func TestIntFormatSpec(t *testing.T) {
	cases := []struct {
		v    int64
		spec string
		want string
	}{
		{255, "04x", "00ff"},
		{255, "#06x", "0x00ff"},
		{255, "X", "FF"},
		{255, "08b", "11111111"},
		{255, "o", "377"},
		{42, "d", "42"},
		{42, "5d", "   42"},
		{42, "05d", "00042"},
		{-42, "05d", "-0042"},
		{42, "+d", "+42"},
		{42, " d", " 42"},
		{1234567, ",d", "1,234,567"},
		{1234567, "_d", "1_234_567"},
		{255, "_b", "1111_1111"},
		{65, "c", "A"},
		{42, "<6", "42    "},
		{42, ">6", "    42"},
		{42, "^6", "  42  "},
		{42, "*^6", "**42**"},
	}
	for _, c := range cases {
		got, err := Format(NewInt(c.v), c.spec)
		if err != nil {
			t.Errorf("Format(%d, %q): %v", c.v, c.spec, err)
			continue
		}
		if got != c.want {
			t.Errorf("Format(%d, %q): got %q want %q", c.v, c.spec, got, c.want)
		}
	}
}

// TestIntFormatFloatCoercion exercises the float-spec branch. CPython
// promotes the int through PyNumber_Float for any 'e'/'E'/'f'/'F'/'g'/
// 'G'/'%' code; gopy mirrors that via bigIntToFloat64 + FormatFloat.
//
// CPython: Python/formatter_unicode.c:1620 format_long_internal float branch
func TestIntFormatFloatCoercion(t *testing.T) {
	cases := []struct {
		v    int64
		spec string
		want string
	}{
		{255, ".2g", "2.6e+02"},
		{255, "10.2f", "    255.00"},
		{255, ".2e", "2.55e+02"},
		{255, ".2%", "25500.00%"},
		{0, "f", "0.000000"},
	}
	for _, c := range cases {
		got, err := Format(NewInt(c.v), c.spec)
		if err != nil {
			t.Errorf("Format(%d, %q): %v", c.v, c.spec, err)
			continue
		}
		if got != c.want {
			t.Errorf("Format(%d, %q): got %q want %q", c.v, c.spec, got, c.want)
		}
	}
}

// TestBoolFormatInheritsIntSlot makes sure bool.__format__ goes through
// the same renderer as int. CPython inherits the slot via tp_base; gopy
// wires it explicitly in long_format.go since the runtime does not walk
// the base chain for the Format slot on built-in types.
//
// CPython: Objects/boolobject.c:222 PyBool_Type tp_base = &PyLong_Type
func TestBoolFormatInheritsIntSlot(t *testing.T) {
	cases := []struct {
		v    Object
		spec string
		want string
	}{
		{True(), "d", "1"},
		{False(), "d", "0"},
		{True(), "04x", "0001"},
		{True(), "+d", "+1"},
	}
	for _, c := range cases {
		got, err := Format(c.v, c.spec)
		if err != nil {
			t.Errorf("Format(%v, %q): %v", c.v, c.spec, err)
			continue
		}
		if got != c.want {
			t.Errorf("Format(%v, %q): got %q want %q", c.v, c.spec, got, c.want)
		}
	}
}

// TestIntFormatJSONEscapeDct reproduces the import-time failure from
// stdlib/json/encoder.py:31. ESCAPE_DCT initializes with
// '\\u{0:04x}'.format(i) for each control char. Before P9 this raised
// `TypeError: unsupported format string passed to int.__format__` and
// kept `import json` from completing.
//
// CPython: Lib/json/encoder.py:31 ESCAPE_DCT
func TestIntFormatJSONEscapeDct(t *testing.T) {
	for i := range 0x20 {
		_, err := Format(NewInt(int64(i)), "04x")
		if err != nil {
			t.Fatalf("Format(%d, %q): %v", i, "04x", err)
		}
	}
}
