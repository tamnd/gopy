// Str method surface. Ports the bulk of unicodeobject.c's user-facing
// methods: search, classification, case, split/join, strip, pad,
// replace, partition, translate, expandtabs.
//
// These operate on the canonical Go string stored on Unicode. They
// trust UTF-8 well-formedness because NewStr round-trips a Go string
// (the Go runtime guarantees its strings are byte-sequences; we don't
// re-validate). Methods that need codepoint indexing rather than byte
// indexing convert via runes.
//
// CPython: Objects/unicodeobject.c (the str.* method panel)
// CPython: Objects/stringlib/{split,find,replace,partition,join,
//          transmogrify,ctype}.h

package objects

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/tamnd/gopy/unicodetype"
)

// asUnicode pulls the Go string out of a *Unicode object. Returns
// "", false if o is not a *Unicode (callers should report TypeError
// in that case).
func asUnicode(o Object) (string, bool) {
	u, ok := o.(*Unicode)
	if !ok {
		return "", false
	}
	return u.v, true
}

// adjustFindIndices matches CPython's _adjust_indices used by find/count/
// startswith/endswith: clamps end to [0,n], adjusts negative start, but
// does NOT clamp start above n so that start > n produces an empty range.
//
// CPython: Objects/stringlib/find.h:_adjust_indices
func adjustFindIndices(n, start, end int) (int, int) {
	if end > n {
		end = n
	} else if end < 0 {
		end += n
		if end < 0 {
			end = 0
		}
	}
	if start < 0 {
		start += n
		if start < 0 {
			start = 0
		}
	}
	return start, end
}

// runeSlice converts s to a []rune for code-point indexing. Used by
// methods whose indices are documented as character (not byte) units.
func runeSlice(s string) []rune { return []rune(s) }

// StrFind ports str.find: leftmost index of needle in s[start:end] in
// code-point units, or -1 if not found.
//
// CPython: Objects/unicodeobject.c:L11956 unicode_find_impl
func StrFind(s, needle string, start, end int) int {
	rs := runeSlice(s)
	n := len(rs)
	start, end = adjustFindIndices(n, start, end)
	if start > end {
		return -1
	}
	needleRunes := []rune(needle)
	if end-start < len(needleRunes) {
		return -1
	}
	view := string(rs[start:end])
	idx := strings.Index(view, needle)
	if idx < 0 {
		return -1
	}
	// Convert byte index back to code-point index.
	prefix := view[:idx]
	return start + utf8.RuneCountInString(prefix)
}

// StrRFind ports str.rfind: rightmost index of needle.
//
// CPython: Objects/unicodeobject.c:L13048 unicode_rfind_impl
func StrRFind(s, needle string, start, end int) int {
	rs := runeSlice(s)
	n := len(rs)
	start, end = adjustFindIndices(n, start, end)
	if start > end {
		return -1
	}
	needleRunes := []rune(needle)
	if end-start < len(needleRunes) {
		return -1
	}
	view := string(rs[start:end])
	idx := strings.LastIndex(view, needle)
	if idx < 0 {
		return -1
	}
	prefix := view[:idx]
	return start + utf8.RuneCountInString(prefix)
}

// StrIndex is StrFind that raises ValueError on miss.
//
// CPython: Objects/unicodeobject.c:L12019 unicode_index_impl
func StrIndex(s, needle string, start, end int) (int, error) {
	i := StrFind(s, needle, start, end)
	if i < 0 {
		return 0, errors.New("ValueError: substring not found")
	}
	return i, nil
}

// StrRIndex is StrRFind that raises ValueError on miss.
//
// CPython: Objects/unicodeobject.c:L13069 unicode_rindex_impl
func StrRIndex(s, needle string, start, end int) (int, error) {
	i := StrRFind(s, needle, start, end)
	if i < 0 {
		return 0, errors.New("ValueError: substring not found")
	}
	return i, nil
}

// StrCount ports str.count: non-overlapping occurrences. Empty
// needle returns rune-count + 1.
//
// CPython: Objects/unicodeobject.c:L11771 unicode_count_impl
func StrCount(s, needle string, start, end int) int {
	rs := runeSlice(s)
	n := len(rs)
	start, end = adjustFindIndices(n, start, end)
	if start > end {
		if needle == "" {
			return 1
		}
		return 0
	}
	view := string(rs[start:end])
	if needle == "" {
		return utf8.RuneCountInString(view) + 1
	}
	return strings.Count(view, needle)
}

// StrStartsWith ports str.startswith.
//
// CPython: Objects/unicodeobject.c:L13609 unicode_startswith_impl
func StrStartsWith(s, prefix string, start, end int) bool {
	rs := runeSlice(s)
	n := len(rs)
	start, end = adjustFindIndices(n, start, end)
	if start > end {
		return false
	}
	view := string(rs[start:end])
	return strings.HasPrefix(view, prefix)
}

// StrEndsWith ports str.endswith.
//
// CPython: Objects/unicodeobject.c:L13665 unicode_endswith_impl
func StrEndsWith(s, suffix string, start, end int) bool {
	rs := runeSlice(s)
	n := len(rs)
	start, end = adjustFindIndices(n, start, end)
	if start > end {
		return false
	}
	view := string(rs[start:end])
	return strings.HasSuffix(view, suffix)
}

// StrSplit ports str.split. sep="" means whitespace mode (CPython
// signals this with sep=None).
//
// CPython: Objects/unicodeobject.c:L13138 unicode_split_impl
func StrSplit(s, sep string, maxsplit int) ([]string, error) {
	if sep == "" {
		return strSplitWhitespace(s, maxsplit, false), nil
	}
	if maxsplit < 0 {
		return strings.Split(s, sep), nil
	}
	return strings.SplitN(s, sep, maxsplit+1), nil
}

// StrRSplit ports str.rsplit. Unlike split, rsplit scans from the right,
// so overlapping separators give different results.
//
// CPython: Objects/unicodeobject.c:L13190 unicode_rsplit_impl
func StrRSplit(s, sep string, maxsplit int) ([]string, error) {
	if sep == "" {
		return strSplitWhitespace(s, maxsplit, true), nil
	}
	// Right-anchored split: walk from the right, peeling off pieces.
	// When maxsplit < 0, split at every occurrence (no limit).
	parts := []string{}
	rest := s
	n := 0
	for maxsplit < 0 || n < maxsplit {
		idx := strings.LastIndex(rest, sep)
		if idx < 0 {
			break
		}
		parts = append([]string{rest[idx+len(sep):]}, parts...)
		rest = rest[:idx]
		n++
	}
	return append([]string{rest}, parts...), nil
}

// isPyWhitespaceASCII matches _PyUnicode_IsWhitespace restricted to
// the ASCII range. CPython's table at unicodetype_db.h:6676 lists
// 0x09-0x0D, 0x1C-0x1F, 0x20 as the ASCII-range whitespace bits
// (the broader Py_UNICODE_ISSPACE set includes 0x85, 0xA0, and the
// U+1680/U+2000-U+200A/U+2028/U+2029/U+202F/U+205F/U+3000 codepoints
// which never appear in an isASCII() input).
//
// CPython: Objects/unicodetype_db.h:6676 _PyUnicode_IsWhitespace
func isPyWhitespaceASCII(c byte) bool {
	switch {
	case c >= 0x09 && c <= 0x0D:
		return true
	case c >= 0x1C && c <= 0x1F:
		return true
	case c == 0x20:
		return true
	}
	return false
}

// isPyWhitespaceRune matches _PyUnicode_IsWhitespace for any
// codepoint. Go's unicode.IsSpace tracks the White_Space Unicode
// property and is missing 0x1C-0x1F (FS, GS, RS, US ASCII control
// chars) which CPython's Py_UNICODE_ISSPACE table does include, so
// we cannot just defer to unicode.IsSpace here.
//
// CPython: Objects/unicodetype_db.h:6676 _PyUnicode_IsWhitespace
func isPyWhitespaceRune(r rune) bool {
	if r < 0x80 {
		return isPyWhitespaceASCII(byte(r))
	}
	switch r {
	case 0x85, 0xA0, 0x1680,
		0x2000, 0x2001, 0x2002, 0x2003, 0x2004,
		0x2005, 0x2006, 0x2007, 0x2008, 0x2009,
		0x200A, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000:
		return true
	}
	return false
}

// reverseStrings flips out in place. Used by the rsplit-whitespace
// fast path to drop the O(n^2) head-prepend without diverging from
// CPython's iteration order.
func reverseStrings(out []string) {
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
}

// strSplitWhitespace handles the sep=None mode: a maximal run of any
// Unicode whitespace is one separator, and leading/trailing
// whitespace produces no empty fragments. reverse=true peels from
// the right (used by rsplit).
//
// ASCII fast path: byte-indexed loop avoids materializing []rune(s)
// and lets the inner loop run as plain byte comparisons. Non-ASCII
// falls back to the rune walk until P4.1 lands kind-2/4 storage so
// BMP/Full strings can hit a kind-dispatched loop too.
//
// CPython: Objects/stringlib/split.h:11 stringlib_split_whitespace
// CPython: Objects/stringlib/split.h:177 stringlib_rsplit_whitespace
func strSplitWhitespace(s string, maxsplit int, reverse bool) []string {
	if isASCII(s) {
		return strSplitWhitespaceASCII(s, maxsplit, reverse)
	}
	return strSplitWhitespaceRunes(s, maxsplit, reverse)
}

// strSplitWhitespaceASCII is the byte-indexed fast path. Mirrors
// stringlib_split_whitespace / stringlib_rsplit_whitespace one-for-one
// against the ASCII char buffer.
//
// CPython: Objects/stringlib/split.h:53 stringlib_split_whitespace
// CPython: Objects/stringlib/split.h:192 stringlib_rsplit_whitespace
func strSplitWhitespaceASCII(s string, maxsplit int, reverse bool) []string {
	n := len(s)
	if reverse {
		// Build in reverse-walk order (right to left) then reverse the
		// slice once before returning. Matches CPython's SPLIT_ADD into
		// a preallocated list that is later finalized in place, and
		// avoids the O(n^2) prepend-into-head pattern.
		out := []string{}
		i := n
		split := 0
		for i > 0 {
			for i > 0 && isPyWhitespaceASCII(s[i-1]) {
				i--
			}
			if i == 0 {
				break
			}
			if maxsplit >= 0 && split >= maxsplit {
				break
			}
			j := i
			for i > 0 && !isPyWhitespaceASCII(s[i-1]) {
				i--
			}
			out = append(out, s[i:j])
			split++
		}
		if i > 0 {
			for i > 0 && isPyWhitespaceASCII(s[i-1]) {
				i--
			}
			if i > 0 {
				out = append(out, s[:i])
			}
		}
		reverseStrings(out)
		return out
	}
	out := []string{}
	i := 0
	split := 0
	for i < n {
		for i < n && isPyWhitespaceASCII(s[i]) {
			i++
		}
		if i == n {
			break
		}
		if maxsplit >= 0 && split >= maxsplit {
			out = append(out, s[i:])
			return out
		}
		j := i
		for i < n && !isPyWhitespaceASCII(s[i]) {
			i++
		}
		out = append(out, s[j:i])
		split++
	}
	return out
}

// strSplitWhitespaceRunes is the non-ASCII slow path. Walks runes
// because Latin-1+ strings still live as UTF-8 in *Unicode.v until
// P4.1 lands pre-encoded kind-2/4 storage.
func strSplitWhitespaceRunes(s string, maxsplit int, reverse bool) []string {
	rs := []rune(s)
	if reverse {
		out := []string{}
		i := len(rs)
		split := 0
		for i > 0 {
			for i > 0 && isPyWhitespaceRune(rs[i-1]) {
				i--
			}
			if i == 0 {
				break
			}
			if maxsplit >= 0 && split >= maxsplit {
				break
			}
			j := i
			for i > 0 && !isPyWhitespaceRune(rs[i-1]) {
				i--
			}
			out = append(out, string(rs[i:j]))
			split++
		}
		if i > 0 {
			head := rs[:i]
			for len(head) > 0 && isPyWhitespaceRune(head[len(head)-1]) {
				head = head[:len(head)-1]
			}
			if len(head) > 0 {
				out = append(out, string(head))
			}
		}
		reverseStrings(out)
		return out
	}
	out := []string{}
	i := 0
	split := 0
	for i < len(rs) {
		for i < len(rs) && isPyWhitespaceRune(rs[i]) {
			i++
		}
		if i == len(rs) {
			break
		}
		if maxsplit >= 0 && split >= maxsplit {
			tail := rs[i:]
			out = append(out, string(tail))
			return out
		}
		j := i
		for i < len(rs) && !isPyWhitespaceRune(rs[i]) {
			i++
		}
		out = append(out, string(rs[j:i]))
		split++
	}
	return out
}

// StrSplitLines ports str.splitlines: line-terminator-aware split.
// CPython recognizes \r, \n, \r\n, plus the wider universal-newline
// set (\v, \f, \x1c..\x1e, \x85,  ,  ).
//
// CPython: Objects/unicodeobject.c:L13342 unicode_splitlines_impl
// CPython: Objects/stringlib/split.h:L243 stringlib_splitlines
func StrSplitLines(s string, keepends bool) []string {
	if s == "" {
		return nil
	}
	out := []string{}
	rs := []rune(s)
	i, lineStart := 0, 0
	for i < len(rs) {
		ch := rs[i]
		if isLineBreak(ch) {
			eol := i + 1
			if ch == '\r' && i+1 < len(rs) && rs[i+1] == '\n' {
				eol = i + 2
			}
			if keepends {
				out = append(out, string(rs[lineStart:eol]))
			} else {
				out = append(out, string(rs[lineStart:i]))
			}
			i = eol
			lineStart = eol
			continue
		}
		i++
	}
	if lineStart < len(rs) {
		out = append(out, string(rs[lineStart:]))
	}
	return out
}

// isLineBreak matches CPython's BLOOM_LINEBREAK predicate plus the
// wider universal-newline set used by splitlines.
//
// CPython: Objects/unicodeobject.c:L11418 BLOOM_LINEBREAK
func isLineBreak(r rune) bool {
	switch r {
	case '\n', '\r', '\v', '\f',
		0x1c, 0x1d, 0x1e, 0x85,
		0x2028, 0x2029:
		return true
	}
	return false
}

// StrPartition ports str.partition.
//
// CPython: Objects/unicodeobject.c:L13272 unicode_partition
func StrPartition(s, sep string) (string, string, string, error) {
	if sep == "" {
		return "", "", "", errors.New("ValueError: empty separator")
	}
	idx := strings.Index(s, sep)
	if idx < 0 {
		return s, "", "", nil
	}
	return s[:idx], sep, s[idx+len(sep):], nil
}

// StrRPartition ports str.rpartition.
//
// CPython: Objects/unicodeobject.c:L13287 unicode_rpartition
func StrRPartition(s, sep string) (string, string, string, error) {
	if sep == "" {
		return "", "", "", errors.New("ValueError: empty separator")
	}
	idx := strings.LastIndex(s, sep)
	if idx < 0 {
		return "", "", s, nil
	}
	return s[:idx], sep, s[idx+len(sep):], nil
}

// StrJoin ports str.join. Each iterable item must be a *Unicode.
// Internally routes through UnicodeWriter (spec 1712 P15.2) so the
// output is classified once via the writer's maxchar tracking.
//
// CPython: Objects/unicodeobject.c:10253 PyUnicode_Join
// CPython: Objects/unicodeobject.c:12532 unicode_join
func StrJoin(sep string, parts []Object) (string, error) {
	u, err := StrJoinUnicode(NewStr(sep).(*Unicode), parts)
	if err != nil {
		return "", err
	}
	return u.Value(), nil
}

// StrJoinUnicode is the *Unicode-returning variant. The str.join
// binding calls this directly so the writer's Finish() produces an
// already-classified *Unicode (kind / ascii / length populated)
// instead of forcing a second walk via NewStr.
//
// CPython: Objects/unicodeobject.c:10278 _PyUnicode_JoinArray
func StrJoinUnicode(sep *Unicode, parts []Object) (*Unicode, error) {
	if len(parts) == 0 {
		return NewStr("").(*Unicode), nil
	}
	// Singleton fast path mirrors _PyUnicode_JoinArray's seqlen==1
	// branch.
	if len(parts) == 1 {
		u, ok := parts[0].(*Unicode)
		if !ok {
			return nil, fmt.Errorf("TypeError: sequence item 0: expected str instance, %s found",
				parts[0].Type().Name)
		}
		if u.Type() == strType {
			return u, nil
		}
	}
	var w UnicodeWriter
	w.Init()
	w.overallocate = true
	for i, p := range parts {
		u, ok := p.(*Unicode)
		if !ok {
			return nil, fmt.Errorf("TypeError: sequence item %d: expected str instance, %s found",
				i, p.Type().Name)
		}
		if i > 0 && sep.length > 0 {
			if err := w.WriteStr(sep); err != nil {
				return nil, err
			}
		}
		if err := w.WriteStr(u); err != nil {
			return nil, err
		}
	}
	return w.Finish(), nil
}

// StrReplace ports str.replace. count<0 means all.
//
// CPython: Objects/unicodeobject.c:L12885 unicode_replace_impl
func StrReplace(s, old, newS string, count int) string {
	if count < 0 {
		return strings.ReplaceAll(s, old, newS)
	}
	return strings.Replace(s, old, newS, count)
}

// stripASCIIWhitespace walks s from each end skipping bytes that
// _PyUnicode_IsWhitespace recognizes in the ASCII range. Returns
// the bounds (lo, hi) of the kept slice. left/right control which
// ends get trimmed: left/right both true is strip, left-only is
// lstrip, right-only is rstrip.
//
// CPython: Objects/stringlib/transmogrify.h:80 do_strip
func stripASCIIWhitespace(s string, left, right bool) string {
	n := len(s)
	lo := 0
	if left {
		for lo < n && isPyWhitespaceASCII(s[lo]) {
			lo++
		}
	}
	hi := n
	if right {
		for hi > lo && isPyWhitespaceASCII(s[hi-1]) {
			hi--
		}
	}
	if lo == 0 && hi == n {
		return s
	}
	return s[lo:hi]
}

// stripRunesWhitespace is the non-ASCII slow path. Walks runes
// because Latin-1+ strings still live as UTF-8 in *Unicode.v until
// P4.1 ports kind-2/4 storage. Uses isPyWhitespaceRune so 0x1C-0x1F
// match alongside the Unicode line/para/NBSP separators.
//
// CPython: Objects/unicodeobject.c:11744 _PyUnicode_XStrip
func stripRunesWhitespace(s string, left, right bool) string {
	rs := []rune(s)
	lo := 0
	if left {
		for lo < len(rs) && isPyWhitespaceRune(rs[lo]) {
			lo++
		}
	}
	hi := len(rs)
	if right {
		for hi > lo && isPyWhitespaceRune(rs[hi-1]) {
			hi--
		}
	}
	if lo == 0 && hi == len(rs) {
		return s
	}
	return string(rs[lo:hi])
}

// StrStrip ports str.strip. chars="" means default whitespace.
//
// CPython: Objects/unicodeobject.c:L12757 unicode_strip_impl
func StrStrip(s, chars string) string {
	if chars == "" {
		if isASCII(s) {
			return stripASCIIWhitespace(s, true, true)
		}
		return stripRunesWhitespace(s, true, true)
	}
	return strings.Trim(s, chars)
}

// StrLStrip ports str.lstrip.
//
// CPython: Objects/unicodeobject.c:L12717 unicode_lstrip_impl
func StrLStrip(s, chars string) string {
	if chars == "" {
		if isASCII(s) {
			return stripASCIIWhitespace(s, true, false)
		}
		return stripRunesWhitespace(s, true, false)
	}
	return strings.TrimLeft(s, chars)
}

// StrRStrip ports str.rstrip.
//
// CPython: Objects/unicodeobject.c:L12737 unicode_rstrip_impl
func StrRStrip(s, chars string) string {
	if chars == "" {
		if isASCII(s) {
			return stripASCIIWhitespace(s, false, true)
		}
		return stripRunesWhitespace(s, false, true)
	}
	return strings.TrimRight(s, chars)
}

// Case operations. These are faithful ports of unicodeobject.c's
// do_upper / do_lower / do_title / do_swapcase / do_capitalize /
// do_casefold over the unicodetype tables, so full (1->N) mappings
// (German sharp s, ligatures, Greek iota-subscript) and the Greek
// final-sigma context rule match CPython rather than Go's strings
// package (which only does simple per-rune mapping).
//
// CPython: Objects/unicodeobject.c:10207 case_operation

// handleCapitalSigma resolves U+03A3 to its lowercase form, picking
// the final form U+03C2 when sigma ends a word (the Final_Sigma
// context) and U+03C3 otherwise.
//
// CPython: Objects/unicodeobject.c:10039 handle_capital_sigma
func handleCapitalSigma(rs []rune, i int) rune {
	var c rune
	j := i - 1
	for ; j >= 0; j-- {
		c = rs[j]
		if !IsCaseIgnorableRune(c) {
			break
		}
	}
	finalSigma := j >= 0 && IsCasedRune(c)
	if finalSigma {
		j = i + 1
		for ; j < len(rs); j++ {
			c = rs[j]
			if !IsCaseIgnorableRune(c) {
				break
			}
		}
		finalSigma = j == len(rs) || !IsCasedRune(c)
	}
	if finalSigma {
		return 0x3C2
	}
	return 0x3C3
}

// lowerUcs4 writes the lowercase mapping of rs[i] into mapped,
// handling the U+03A3 final-sigma special case.
//
// CPython: Objects/unicodeobject.c:10068 lower_ucs4
func lowerUcs4(rs []rune, i int, c rune, mapped []rune) int {
	if c == 0x3A3 {
		mapped[0] = handleCapitalSigma(rs, i)
		return 1
	}
	return unicodetype.ToLowerFull(c, mapped)
}

func StrLower(s string) string {
	rs := strLenientRunes(s)
	out := make([]rune, 0, len(rs))
	var mapped [3]rune
	for i, c := range rs {
		n := lowerUcs4(rs, i, c, mapped[:])
		out = append(out, mapped[:n]...)
	}
	return RunesToStr(out)
}

func StrUpper(s string) string {
	rs := strLenientRunes(s)
	out := make([]rune, 0, len(rs))
	var mapped [3]rune
	for _, c := range rs {
		n := unicodetype.ToUpperFull(c, mapped[:])
		out = append(out, mapped[:n]...)
	}
	return RunesToStr(out)
}

// StrCaseFold ports str.casefold via the full folding table.
//
// CPython: Objects/unicodeobject.c:10162 do_casefold
func StrCaseFold(s string) string {
	rs := strLenientRunes(s)
	out := make([]rune, 0, len(rs))
	var mapped [3]rune
	for _, c := range rs {
		n := unicodetype.ToFoldedFull(c, mapped[:])
		out = append(out, mapped[:n]...)
	}
	return RunesToStr(out)
}

// StrSwapCase swaps each character's case.
//
// CPython: Objects/unicodeobject.c:10104 do_swapcase
func StrSwapCase(s string) string {
	rs := strLenientRunes(s)
	out := make([]rune, 0, len(rs))
	var mapped [3]rune
	for i, c := range rs {
		var n int
		switch {
		case IsUpperRune(c):
			n = lowerUcs4(rs, i, c, mapped[:])
		case IsLowerRune(c):
			n = unicodetype.ToUpperFull(c, mapped[:])
		default:
			n = 1
			mapped[0] = c
		}
		out = append(out, mapped[:n]...)
	}
	return RunesToStr(out)
}

// StrCapitalize ports str.capitalize: titlecase the first character,
// lowercase the rest. Empty string is unchanged.
//
// CPython: Objects/unicodeobject.c:10080 do_capitalize
func StrCapitalize(s string) string {
	if s == "" {
		return s
	}
	rs := strLenientRunes(s)
	out := make([]rune, 0, len(rs))
	var mapped [3]rune
	n := unicodetype.ToTitleFull(rs[0], mapped[:])
	out = append(out, mapped[:n]...)
	for i := 1; i < len(rs); i++ {
		n = lowerUcs4(rs, i, rs[i], mapped[:])
		out = append(out, mapped[:n]...)
	}
	return RunesToStr(out)
}

// StrTitle ports str.title: the first cased character of each word is
// titlecased, the rest lowercased. Word boundaries are runs of cased
// characters.
//
// CPython: Objects/unicodeobject.c:10179 do_title
func StrTitle(s string) string {
	rs := strLenientRunes(s)
	out := make([]rune, 0, len(rs))
	var mapped [3]rune
	previousIsCased := false
	for i, c := range rs {
		var n int
		if previousIsCased {
			n = lowerUcs4(rs, i, c, mapped[:])
		} else {
			n = unicodetype.ToTitleFull(c, mapped[:])
		}
		out = append(out, mapped[:n]...)
		previousIsCased = IsCasedRune(c)
	}
	return RunesToStr(out)
}

// StrCenter / StrLJust / StrRJust pad to width with fillchar.
//
// CPython: Objects/unicodeobject.c:L11166 unicode_center_impl
// CPython: Objects/unicodeobject.c:L12549 unicode_ljust_impl
// CPython: Objects/unicodeobject.c:L13085 unicode_rjust_impl
func StrCenter(s string, width int, fillchar rune) string {
	n := utf8.RuneCountInString(s)
	if n >= width {
		return s
	}
	pad := width - n
	left := pad / 2
	if pad%2 == 1 && width%2 == 1 {
		left = pad/2 + 1
	}
	right := pad - left
	return strings.Repeat(string(fillchar), left) + s +
		strings.Repeat(string(fillchar), right)
}

func StrLJust(s string, width int, fillchar rune) string {
	n := utf8.RuneCountInString(s)
	if n >= width {
		return s
	}
	return s + strings.Repeat(string(fillchar), width-n)
}

func StrRJust(s string, width int, fillchar rune) string {
	n := utf8.RuneCountInString(s)
	if n >= width {
		return s
	}
	return strings.Repeat(string(fillchar), width-n) + s
}

// StrZFill pads with leading zeros, preserving an optional +/- sign.
//
// CPython: Objects/unicodeobject.c:L13110 unicode_zfill_impl
func StrZFill(s string, width int) string {
	n := utf8.RuneCountInString(s)
	if n >= width {
		return s
	}
	pad := strings.Repeat("0", width-n)
	if s != "" && (s[0] == '+' || s[0] == '-') {
		return string(s[0]) + pad + s[1:]
	}
	return pad + s
}

// StrExpandTabs replaces tab characters with spaces such that columns
// align to the next multiple of tabsize.
//
// CPython: Objects/unicodeobject.c:L11871 unicode_expandtabs_impl
func StrExpandTabs(s string, tabsize int) string {
	var b strings.Builder
	col := 0
	for _, r := range s {
		switch r {
		case '\t':
			var n int
			if tabsize > 0 {
				n = tabsize - col%tabsize
			}
			for i := 0; i < n; i++ {
				b.WriteByte(' ')
			}
			col += n
		case '\n', '\r':
			b.WriteRune(r)
			col = 0
		default:
			b.WriteRune(r)
			col++
		}
	}
	return b.String()
}

// StrTranslate maps each rune via table[rune]. Missing entries pass
// through; entries whose value is -1 are deleted.
//
// CPython: Objects/unicodeobject.c:L11231 unicode_translate
func StrTranslate(s string, table map[rune]rune) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		v, ok := table[r]
		if !ok {
			out = append(out, r)
			continue
		}
		if v == -1 {
			continue
		}
		out = append(out, v)
	}
	return string(out)
}

// MakeStrTrans builds a translation table from "from"/"to"/"delete"
// strings. from and to must be the same length; characters in delete
// map to -1.
//
// CPython: Objects/unicodeobject.c:L13476 maketrans
func MakeStrTrans(from, to, drop string) (map[rune]rune, error) {
	fromR := []rune(from)
	toR := []rune(to)
	if len(fromR) != len(toR) {
		return nil, errors.New(
			"ValueError: the first two maketrans arguments must have equal length")
	}
	m := make(map[rune]rune, len(fromR)+len(drop))
	for i, r := range fromR {
		m[r] = toR[i]
	}
	for _, r := range drop {
		m[r] = -1
	}
	return m, nil
}

// Classification predicates. Each follows CPython's "non-empty AND
// every codepoint is in the predicate's set" rule.
//
// CPython: Objects/unicodeobject.c:L12043 unicode_isascii_impl
// CPython: Objects/unicodeobject.c:L12059 unicode_islower_impl
// CPython: Objects/unicodeobject.c:L12102 unicode_isupper_impl
// CPython: Objects/unicodeobject.c:L12145 unicode_istitle_impl
// CPython: Objects/unicodeobject.c:L12201 unicode_isspace_impl
// CPython: Objects/unicodeobject.c:L12239 unicode_isalpha_impl
// CPython: Objects/unicodeobject.c:L12276 unicode_isalnum_impl
// CPython: Objects/unicodeobject.c:L12315 unicode_isdecimal_impl
// CPython: Objects/unicodeobject.c:L12352 unicode_isdigit_impl
// CPython: Objects/unicodeobject.c:L12390 unicode_isnumeric_impl
// CPython: Objects/unicodeobject.c:L12470 unicode_isidentifier_impl
// CPython: Objects/unicodeobject.c:L12485 unicode_isprintable_impl

func StrIsASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// CPython: Objects/unicodeobject.c:12134 unicode_islower_impl
func StrIsLower(s string) bool {
	hasCased := false
	for _, r := range s {
		if IsUpperRune(r) || IsTitleRune(r) {
			return false
		}
		if IsLowerRune(r) {
			hasCased = true
		}
	}
	return hasCased
}

// CPython: Objects/unicodeobject.c:12164 unicode_isupper_impl
func StrIsUpper(s string) bool {
	hasCased := false
	for _, r := range s {
		if IsLowerRune(r) || IsTitleRune(r) {
			return false
		}
		if IsUpperRune(r) {
			hasCased = true
		}
	}
	return hasCased
}

// CPython: Objects/unicodeobject.c:12246 unicode_istitle_impl
func StrIsTitle(s string) bool {
	prevCased := false
	hasCased := false
	for _, r := range s {
		switch {
		case IsUpperRune(r) || IsTitleRune(r):
			if prevCased {
				return false
			}
			prevCased = true
			hasCased = true
		case IsLowerRune(r):
			if !prevCased {
				return false
			}
			prevCased = true
			hasCased = true
		default:
			prevCased = false
		}
	}
	return hasCased
}

// StrIsSpace ports str.isspace. A string is whitespace if it is
// non-empty and every code point satisfies _PyUnicode_IsWhitespace.
// ASCII haystacks walk bytes via isPyWhitespaceASCII (covering the
// 0x09-0x0D, 0x1C-0x1F, 0x20 set Go's unicode.IsSpace drops on the
// floor for FS/GS/RS/US); non-ASCII falls back to isPyWhitespaceRune.
//
// CPython: Objects/unicodeobject.c:12209 unicode_isspace_impl
func StrIsSpace(s string) bool {
	if s == "" {
		return false
	}
	if isASCII(s) {
		for i := 0; i < len(s); i++ {
			if !isPyWhitespaceASCII(s[i]) {
				return false
			}
		}
		return true
	}
	for _, r := range s {
		if !isPyWhitespaceRune(r) {
			return false
		}
	}
	return true
}

// CPython: Objects/unicodeobject.c:12054 unicode_isalpha_impl
func StrIsAlpha(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !IsAlphaRune(r) {
			return false
		}
	}
	return true
}

// CPython: Objects/unicodeobject.c:12305 unicode_isalnum_impl
func StrIsAlnum(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !IsAlphaRune(r) && !IsDecimalRune(r) && !IsDigitRune(r) && !IsNumericRune(r) {
			return false
		}
	}
	return true
}

// CPython: Objects/unicodeobject.c:12085 unicode_isdecimal_impl
func StrIsDecimal(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !IsDecimalRune(r) {
			return false
		}
	}
	return true
}

// CPython: Objects/unicodeobject.c:12104 unicode_isdigit_impl
func StrIsDigit(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !IsDigitRune(r) {
			return false
		}
	}
	return true
}

// CPython: Objects/unicodeobject.c:12345 unicode_isnumeric_impl
func StrIsNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !IsNumericRune(r) {
			return false
		}
	}
	return true
}

// CPython: Objects/unicodeobject.c:12386 unicode_isidentifier_impl
func StrIsIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if !IsXIDStartRune(r) {
				return false
			}
			continue
		}
		if !IsXIDContinueRune(r) {
			return false
		}
	}
	return true
}

// CPython: Objects/unicodeobject.c:12421 unicode_isprintable_impl
func StrIsPrintable(s string) bool {
	// Decode leniently: a lone surrogate is stored as 3-byte pseudo-UTF-8
	// and Go's range loop would surface it as U+FFFD (printable), masking
	// the surrogate. CPython classifies surrogates as non-printable.
	for _, r := range strLenientRunes(s) {
		if !IsPrintableRune(r) {
			return false
		}
	}
	return true
}
