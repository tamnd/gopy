// Lexer for the C-with-DSL flavor that CPython's bytecodes.c uses.
// Direct port of Tools/cases_generator/lexer.py: a regex-based scanner
// that emits a stream of (kind, text, begin, end) tokens. Whitespace
// is silently skipped (unmatched by the master alternation), newlines
// drive the line counter, macros (#if / #else / #endif / #other) are
// classified separately, and the DSL-specific keywords (inst, op,
// macro, label, spilled) plus the uop annotations (specializing,
// pure, replicate, ...) are recognized inline.
//
// CPython: Tools/cases_generator/lexer.py

package main

import (
	"fmt"
	"regexp"
	"strings"
)

// Token kinds. Names match CPython's so a port of parsing.py / analyzer.go
// can compare against string literals from those files unchanged. The
// values are also the canonical "kind" strings; tests assert on them.
const (
	// Operators (longest first matters for the master alternation).
	TokPlusPlus    = "PLUSPLUS"
	TokMinusMinus  = "MINUSMINUS"
	TokArrow       = "ARROW"
	TokEllipsis    = "ELLIPSIS"
	TokTimesEqual  = "TIMESEQUAL"
	TokDivEqual    = "DIVEQUAL"
	TokModEqual    = "MODEQUAL"
	TokPlusEqual   = "PLUSEQUAL"
	TokMinusEqual  = "MINUSEQUAL"
	TokLShiftEqual = "LSHIFTEQUAL"
	TokRShiftEqual = "RSHIFTEQUAL"
	TokAndEqual    = "ANDEQUAL"
	TokOrEqual     = "OREQUAL"
	TokXorEqual    = "XOREQUAL"
	TokPlus        = "PLUS"
	TokMinus       = "MINUS"
	TokTimes       = "TIMES"
	TokDivide      = "DIVIDE"
	TokMod         = "MOD"
	TokNot         = "NOT"
	TokXor         = "XOR"
	TokLOr         = "LOR"
	TokLAnd        = "LAND"
	TokLShift      = "LSHIFT"
	TokRShift      = "RSHIFT"
	TokLE          = "LE"
	TokGE          = "GE"
	TokEQ          = "EQ"
	TokNE          = "NE"
	TokLT          = "LT"
	TokGT          = "GT"
	TokLNot        = "LNOT"
	TokOr          = "OR"
	TokAnd         = "AND"
	TokEquals      = "EQUALS"
	TokCondOp      = "CONDOP"
	TokLParen      = "LPAREN"
	TokRParen      = "RPAREN"
	TokLBracket    = "LBRACKET"
	TokRBracket    = "RBRACKET"
	TokLBrace      = "LBRACE"
	TokRBrace      = "RBRACE"
	TokComma       = "COMMA"
	TokPeriod      = "PERIOD"
	TokSemi        = "SEMI"
	TokColon       = "COLON"
	TokBackslash   = "BACKSLASH"

	// Macros.
	TokCMacroIf    = "CMACRO_IF"
	TokCMacroElse  = "CMACRO_ELSE"
	TokCMacroEndif = "CMACRO_ENDIF"
	TokCMacroOther = "CMACRO_OTHER"

	// Atoms.
	TokIdentifier = "IDENTIFIER"
	TokNumber     = "NUMBER"
	TokString     = "STRING"
	TokCharacter  = "CHARACTER"
	TokComment    = "COMMENT"
	TokAnnotation = "ANNOTATION"

	// C keywords (subset CPython's lexer recognizes).
	TokAuto     = "AUTO"
	TokBreak    = "BREAK"
	TokCase     = "CASE"
	TokChar     = "CHAR"
	TokConst    = "CONST"
	TokContinue = "CONTINUE"
	TokDefault  = "DEFAULT"
	TokDo       = "DO"
	TokDouble   = "DOUBLE"
	TokElse     = "ELSE"
	TokEnum     = "ENUM"
	TokExtern   = "EXTERN"
	TokFloat    = "FLOAT"
	TokFor      = "FOR"
	TokGoto     = "GOTO"
	TokIf       = "IF"
	TokInline   = "INLINE"
	TokInt      = "INT"
	TokLong     = "LONG"
	TokOffsetof = "OFFSETOF"
	TokRestrict = "RESTRICT"
	TokReturn   = "RETURN"
	TokShort    = "SHORT"
	TokSigned   = "SIGNED"
	TokSizeof   = "SIZEOF"
	TokStatic   = "STATIC"
	TokStruct   = "STRUCT"
	TokSwitch   = "SWITCH"
	TokTypedef  = "TYPEDEF"
	TokUnion    = "UNION"
	TokUnsigned = "UNSIGNED"
	TokVoid     = "VOID"
	TokVolatile = "VOLATILE"
	TokWhile    = "WHILE"

	// DSL keywords.
	TokInst    = "INST"
	TokOp      = "OP"
	TokMacro   = "MACRO"
	TokLabel   = "LABEL"
	TokSpilled = "SPILLED"
)

// keywords maps lowercased keyword text to its token kind. Order
// mirrors lexer.py: the C reserved words first, then the DSL keywords
// the cases_generator added (inst / op / macro / label / spilled).
//
// CPython: Tools/cases_generator/lexer.py:141-224 keywords
var keywords = map[string]string{
	"auto":     TokAuto,
	"break":    TokBreak,
	"case":     TokCase,
	"char":     TokChar,
	"const":    TokConst,
	"continue": TokContinue,
	"default":  TokDefault,
	"do":       TokDo,
	"double":   TokDouble,
	"else":     TokElse,
	"enum":     TokEnum,
	"extern":   TokExtern,
	"float":    TokFloat,
	"for":      TokFor,
	"goto":     TokGoto,
	"if":       TokIf,
	"inline":   TokInline,
	"int":      TokInt,
	"long":     TokLong,
	"offsetof": TokOffsetof,
	"restrict": TokRestrict,
	"return":   TokReturn,
	"short":    TokShort,
	"signed":   TokSigned,
	"sizeof":   TokSizeof,
	"static":   TokStatic,
	"struct":   TokStruct,
	"switch":   TokSwitch,
	"typedef":  TokTypedef,
	"union":    TokUnion,
	"unsigned": TokUnsigned,
	"void":     TokVoid,
	"volatile": TokVolatile,
	"while":    TokWhile,
	"inst":     TokInst,
	"op":       TokOp,
	"macro":    TokMacro,
	"label":    TokLabel,
	"spilled":  TokSpilled,
}

// annotations is the set of identifiers the cases_generator treats as
// uop annotations rather than plain identifiers. They appear in
// constructs like `pure inst(NAME, ...)`.
//
// CPython: Tools/cases_generator/lexer.py:227-237 annotations
var annotations = map[string]struct{}{
	"specializing": {},
	"override":     {},
	"register":     {},
	"replaced":     {},
	"pure":         {},
	"replicate":    {},
	"tier1":        {},
	"tier2":        {},
	"no_save_ip":   {},
}

// opEntry pairs an operator's literal text with its token kind. Order
// matters: the master alternation is built in this sequence, so longer
// operators must come before any prefix of themselves (e.g. "<<="
// before "<<" before "<").
//
// CPython: Tools/cases_generator/lexer.py:18-78 (operator regex
// constants and the operators dict)
type opEntry struct {
	text string
	kind string
}

var operators = []opEntry{
	{"++", TokPlusPlus},
	{"--", TokMinusMinus},
	{"->", TokArrow},
	{"...", TokEllipsis},
	{"*=", TokTimesEqual},
	{"/=", TokDivEqual},
	{"%=", TokModEqual},
	{"+=", TokPlusEqual},
	{"-=", TokMinusEqual},
	{"<<=", TokLShiftEqual},
	{">>=", TokRShiftEqual},
	{"&=", TokAndEqual},
	{"|=", TokOrEqual},
	{"^=", TokXorEqual},
	{"<<", TokLShift},
	{">>", TokRShift},
	{"<=", TokLE},
	{">=", TokGE},
	{"==", TokEQ},
	{"!=", TokNE},
	{"||", TokLOr},
	{"&&", TokLAnd},
	{"+", TokPlus},
	{"-", TokMinus},
	{"*", TokTimes},
	{"/", TokDivide},
	{"%", TokMod},
	{"~", TokNot},
	{"^", TokXor},
	{"<", TokLT},
	{">", TokGT},
	{"!", TokLNot},
	{"|", TokOr},
	{"&", TokAnd},
	{"=", TokEquals},
	{"?", TokCondOp},
	{"(", TokLParen},
	{")", TokRParen},
	{"[", TokLBracket},
	{"]", TokRBracket},
	{"{", TokLBrace},
	{"}", TokRBrace},
	{",", TokComma},
	{".", TokPeriod},
	{";", TokSemi},
	{":", TokColon},
	{"\\", TokBackslash},
}

// opmap maps the literal operator text back to its kind for the
// classification step in the tokenize loop.
//
// CPython: Tools/cases_generator/lexer.py:79 opmap
var opmap = func() map[string]string {
	m := make(map[string]string, len(operators))
	for _, op := range operators {
		m[op.text] = op.kind
	}
	return m
}()

// matcher is the master alternation. Order: identifier, number,
// string, character, newline, macro, comment, operators (longest
// first), then \S as a catch-all for "bad token" reporting. Whitespace
// (other than \n, captured) is silently skipped: nothing in the
// alternation matches it, so re.finditer / regexp.FindAllStringIndex
// glides over it.
//
// CPython: Tools/cases_generator/lexer.py:125-137 matcher
var matcher = func() *regexp.Regexp {
	idRE := `[a-zA-Z_][0-9a-zA-Z_]*`
	suffix := `[uU]?[lL]?[lL]?`
	octal := `0[0-7]+` + suffix
	hexLit := `0[xX][0-9a-fA-F]+`
	decimalDigits := `(?:0|[1-9][0-9]*)`
	decimal := decimalDigits + suffix
	exponent := `[eE][-+]?[0-9]+`
	fraction := `(?:[0-9]*\.[0-9]+|[0-9]+\.)`
	floatLit := `(?:(?:(?:` + fraction + `)(?:` + exponent + `)?|[0-9]+` + exponent + `)[FfLl]?)`
	numberRE := alts(octal, hexLit, floatLit, decimal)

	simpleEscape := `[a-zA-Z._~!=&\^\-\\?'"]`
	decimalEscape := `\d+`
	hexEscape := `x[0-9a-fA-F]+`
	escapeSeq := `\\(?:` + simpleEscape + `|` + decimalEscape + `|` + hexEscape + `)`
	stringChar := `(?:[^"\\\n]|` + escapeSeq + `)`
	strRE := `"` + stringChar + `*"`
	charRE := `'.'`

	commentRE := `(?:(?://[^\n]*)|/\*(?:[^*]|\*[^/])*\*/)`
	macroRE := `#[^\n]*\n`
	newlineRE := `\n`

	var opPatterns []string
	for _, op := range operators {
		opPatterns = append(opPatterns, regexp.QuoteMeta(op.text))
	}

	all := []string{
		idRE,
		numberRE,
		strRE,
		charRE,
		newlineRE,
		macroRE,
		commentRE,
	}
	all = append(all, opPatterns...)
	all = append(all, `\S`) // catch-all for "bad token"

	return regexp.MustCompile(alts(all...))
}()

// alts wraps each alternative in a non-capturing group and joins with
// "|". CPython's choice() uses capturing groups; gopy uses
// non-capturing so the regex engine doesn't allocate captures we never
// inspect.
//
// CPython: Tools/cases_generator/lexer.py:10-11 choice
func alts(opts ...string) string {
	parts := make([]string, len(opts))
	for i, o := range opts {
		parts[i] = "(?:" + o + ")"
	}
	return strings.Join(parts, "|")
}

// Pos is a (line, column) pair. Lines are 1-based; columns are
// 0-based, matching Token.begin / Token.end in lexer.py.
type Pos struct {
	Line, Col int
}

// Token is one lexed unit. Mirrors lexer.py's @dataclass Token.
//
// CPython: Tools/cases_generator/lexer.py:253-291 Token
type Token struct {
	Filename string
	Kind     string
	Text     string
	Begin    Pos
	End      Pos
}

// Width is end column minus begin column. Single-line tokens only
// (multi-line comments give a meaningless number, matching CPython).
//
// CPython: Tools/cases_generator/lexer.py:277-279 Token.width
func (t Token) Width() int { return t.End.Col - t.Begin.Col }

// SyntaxError carries the same payload Python's SyntaxError does so
// errors round-trip with full context (file, line, column, line text)
// for a future error-reporter port.
//
// CPython: Tools/cases_generator/lexer.py:243-250 make_syntax_error
type SyntaxError struct {
	Message  string
	Filename string
	Line     int
	Column   int
	LineText string
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("%s:%d:%d: %s", e.Filename, e.Line, e.Column, e.Message)
}

// letterRE recognizes a leading C-identifier letter; the tokenize
// loop uses it to fall back to IDENTIFIER for anything that isn't a
// reserved word or annotation.
//
// CPython: Tools/cases_generator/lexer.py:138 letter
var letterRE = regexp.MustCompile(`^[a-zA-Z_]`)

// Tokenize scans src and returns every Token in source order. Whitespace
// (other than \n, which is consumed silently) does not produce tokens.
// startLine seeds the line counter for callers that lex a slice of a
// larger file.
//
// CPython: Tools/cases_generator/lexer.py:294-359 tokenize
func Tokenize(src, filename string, startLine int) ([]Token, error) {
	if startLine == 0 {
		startLine = 1
	}
	line := startLine
	linestart := -1
	var tokens []Token

	matches := matcher.FindAllStringIndex(src, -1)
	for _, m := range matches {
		start, end := m[0], m[1]
		text := src[start:end]
		var (
			kind      string
			macroBody string
		)
		switch {
		case isKeyword(text):
			kind = keywords[text]
		case isAnnotation(text):
			kind = TokAnnotation
		case letterRE.MatchString(text):
			kind = TokIdentifier
		case text == "...":
			kind = TokEllipsis
		case text == ".":
			kind = TokPeriod
		case (text[0] >= '0' && text[0] <= '9') || text[0] == '.':
			kind = TokNumber
		case text[0] == '"':
			kind = TokString
		case text == "\n":
			linestart = start
			line++
			kind = "\n"
		case text[0] == '\'':
			kind = TokCharacter
		case text[0] == '#':
			macroBody = strings.TrimSpace(text[1:])
			switch {
			case strings.HasPrefix(macroBody, "if"):
				kind = TokCMacroIf
			case strings.HasPrefix(macroBody, "else"):
				kind = TokCMacroElse
			case strings.HasPrefix(macroBody, "endif"):
				kind = TokCMacroEndif
			default:
				kind = TokCMacroOther
			}
		case text[0] == '/' && len(text) > 1 && (text[1] == '/' || text[1] == '*'):
			kind = TokComment
		default:
			if k, ok := opmap[text]; ok {
				kind = k
				break
			}
			lineend := strings.Index(src[start:], "\n")
			if lineend == -1 {
				lineend = len(src) - start
			}
			return tokens, &SyntaxError{
				Message:  "Bad token: " + text,
				Filename: filename,
				Line:     line,
				Column:   start - linestart,
				LineText: src[linestart+1 : start+lineend],
			}
		}

		var begin Pos
		if kind == TokComment {
			begin = Pos{line, start - linestart}
			newlines := strings.Count(text, "\n")
			if newlines > 0 {
				linestart = start + strings.LastIndex(text, "\n")
				line += newlines
			}
		} else {
			begin = Pos{line, start - linestart}
			if macroBody != "" {
				linestart = end
				line++
			}
		}
		if kind == "\n" {
			continue
		}
		tokens = append(tokens, Token{
			Filename: filename,
			Kind:     kind,
			Text:     text,
			Begin:    begin,
			End:      Pos{line, start - linestart + len(text)},
		})
	}
	return tokens, nil
}

func isKeyword(s string) bool { _, ok := keywords[s]; return ok }
func isAnnotation(s string) bool {
	_, ok := annotations[s]
	return ok
}

// ToText reconstructs source text from a token list, padding with
// spaces and newlines to match each token's recorded position. dedent
// shifts the column origin (negative dedents pull text left, useful
// when reflowing nested blocks).
//
// CPython: Tools/cases_generator/lexer.py:362-382 to_text
func ToText(tokens []Token, dedent int) string {
	var sb strings.Builder
	line, col := -1, 1+dedent
	for _, tkn := range tokens {
		if line == -1 {
			line = tkn.Begin.Line
		}
		l, c := tkn.Begin.Line, tkn.Begin.Col
		for l > line {
			line++
			sb.WriteByte('\n')
			col = 1 + dedent
		}
		if c > col {
			sb.WriteString(strings.Repeat(" ", c-col))
		}
		text := tkn.Text
		if dedent != 0 && tkn.Kind == TokComment && strings.Contains(text, "\n") {
			if dedent < 0 {
				text = strings.ReplaceAll(text, "\n", "\n"+strings.Repeat(" ", -dedent))
			}
		}
		sb.WriteString(text)
		line, col = tkn.End.Line, tkn.End.Col
	}
	return sb.String()
}
