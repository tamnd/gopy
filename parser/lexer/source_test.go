package lexer

import "testing"

func TestDetectEncodingCookie(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"none", "x = 1\n", ""},
		{"first_line_colon", "# coding: latin-1\nx = 1\n", "latin-1"},
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
	st := FromString("# coding: latin-1\nx = 1\n", ModeFile)
	if st.Encoding() != "latin-1" {
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
