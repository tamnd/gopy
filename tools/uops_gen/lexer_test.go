// Lexer parity tests. Each fixture's expected output was captured from
// running CPython's Tools/cases_generator/lexer.py on the same source
// text and printing the Token __repr__ for every emitted token. The
// Go lexer's repr format (line:beginCol[:endCol] | line:beginCol,
// endLine:endCol) matches CPython's so the assertions are literal
// string equality. If the upstream lexer.py changes shape, regenerate
// these fixtures by re-running the snippet in the repo root README.
//
// CPython: Tools/cases_generator/lexer.py:285-291 Token.__repr__

package main

import (
	"fmt"
	"strings"
	"testing"
)

// reprPy mirrors Python's repr() on a string literal. Go's %q
// uses double quotes always; Python uses single quotes unless the
// string contains a single quote, in which case double. Token.text
// values in our fixtures contain newline / backslash / double-quote
// but never single-quote, so always-single is fine for the cases
// tested here.
func reprPy(t Token) string {
	q := pyRepr(t.Text)
	if t.Begin.Line == t.End.Line {
		return fmt.Sprintf("%s(%s, %d:%d:%d)", t.Kind, q, t.Begin.Line, t.Begin.Col, t.End.Col)
	}
	return fmt.Sprintf("%s(%s, %d:%d, %d:%d)", t.Kind, q, t.Begin.Line, t.Begin.Col, t.End.Line, t.End.Col)
}

// pyRepr renders s the way Python's repr() does for a single-quoted
// string. Only the escapes our fixtures actually exercise are
// supported: backslash, newline, single quote (escapes to \'), and
// printable ASCII passthrough.
func pyRepr(s string) string {
	var sb strings.Builder
	sb.WriteByte('\'')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\':
			sb.WriteString(`\\`)
		case '\n':
			sb.WriteString(`\n`)
		case '\t':
			sb.WriteString(`\t`)
		case '\'':
			sb.WriteString(`\'`)
		default:
			sb.WriteByte(c)
		}
	}
	sb.WriteByte('\'')
	return sb.String()
}

// runFixture lexes src and returns the joined Python-style repr of
// every emitted token. The newline tokens never reach the output
// stream (matching tokenize() in lexer.py), so the result has one
// repr per line and no trailing whitespace.
func runFixture(t *testing.T, src string) string {
	t.Helper()
	tokens, err := Tokenize(src, "t.c", 0)
	if err != nil {
		t.Fatalf("Tokenize: %v", err)
	}
	parts := make([]string, len(tokens))
	for i, tok := range tokens {
		parts[i] = reprPy(tok)
	}
	return strings.Join(parts, "\n")
}

// TestLex_Basic covers the simplest C statement: keyword, identifier,
// equals operator, number, semicolon. Reference captured from
// lexer.py.
func TestLex_Basic(t *testing.T) {
	got := runFixture(t, "int x = 42;")
	want := strings.Join([]string{
		"INT('int', 1:1:4)",
		"IDENTIFIER('x', 1:5:6)",
		"EQUALS('=', 1:7:8)",
		"NUMBER('42', 1:9:11)",
		"SEMI(';', 1:11:12)",
	}, "\n")
	if got != want {
		t.Errorf("mismatch:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

// TestLex_HexAndOctal confirms the master alternation tries octal and
// hex before decimal: 0xDEADBEEF must lex as one NUMBER (not 0 then
// xDEADBEEF), and 0177 must lex as octal (not as decimal 0 then 177).
func TestLex_HexAndOctal(t *testing.T) {
	got := runFixture(t, "0xDEADBEEF + 0177")
	want := strings.Join([]string{
		"NUMBER('0xDEADBEEF', 1:1:11)",
		"PLUS('+', 1:12:13)",
		"NUMBER('0177', 1:14:18)",
	}, "\n")
	if got != want {
		t.Errorf("mismatch:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

// TestLex_Floats covers every float shape lexer.py recognizes:
// digits.digits, digits-with-trailing-dot, dot-with-fraction,
// integer-with-exponent, fraction-with-signed-exponent.
func TestLex_Floats(t *testing.T) {
	got := runFixture(t, "3.14 1. .5 1e10 .5e-3")
	want := strings.Join([]string{
		"NUMBER('3.14', 1:1:5)",
		"NUMBER('1.', 1:6:8)",
		"NUMBER('.5', 1:9:11)",
		"NUMBER('1e10', 1:12:16)",
		"NUMBER('.5e-3', 1:17:22)",
	}, "\n")
	if got != want {
		t.Errorf("mismatch:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

// TestLex_StringEscape verifies the master alternation does not split
// a string literal on its embedded escape sequence: "hello\nworld"
// is one STRING token, not three.
func TestLex_StringEscape(t *testing.T) {
	got := runFixture(t, `"hello\nworld"`)
	want := `STRING('"hello\\nworld"', 1:1:15)`
	if got != want {
		t.Errorf("mismatch:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

// TestLex_LongOperatorsBeforeShort confirms that <<= and >>= win over
// <<, >>, =; that ++ and -- win over +, -; and that -> wins over -.
// The alternation order in operators[] enforces this.
func TestLex_LongOperatorsBeforeShort(t *testing.T) {
	got := runFixture(t, "<<= >>= == != ++ -- ->")
	want := strings.Join([]string{
		"LSHIFTEQUAL('<<=', 1:1:4)",
		"RSHIFTEQUAL('>>=', 1:5:8)",
		"EQ('==', 1:9:11)",
		"NE('!=', 1:12:14)",
		"PLUSPLUS('++', 1:15:17)",
		"MINUSMINUS('--', 1:18:20)",
		"ARROW('->', 1:21:23)",
	}, "\n")
	if got != want {
		t.Errorf("mismatch:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

// TestLex_AnnotationAndDSLKeywords confirms that "pure" is classified
// as ANNOTATION (not IDENTIFIER), "inst" as INST (DSL keyword), and
// the surrounding C punctuation is recognized correctly.
func TestLex_AnnotationAndDSLKeywords(t *testing.T) {
	got := runFixture(t, "pure inst(FOO, (a -- b)) { x = a; }")
	want := strings.Join([]string{
		"ANNOTATION('pure', 1:1:5)",
		"INST('inst', 1:6:10)",
		"LPAREN('(', 1:10:11)",
		"IDENTIFIER('FOO', 1:11:14)",
		"COMMA(',', 1:14:15)",
		"LPAREN('(', 1:16:17)",
		"IDENTIFIER('a', 1:17:18)",
		"MINUSMINUS('--', 1:19:21)",
		"IDENTIFIER('b', 1:22:23)",
		"RPAREN(')', 1:23:24)",
		"RPAREN(')', 1:24:25)",
		"LBRACE('{', 1:26:27)",
		"IDENTIFIER('x', 1:28:29)",
		"EQUALS('=', 1:30:31)",
		"IDENTIFIER('a', 1:32:33)",
		"SEMI(';', 1:33:34)",
		"RBRACE('}', 1:35:36)",
	}, "\n")
	if got != want {
		t.Errorf("mismatch:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

// TestLex_MacroIfElseEndif confirms the # leader splits into
// CMACRO_IF / CMACRO_ELSE / CMACRO_ENDIF correctly, that the trailing
// newline is consumed into the macro's text, and that line/column
// tracking carries through (next token starts at column 0 right after
// the macro, matching lexer.py's linestart = end quirk).
func TestLex_MacroIfElseEndif(t *testing.T) {
	src := "#if PY_BIG_ENDIAN\nint x;\n#endif\n"
	got := runFixture(t, src)
	want := strings.Join([]string{
		`CMACRO_IF('#if PY_BIG_ENDIAN\n', 1:1, 2:0)`,
		"INT('int', 2:0:3)",
		"IDENTIFIER('x', 2:4:5)",
		"SEMI(';', 2:5:6)",
		`CMACRO_ENDIF('#endif\n', 3:1, 4:0)`,
	}, "\n")
	if got != want {
		t.Errorf("mismatch:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

// TestLex_LineComment exercises a // comment followed by code on the
// next line. Verifies the comment ends at the newline (no embedded
// linefeed) and that line counter advances.
func TestLex_LineComment(t *testing.T) {
	got := runFixture(t, "// line cmt\nint x;")
	want := strings.Join([]string{
		"COMMENT('// line cmt', 1:1:12)",
		"INT('int', 2:1:4)",
		"IDENTIFIER('x', 2:5:6)",
		"SEMI(';', 2:6:7)",
	}, "\n")
	if got != want {
		t.Errorf("mismatch:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

// TestLex_BlockCommentSpansLines exercises /* ... */ across a newline.
// The comment token records begin on line 1 and end on line 2, and
// the next code token resumes on line 3 with column reset.
func TestLex_BlockCommentSpansLines(t *testing.T) {
	got := runFixture(t, "/* multi\nline */\nint x;")
	want := strings.Join([]string{
		`COMMENT('/* multi\nline */', 1:1, 2:8)`,
		"INT('int', 3:1:4)",
		"IDENTIFIER('x', 3:5:6)",
		"SEMI(';', 3:6:7)",
	}, "\n")
	if got != want {
		t.Errorf("mismatch:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

// TestLex_MultilineKeepsColumns walks three statements on three lines
// and confirms each "int" / identifier / semi keeps stable column
// positions; this is the simplest regression test for the line/col
// counter.
func TestLex_MultilineKeepsColumns(t *testing.T) {
	got := runFixture(t, "int a;\nint b;\nint c;")
	want := strings.Join([]string{
		"INT('int', 1:1:4)",
		"IDENTIFIER('a', 1:5:6)",
		"SEMI(';', 1:6:7)",
		"INT('int', 2:1:4)",
		"IDENTIFIER('b', 2:5:6)",
		"SEMI(';', 2:6:7)",
		"INT('int', 3:1:4)",
		"IDENTIFIER('c', 3:5:6)",
		"SEMI(';', 3:6:7)",
	}, "\n")
	if got != want {
		t.Errorf("mismatch:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

// TestLex_AllCKeywords is a survey: every C reserved word the lexer
// classifies as a non-IDENTIFIER kind appears here. The point is to
// catch regressions where adding a new kind on the Go side forgets to
// register the matching keywords entry.
func TestLex_AllCKeywords(t *testing.T) {
	src := "auto break case char const continue default do double else " +
		"enum extern float for goto if inline int long offsetof " +
		"restrict return short signed sizeof static struct switch " +
		"typedef union unsigned void volatile while inst op macro " +
		"label spilled"
	tokens, err := Tokenize(src, "t.c", 0)
	if err != nil {
		t.Fatalf("Tokenize: %v", err)
	}
	wantKinds := []string{
		TokAuto, TokBreak, TokCase, TokChar, TokConst, TokContinue,
		TokDefault, TokDo, TokDouble, TokElse, TokEnum, TokExtern,
		TokFloat, TokFor, TokGoto, TokIf, TokInline, TokInt, TokLong,
		TokOffsetof, TokRestrict, TokReturn, TokShort, TokSigned,
		TokSizeof, TokStatic, TokStruct, TokSwitch, TokTypedef,
		TokUnion, TokUnsigned, TokVoid, TokVolatile, TokWhile,
		TokInst, TokOp, TokMacro, TokLabel, TokSpilled,
	}
	if len(tokens) != len(wantKinds) {
		t.Fatalf("got %d tokens, want %d", len(tokens), len(wantKinds))
	}
	for i, w := range wantKinds {
		if tokens[i].Kind != w {
			t.Errorf("token %d: kind = %s, want %s", i, tokens[i].Kind, w)
		}
	}
}

// TestLex_BadTokenReportsSyntaxError feeds a stray backtick (not part
// of any pattern; the catch-all \S grabs it) and confirms tokenize
// returns a SyntaxError rather than silently dropping it.
func TestLex_BadTokenReportsSyntaxError(t *testing.T) {
	_, err := Tokenize("int x = `;", "t.c", 0)
	if err == nil {
		t.Fatal("Tokenize: want error for bad token")
	}
	se, ok := err.(*SyntaxError)
	if !ok {
		t.Fatalf("err = %T, want *SyntaxError", err)
	}
	if !strings.Contains(se.Message, "Bad token") {
		t.Errorf("message = %q, want contains 'Bad token'", se.Message)
	}
}

// TestLex_AllAnnotations confirms each annotation identifier in the
// upstream set lands on TokAnnotation rather than TokIdentifier.
func TestLex_AllAnnotations(t *testing.T) {
	for name := range annotations {
		tokens, err := Tokenize(name, "t.c", 0)
		if err != nil {
			t.Errorf("Tokenize(%q): %v", name, err)
			continue
		}
		if len(tokens) != 1 || tokens[0].Kind != TokAnnotation {
			t.Errorf("Tokenize(%q): got %v, want one ANNOTATION", name, tokens)
		}
	}
}
