// Descriptor wiring for the bytes / bytearray method panel. Every
// row in CPython's bytes_methods and bytearray_methods tables that
// produces a non-mutating result lives here. The handler functions
// inspect args[0] (the receiver) to know whether to return a *Bytes
// or a *ByteArray, so the same closure serves both types and we
// install identical descriptors on BytesType and ByteArrayType.
//
// Mutating bytearray methods (append, extend, insert, pop, remove,
// clear, reverse, copy, ...) live in bytearray_methods_descr.go.
//
// CPython: Objects/bytesobject.c:2665 bytes_methods
// CPython: Objects/bytearrayobject.c:2503 bytearray_methods

package objects

import (
	"errors"
	"fmt"
	"math"

	"github.com/tamnd/gopy/codecs"
)

// bytesLikeView returns the byte payload of self when self is *Bytes
// or *ByteArray. Otherwise it produces the CPython-shaped TypeError
// that descrobject.c emits when a method descriptor is called with
// the wrong receiver type.
func bytesLikeView(self Object, methodName string) ([]byte, error) {
	switch x := self.(type) {
	case *Bytes:
		return x.v, nil
	case *ByteArray:
		return x.v, nil
	}
	return nil, fmt.Errorf("TypeError: descriptor '%s' requires a 'bytes' or 'bytearray' object but received a '%s'",
		methodName, self.Type().Name)
}

// bytesLikeOf wraps buf into the same kind of object as template
// (*Bytes if template is *Bytes, *ByteArray if it's *ByteArray). Used
// by handlers that produce bytes-like results so bytes.lower() returns
// bytes and bytearray.lower() returns bytearray.
func bytesLikeOf(template Object, buf []byte) Object {
	if _, ok := template.(*ByteArray); ok {
		return NewByteArray(buf)
	}
	return NewBytes(buf)
}

// bytesArgAsBuffer coerces obj into a []byte. Accepts *Bytes and
// *ByteArray (the buffer protocol surface). Other types are a
// TypeError, mirroring CPython's PyObject_GetBuffer + PyBUF_SIMPLE.
func bytesArgAsBuffer(obj Object, methodName string) ([]byte, error) {
	if buf, ok := asBytesLike(obj); ok {
		return buf, nil
	}
	// PEP 688: a pure-Python object can expose the buffer protocol through
	// __buffer__(flags). PyObject_GetBuffer runs it (with PyBUF_SIMPLE = 0)
	// and reads the resulting view, so a re-entrant resize inside it can
	// raise BufferError under a buffer export.
	if m, err := LookupSpecial(obj, "__buffer__"); err == nil && m != nil {
		res, cerr := Call(m, NewTuple([]Object{NewInt(0)}), nil)
		if cerr != nil {
			return nil, cerr
		}
		if buf, ok := asBytesLike(res); ok {
			return buf, nil
		}
	}
	return nil, fmt.Errorf("TypeError: a bytes-like object is required, not '%s' (in %s)",
		obj.Type().Name, methodName)
}

// bytesIntArg pulls an integer out of args[i] when present, otherwise
// returns def. Out-of-range / non-int values produce the same
// "an integer is required" TypeError that CPython raises.
func bytesIntArg(args []Object, idx int, methodName string, def int) (int, error) {
	if idx >= len(args) || args[idx] == nil || args[idx] == None() {
		return def, nil
	}
	n, ok := args[idx].(*Int)
	if !ok {
		return 0, fmt.Errorf("TypeError: %s indices must be integers, not %s",
			methodName, args[idx].Type().Name)
	}
	v, fits := n.Int64()
	if !fits {
		return 0, fmt.Errorf("OverflowError: %s argument out of range", methodName)
	}
	if v > math.MaxInt32 {
		return bytesMaxIndex, nil
	}
	if v < math.MinInt32 {
		return -bytesMaxIndex, nil
	}
	return int(v), nil
}

// bytesSliceIndexArg pulls a start/end slice bound out of args[i]. Unlike
// bytesIntArg it mirrors CPython's slice_index converter: an out-of-range
// integer is clamped to the Py_ssize_t extremes rather than raising
// OverflowError, so b.find(sub, sys.maxsize+1, 0) returns -1 instead of
// blowing up.
//
// CPython: Python/ceval.c _PyEval_SliceIndex (the slice_index clinic converter)
func bytesSliceIndexArg(args []Object, idx int, methodName string, def int) (int, error) {
	if idx >= len(args) || args[idx] == nil || args[idx] == None() {
		return def, nil
	}
	n, ok := args[idx].(*Int)
	if !ok {
		return 0, fmt.Errorf("TypeError: %s indices must be integers, not %s",
			methodName, args[idx].Type().Name)
	}
	v, fits := n.Int64()
	if !fits {
		if n.Sign() < 0 {
			return -bytesMaxIndex, nil
		}
		return bytesMaxIndex, nil
	}
	if v > math.MaxInt32 {
		return bytesMaxIndex, nil
	}
	if v < math.MinInt32 {
		return -bytesMaxIndex, nil
	}
	return int(v), nil
}

// bytesSubArg unpacks the (sub) argument used by find/rfind/index/
// rindex/count: either an int byte value or a bytes-like sequence.
// The returned []byte is a single-byte slice when sub is an Int.
func bytesSubArg(obj Object, methodName string) ([]byte, error) {
	// parse_args_finds_byte checks the buffer protocol before __index__,
	// so a bytes-like needle wins over an object that is also index-able.
	if buf, ok := asBytesLike(obj); ok {
		return buf, nil
	}
	if IndexCheck(obj) {
		iv, err := NumberIndex(obj)
		if err != nil {
			return nil, err
		}
		n, fits := iv.(*Int).Int64()
		if !fits || n < 0 || n > 255 {
			return nil, errors.New("ValueError: byte must be in range(0, 256)")
		}
		return []byte{byte(n)}, nil
	}
	return nil, fmt.Errorf("TypeError: argument should be integer or bytes-like object, not '%s' (in %s)",
		obj.Type().Name, methodName)
}

// methodFn is the shared body shape for every bytes / bytearray
// method descriptor below.
type methodFn = func(args []Object, kwargs map[string]Object) (Object, error)

// arityErr is the CPython-shaped wrong-arg-count message.
func arityErr(name string, want, got int) error {
	return fmt.Errorf("TypeError: %s() takes %d positional argument(s) but %d were given", name, want, got)
}

// bindKwargs folds keyword arguments onto positional slots following the
// parameter order in names (names[0] is args[1], the first slot after the
// receiver). It mirrors how CPython's Argument Clinic binds a
// keyword-or-positional signature: a keyword that duplicates a position
// raises TypeError, an unknown keyword raises TypeError, and a missing
// leading slot becomes a nil so callers see "not supplied".
func bindKwargs(method string, args []Object, kwargs map[string]Object, names ...string) ([]Object, error) {
	if len(kwargs) == 0 {
		return args, nil
	}
	out := append([]Object(nil), args...)
	for k, val := range kwargs {
		idx := -1
		for i, n := range names {
			if n == k {
				idx = i + 1
				break
			}
		}
		if idx < 0 {
			return nil, fmt.Errorf("TypeError: '%s' is an invalid keyword argument for %s()", k, method)
		}
		if idx < len(args) {
			return nil, fmt.Errorf("TypeError: argument for %s() given by name ('%s') and position (%d)", method, k, idx)
		}
		for len(out) <= idx {
			out = append(out, nil)
		}
		out[idx] = val
	}
	return out, nil
}

// arityRangeErr reports "expected X to Y arguments, got Z".
func arityRangeErr(name string, lo, hi, got int) error {
	return fmt.Errorf("TypeError: %s expected %d to %d arguments, got %d", name, lo, hi, got)
}

// classifierMethod builds a NOARGS handler that returns Bool(pred(view)).
func classifierMethod(name string, pred func([]byte) bool) methodFn {
	return func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 1 {
			return nil, arityErr(name, 1, len(args))
		}
		v, err := bytesLikeView(args[0], name)
		if err != nil {
			return nil, err
		}
		return NewBool(pred(v)), nil
	}
}

// caseTransformMethod builds a NOARGS handler that maps view through
// fn and wraps the result in the same kind of bytes-like as args[0].
func caseTransformMethod(name string, fn func([]byte) []byte) methodFn {
	return func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 1 {
			return nil, arityErr(name, 1, len(args))
		}
		v, err := bytesLikeView(args[0], name)
		if err != nil {
			return nil, err
		}
		return bytesLikeOf(args[0], fn(v)), nil
	}
}

// findMethod backs find/rfind/index/rindex. The forward / reverse and
// raise-on-miss decisions come from flags; the rest is the same
// argument unpacking.
//
// CPython: Objects/bytesobject.c:1931 bytes_find_impl (+ rfind/index/rindex)
func findMethod(name string, reverse, raiseOnMiss bool) methodFn {
	return func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) < 2 || len(args) > 4 {
			return nil, arityRangeErr(name, 1, 3, len(args)-1)
		}
		v, err := bytesLikeView(args[0], name)
		if err != nil {
			return nil, err
		}
		// gh-142560: lock the receiver's buffer while the needle is
		// converted so a re-entrant resize (the needle's __index__ or
		// __buffer__ mutating self) raises BufferError.
		unlock := lockSearchSelf(args[0])
		sub, err := bytesSubArg(args[1], name)
		unlock()
		if err != nil {
			return nil, err
		}
		start, err := bytesSliceIndexArg(args, 2, name, 0)
		if err != nil {
			return nil, err
		}
		end, err := bytesSliceIndexArg(args, 3, name, bytesMaxIndex)
		if err != nil {
			return nil, err
		}
		lo, hi := adjustBytesIndices(len(v), start, end)
		idx := -1
		// CPython: Objects/bytes_methods.c:483 (end - start < sub_len) => -1.
		// This also keeps v[lo:hi] in bounds since hi <= len(v) and a
		// non-negative slice implies lo <= hi.
		if hi-lo >= len(sub) {
			if reverse {
				idx = lastIndex(v[lo:hi], sub)
			} else {
				idx = firstIndex(v[lo:hi], sub)
			}
			if idx >= 0 {
				idx += lo
			}
		}
		if idx >= 0 {
			return NewInt(int64(idx)), nil
		}
		if raiseOnMiss {
			return nil, fmt.Errorf("ValueError: subsection not found")
		}
		return NewInt(-1), nil
	}
}

// lockSearchSelf bumps the receiver's buffer-export count for the
// duration of search-argument conversion, mirroring CPython's
// _bytearray_with_buffer (it raises ob_exports around the find/count op).
// The returned closure drops the lock; it is a no-op for immutable bytes.
//
// CPython: Objects/bytearrayobject.c:90 _bytearray_with_buffer
func lockSearchSelf(self Object) func() {
	if ba, ok := self.(*ByteArray); ok {
		ba.ExportInc()
		return ba.ExportDec
	}
	return func() {}
}

// firstIndex returns the lowest index of sub in v, or -1. The empty
// needle matches at 0; otherwise it drives the stringlib fastsearch so
// large needles stay O(n + m) rather than quadratic.
//
// CPython: Objects/stringlib/find.h:7 STRINGLIB(find)
func firstIndex(v, sub []byte) int {
	if len(sub) == 0 {
		return 0
	}
	return stringlibFastSearch(v, sub, math.MaxInt, fastSearch)
}

// lastIndex returns the highest index of sub in v, or -1. The empty
// needle matches at len(v).
//
// CPython: Objects/stringlib/find.h:34 STRINGLIB(rfind)
func lastIndex(v, sub []byte) int {
	if len(sub) == 0 {
		return len(v)
	}
	return stringlibFastSearch(v, sub, math.MaxInt, fastRSearch)
}

// countMethod backs bytes.count / bytearray.count.
//
// CPython: Objects/bytesobject.c:2131 bytes_count_impl
func countMethod() methodFn {
	return func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) < 2 || len(args) > 4 {
			return nil, arityRangeErr("count", 1, 3, len(args)-1)
		}
		v, err := bytesLikeView(args[0], "count")
		if err != nil {
			return nil, err
		}
		unlock := lockSearchSelf(args[0])
		sub, err := bytesSubArg(args[1], "count")
		unlock()
		if err != nil {
			return nil, err
		}
		start, err := bytesSliceIndexArg(args, 2, "count", 0)
		if err != nil {
			return nil, err
		}
		end, err := bytesSliceIndexArg(args, 3, "count", bytesMaxIndex)
		if err != nil {
			return nil, err
		}
		lo, hi := adjustBytesIndices(len(v), start, end)
		// CPython: Objects/stringlib/count.h:13 (str_len < 0) => 0.
		if hi-lo < 0 {
			return NewInt(0), nil
		}
		slice := v[lo:hi]
		if len(sub) == 0 {
			return NewInt(int64(len(slice) + 1)), nil
		}
		// fastsearch FAST_COUNT returns -1 when no match is possible
		// (needle longer than haystack); normalize that to 0.
		// CPython: Objects/stringlib/count.h:13 STRINGLIB(count)
		count := max(stringlibFastSearch(slice, sub, math.MaxInt, fastCount), 0)
		return NewInt(int64(count)), nil
	}
}

// tailMatchMethod backs startswith / endswith. CPython accepts a tuple
// of candidates; the gopy port handles the bytes-like single-prefix
// case plus *Tuple-of-bytes-like, matching what re._compiler.py needs.
//
// CPython: Objects/bytesobject.c:2411 bytes_startswith_impl / 2434 endswith
func tailMatchMethod(name string, suffix bool) methodFn {
	return func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) < 2 || len(args) > 4 {
			return nil, arityRangeErr(name, 1, 3, len(args)-1)
		}
		v, err := bytesLikeView(args[0], name)
		if err != nil {
			return nil, err
		}
		start, err := bytesSliceIndexArg(args, 2, name, 0)
		if err != nil {
			return nil, err
		}
		end, err := bytesSliceIndexArg(args, 3, name, bytesMaxIndex)
		if err != nil {
			return nil, err
		}
		lo, hi := adjustBytesIndices(len(v), start, end)
		// CPython: Objects/bytes_methods.c:632 tailmatch. suffix==true is
		// endswith (direction +1), suffix==false is startswith (-1).
		check := func(buf []byte) bool {
			slen := len(buf)
			s := lo
			if suffix {
				if hi-s < slen || s > len(v) {
					return false
				}
				if hi-slen > s {
					s = hi - slen
				}
			} else {
				if s > len(v)-slen {
					return false
				}
			}
			if hi-s < slen {
				return false
			}
			return bytesEqualAt(v, s, buf)
		}
		if t, ok := args[1].(*Tuple); ok {
			for i := 0; i < t.Len(); i++ {
				buf, err := bytesArgAsBuffer(t.Item(i), name)
				if err != nil {
					return nil, err
				}
				if check(buf) {
					return NewBool(true), nil
				}
			}
			return NewBool(false), nil
		}
		buf, ok := asBytesLike(args[1])
		if !ok {
			// CPython: Objects/bytes_methods.c:702 reframes a failed buffer
			// fetch on the single-arg path into the tailmatch-specific
			// message that names both bytes and tuple.
			return nil, fmt.Errorf("TypeError: %s first arg must be bytes or a tuple of bytes, not %s",
				name, args[1].Type().Name)
		}
		return NewBool(check(buf)), nil
	}
}

func bytesEqualAt(v []byte, off int, sub []byte) bool {
	if off < 0 || off+len(sub) > len(v) {
		return false
	}
	for i, c := range sub {
		if v[off+i] != c {
			return false
		}
	}
	return true
}

// joinMethod backs bytes.join / bytearray.join. Items in the iterable
// must be bytes-like; index appears in the TypeError message.
//
// CPython: Objects/bytesobject.c:1892 bytes_join_impl
func joinMethod() methodFn {
	return func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 2 {
			return nil, arityErr("join", 2, len(args))
		}
		sep, err := bytesLikeView(args[0], "join")
		if err != nil {
			return nil, err
		}
		// Lock the separator's buffer for the whole join: bytearray.join
		// bumps ob_exports so an iterator that mutates the receiver
		// mid-iteration (e.g. self.clear()) raises BufferError instead
		// of corrupting the in-flight read.
		//
		// CPython: Objects/bytearrayobject.c:2370 bytearray_join (ob_exports)
		if ba, ok := args[0].(*ByteArray); ok {
			ba.ExportInc()
			defer ba.ExportDec()
		}
		items, err := IterToSlice(args[1])
		if err != nil {
			return nil, err
		}
		parts := make([][]byte, len(items))
		total := 0
		for i, it := range items {
			buf, ok := asBytesLike(it)
			if !ok {
				return nil, fmt.Errorf("TypeError: sequence item %d: expected a bytes-like object, %s found",
					i, it.Type().Name)
			}
			parts[i] = buf
			total += len(buf)
		}
		if len(parts) > 0 {
			total += (len(parts) - 1) * len(sep)
		}
		out := make([]byte, 0, total)
		for i, p := range parts {
			if i > 0 {
				out = append(out, sep...)
			}
			out = append(out, p...)
		}
		return bytesLikeOf(args[0], out), nil
	}
}

// stripMethod backs strip / lstrip / rstrip. side picks the trim end:
// 'l', 'r', or 'b' for both.
//
// CPython: Objects/bytesobject.c:2081 bytes_strip_impl (+ lstrip/rstrip)
func stripMethod(name string, side byte) methodFn {
	return func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) < 1 || len(args) > 2 {
			return nil, arityRangeErr(name, 0, 1, len(args)-1)
		}
		v, err := bytesLikeView(args[0], name)
		if err != nil {
			return nil, err
		}
		var chars []byte
		if len(args) == 2 && args[1] != nil && args[1] != None() {
			chars, err = bytesArgAsBuffer(args[1], name)
			if err != nil {
				return nil, err
			}
		}
		in := stripPredicate(chars)
		lo, hi := 0, len(v)
		if side == 'l' || side == 'b' {
			for lo < hi && in(v[lo]) {
				lo++
			}
		}
		if side == 'r' || side == 'b' {
			for hi > lo && in(v[hi-1]) {
				hi--
			}
		}
		out := make([]byte, hi-lo)
		copy(out, v[lo:hi])
		return bytesLikeOf(args[0], out), nil
	}
}

// replaceMethod backs bytes.replace / bytearray.replace.
//
// CPython: Objects/bytesobject.c:2310 bytes_replace_impl
func replaceMethod() methodFn {
	return func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) < 3 || len(args) > 4 {
			return nil, arityRangeErr("replace", 2, 3, len(args)-1)
		}
		v, err := bytesLikeView(args[0], "replace")
		if err != nil {
			return nil, err
		}
		old, err := bytesArgAsBuffer(args[1], "replace")
		if err != nil {
			return nil, err
		}
		repl, err := bytesArgAsBuffer(args[2], "replace")
		if err != nil {
			return nil, err
		}
		count, err := bytesIntArg(args, 3, "replace", -1)
		if err != nil {
			return nil, err
		}
		return bytesLikeOf(args[0], bytesReplaceN(v, old, repl, count)), nil
	}
}

// bytesReplaceN replaces up to count occurrences of old with repl.
// count<0 means replace all. Mirrors stringlib_replace.
//
// CPython: Objects/stringlib/transmogrify.h:444 stringlib_replace
func bytesReplaceN(v, old, repl []byte, count int) []byte {
	if count == 0 {
		out := make([]byte, len(v))
		copy(out, v)
		return out
	}
	if len(old) == 0 {
		// Insert repl between every byte and at the ends.
		var out []byte
		if count < 0 {
			count = len(v) + 1
		}
		for i, c := range v {
			if i < count {
				out = append(out, repl...)
			}
			out = append(out, c)
		}
		if len(v) < count {
			out = append(out, repl...)
		}
		return out
	}
	var out []byte
	i := 0
	for i <= len(v)-len(old) {
		match := true
		for j, c := range old {
			if v[i+j] != c {
				match = false
				break
			}
		}
		if match && count != 0 {
			out = append(out, repl...)
			i += len(old)
			if count > 0 {
				count--
			}
			continue
		}
		out = append(out, v[i])
		i++
	}
	out = append(out, v[i:]...)
	return out
}

// splitMethod backs bytes.split / bytearray.split. reverse=true is
// the rsplit variant.
//
// CPython: Objects/bytesobject.c:1768 bytes_split_impl / 1853 rsplit_impl
func splitMethod(name string, reverse bool) methodFn {
	return func(args []Object, kwargs map[string]Object) (Object, error) {
		args, err := bindKwargs(name, args, kwargs, "sep", "maxsplit")
		if err != nil {
			return nil, err
		}
		if len(args) < 1 || len(args) > 3 {
			return nil, arityRangeErr(name, 0, 2, len(args)-1)
		}
		v, err := bytesLikeView(args[0], name)
		if err != nil {
			return nil, err
		}
		var sep []byte
		hasSep := false
		if len(args) >= 2 && args[1] != nil && args[1] != None() {
			// gh-142560: lock the receiver while the separator buffer is
			// acquired so a re-entrant resize inside sep.__buffer__ raises
			// BufferError (CPython bumps ob_exports around the GetBuffer).
			unlock := lockSearchSelf(args[0])
			sep, err = bytesArgAsBuffer(args[1], name)
			unlock()
			if err != nil {
				return nil, err
			}
			hasSep = true
		}
		maxsplit, err := bytesIntArg(args, 2, name, -1)
		if err != nil {
			return nil, err
		}
		if !hasSep {
			parts := splitWhitespace(v, maxsplit, reverse)
			items := make([]Object, len(parts))
			for i, p := range parts {
				items[i] = bytesLikeOf(args[0], p.v)
			}
			return NewList(items), nil
		}
		if len(sep) == 0 {
			return nil, fmt.Errorf("ValueError: empty separator")
		}
		var raw [][]byte
		if reverse {
			raw = splitOnSepRight(v, sep, maxsplit)
		} else {
			raw = splitOnSep(v, sep, maxsplit)
		}
		items := make([]Object, len(raw))
		for i, p := range raw {
			items[i] = bytesLikeOf(args[0], p)
		}
		return NewList(items), nil
	}
}

func splitOnSep(v, sep []byte, maxsplit int) [][]byte {
	if maxsplit < 0 {
		maxsplit = bytesMaxIndex
	}
	var out [][]byte
	start := 0
	splits := 0
	for splits < maxsplit && start <= len(v)-len(sep) {
		i := firstIndex(v[start:], sep)
		if i < 0 {
			break
		}
		out = append(out, v[start:start+i])
		start += i + len(sep)
		splits++
	}
	out = append(out, v[start:])
	return out
}

func splitOnSepRight(v, sep []byte, maxsplit int) [][]byte {
	// rsplit must always scan right-to-left: with overlapping matches
	// (e.g. "bb" in "abbbc") the rightmost match differs from the leftmost,
	// so delegating to the forward splitter would give the wrong partition.
	if maxsplit < 0 {
		maxsplit = bytesMaxIndex
	}
	var rev [][]byte
	end := len(v)
	splits := 0
	for splits < maxsplit {
		i := lastIndex(v[:end], sep)
		if i < 0 {
			break
		}
		rev = append(rev, v[i+len(sep):end])
		end = i
		splits++
	}
	rev = append(rev, v[:end])
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev
}

// splitLinesMethod backs splitlines (CR / LF / CRLF terminators).
//
// CPython: Objects/bytesobject.c:2480 bytes_splitlines_impl
func splitLinesMethod() methodFn {
	return func(args []Object, kwargs map[string]Object) (Object, error) {
		if len(args) < 1 || len(args) > 2 {
			return nil, arityRangeErr("splitlines", 0, 1, len(args)-1)
		}
		v, err := bytesLikeView(args[0], "splitlines")
		if err != nil {
			return nil, err
		}
		keepends := false
		if len(args) == 2 && args[1] != nil && args[1] != None() {
			keepends = IsTrue(args[1])
		}
		// CPython parses `keepends` as either positional or keyword; the
		// `bytes.splitlines` signature in Objects/clinic/bytesobject.c.h
		// is `*, keepends=False`.
		if kw, ok := kwargs["keepends"]; ok {
			if len(args) == 2 {
				return nil, fmt.Errorf("TypeError: splitlines() got multiple values for argument 'keepends'")
			}
			if kw != nil && kw != None() {
				keepends = IsTrue(kw)
			}
		}
		raw := splitLines(v, keepends)
		items := make([]Object, len(raw))
		for i, p := range raw {
			items[i] = bytesLikeOf(args[0], p)
		}
		return NewList(items), nil
	}
}

func splitLines(v []byte, keepends bool) [][]byte {
	var out [][]byte
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
		out = append(out, append([]byte(nil), v[j:eol]...))
		j = i
	}
	return out
}

// partitionMethod backs partition / rpartition.
//
// CPython: Objects/bytesobject.c:1807 bytes_partition_impl
func partitionMethod(name string, reverse bool) methodFn {
	return func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 2 {
			return nil, arityErr(name, 2, len(args))
		}
		v, err := bytesLikeView(args[0], name)
		if err != nil {
			return nil, err
		}
		sep, err := bytesArgAsBuffer(args[1], name)
		if err != nil {
			return nil, err
		}
		if len(sep) == 0 {
			return nil, fmt.Errorf("ValueError: empty separator")
		}
		var idx int
		if reverse {
			idx = lastIndex(v, sep)
		} else {
			idx = firstIndex(v, sep)
		}
		if idx < 0 {
			if reverse {
				return NewTuple([]Object{
					bytesLikeOf(args[0], nil),
					bytesLikeOf(args[0], nil),
					bytesLikeOf(args[0], v),
				}), nil
			}
			return NewTuple([]Object{
				bytesLikeOf(args[0], v),
				bytesLikeOf(args[0], nil),
				bytesLikeOf(args[0], nil),
			}), nil
		}
		head := append([]byte(nil), v[:idx]...)
		tail := append([]byte(nil), v[idx+len(sep):]...)
		return NewTuple([]Object{
			bytesLikeOf(args[0], head),
			bytesLikeOf(args[0], sep),
			bytesLikeOf(args[0], tail),
		}), nil
	}
}

// translateMethod backs bytes.translate / bytearray.translate.
//
// CPython: Objects/bytesobject.c:2155 bytes_translate_impl
func translateMethod() methodFn {
	return func(args []Object, kwargs map[string]Object) (Object, error) {
		if len(args) < 2 || len(args) > 3 {
			return nil, arityRangeErr("translate", 1, 2, len(args)-1)
		}
		v, err := bytesLikeView(args[0], "translate")
		if err != nil {
			return nil, err
		}
		var table []byte
		if args[1] != nil && args[1] != None() {
			table, err = bytesArgAsBuffer(args[1], "translate")
			if err != nil {
				return nil, err
			}
			if len(table) != 256 {
				return nil, fmt.Errorf("ValueError: translation table must be 256 characters long")
			}
		}
		// deletechars accepts None for the table but NOT for the delete
		// argument: bytes_translate_impl only special-cases Py_None for the
		// table. A delete value of None falls through to PyObject_GetBuffer,
		// which raises TypeError. So once delete is supplied (positionally or
		// by keyword) it must be a bytes-like object even if it is None.
		//
		// CPython: Objects/bytesobject.c:2193 bytes_translate_impl
		var del []byte
		if len(args) == 3 && args[2] != nil {
			del, err = bytesArgAsBuffer(args[2], "translate")
			if err != nil {
				return nil, err
			}
		}
		if d, ok := kwargs["delete"]; ok && d != nil {
			del, err = bytesArgAsBuffer(d, "translate")
			if err != nil {
				return nil, err
			}
		}
		var drop [256]bool
		for _, c := range del {
			drop[c] = true
		}
		out := make([]byte, 0, len(v))
		for _, c := range v {
			if drop[c] {
				continue
			}
			if table != nil {
				out = append(out, table[c])
			} else {
				out = append(out, c)
			}
		}
		return bytesLikeOf(args[0], out), nil
	}
}

// expandTabsMethod backs bytes.expandtabs / bytearray.expandtabs.
//
// CPython: Objects/stringlib/transmogrify.h:62 stringlib_expandtabs
func expandTabsMethod() methodFn {
	return func(args []Object, kwargs map[string]Object) (Object, error) {
		args, err := bindKwargs("expandtabs", args, kwargs, "tabsize")
		if err != nil {
			return nil, err
		}
		if len(args) < 1 || len(args) > 2 {
			return nil, arityRangeErr("expandtabs", 0, 1, len(args)-1)
		}
		v, err := bytesLikeView(args[0], "expandtabs")
		if err != nil {
			return nil, err
		}
		tabsize := 8
		if len(args) == 2 && args[1] != nil && args[1] != None() {
			tabsize, err = bytesIntArg(args, 1, "expandtabs", 8)
			if err != nil {
				return nil, err
			}
		}
		out := make([]byte, 0, len(v))
		col := 0
		for _, c := range v {
			switch c {
			case '\t':
				if tabsize > 0 {
					pad := tabsize - col%tabsize
					for range pad {
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
		return bytesLikeOf(args[0], out), nil
	}
}

// padMethod backs center / ljust / rjust. side picks the pad layout.
//
// CPython: Objects/stringlib/transmogrify.h:174 stringlib_center / ljust / rjust
func padMethod(name string, side byte) methodFn {
	return func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) < 2 || len(args) > 3 {
			return nil, arityRangeErr(name, 1, 2, len(args)-1)
		}
		v, err := bytesLikeView(args[0], name)
		if err != nil {
			return nil, err
		}
		width, err := bytesIntArg(args, 1, name, 0)
		if err != nil {
			return nil, err
		}
		fill := byte(' ')
		if len(args) == 3 && args[2] != nil && args[2] != None() {
			buf, err := bytesArgAsBuffer(args[2], name)
			if err != nil {
				return nil, err
			}
			if len(buf) != 1 {
				return nil, fmt.Errorf("TypeError: %s() argument 2 must be a byte string of length 1, not a bytes-like of length %d",
					name, len(buf))
			}
			fill = buf[0]
		}
		pad := width - len(v)
		if pad <= 0 {
			out := make([]byte, len(v))
			copy(out, v)
			return bytesLikeOf(args[0], out), nil
		}
		var left, right int
		switch side {
		case 'l':
			right = pad
		case 'r':
			left = pad
		default:
			left = pad / 2
			if pad%2 == 1 && width%2 == 1 {
				left++
			}
			right = pad - left
		}
		return bytesLikeOf(args[0], padFill(v, left, right, fill)), nil
	}
}

// zfillMethod backs bytes.zfill / bytearray.zfill.
//
// CPython: Objects/stringlib/transmogrify.h:225 stringlib_zfill
func zfillMethod() methodFn {
	return func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 2 {
			return nil, arityErr("zfill", 2, len(args))
		}
		v, err := bytesLikeView(args[0], "zfill")
		if err != nil {
			return nil, err
		}
		width, err := bytesIntArg(args, 1, "zfill", 0)
		if err != nil {
			return nil, err
		}
		pad := width - len(v)
		if pad <= 0 {
			out := make([]byte, len(v))
			copy(out, v)
			return bytesLikeOf(args[0], out), nil
		}
		out := make([]byte, width)
		idx := 0
		if len(v) > 0 && (v[0] == '+' || v[0] == '-') {
			out[0] = v[0]
			idx = 1
		}
		for i := idx; i < idx+pad; i++ {
			out[i] = '0'
		}
		if idx == 1 {
			copy(out[1+pad:], v[1:])
		} else {
			copy(out[pad:], v)
		}
		return bytesLikeOf(args[0], out), nil
	}
}

// hexMethod backs bytes.hex / bytearray.hex.
//
// CPython: Objects/bytesobject.c:2647 bytes_hex_impl
func hexMethod() methodFn {
	return func(args []Object, kwargs map[string]Object) (Object, error) {
		args, err := bindKwargs("hex", args, kwargs, "sep", "bytes_per_sep")
		if err != nil {
			return nil, err
		}
		if len(args) < 1 || len(args) > 3 {
			return nil, arityRangeErr("hex", 0, 2, len(args)-1)
		}
		v, err := bytesLikeView(args[0], "hex")
		if err != nil {
			return nil, err
		}
		var sep byte
		hasSep := false
		bytesPerSep := 1
		if len(args) >= 2 && args[1] != nil {
			// _Py_strhex_impl reads PyObject_Length(sep) before inspecting
			// the type, so a re-entrant __len__ (or a None argument) is
			// observed here. Locking the receiver makes a mutation inside
			// that __len__ raise BufferError (gh-143195).
			unlock := lockSearchSelf(args[0])
			s, perr := hexSepByte(args[1])
			unlock()
			if perr != nil {
				return nil, perr
			}
			sep, hasSep = s, true
		}
		if len(args) == 3 && args[2] != nil && args[2] != None() {
			bytesPerSep, err = bytesIntArg(args, 2, "hex", 1)
			if err != nil {
				return nil, err
			}
		}
		return NewStr(hexEncode(v, sep, hasSep, bytesPerSep)), nil
	}
}

// hexSepByte validates a hex() separator and returns its single byte.
// PyObject_Length runs first (so None or a re-entrant __len__ is observed),
// then the str / bytes type split decides the byte value. A non-ASCII
// separator is rejected because hex() produces a str (return_bytes == 0).
//
// CPython: Python/pystrhex.c:7 _Py_strhex_impl
func hexSepByte(sepObj Object) (byte, error) {
	seplen, err := Length(sepObj)
	if err != nil {
		return 0, err
	}
	if seplen != 1 {
		return 0, errors.New("ValueError: sep must be length 1.")
	}
	if uni, ok := sepObj.(*Unicode); ok {
		r := []rune(uni.Value())[0]
		if r > 127 {
			return 0, errors.New("ValueError: sep must be ASCII.")
		}
		return byte(r), nil
	}
	if buf, ok := asBytesLike(sepObj); ok {
		if buf[0] > 127 {
			return 0, errors.New("ValueError: sep must be ASCII.")
		}
		return buf[0], nil
	}
	return 0, errors.New("TypeError: sep must be str or bytes.")
}

func hexEncode(v []byte, sep byte, hasSep bool, bytesPerSep int) string {
	if len(v) == 0 {
		return ""
	}
	if !hasSep || bytesPerSep == 0 {
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

// fromHexMethod backs bytes.fromhex / bytearray.fromhex. It runs as a
// classmethod, so args[0] is the type, not an instance.
//
// CPython: Objects/bytesobject.c:2503 bytes_fromhex_impl
func fromHexMethod(produceBytearray bool) methodFn {
	return func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 2 {
			return nil, arityErr("fromhex", 2, len(args))
		}
		// 3.14 accepts a str or any bytes-like object (the buffer
		// protocol); other types are TypeError.
		//
		// CPython: Objects/bytesobject.c:2514 _PyBytes_FromHex
		var src string
		if s, ok := args[1].(*Unicode); ok {
			src = s.Value()
		} else if buf, ok := AsBytesLike(args[1]); ok {
			src = string(buf)
		} else {
			return nil, fmt.Errorf("TypeError: fromhex() argument must be str or bytes-like, not %s", args[1].Type().Name)
		}
		b, err := BytesFromHex(src)
		if err != nil {
			return nil, err
		}
		// args[0] is the class the classmethod was invoked on. For the
		// base type return the raw result; for a subtype rebuild the
		// value by calling the subtype with the decoded bytes so the
		// subclass __new__/__init__ runs (and the result carries the
		// subtype). Mirrors the `type != &Py..._Type` branch.
		//
		// CPython: Objects/bytesobject.c:2503 bytes_fromhex_impl
		// CPython: Objects/bytearrayobject.c:2225 bytearray_fromhex_impl
		cls, _ := args[0].(*Type)
		if produceBytearray {
			ba := NewByteArray(b.v)
			if cls != nil && cls != ByteArrayType {
				return CallOneArg(cls, ba)
			}
			return ba, nil
		}
		if cls != nil && cls != BytesType {
			return CallOneArg(cls, b)
		}
		return b, nil
	}
}

// makeTransMethod backs bytes.maketrans / bytearray.maketrans, both
// staticmethods.
//
// CPython: Objects/bytes_methods.c:357 _Py_bytes_maketrans
func makeTransMethod() methodFn {
	return func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 2 {
			return nil, arityErr("maketrans", 2, len(args))
		}
		frm, err := bytesArgAsBuffer(args[0], "maketrans")
		if err != nil {
			return nil, err
		}
		to, err := bytesArgAsBuffer(args[1], "maketrans")
		if err != nil {
			return nil, err
		}
		return MakeBytesTrans(frm, to)
	}
}

// decodeMethod backs bytes.decode / bytearray.decode. The gopy port
// only supports the codecs we ship; that's currently the ASCII and
// Latin-1 surface plus a UTF-8 default. Unknown encodings raise
// LookupError to match CPython.
//
// CPython: Objects/bytesobject.c:1554 bytes_decode_impl
func decodeMethod() methodFn {
	return func(args []Object, kwargs map[string]Object) (Object, error) {
		if len(args) < 1 || len(args) > 3 {
			return nil, arityRangeErr("decode", 0, 2, len(args)-1)
		}
		v, err := bytesLikeView(args[0], "decode")
		if err != nil {
			return nil, err
		}
		encoding := "utf-8"
		if len(args) >= 2 && args[1] != nil && args[1] != None() {
			s, ok := args[1].(*Unicode)
			if !ok {
				return nil, fmt.Errorf("TypeError: decode() argument 'encoding' must be str, not %s", args[1].Type().Name)
			}
			encoding = s.Value()
		}
		if v, ok := kwargs["encoding"]; ok {
			if s, ok := v.(*Unicode); ok {
				encoding = s.Value()
			}
		}
		errHandler := "strict"
		if len(args) >= 3 && args[2] != nil && args[2] != None() {
			s, ok := args[2].(*Unicode)
			if !ok {
				return nil, fmt.Errorf("TypeError: decode() argument 'errors' must be str, not %s", args[2].Type().Name)
			}
			errHandler = s.Value()
		}
		if v, ok := kwargs["errors"]; ok {
			if s, ok := v.(*Unicode); ok {
				errHandler = s.Value()
			}
		}
		// Trigger codec lookup so _is_text_encoding is populated for Python codecs.
		if _, lerr := codecs.Lookup(encoding); lerr != nil {
			return nil, lerr
		}
		// Reject non-text encodings (binary transforms, str→str transforms).
		// CPython: Objects/bytesobject.c:1554 bytes_decode_impl _is_text_encoding check
		if !codecs.IsTextEncoding(encoding) {
			return nil, fmt.Errorf("LookupError: '%s' is not a text encoding; use codecs.decode() to handle arbitrary codecs", encoding)
		}
		s, _, err := codecs.Decode(v, encoding, errHandler)
		if err != nil {
			if FormatNoteHook != nil {
				FormatNoteHook(fmt.Sprintf("decoding with '%s' codec failed", encoding))
			}
			return nil, err
		}
		return NewStr(s), nil
	}
}

// IterToSlice walks an iterable into []Object. Sequences with a
// known length get pre-allocated; pure iterators grow.
func IterToSlice(o Object) ([]Object, error) {
	it, err := Iter(o)
	if err != nil {
		return nil, err
	}
	var out []Object
	for {
		v, err := IterNext(it)
		if err != nil {
			if errors.Is(err, ErrStopIteration) {
				return out, nil
			}
			return nil, err
		}
		out = append(out, v)
	}
}
