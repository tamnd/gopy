// DSL tokenizer for Python/bytecodes.c. Mirrors the regex-based
// tokenizer from the upstream Python generator.
//
// CPython: Tools/cases_generator/lexer.py:294 tokenize

package main

import (
	"fmt"
	"regexp"
	"strings"
)

type tokKind int

const (
	tokInvalid tokKind = iota
	tokNewline
	tokIdentifier
	tokNumber
	tokString
	tokCharacter
	tokComment
	tokCMacro

	// Operators and punctuation
	tokLParen
	tokRParen
	tokLBracket
	tokRBracket
	tokLBrace
	tokRBrace
	tokComma
	tokPeriod
	tokSemi
	tokColon
	tokArrow
	tokEllipsis
	tokEquals
	tokEq
	tokNe
	tokLt
	tokGt
	tokLe
	tokGe
	tokPlus
	tokMinus
	tokTimes
	tokDivide
	tokMod
	tokAnd
	tokOr
	tokXor
	tokNot
	tokLAnd
	tokLOr
	tokLNot
	tokLShift
	tokRShift
	tokPlusPlus
	tokMinusMinus
	tokCondOp
	tokQuestion
	tokBackslash

	// Compound assignment
	tokTimesEq
	tokDivEq
	tokModEq
	tokPlusEq
	tokMinusEq
	tokLShiftEq
	tokRShiftEq
	tokAndEq
	tokOrEq
	tokXorEq

	// C keywords
	tokAuto
	tokBreak
	tokCase
	tokChar
	tokConst
	tokContinue
	tokDefault
	tokDo
	tokDouble
	tokElse
	tokEnum
	tokExtern
	tokFloat
	tokFor
	tokGoto
	tokIf
	tokInline
	tokInt
	tokLong
	tokOffsetof
	tokRestrict
	tokReturn
	tokShort
	tokSigned
	tokSizeof
	tokStatic
	tokStruct
	tokSwitch
	tokTypedef
	tokUnion
	tokUnsigned
	tokVoid
	tokVolatile
	tokWhile

	// DSL keywords
	tokInst
	tokOp
	tokMacro
	tokLabel
	tokSpilled

	// DSL annotations: specializing, override, replaced, pure, replicate, etc.
	tokAnnotation
)

var kindNames = map[tokKind]string{
	tokInvalid:    "INVALID",
	tokNewline:    "NEWLINE",
	tokIdentifier: "IDENTIFIER",
	tokNumber:     "NUMBER",
	tokString:     "STRING",
	tokCharacter:  "CHARACTER",
	tokComment:    "COMMENT",
	tokCMacro:     "CMACRO",
	tokLParen:     "LPAREN",
	tokRParen:     "RPAREN",
	tokLBracket:   "LBRACKET",
	tokRBracket:   "RBRACKET",
	tokLBrace:     "LBRACE",
	tokRBrace:     "RBRACE",
	tokComma:      "COMMA",
	tokPeriod:     "PERIOD",
	tokSemi:       "SEMI",
	tokColon:      "COLON",
	tokArrow:      "ARROW",
	tokEllipsis:   "ELLIPSIS",
	tokEquals:     "EQUALS",
	tokEq:         "EQ",
	tokNe:         "NE",
	tokLt:         "LT",
	tokGt:         "GT",
	tokLe:         "LE",
	tokGe:         "GE",
	tokPlus:       "PLUS",
	tokMinus:      "MINUS",
	tokTimes:      "TIMES",
	tokDivide:     "DIVIDE",
	tokMod:        "MOD",
	tokAnd:        "AND",
	tokOr:         "OR",
	tokXor:        "XOR",
	tokNot:        "NOT",
	tokLAnd:       "LAND",
	tokLOr:        "LOR",
	tokLNot:       "LNOT",
	tokLShift:     "LSHIFT",
	tokRShift:     "RSHIFT",
	tokPlusPlus:   "PLUSPLUS",
	tokMinusMinus: "MINUSMINUS",
	tokCondOp:     "CONDOP",
	tokQuestion:   "QUESTION",
	tokBackslash:  "BACKSLASH",
	tokTimesEq:    "TIMESEQUAL",
	tokDivEq:      "DIVEQUAL",
	tokModEq:      "MODEQUAL",
	tokPlusEq:     "PLUSEQUAL",
	tokMinusEq:    "MINUSEQUAL",
	tokLShiftEq:   "LSHIFTEQUAL",
	tokRShiftEq:   "RSHIFTEQUAL",
	tokAndEq:      "ANDEQUAL",
	tokOrEq:       "OREQUAL",
	tokXorEq:      "XOREQUAL",
	tokAuto:       "AUTO",
	tokBreak:      "BREAK",
	tokCase:       "CASE",
	tokChar:       "CHAR",
	tokConst:      "CONST",
	tokContinue:   "CONTINUE",
	tokDefault:    "DEFAULT",
	tokDo:         "DO",
	tokDouble:     "DOUBLE",
	tokElse:       "ELSE",
	tokEnum:       "ENUM",
	tokExtern:     "EXTERN",
	tokFloat:      "FLOAT",
	tokFor:        "FOR",
	tokGoto:       "GOTO",
	tokIf:         "IF",
	tokInline:     "INLINE",
	tokInt:        "INT",
	tokLong:       "LONG",
	tokOffsetof:   "OFFSETOF",
	tokRestrict:   "RESTRICT",
	tokReturn:     "RETURN",
	tokShort:      "SHORT",
	tokSigned:     "SIGNED",
	tokSizeof:     "SIZEOF",
	tokStatic:     "STATIC",
	tokStruct:     "STRUCT",
	tokSwitch:     "SWITCH",
	tokTypedef:    "TYPEDEF",
	tokUnion:      "UNION",
	tokUnsigned:   "UNSIGNED",
	tokVoid:       "VOID",
	tokVolatile:   "VOLATILE",
	tokWhile:      "WHILE",
	tokInst:       "INST",
	tokOp:         "OP",
	tokMacro:      "MACRO",
	tokLabel:      "LABEL",
	tokSpilled:    "SPILLED",
	tokAnnotation: "ANNOTATION",
}

func (k tokKind) String() string {
	if s, ok := kindNames[k]; ok {
		return s
	}
	return fmt.Sprintf("kind(%d)", int(k))
}

var keywordKinds = map[string]tokKind{
	"auto":     tokAuto,
	"break":    tokBreak,
	"case":     tokCase,
	"char":     tokChar,
	"const":    tokConst,
	"continue": tokContinue,
	"default":  tokDefault,
	"do":       tokDo,
	"double":   tokDouble,
	"else":     tokElse,
	"enum":     tokEnum,
	"extern":   tokExtern,
	"float":    tokFloat,
	"for":      tokFor,
	"goto":     tokGoto,
	"if":       tokIf,
	"inline":   tokInline,
	"int":      tokInt,
	"long":     tokLong,
	"offsetof": tokOffsetof,
	"restrict": tokRestrict,
	"return":   tokReturn,
	"short":    tokShort,
	"signed":   tokSigned,
	"sizeof":   tokSizeof,
	"static":   tokStatic,
	"struct":   tokStruct,
	"switch":   tokSwitch,
	"typedef":  tokTypedef,
	"union":    tokUnion,
	"unsigned": tokUnsigned,
	"void":     tokVoid,
	"volatile": tokVolatile,
	"while":    tokWhile,
	"inst":     tokInst,
	"op":       tokOp,
	"macro":    tokMacro,
	"label":    tokLabel,
	"spilled":  tokSpilled,
}

var annotationNames = map[string]bool{
	"specializing": true,
	"override":     true,
	"register":     true,
	"replaced":     true,
	"pure":         true,
	"replicate":    true,
	"tier1":        true,
	"tier2":        true,
	"no_save_ip":   true,
	"guard":        true,
}

// dslTok is one token from the bytecodes DSL stream.
type dslTok struct {
	Kind  tokKind
	Text  string
	Line  int // 1-based
	Col   int // 1-based at token start
	Begin int // byte offset, inclusive
	End   int // byte offset, exclusive
}

func (t dslTok) String() string {
	return fmt.Sprintf("%s(%q, %d:%d)", t.Kind, t.Text, t.Line, t.Col)
}

// One big alternation, longer operators before shorter ones so the
// regex engine prefers e.g. `<<=` over `<<` over `<`.
var dslMatcher = regexp.MustCompile(strings.Join([]string{
	// Identifier
	`[a-zA-Z_][0-9a-zA-Z_]*`,
	// Float (must come before integer alternatives)
	`(?:(?:(?:(?:[0-9]*\.[0-9]+)|(?:[0-9]+\.))(?:[eE][-+]?[0-9]+)?|[0-9]+(?:[eE][-+]?[0-9]+))[FfLl]?)`,
	// Integer: hex, octal, decimal
	`0[xX][0-9a-fA-F]+[uU]?[lL]?[lL]?`,
	`0[0-7]+[uU]?[lL]?[lL]?`,
	`(?:0|[1-9][0-9]*)[uU]?[lL]?[lL]?`,
	// String
	`"(?:[^"\\\n]|\\(?:[a-zA-Z._~!=&\^\-\\?'"]|\d+|x[0-9a-fA-F]+))*"`,
	// Character (single ASCII char between single quotes; no escapes for now, mirrors upstream TODO)
	`'.'`,
	// Newline
	`\n`,
	// C preprocessor macro line (#... up to newline)
	`#.*\n`,
	// Comment: line comment OR block comment
	`//[^\n]*`,
	`/\*(?:[^*]|\*[^/])*\*/`,
	// Compound assignment (longer first)
	`<<=`, `>>=`, `\*=`, `/=`, `%=`, `\+=`, `-=`, `&=`, `\|=`, `\^=`,
	// Operators (longer first)
	`->`, `\.\.\.`, `\+\+`, `--`, `<<`, `>>`,
	`<=`, `>=`, `==`, `!=`,
	`\|\|`, `&&`,
	// Single-char operators and punctuation
	`[+\-*/%~^<>!?=&|()\[\]{},.;:\\]`,
	// Catch-all (single non-space non-matched char)
	`\S`,
}, "|"))

// tokenize splits src into a slice of tokens. Whitespace (other than
// newlines) and embedded newlines inside #-prefixed macro lines are
// dropped, mirroring the upstream behavior.
//
// Content inside `#ifdef FLAG ... #endif` regions where FLAG names a
// build option the runtime doesn't carry (Py_STATS, Py_GIL_DISABLED,
// ENABLE_SPECIALIZATION_FT) is also dropped so dead-code helpers like
// `_Py_GatherStats_*` never reach the action translator. Nested ifdefs
// inside a skipped region count toward the same skip-depth.
func tokenize(src string) ([]dslTok, error) {
	skipFlags := map[string]bool{
		"Py_STATS":                 true,
		"Py_GIL_DISABLED":          true,
		"ENABLE_SPECIALIZATION_FT": true,
	}
	var out []dslTok
	line := 1
	lineStart := 0 // byte offset where current line begins
	skipDepth := 0
	matches := dslMatcher.FindAllStringIndex(src, -1)
	for _, m := range matches {
		start, end := m[0], m[1]
		text := src[start:end]
		col := start - lineStart + 1
		kind, err := classify(text, src, start)
		if err != nil {
			return out, fmt.Errorf("line %d col %d: %w (%q)", line, col, err, text)
		}
		t := dslTok{Kind: kind, Text: text, Line: line, Col: col, Begin: start, End: end}
		// Update line tracking. Newlines inside comments / macros / strings
		// must advance line and reset lineStart.
		if kind == tokNewline {
			line++
			lineStart = end
			continue
		}
		// Account for embedded newlines inside other tokens
		// (block comments, multi-line strings, macro continuations).
		if nl := strings.LastIndexByte(text, '\n'); nl >= 0 {
			line += strings.Count(text, "\n")
			lineStart = start + nl + 1
		}
		if kind == tokCMacro {
			trimmed := strings.TrimSpace(text)
			switch {
			case strings.HasPrefix(trimmed, "#ifdef ") || strings.HasPrefix(trimmed, "#ifndef ") || strings.HasPrefix(trimmed, "#if "):
				if skipDepth > 0 {
					skipDepth++
					continue
				}
				flag := ""
				if strings.HasPrefix(trimmed, "#ifdef ") {
					flag = strings.TrimSpace(strings.TrimPrefix(trimmed, "#ifdef"))
				} else if strings.HasPrefix(trimmed, "#ifndef ") {
					flag = strings.TrimSpace(strings.TrimPrefix(trimmed, "#ifndef"))
				} else {
					rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "#if"))
					if strings.HasPrefix(rest, "defined(") {
						flag = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(rest, "defined("), ")"))
					}
				}
				if skipFlags[flag] {
					skipDepth = 1
				}
				continue
			case strings.HasPrefix(trimmed, "#endif"):
				if skipDepth > 0 {
					skipDepth--
				}
				continue
			case strings.HasPrefix(trimmed, "#else") || strings.HasPrefix(trimmed, "#elif"):
				// Flip the active branch at the level we're currently
				// at: depth-1 means we were skipping the if-branch and
				// want to keep the else; depth-0 means we kept the
				// if-branch and want to skip the else.
				if skipDepth == 1 {
					skipDepth = 0
				} else if skipDepth == 0 {
					skipDepth = 1
				}
				continue
			}
			continue
		}
		if skipDepth > 0 {
			continue
		}
		if kind == tokComment {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

// tokenizeAll returns every match including comments and macros. Used
// by lexer round-trip tests.
//
//nolint:unused // kept for round-trip tests; will be exercised when the action translator (B6) starts consuming raw tokens.
func tokenizeAll(src string) ([]dslTok, error) {
	var out []dslTok
	line := 1
	lineStart := 0
	matches := dslMatcher.FindAllStringIndex(src, -1)
	for _, m := range matches {
		start, end := m[0], m[1]
		text := src[start:end]
		col := start - lineStart + 1
		kind, err := classify(text, src, start)
		if err != nil {
			return out, fmt.Errorf("line %d col %d: %w (%q)", line, col, err, text)
		}
		out = append(out, dslTok{Kind: kind, Text: text, Line: line, Col: col, Begin: start, End: end})
		if kind == tokNewline {
			line++
			lineStart = end
			continue
		}
		if nl := strings.LastIndexByte(text, '\n'); nl >= 0 {
			line += strings.Count(text, "\n")
			lineStart = start + nl + 1
		}
	}
	return out, nil
}

//nolint:gocyclo // mirrors the upstream tok kind dispatcher; collapsing the branches would diverge from parsing.py.
func classify(text, src string, start int) (tokKind, error) {
	if text == "\n" {
		return tokNewline, nil
	}
	if text == "..." {
		return tokEllipsis, nil
	}
	first := text[0]
	if first == '#' {
		return tokCMacro, nil
	}
	if isLetter(first) || first == '_' {
		if k, ok := keywordKinds[text]; ok {
			return k, nil
		}
		if annotationNames[text] {
			return tokAnnotation, nil
		}
		return tokIdentifier, nil
	}
	if (first >= '0' && first <= '9') || (first == '.' && len(text) > 1 && text[1] >= '0' && text[1] <= '9') {
		return tokNumber, nil
	}
	if first == '"' {
		return tokString, nil
	}
	if first == '\'' {
		return tokCharacter, nil
	}
	if first == '/' && len(text) >= 2 && (text[1] == '/' || text[1] == '*') {
		return tokComment, nil
	}
	if k, ok := opKinds[text]; ok {
		return k, nil
	}
	if first == '.' {
		return tokPeriod, nil
	}
	_ = src
	_ = start
	return tokInvalid, fmt.Errorf("bad token")
}

var opKinds = map[string]tokKind{
	"(":   tokLParen,
	")":   tokRParen,
	"[":   tokLBracket,
	"]":   tokRBracket,
	"{":   tokLBrace,
	"}":   tokRBrace,
	",":   tokComma,
	".":   tokPeriod,
	";":   tokSemi,
	":":   tokColon,
	"->":  tokArrow,
	"=":   tokEquals,
	"==":  tokEq,
	"!=":  tokNe,
	"<":   tokLt,
	">":   tokGt,
	"<=":  tokLe,
	">=":  tokGe,
	"+":   tokPlus,
	"-":   tokMinus,
	"*":   tokTimes,
	"/":   tokDivide,
	"%":   tokMod,
	"&":   tokAnd,
	"|":   tokOr,
	"^":   tokXor,
	"~":   tokNot,
	"&&":  tokLAnd,
	"||":  tokLOr,
	"!":   tokLNot,
	"<<":  tokLShift,
	">>":  tokRShift,
	"++":  tokPlusPlus,
	"--":  tokMinusMinus,
	"?":   tokQuestion,
	"\\":  tokBackslash,
	"*=":  tokTimesEq,
	"/=":  tokDivEq,
	"%=":  tokModEq,
	"+=":  tokPlusEq,
	"-=":  tokMinusEq,
	"<<=": tokLShiftEq,
	">>=": tokRShiftEq,
	"&=":  tokAndEq,
	"|=":  tokOrEq,
	"^=":  tokXorEq,
}

func isLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b == '_'
}
