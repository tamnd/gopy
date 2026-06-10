// Bind str method descriptors. The Go-side helpers in str_methods.go
// already implement the per-method behavior; this file wires each one
// onto strType so Python code can call `s.startswith(...)`.
//
// CPython: Objects/unicodeobject.c:L15263 unicode_methods

package objects

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/tamnd/gopy/codecs"
)

func init() {
	bind := func(name string, fn func(args []Object, kwargs map[string]Object) (Object, error)) {
		SetTypeDescr(strType, name, NewMethodDescr(strType, name, fn))
	}
	// bindNoArgs wires a METH_NOARGS row so methodDescrCheckArity emits
	// the CPython arg-count TypeError ("str.<name>() takes no arguments
	// (N given)") through _PyObject_FunctionStr, instead of the bare
	// message the hand-rolled bodies used to raise.
	bindNoArgs := func(name string, fn func(args []Object, kwargs map[string]Object) (Object, error)) {
		SetTypeDescr(strType, name, NewMethodDescrConv(strType, name, MethNoArgs, fn))
	}

	bind("startswith", strStartsWithMethod)
	bind("endswith", strEndsWithMethod)
	bind("find", strFindMethod)
	bind("rfind", strRFindMethod)
	bind("index", strIndexMethod)
	bind("rindex", strRIndexMethod)
	bind("count", strCountMethod)
	bindNoArgs("lower", strSimple1(StrLower))
	bindNoArgs("upper", strSimple1(StrUpper))
	bindNoArgs("casefold", strSimple1(StrCaseFold))
	bindNoArgs("swapcase", strSimple1(StrSwapCase))
	bindNoArgs("capitalize", strSimple1(StrCapitalize))
	bindNoArgs("title", strSimple1(StrTitle))
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
	bindNoArgs("isdigit", strBoolMethod(StrIsDigit))
	bindNoArgs("isalpha", strBoolMethod(StrIsAlpha))
	bindNoArgs("isalnum", strBoolMethod(StrIsAlnum))
	bindNoArgs("isspace", strBoolMethod(func(s string) bool { return s != "" && allSpace(s) }))
	bindNoArgs("isascii", strBoolMethod(StrIsASCII))
	bindNoArgs("islower", strBoolMethod(StrIsLower))
	bindNoArgs("isupper", strBoolMethod(StrIsUpper))
	bindNoArgs("istitle", strBoolMethod(StrIsTitle))
	bindNoArgs("isidentifier", strBoolMethod(StrIsIdentifier))
	bindNoArgs("isdecimal", strBoolMethod(StrIsDecimal))
	bindNoArgs("isnumeric", strBoolMethod(StrIsNumeric))
	bindNoArgs("isprintable", strBoolMethod(StrIsPrintable))
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
	bind("__getnewargs__", strGetNewArgs)
	bind("__getitem__", strGetItemMethod)
	bind("__mul__", strMulMethod)
	bind("__rmul__", strRMulMethod)
	bind("__add__", strAddMethod)
	bind("__mod__", strModMethod)
	bind("__sizeof__", strSizeofMethod)
}

// strSizeofMethod ports str.__sizeof__. CPython reports the compact
// unicode layout: a compact ASCII string is sizeof(PyASCIIObject) plus
// one byte per code point plus the trailing NUL; any other compact
// string is sizeof(PyCompactUnicodeObject) plus (len+1) units of the
// PEP 393 kind width. gopy stores text as UTF-8 but mirrors the same
// reported size so sys.getsizeof and __sizeof__ stay byte-compatible.
//
// CPython: Objects/unicodeobject.c:13991 unicode_sizeof_impl
func strSizeofMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __sizeof__() takes no arguments (%d given)", len(args)-1)
	}
	u, ok := args[0].(*Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__sizeof__' for 'str' objects doesn't apply to a '%s' object", typeNameOf(args[0]))
	}
	charSize, structSize := u.charSizeAndStruct()
	return NewInt(int64(structSize + charSize*(u.length+1))), nil
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

// selfStrUnicode returns the *Unicode receiver so callers can read
// the kind tag and length without re-classifying. Used by the
// kind-dispatched search / index / count fast paths.
//
// CPython: Objects/unicodeobject.c:9680 any_find_slice reads
// PyUnicode_KIND off the receiver to pick the asciilib_* / ucs1lib_*
// / ucs2lib_* / ucs4lib_* path.
func selfStrUnicode(args []Object, name string) (*Unicode, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: descriptor '%s' of 'str' needs an argument", name)
	}
	u, ok := args[0].(*Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '%s' requires a 'str' object", name)
	}
	return u, nil
}

// strRange parses the optional start/end integer arguments at positions
// 2 and 3 (args[0]=self, args[1]=pattern). None is accepted as "not
// specified", matching CPython's clinic "Py_ssize_t start=0" default.
// math.MaxInt signals "end not specified" — adjustFindIndices clamps to n.
//
// CPython: Objects/unicodeobject.c:9680 any_find_slice start/end parsing
// strRangeNamed parses optional (start, end) from args[2:] and
// rejects any arguments beyond args[3]. methodName is used in the
// TypeError message so callers can satisfy CPython's "find() takes at
// most 3 arguments (N given)" pattern.
//
// CPython: Objects/unicodeobject.c find/count/startswith/endswith arg
// parsing in each _impl function.
func strRangeNamed(args []Object, methodName string) (int, int, error) {
	const base = 2
	if len(args) > base+2 {
		if methodName != "" {
			return 0, 0, fmt.Errorf("TypeError: %s() takes at most 3 arguments (%d given)", methodName, len(args)-1)
		}
		return 0, 0, fmt.Errorf("TypeError: takes at most 3 arguments (%d given)", len(args)-1)
	}
	start, end := 0, math.MaxInt
	if len(args) > base && !IsNone(args[base]) {
		i, ok := args[base].(*Int)
		if !ok {
			return 0, 0, fmt.Errorf("TypeError: slice indices must be integers or None")
		}
		n, _ := i.Int64()
		start = int(n)
	}
	if len(args) > base+1 && !IsNone(args[base+1]) {
		i, ok := args[base+1].(*Int)
		if !ok {
			return 0, 0, fmt.Errorf("TypeError: slice indices must be integers or None")
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

func strStartsWithMethod(args []Object, _ map[string]Object) (Object, error) {
	u, err := selfStrUnicode(args, "startswith")
	if err != nil {
		return nil, err
	}
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: startswith() takes at least 1 argument")
	}
	start, end, rerr := strRangeNamed(args, "startswith")
	if rerr != nil {
		return nil, rerr
	}

	check := func(prefix string) bool { return strStartsWithKind(u, prefix, start, end) }
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
		return nil, fmt.Errorf("TypeError: startswith first arg must be str or a tuple of str, not %s", typeNameOf(args[1]))
	}
	return NewBool(check(p)), nil
}

func strEndsWithMethod(args []Object, _ map[string]Object) (Object, error) {
	u, err := selfStrUnicode(args, "endswith")
	if err != nil {
		return nil, err
	}
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: endswith() takes at least 1 argument")
	}
	start, end, rerr := strRangeNamed(args, "endswith")
	if rerr != nil {
		return nil, rerr
	}

	check := func(suffix string) bool { return strEndsWithKind(u, suffix, start, end) }
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
		return nil, fmt.Errorf("TypeError: endswith first arg must be str or a tuple of str, not %s", typeNameOf(args[1]))
	}
	return NewBool(check(p)), nil
}

func strFindMethod(args []Object, _ map[string]Object) (Object, error) {
	u, err := selfStrUnicode(args, "find")
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
	start, end, rerr := strRangeNamed(args, "find")
	if rerr != nil {
		return nil, rerr
	}

	return NewInt(int64(strFindKind(u, needle, start, end))), nil
}

func strRFindMethod(args []Object, _ map[string]Object) (Object, error) {
	u, err := selfStrUnicode(args, "rfind")
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
	start, end, rerr := strRangeNamed(args, "rfind")
	if rerr != nil {
		return nil, rerr
	}

	return NewInt(int64(strRFindKind(u, needle, start, end))), nil
}

func strIndexMethod(args []Object, _ map[string]Object) (Object, error) {
	u, err := selfStrUnicode(args, "index")
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
	start, end, rerr := strRangeNamed(args, "index")
	if rerr != nil {
		return nil, rerr
	}

	i, ierr := strIndexKind(u, needle, start, end)
	if ierr != nil {
		return nil, ierr
	}
	return NewInt(int64(i)), nil
}

func strRIndexMethod(args []Object, _ map[string]Object) (Object, error) {
	u, err := selfStrUnicode(args, "rindex")
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
	start, end, rerr := strRangeNamed(args, "rindex")
	if rerr != nil {
		return nil, rerr
	}

	i, ierr := strRIndexKind(u, needle, start, end)
	if ierr != nil {
		return nil, ierr
	}
	return NewInt(int64(i)), nil
}

func strCountMethod(args []Object, _ map[string]Object) (Object, error) {
	u, err := selfStrUnicode(args, "count")
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
	start, end, rerr := strRangeNamed(args, "count")
	if rerr != nil {
		return nil, rerr
	}

	return NewInt(int64(strCountKind(u, needle, start, end))), nil
}

func strSimple1(fn func(string) string) func([]Object, map[string]Object) (Object, error) {
	return func(args []Object, kwargs map[string]Object) (Object, error) {
		s, err := selfStr(args, "str-method")
		if err != nil {
			return nil, err
		}
		if len(args) > 1 || len(kwargs) > 0 {
			return nil, fmt.Errorf("TypeError: takes no arguments (%d given)", len(args)-1)
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

func strSplitMethod(args []Object, kwargs map[string]Object) (Object, error) {
	s, err := selfStr(args, "split")
	if err != nil {
		return nil, err
	}
	if len(args) > 3 {
		return nil, fmt.Errorf("TypeError: split() takes at most 2 arguments (%d given)", len(args)-1)
	}
	sepObj := None()
	if len(args) >= 2 {
		sepObj = args[1]
	} else if kw, ok := kwargs["sep"]; ok {
		sepObj = kw
	}
	maxsplit := -1
	if len(args) >= 3 {
		if i, ok := args[2].(*Int); ok {
			n, _ := i.Int64()
			maxsplit = int(n)
		}
	} else if kw, ok := kwargs["maxsplit"]; ok {
		if i, ok2 := kw.(*Int); ok2 {
			n, _ := i.Int64()
			maxsplit = int(n)
		}
	}
	sep := ""
	if sepObj != None() {
		c, cerr := strNeedle(sepObj)
		if cerr != nil {
			return nil, cerr
		}
		if c == "" {
			return nil, fmt.Errorf("ValueError: empty separator")
		}
		sep = c
	}
	parts, perr := StrSplit(s, sep, maxsplit)
	if perr != nil {
		return nil, perr
	}
	return strListFromGoSlice(parts), nil
}

func strRSplitMethod(args []Object, kwargs map[string]Object) (Object, error) {
	s, err := selfStr(args, "rsplit")
	if err != nil {
		return nil, err
	}
	if len(args) > 3 {
		return nil, fmt.Errorf("TypeError: rsplit() takes at most 2 arguments (%d given)", len(args)-1)
	}
	sepObj := None()
	if len(args) >= 2 {
		sepObj = args[1]
	} else if kw, ok := kwargs["sep"]; ok {
		sepObj = kw
	}
	maxsplit := -1
	if len(args) >= 3 {
		if i, ok := args[2].(*Int); ok {
			n, _ := i.Int64()
			maxsplit = int(n)
		}
	} else if kw, ok := kwargs["maxsplit"]; ok {
		if i, ok2 := kw.(*Int); ok2 {
			n, _ := i.Int64()
			maxsplit = int(n)
		}
	}
	sep := ""
	if sepObj != None() {
		c, cerr := strNeedle(sepObj)
		if cerr != nil {
			return nil, cerr
		}
		if c == "" {
			return nil, fmt.Errorf("ValueError: empty separator")
		}
		sep = c
	}
	parts, perr := StrRSplit(s, sep, maxsplit)
	if perr != nil {
		return nil, perr
	}
	return strListFromGoSlice(parts), nil
}

func strSplitLinesMethod(args []Object, kwargs map[string]Object) (Object, error) {
	s, err := selfStr(args, "splitlines")
	if err != nil {
		return nil, err
	}
	if len(args) > 2 {
		return nil, fmt.Errorf("TypeError: splitlines() takes at most 1 argument (%d given)", len(args)-1)
	}
	keepends := false
	if len(args) >= 2 {
		keepends = strToBool(args[1])
	}
	if kw, ok := kwargs["keepends"]; ok {
		if len(args) >= 2 {
			return nil, fmt.Errorf("TypeError: splitlines() got multiple values for argument 'keepends'")
		}
		keepends = strToBool(kw)
	}
	return strListFromGoSlice(StrSplitLines(s, keepends)), nil
}

func strReplaceMethod(args []Object, kwargs map[string]Object) (Object, error) {
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
	} else if cv, ok := kwargs["count"]; ok {
		if i, ok := cv.(*Int); ok {
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
	it, ierr := Iter(args[1])
	if ierr != nil {
		return nil, fmt.Errorf("TypeError: can only join an iterable")
	}
	var parts []Object
	for {
		v, e := IterNext(it)
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
	sepU, ok := args[0].(*Unicode)
	if !ok {
		sepU = NewStr(sep).(*Unicode)
	}
	out, jerr := StrJoinUnicode(sepU, parts)
	if jerr != nil {
		return nil, jerr
	}
	return out, nil
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
	return func(args []Object, kwargs map[string]Object) (Object, error) {
		s, err := selfStr(args, "is*")
		if err != nil {
			return nil, err
		}
		if len(args) > 1 || len(kwargs) > 0 {
			return nil, fmt.Errorf("TypeError: takes no arguments (%d given)", len(args)-1)
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

// strEncodeMethod ports str.encode(encoding="utf-8", errors="strict").
// Routes through codecs.Encode so non-utf-8 encodings (latin-1, ascii,
// the multi-byte codecs the io textio port registers) produce the
// right bytes; the original implementation returned the raw utf-8
// backing slice for every encoding.
//
// CPython: Objects/unicodeobject.c:L13262 unicode_encode_impl
func strEncodeMethod(args []Object, kwargs map[string]Object) (Object, error) {
	s, err := selfStr(args, "encode")
	if err != nil {
		return nil, err
	}
	encoding := "utf-8"
	errorsName := "strict"
	rest := args[1:]
	if len(rest) > 0 {
		u, ok := rest[0].(*Unicode)
		if !ok {
			return nil, fmt.Errorf("TypeError: encode() argument 'encoding' must be str, not %s", rest[0].Type().Name)
		}
		encoding = u.Value()
	}
	if len(rest) > 1 {
		u, ok := rest[1].(*Unicode)
		if !ok {
			return nil, fmt.Errorf("TypeError: encode() argument 'errors' must be str, not %s", rest[1].Type().Name)
		}
		errorsName = u.Value()
	}
	if v, ok := kwargs["encoding"]; ok {
		u, isStr := v.(*Unicode)
		if !isStr {
			return nil, fmt.Errorf("TypeError: encode() argument 'encoding' must be str, not %s", v.Type().Name)
		}
		encoding = u.Value()
	}
	if v, ok := kwargs["errors"]; ok {
		u, isStr := v.(*Unicode)
		if !isStr {
			return nil, fmt.Errorf("TypeError: encode() argument 'errors' must be str, not %s", v.Type().Name)
		}
		errorsName = u.Value()
	}
	// Trigger codec lookup so _is_text_encoding is populated for Python codecs.
	if _, lerr := codecs.Lookup(encoding); lerr != nil {
		return nil, lerr
	}
	// Reject non-text encodings (binary transforms, str→str transforms).
	// CPython: Objects/unicodeobject.c:L13262 unicode_encode_impl _is_text_encoding check
	if !codecs.IsTextEncoding(encoding) {
		return nil, fmt.Errorf("LookupError: '%s' is not a text encoding; use codecs.encode() to handle arbitrary codecs", encoding)
	}
	if cerr := codecs.CheckEncodingErrors(encoding, errorsName); cerr != nil {
		return nil, cerr
	}
	out, _, encErr := codecs.Encode(s, encoding, errorsName)
	if encErr != nil {
		if FormatNoteHook != nil {
			FormatNoteHook(fmt.Sprintf("encoding with '%s' codec failed", encoding))
		}
		return nil, encErr
	}
	return NewBytes(out), nil
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
	return out, nil
}

// strFormatExpand walks the str.format() template and routes every literal
// chunk and rendered field through UnicodeWriter so Finish() builds the
// result *Unicode with kind/maxchar pre-populated.
//
// CPython: Objects/stringlib/unicode_format.h:1306 do_string_format
func strFormatExpand(s string, args []Object, kwargs map[string]Object) (*Unicode, error) {
	auto := 0
	return strFormatExpandInner(s, args, kwargs, nil, &auto, 0)
}

// strFormatExpandInner is the shared implementation of str.format() and
// str.format_map().  mapObj, when non-nil, routes named-key lookups through
// GetItem(mapObj, key) so that __missing__ / __getitem__ on the mapping is
// honored.  auto tracks the auto-numbering / manual-numbering mode across
// nested spec expansions (the same counter is re-used for both the field
// name and the spec string, matching CPython's AutoNumber behavior).
//
// CPython: Objects/stringlib/unicode_format.h:906 build_string
func strFormatExpandInner(s string, args []Object, kwargs map[string]Object, mapObj Object, auto *int, depth int) (*Unicode, error) {
	if depth > 1 {
		return nil, fmt.Errorf("ValueError: Max string recursion exceeded")
	}
	var w UnicodeWriter
	w.Init()
	w.overallocate = true
	i := 0
	for i < len(s) {
		next := strings.IndexAny(s[i:], "{}")
		if next < 0 {
			if err := writeBodyChunk(&w, s[i:]); err != nil {
				return nil, err
			}
			break
		}
		if next > 0 {
			if err := writeBodyChunk(&w, s[i:i+next]); err != nil {
				return nil, err
			}
			i += next
		}
		c := s[i]
		if c == '{' {
			if i+1 < len(s) && s[i+1] == '{' {
				if err := w.WriteChar('{'); err != nil {
					return nil, err
				}
				i += 2
				continue
			}
			// Find the matching '}', respecting nested braces so that
			// specs like {:{}} are parsed correctly.
			//
			// CPython: Objects/stringlib/unicode_format.h:619 parse_field (count loop)
			end := strFormatMatchBrace(s, i+1)
			if end < 0 {
				return nil, fmt.Errorf("ValueError: Single '{' encountered in format string")
			}
			field := s[i+1 : end]
			rendered, ferr := strFormatFieldInner(field, args, kwargs, mapObj, auto, depth)
			if ferr != nil {
				return nil, ferr
			}
			if err := writeBodyChunk(&w, rendered); err != nil {
				return nil, err
			}
			i = end + 1
			continue
		}
		// c == '}'
		if i+1 < len(s) && s[i+1] == '}' {
			if err := w.WriteChar('}'); err != nil {
				return nil, err
			}
			i += 2
			continue
		}
		return nil, fmt.Errorf("ValueError: Single '}' encountered in format string")
	}
	return w.Finish(), nil
}

// strFormatMatchBrace finds the matching '}' for an opening '{'.
// start is the index immediately after the opening '{'.
// Returns the index of the matching '}', or -1 if not found.
// Content inside '[...]' brackets is skipped so that brace characters
// in subscript keys (e.g. "{[{]}") are not counted.
//
// CPython: Objects/stringlib/unicode_format.h:619 parse_field (depth counter)
func strFormatMatchBrace(s string, start int) int {
	depth := 1
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '[':
			// Skip to the matching ']', so brace chars in subscript keys
			// are ignored (e.g. "{[{]}" has key "{").
			for i++; i < len(s) && s[i] != ']'; i++ {
			}
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func strFormatFieldInner(field string, args []Object, kwargs map[string]Object, mapObj Object, auto *int, depth int) (string, error) {
	// Find the ':' that separates the field name from the format spec,
	// skipping any '[...]' subscript components in the name.
	//
	// CPython: Objects/stringlib/unicode_format.h:554 parse_field
	colon := -1
	for i := 0; i < len(field); i++ {
		switch field[i] {
		case '[':
			for i++; i < len(field) && field[i] != ']'; i++ {
			}
		case ':':
			colon = i
			goto colonDone
		}
	}
colonDone:
	name, spec := field, ""
	if colon >= 0 {
		name, spec = field[:colon], field[colon+1:]
	}
	// Conversion suffix: name!s / name!r / name!a.
	// Skip '[...]' subscripts so that '!' inside a key is not mistaken
	// for a conversion specifier.
	//
	// CPython: Objects/stringlib/unicode_format.h:702 parse_field
	conversion := byte(0)
	bang := -1
	for i := 0; i < len(name); i++ {
		switch name[i] {
		case '[':
			for i++; i < len(name) && name[i] != ']'; i++ {
			}
		case '!':
			bang = i
		}
	}
	if bang >= 0 {
		if bang+1 >= len(name) {
			return "", fmt.Errorf("ValueError: end of format while looking for conversion specifier")
		}
		conversion = name[bang+1]
		if len(name) > bang+2 {
			return "", fmt.Errorf("ValueError: expected ':' after conversion specifier")
		}
		name = name[:bang]
	}
	v, err := strFormatLookupInner(name, args, kwargs, mapObj, auto)
	if err != nil {
		return "", err
	}
	switch conversion {
	case 0:
		// no conversion
	case 's':
		sv, sErr := StrObject(v)
		if sErr != nil {
			return "", sErr
		}
		v = sv
	case 'r':
		rv, rErr := ReprObject(v)
		if rErr != nil {
			return "", rErr
		}
		v = rv
	case 'a':
		rv, rErr := Repr(v)
		if rErr != nil {
			return "", rErr
		}
		v = NewStr(asciiEscape(rv))
	default:
		return "", fmt.Errorf("ValueError: Unknown conversion specifier %c", conversion)
	}
	// If the format spec itself contains field references (e.g., {:{}}),
	// recursively expand them before passing to PyObject_Format.
	//
	// CPython: Objects/stringlib/unicode_format.h:833 output_markup (build_string on spec)
	if strings.ContainsRune(spec, '{') {
		expanded, expErr := strFormatExpandInner(spec, args, kwargs, mapObj, auto, depth+1)
		if expErr != nil {
			return "", expErr
		}
		spec = expanded.Value()
	}
	// Dispatch through PyObject_Format so each replacement uses the
	// argument's own __format__ slot.
	//
	// CPython: Objects/stringlib/unicode_format.h:1024 output_markup
	return Format(v, spec)
}

// strFormatLookupInner resolves a field name against args/kwargs (or mapObj
// for format_map) and applies any trailing ".attr" / "[key]" components.
//
// The auto counter encodes numbering mode:
//
//	>= 0 : auto mode (or unset when 0 with no fields consumed yet)
//	-1   : manual mode
//
// CPython: Objects/stringlib/unicode_format.h:392 get_field_object
func strFormatLookupInner(name string, args []Object, kwargs map[string]Object, mapObj Object, auto *int) (Object, error) {
	base, rest := strFormatBaseName(name)
	// Validate component syntax early so bracket errors surface as
	// ValueError regardless of whether the base lookup would fail.
	//
	// CPython: Objects/stringlib/unicode_format.h:610 parse_field ([ scan)
	if err := strFormatValidateRest(rest); err != nil {
		return nil, err
	}
	var obj Object
	if base == "" {
		// Auto-numbered field: {}.
		if mapObj != nil {
			return nil, fmt.Errorf("ValueError: Format string contains positional fields")
		}
		if *auto < 0 {
			return nil, fmt.Errorf("ValueError: cannot switch from manual field specification to automatic field numbering")
		}
		if *auto >= len(args) {
			return nil, fmt.Errorf("IndexError: tuple index out of range")
		}
		obj = args[*auto]
		*auto++
	} else if len(base) > 0 && strIsAllDigits(base) {
		// Manually-numbered field: {0}, {1}, etc.
		// Only purely-numeric names are treated as integer indices;
		// mixed strings like "0]" are keyword lookups per CPython.
		if mapObj != nil {
			return nil, fmt.Errorf("ValueError: Format string contains positional fields")
		}
		if *auto > 0 {
			return nil, fmt.Errorf("ValueError: cannot switch from automatic field numbering to manual field specification")
		}
		*auto = -1
		idx64, parseErr := strconv.ParseInt(base, 10, 64)
		if parseErr != nil || idx64 < 0 {
			return nil, fmt.Errorf("ValueError: too many decimal digits in format string")
		}
		idx := int(idx64)
		if idx >= len(args) {
			return nil, fmt.Errorf("IndexError: tuple index out of range")
		}
		obj = args[idx]
	} else {
		// Named field: {name}.
		if mapObj != nil {
			v, err := GetItem(mapObj, NewStr(base))
			if err != nil {
				return nil, err
			}
			obj = v
		} else {
			v, ok := kwargs[base]
			if !ok {
				return nil, fmt.Errorf("KeyError: '%s'", base)
			}
			obj = v
		}
	}
	// Apply remaining components: ".attr" or "[key]".
	//
	// CPython: Objects/stringlib/unicode_format.h:454 FieldNameIterator_next loop
	for len(rest) > 0 {
		if rest[0] == '.' {
			rest = rest[1:]
			end := strings.IndexAny(rest, ".[")
			var attr string
			if end < 0 {
				attr, rest = rest, ""
			} else {
				attr, rest = rest[:end], rest[end:]
			}
			if attr == "" {
				return nil, fmt.Errorf("ValueError: Empty attribute in format string")
			}
			var err error
			obj, err = GetAttr(obj, NewStr(attr))
			if err != nil {
				return nil, err
			}
		} else if rest[0] == '[' {
			close := strings.IndexByte(rest, ']')
			if close < 0 {
				return nil, fmt.Errorf("ValueError: missing ']' in format string")
			}
			key := rest[1:close]
			rest = rest[close+1:]
			// All-digit key → integer index; otherwise string key.
			isInt := len(key) > 0
			for _, r := range key {
				if r < '0' || r > '9' {
					isInt = false
					break
				}
			}
			var err error
			if isInt {
				idx64, parseErr := strconv.ParseInt(key, 10, 64)
				if parseErr != nil || idx64 < 0 {
					return nil, fmt.Errorf("ValueError: too many decimal digits in format string")
				}
				obj, err = GetItem(obj, NewInt(idx64))
			} else {
				obj, err = GetItem(obj, NewStr(key))
			}
			if err != nil {
				return nil, err
			}
		} else {
			return nil, fmt.Errorf("ValueError: Only '.' or '[' may follow ']' in format field specifier")
		}
	}
	return obj, nil
}

// strFormatBaseName splits a field name into the leading identifier and the
// remaining component string. E.g. "d[a].b" → ("d", "[a].b").
func strIsAllDigits(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func strFormatBaseName(name string) (base, rest string) {
	i := strings.IndexAny(name, ".[{")
	if i < 0 {
		return name, ""
	}
	return name[:i], name[i:]
}

// strFormatValidateRest checks field component syntax: every component
// must start with '.' or '[', and every '[' must have a matching ']'.
// This is called before the base-object lookup so invalid component
// syntax always surfaces as ValueError rather than a lookup error.
//
// CPython: Objects/stringlib/unicode_format.h:610 parse_field ([ → '] scan)
func strFormatValidateRest(rest string) error {
	for i := 0; i < len(rest); {
		switch rest[i] {
		case '.':
			// Skip dot and the attribute name that follows.
			i++
			for i < len(rest) && rest[i] != '.' && rest[i] != '[' {
				i++
			}
		case '[':
			close := strings.IndexByte(rest[i:], ']')
			if close < 0 {
				return fmt.Errorf("ValueError: expected '}' before end of string")
			}
			i += close + 1
		default:
			return fmt.Errorf("ValueError: Only '.' or '[' may follow ']' in format field specifier")
		}
	}
	return nil
}

func strFormatMapMethod(args []Object, _ map[string]Object) (Object, error) {
	s, err := selfStr(args, "format_map")
	if err != nil {
		return nil, err
	}
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: format_map() takes 1 argument")
	}
	mapObj := args[1]
	if !strIsMappingLike(mapObj) {
		return nil, fmt.Errorf("TypeError: format_map() argument must be a mapping")
	}
	auto := 0
	out, ferr := strFormatExpandInner(s, nil, nil, mapObj, &auto, 0)
	if ferr != nil {
		return nil, ferr
	}
	return out, nil
}

// strIsMappingLike returns true when o is a dict, a dict subclass, or a user
// type with __getitem__.  Lists and other sequence-only types return false.
//
// CPython: abstract.c:2260 PyMapping_Check
func strIsMappingLike(o Object) bool {
	if o.Type().Mapping != nil {
		return true
	}
	if o.Type().IsUser {
		descr, _ := LookupDescriptor(o.Type(), "__getitem__")
		return descr != nil
	}
	return false
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
			if n < 0 || n > 0x10FFFF {
				return nil, fmt.Errorf("ValueError: character mapping must be in range(0x110000)")
			}
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
			return nil, fmt.Errorf("ValueError: string keys in translate table must be of length 1")
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
func strExpandTabsMethod(args []Object, kwargs map[string]Object) (Object, error) {
	s, err := selfStr(args, "expandtabs")
	if err != nil {
		return nil, err
	}
	if len(args) > 2 {
		return nil, fmt.Errorf("TypeError: expandtabs() takes at most 1 argument (%d given)", len(args)-1)
	}
	tabsize := 8
	if len(args) >= 2 {
		if i, ok := args[1].(*Int); ok {
			n, _ := i.Int64()
			tabsize = int(n)
		}
	} else if kw, ok := kwargs["tabsize"]; ok {
		if i, ok2 := kw.(*Int); ok2 {
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
	if len(args) > 2 {
		return nil, fmt.Errorf("TypeError: removeprefix() takes exactly one argument (%d given)", len(args)-1)
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
	if len(args) > 2 {
		return nil, fmt.Errorf("TypeError: removesuffix() takes exactly one argument (%d given)", len(args)-1)
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

func allSpace(s string) bool {
	for _, r := range s {
		if !IsSpaceRune(r) {
			return false
		}
	}
	return true
}

// strGetNewArgs returns a 1-tuple containing a copy of the string,
// used by pickle to reconstruct str instances.
//
// CPython: Objects/unicodeobject.c:15184 unicode_getnewargs
func strGetNewArgs(args []Object, _ map[string]Object) (Object, error) {
	s, err := selfStr(args, "__getnewargs__")
	if err != nil {
		return nil, err
	}
	return NewTuple([]Object{NewStr(s)}), nil
}

// strGetItemMethod implements str.__getitem__: handles both integer
// indexing and slice objects.
//
// CPython: Objects/unicodeobject.c:15232 unicode_subscript
func strGetItemMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __getitem__() takes exactly one argument (%d given)", len(args)-1)
	}
	u, ok := args[0].(*Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__getitem__' requires a 'str' object")
	}
	if sl, ok := args[1].(*Slice); ok {
		return StrGetSlice(u, sl)
	}
	// Try __index__ coercion (e.g. numpy int types), same as
	// CPython's PyIndex_AsSsize_t call in unicode_subscript.
	//
	// CPython: Objects/unicodeobject.c:15232 unicode_subscript
	idx, err := NumberIndex(args[1])
	if err != nil {
		return nil, fmt.Errorf("TypeError: string indices must be integers, not '%s'", typeNameOf(args[1]))
	}
	i, ok2 := idx.(*Int)
	if !ok2 {
		return nil, fmt.Errorf("TypeError: string indices must be integers, not '%s'", typeNameOf(args[1]))
	}
	n, _ := i.Int64()
	return unicodeGetItemKind(u, int(n))
}

// StrGetSlice extracts a substring via a slice object, honoring
// negative indices and step.
//
// CPython: Objects/unicodeobject.c:15232 unicode_subscript (slice path)
func StrGetSlice(u *Unicode, sl *Slice) (Object, error) {
	n := u.length
	start, stop, step, slicelen, err := sl.GetIndices(n)
	if err != nil {
		return nil, err
	}
	if slicelen == 0 {
		return NewStr(""), nil
	}
	if step == 1 {
		if u.ascii {
			return NewStr(u.v[start:stop]), nil
		}
		runes := []rune(u.v)
		return NewStr(string(runes[start:stop])), nil
	}
	runes := []rune(u.v)
	out := make([]rune, slicelen)
	for i := range slicelen {
		out[i] = runes[start+i*step]
	}
	return NewStr(string(out)), nil
}

// strMulMethod implements str.__mul__(n): 'ab' * 3 == 'ababab'.
// Raises TypeError for non-integer arguments (CPython routes through
// sq_repeat which calls _PyIndex_AsSsize_t, not nb_multiply).
//
// CPython: Objects/unicodeobject.c:11556 unicode_repeat
func strMulMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __mul__() takes exactly one argument (%d given)", len(args)-1)
	}
	u, ok := args[0].(*Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__mul__' requires a 'str' object")
	}
	// str.__mul__ is the sq_repeat slot wrapped by wrap_indexargfunc, which
	// coerces the count through PyNumber_AsSsize_t and raises TypeError when
	// the argument has no __index__. That differs from the nb_multiply
	// operator path, which returns NotImplemented; the bound method raises.
	//
	// CPython: Objects/typeobject.c wrap_indexargfunc / Objects/abstract.c PyNumber_AsSsize_t
	n, err := NumberIndex(args[1])
	if err != nil {
		return nil, err
	}
	ni, ok2 := n.(*Int)
	if !ok2 {
		return nil, fmt.Errorf("TypeError: '%s' object cannot be interpreted as an integer", args[1].Type().Name)
	}
	nv, fits := ni.Int64()
	if !fits {
		return nil, fmt.Errorf("OverflowError: cannot fit 'int' into an index-sized integer")
	}
	return strType.Sequence.Repeat(u, int(nv))
}

// strRMulMethod implements str.__rmul__(n): 3 * 'ab' == 'ababab'.
//
// CPython: Objects/unicodeobject.c:11556 unicode_repeat
func strRMulMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __rmul__() takes exactly one argument (%d given)", len(args)-1)
	}
	u, ok := args[0].(*Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__rmul__' requires a 'str' object")
	}
	// Like __mul__, str.__rmul__ wraps sq_repeat and raises TypeError for a
	// non-index argument rather than returning NotImplemented.
	//
	// CPython: Objects/typeobject.c wrap_indexargfunc / Objects/abstract.c PyNumber_AsSsize_t
	n, err := NumberIndex(args[1])
	if err != nil {
		return nil, err
	}
	ni, ok2 := n.(*Int)
	if !ok2 {
		return nil, fmt.Errorf("TypeError: '%s' object cannot be interpreted as an integer", args[1].Type().Name)
	}
	nv, fits := ni.Int64()
	if !fits {
		return nil, fmt.Errorf("OverflowError: cannot fit 'int' into an index-sized integer")
	}
	return strType.Sequence.Repeat(u, int(nv))
}

// strAddMethod implements str.__add__(other): 'ab' + 'cd' == 'abcd'.
// Returns NotImplemented for non-str right-hand sides.
//
// CPython: Objects/unicodeobject.c:11641 PyUnicode_Concat
func strAddMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __add__() takes exactly one argument (%d given)", len(args)-1)
	}
	u, ok := args[0].(*Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__add__' requires a 'str' object")
	}
	v, ok := args[1].(*Unicode)
	if !ok {
		return NotImplemented(), nil
	}
	return NewStr(u.v + v.v), nil
}

// strModMethod implements str.__mod__(args): '%s' % 'x' == 'x'.
//
// CPython: Objects/unicodeobject.c:14538 unicode_mod
func strModMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __mod__() takes exactly one argument (%d given)", len(args)-1)
	}
	if _, ok := args[0].(*Unicode); !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__mod__' requires a 'str' object")
	}
	return NumberRemainder(args[0], args[1])
}
