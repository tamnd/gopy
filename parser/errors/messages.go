// Package errors holds the SyntaxError text panel and the helpers
// that turn (parser, token, message) into a structured *SyntaxError.
//
// The constants below mirror the message strings emitted from
// cpython/Parser/pegen_errors.c. CPython freezes parser error text
// per release, so the goal here is byte-for-byte parity with 3.14:
// when a Python user feeds gopy a broken program, the SyntaxError
// they see should be indistinguishable from CPython's.
//
// CPython: Parser/pegen_errors.c
package errors

// MsgInvalidSyntax is the generic fallback raised when no rule in
// the second-pass invalid grammar matches.
//
// CPython: Parser/pegen_errors.c:450 RAISE_SYNTAX_ERROR_KNOWN_LOCATION
const MsgInvalidSyntax = "invalid syntax"

// MsgUnexpectedEOF fires when the parser hits EOF mid-statement and
// the lexer is not inside an unmatched paren.
//
// CPython: Parser/pegen_errors.c:88 _Pypegen_tokenizer_error E_EOF case
const MsgUnexpectedEOF = "unexpected EOF while parsing"

// MsgUnexpectedIndent / MsgUnexpectedUnindent surface lexer-level
// indentation transitions that the grammar cannot accept.
//
// CPython: Parser/pegen_errors.c:441 _Pypegen_set_syntax_error
const (
	MsgUnexpectedIndent   = "unexpected indent"
	MsgUnexpectedUnindent = "unexpected unindent"
)

// MsgInconsistentDedent is raised when a DEDENT does not line up
// with any prior INDENT level.
//
// CPython: Parser/pegen_errors.c:92 E_DEDENT case
const MsgInconsistentDedent = "unindent does not match any outer indentation level"

// MsgTabSpace is the TabError message for mixed tabs and spaces.
//
// CPython: Parser/pegen_errors.c:104 E_TABSPACE case
const MsgTabSpace = "inconsistent use of tabs and spaces in indentation"

// MsgTooDeep is the IndentationError raised at maxIndent.
//
// CPython: Parser/pegen_errors.c:108 E_TOODEEP case
const MsgTooDeep = "too many levels of indentation"

// MsgInvalidToken is the catch-all for E_TOKEN.
//
// CPython: Parser/pegen_errors.c:82 E_TOKEN case
const MsgInvalidToken = "invalid token"

// MsgLineCont fires when a backslash-newline is followed by
// something other than the next physical line.
//
// CPython: Parser/pegen_errors.c:112 E_LINECONT case
const MsgLineCont = "unexpected character after line continuation character"

// MsgColumnOverflow is the OverflowError raised when a source line
// exceeds the column-offset range the parser tracks.
//
// CPython: Parser/pegen_errors.c:117 E_COLUMNOVERFLOW case
const MsgColumnOverflow = "Parser column offset overflow - source line is too big"

// MsgUnknownParseError is the default-arm fallback in
// _Pypegen_tokenizer_error when no E_* code matches.
//
// CPython: Parser/pegen_errors.c:120 default case
const MsgUnknownParseError = "unknown parsing error"

// MsgUnclosedParen is the "'%c' was never closed" template.
// The single byte is the offending opener (one of '(', '[', '{').
//
// CPython: Parser/pegen_errors.c:65 raise_unclosed_parentheses_error
const MsgUnclosedParen = "'%c' was never closed"

// MsgErrorAtStart is raised when fill is zero and we already need
// to surface a SyntaxError. The location is (0, 0).
//
// CPython: Parser/pegen_errors.c:428 _Pypegen_set_syntax_error
const MsgErrorAtStart = "error at start before reading any input"

// MsgStackOverflow is the MemoryError raised when the recursion
// limit guard trips inside the generated parser.
//
// CPython: Parser/pegen_errors.c:460 _Pypegen_stack_overflow
const MsgStackOverflow = "Parser stack overflowed - Python source too complex to parse"

// MsgDecodeError templates. The %s slot carries "unicode error" or
// "value error"; the trailing %s carries the underlying message.
//
// CPython: Parser/pegen_errors.c:148 _Pypegen_raise_decode_error
const (
	MsgDecodeWrap    = "(%s) %s"
	MsgDecodeUnknown = "(%s) unknown error"
)

// MsgExpected family. The forced-expect machinery in pegen.c uses
// MsgExpectedTok for single-character expectations and MsgExpected
// for multi-token alternatives.
//
// CPython: Parser/pegen.c expect_forced_token
const (
	MsgExpectedTok = "expected '%s'"
	MsgExpected    = "expected (%s)"
)

// Numeric and string literal complaints raised from the
// post-tokenization pass.
//
// CPython: Parser/string_parser.c and Parser/action_helpers.c
const (
	MsgMixedBytesLiterals    = "cannot mix bytes and nonbytes literals"
	MsgImaginaryRequired     = "imaginary number required in complex literal"
	MsgRealRequired          = "real number required in complex literal"
	MsgNumericUnderscore     = "Underscores in numeric literals are only supported in Python 3.6 and greater"
	MsgBarryAsBDFL           = "with Barry as BDFL, use '<>' instead of '!='"
	MsgMultipleStmtsInteract = "multiple statements found while compiling a single statement"
)

// Assignment-target diagnostics. The %s slot carries a phrase like
// "literal", "function call", or "f-string expression".
//
// CPython: Parser/action_helpers.c _PyPegen_get_invalid_target
const (
	MsgCannotAssignTo       = "cannot assign to %s"
	MsgCannotDelete         = "cannot delete %s"
	MsgCannotAssignToWalrus = "cannot use assignment expressions with %s"
)

// Walrus / star / yield placement errors.
//
// CPython: Parser/parser.c invalid_* rules
const (
	MsgWalrusInComp          = "assignment expression cannot be used in a comprehension iterable expression"
	MsgStarOutsideFunction   = "can't use starred expression here"
	MsgYieldOutsideFunction  = "'yield' outside function"
	MsgReturnOutsideFunction = "'return' outside function"
	MsgAwaitOutsideFunction  = "'await' outside function"
	MsgAwaitOutsideAsync     = "'await' outside async function"
)

// f-string and t-string structural errors. CPython's tokenizer surfaces
// most of these via lexer_error; the parser side wraps them.
//
// CPython: Parser/lexer/lexer.c tok_get_fstring_mode
const (
	MsgFStringExprEmpty     = "f-string: empty expression not allowed"
	MsgFStringExprBackslash = "f-string expression part cannot include a backslash"
	MsgFStringExprComment   = "f-string expression part cannot include '#'"
	MsgFStringSingleBrace   = "f-string: single '}' is not allowed"
	MsgFStringUnterminated  = "f-string: unterminated string"
	MsgFStringExprNesting   = "f-string: expressions nested too deeply"
	MsgFStringInvalidConv   = "f-string: invalid conversion character %c: expected 's', 'r', or 'a'"
	MsgFStringMissingRBrace = "f-string: expecting '}'"
	MsgTStringExprEmpty     = "t-string: empty expression not allowed"
	MsgTStringExprBackslash = "t-string expression part cannot include a backslash"
	MsgTStringExprComment   = "t-string expression part cannot include '#'"
	MsgTStringSingleBrace   = "t-string: single '}' is not allowed"
	MsgTStringUnterminated  = "t-string: unterminated string"
)

// Suggestion-style hints that pegen emits inline. The richer
// "did you mean ...?" surface lives in errors/suggest (1611).
//
// CPython: Parser/pegen.c suggestion arms
const (
	MsgDidYouMeanWalrus = "did you mean ':='?"
	MsgPrintParens      = "Missing parentheses in call to 'print'. Did you mean print(...)?"
	MsgExecParens       = "Missing parentheses in call to 'exec'. Did you mean exec(...)?"
)

// Function/class signature errors raised from the invalid_ rules.
//
// CPython: Parser/parser.c invalid_arguments / invalid_parameters
const (
	MsgPositionalAfterStar     = "positional argument follows keyword argument"
	MsgPositionalAfterUnpack   = "positional argument follows keyword argument unpacking"
	MsgIterableUnpackInComp    = "iterable unpacking cannot be used in comprehension"
	MsgGeneratorInCallNoParens = "Generator expression must be parenthesized"
	MsgArgsAfterStarStar       = "argument cannot follow '**' expansion"
	MsgKeywordExpression       = "expression cannot contain assignment, perhaps you meant \"==\"?"
	MsgDuplicateArgument       = "duplicate argument '%s' in function definition"
	MsgNonDefaultAfterDefault  = "non-default argument follows default argument"
	MsgStarAfterStar           = "* argument may appear only once"
	MsgNamedExprWithoutTarget  = "named expression must be parenthesized in this context"
)

// match-statement errors. PEP 634 added the structural-pattern
// matching diagnostics surfaced from the invalid_pattern rules.
//
// CPython: Parser/parser.c invalid_match_stmt
const (
	MsgMatchSubjectMustEnd        = "expected ':'"
	MsgPatternCaptureClassPattern = "patterns may only match attributes (got %s)"
	MsgMatchClassDoubleKeyword    = "attribute name repeated in class pattern: %s"
	MsgMatchMappingDoubleKey      = "mapping pattern checks duplicate key (%s)"
	MsgMatchStarPlacement         = "starred pattern must end the sequence"
	MsgMatchStarMultiple          = "multiple starred names in sequence pattern"
)

// import / from-import diagnostics.
//
// CPython: Parser/parser.c invalid_import_from
const (
	MsgImportFromStarLevel = "from __future__ imports must occur at the beginning of the file"
	MsgImportTrailingComma = "trailing comma not allowed without surrounding parentheses"
)
