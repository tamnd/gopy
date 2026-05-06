// The bytes-method panel: split/rsplit/splitlines, partition/
// rpartition, join, find/rfind/index/rindex/count, replace, strip/
// lstrip/rstrip, translate/maketrans, startswith/endswith,
// expandtabs, center/ljust/rjust/zfill, and hex/fromhex.
//
// In CPython most of these live in stringlib templates that get
// instantiated for both bytes and bytearray. The gopy port keeps
// them as plain Go functions taking []byte and returning either
// []byte or []int. The Bytes-typed wrappers below adapt to *Bytes
// in / out; bytearray.go can call the same helpers when its own
// method panel lands.
//
// CPython: Objects/bytesobject.c:2664 bytes_methods
// CPython: Objects/stringlib/find.h, split.h, transmogrify.h,
//          partition.h, fastsearch.h

package objects

import (
	"bytes"
	"fmt"
)

// asBytesLike pulls out the byte payload from any object that
// supports the buffer protocol in CPython (bytes, bytearray). Other
// types return ok=false; the caller decides whether that's a
// TypeError or a different code path.
//
// CPython: Objects/abstract.c PyObject_GetBuffer (PyBUF_SIMPLE)
func asBytesLike(o Object) ([]byte, bool) {
	switch x := o.(type) {
	case *Bytes:
		return x.v, true
	case *ByteArray:
		return x.v, true
	}
	return nil, false
}

// clampBytesSlice maps Python's optional [start:end] slice arguments
// onto a real [lo, hi) range against a payload of length n. Negative
// values count from the end; values past the bounds are clamped. A
// missing argument is signalled by start=0, end=intMax (matching the
// clinic defaults of bytes.find / bytes.count).
//
// CPython: Objects/bytesobject.c:2117 ADJUST_INDICES (the macro
// expanded inside _Py_bytes_find/_Py_bytes_count)
func clampBytesSlice(n, start, end int) (int, int) {
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
	if start > n {
		start = n
	}
	if end < start {
		end = start
	}
	return start, end
}

const bytesMaxIndex = int(^uint(0) >> 1)

// Find returns the lowest index in b where sub is found within
// b[start:end], or -1 when not present. Matches bytes.find.
//
// CPython: Objects/bytesobject.c:1931 bytes_find_impl
func (b *Bytes) Find(sub []byte, start, end int) int {
	lo, hi := clampBytesSlice(len(b.v), start, end)
	idx := bytes.Index(b.v[lo:hi], sub)
	if idx < 0 {
		return -1
	}
	return lo + idx
}

// RFind returns the highest index in b where sub starts, or -1.
//
// CPython: Objects/bytesobject.c:1969 bytes_rfind_impl
func (b *Bytes) RFind(sub []byte, start, end int) int {
	lo, hi := clampBytesSlice(len(b.v), start, end)
	idx := bytes.LastIndex(b.v[lo:hi], sub)
	if idx < 0 {
		return -1
	}
	return lo + idx
}

// Index is Find but raises ValueError when the substring is missing.
//
// CPython: Objects/bytesobject.c:1990 bytes_index_impl
func (b *Bytes) Index(sub []byte, start, end int) (int, error) {
	i := b.Find(sub, start, end)
	if i < 0 {
		return -1, fmt.Errorf("ValueError: subsection not found")
	}
	return i, nil
}

// RIndex is RFind but raises ValueError when the substring is missing.
//
// CPython: Objects/bytesobject.c:2009 bytes_rindex_impl
func (b *Bytes) RIndex(sub []byte, start, end int) (int, error) {
	i := b.RFind(sub, start, end)
	if i < 0 {
		return -1, fmt.Errorf("ValueError: subsection not found")
	}
	return i, nil
}

// Count returns the number of non-overlapping occurrences of sub in
// b[start:end]. The empty-needle case is len(slice)+1, matching both
// CPython and Go's bytes.Count.
//
// CPython: Objects/bytesobject.c:2131 bytes_count_impl
func (b *Bytes) Count(sub []byte, start, end int) int {
	lo, hi := clampBytesSlice(len(b.v), start, end)
	return bytes.Count(b.v[lo:hi], sub)
}

// StartsWith reports whether b[start:end] begins with prefix. CPython
// also accepts a tuple of prefixes; that variant is a follow-up.
//
// CPython: Objects/bytesobject.c:2411 bytes_startswith_impl
func (b *Bytes) StartsWith(prefix []byte, start, end int) bool {
	lo, hi := clampBytesSlice(len(b.v), start, end)
	if hi-lo < len(prefix) {
		return false
	}
	return bytes.HasPrefix(b.v[lo:hi], prefix)
}

// EndsWith reports whether b[start:end] ends with suffix.
//
// CPython: Objects/bytesobject.c:2434 bytes_endswith_impl
func (b *Bytes) EndsWith(suffix []byte, start, end int) bool {
	lo, hi := clampBytesSlice(len(b.v), start, end)
	if hi-lo < len(suffix) {
		return false
	}
	return bytes.HasSuffix(b.v[lo:hi], suffix)
}

// isAsciiSpace returns true for the bytes Py_ISSPACE recognises:
// space, tab, newline, vertical tab, form feed, carriage return.
//
// CPython: Include/internal/pycore_ctype.h Py_ISSPACE
func isAsciiSpace(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\v', '\f', '\r':
		return true
	}
	return false
}

// Split splits the bytes on sep, with at most maxsplit cuts. A nil
// sep falls back to the whitespace split: runs of ASCII space are
// the delimiter and empty leading/trailing fields are dropped, as in
// "  a   b  ".split() == ["a", "b"]. maxsplit < 0 means unlimited.
//
// CPython: Objects/bytesobject.c:1768 bytes_split_impl
// CPython: Objects/stringlib/split.h:60 stringlib_split (and
//          stringlib_split_whitespace)
func (b *Bytes) Split(sep []byte, maxsplit int) ([]*Bytes, error) {
	if sep == nil {
		return splitWhitespace(b.v, maxsplit, false), nil
	}
	if len(sep) == 0 {
		return nil, fmt.Errorf("ValueError: empty separator")
	}
	if maxsplit < 0 {
		maxsplit = bytesMaxIndex
	}
	parts := bytes.SplitN(b.v, sep, maxsplit+1)
	out := make([]*Bytes, len(parts))
	for i, p := range parts {
		out[i] = NewBytes(p)
	}
	return out, nil
}

// RSplit is Split that walks from the end. It only differs from Split
// when maxsplit is positive: the rightmost cuts are kept.
//
// CPython: Objects/bytesobject.c:1853 bytes_rsplit_impl
// CPython: Objects/stringlib/split.h:154 stringlib_rsplit
func (b *Bytes) RSplit(sep []byte, maxsplit int) ([]*Bytes, error) {
	if sep == nil {
		return splitWhitespace(b.v, maxsplit, true), nil
	}
	if len(sep) == 0 {
		return nil, fmt.Errorf("ValueError: empty separator")
	}
	if maxsplit < 0 || maxsplit >= len(b.v) {
		return b.Split(sep, maxsplit)
	}
	out := make([]*Bytes, 0, maxsplit+1)
	cur := b.v
	for i := 0; i < maxsplit; i++ {
		idx := bytes.LastIndex(cur, sep)
		if idx < 0 {
			break
		}
		out = append(out, NewBytes(cur[idx+len(sep):]))
		cur = cur[:idx]
	}
	out = append(out, NewBytes(cur))
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// splitWhitespace cuts on runs of ASCII space and drops the empty
// fragments that show up at the very ends. With maxsplit set, a
// forward pass keeps the leftmost maxsplit cuts; reverse=true keeps
// the rightmost cuts (the rsplit case).
//
// CPython: Objects/stringlib/split.h:8 STRIPSPACE / split_whitespace
//          and rsplit_whitespace
func splitWhitespace(v []byte, maxsplit int, reverse bool) []*Bytes {
	if maxsplit < 0 {
		maxsplit = bytesMaxIndex
	}
	if !reverse {
		var out []*Bytes
		i := 0
		n := len(v)
		for i < n {
			for i < n && isAsciiSpace(v[i]) {
				i++
			}
			if i >= n {
				break
			}
			if len(out) >= maxsplit {
				out = append(out, NewBytes(v[i:]))
				return out
			}
			j := i
			for j < n && !isAsciiSpace(v[j]) {
				j++
			}
			out = append(out, NewBytes(v[i:j]))
			i = j
		}
		return out
	}
	var out []*Bytes
	n := len(v)
	j := n
	for j > 0 {
		for j > 0 && isAsciiSpace(v[j-1]) {
			j--
		}
		if j <= 0 {
			break
		}
		if len(out) >= maxsplit {
			out = append(out, NewBytes(v[:j]))
			break
		}
		i := j
		for i > 0 && !isAsciiSpace(v[i-1]) {
			i--
		}
		out = append(out, NewBytes(v[i:j]))
		j = i
	}
	for i, k := 0, len(out)-1; i < k; i, k = i+1, k-1 {
		out[i], out[k] = out[k], out[i]
	}
	return out
}

// SplitLines breaks b at \n, \r, or \r\n. With keepends=true the
// terminator is kept on the line; otherwise it's dropped. Mirrors
// bytes.splitlines, which only treats CR/LF as line breaks (the
// universal newline set str.splitlines uses is wider).
//
// CPython: Objects/bytesobject.c:2480 bytes_splitlines_impl
// CPython: Objects/stringlib/split.h:343 stringlib_splitlines
func (b *Bytes) SplitLines(keepends bool) []*Bytes {
	out := []*Bytes{}
	v := b.v
	n := len(v)
	i, j := 0, 0
	for i < n {
		for i < n && v[i] != '\n' && v[i] != '\r' {
			i++
		}
		eol := i
		if i < n {
			if v[i] == '\r' && i+1 < n && v[i+1] == '\n' {
				i += 2
			} else {
				i++
			}
			if keepends {
				eol = i
			}
		}
		out = append(out, NewBytes(v[j:eol]))
		j = i
	}
	return out
}

// Partition cuts b at the first occurrence of sep into (head, sep,
// tail). When sep doesn't occur, the result is (b, b'', b''). An
// empty sep is a ValueError.
//
// CPython: Objects/bytesobject.c:1807 bytes_partition_impl
// CPython: Objects/stringlib/partition.h:13 stringlib_partition
func (b *Bytes) Partition(sep []byte) (*Bytes, *Bytes, *Bytes, error) {
	if len(sep) == 0 {
		return nil, nil, nil, fmt.Errorf("ValueError: empty separator")
	}
	idx := bytes.Index(b.v, sep)
	if idx < 0 {
		return NewBytes(b.v), EmptyBytes(), EmptyBytes(), nil
	}
	return NewBytes(b.v[:idx]),
		NewBytes(sep),
		NewBytes(b.v[idx+len(sep):]),
		nil
}

// RPartition is Partition that searches from the right. The miss
// case mirrors CPython: (b'', b'', b) instead of (b, b'', b'').
//
// CPython: Objects/bytesobject.c:1834 bytes_rpartition_impl
// CPython: Objects/stringlib/partition.h:55 stringlib_rpartition
func (b *Bytes) RPartition(sep []byte) (*Bytes, *Bytes, *Bytes, error) {
	if len(sep) == 0 {
		return nil, nil, nil, fmt.Errorf("ValueError: empty separator")
	}
	idx := bytes.LastIndex(b.v, sep)
	if idx < 0 {
		return EmptyBytes(), EmptyBytes(), NewBytes(b.v), nil
	}
	return NewBytes(b.v[:idx]),
		NewBytes(sep),
		NewBytes(b.v[idx+len(sep):]),
		nil
}

// Join concatenates the items of seq with b as the separator. Every
// item must be bytes-like; CPython raises TypeError on the first
// other type and includes the item index in the message.
//
// CPython: Objects/bytesobject.c:1892 bytes_join_impl
// CPython: Objects/stringlib/join.h:13 stringlib_bytes_join
func (b *Bytes) Join(seq []Object) (*Bytes, error) {
	if len(seq) == 0 {
		return EmptyBytes(), nil
	}
	parts := make([][]byte, len(seq))
	total := 0
	for i, it := range seq {
		buf, ok := asBytesLike(it)
		if !ok {
			return nil, fmt.Errorf("TypeError: sequence item %d: expected a bytes-like object, %s found", i, it.Type().Name)
		}
		parts[i] = buf
		total += len(buf)
	}
	total += (len(seq) - 1) * len(b.v)
	out := make([]byte, 0, total)
	for i, p := range parts {
		if i > 0 {
			out = append(out, b.v...)
		}
		out = append(out, p...)
	}
	return NewBytes(out), nil
}

// Replace returns a copy with at most count non-overlapping
// occurrences of old swapped for new. count<0 means replace all.
//
// CPython: Objects/bytesobject.c:2310 bytes_replace_impl
// CPython: Objects/stringlib/transmogrify.h:444 stringlib_replace
func (b *Bytes) Replace(old, new []byte, count int) *Bytes {
	if count < 0 {
		count = -1
	}
	return NewBytes(bytes.Replace(b.v, old, new, count))
}

// stripPredicate returns a function that reports whether c is in the
// strip set. With chars=nil the set is ASCII whitespace; otherwise
// any byte that appears in chars is in the set.
//
// CPython: Objects/bytesobject.c:1990 do_argstrip helpers
func stripPredicate(chars []byte) func(byte) bool {
	if chars == nil {
		return isAsciiSpace
	}
	var set [256]bool
	for _, c := range chars {
		set[c] = true
	}
	return func(c byte) bool { return set[c] }
}

// Strip removes leading and trailing bytes that match chars. A nil
// chars defaults to ASCII whitespace.
//
// CPython: Objects/bytesobject.c:2081 bytes_strip_impl
func (b *Bytes) Strip(chars []byte) *Bytes {
	in := stripPredicate(chars)
	lo, hi := 0, len(b.v)
	for lo < hi && in(b.v[lo]) {
		lo++
	}
	for hi > lo && in(b.v[hi-1]) {
		hi--
	}
	return NewBytes(b.v[lo:hi])
}

// LStrip strips only the leading run.
//
// CPython: Objects/bytesobject.c:2099 bytes_lstrip_impl
func (b *Bytes) LStrip(chars []byte) *Bytes {
	in := stripPredicate(chars)
	lo, hi := 0, len(b.v)
	for lo < hi && in(b.v[lo]) {
		lo++
	}
	return NewBytes(b.v[lo:hi])
}

// RStrip strips only the trailing run.
//
// CPython: Objects/bytesobject.c:2117 bytes_rstrip_impl
func (b *Bytes) RStrip(chars []byte) *Bytes {
	in := stripPredicate(chars)
	lo, hi := 0, len(b.v)
	for hi > lo && in(b.v[hi-1]) {
		hi--
	}
	return NewBytes(b.v[lo:hi])
}

// Translate maps each byte through table (a 256-byte lookup) and
// drops any byte that appears in delete. A nil table is the identity.
// CPython requires the table to be exactly 256 bytes when present.
//
// CPython: Objects/bytesobject.c:2155 bytes_translate_impl
func (b *Bytes) Translate(table, deleteChars []byte) (*Bytes, error) {
	if table != nil && len(table) != 256 {
		return nil, fmt.Errorf("ValueError: translation table must be 256 characters long")
	}
	var drop [256]bool
	for _, c := range deleteChars {
		drop[c] = true
	}
	out := make([]byte, 0, len(b.v))
	for _, c := range b.v {
		if drop[c] {
			continue
		}
		if table != nil {
			out = append(out, table[c])
		} else {
			out = append(out, c)
		}
	}
	return NewBytes(out), nil
}

// MakeBytesTrans builds a 256-byte translation table mapping frm[i]
// to to[i] for each i. Both must have the same length; CPython
// raises ValueError otherwise.
//
// CPython: Objects/bytes_methods.c:_Py_bytes_maketrans
func MakeBytesTrans(frm, to []byte) (*Bytes, error) {
	if len(frm) != len(to) {
		return nil, fmt.Errorf("ValueError: maketrans arguments must have same length")
	}
	tbl := make([]byte, 256)
	for i := range tbl {
		tbl[i] = byte(i)
	}
	for i := range frm {
		tbl[frm[i]] = to[i]
	}
	return NewBytes(tbl), nil
}

// ExpandTabs replaces tabs with enough spaces to advance to the next
// multiple of tabsize, counting from the start of the current line.
// tabsize <= 0 just removes tabs.
//
// CPython: Objects/stringlib/transmogrify.h:62 stringlib_expandtabs
func (b *Bytes) ExpandTabs(tabsize int) *Bytes {
	out := make([]byte, 0, len(b.v))
	col := 0
	for _, c := range b.v {
		switch c {
		case '\t':
			if tabsize > 0 {
				pad := tabsize - col%tabsize
				for i := 0; i < pad; i++ {
					out = append(out, ' ')
				}
				col += pad
			}
		case '\n', '\r':
			out = append(out, c)
			col = 0
		default:
			out = append(out, c)
			col++
		}
	}
	return NewBytes(out)
}

// Center returns b padded to width with fill on both sides. Extra
// padding when the gap is odd lands on the right, matching CPython.
//
// CPython: Objects/stringlib/transmogrify.h:174 stringlib_center
func (b *Bytes) Center(width int, fill byte) *Bytes {
	pad := width - len(b.v)
	if pad <= 0 {
		return NewBytes(b.v)
	}
	left := pad / 2
	if pad%2 == 1 && width%2 == 1 {
		left++
	}
	right := pad - left
	return NewBytes(padFill(b.v, left, right, fill))
}

// LJust pads on the right.
//
// CPython: Objects/stringlib/transmogrify.h:139 stringlib_ljust
func (b *Bytes) LJust(width int, fill byte) *Bytes {
	pad := width - len(b.v)
	if pad <= 0 {
		return NewBytes(b.v)
	}
	return NewBytes(padFill(b.v, 0, pad, fill))
}

// RJust pads on the left.
//
// CPython: Objects/stringlib/transmogrify.h:155 stringlib_rjust
func (b *Bytes) RJust(width int, fill byte) *Bytes {
	pad := width - len(b.v)
	if pad <= 0 {
		return NewBytes(b.v)
	}
	return NewBytes(padFill(b.v, pad, 0, fill))
}

// padFill returns a fresh slice with left fill bytes, the source,
// then right fill bytes.
func padFill(src []byte, left, right int, fill byte) []byte {
	out := make([]byte, left+len(src)+right)
	for i := 0; i < left; i++ {
		out[i] = fill
	}
	copy(out[left:], src)
	for i := left + len(src); i < len(out); i++ {
		out[i] = fill
	}
	return out
}

// ZFill pads on the left with '0', preserving a leading '+' or '-'.
//
// CPython: Objects/stringlib/transmogrify.h:225 stringlib_zfill
func (b *Bytes) ZFill(width int) *Bytes {
	pad := width - len(b.v)
	if pad <= 0 {
		return NewBytes(b.v)
	}
	out := make([]byte, width)
	idx := 0
	if len(b.v) > 0 && (b.v[0] == '+' || b.v[0] == '-') {
		out[0] = b.v[0]
		idx = 1
	}
	for i := idx; i < idx+pad; i++ {
		out[i] = '0'
	}
	if idx == 1 {
		copy(out[1+pad:], b.v[1:])
	} else {
		copy(out[pad:], b.v)
	}
	return NewBytes(out)
}

// hexAlphabet is the lowercase hex digits in the order CPython's
// _Py_strhex emits them.
const hexAlphabet = "0123456789abcdef"

// Hex returns the hexadecimal representation of b. With sep != 0 the
// runs of bytesPerSep bytes are joined by sep; positive bytesPerSep
// counts from the right, negative from the left. bytesPerSep == 0
// or sep == 0 means "no separator" (the simple b.hex() default).
//
// CPython: Objects/bytesobject.c:2647 bytes_hex_impl
// CPython: Python/pystrhex.c _Py_strhex_with_sep
func (b *Bytes) Hex(sep byte, bytesPerSep int) string {
	v := b.v
	if len(v) == 0 {
		return ""
	}
	if sep == 0 || bytesPerSep == 0 {
		out := make([]byte, len(v)*2)
		for i, c := range v {
			out[i*2] = hexAlphabet[c>>4]
			out[i*2+1] = hexAlphabet[c&0xf]
		}
		return string(out)
	}
	abs := bytesPerSep
	fromLeft := abs < 0
	if fromLeft {
		abs = -abs
	}
	var out []byte
	if fromLeft {
		for i, c := range v {
			if i > 0 && i%abs == 0 {
				out = append(out, sep)
			}
			out = append(out, hexAlphabet[c>>4], hexAlphabet[c&0xf])
		}
	} else {
		n := len(v)
		for i, c := range v {
			rem := n - i
			if i > 0 && rem%abs == 0 {
				out = append(out, sep)
			}
			out = append(out, hexAlphabet[c>>4], hexAlphabet[c&0xf])
		}
	}
	return string(out)
}

// BytesFromHex parses a hex string into a fresh bytes object.
// Whitespace between digit pairs is allowed (CPython documents this
// as "spaces between two numbers are accepted").
//
// CPython: Objects/bytesobject.c:2503 bytes_fromhex_impl
// CPython: Objects/bytesobject.c:2513 _PyBytes_FromHex
func BytesFromHex(s string) (*Bytes, error) {
	out := make([]byte, 0, len(s)/2)
	i, n := 0, len(s)
	for i < n {
		for i < n && isAsciiSpace(s[i]) {
			i++
		}
		if i >= n {
			break
		}
		top, ok := hexDigit(s[i])
		if !ok {
			return nil, fmt.Errorf("ValueError: non-hexadecimal number found in fromhex() arg at position %d", i)
		}
		i++
		if i >= n {
			return nil, fmt.Errorf("ValueError: fromhex() arg must contain an even number of hexadecimal digits")
		}
		bot, ok := hexDigit(s[i])
		if !ok {
			return nil, fmt.Errorf("ValueError: non-hexadecimal number found in fromhex() arg at position %d", i)
		}
		i++
		out = append(out, byte(top<<4|bot))
	}
	return NewBytes(out), nil
}

// hexDigit maps an ASCII hex character to its value, or false when
// the byte isn't a valid hex digit.
func hexDigit(c byte) (int, bool) {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0'), true
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10, true
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10, true
	}
	return 0, false
}
