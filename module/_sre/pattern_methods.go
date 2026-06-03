package _sre

// Higher-level Pattern methods backed by the bytecode engine:
// findall, finditer, sub, subn, split. Each one walks the input by
// repeatedly invoking engineSearch, advancing past the previous match
// (handling zero-width matches the way CPython does), and assembling
// the appropriate Python-side return value.
//
// CPython references for each are pinned on the individual funcs.

import (
	"fmt"
	"strings"

	"github.com/tamnd/gopy/objects"
)

// patternFindall implements SRE_Pattern.findall(string[, pos[, endpos]]).
// The return shape depends on the pattern's group count:
//   - 0 groups: list of matched substrings
//   - 1 group: list of the captured group's substring
//   - N groups: list of N-tuples of the captured groups
//
// CPython: Modules/_sre/sre.c:2723 _sre_SRE_Pattern_findall_impl
func patternFindall(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	cp, ok := lookupCompiled(args[0])
	if !ok {
		return nil, fmt.Errorf("TypeError: findall() called on non-Pattern")
	}
	in, src, pos, endpos, err := matchPosEnd(cp, args)
	if err != nil {
		return nil, err
	}
	srcStr := stringOf(src)

	results := make([]objects.Object, 0)
	cur := pos
	mustAdv := false
	for cur <= endpos {
		st, err := engineSearch(cp, in, cur, endpos, mustAdv)
		if err != nil {
			return nil, err
		}
		if st == nil {
			break
		}
		mStart, mEnd := st.start, st.ptr

		var item objects.Object
		switch cp.groups {
		case 0:
			item = sliceSubject(srcStr, mStart, mEnd, cp.isbytes)
		case 1:
			item = sliceSubject(srcStr, st.mark[0], st.mark[1], cp.isbytes)
		default:
			parts := make([]objects.Object, cp.groups)
			for g := 0; g < cp.groups; g++ {
				parts[g] = sliceSubject(srcStr, st.mark[2*g], st.mark[2*g+1], cp.isbytes)
			}
			item = objects.NewTuple(parts)
		}
		results = append(results, item)

		if mStart == mEnd {
			cur = mStart + 1
			mustAdv = false
		} else {
			cur = mEnd
			mustAdv = false
		}
	}
	return objects.NewList(results), nil
}

// patternFinditer implements SRE_Pattern.finditer. CPython hands back a
// callable_iterator wrapping a Scanner; we mirror the same semantics
// by collecting all Match objects up front and returning them as a
// list (iterable in Python, sufficient for `for m in ...`).
//
// CPython: Modules/_sre/sre.c:2768 _sre_SRE_Pattern_finditer_impl
func patternFinditer(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	cp, ok := lookupCompiled(args[0])
	if !ok {
		return nil, fmt.Errorf("TypeError: finditer() called on non-Pattern")
	}
	in, src, pos, endpos, err := matchPosEnd(cp, args)
	if err != nil {
		return nil, err
	}

	matches := make([]objects.Object, 0)
	cur := pos
	for cur <= endpos {
		st, err := engineSearch(cp, in, cur, endpos, false)
		if err != nil {
			return nil, err
		}
		if st == nil {
			break
		}
		matches = append(matches, makeMatch(cp, st, src, args[0], pos, endpos))
		if st.start == st.ptr {
			cur = st.start + 1
		} else {
			cur = st.ptr
		}
	}
	return objects.NewList(matches), nil
}

// patternSub implements SRE_Pattern.sub(repl, string[, count]).
//
// CPython: Modules/_sre/sre.c:2700 _sre_SRE_Pattern_sub_impl
func patternSub(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	out, _, err := patternSubCommon(args, "sub")
	if err != nil {
		return nil, err
	}
	return objects.NewStr(out), nil
}

// patternSubn implements SRE_Pattern.subn(repl, string[, count]).
// Returns (result, n).
//
// CPython: Modules/_sre/sre.c:2716 _sre_SRE_Pattern_subn_impl
func patternSubn(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	out, n, err := patternSubCommon(args, "subn")
	if err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{
		objects.NewStr(out),
		objects.NewInt(int64(n)),
	}), nil
}

// patternSubCommon is the shared body of sub/subn. Drives the engine
// across the input, applying the template (or callable) for each
// match. Stops after `count` matches when count > 0.
//
// CPython: Modules/_sre/sre.c:1855 pattern_subx
func patternSubCommon(args []objects.Object, name string) (string, int, error) {
	if len(args) < 3 {
		return "", 0, fmt.Errorf("TypeError: %s() takes at least 2 args (pattern, repl, string)", name)
	}
	cp, ok := lookupCompiled(args[0])
	if !ok {
		return "", 0, fmt.Errorf("TypeError: %s() called on non-Pattern", name)
	}
	repl := args[1]
	count := 0
	if len(args) >= 4 {
		if i, ok := args[3].(*objects.Int); ok {
			v, _ := i.Int64()
			count = int(v)
		}
	}

	// args layout: pat, repl, string, [count]: matchPosEnd wants pat at
	// [0] and string at [1].
	posEndArgs := []objects.Object{args[0], args[2]}
	in, src, pos, endpos, err := matchPosEnd(cp, posEndArgs)
	if err != nil {
		return "", 0, err
	}
	srcStr := stringOf(src)
	replIsStr := false
	var tmpl string
	if u, ok := repl.(*objects.Unicode); ok {
		tmpl = u.Value()
		replIsStr = true
	} else if b, ok := repl.(*objects.Bytes); ok {
		tmpl = string(b.Bytes())
		replIsStr = true
	}

	// For Unicode strings the engine uses code-point indices; Go string
	// slicing is byte-based, so we must work through a rune slice.
	//
	// CPython: Modules/_sre/sre.c:2492 pattern_subx_impl (uses
	// character-position cursors throughout)
	var srcRunes []rune
	if _, isUnicode := src.(*objects.Unicode); isUnicode {
		srcRunes = []rune(srcStr)
	}

	// runeSlice converts a half-open [lo, hi) code-point range to a string.
	slice := func(lo, hi int) string {
		if srcRunes != nil {
			if lo < 0 {
				lo = 0
			}
			if hi > len(srcRunes) {
				hi = len(srcRunes)
			}
			return string(srcRunes[lo:hi])
		}
		return srcStr[lo:hi]
	}

	var out strings.Builder
	cur := pos
	n := 0
	for cur <= endpos {
		if count > 0 && n >= count {
			break
		}
		st, err := engineSearch(cp, in, cur, endpos, false)
		if err != nil {
			return "", 0, err
		}
		if st == nil {
			break
		}
		mStart, mEnd := st.start, st.ptr
		out.WriteString(slice(cur, mStart))

		if replIsStr {
			m := makeMatch(cp, st, src, args[0], pos, endpos).(*objects.Instance)
			locs := matchStore[m].locs
			s, terr := expandTemplate(m, tmpl, locs, srcStr)
			if terr != nil {
				return "", 0, terr
			}
			out.WriteString(s)
		} else {
			m := makeMatch(cp, st, src, args[0], pos, endpos)
			r, cerr := objects.Vectorcall(repl, []objects.Object{m}, 1, nil)
			if cerr != nil {
				return "", 0, cerr
			}
			rs, serr := objects.Str(r)
			if serr != nil {
				return "", 0, serr
			}
			out.WriteString(rs)
		}
		n++

		if mStart == mEnd {
			// zero-width: copy one code-point from cur, advance past it.
			if mEnd < endpos {
				out.WriteString(slice(mEnd, mEnd+1))
			}
			cur = mEnd + 1
		} else {
			cur = mEnd
		}
	}
	if cur <= endpos {
		out.WriteString(slice(cur, endpos))
	}
	return out.String(), n, nil
}

// patternSplit implements SRE_Pattern.split(string[, maxsplit]).
// Returns a list of substrings; if the pattern has groups, captured
// groups are interleaved into the result the way CPython does.
//
// CPython: Modules/_sre/sre.c:2685 _sre_SRE_Pattern_split_impl
func patternSplit(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: split() requires string argument")
	}
	cp, ok := lookupCompiled(args[0])
	if !ok {
		return nil, fmt.Errorf("TypeError: split() called on non-Pattern")
	}
	maxsplit := 0
	if len(args) >= 3 {
		if i, ok := args[2].(*objects.Int); ok {
			v, _ := i.Int64()
			maxsplit = int(v)
		}
	}
	// split() signature is (string, maxsplit); pos/endpos are not part
	// of the CPython API. Hand matchPosEnd just (pat, string).
	in, src, pos, endpos, err := matchPosEnd(cp, []objects.Object{args[0], args[1]})
	if err != nil {
		return nil, err
	}
	srcStr := stringOf(src)

	out := make([]objects.Object, 0)
	cur := pos
	n := 0
	for cur <= endpos {
		if maxsplit > 0 && n >= maxsplit {
			break
		}
		st, err := engineSearch(cp, in, cur, endpos, false)
		if err != nil {
			return nil, err
		}
		if st == nil {
			break
		}
		mStart, mEnd := st.start, st.ptr
		if mStart == mEnd && mStart == cur {
			// Skip zero-width at the current cursor; do not split.
			if mEnd >= endpos {
				break
			}
			cur = mEnd + 1
			continue
		}
		out = append(out, objects.NewStr(srcStr[cur:mStart]))
		for g := 0; g < cp.groups; g++ {
			lo, hi := st.mark[2*g], st.mark[2*g+1]
			if lo < 0 || hi < 0 {
				out = append(out, objects.None())
			} else {
				out = append(out, objects.NewStr(srcStr[lo:hi]))
			}
		}
		n++
		if mStart == mEnd {
			cur = mEnd + 1
		} else {
			cur = mEnd
		}
	}
	if cur <= endpos {
		out = append(out, objects.NewStr(srcStr[cur:endpos]))
	} else {
		out = append(out, objects.NewStr(""))
	}
	return objects.NewList(out), nil
}

// stringOf turns a Python str or bytes argument into its Go string
// representation. Returns "" for anything else.
// sliceSubject materialises subject[lo:hi] as a bytes object when the
// pattern is byte-oriented and a str otherwise. An unset group (lo or hi
// negative) yields the empty result of the matching kind. Mirrors
// getslice's str/bytes branch in the C engine.
//
// CPython: Modules/_sre/sre.c:1769 getslice
func sliceSubject(s string, lo, hi int, isBytes bool) objects.Object {
	var seg string
	if lo >= 0 && hi >= 0 {
		seg = s[lo:hi]
	}
	if isBytes {
		return objects.NewBytes([]byte(seg))
	}
	return objects.NewStr(seg)
}

func stringOf(o objects.Object) string {
	if u, ok := o.(*objects.Unicode); ok {
		return u.Value()
	}
	// bytes, bytearray, memoryview and array all expose a buffer; read it
	// uniformly so a bytearray subject is not silently treated as empty.
	if b, ok := objects.AsBytesLike(o); ok {
		return string(b)
	}
	return ""
}
