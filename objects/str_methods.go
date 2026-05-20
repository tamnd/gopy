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
	"unicode"
	"unicode/utf8"
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

// clampStrSlice mirrors clampBytesSlice but in code-point space. start
// and end are negative-aware; they are clamped to [0, n] where n is
// the rune count of the string.
//
// CPython: Objects/unicodeobject.c:L11949 ADJUST_INDICES (unicode flavor)
func clampStrSlice(n, start, end int) (int, int) {
	if start < 0 {
		start += n
		if start < 0 {
			start = 0
		}
	}
	if end < 0 {
		end += n
		if end < 0 {
			end = 0
		}
	}
	if start > n {
		start = n
	}
	if end > n {
		end = n
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
	start, end = clampStrSlice(n, start, end)
	if end-start < len([]rune(needle)) {
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
	start, end = clampStrSlice(n, start, end)
	if end-start < len([]rune(needle)) {
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
	start, end = clampStrSlice(n, start, end)
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
	start, end = clampStrSlice(n, start, end)
	view := string(rs[start:end])
	return strings.HasPrefix(view, prefix)
}

// StrEndsWith ports str.endswith.
//
// CPython: Objects/unicodeobject.c:L13665 unicode_endswith_impl
func StrEndsWith(s, suffix string, start, end int) bool {
	rs := runeSlice(s)
	n := len(rs)
	start, end = clampStrSlice(n, start, end)
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

// StrRSplit ports str.rsplit.
//
// CPython: Objects/unicodeobject.c:L13190 unicode_rsplit_impl
func StrRSplit(s, sep string, maxsplit int) ([]string, error) {
	if sep == "" {
		return strSplitWhitespace(s, maxsplit, true), nil
	}
	if maxsplit < 0 {
		return strings.Split(s, sep), nil
	}
	// Right-anchored split: walk the source from the right, peeling
	// off up to maxsplit pieces, then prepend the remainder.
	parts := []string{}
	rest := s
	for i := 0; i < maxsplit; i++ {
		idx := strings.LastIndex(rest, sep)
		if idx < 0 {
			break
		}
		parts = append([]string{rest[idx+len(sep):]}, parts...)
		rest = rest[:idx]
	}
	return append([]string{rest}, parts...), nil
}

// strSplitWhitespace handles the sep=None mode: a maximal run of any
// Unicode whitespace is one separator, and leading/trailing
// whitespace produces no empty fragments. reverse=true peels from
// the right (used by rsplit).
//
// CPython: Objects/stringlib/split.h:L11 stringlib_split_whitespace
// CPython: Objects/stringlib/split.h:L177 stringlib_rsplit_whitespace
func strSplitWhitespace(s string, maxsplit int, reverse bool) []string {
	rs := []rune(s)
	if reverse {
		out := []string{}
		i := len(rs)
		split := 0
		for i > 0 {
			for i > 0 && unicode.IsSpace(rs[i-1]) {
				i--
			}
			if i == 0 {
				break
			}
			if maxsplit >= 0 && split >= maxsplit {
				break
			}
			j := i
			for i > 0 && !unicode.IsSpace(rs[i-1]) {
				i--
			}
			out = append([]string{string(rs[i:j])}, out...)
			split++
		}
		if i > 0 {
			// Trim trailing whitespace from the head fragment. Per
			// CPython, the remainder keeps its leading whitespace if
			// it has more than one non-WS run, but loses it if it's
			// at the very head.
			head := rs[:i]
			// strip trailing ws from head
			for len(head) > 0 && unicode.IsSpace(head[len(head)-1]) {
				head = head[:len(head)-1]
			}
			if len(head) > 0 {
				out = append([]string{string(head)}, out...)
			}
		}
		return out
	}
	out := []string{}
	i := 0
	split := 0
	for i < len(rs) {
		for i < len(rs) && unicode.IsSpace(rs[i]) {
			i++
		}
		if i == len(rs) {
			break
		}
		if maxsplit >= 0 && split >= maxsplit {
			// remainder, with its trailing ws, is one fragment
			tail := rs[i:]
			out = append(out, string(tail))
			return out
		}
		j := i
		for i < len(rs) && !unicode.IsSpace(rs[i]) {
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

// StrStrip ports str.strip. chars="" means default whitespace.
//
// CPython: Objects/unicodeobject.c:L12757 unicode_strip_impl
func StrStrip(s, chars string) string {
	if chars == "" {
		return strings.TrimFunc(s, unicode.IsSpace)
	}
	return strings.Trim(s, chars)
}

// StrLStrip ports str.lstrip.
//
// CPython: Objects/unicodeobject.c:L12717 unicode_lstrip_impl
func StrLStrip(s, chars string) string {
	if chars == "" {
		return strings.TrimLeftFunc(s, unicode.IsSpace)
	}
	return strings.TrimLeft(s, chars)
}

// StrRStrip ports str.rstrip.
//
// CPython: Objects/unicodeobject.c:L12737 unicode_rstrip_impl
func StrRStrip(s, chars string) string {
	if chars == "" {
		return strings.TrimRightFunc(s, unicode.IsSpace)
	}
	return strings.TrimRight(s, chars)
}

// Case operations. Go's strings package routes through unicode case
// mappings, which match CPython for the vast majority of inputs.
// Edge cases like the Greek final-sigma in lower() are subtly
// different and tracked in 1677-D.
//
// CPython: Objects/unicodeobject.c:L12559 unicode_lower_impl
// CPython: Objects/unicodeobject.c:L11042 unicode_upper_impl
func StrLower(s string) string { return strings.ToLower(s) }
func StrUpper(s string) string { return strings.ToUpper(s) }

// StrCaseFold approximates str.casefold via lowercasing.
//
// CPython: Objects/unicodeobject.c:L11122 unicode_casefold_impl
func StrCaseFold(s string) string { return strings.ToLower(s) }

// StrSwapCase swaps each character's case.
//
// CPython: Objects/unicodeobject.c:L11130 unicode_swapcase_impl
func StrSwapCase(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case unicode.IsUpper(r):
			out = append(out, unicode.ToLower(r))
		case unicode.IsLower(r):
			out = append(out, unicode.ToUpper(r))
		default:
			out = append(out, r)
		}
	}
	return string(out)
}

// StrCapitalize ports str.capitalize: first character upper, rest
// lower. Empty string is unchanged.
//
// CPython: Objects/unicodeobject.c:L11107 unicode_capitalize_impl
func StrCapitalize(s string) string {
	if s == "" {
		return s
	}
	rs := []rune(s)
	rs[0] = unicode.ToUpper(rs[0])
	for i := 1; i < len(rs); i++ {
		rs[i] = unicode.ToLower(rs[i])
	}
	return string(rs)
}

// StrTitle ports str.title: first character of each word uppercased,
// the rest lowercased. Word boundaries are runs of cased characters.
//
// CPython: Objects/unicodeobject.c:L11091 unicode_title_impl
func StrTitle(s string) string {
	out := make([]rune, 0, len(s))
	prevCased := false
	for _, r := range s {
		isCased := unicode.IsUpper(r) || unicode.IsLower(r) || unicode.IsTitle(r)
		switch {
		case isCased && !prevCased:
			out = append(out, unicode.ToUpper(r))
		case isCased:
			out = append(out, unicode.ToLower(r))
		default:
			out = append(out, r)
		}
		prevCased = isCased
	}
	return string(out)
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

func StrIsLower(s string) bool {
	hasCased := false
	for _, r := range s {
		if unicode.IsUpper(r) || unicode.IsTitle(r) {
			return false
		}
		if unicode.IsLower(r) {
			hasCased = true
		}
	}
	return hasCased
}

func StrIsUpper(s string) bool {
	hasCased := false
	for _, r := range s {
		if unicode.IsLower(r) || unicode.IsTitle(r) {
			return false
		}
		if unicode.IsUpper(r) {
			hasCased = true
		}
	}
	return hasCased
}

func StrIsTitle(s string) bool {
	prevCased := false
	hasCased := false
	for _, r := range s {
		switch {
		case unicode.IsUpper(r) || unicode.IsTitle(r):
			if prevCased {
				return false
			}
			prevCased = true
			hasCased = true
		case unicode.IsLower(r):
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

func StrIsSpace(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

func StrIsAlpha(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return true
}

func StrIsAlnum(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsNumber(r) {
			return false
		}
	}
	return true
}

func StrIsDecimal(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func StrIsDigit(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) && !unicode.IsNumber(r) {
			return false
		}
	}
	return true
}

func StrIsNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsNumber(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func StrIsIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if !unicode.IsLetter(r) && r != '_' {
				return false
			}
			continue
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			return false
		}
	}
	return true
}

func StrIsPrintable(s string) bool {
	for _, r := range s {
		if !unicode.IsPrint(r) && r != ' ' {
			return false
		}
	}
	return true
}
