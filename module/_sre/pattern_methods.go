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
			item = objects.NewStr(srcStr[mStart:mEnd])
		case 1:
			lo, hi := st.mark[0], st.mark[1]
			if lo < 0 || hi < 0 {
				item = objects.NewStr("")
			} else {
				item = objects.NewStr(srcStr[lo:hi])
			}
		default:
			parts := make([]objects.Object, cp.groups)
			for g := 0; g < cp.groups; g++ {
				lo, hi := st.mark[2*g], st.mark[2*g+1]
				if lo < 0 || hi < 0 {
					parts[g] = objects.NewStr("")
				} else {
					parts[g] = objects.NewStr(srcStr[lo:hi])
				}
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
		out.WriteString(srcStr[cur:mStart])

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
			// zero-width: copy one char from cur, advance past it.
			if mEnd < endpos {
				out.WriteString(srcStr[mEnd : mEnd+1])
			}
			cur = mEnd + 1
		} else {
			cur = mEnd
		}
	}
	if cur <= endpos {
		out.WriteString(srcStr[cur:endpos])
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
func stringOf(o objects.Object) string {
	if u, ok := o.(*objects.Unicode); ok {
		return u.Value()
	}
	if b, ok := o.(*objects.Bytes); ok {
		return string(b.Bytes())
	}
	return ""
}
