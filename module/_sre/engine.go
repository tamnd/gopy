package _sre

// Bytecode-level regex matcher. Direct port of Modules/_sre/sre_lib.h.
// Input is held as []int32 code points so the engine operates uniformly
// over str (one rune per slot) and bytes (one byte per slot). Bytecode
// is []uint32 to match CPython's SRE_CODE = Py_UCS4 = 4 bytes.
//
// CPython's sre_lib.h folds backtracking into an explicit context-frame
// stack to bound C stack usage. Go's goroutine stacks grow on demand,
// so this port keeps the per-opcode semantics line-for-line but uses
// host recursion instead. Every `DO_JUMP(..., label, newpat)` becomes
// a recursive call here; the value the C side leaves in `ret` after
// jumping back to `label` is exactly the recursive call's return.
//
// CPython: Modules/_sre/sre_lib.h
// CPython: Modules/_sre/sre.h

import (
	"unicode"
)

// errorCode values returned by match. <0 are CPython's SRE_ERROR_*
// constants; 0 means "no match"; 1 means "matched".
//
// CPython: Modules/_sre/sre.c:104 SRE_ERROR_*
const (
	errIllegal     = -1
	errState       = -2
	errRecursion   = -3
	errMemory      = -9
	errInterrupted = -10
)

// markUnset is the sentinel mark[i] value for "this group end has not
// been touched". CPython uses NULL.
const markUnset = -1

// repeatStack mirrors SRE_REPEAT. The MAX_UNTIL / MIN_UNTIL opcodes
// pop a node off the head, REPEAT installs one.
//
// CPython: Modules/_sre/sre.h:71 SRE_REPEAT
type repeatStack struct {
	count       int
	patternIdx  int  // offset into code; points to REPEAT operator's <skip>
	lastPtr     int  // last input index seen by this repeat; -1 = none
	hasLastPtr  bool // distinguishes "lastPtr=0" from "never set"
	prev        *repeatStack
}

// state mirrors SRE_STATE. Pointers in the C version are int offsets
// here. mark[i] is -1 when unset.
//
// CPython: Modules/_sre/sre.h:81 SRE_STATE
type state struct {
	input []int32 // target string as code points
	code  []uint32 // pattern bytecode

	beginning int // 0; kept for symmetry with the C field
	start     int // current slice start (pos)
	end       int // current slice end (endpos)
	ptr       int // current cursor (between match calls)

	pos    int // user-facing pos
	endpos int // user-facing endpos

	mark      []int // length = 2*groups; -1 = unset
	lastmark  int
	lastindex int

	repeat *repeatStack

	isbytes     bool
	matchAll    bool
	mustAdvance bool
}

// newState builds a state ready for match/search. start/end are
// clipped to [0, len(input)].
//
// CPython: Modules/_sre/sre.c:539 state_init
func newState(input []int32, code []uint32, groups int, start, end int, isbytes bool) *state {
	length := len(input)
	if start < 0 {
		start = 0
	} else if start > length {
		start = length
	}
	if end < 0 {
		end = 0
	} else if end > length {
		end = length
	}
	mark := make([]int, 2*groups)
	for i := range mark {
		mark[i] = markUnset
	}
	return &state{
		input:     input,
		code:      code,
		beginning: 0,
		start:     start,
		end:       end,
		ptr:       start,
		pos:       start,
		endpos:    end,
		mark:      mark,
		lastmark:  -1,
		lastindex: -1,
		isbytes:   isbytes,
	}
}

// reset clears per-attempt scratch state. Called between repeated
// match attempts in search loops.
//
// CPython: Modules/_sre/sre.c:487 state_reset
func (s *state) reset() {
	s.lastmark = -1
	s.lastindex = -1
	s.repeat = nil
}

// resetCaptureGroup clears the lastmark/lastindex scratch slots
// between successive match attempts in SRE(search)'s inner loop.
//
// CPython: Modules/_sre/sre_lib.h:1688 RESET_CAPTURE_GROUP
func (s *state) resetCaptureGroup() {
	s.lastmark = -1
	s.lastindex = -1
}

// ---------------------------------------------------------------------------
// Character predicates.

// sreIsDigit is the ASCII \d predicate; matches '0'..'9'.
//
// CPython: Modules/_sre/sre.c:134 SRE_IS_DIGIT
func sreIsDigit(ch int32) bool {
	return ch <= '9' && ch >= '0'
}

// sreIsSpace is the ASCII \s predicate; matches the six chars
// Py_ISSPACE accepts (space, \t, \n, \v, \f, \r).
//
// CPython: Modules/_sre/sre.c:136 SRE_IS_SPACE
func sreIsSpace(ch int32) bool {
	switch ch {
	case ' ', '\t', '\n', '\v', '\f', '\r':
		return true
	}
	return false
}

// sreIsLinebreak is the standalone linebreak predicate. CPython
// only counts '\n'.
//
// CPython: Modules/_sre/sre.c:138 SRE_IS_LINEBREAK
func sreIsLinebreak(ch int32) bool {
	return ch == '\n'
}

// sreIsAlnum is the ASCII alnum predicate.
//
// CPython: Modules/_sre/sre.c:140 Py_ISALNUM call chain
func sreIsAlnum(ch int32) bool {
	return (ch >= '0' && ch <= '9') ||
		(ch >= 'A' && ch <= 'Z') ||
		(ch >= 'a' && ch <= 'z')
}

// sreIsWord is the ASCII \w predicate.
//
// CPython: Modules/_sre/sre.c:140 SRE_IS_WORD
func sreIsWord(ch int32) bool {
	return ch <= 'z' && (sreIsAlnum(ch) || ch == '_')
}

// sreLowerAscii lowercases ASCII letters; pass-through otherwise.
//
// CPython: Modules/_sre/sre.c:143 sre_lower_ascii
func sreLowerAscii(ch int32) int32 {
	if ch < 128 && ch >= 'A' && ch <= 'Z' {
		return ch + ('a' - 'A')
	}
	return ch
}

// sreLowerUnicode is CPython's Py_UNICODE_TOLOWER.
//
// CPython: Modules/_sre/sre.c:172 sre_lower_unicode
func sreLowerUnicode(ch int32) int32 {
	return int32(unicode.ToLower(rune(ch)))
}

// sreUpperUnicode is CPython's Py_UNICODE_TOUPPER.
//
// CPython: Modules/_sre/sre.c:177 sre_upper_unicode
func sreUpperUnicode(ch int32) int32 {
	return int32(unicode.ToUpper(rune(ch)))
}

// sreLowerLocale: Go has no locale handling. CPython recommends
// against LOCALE classes in Python 3 anyway. Fall through to ASCII
// semantics; spec 1702 records this as an accepted deviation.
//
// CPython: Modules/_sre/sre.c:154 sre_lower_locale
func sreLowerLocale(ch int32) int32 { return sreLowerAscii(ch) }

// sreUpperLocale matches sreLowerLocale's accepted deviation.
//
// CPython: Modules/_sre/sre.c:159 sre_upper_locale
func sreUpperLocale(ch int32) int32 {
	if ch < 128 && ch >= 'a' && ch <= 'z' {
		return ch - ('a' - 'A')
	}
	return ch
}

// sreUniIsAlnum, sreUniIsDigit, sreUniIsSpace, sreUniIsLinebreak,
// sreUniIsWord are CPython's Py_UNICODE_IS* predicates routed
// through Go's unicode tables. CPython's IS_DECIMAL maps to
// unicode.IsDigit which checks the Nd category — exactly what
// CPython means by "decimal digit".
//
// CPython: Modules/_sre/sre.c:166 SRE_UNI_IS_*
func sreUniIsAlnum(ch int32) bool {
	r := rune(ch)
	return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsNumber(r)
}
func sreUniIsDigit(ch int32) bool     { return unicode.IsDigit(rune(ch)) }
func sreUniIsSpace(ch int32) bool     { return unicode.IsSpace(rune(ch)) }
func sreUniIsLinebreak(ch int32) bool { return isUnicodeLinebreak(rune(ch)) }
func sreUniIsWord(ch int32) bool      { return sreUniIsAlnum(ch) || ch == '_' }

// isUnicodeLinebreak mirrors Py_UNICODE_ISLINEBREAK: every Unicode
// line-break character (\n, \r, \v, \f, NEL, LS, PS, file/group/record
// separators).
//
// CPython: Include/cpython/unicodeobject.h Py_UNICODE_ISLINEBREAK
func isUnicodeLinebreak(r rune) bool {
	switch r {
	case 0x000A, 0x000B, 0x000C, 0x000D,
		0x001C, 0x001D, 0x001E, 0x0085,
		0x2028, 0x2029:
		return true
	}
	return false
}

// sreLocIsWord is CPython's locale word predicate. With the LOCALE
// flag deferred (see sreLowerLocale), fall through to ASCII.
//
// CPython: Modules/_sre/sre.c:152 SRE_LOC_IS_WORD
func sreLocIsWord(ch int32) bool { return sreIsWord(ch) }

// sreCategory dispatches the 18 category codes.
//
// CPython: Modules/_sre/sre.c:183 sre_category
func sreCategory(cat uint32, ch int32) bool {
	switch cat {
	case CategoryDigit:
		return sreIsDigit(ch)
	case CategoryNotDigit:
		return !sreIsDigit(ch)
	case CategorySpace:
		return sreIsSpace(ch)
	case CategoryNotSpace:
		return !sreIsSpace(ch)
	case CategoryWord:
		return sreIsWord(ch)
	case CategoryNotWord:
		return !sreIsWord(ch)
	case CategoryLinebreak:
		return sreIsLinebreak(ch)
	case CategoryNotLinebreak:
		return !sreIsLinebreak(ch)
	case CategoryLocWord:
		return sreLocIsWord(ch)
	case CategoryLocNotWord:
		return !sreLocIsWord(ch)
	case CategoryUniDigit:
		return sreUniIsDigit(ch)
	case CategoryUniNotDigit:
		return !sreUniIsDigit(ch)
	case CategoryUniSpace:
		return sreUniIsSpace(ch)
	case CategoryUniNotSpace:
		return !sreUniIsSpace(ch)
	case CategoryUniWord:
		return sreUniIsWord(ch)
	case CategoryUniNotWord:
		return !sreUniIsWord(ch)
	case CategoryUniLinebreak:
		return sreUniIsLinebreak(ch)
	case CategoryUniNotLinebreak:
		return !sreUniIsLinebreak(ch)
	}
	return false
}

// charLocIgnore matches CPython's locale-ignore-case char compare.
//
// CPython: Modules/_sre/sre.c:229 char_loc_ignore
func charLocIgnore(pat, ch int32) bool {
	return ch == pat ||
		sreLowerLocale(ch) == pat ||
		sreUpperLocale(ch) == pat
}

// ---------------------------------------------------------------------------
// AT-position predicate.

// at evaluates an OpAt position code at input index ptr.
//
// CPython: Modules/_sre/sre_lib.h:16 SRE(at)
func at(s *state, ptr int, code uint32) bool {
	switch code {
	case AtBeginning, AtBeginningString:
		return ptr == s.beginning
	case AtBeginningLine:
		return ptr == s.beginning || sreIsLinebreak(s.input[ptr-1])
	case AtEnd:
		return (s.end-ptr == 1 && sreIsLinebreak(s.input[ptr])) || ptr == s.end
	case AtEndLine:
		return ptr == s.end || sreIsLinebreak(s.input[ptr])
	case AtEndString:
		return ptr == s.end
	case AtBoundary:
		return boundary(s, ptr, sreIsWord)
	case AtNonBoundary:
		return !boundary(s, ptr, sreIsWord)
	case AtLocBoundary:
		return boundary(s, ptr, sreLocIsWord)
	case AtLocNonBoundary:
		return !boundary(s, ptr, sreLocIsWord)
	case AtUniBoundary:
		return boundary(s, ptr, sreUniIsWord)
	case AtUniNonBoundary:
		return !boundary(s, ptr, sreUniIsWord)
	}
	return false
}

// boundary implements the word-boundary check shared by AT_*BOUNDARY
// variants: a transition between a word and non-word char on either
// side of ptr.
func boundary(s *state, ptr int, isWord func(int32) bool) bool {
	thatp := false
	if ptr > s.beginning {
		thatp = isWord(s.input[ptr-1])
	}
	thisp := false
	if ptr < s.end {
		thisp = isWord(s.input[ptr])
	}
	return thisp != thatp
}

// ---------------------------------------------------------------------------
// Charset membership.

// charset evaluates a character set sub-program starting at code[setIdx].
// The set is a sequence of OP_LITERAL / OP_RANGE / OP_CATEGORY /
// OP_CHARSET / OP_BIGCHARSET / OP_NEGATE / OP_RANGE_UNI_IGNORE entries
// terminated by OP_FAILURE.
//
// CPython: Modules/_sre/sre_lib.h:91 SRE(charset)
func charset(s *state, setIdx int, ch int32) bool {
	ok := true
	code := s.code
	i := setIdx
	for {
		op := code[i]
		i++
		switch op {
		case OpFailure:
			return !ok
		case OpLiteral:
			if ch == int32(code[i]) {
				return ok
			}
			i++
		case OpCategory:
			if sreCategory(code[i], ch) {
				return ok
			}
			i++
		case OpCharset:
			// <CHARSET> <256-bit bitmap>: 8 uint32 words.
			if ch < 256 {
				word := code[i+int(ch>>5)]
				if word&(uint32(1)<<(uint32(ch)&31)) != 0 {
					return ok
				}
			}
			i += 8
		case OpRange:
			if int32(code[i]) <= ch && ch <= int32(code[i+1]) {
				return ok
			}
			i += 2
		case OpRangeUniIgnore:
			// ch is already lowercased per the caller; also try the
			// uppercase form.
			if int32(code[i]) <= ch && ch <= int32(code[i+1]) {
				return ok
			}
			uch := sreUpperUnicode(ch)
			if int32(code[i]) <= uch && uch <= int32(code[i+1]) {
				return ok
			}
			i += 2
		case OpNegate:
			ok = !ok
		case OpBigcharset:
			// <BIGCHARSET> <blockcount> <256 blockindices packed in
			// 256/sizeof(SRE_CODE) = 64 words> <blocks * 8 words>
			count := int(code[i])
			i++
			block := -1
			if uint32(ch) < 0x10000 {
				blockIdx := uint32(ch) >> 8 // 0..255
				word := code[i+int(blockIdx>>2)]
				block = int(int8(word >> ((blockIdx & 3) * 8)))
			}
			i += 64
			if block >= 0 {
				bit := uint32(ch) & 0xFF
				word := code[i+block*8+int(bit>>5)]
				if word&(uint32(1)<<(bit&31)) != 0 {
					return ok
				}
			}
			i += count * 8
		default:
			return false
		}
	}
}

// charsetLocIgnore wraps charset with locale upper/lower variants.
//
// CPython: Modules/_sre/sre_lib.h:179 SRE(charset_loc_ignore)
func charsetLocIgnore(s *state, setIdx int, ch int32) bool {
	lo := sreLowerLocale(ch)
	if charset(s, setIdx, lo) {
		return true
	}
	up := sreUpperLocale(ch)
	return up != lo && charset(s, setIdx, up)
}

// ---------------------------------------------------------------------------
// count: how many times a single-char pattern at code[patIdx] matches
// starting at state.ptr. Honours maxcount = SRE_MAXREPEAT for "unbounded".
//
// CPython: Modules/_sre/sre_lib.h:192 SRE(count)
func count(s *state, patIdx int, maxcount int) (int, error) {
	code := s.code
	ptr := s.ptr
	end := s.end
	if maxcount != int(MaxRepeat) && maxcount < end-ptr {
		end = ptr + maxcount
	}
	switch code[patIdx] {
	case OpIn:
		// <IN> <skip> <set>
		for ptr < end && charset(s, patIdx+2, s.input[ptr]) {
			ptr++
		}
	case OpAny:
		for ptr < end && !sreIsLinebreak(s.input[ptr]) {
			ptr++
		}
	case OpAnyAll:
		ptr = end
	case OpLiteral:
		chr := int32(code[patIdx+1])
		for ptr < end && s.input[ptr] == chr {
			ptr++
		}
	case OpLiteralIgnore:
		chr := int32(code[patIdx+1])
		for ptr < end && sreLowerAscii(s.input[ptr]) == chr {
			ptr++
		}
	case OpLiteralUniIgnore:
		chr := int32(code[patIdx+1])
		for ptr < end && sreLowerUnicode(s.input[ptr]) == chr {
			ptr++
		}
	case OpLiteralLocIgnore:
		chr := int32(code[patIdx+1])
		for ptr < end && charLocIgnore(chr, s.input[ptr]) {
			ptr++
		}
	case OpNotLiteral:
		chr := int32(code[patIdx+1])
		for ptr < end && s.input[ptr] != chr {
			ptr++
		}
	case OpNotLiteralIgnore:
		chr := int32(code[patIdx+1])
		for ptr < end && sreLowerAscii(s.input[ptr]) != chr {
			ptr++
		}
	case OpNotLiteralUniIgnore:
		chr := int32(code[patIdx+1])
		for ptr < end && sreLowerUnicode(s.input[ptr]) != chr {
			ptr++
		}
	case OpNotLiteralLocIgnore:
		chr := int32(code[patIdx+1])
		for ptr < end && !charLocIgnore(chr, s.input[ptr]) {
			ptr++
		}
	default:
		// General single-char subpattern; reentrant match.
		saved := s.ptr
		for s.ptr < end {
			r, err := match(s, patIdx, false)
			if err != nil {
				return 0, err
			}
			if r < 0 {
				return r, nil
			}
			if r == 0 {
				break
			}
		}
		n := s.ptr - saved
		s.ptr = saved
		return n, nil
	}
	return ptr - s.ptr, nil
}

// ---------------------------------------------------------------------------
// match: dispatcher. Returns 1 on success, 0 on failure, <0 on error.
// state.ptr is set to the consumed position on success.
//
// CPython: Modules/_sre/sre_lib.h:598 SRE(match)
func match(s *state, codeIdx int, toplevel bool) (int, error) {
	code := s.code
	ptr := s.ptr
	end := s.end

	if codeIdx >= len(code) {
		return errIllegal, nil
	}
	if code[codeIdx] == OpInfo {
		// <INFO> <skip> <flags> <min> <max> ...
		if code[codeIdx+3] != 0 && uint32(end-ptr) < code[codeIdx+3] {
			return 0, nil
		}
		codeIdx += int(code[codeIdx+1]) + 1
	}

	for {
		op := code[codeIdx]
		codeIdx++
		switch op {
		case OpMark:
			// <MARK> <gid>
			i := int(code[codeIdx])
			if i&1 != 0 {
				s.lastindex = i/2 + 1
			}
			if i > s.lastmark {
				for j := s.lastmark + 1; j < i; j++ {
					s.mark[j] = markUnset
				}
				s.lastmark = i
			}
			s.mark[i] = ptr
			codeIdx++

		case OpLiteral:
			if ptr >= end || s.input[ptr] != int32(code[codeIdx]) {
				return 0, nil
			}
			codeIdx++
			ptr++

		case OpNotLiteral:
			if ptr >= end || s.input[ptr] == int32(code[codeIdx]) {
				return 0, nil
			}
			codeIdx++
			ptr++

		case OpSuccess:
			if toplevel && ((s.matchAll && ptr != s.end) ||
				(s.mustAdvance && ptr == s.start)) {
				return 0, nil
			}
			s.ptr = ptr
			return 1, nil

		case OpAt:
			if !at(s, ptr, code[codeIdx]) {
				return 0, nil
			}
			codeIdx++

		case OpCategory:
			if ptr >= end || !sreCategory(code[codeIdx], s.input[ptr]) {
				return 0, nil
			}
			codeIdx++
			ptr++

		case OpAny:
			if ptr >= end || sreIsLinebreak(s.input[ptr]) {
				return 0, nil
			}
			ptr++

		case OpAnyAll:
			if ptr >= end {
				return 0, nil
			}
			ptr++

		case OpIn:
			// <IN> <skip> <set>
			if ptr >= end || !charset(s, codeIdx+1, s.input[ptr]) {
				return 0, nil
			}
			codeIdx += int(code[codeIdx])
			ptr++

		case OpLiteralIgnore:
			if ptr >= end || sreLowerAscii(s.input[ptr]) != int32(code[codeIdx]) {
				return 0, nil
			}
			codeIdx++
			ptr++

		case OpLiteralUniIgnore:
			if ptr >= end || sreLowerUnicode(s.input[ptr]) != int32(code[codeIdx]) {
				return 0, nil
			}
			codeIdx++
			ptr++

		case OpLiteralLocIgnore:
			if ptr >= end || !charLocIgnore(int32(code[codeIdx]), s.input[ptr]) {
				return 0, nil
			}
			codeIdx++
			ptr++

		case OpNotLiteralIgnore:
			if ptr >= end || sreLowerAscii(s.input[ptr]) == int32(code[codeIdx]) {
				return 0, nil
			}
			codeIdx++
			ptr++

		case OpNotLiteralUniIgnore:
			if ptr >= end || sreLowerUnicode(s.input[ptr]) == int32(code[codeIdx]) {
				return 0, nil
			}
			codeIdx++
			ptr++

		case OpNotLiteralLocIgnore:
			if ptr >= end || charLocIgnore(int32(code[codeIdx]), s.input[ptr]) {
				return 0, nil
			}
			codeIdx++
			ptr++

		case OpInIgnore:
			if ptr >= end || !charset(s, codeIdx+1, sreLowerAscii(s.input[ptr])) {
				return 0, nil
			}
			codeIdx += int(code[codeIdx])
			ptr++

		case OpInUniIgnore:
			if ptr >= end || !charset(s, codeIdx+1, sreLowerUnicode(s.input[ptr])) {
				return 0, nil
			}
			codeIdx += int(code[codeIdx])
			ptr++

		case OpInLocIgnore:
			if ptr >= end || !charsetLocIgnore(s, codeIdx+1, s.input[ptr]) {
				return 0, nil
			}
			codeIdx += int(code[codeIdx])
			ptr++

		case OpJump, OpInfo:
			// <JUMP> <offset>
			codeIdx += int(code[codeIdx])

		case OpBranch:
			// <BRANCH> <0=skip> code <JUMP> ... <NULL>
			savedLastmark := s.lastmark
			savedLastindex := s.lastindex
			var savedMarks []int
			if s.repeat != nil && s.lastmark >= 0 {
				savedMarks = append(savedMarks, s.mark[:s.lastmark+1]...)
			}
			for code[codeIdx] != 0 {
				// Branch prefix optimization: skip alternatives whose
				// first opcode can't match the current char.
				if code[codeIdx+1] == OpLiteral &&
					(ptr >= end || s.input[ptr] != int32(code[codeIdx+2])) {
					codeIdx += int(code[codeIdx])
					continue
				}
				if code[codeIdx+1] == OpIn &&
					(ptr >= end || !charset(s, codeIdx+3, s.input[ptr])) {
					codeIdx += int(code[codeIdx])
					continue
				}
				s.ptr = ptr
				r, err := match(s, codeIdx+1, toplevel)
				if err != nil {
					return -1, err
				}
				if r < 0 {
					return r, nil
				}
				if r > 0 {
					return 1, nil
				}
				if s.repeat != nil && savedMarks != nil {
					copy(s.mark, savedMarks)
				}
				s.lastmark = savedLastmark
				s.lastindex = savedLastindex
				codeIdx += int(code[codeIdx])
			}
			return 0, nil

		case OpRepeatOne:
			// <REPEAT_ONE> <skip> <min> <max> item <SUCCESS> tail
			pat := codeIdx
			minCount := int(code[pat+1])
			maxCount := int(code[pat+2])
			if int(code[pat+1]) > end-ptr {
				return 0, nil
			}
			s.ptr = ptr
			cnt, err := count(s, pat+3, maxCount)
			if err != nil {
				return -1, err
			}
			if cnt < 0 {
				return cnt, nil
			}
			ptr += cnt
			if cnt < minCount {
				return 0, nil
			}
			tailIdx := pat + int(code[pat])
			if code[tailIdx] == OpSuccess && ptr == s.end &&
				!(toplevel && s.mustAdvance && ptr == s.start) {
				s.ptr = ptr
				return 1, nil
			}
			savedLastmark := s.lastmark
			savedLastindex := s.lastindex
			var savedMarks []int
			if s.repeat != nil && s.lastmark >= 0 {
				savedMarks = append(savedMarks, s.mark[:s.lastmark+1]...)
			}
			if code[tailIdx] == OpLiteral {
				lit := int32(code[tailIdx+1])
				for {
					for cnt >= minCount && (ptr >= end || s.input[ptr] != lit) {
						ptr--
						cnt--
					}
					if cnt < minCount {
						break
					}
					s.ptr = ptr
					r, err := match(s, tailIdx, toplevel)
					if err != nil {
						return -1, err
					}
					if r < 0 {
						return r, nil
					}
					if r > 0 {
						return 1, nil
					}
					if s.repeat != nil && savedMarks != nil {
						copy(s.mark, savedMarks)
					}
					s.lastmark = savedLastmark
					s.lastindex = savedLastindex
					ptr--
					cnt--
				}
			} else {
				for cnt >= minCount {
					s.ptr = ptr
					r, err := match(s, tailIdx, toplevel)
					if err != nil {
						return -1, err
					}
					if r < 0 {
						return r, nil
					}
					if r > 0 {
						return 1, nil
					}
					if s.repeat != nil && savedMarks != nil {
						copy(s.mark, savedMarks)
					}
					s.lastmark = savedLastmark
					s.lastindex = savedLastindex
					ptr--
					cnt--
				}
			}
			return 0, nil

		case OpMinRepeatOne:
			// <MIN_REPEAT_ONE> <skip> <min> <max> item <SUCCESS> tail
			pat := codeIdx
			minCount := int(code[pat+1])
			maxCount := int(code[pat+2])
			if minCount > end-ptr {
				return 0, nil
			}
			s.ptr = ptr
			var cnt int
			if minCount == 0 {
				cnt = 0
			} else {
				r, err := count(s, pat+3, minCount)
				if err != nil {
					return -1, err
				}
				if r < 0 {
					return r, nil
				}
				if r < minCount {
					return 0, nil
				}
				cnt = r
				ptr += cnt
			}
			tailIdx := pat + int(code[pat])
			if code[tailIdx] == OpSuccess &&
				!(toplevel && ((s.matchAll && ptr != s.end) ||
					(s.mustAdvance && ptr == s.start))) {
				s.ptr = ptr
				return 1, nil
			}
			savedLastmark := s.lastmark
			savedLastindex := s.lastindex
			var savedMarks []int
			if s.repeat != nil && s.lastmark >= 0 {
				savedMarks = append(savedMarks, s.mark[:s.lastmark+1]...)
			}
			for maxCount == int(MaxRepeat) || cnt <= maxCount {
				s.ptr = ptr
				r, err := match(s, tailIdx, toplevel)
				if err != nil {
					return -1, err
				}
				if r < 0 {
					return r, nil
				}
				if r > 0 {
					return 1, nil
				}
				if s.repeat != nil && savedMarks != nil {
					copy(s.mark, savedMarks)
				}
				s.lastmark = savedLastmark
				s.lastindex = savedLastindex
				s.ptr = ptr
				r, err = count(s, pat+3, 1)
				if err != nil {
					return -1, err
				}
				if r < 0 {
					return r, nil
				}
				if r == 0 {
					break
				}
				ptr++
				cnt++
			}
			return 0, nil

		case OpPossessiveRepeatOne:
			// <POSSESSIVE_REPEAT_ONE> <skip> <min> <max> item <SUCCESS> tail
			pat := codeIdx
			minCount := int(code[pat+1])
			maxCount := int(code[pat+2])
			if ptr+minCount > end {
				return 0, nil
			}
			s.ptr = ptr
			cnt, err := count(s, pat+3, maxCount)
			if err != nil {
				return -1, err
			}
			if cnt < 0 {
				return cnt, nil
			}
			ptr += cnt
			if cnt < minCount {
				return 0, nil
			}
			codeIdx = pat + int(code[pat])
			if code[codeIdx] == OpSuccess && ptr == s.end &&
				!(toplevel && s.mustAdvance && ptr == s.start) {
				s.ptr = ptr
				return 1, nil
			}
			// fall through into the dispatch loop with codeIdx advanced

		case OpRepeat:
			// <REPEAT> <skip> <min> <max> <repeat_index> item <UNTIL> tail
			pat := codeIdx
			rep := &repeatStack{
				count:      -1,
				patternIdx: pat,
				prev:       s.repeat,
			}
			s.repeat = rep
			s.ptr = ptr
			r, err := match(s, pat+int(code[pat]), toplevel)
			s.repeat = rep.prev
			if err != nil {
				return -1, err
			}
			if r > 0 {
				return 1, nil
			}
			if r < 0 {
				return r, nil
			}
			return 0, nil

		case OpMaxUntil:
			// <REPEAT> ... <MAX_UNTIL> tail
			rep := s.repeat
			if rep == nil {
				return errState, nil
			}
			s.ptr = ptr
			cnt := rep.count + 1
			repPat := rep.patternIdx
			if cnt < int(code[repPat+1]) {
				rep.count = cnt
				r, err := match(s, repPat+3, toplevel)
				if err != nil {
					return -1, err
				}
				if r > 0 {
					return 1, nil
				}
				if r < 0 {
					return r, nil
				}
				rep.count = cnt - 1
				s.ptr = ptr
				return 0, nil
			}
			if (cnt < int(code[repPat+2]) || code[repPat+2] == MaxRepeat) &&
				(!rep.hasLastPtr || s.ptr != rep.lastPtr) {
				rep.count = cnt
				savedLastmark := s.lastmark
				savedLastindex := s.lastindex
				var savedMarks []int
				if s.lastmark >= 0 {
					savedMarks = append(savedMarks, s.mark[:s.lastmark+1]...)
				}
				savedLastPtr := rep.lastPtr
				savedHas := rep.hasLastPtr
				rep.lastPtr = s.ptr
				rep.hasLastPtr = true
				r, err := match(s, repPat+3, toplevel)
				rep.lastPtr = savedLastPtr
				rep.hasLastPtr = savedHas
				if err != nil {
					return -1, err
				}
				if r > 0 {
					return 1, nil
				}
				if r < 0 {
					return r, nil
				}
				if savedMarks != nil {
					copy(s.mark, savedMarks)
				}
				s.lastmark = savedLastmark
				s.lastindex = savedLastindex
				rep.count = cnt - 1
				s.ptr = ptr
			}
			savedRepeat := s.repeat
			s.repeat = rep.prev
			r, err := match(s, codeIdx, toplevel)
			s.repeat = savedRepeat
			if err != nil {
				return -1, err
			}
			if r > 0 {
				return 1, nil
			}
			if r < 0 {
				return r, nil
			}
			s.ptr = ptr
			return 0, nil

		case OpMinUntil:
			rep := s.repeat
			if rep == nil {
				return errState, nil
			}
			s.ptr = ptr
			cnt := rep.count + 1
			repPat := rep.patternIdx
			if cnt < int(code[repPat+1]) {
				rep.count = cnt
				r, err := match(s, repPat+3, toplevel)
				if err != nil {
					return -1, err
				}
				if r > 0 {
					return 1, nil
				}
				if r < 0 {
					return r, nil
				}
				rep.count = cnt - 1
				s.ptr = ptr
				return 0, nil
			}
			savedRepeat := s.repeat
			s.repeat = rep.prev
			savedLastmark := s.lastmark
			savedLastindex := s.lastindex
			var savedMarks []int
			if s.repeat != nil && s.lastmark >= 0 {
				savedMarks = append(savedMarks, s.mark[:s.lastmark+1]...)
			}
			r, err := match(s, codeIdx, toplevel)
			repeatOfTail := s.repeat
			s.repeat = savedRepeat
			if err != nil {
				return -1, err
			}
			if r > 0 {
				return 1, nil
			}
			if r < 0 {
				return r, nil
			}
			if repeatOfTail != nil && savedMarks != nil {
				copy(s.mark, savedMarks)
			}
			s.lastmark = savedLastmark
			s.lastindex = savedLastindex
			s.ptr = ptr
			if (cnt >= int(code[repPat+2]) && code[repPat+2] != MaxRepeat) ||
				(rep.hasLastPtr && s.ptr == rep.lastPtr) {
				return 0, nil
			}
			rep.count = cnt
			savedLastPtr := rep.lastPtr
			savedHas := rep.hasLastPtr
			rep.lastPtr = s.ptr
			rep.hasLastPtr = true
			r, err = match(s, repPat+3, toplevel)
			rep.lastPtr = savedLastPtr
			rep.hasLastPtr = savedHas
			if err != nil {
				return -1, err
			}
			if r > 0 {
				return 1, nil
			}
			if r < 0 {
				return r, nil
			}
			rep.count = cnt - 1
			s.ptr = ptr
			return 0, nil

		case OpPossessiveRepeat:
			// <POSSESSIVE_REPEAT> <skip> <min> <max> pattern <SUCCESS> tail
			pat := codeIdx
			s.ptr = ptr
			rep := &repeatStack{count: -1, patternIdx: -1, prev: s.repeat}
			s.repeat = rep
			cnt := 0
			for cnt < int(code[pat+1]) {
				r, err := match(s, pat+3, false)
				if err != nil {
					s.repeat = rep.prev
					return -1, err
				}
				if r > 0 {
					cnt++
				} else {
					s.ptr = ptr
					s.repeat = rep.prev
					if r < 0 {
						return r, nil
					}
					return 0, nil
				}
			}
			lastPtr := -1
			for (cnt < int(code[pat+2]) || code[pat+2] == MaxRepeat) && s.ptr != lastPtr {
				savedLastmark := s.lastmark
				savedLastindex := s.lastindex
				var savedMarks []int
				if s.lastmark >= 0 {
					savedMarks = append(savedMarks, s.mark[:s.lastmark+1]...)
				}
				lastPtr = s.ptr
				r, err := match(s, pat+3, false)
				if err != nil {
					s.repeat = rep.prev
					return -1, err
				}
				if r > 0 {
					cnt++
				} else {
					if savedMarks != nil {
						copy(s.mark, savedMarks)
					}
					s.lastmark = savedLastmark
					s.lastindex = savedLastindex
					s.ptr = lastPtr
					if r < 0 {
						s.repeat = rep.prev
						return r, nil
					}
					break
				}
			}
			s.repeat = rep.prev
			codeIdx = pat + int(code[pat]) + 1
			ptr = s.ptr

		case OpAtomicGroup:
			// <ATOMIC_GROUP> <skip> pattern <SUCCESS> tail
			pat := codeIdx
			s.ptr = ptr
			r, err := match(s, pat+1, false)
			if err != nil {
				return -1, err
			}
			if r < 0 {
				return r, nil
			}
			if r == 0 {
				s.ptr = ptr
				return 0, nil
			}
			codeIdx = pat + int(code[pat])
			ptr = s.ptr

		case OpGroupref:
			// <GROUPREF> <gid>
			groupref := int(code[codeIdx]) * 2
			if groupref >= s.lastmark {
				return 0, nil
			}
			p := s.mark[groupref]
			e := s.mark[groupref+1]
			if p == markUnset || e == markUnset || e < p {
				return 0, nil
			}
			for p < e {
				if ptr >= end || s.input[ptr] != s.input[p] {
					return 0, nil
				}
				p++
				ptr++
			}
			codeIdx++

		case OpGrouprefIgnore:
			groupref := int(code[codeIdx]) * 2
			if groupref >= s.lastmark {
				return 0, nil
			}
			p := s.mark[groupref]
			e := s.mark[groupref+1]
			if p == markUnset || e == markUnset || e < p {
				return 0, nil
			}
			for p < e {
				if ptr >= end || sreLowerAscii(s.input[ptr]) != sreLowerAscii(s.input[p]) {
					return 0, nil
				}
				p++
				ptr++
			}
			codeIdx++

		case OpGrouprefUniIgnore:
			groupref := int(code[codeIdx]) * 2
			if groupref >= s.lastmark {
				return 0, nil
			}
			p := s.mark[groupref]
			e := s.mark[groupref+1]
			if p == markUnset || e == markUnset || e < p {
				return 0, nil
			}
			for p < e {
				if ptr >= end || sreLowerUnicode(s.input[ptr]) != sreLowerUnicode(s.input[p]) {
					return 0, nil
				}
				p++
				ptr++
			}
			codeIdx++

		case OpGrouprefLocIgnore:
			groupref := int(code[codeIdx]) * 2
			if groupref >= s.lastmark {
				return 0, nil
			}
			p := s.mark[groupref]
			e := s.mark[groupref+1]
			if p == markUnset || e == markUnset || e < p {
				return 0, nil
			}
			for p < e {
				if ptr >= end || sreLowerLocale(s.input[ptr]) != sreLowerLocale(s.input[p]) {
					return 0, nil
				}
				p++
				ptr++
			}
			codeIdx++

		case OpGrouprefExists:
			// <GROUPREF_EXISTS> <gid> <skip> codeyes <JUMP> codeno
			groupref := int(code[codeIdx]) * 2
			if groupref >= s.lastmark {
				codeIdx += int(code[codeIdx+1])
				continue
			}
			p := s.mark[groupref]
			e := s.mark[groupref+1]
			if p == markUnset || e == markUnset || e < p {
				codeIdx += int(code[codeIdx+1])
				continue
			}
			codeIdx += 2

		case OpAssert:
			// <ASSERT> <skip> <back> <pattern>
			back := int(code[codeIdx+1])
			if ptr-s.beginning < back {
				return 0, nil
			}
			s.ptr = ptr - back
			r, err := match(s, codeIdx+2, false)
			if err != nil {
				return -1, err
			}
			if r < 0 {
				return r, nil
			}
			if r == 0 {
				return 0, nil
			}
			codeIdx += int(code[codeIdx])

		case OpAssertNot:
			back := int(code[codeIdx+1])
			if ptr-s.beginning >= back {
				s.ptr = ptr - back
				savedLastmark := s.lastmark
				savedLastindex := s.lastindex
				var savedMarks []int
				if s.repeat != nil && s.lastmark >= 0 {
					savedMarks = append(savedMarks, s.mark[:s.lastmark+1]...)
				}
				r, err := match(s, codeIdx+2, false)
				if err != nil {
					return -1, err
				}
				if r > 0 {
					return 0, nil
				}
				if r < 0 {
					return r, nil
				}
				if s.repeat != nil && savedMarks != nil {
					copy(s.mark, savedMarks)
				}
				s.lastmark = savedLastmark
				s.lastindex = savedLastindex
			}
			codeIdx += int(code[codeIdx])

		case OpFailure:
			return 0, nil

		default:
			return errIllegal, nil
		}
	}
}

// ---------------------------------------------------------------------------
// search: start-position walk. Honours the INFO prefix / charset
// optimisations the compiler emits.
//
// CPython: Modules/_sre/sre_lib.h:1691 SRE(search)
func search(s *state, codeIdx int) (int, error) {
	code := s.code
	ptr := s.start
	end := s.end
	if ptr > end {
		return 0, nil
	}
	if codeIdx >= len(code) {
		return errIllegal, nil
	}

	var (
		prefixLen  = 0
		prefixSkip = 0
		prefix     = 0
		charsetIdx = -1
		overlap    = 0
		flags      uint32
	)
	patternIdx := codeIdx

	if code[patternIdx] == OpInfo {
		// <INFO> <skip> <flags> <min> <max> ...
		flags = code[patternIdx+2]
		if code[patternIdx+3] != 0 && uint32(end-ptr) < code[patternIdx+3] {
			return 0, nil
		}
		if code[patternIdx+3] > 1 {
			end -= int(code[patternIdx+3]) - 1
			if end <= ptr {
				end = ptr
			}
		}
		if flags&SreInfoPrefix != 0 {
			prefixLen = int(code[patternIdx+5])
			prefixSkip = int(code[patternIdx+6])
			prefix = patternIdx + 7
			overlap = prefix + prefixLen - 1
		} else if flags&SreInfoCharset != 0 {
			charsetIdx = patternIdx + 5
		}
		patternIdx += 1 + int(code[patternIdx+1])
	}

	if prefixLen == 1 {
		c := int32(code[prefix])
		end = s.end
		s.mustAdvance = false
		for ptr < end {
			for s.input[ptr] != c {
				ptr++
				if ptr >= end {
					return 0, nil
				}
			}
			s.start = ptr
			s.ptr = ptr + prefixSkip
			if flags&SreInfoLiteral != 0 {
				return 1, nil
			}
			r, err := match(s, patternIdx+2*prefixSkip, false)
			if err != nil {
				return -1, err
			}
			if r != 0 {
				return r, nil
			}
			ptr++
			s.resetCaptureGroup()
		}
		return 0, nil
	}

	if prefixLen > 1 {
		i := 0
		end = s.end
		if prefixLen > end-ptr {
			return 0, nil
		}
		for ptr < end {
			c := int32(code[prefix])
			for s.input[ptr] != c {
				ptr++
				if ptr >= end {
					return 0, nil
				}
			}
			ptr++
			if ptr >= end {
				return 0, nil
			}
			i = 1
			s.mustAdvance = false
			for {
				if s.input[ptr] == int32(code[prefix+i]) {
					i++
					if i != prefixLen {
						ptr++
						if ptr >= end {
							return 0, nil
						}
						continue
					}
					s.start = ptr - (prefixLen - 1)
					s.ptr = ptr - (prefixLen - prefixSkip - 1)
					if flags&SreInfoLiteral != 0 {
						return 1, nil
					}
					r, err := match(s, patternIdx+2*prefixSkip, false)
					if err != nil {
						return -1, err
					}
					if r != 0 {
						return r, nil
					}
					ptr++
					if ptr >= end {
						return 0, nil
					}
					s.resetCaptureGroup()
				}
				i = int(code[overlap+i])
				if i == 0 {
					break
				}
			}
		}
		return 0, nil
	}

	if charsetIdx >= 0 {
		end = s.end
		s.mustAdvance = false
		for {
			for ptr < end && !charset(s, charsetIdx, s.input[ptr]) {
				ptr++
			}
			if ptr >= end {
				return 0, nil
			}
			s.start = ptr
			s.ptr = ptr
			r, err := match(s, patternIdx, false)
			if err != nil {
				return -1, err
			}
			if r != 0 {
				return r, nil
			}
			ptr++
			s.resetCaptureGroup()
		}
	}

	s.start = ptr
	s.ptr = ptr
	r, err := match(s, patternIdx, true)
	if err != nil {
		return -1, err
	}
	s.mustAdvance = false
	if r == 0 && code[patternIdx] == OpAt &&
		(code[patternIdx+1] == AtBeginning || code[patternIdx+1] == AtBeginningString) {
		s.start = end
		s.ptr = end
		return 0, nil
	}
	for r == 0 && ptr < end {
		ptr++
		s.resetCaptureGroup()
		s.start = ptr
		s.ptr = ptr
		r, err = match(s, patternIdx, false)
		if err != nil {
			return -1, err
		}
	}
	return r, nil
}
