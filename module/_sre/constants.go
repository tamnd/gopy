package _sre

// Opcode values for the _sre bytecode. The ordering and numeric values
// match the OPCODES list produced by _makecodes after the
// `del OPCODES[-2:]` line that strips the parser-only MIN_REPEAT and
// MAX_REPEAT entries.
//
// CPython: Lib/re/_constants.py:78 OPCODES
// CPython: Modules/_sre/sre_constants.h:13 SRE_OP_*
const (
	OpFailure uint32 = iota
	OpSuccess
	OpAny
	OpAnyAll
	OpAssert
	OpAssertNot
	OpAt
	OpBranch
	OpCategory
	OpCharset
	OpBigcharset
	OpGroupref
	OpGrouprefExists
	OpIn
	OpInfo
	OpJump
	OpLiteral
	OpMark
	OpMaxUntil
	OpMinUntil
	OpNotLiteral
	OpNegate
	OpRange
	OpRepeat
	OpRepeatOne
	OpSubpattern
	OpMinRepeatOne
	OpAtomicGroup
	OpPossessiveRepeat
	OpPossessiveRepeatOne
	OpGrouprefIgnore
	OpInIgnore
	OpLiteralIgnore
	OpNotLiteralIgnore
	OpGrouprefLocIgnore
	OpInLocIgnore
	OpLiteralLocIgnore
	OpNotLiteralLocIgnore
	OpGrouprefUniIgnore
	OpInUniIgnore
	OpLiteralUniIgnore
	OpNotLiteralUniIgnore
	OpRangeUniIgnore
)

// AT-position codes used by OpAt for anchors and word-boundary checks.
//
// CPython: Lib/re/_constants.py:130 ATCODES
const (
	AtBeginning uint32 = iota
	AtBeginningLine
	AtBeginningString
	AtBoundary
	AtNonBoundary
	AtEnd
	AtEndLine
	AtEndString
	AtLocBoundary
	AtLocNonBoundary
	AtUniBoundary
	AtUniNonBoundary
)

// Character-category codes used by OpCategory for the standard
// \d, \D, \s, \S, \w, \W, plus the linebreak class and locale /
// Unicode variants.
//
// CPython: Lib/re/_constants.py:141 CHCODES
const (
	CategoryDigit uint32 = iota
	CategoryNotDigit
	CategorySpace
	CategoryNotSpace
	CategoryWord
	CategoryNotWord
	CategoryLinebreak
	CategoryNotLinebreak
	CategoryLocWord
	CategoryLocNotWord
	CategoryUniDigit
	CategoryUniNotDigit
	CategoryUniSpace
	CategoryUniNotSpace
	CategoryUniWord
	CategoryUniNotWord
	CategoryUniLinebreak
	CategoryUniNotLinebreak
)

// Pattern compilation flags. Values match the SRE_FLAG_* bits in
// _constants.py.
//
// CPython: Lib/re/_constants.py:212 SRE_FLAG_*
const (
	SreFlagIgnorecase uint32 = 2
	SreFlagLocale     uint32 = 4
	SreFlagMultiline  uint32 = 8
	SreFlagDotall     uint32 = 16
	SreFlagUnicode    uint32 = 32
	SreFlagVerbose    uint32 = 64
	SreFlagDebug      uint32 = 128
	SreFlagAscii      uint32 = 256
)

// INFO-block flags appearing in the operand of OpInfo emitted by
// _compiler.py.
//
// CPython: Lib/re/_constants.py:222 SRE_INFO_*
const (
	SreInfoPrefix  uint32 = 1
	SreInfoLiteral uint32 = 2
	SreInfoCharset uint32 = 4
)

// MagicNumber identifies the bytecode version emitted by
// _compiler.py. Patterns whose stored magic does not match the
// engine's are recompiled by the Python layer.
//
// CPython: Lib/re/_constants.py:16 MAGIC
const MagicNumber = 20230612

// CodeSize is the byte width of one bytecode word. CPython's
// `_sre.compile` packs the bytecode into a `Py_UCS4` array so each
// word is 4 bytes. We expose the same constant for the Python layer.
//
// CPython: Modules/_sre/sre.c:3414 CODESIZE
const CodeSize = 4

// MaxRepeat is the sentinel for an unbounded repeat count. CPython
// uses ~(SRE_CODE)0 which on the 32-bit UCS4 bytecode is 0xFFFFFFFF.
//
// CPython: Modules/_sre/sre.h:20 SRE_MAXREPEAT
const MaxRepeat uint32 = 0xFFFFFFFF

// MaxGroups bounds the group count `_compiler.py` will pass to
// `_sre.compile`. CPython's hard cap is INT32_MAX / 2.
//
// CPython: Modules/_sre/sre.h:21 SRE_MAXGROUPS
const MaxGroups = 1073741823

// opIgnore maps a literal opcode to its case-insensitive ASCII
// variant. Emitted by _compiler.py when SRE_FLAG_IGNORECASE is set
// without SRE_FLAG_UNICODE.
//
// CPython: Lib/re/_constants.py:157 OP_IGNORE
var opIgnore = map[uint32]uint32{
	OpLiteral:    OpLiteralIgnore,
	OpNotLiteral: OpNotLiteralIgnore,
}

// opLocaleIgnore maps a literal opcode to its locale-aware
// case-insensitive variant.
//
// CPython: Lib/re/_constants.py:162 OP_LOCALE_IGNORE
var opLocaleIgnore = map[uint32]uint32{
	OpLiteral:    OpLiteralLocIgnore,
	OpNotLiteral: OpNotLiteralLocIgnore,
}

// opUnicodeIgnore maps a literal opcode to its Unicode
// case-insensitive variant.
//
// CPython: Lib/re/_constants.py:167 OP_UNICODE_IGNORE
var opUnicodeIgnore = map[uint32]uint32{
	OpLiteral:    OpLiteralUniIgnore,
	OpNotLiteral: OpNotLiteralUniIgnore,
}

// atMultiline rewrites the start/end-of-string anchors into their
// per-line counterparts when SRE_FLAG_MULTILINE is set.
//
// CPython: Lib/re/_constants.py:172 AT_MULTILINE
var atMultiline = map[uint32]uint32{
	AtBeginning: AtBeginningLine,
	AtEnd:       AtEndLine,
}

// atLocale rewrites boundary anchors into their locale-aware
// counterparts.
//
// CPython: Lib/re/_constants.py:177 AT_LOCALE
var atLocale = map[uint32]uint32{
	AtBoundary:    AtLocBoundary,
	AtNonBoundary: AtLocNonBoundary,
}

// atUnicode rewrites boundary anchors into their Unicode-aware
// counterparts.
//
// CPython: Lib/re/_constants.py:182 AT_UNICODE
var atUnicode = map[uint32]uint32{
	AtBoundary:    AtUniBoundary,
	AtNonBoundary: AtUniNonBoundary,
}

// chLocale maps the eight ASCII category codes into their locale
// variants. \d, \D, \s, \S, linebreak categories are pass-through;
// only the word categories rebind.
//
// CPython: Lib/re/_constants.py:187 CH_LOCALE
var chLocale = map[uint32]uint32{
	CategoryDigit:        CategoryDigit,
	CategoryNotDigit:     CategoryNotDigit,
	CategorySpace:        CategorySpace,
	CategoryNotSpace:     CategoryNotSpace,
	CategoryWord:         CategoryLocWord,
	CategoryNotWord:      CategoryLocNotWord,
	CategoryLinebreak:    CategoryLinebreak,
	CategoryNotLinebreak: CategoryNotLinebreak,
}

// chUnicode rebinds every ASCII category code to its Unicode
// counterpart.
//
// CPython: Lib/re/_constants.py:198 CH_UNICODE
var chUnicode = map[uint32]uint32{
	CategoryDigit:        CategoryUniDigit,
	CategoryNotDigit:     CategoryUniNotDigit,
	CategorySpace:        CategoryUniSpace,
	CategoryNotSpace:     CategoryUniNotSpace,
	CategoryWord:         CategoryUniWord,
	CategoryNotWord:      CategoryUniNotWord,
	CategoryLinebreak:    CategoryUniLinebreak,
	CategoryNotLinebreak: CategoryUniNotLinebreak,
}

// chNegate maps each category code to its complement, built from
// the CHCODES list the same way `dict(zip(CHCODES[::2] + CHCODES[1::2],
// CHCODES[1::2] + CHCODES[::2]))` does in Python.
//
// CPython: Lib/re/_constants.py:209 CH_NEGATE
var chNegate = map[uint32]uint32{
	CategoryDigit:           CategoryNotDigit,
	CategorySpace:           CategoryNotSpace,
	CategoryWord:            CategoryNotWord,
	CategoryLinebreak:       CategoryNotLinebreak,
	CategoryLocWord:         CategoryLocNotWord,
	CategoryUniDigit:        CategoryUniNotDigit,
	CategoryUniSpace:        CategoryUniNotSpace,
	CategoryUniWord:         CategoryUniNotWord,
	CategoryUniLinebreak:    CategoryUniNotLinebreak,
	CategoryNotDigit:        CategoryDigit,
	CategoryNotSpace:        CategorySpace,
	CategoryNotWord:         CategoryWord,
	CategoryNotLinebreak:    CategoryLinebreak,
	CategoryLocNotWord:      CategoryLocWord,
	CategoryUniNotDigit:     CategoryUniDigit,
	CategoryUniNotSpace:     CategoryUniSpace,
	CategoryUniNotWord:      CategoryUniWord,
	CategoryUniNotLinebreak: CategoryUniLinebreak,
}
