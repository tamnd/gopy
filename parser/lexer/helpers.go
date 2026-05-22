// CPython: Parser/tokenizer/helpers.c.
//
// Function map (helpers.c → gopy):
//
//	_syntaxerror_range                       → syntaxError (inlined)
//	_PyTokenizer_syntaxerror                 → syntaxError
//	_PyTokenizer_syntaxerror_known_range     → syntaxErrorKnownRange
//	_PyTokenizer_indenterror                 → indentError
//	_PyTokenizer_error_ret                   → errorRet
//	_PyTokenizer_warn_invalid_escape_sequence→ warnInvalidEscape
//	_PyTokenizer_parser_warn                 → parserWarn
//	_PyTokenizer_new_string                  → newString
//	_PyTokenizer_translate_into_utf8         → translateIntoUTF8
//	_PyTokenizer_translate_newlines          → source.go NormalizeNewlines
//	_PyTokenizer_check_bom                   → source.go CheckBOMCookieConflict
//	get_normal_name                          → source.go normalizeEncodingName
//	get_coding_spec                          → source.go matchCodingCookie
//	_PyTokenizer_check_coding_spec           → source.go DetectEncodingCookie
//	valid_utf8                               → source.go ValidateUTF8 (predicate)
//	_PyTokenizer_ensure_utf8                 → source.go ValidateUTF8
//	_PyTokenizer_print_escape                → printEscape (debug)
//	_PyTokenizer_tok_dump                    → tokDump (debug)

package lexer

import (
	"fmt"
	"strings"

	"github.com/tamnd/gopy/token"
)

// syntaxError records a SyntaxError at the current cursor position and
// returns an ERRORTOKEN. Mirrors _PyTokenizer_syntaxerror, which uses
// the current line range as the error location.
//
// CPython: Parser/tokenizer/helpers.c:66 _PyTokenizer_syntaxerror
func (s *State) syntaxError(format string, args ...any) Tok {
	s.recordError(fmt.Sprintf(format, args...))
	s.done = eSyntax
	return s.tokenSetup(token.ERRORTOKEN, s.cur, s.cur)
}

// syntaxErrorKnownRange records a SyntaxError pinned to a specific
// column range. The PEG layer uses this when it has already consumed
// the bad token and can name the exact span.
//
// CPython: Parser/tokenizer/helpers.c:77 _PyTokenizer_syntaxerror_known_range
func (s *State) syntaxErrorKnownRange(startCol, endCol int, format string, args ...any) Tok {
	s.recordError(fmt.Sprintf(format, args...))
	if s.err != nil {
		s.err.Pos.Col = startCol
		s.err.EndPos.Col = endCol
	}
	s.done = eSyntax
	return s.tokenSetup(token.ERRORTOKEN, s.cur, s.cur)
}

// indentError flags an inconsistent-tab/space situation. Mirrors the
// E_TABSPACE branch.
//
// CPython: Parser/tokenizer/helpers.c:88 _PyTokenizer_indenterror
func (s *State) indentError() Tok {
	s.done = eTabSpace
	s.cur = s.inp
	return s.tokenSetup(token.ERRORTOKEN, s.cur, s.cur)
}

// warnInvalidEscape mirrors the deprecation warning the C tokenizer
// raises for unrecognized \X escapes inside string literals. CPython
// routes this through PyErr_WarnExplicitObject(PyExc_SyntaxWarning),
// and when the warnings filter elevates the warning to an error,
// surfaces a SyntaxError with a slightly shorter message. gopy stashes
// the entry via parserWarn; State.FlushWarnings() hands it to the
// runtime WarnHook (set by module/_warnings.init) once tokenization
// is over so the actual PyErr_WarnExplicit call runs there.
//
// CPython: Parser/tokenizer/helpers.c:110 _PyTokenizer_warn_invalid_escape_sequence
func (s *State) warnInvalidEscape(c byte) {
	if !s.reportWarnings {
		return
	}
	s.parserWarn("SyntaxWarning",
		"\"\\%c\" is an invalid escape sequence. "+
			"Such sequences will not work in the future. "+
			"Did you mean \"\\\\%c\"? A raw string is also an option.",
		c, c)
}

// errorRet returns the canonical ERRORTOKEN value the C lexer hands
// back when something has gone wrong. CPython tags `tok->cur = tok->inp`
// before returning so the dispatch loop stops; gopy does the same.
//
// CPython: Parser/tokenizer/helpers.c:97 _PyTokenizer_error_ret
func (s *State) errorRet() Tok {
	s.cur = s.inp
	if s.done == eOK {
		s.done = eSyntax
	}
	return s.tokenSetup(token.ERRORTOKEN, s.cur, s.cur)
}

// parserWarn records a SyntaxWarning-class diagnostic. CPython hands
// this to PyErr_WarnExplicitObject so the warnings filter can decide
// to ignore, log, or escalate; gopy stashes it on s.warnings and
// drains via State.FlushWarnings() / lexer.WarnHook, which the
// runtime (module/_warnings.init) wires to a real
// PyErr_WarnExplicit call. parser.runParse drains at end-of-parse;
// module/_tokenize drains per-token so iterator consumers see the
// warning between Next() calls.
//
// CPython: Parser/tokenizer/helpers.c:153 _PyTokenizer_parser_warn
func (s *State) parserWarn(category, format string, args ...any) {
	if !s.reportWarnings {
		return
	}
	// CPython's _showwarnmsg fetches the source line from linecache
	// (Lib/warnings.py:153 _formatwarnmsg_impl). gopy stashes the line
	// here so FlushWarnings can hand it directly to the warnings filter,
	// matching the same display even when warnings.py has not loaded yet.
	w := SyntaxError{
		Pos:      Pos{Line: s.lineno, Col: s.col},
		EndPos:   Pos{Line: s.lineno, Col: s.col},
		Message:  fmt.Sprintf(format, args...),
		Text:     nthLine(s.buf, s.lineno),
		Category: category,
	}
	s.warnings = append(s.warnings, w)
}

// newString duplicates a byte range into an owned string. CPython
// allocates with PyMem_Malloc and copies; in Go the conversion does
// both in one step.
//
// CPython: Parser/tokenizer/helpers.c:191 _PyTokenizer_new_string
func newString(src []byte) string {
	return string(src)
}

// translateIntoUTF8 recodes a string from src encoding into UTF-8.
// CPython delegates to PyUnicode_AsEncodedString. gopy currently only
// supports utf-8 source so the function is a pass-through; non-utf-8
// inputs are rejected earlier by check_coding_spec.
//
// CPython: Parser/tokenizer/helpers.c:204 _PyTokenizer_translate_into_utf8
func translateIntoUTF8(src, enc string) (string, error) {
	if enc == "" || strings.EqualFold(enc, "utf-8") || strings.EqualFold(enc, "utf8") {
		return src, nil
	}
	return "", fmt.Errorf("unsupported source encoding: %s", enc)
}

// printEscape writes a debug rendering of src to a string. CPython
// targets a FILE*; we return the formatted string so callers can
// route it however they please. Used only by tokDump.
//
// CPython: Parser/tokenizer/helpers.c:548 _PyTokenizer_print_escape
func printEscape(src []byte) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, c := range src {
		switch c {
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		default:
			if c < 0x20 || c >= 0x7f {
				fmt.Fprintf(&b, `\x%02x`, c)
			} else {
				b.WriteByte(c)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// tokDump returns the debug one-liner describing a token kind +
// payload, matching the format CPython emits when TOKENIZER_DUMP is
// enabled. Pure debug helper.
//
// CPython: Parser/tokenizer/helpers.c:575 _PyTokenizer_tok_dump
func tokDump(kind token.Type, payload []byte) string {
	return fmt.Sprintf("%-10s %s", kind.String(), printEscape(payload))
}
