package lexer

import (
	"strings"
	"testing"

	"github.com/tamnd/gopy/token"
)

// TestLatin1SourceDecodes pins that a `# coding: latin-1` cookie causes
// the source bytes to be decoded through the codecs registry, so a
// latin-1 high byte (0xE9 == é) survives tokenization as the matching
// UTF-8 sequence.
func TestLatin1SourceDecodes(t *testing.T) {
	src := []byte("# coding: latin-1\nx = \"caf\xe9\"\n")
	st := FromBytes(src, ModeFile)
	if st.Encoding() != "latin-1" {
		t.Fatalf("Encoding = %q, want latin-1", st.Encoding())
	}
	var sawString bool
	for i := 0; i < 50; i++ {
		tk := st.Get()
		if tk.Kind == token.STRING {
			sawString = true
			// The raw bytes should now be valid UTF-8 with `é` in place
			// of the original 0xE9.
			if !strings.Contains(string(tk.Bytes), "café") {
				t.Errorf("string token = %q, want to contain %q", tk.Bytes, "café")
			}
		}
		if tk.Kind == token.ENDMARKER || tk.Kind == token.ERRORTOKEN {
			break
		}
	}
	if !sawString {
		t.Fatal("expected a STRING token")
	}
	if st.Err() != nil {
		t.Errorf("unexpected lexer error: %v", st.Err())
	}
}

// TestUnknownCookieRecordsError pins that an unrecognized encoding
// name lands as a SyntaxError-shaped lexer error and the eEncoding
// done state.
func TestUnknownCookieRecordsError(t *testing.T) {
	st := FromBytes([]byte("# coding: not_a_real_codec\nx = 1\n"), ModeFile)
	if st.Err() == nil {
		t.Fatal("expected lexer error for unknown encoding")
	}
	if !strings.Contains(st.Err().Message, "encoding problem") {
		t.Errorf("error = %q, want \"encoding problem\"", st.Err().Message)
	}
}

// TestBOMConflictWithLatin1Cookie pins that a UTF-8 BOM combined with
// a non-utf-8 cookie surfaces the CPython error wording, namely
// "encoding problem: <name> with BOM".
//
// CPython: Parser/tokenizer/helpers.c:425 check_coding_spec
func TestBOMConflictWithLatin1Cookie(t *testing.T) {
	src := []byte("\xef\xbb\xbf# coding: latin-1\nx = 1\n")
	st := FromBytes(src, ModeFile)
	if st.Err() == nil {
		t.Fatal("expected lexer error for BOM/cookie mismatch")
	}
	if !strings.Contains(st.Err().Message, "encoding problem: latin-1 with BOM") {
		t.Errorf("error = %q, want to mention \"encoding problem: latin-1 with BOM\"", st.Err().Message)
	}
}

// TestUTF8CookieNoDecode pins that an explicit utf-8 cookie does not
// trip the decode path (the source remains untouched).
func TestUTF8CookieNoDecode(t *testing.T) {
	st := FromBytes([]byte("# coding: utf-8\nx = 1\n"), ModeFile)
	if st.Encoding() != "utf-8" {
		t.Errorf("Encoding = %q, want utf-8", st.Encoding())
	}
	if st.Err() != nil {
		t.Errorf("unexpected error: %v", st.Err())
	}
}

// TestReaderDriverDecodesLatin1 pins that the file driver also runs
// the cookie/decode pipeline, slurping the stream into FromBytes when
// a non-utf-8 cookie is present.
func TestReaderDriverDecodesLatin1(t *testing.T) {
	src := "# coding: latin-1\ny = \"caf\xe9\"\n"
	st := FromReader(strings.NewReader(src), ModeFile)
	if st.Encoding() != "latin-1" {
		t.Fatalf("Encoding = %q, want latin-1", st.Encoding())
	}
	var sawString bool
	for i := 0; i < 50; i++ {
		tk := st.Get()
		if tk.Kind == token.STRING && strings.Contains(string(tk.Bytes), "café") {
			sawString = true
		}
		if tk.Kind == token.ENDMARKER || tk.Kind == token.ERRORTOKEN {
			break
		}
	}
	if !sawString {
		t.Fatal("expected decoded STRING token from reader driver")
	}
}

// TestNonUTF8ErrorMessageFormat pins the byte-for-byte parity of the
// "Non-UTF-8 code starting with '\xNN' on line N..." SyntaxError text
// against CPython's _PyTokenizer_syntaxerror_known_range template.
//
// CPython: Parser/tokenizer/helpers.c:529 _PyTokenizer_syntaxerror_known_range
func TestNonUTF8ErrorMessageFormat(t *testing.T) {
	// 0xE9 on line 2, no encoding cookie, no BOM.
	src := []byte("x = 1\nbad = \"\xe9\"\n")
	st := FromBytes(src, ModeFile)
	if st.Err() == nil {
		t.Fatal("expected lexer error for non-utf-8 byte without cookie")
	}
	want := "Non-UTF-8 code starting with '\\xe9' on line 2, " +
		"but no encoding declared; " +
		"see https://peps.python.org/pep-0263/ for details"
	if st.Err().Message != want {
		t.Errorf("error message = %q\nwant %q", st.Err().Message, want)
	}
}

// TestReaderDriverPlainUTF8 pins that the streaming path is preserved
// when there is no non-utf-8 cookie. Refilling line-by-line is the
// established behavior; only the cookie case slurps eagerly.
func TestReaderDriverPlainUTF8(t *testing.T) {
	st := FromReader(strings.NewReader("a = 1\nb = 2\n"), ModeFile)
	var got []token.Type
	for i := 0; i < 50; i++ {
		tk := st.Get()
		got = append(got, tk.Kind)
		if tk.Kind == token.ENDMARKER || tk.Kind == token.ERRORTOKEN {
			break
		}
	}
	if len(got) < 5 {
		t.Fatalf("short token stream: %v", got)
	}
}
