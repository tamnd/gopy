package _warnings

import (
	"github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/parser/lexer"
)

// init wires the package-level hook in parser/lexer so the SyntaxWarning
// diagnostics the lexer stashed on State.warnings get drained through
// the real warnings filter. The indirection keeps parser/lexer leaf
// while still letting the runtime route through PyErr_WarnExplicit.
//
// CPython: Parser/tokenizer/helpers.c:152 _PyTokenizer_parser_warn
// (the PyErr_WarnExplicitObject(category, msg, tok->filename,
// tok->lineno, NULL, NULL) call).
func init() {
	lexer.WarnHook = FlushLexerWarnings
}

// FlushLexerWarnings posts every SyntaxWarning-class diagnostic the
// lexer recorded as a real PyErr_WarnExplicit call. CPython does this
// inline from _PyTokenizer_parser_warn (helpers.c:152); gopy stashes
// the entries on State.warnings to keep parser/lexer leaf, then drains
// them through this hook once tokenization is complete.
//
// CPython: Parser/tokenizer/helpers.c:152 _PyTokenizer_parser_warn
func FlushLexerWarnings(filename string, warns []lexer.SyntaxError) {
	for _, w := range warns {
		cat := warningCategory(w.Category)
		_ = WarnExplicit(cat, w.Message, filename, int64(w.Pos.Line), "", nil)
	}
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
