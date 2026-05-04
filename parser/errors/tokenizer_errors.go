// CPython: Parser/pegen_errors.c:69 _Pypegen_tokenizer_error and
// :414 _Pypegen_set_syntax_error. The dispatch maps lexer errcode
// values onto the SyntaxError text panel above.

package errors

// Errcode is the lexer-side done state. Values match
// Parser/lexer/state.h's E_* family in order. The lexer package
// keeps its own typed alias; this one lets the errors package
// stay independent of the lexer import graph.
type Errcode int

// Errcode values. The order matches lexer.errCode so a numeric
// cast is safe both ways.
const (
	ErrOK Errcode = iota
	ErrEOF
	ErrIntr
	ErrToken
	ErrSyntax
	ErrIndent
	ErrDedent
	ErrTabSpace
	ErrOverflow
	ErrDecode
	ErrEOFS
	ErrEOLS
	ErrErrLine
	ErrBadVisibility
	ErrEncoding
	ErrColumnOverflow
)

// FromTokenizer maps a lexer errcode into the matching SyntaxError
// record. The (lineno, col) pair comes from the tokenizer; the
// filename is forwarded from the Parser. Returns nil for codes that
// do not raise a SyntaxError on their own (E_OK, E_EOF when level
// is zero is handled by the caller, etc.).
//
// CPython: Parser/pegen_errors.c:69 _Pypegen_tokenizer_error switch
func FromTokenizer(code Errcode, filename string, pos Pos) *SyntaxError {
	switch code {
	case ErrToken:
		return Raise(filename, pos, MsgInvalidToken)
	case ErrDedent:
		return RaiseIndent(filename, pos, MsgInconsistentDedent)
	case ErrTabSpace:
		return RaiseTab(filename, pos, MsgTabSpace)
	case ErrOverflow:
		return RaiseIndent(filename, pos, MsgTooDeep)
	case ErrColumnOverflow:
		return Build(KindOverflow, filename, pos, MsgColumnOverflow)
	case ErrErrLine:
		return Raise(filename, pos, MsgLineCont)
	default:
		return Raise(filename, pos, MsgUnknownParseError)
	}
}

// UnclosedParen is the dedicated builder for the lexer's level>0
// EOF arm. The caller already knows which paren character was open.
//
// CPython: Parser/pegen_errors.c:60 raise_unclosed_parentheses_error
func UnclosedParen(filename string, pos Pos, opener byte) *SyntaxError {
	return Raise(filename, pos, MsgUnclosedParen, opener)
}
