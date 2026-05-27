package _warnings

import (
	"strings"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/parser/lexer"
	parsererrors "github.com/tamnd/gopy/parser/errors"
)

// init wires the package-level hooks in parser/lexer and compile so
// their SyntaxWarning diagnostics get drained through the real warnings
// filter. The indirection keeps parser/lexer and compile leaf while
// still letting the runtime route through PyErr_WarnExplicit.
//
// CPython: Parser/tokenizer/helpers.c:152 _PyTokenizer_parser_warn
// CPython: Python/compile.c:237 _PyCompile_Warn
func init() {
	lexer.WarnHook = FlushLexerWarnings
	compile.WarnHook = compileWarn
}

// FlushLexerWarnings posts every SyntaxWarning-class diagnostic the
// lexer recorded as a real PyErr_WarnExplicit call. Returns the first
// error produced by the filter (a warning elevated to SyntaxError).
// CPython does this inline from _PyTokenizer_parser_warn (helpers.c:152);
// gopy stashes the entries on State.warnings to keep parser/lexer leaf,
// then drains them through this hook once tokenization is complete.
//
// When the filter elevates a warning to an error, CPython promotes the
// exception to a SyntaxError via _PyTokenizer_syntaxerror which calls
// _syntaxerror_range with col_offset = len(text-from-line-start-to-cur).
// gopy mirrors this by returning a structured *parsererrors.SyntaxError
// so synthesizeException can populate all SyntaxError attributes.
// col_offset in CPython == w.Pos.Col (s.col at warning time, which is
// one past the 0-based column); parsererrors.Pos.ColOff is 0-indexed, so
// ColOff = w.Pos.Col - 1 matches CPython's SyntaxError.offset = col_offset.
//
// CPython: Parser/tokenizer/helpers.c:152 _PyTokenizer_parser_warn
// CPython: Parser/tokenizer/helpers.c:11 _syntaxerror_range
func FlushLexerWarnings(filename string, warns []lexer.SyntaxError) error {
	for _, w := range warns {
		cat := warningCategory(w.Category)
		if err := WarnExplicitWithSourceline(cat, w.Message, filename, int64(w.Pos.Line), "", w.Text, nil); err != nil {
			// Elevation: convert to a structured SyntaxError so the VM can
			// populate filename/lineno/offset/text on the Python exception.
			// ColOff = w.Pos.Col - 1 because w.Pos.Col = s.col which is
			// one ahead of the 0-based column index.
			colOff := w.Pos.Col - 1
			if colOff < 0 {
				colOff = 0
			}
			// CPython uses a shorter message for the SyntaxError case than for the
			// SyntaxWarning: it omits "Such sequences will not work in the future."
			// (Parser/string_parser.c:101-111 vs :30-41). Mirror that by stripping
			// the sentence before building the SyntaxError.
			errMsg := strings.ReplaceAll(w.Message, "Such sequences will not work in the future. ", "")
			return &parsererrors.SyntaxError{
				Kind: parsererrors.KindSyntax,
				Pos: parsererrors.Pos{
					Lineno:  w.Pos.Line,
					ColOff:  colOff,
					EndLine: w.EndPos.Line,
					EndCol:  colOff,
				},
				Filename: filename,
				Message:  errMsg,
				Text:     w.Text,
			}
		}
	}
	return nil
}

// compileWarn is the compile.WarnHook implementation. It maps the
// category name to the right Warning subclass and calls WarnExplicit.
// If the filter elevates the warning to an error, WarnExplicit returns
// a non-nil error which propagates as a SyntaxError from the compiler.
//
// CPython: Python/compile.c:237 _PyCompile_Warn (PyErr_WarnExplicitObject call)
func compileWarn(category, message, filename string, lineno int) error {
	cat := warningCategory(category)
	return WarnExplicit(cat, message, filename, int64(lineno), "", nil)
}

// warningCategory maps the lexer's category name to the corresponding
// built-in Warning subclass. Unknown names fall back to Warning so the
// filter still gets a chance to surface them.
func warningCategory(name string) *objects.Type {
	switch name {
	case "SyntaxWarning":
		return errors.PyExc_SyntaxWarning
	case "DeprecationWarning":
		return errors.PyExc_DeprecationWarning
	}
	return errors.PyExc_Warning
}
