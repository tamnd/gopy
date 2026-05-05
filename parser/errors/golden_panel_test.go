// Golden panel: every message constant gets an exact-string check.
// New constants must add a row here; that's the point. The list is
// alphabetical by constant name so diffs stay readable.
//
// CPython: Parser/pegen_errors.c, Parser/parser.c invalid_* rules

package errors

import "testing"

func TestGoldenMessagePanel(t *testing.T) {
	rows := []struct {
		name string
		got  string
		want string
	}{
		{"MsgArgsAfterStarStar", MsgArgsAfterStarStar, "argument cannot follow '**' expansion"},
		{"MsgAwaitOutsideAsync", MsgAwaitOutsideAsync, "'await' outside async function"},
		{"MsgAwaitOutsideFunction", MsgAwaitOutsideFunction, "'await' outside function"},
		{"MsgBarryAsBDFL", MsgBarryAsBDFL, "with Barry as BDFL, use '<>' instead of '!='"},
		{"MsgCannotAssignTo", MsgCannotAssignTo, "cannot assign to %s"},
		{"MsgCannotAssignToWalrus", MsgCannotAssignToWalrus, "cannot use assignment expressions with %s"},
		{"MsgCannotDelete", MsgCannotDelete, "cannot delete %s"},
		{"MsgColumnOverflow", MsgColumnOverflow, "Parser column offset overflow - source line is too big"},
		{"MsgDecodeUnknown", MsgDecodeUnknown, "(%s) unknown error"},
		{"MsgDecodeWrap", MsgDecodeWrap, "(%s) %s"},
		{"MsgDidYouMeanWalrus", MsgDidYouMeanWalrus, "did you mean ':='?"},
		{"MsgDuplicateArgument", MsgDuplicateArgument, "duplicate argument '%s' in function definition"},
		{"MsgErrorAtStart", MsgErrorAtStart, "error at start before reading any input"},
		{"MsgExecParens", MsgExecParens, "Missing parentheses in call to 'exec'. Did you mean exec(...)?"},
		{"MsgExpected", MsgExpected, "expected (%s)"},
		{"MsgExpectedTok", MsgExpectedTok, "expected '%s'"},
		{"MsgFStringExprBackslash", MsgFStringExprBackslash, "f-string expression part cannot include a backslash"},
		{"MsgFStringExprComment", MsgFStringExprComment, "f-string expression part cannot include '#'"},
		{"MsgFStringExprEmpty", MsgFStringExprEmpty, "f-string: empty expression not allowed"},
		{"MsgFStringExprNesting", MsgFStringExprNesting, "f-string: expressions nested too deeply"},
		{"MsgFStringInvalidConv", MsgFStringInvalidConv, "f-string: invalid conversion character %c: expected 's', 'r', or 'a'"},
		{"MsgFStringMissingRBrace", MsgFStringMissingRBrace, "f-string: expecting '}'"},
		{"MsgFStringSingleBrace", MsgFStringSingleBrace, "f-string: single '}' is not allowed"},
		{"MsgFStringUnterminated", MsgFStringUnterminated, "f-string: unterminated string"},
		{"MsgGeneratorInCallNoParens", MsgGeneratorInCallNoParens, "Generator expression must be parenthesized"},
		{"MsgImaginaryRequired", MsgImaginaryRequired, "imaginary number required in complex literal"},
		{"MsgImportFromStarLevel", MsgImportFromStarLevel, "from __future__ imports must occur at the beginning of the file"},
		{"MsgImportTrailingComma", MsgImportTrailingComma, "trailing comma not allowed without surrounding parentheses"},
		{"MsgInconsistentDedent", MsgInconsistentDedent, "unindent does not match any outer indentation level"},
		{"MsgInvalidSyntax", MsgInvalidSyntax, "invalid syntax"},
		{"MsgInvalidToken", MsgInvalidToken, "invalid token"},
		{"MsgIterableUnpackInComp", MsgIterableUnpackInComp, "iterable unpacking cannot be used in comprehension"},
		{"MsgKeywordExpression", MsgKeywordExpression, "expression cannot contain assignment, perhaps you meant \"==\"?"},
		{"MsgLineCont", MsgLineCont, "unexpected character after line continuation character"},
		{"MsgMatchClassDoubleKeyword", MsgMatchClassDoubleKeyword, "attribute name repeated in class pattern: %s"},
		{"MsgMatchMappingDoubleKey", MsgMatchMappingDoubleKey, "mapping pattern checks duplicate key (%s)"},
		{"MsgMatchStarMultiple", MsgMatchStarMultiple, "multiple starred names in sequence pattern"},
		{"MsgMatchStarPlacement", MsgMatchStarPlacement, "starred pattern must end the sequence"},
		{"MsgMatchSubjectMustEnd", MsgMatchSubjectMustEnd, "expected ':'"},
		{"MsgMixedBytesLiterals", MsgMixedBytesLiterals, "cannot mix bytes and nonbytes literals"},
		{"MsgMultipleStmtsInteract", MsgMultipleStmtsInteract, "multiple statements found while compiling a single statement"},
		{"MsgNamedExprWithoutTarget", MsgNamedExprWithoutTarget, "named expression must be parenthesized in this context"},
		{"MsgNonDefaultAfterDefault", MsgNonDefaultAfterDefault, "non-default argument follows default argument"},
		{"MsgNumericUnderscore", MsgNumericUnderscore, "Underscores in numeric literals are only supported in Python 3.6 and greater"},
		{"MsgPatternCaptureClassPattern", MsgPatternCaptureClassPattern, "patterns may only match attributes (got %s)"},
		{"MsgPositionalAfterStar", MsgPositionalAfterStar, "positional argument follows keyword argument"},
		{"MsgPositionalAfterUnpack", MsgPositionalAfterUnpack, "positional argument follows keyword argument unpacking"},
		{"MsgPrintParens", MsgPrintParens, "Missing parentheses in call to 'print'. Did you mean print(...)?"},
		{"MsgRealRequired", MsgRealRequired, "real number required in complex literal"},
		{"MsgReturnOutsideFunction", MsgReturnOutsideFunction, "'return' outside function"},
		{"MsgStackOverflow", MsgStackOverflow, "Parser stack overflowed - Python source too complex to parse"},
		{"MsgStarAfterStar", MsgStarAfterStar, "* argument may appear only once"},
		{"MsgStarOutsideFunction", MsgStarOutsideFunction, "can't use starred expression here"},
		{"MsgTStringExprBackslash", MsgTStringExprBackslash, "t-string expression part cannot include a backslash"},
		{"MsgTStringExprComment", MsgTStringExprComment, "t-string expression part cannot include '#'"},
		{"MsgTStringExprEmpty", MsgTStringExprEmpty, "t-string: empty expression not allowed"},
		{"MsgTStringSingleBrace", MsgTStringSingleBrace, "t-string: single '}' is not allowed"},
		{"MsgTStringUnterminated", MsgTStringUnterminated, "t-string: unterminated string"},
		{"MsgTabSpace", MsgTabSpace, "inconsistent use of tabs and spaces in indentation"},
		{"MsgTooDeep", MsgTooDeep, "too many levels of indentation"},
		{"MsgUnclosedParen", MsgUnclosedParen, "'%c' was never closed"},
		{"MsgUnexpectedEOF", MsgUnexpectedEOF, "unexpected EOF while parsing"},
		{"MsgUnexpectedIndent", MsgUnexpectedIndent, "unexpected indent"},
		{"MsgUnexpectedUnindent", MsgUnexpectedUnindent, "unexpected unindent"},
		{"MsgUnknownParseError", MsgUnknownParseError, "unknown parsing error"},
		{"MsgUnterminatedFString", MsgUnterminatedFString, "unterminated %c-string literal (detected at line %d)"},
		{"MsgUnterminatedString", MsgUnterminatedString, "unterminated string literal (detected at line %d)"},
		{"MsgUnterminatedTripleFStr", MsgUnterminatedTripleFStr, "unterminated triple-quoted %c-string literal (detected at line %d)"},
		{"MsgUnterminatedTripleString", MsgUnterminatedTripleString, "unterminated triple-quoted string literal (detected at line %d)"},
		{"MsgTypeParamsEmpty", MsgTypeParamsEmpty, "Type parameter list cannot be empty"},
		{"MsgTypeVarTupleBound", MsgTypeVarTupleBound, "cannot use bound with TypeVarTuple"},
		{"MsgTypeVarTupleConstrain", MsgTypeVarTupleConstrain, "cannot use constraints with TypeVarTuple"},
		{"MsgWalrusInComp", MsgWalrusInComp, "assignment expression cannot be used in a comprehension iterable expression"},
		{"MsgYieldOutsideFunction", MsgYieldOutsideFunction, "'yield' outside function"},
	}
	for _, r := range rows {
		if r.got != r.want {
			t.Errorf("%s drift: got %q want %q", r.name, r.got, r.want)
		}
	}
}
