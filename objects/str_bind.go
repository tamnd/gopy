// Bind str method descriptors. The Go-side helpers in str_methods.go
// already implement the per-method behavior; this file wires each one
// onto strType so Python code can call `s.startswith(...)`.
//
// CPython: Objects/unicodeobject.c:L15263 unicode_methods

package objects

import (
	"errors"
	"fmt"
	"strings"
)

func init() {
	bind := func(name string, fn func(args []Object, kwargs map[string]Object) (Object, error)) {
		SetTypeDescr(strType, name, NewMethodDescr(strType, name, fn))
	}

	bind("startswith", strStartsWithMethod)
	bind("endswith", strEndsWithMethod)
	bind("find", strFindMethod)
	bind("rfind", strRFindMethod)
	bind("index", strIndexMethod)
	bind("rindex", strRIndexMethod)
	bind("count", strCountMethod)
	bind("lower", strSimple1(StrLower))
	bind("upper", strSimple1(StrUpper))
	bind("casefold", strSimple1(StrCaseFold))
	bind("swapcase", strSimple1(StrSwapCase))
	bind("capitalize", strSimple1(StrCapitalize))
	bind("title", strSimple1(StrTitle))
	bind("strip", strStripMethod(StrStrip))
	bind("lstrip", strStripMethod(StrLStrip))
	bind("rstrip", strStripMethod(StrRStrip))
	bind("split", strSplitMethod)
	bind("rsplit", strRSplitMethod)
	bind("splitlines", strSplitLinesMethod)
	bind("replace", strReplaceMethod)
	bind("join", strJoinMethod)
	bind("partition", strPartitionMethod)
	bind("rpartition", strRPartitionMethod)
	bind("isdigit", strBoolMethod(func(s string) bool { return s != "" && allDigits(s) }))
	bind("isalpha", strBoolMethod(func(s string) bool { return s != "" && allAlpha(s) }))
	bind("isalnum", strBoolMethod(func(s string) bool { return s != "" && allAlnum(s) }))
	bind("isspace", strBoolMethod(func(s string) bool { return s != "" && allSpace(s) }))
	bind("isascii", strBoolMethod(StrIsASCII))
	bind("islower", strBoolMethod(StrIsLower))
	bind("isupper", strBoolMethod(StrIsUpper))
	bind("encode", strEncodeMethod)
	bind("format", strFormatMethod)
	bind("format_map", strFormatMapMethod)
	bind("__contains__", strContainsMethod)
	bind("__len__", strLenMethod)
	bind("translate", strTranslateMethod)
	bind("zfill", strZfillMethod)
	bind("center", strCenterMethod)
	bind("ljust", strLjustMethod)
	bind("rjust", strRjustMethod)
	bind("expandtabs", strExpandTabsMethod)
	bind("removeprefix", strRemovePrefixMethod)
	bind("removesuffix", strRemoveSuffixMethod)
	SetTypeDescr(strType, "maketrans", NewStaticMethod(NewBuiltinFunction("maketrans", strMakeTrans)))
}

func selfStr(args []Object, name string) (string, error) {
	if len(args) < 1 {
		return "", fmt.Errorf("TypeError: descriptor '%s' of 'str' needs an argument", name)
	}
	s, ok := asUnicode(args[0])
	if !ok {
		return "", fmt.Errorf("TypeError: descriptor '%s' requires a 'str' object", name)
	}
	return s, nil
}

func strRange(args []Object) (int, int, error) {
	const base = 2
	start, end := 0, -1
	if len(args) > base {
		i, ok := args[base].(*Int)
		if !ok {
			return 0, 0, fmt.Errorf("TypeError: slice indices must be integers")
		}
		n, _ := i.Int64()
		start = int(n)
	}
	if len(args) > base+1 {
		i, ok := args[base+1].(*Int)
		if !ok {
			return 0, 0, fmt.Errorf("TypeError: slice indices must be integers")
		}
		n, _ := i.Int64()
		end = int(n)
	}
	return start, end, nil
}

func strNeedle(o Object) (string, error) {
	s, ok := asUnicode(o)
	if !ok {
		return "", fmt.Errorf("TypeError: expected str")
	}
	return s, nil
}

func resolveEnd(end, n int) int {
	if end < 0 {
		return n
	}
	return end
}

func strStartsWithMethod(args []Object, _ map[string]Object) (Object, error) {
	s, err := selfStr(args, "startswith")
	if err != nil {
		return nil, err
	}
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: startswith() takes at least 1 argument")
	}
	start, end, rerr := strRange(args)
	if rerr != nil {
		return nil, rerr
	}
	rs := runeSlice(s)
	end = resolveEnd(end, len(rs))
	check := func(prefix string) bool { return StrStartsWith(s, prefix, start, end) }
	if t, ok := args[1].(*Tuple); ok {
		for _, item := range tupleSlice(t) {
			p, perr := strNeedle(item)
			if perr != nil {
				return nil, perr
			}
			if check(p) {
				return True(), nil
			}
		}
		return False(), nil
	}
	p, perr := strNeedle(args[1])
	if perr != nil {
		return nil, perr
	}
	return NewBool(check(p)), nil
}

func strEndsWithMethod(args []Object, _ map[string]Object) (Object, error) {
	s, err := selfStr(args, "endswith")
	if err != nil {
		return nil, err
	}
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: endswith() takes at least 1 argument")
	}
	start, end, rerr := strRange(args)
	if rerr != nil {
		return nil, rerr
	}
	rs := runeSlice(s)
	end = resolveEnd(end, len(rs))
	check := func(suffix string) bool { return StrEndsWith(s, suffix, start, end) }
	if t, ok := args[1].(*Tuple); ok {
		for _, item := range tupleSlice(t) {
			p, perr := strNeedle(item)
			if perr != nil {
				return nil, perr
			}
			if check(p) {
				return True(), nil
			}
		}
		return False(), nil
	}
	p, perr := strNeedle(args[1])
	if perr != nil {
		return nil, perr
	}
	return NewBool(check(p)), nil
}

func strFindMethod(args []Object, _ map[string]Object) (Object, error) {
	s, err := selfStr(args, "find")
	if err != nil {
		return nil, err
	}
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: find() takes at least 1 argument")
	}
	needle, nerr := strNeedle(args[1])
	if nerr != nil {
		return nil, nerr
	}
	start, end, rerr := strRange(args)
	if rerr != nil {
		return nil, rerr
	}
	end = resolveEnd(end, len(runeSlice(s)))
	return NewInt(int64(StrFind(s, needle, start, end))), nil
}

func strRFindMethod(args []Object, _ map[string]Object) (Object, error) {
	s, err := selfStr(args, "rfind")
	if err != nil {
		return nil, err
	}
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: rfind() takes at least 1 argument")
	}
	needle, nerr := strNeedle(args[1])
	if nerr != nil {
		return nil, nerr
	}
	start, end, rerr := strRange(args)
	if rerr != nil {
		return nil, rerr
	}
	end = resolveEnd(end, len(runeSlice(s)))
	return NewInt(int64(StrRFind(s, needle, start, end))), nil
}

func strIndexMethod(args []Object, _ map[string]Object) (Object, error) {
	s, err := selfStr(args, "index")
	if err != nil {
		return nil, err
	}
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: index() takes at least 1 argument")
	}
	needle, nerr := strNeedle(args[1])
	if nerr != nil {
		return nil, nerr
	}
	start, end, rerr := strRange(args)
	if rerr != nil {
		return nil, rerr
	}
	end = resolveEnd(end, len(runeSlice(s)))
	i, ierr := StrIndex(s, needle, start, end)
	if ierr != nil {
		return nil, ierr
	}
	return NewInt(int64(i)), nil
}

func strRIndexMethod(args []Object, _ map[string]Object) (Object, error) {
	s, err := selfStr(args, "rindex")
	if err != nil {
		return nil, err
	}
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: rindex() takes at least 1 argument")
	}
	needle, nerr := strNeedle(args[1])
	if nerr != nil {
		return nil, nerr
	}
	start, end, rerr := strRange(args)
	if rerr != nil {
		return nil, rerr
	}
	end = resolveEnd(end, len(runeSlice(s)))
	i, ierr := StrRIndex(s, needle, start, end)
	if ierr != nil {
		return nil, ierr
	}
	return NewInt(int64(i)), nil
}

func strCountMethod(args []Object, _ map[string]Object) (Object, error) {
	s, err := selfStr(args, "count")
	if err != nil {
		return nil, err
	}
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: count() takes at least 1 argument")
	}
	needle, nerr := strNeedle(args[1])
	if nerr != nil {
		return nil, nerr
	}
	start, end, rerr := strRange(args)
	if rerr != nil {
		return nil, rerr
	}
	end = resolveEnd(end, len(runeSlice(s)))
	return NewInt(int64(StrCount(s, needle, start, end))), nil
}

func strSimple1(fn func(string) string) func([]Object, map[string]Object) (Object, error) {
	return func(args []Object, _ map[string]Object) (Object, error) {
		s, err := selfStr(args, "str-method")
		if err != nil {
			return nil, err
		}
		return NewStr(fn(s)), nil
	}
}

func strStripMethod(fn func(string, string) string) func([]Object, map[string]Object) (Object, error) {
	return func(args []Object, _ map[string]Object) (Object, error) {
		s, err := selfStr(args, "strip")
		if err != nil {
			return nil, err
		}
		chars := ""
		if len(args) >= 2 && args[1] != None() {
			c, cerr := strNeedle(args[1])
			if cerr != nil {
				return nil, cerr
			}
			chars = c
		}
		return NewStr(fn(s, chars)), nil
	}
}

func strSplitMethod(args []Object, _ map[string]Object) (Object, error) {
	s, err := selfStr(args, "split")
	if err != nil {
		return nil, err
	}
	sep := ""
	if len(args) >= 2 && args[1] != None() {
		c, cerr := strNeedle(args[1])
		if cerr != nil {
			return nil, cerr
		}
		sep = c
	}
	maxsplit := -1
	if len(args) >= 3 {
		if i, ok := args[2].(*Int); ok {
			n, _ := i.Int64()
			maxsplit = int(n)
		}
	}
	parts, perr := StrSplit(s, sep, maxsplit)
	if perr != nil {
		return nil, perr
	}
	return strListFromGoSlice(parts), nil
}

func strRSplitMethod(args []Object, _ map[string]Object) (Object, error) {
	s, err := selfStr(args, "rsplit")
	if err != nil {
		return nil, err
	}
	sep := ""
	if len(args) >= 2 && args[1] != None() {
		c, cerr := strNeedle(args[1])
		if cerr != nil {
			return nil, cerr
		}
		sep = c
	}
	maxsplit := -1
	if len(args) >= 3 {
		if i, ok := args[2].(*Int); ok {
			n, _ := i.Int64()
			maxsplit = int(n)
		}
	}
	parts, perr := StrRSplit(s, sep, maxsplit)
	if perr != nil {
		return nil, perr
	}
	return strListFromGoSlice(parts), nil
}

func strSplitLinesMethod(args []Object, _ map[string]Object) (Object, error) {
	s, err := selfStr(args, "splitlines")
	if err != nil {
		return nil, err
	}
	keepends := false
	if len(args) >= 2 {
		keepends = strToBool(args[1])
	}
	return strListFromGoSlice(StrSplitLines(s, keepends)), nil
}

func strReplaceMethod(args []Object, _ map[string]Object) (Object, error) {
	s, err := selfStr(args, "replace")
	if err != nil {
		return nil, err
	}
	if len(args) < 3 {
		return nil, fmt.Errorf("TypeError: replace() takes at least 2 arguments")
	}
	old, oerr := strNeedle(args[1])
	if oerr != nil {
		return nil, oerr
	}
	newS, nerr := strNeedle(args[2])
	if nerr != nil {
		return nil, nerr
	}
	count := -1
	if len(args) >= 4 {
		if i, ok := args[3].(*Int); ok {
			n, _ := i.Int64()
			count = int(n)
		}
	}
	return NewStr(StrReplace(s, old, newS, count)), nil
}

func strJoinMethod(args []Object, _ map[string]Object) (Object, error) {
	sep, err := selfStr(args, "join")
	if err != nil {
		return nil, err
	}
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: join() takes 1 argument")
	}
	tp := args[1].Type()
	if tp.Iter == nil {
		return nil, fmt.Errorf("TypeError: can only join an iterable")
	}
	it, ierr := tp.Iter(args[1])
	if ierr != nil {
		return nil, ierr
	}
	itType := it.Type()
	var parts []Object
	for {
		v, e := itType.IterNext(it)
		if e != nil {
			if errors.Is(e, ErrStopIteration) {
				break
			}
			return nil, e
		}
		if v == nil {
			break
		}
		parts = append(parts, v)
	}
	out, jerr := StrJoin(sep, parts)
	if jerr != nil {
		return nil, jerr
	}
	return NewStr(out), nil
}

func strPartitionMethod(args []Object, _ map[string]Object) (Object, error) {
	s, err := selfStr(args, "partition")
	if err != nil {
		return nil, err
	}
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: partition() takes 1 argument")
	}
	sep, serr := strNeedle(args[1])
	if serr != nil {
		return nil, serr
	}
	a, b, c, perr := StrPartition(s, sep)
	if perr != nil {
		return nil, perr
	}
	return NewTuple([]Object{NewStr(a), NewStr(b), NewStr(c)}), nil
}

func strRPartitionMethod(args []Object, _ map[string]Object) (Object, error) {
	s, err := selfStr(args, "rpartition")
	if err != nil {
		return nil, err
	}
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: rpartition() takes 1 argument")
	}
	sep, serr := strNeedle(args[1])
	if serr != nil {
		return nil, serr
	}
	a, b, c, perr := StrRPartition(s, sep)
	if perr != nil {
		return nil, perr
	}
	return NewTuple([]Object{NewStr(a), NewStr(b), NewStr(c)}), nil
}

func strBoolMethod(fn func(string) bool) func([]Object, map[string]Object) (Object, error) {
	return func(args []Object, _ map[string]Object) (Object, error) {
		s, err := selfStr(args, "is*")
		if err != nil {
			return nil, err
		}
		return NewBool(fn(s)), nil
	}
}

func strContainsMethod(args []Object, _ map[string]Object) (Object, error) {
	s, err := selfStr(args, "__contains__")
	if err != nil {
		return nil, err
	}
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: __contains__ takes 1 argument")
	}
	needle, nerr := strNeedle(args[1])
	if nerr != nil {
		return nil, nerr
	}
	return NewBool(strings.Contains(s, needle)), nil
}

func strLenMethod(args []Object, _ map[string]Object) (Object, error) {
	s, err := selfStr(args, "__len__")
	if err != nil {
		return nil, err
	}
	return NewInt(int64(len(runeSlice(s)))), nil
}

func strEncodeMethod(args []Object, _ map[string]Object) (Object, error) {
	s, err := selfStr(args, "encode")
	if err != nil {
		return nil, err
	}
	return NewBytes([]byte(s)), nil
}

func strFormatMethod(args []Object, kwargs map[string]Object) (Object, error) {
	s, err := selfStr(args, "format")
	if err != nil {
		return nil, err
	}
	out, ferr := strFormatExpand(s, args[1:], kwargs)
	if ferr != nil {
		return nil, ferr
	}
	return NewStr(out), nil
}

func strFormatExpand(s string, args []Object, kwargs map[string]Object) (string, error) {
	var b strings.Builder
	auto := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '{' && i+1 < len(s) && s[i+1] == '{' {
			b.WriteByte('{')
			i++
			continue
		}
		if c == '}' && i+1 < len(s) && s[i+1] == '}' {
			b.WriteByte('}')
			i++
			continue
		}
		if c == '}' {
			return "", fmt.Errorf("ValueError: single '}' in format string")
		}
		if c != '{' {
			b.WriteByte(c)
			continue
		}
		end := strings.IndexByte(s[i:], '}')
		if end < 0 {
			return "", fmt.Errorf("ValueError: unmatched '{' in format string")
		}
		rendered, ferr := strFormatField(s[i+1:i+end], args, kwargs, &auto)
		if ferr != nil {
			return "", ferr
		}
		b.WriteString(rendered)
		i += end
	}
	return b.String(), nil
}

func strFormatField(field string, args []Object, kwargs map[string]Object, auto *int) (string, error) {
	name, spec := field, ""
	if colon := strings.IndexByte(field, ':'); colon >= 0 {
		name, spec = field[:colon], field[colon+1:]
	}
	v, err := strFormatLookup(name, args, kwargs, auto)
	if err != nil {
		return "", err
	}
	return unicodeFormat(v, spec)
}

func strFormatLookup(name string, args []Object, kwargs map[string]Object, auto *int) (Object, error) {
	if name == "" {
		if *auto >= len(args) {
			return nil, fmt.Errorf("IndexError: tuple index out of range")
		}
		v := args[*auto]
		*auto++
		return v, nil
	}
	if name[0] >= '0' && name[0] <= '9' {
		idx := 0
		for _, r := range name {
			if r < '0' || r > '9' {
				return nil, fmt.Errorf("ValueError: invalid field index %q", name)
			}
			idx = idx*10 + int(r-'0')
		}
		if idx >= len(args) {
			return nil, fmt.Errorf("IndexError: tuple index out of range")
		}
		return args[idx], nil
	}
	key := name
	if attrIdx := strings.IndexAny(name, ".["); attrIdx >= 0 {
		key = name[:attrIdx]
	}
	v, ok := kwargs[key]
	if !ok {
		return nil, fmt.Errorf("KeyError: '%s'", key)
	}
	return v, nil
}

func strFormatMapMethod(args []Object, _ map[string]Object) (Object, error) {
	s, err := selfStr(args, "format_map")
	if err != nil {
		return nil, err
	}
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: format_map() takes 1 argument")
	}
	mp, ok := args[1].(*Dict)
	if !ok {
		return nil, fmt.Errorf("TypeError: format_map() argument must be a mapping")
	}
	kwargs := map[string]Object{}
	for _, k := range mp.Keys() {
		v, _ := mp.GetItem(k)
		ks, _ := Str(k)
		kwargs[ks] = v
	}
	out, ferr := strFormatExpand(s, nil, kwargs)
	if ferr != nil {
		return nil, ferr
	}
	return NewStr(out), nil
}

// strTranslateMethod backs str.translate(table).
// table is a dict mapping ordinals to ordinals, strings, or None.
//
// CPython: Objects/unicodeobject.c:11231 unicode_translate_impl
func strTranslateMethod(args []Object, _ map[string]Object) (Object, error) {
	s, err := selfStr(args, "translate")
	if err != nil {
		return nil, err
	}
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: translate() takes exactly one argument")
	}
	table, ok := args[1].(*Dict)
	if !ok {
		return nil, fmt.Errorf("TypeError: translate() argument must be a dict")
	}
	var out []rune
	for _, r := range s {
		key := NewInt(int64(r))
		v, err := table.GetItem(key)
		if err != nil {
			out = append(out, r)
			continue
		}
		if IsNone(v) {
			continue
		}
		switch tv := v.(type) {
		case *Int:
			n, _ := tv.Int64()
			out = append(out, rune(n))
		case *Unicode:
			out = append(out, []rune(tv.Value())...)
		default:
			return nil, fmt.Errorf("TypeError: character mapping must return integer, None or str, not '%s'", v.Type().Name)
		}
	}
	return NewStr(string(out)), nil
}

// strMakeTrans backs str.maketrans(x[, y[, z]]).
//
// CPython: Objects/unicodeobject.c:13476 unicode_maketrans_impl
func strMakeTrans(args []Object, _ map[string]Object) (Object, error) {
	if len(args) == 0 || len(args) > 3 {
		return nil, fmt.Errorf("TypeError: maketrans() takes 1 to 3 arguments (%d given)", len(args))
	}
	if len(args) == 1 {
		return makeTransFromDict(args[0])
	}
	return makeTransFromStrings(args)
}

// makeTransFromDict builds the translation table from the one-arg
// dict form. Each key must be an int or a length-1 string.
func makeTransFromDict(arg Object) (Object, error) {
	mp, ok := arg.(*Dict)
	if !ok {
		return nil, fmt.Errorf("TypeError: maketrans() argument 1 must be a dict")
	}
	out := NewDict()
	for _, k := range mp.Keys() {
		v, _ := mp.GetItem(k)
		intKey, err := makeTransKey(k)
		if err != nil {
			return nil, err
		}
		if err := out.SetItem(intKey, v); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func makeTransKey(k Object) (Object, error) {
	switch kt := k.(type) {
	case *Int:
		return kt, nil
	case *Unicode:
		rs := []rune(kt.Value())
		if len(rs) != 1 {
			return nil, fmt.Errorf("TypeError: string keys in translate table must be of length 1")
		}
		return NewInt(int64(rs[0])), nil
	}
	return nil, fmt.Errorf("TypeError: maketrans() argument 1 has bad key type")
}

// makeTransFromStrings builds the table from the (from, to[, delete])
// string form. Pairs each rune in `from` with the matching rune in
// `to`, then maps every rune in `delete` to None.
func makeTransFromStrings(args []Object) (Object, error) {
	fromStr, ok1 := args[0].(*Unicode)
	toStr, ok2 := args[1].(*Unicode)
	if !ok1 || !ok2 {
		return nil, fmt.Errorf("TypeError: maketrans() argument 1 must be str")
	}
	fromR := []rune(fromStr.Value())
	toR := []rune(toStr.Value())
	if len(fromR) != len(toR) {
		return nil, fmt.Errorf("ValueError: the first two maketrans arguments must have equal length")
	}
	out := NewDict()
	for i, r := range fromR {
		if err := out.SetItem(NewInt(int64(r)), NewInt(int64(toR[i]))); err != nil {
			return nil, err
		}
	}
	if len(args) == 3 {
		delStr, ok := args[2].(*Unicode)
		if !ok {
			return nil, fmt.Errorf("TypeError: maketrans() argument 3 must be str")
		}
		for _, r := range delStr.Value() {
			if err := out.SetItem(NewInt(int64(r)), None()); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// strZfillMethod backs str.zfill(width).
//
// CPython: Objects/unicodeobject.c:12994 unicode_zfill_impl
func strZfillMethod(args []Object, _ map[string]Object) (Object, error) {
	s, err := selfStr(args, "zfill")
	if err != nil {
		return nil, err
	}
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: zfill() takes exactly one argument")
	}
	w, ok := args[1].(*Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: zfill() argument must be int")
	}
	width, _ := w.Int64()
	rs := []rune(s)
	n := int(width) - len(rs)
	if n <= 0 {
		return NewStr(s), nil
	}
	pad := strings.Repeat("0", n)
	if len(rs) > 0 && (rs[0] == '+' || rs[0] == '-') {
		return NewStr(string(rs[0]) + pad + string(rs[1:])), nil
	}
	return NewStr(pad + s), nil
}

// strCenterMethod backs str.center(width[, fillchar]).
//
// CPython: Objects/unicodeobject.c:10769 unicode_center_impl
func strCenterMethod(args []Object, _ map[string]Object) (Object, error) {
	s, err := selfStr(args, "center")
	if err != nil {
		return nil, err
	}
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: center() takes at least one argument")
	}
	w, ok := args[1].(*Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: center() argument 1 must be int")
	}
	width, _ := w.Int64()
	fill := " "
	if len(args) >= 3 {
		fc, fok := args[2].(*Unicode)
		if !fok {
			return nil, fmt.Errorf("TypeError: center() argument 2 must be str")
		}
		fill = fc.Value()
	}
	rs := []rune(s)
	marg := int(width) - len(rs)
	if marg <= 0 {
		return NewStr(s), nil
	}
	left := marg / 2
	right := marg - left
	return NewStr(strings.Repeat(fill, left) + s + strings.Repeat(fill, right)), nil
}

// strLjustMethod backs str.ljust(width[, fillchar]).
//
// CPython: Objects/unicodeobject.c:10800 unicode_ljust_impl
func strLjustMethod(args []Object, _ map[string]Object) (Object, error) {
	s, err := selfStr(args, "ljust")
	if err != nil {
		return nil, err
	}
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: ljust() takes at least one argument")
	}
	w, ok := args[1].(*Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: ljust() argument 1 must be int")
	}
	width, _ := w.Int64()
	fill := " "
	if len(args) >= 3 {
		fc, fok := args[2].(*Unicode)
		if !fok {
			return nil, fmt.Errorf("TypeError: ljust() argument 2 must be str")
		}
		fill = fc.Value()
	}
	rs := []rune(s)
	n := int(width) - len(rs)
	if n <= 0 {
		return NewStr(s), nil
	}
	return NewStr(s + strings.Repeat(fill, n)), nil
}

// strRjustMethod backs str.rjust(width[, fillchar]).
//
// CPython: Objects/unicodeobject.c:10821 unicode_rjust_impl
func strRjustMethod(args []Object, _ map[string]Object) (Object, error) {
	s, err := selfStr(args, "rjust")
	if err != nil {
		return nil, err
	}
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: rjust() takes at least one argument")
	}
	w, ok := args[1].(*Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: rjust() argument 1 must be int")
	}
	width, _ := w.Int64()
	fill := " "
	if len(args) >= 3 {
		fc, fok := args[2].(*Unicode)
		if !fok {
			return nil, fmt.Errorf("TypeError: rjust() argument 2 must be str")
		}
		fill = fc.Value()
	}
	rs := []rune(s)
	n := int(width) - len(rs)
	if n <= 0 {
		return NewStr(s), nil
	}
	return NewStr(strings.Repeat(fill, n) + s), nil
}

// strExpandTabsMethod backs str.expandtabs([tabsize=8]).
//
// CPython: Objects/unicodeobject.c:11157 unicode_expandtabs_impl
func strExpandTabsMethod(args []Object, _ map[string]Object) (Object, error) {
	s, err := selfStr(args, "expandtabs")
	if err != nil {
		return nil, err
	}
	tabsize := 8
	if len(args) >= 2 {
		if i, ok := args[1].(*Int); ok {
			n, _ := i.Int64()
			tabsize = int(n)
		}
	}
	var b strings.Builder
	col := 0
	for _, r := range s {
		switch r {
		case '\t':
			if tabsize > 0 {
				spaces := tabsize - (col % tabsize)
				b.WriteString(strings.Repeat(" ", spaces))
				col += spaces
			}
		case '\n', '\r':
			b.WriteRune(r)
			col = 0
		default:
			b.WriteRune(r)
			col++
		}
	}
	return NewStr(b.String()), nil
}

// strRemovePrefixMethod backs str.removeprefix(prefix).
//
// CPython: Objects/unicodeobject.c:12958 unicode_removeprefix_impl
func strRemovePrefixMethod(args []Object, _ map[string]Object) (Object, error) {
	s, err := selfStr(args, "removeprefix")
	if err != nil {
		return nil, err
	}
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: removeprefix() takes exactly one argument")
	}
	prefix, perr := strNeedle(args[1])
	if perr != nil {
		return nil, perr
	}
	if strings.HasPrefix(s, prefix) {
		return NewStr(s[len(prefix):]), nil
	}
	return NewStr(s), nil
}

// strRemoveSuffixMethod backs str.removesuffix(suffix).
//
// CPython: Objects/unicodeobject.c:12976 unicode_removesuffix_impl
func strRemoveSuffixMethod(args []Object, _ map[string]Object) (Object, error) {
	s, err := selfStr(args, "removesuffix")
	if err != nil {
		return nil, err
	}
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: removesuffix() takes exactly one argument")
	}
	suffix, serr := strNeedle(args[1])
	if serr != nil {
		return nil, serr
	}
	if strings.HasSuffix(s, suffix) {
		return NewStr(s[:len(s)-len(suffix)]), nil
	}
	return NewStr(s), nil
}

func tupleSlice(t *Tuple) []Object {
	out := make([]Object, t.Len())
	for i := 0; i < t.Len(); i++ {
		out[i] = t.Item(i)
	}
	return out
}

func strToBool(o Object) bool {
	if o == nil {
		return false
	}
	if b, ok := o.(*Bool); ok {
		return b == True()
	}
	if i, ok := o.(*Int); ok {
		n, _ := i.Int64()
		return n != 0
	}
	return o != None()
}

func strListFromGoSlice(parts []string) Object {
	items := make([]Object, len(parts))
	for i, p := range parts {
		items[i] = NewStr(p)
	}
	return NewList(items)
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isAlpha(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func allAlpha(s string) bool {
	for _, r := range s {
		if !isAlpha(r) {
			return false
		}
	}
	return true
}

func allAlnum(s string) bool {
	for _, r := range s {
		if !isAlpha(r) && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func allSpace(s string) bool {
	for _, r := range s {
		if r != ' ' && r != '\t' && r != '\n' && r != '\r' && r != '\v' && r != '\f' {
			return false
		}
	}
	return true
}
