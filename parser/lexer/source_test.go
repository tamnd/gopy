package lexer

import "testing"

func TestDetectEncodingCookie(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"none", "x = 1\n", ""},
		{"first_line_colon", "# coding: latin-1\nx = 1\n", "iso-8859-1"},
		{"first_line_equals", "# -*- coding=utf-8 -*-\n", "utf-8"},
		{"second_line", "#!/usr/bin/env python\n# coding: cp1252\n", "cp1252"},
		{"third_line_ignored", "\n\n# coding: utf-8\n", ""},
		{"non_comment", "x = 'coding: utf-8'\n", ""},
		{"crlf", "# coding: utf-8\r\nx\r\n", "utf-8"},
		// Line 1 carries real code, so the line-2 cookie must be
		// ignored. Matches CPython's STATE_NORMAL transition in
		// _PyTokenizer_check_coding_spec.
		{"code_then_cookie", "x = 1\n# coding: latin-1\n", ""},
		// Form-feed counts as whitespace, so a blank-but-FF first
		// line still permits the line-2 cookie scan.
		{"formfeed_then_cookie", "\014\n# coding: utf-8\n", "utf-8"},
		// Backslash continuation: when line 1 ends with `\`, line 2
		// carries cont_line == 1 in CPython and the cookie is
		// ignored. Mirrors helpers.c:392-396.
		{"cont_line_skips_cookie", "\\\n# coding: utf-8\n", ""},
	}
	for _, c := range cases {
		got := DetectEncodingCookie([]byte(c.src))
		if got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestBOMOnly(t *testing.T) {
	st := FromBytes([]byte("\xef\xbb\xbfx = 1\n"), ModeFile)
	if st.Encoding() != "utf-8" {
		t.Errorf("BOM-only Encoding = %q, want utf-8", st.Encoding())
	}
}

func TestBOMWithUTF8Cookie(t *testing.T) {
	src := []byte("\xef\xbb\xbf# coding: utf-8\nx = 1\n")
	if msg := CheckBOMCookieConflict(src); msg != "" {
		t.Errorf("CheckBOMCookieConflict = %q, want empty for utf-8", msg)
	}
}

func TestBOMConflictsWithLatin1Cookie(t *testing.T) {
	src := []byte("\xef\xbb\xbf# coding: latin-1\nx = 1\n")
	msg := CheckBOMCookieConflict(src)
	if msg == "" {
		t.Errorf("CheckBOMCookieConflict = empty, want conflict text")
	}
}

func TestBOMConflictNoneWithoutCookie(t *testing.T) {
	src := []byte("\xef\xbb\xbfx = 1\n")
	if msg := CheckBOMCookieConflict(src); msg != "" {
		t.Errorf("CheckBOMCookieConflict = %q, want empty", msg)
	}
}

func TestNormalizeNewlines(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"a\nb\n", "a\nb\n"},
		{"a\r\nb\r\n", "a\nb\n"},
		{"a\rb\rc", "a\nb\nc"},
		{"a\r\n\rb", "a\n\nb"},
	}
	for _, c := range cases {
		got := string(NormalizeNewlines([]byte(c.in)))
		if got != c.want {
			t.Errorf("Normalize(%q) = %q want %q", c.in, got, c.want)
		}
	}
}

func TestFromBytesPicksUpEncoding(t *testing.T) {
	// Cookie detection runs on the bytes-input path; FromString (str
	// path, equivalent to CPython _PyTokenizer_FromUTF8) ignores
	// cookies because the source is presumed pre-decoded.
	st := FromBytes([]byte("# coding: latin-1\nx = 1\n"), ModeFile)
	if st.Encoding() != "iso-8859-1" {
		t.Errorf("Encoding = %q", st.Encoding())
	}
}

func TestFromBytesNormalizesCRLF(t *testing.T) {
	toks := tokenize_(t, "x = 1\r\n")
	got := kinds(toks)
	if len(got) < 4 {
		t.Fatalf("short token stream: %v", got)
	}
}

// TestValidUTF8RejectsOverlongAndSurrogates pins the full
// stringlib/codecs.h:utf8_decode rejection set: overlong encodings
// (\xC0 / \xC1 / \xE0\x80-\x9F / \xF0\x80-\x8F), surrogates
// (\xED\xA0-\xBF), and overflow past U+10FFFF (\xF4\x90+, \xF5+).
// Mirrors Parser/tokenizer/helpers.c:446 valid_utf8.
func TestValidUTF8RejectsOverlongAndSurrogates(t *testing.T) {
	rejects := [][]byte{
		// Overlong 2-byte: \xC0\x80 would encode U+0000.
		{0xC0, 0x80},
		// Overlong 2-byte: \xC1\xBF would encode U+007F.
		{0xC1, 0xBF},
		// Overlong 3-byte: \xE0\x80\x80, \xE0\x9F\xBF.
		{0xE0, 0x80, 0x80},
		{0xE0, 0x9F, 0xBF},
		// Surrogate: \xED\xA0\x80 (U+D800).
		{0xED, 0xA0, 0x80},
		// Surrogate: \xED\xBF\xBF (U+DFFF).
		{0xED, 0xBF, 0xBF},
		// Overlong 4-byte: \xF0\x80\x80\x80, \xF0\x8F\xBF\xBF.
		{0xF0, 0x80, 0x80, 0x80},
		{0xF0, 0x8F, 0xBF, 0xBF},
		// Overflow: \xF4\x90\x80\x80 (one past U+10FFFF).
		{0xF4, 0x90, 0x80, 0x80},
		// Invalid leading byte: \xF5+.
		{0xF5, 0x80, 0x80, 0x80},
		{0xFF},
		// Bare continuation byte.
		{0x80},
		// Truncated 2-byte sequence.
		{0xC2},
		// Bad continuation in 3-byte sequence.
		{0xE0, 0xA0, 0x40},
	}
	for _, src := range rejects {
		if n := validUTF8(src); n != 0 {
			t.Errorf("validUTF8(%x) = %d, want 0", src, n)
		}
	}

	accepts := []struct {
		src  []byte
		want int
	}{
		{[]byte("A"), 1},
		{[]byte{0xC2, 0xA9}, 2},             // U+00A9 (copyright)
		{[]byte{0xE2, 0x82, 0xAC}, 3},       // U+20AC (euro)
		{[]byte{0xF0, 0x9F, 0x98, 0x80}, 4}, // U+1F600 (grinning face)
		// Boundary: \xF4\x8F\xBF\xBF encodes the maximum U+10FFFF.
		{[]byte{0xF4, 0x8F, 0xBF, 0xBF}, 4},
	}
	for _, c := range accepts {
		if n := validUTF8(c.src); n != c.want {
			t.Errorf("validUTF8(%x) = %d, want %d", c.src, n, c.want)
		}
	}
}
