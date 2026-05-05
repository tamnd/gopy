package string

import "testing"

func TestParseString(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		isBytes bool
		raw     bool
	}{
		{`'hello'`, "hello", false, false},
		{`"hello"`, "hello", false, false},
		{`'a\nb'`, "a\nb", false, false},
		{`'a\tb'`, "a\tb", false, false},
		{`'\x41'`, "A", false, false},
		{`'é'`, "é", false, false},
		{`'\101'`, "A", false, false},
		{`r'\n'`, `\n`, false, true},
		{`R'\n'`, `\n`, false, true},
		{`'''triple'''`, "triple", false, false},
		{`"""triple"""`, "triple", false, false},
		{`u'unicode'`, "unicode", false, false},
		{`'\\'`, `\`, false, false},
		{`'\''`, "'", false, false},
		{`'\"'`, `"`, false, false},
		{`'\q'`, `\q`, false, false},
	}
	for _, c := range cases {
		got, err := ParseString([]byte(c.in))
		if err != nil {
			t.Errorf("ParseString(%q) err = %v", c.in, err)
			continue
		}
		if got.IsBytes != c.isBytes {
			t.Errorf("ParseString(%q) IsBytes = %v want %v", c.in, got.IsBytes, c.isBytes)
		}
		if got.IsRaw != c.raw {
			t.Errorf("ParseString(%q) IsRaw = %v want %v", c.in, got.IsRaw, c.raw)
		}
		if !c.isBytes && got.Text != c.want {
			t.Errorf("ParseString(%q) Text = %q want %q", c.in, got.Text, c.want)
		}
	}
}

func TestParseBytes(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`b'hi'`, "hi"},
		{`B'hi'`, "hi"},
		{`b'\x41'`, "A"},
		{`b'\101'`, "A"},
		{`rb'\n'`, `\n`},
		{`br'\n'`, `\n`},
	}
	for _, c := range cases {
		got, err := ParseString([]byte(c.in))
		if err != nil {
			t.Errorf("ParseString(%q) err = %v", c.in, err)
			continue
		}
		if !got.IsBytes {
			t.Errorf("ParseString(%q) not bytes", c.in)
		}
		if string(got.Bytes) != c.want {
			t.Errorf("ParseString(%q) Bytes = %q want %q", c.in, got.Bytes, c.want)
		}
	}
}

func TestBytesPassesThroughUnicodeEscapes(t *testing.T) {
	// In bytes literals, \u \U \N{...} are not interpreted; the
	// backslash and the letter stay in the output verbatim.
	//
	// CPython: Objects/bytesobject.c _PyBytes_DecodeEscape
	cases := []struct{ in, want string }{
		{`b'\U0001F600'`, `\U0001F600`},
		{`b'\N{LATIN SMALL LETTER A}'`, `\N{LATIN SMALL LETTER A}`},
	}
	for _, c := range cases {
		got, err := ParseString([]byte(c.in))
		if err != nil {
			t.Errorf("ParseString(%q) err = %v", c.in, err)
			continue
		}
		if string(got.Bytes) != c.want {
			t.Errorf("ParseString(%q) Bytes = %q want %q", c.in, got.Bytes, c.want)
		}
	}
}

func TestBytesRejectsHighChar(t *testing.T) {
	if _, err := ParseString([]byte("b'é'")); err == nil {
		t.Errorf("expected non-ASCII bytes literal to fail")
	}
}
