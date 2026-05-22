// Package _codecs is the gopy port of CPython's _codecs built-in module.
// It exposes Python's codec lookup infrastructure: the registry search
// functions, encode/decode helpers, and the per-codec convenience
// functions for UTF-8, ASCII, and Latin-1.
//
// The back-end Go codec logic lives in the codecs package; this module
// is the Python-visible surface that `import _codecs` resolves to.
//
// CPython: Modules/_codecsmodule.c:1 _codecs module
package _codecs

import (
	"fmt"
	"sync"

	"github.com/tamnd/gopy/codecs"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("_codecs", buildModule)
}

// buildModule constructs the _codecs module dict. Mirrors PyInit__codecs /
// _codecs_exec on the C side.
//
// CPython: Modules/_codecsmodule.c:507 _codecs_exec
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_codecs")
	d := m.Dict()
	fns := []struct {
		name string
		fn   func([]objects.Object, map[string]objects.Object) (objects.Object, error)
	}{
		{"lookup", codecsLookup},
		{"encode", codecsEncode},
		{"decode", codecsDecode},
		{"utf_8_encode", utf8Encode},
		{"utf_8_decode", utf8Decode},
		{"ascii_encode", asciiEncode},
		{"ascii_decode", asciiDecode},
		{"latin_1_encode", latin1Encode},
		{"latin_1_decode", latin1Decode},
		{"charmap_encode", charmapEncode},
		{"charmap_decode", charmapDecode},
		{"charmap_build", charmapBuild},
		{"register", codecsRegister},
		{"register_error", codecsRegisterError},
		{"lookup_error", codecsLookupError},
	}
	for _, f := range fns {
		if err := d.SetItem(objects.NewStr(f.name), objects.NewBuiltinFunction(f.name, f.fn)); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Error handler registry backed by the codecs package.
// ---------------------------------------------------------------------------

// pyErrorHandlers stores Python-level callable error handlers registered
// via register_error. The codecs package holds Go-level handlers; we
// bridge calls through pyCallErrorHandler.
var (
	pyErrMu       sync.RWMutex
	pyErrHandlers = map[string]objects.Object{}
)

// The well-known error handler names (strict, ignore, replace,
// xmlcharrefreplace, backslashreplace, namereplace, surrogatepass,
// surrogateescape) are already seeded by the codecs package's init,
// so we no longer wrap them here. A previous version of this file
// re-registered each name with a wrapper that called LookupError,
// which simply returned the wrapper itself and recursed forever.

// ---------------------------------------------------------------------------
// lookup(encoding) -> (encoder, decoder, stream_reader, stream_writer)
//
// CPython: Modules/_codecsmodule.c:90 _codecs_lookup_impl
// ---------------------------------------------------------------------------

// codecInfoType is the CodecInfo type object returned by lookup().
// CPython returns a tuple subtype; here we return a plain 4-tuple.
var codecInfoType = objects.NewType("CodecInfo", []*objects.Type{objects.ObjectType()})

func init() {
	codecInfoType.HasDict = true
	codecInfoType.Getattro = objects.GenericGetAttr
}

// codecsLookup implements _codecs.lookup(encoding).
//
// CPython: Modules/_codecsmodule.c:90 _codecs_lookup_impl
func codecsLookup(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: lookup() takes exactly 1 argument (%d given)", len(args))
	}
	name, ok := args[0].(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: lookup() argument must be str, not %s", args[0].Type().Name)
	}
	ci, err := codecs.Lookup(name.Value())
	if err != nil {
		return nil, err
	}
	return makeCodecInfo(ci), nil
}

// makeCodecInfo wraps a Go CodecInfo in a Python Instance whose dict
// exposes encoder/decoder callables, mirroring the CodecInfo namedtuple.
//
// CPython: Modules/_codecsmodule.c:34 codec_info_new
func makeCodecInfo(ci *codecs.CodecInfo) objects.Object {
	inst := objects.NewInstance(codecInfoType)

	encodeFn := objects.NewBuiltinFunction("encode", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		s, errors, err := encodeArgs(args, "encode")
		if err != nil {
			return nil, err
		}
		out, n, err := ci.Encode(s, errors)
		if err != nil {
			return nil, err
		}
		return objects.NewTuple([]objects.Object{
			objects.NewBytes(out),
			objects.NewInt(int64(n)),
		}), nil
	})

	decodeFn := objects.NewBuiltinFunction("decode", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		b, errors, err := decodeArgs(args, "decode")
		if err != nil {
			return nil, err
		}
		out, n, err := ci.Decode(b, errors)
		if err != nil {
			return nil, err
		}
		return objects.NewTuple([]objects.Object{
			objects.NewStr(out),
			objects.NewInt(int64(n)),
		}), nil
	})

	d := inst.EnsureDict()
	_ = d.SetItem(objects.NewStr("name"), objects.NewStr(ci.Name))
	_ = d.SetItem(objects.NewStr("encode"), encodeFn)
	_ = d.SetItem(objects.NewStr("decode"), decodeFn)
	// stream_reader and stream_writer are None until streaming codecs land.
	_ = d.SetItem(objects.NewStr("streamreader"), objects.None())
	_ = d.SetItem(objects.NewStr("streamwriter"), objects.None())
	return inst
}

// ---------------------------------------------------------------------------
// encode(obj, encoding='utf-8', errors='strict') -> bytes
//
// CPython: Modules/_codecsmodule.c:127 _codecs_encode_impl
// ---------------------------------------------------------------------------

func codecsEncode(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 || len(args) > 3 {
		return nil, fmt.Errorf("TypeError: encode() takes 1 to 3 arguments (%d given)", len(args))
	}
	str, ok := args[0].(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: encode() argument 1 must be str, not %s", args[0].Type().Name)
	}
	encoding := "utf-8"
	if len(args) >= 2 {
		e, ok := args[1].(*objects.Unicode)
		if !ok {
			return nil, fmt.Errorf("TypeError: encode() argument 2 must be str, not %s", args[1].Type().Name)
		}
		encoding = e.Value()
	}
	errors := "strict"
	if len(args) >= 3 {
		e, ok := args[2].(*objects.Unicode)
		if !ok {
			return nil, fmt.Errorf("TypeError: encode() argument 3 must be str, not %s", args[2].Type().Name)
		}
		errors = e.Value()
	}
	out, _, err := codecs.Encode(str.Value(), encoding, errors)
	if err != nil {
		return nil, err
	}
	return objects.NewBytes(out), nil
}

// ---------------------------------------------------------------------------
// decode(obj, encoding='utf-8', errors='strict') -> str
//
// CPython: Modules/_codecsmodule.c:165 _codecs_decode_impl
// ---------------------------------------------------------------------------

func codecsDecode(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 || len(args) > 3 {
		return nil, fmt.Errorf("TypeError: decode() takes 1 to 3 arguments (%d given)", len(args))
	}
	b, err := toBytes(args[0], "decode", 1)
	if err != nil {
		return nil, err
	}
	encoding := "utf-8"
	if len(args) >= 2 {
		e, ok := args[1].(*objects.Unicode)
		if !ok {
			return nil, fmt.Errorf("TypeError: decode() argument 2 must be str, not %s", args[1].Type().Name)
		}
		encoding = e.Value()
	}
	errors := "strict"
	if len(args) >= 3 {
		e, ok := args[2].(*objects.Unicode)
		if !ok {
			return nil, fmt.Errorf("TypeError: decode() argument 3 must be str, not %s", args[2].Type().Name)
		}
		errors = e.Value()
	}
	out, _, derr := codecs.Decode(b, encoding, errors)
	if derr != nil {
		return nil, derr
	}
	return objects.NewStr(out), nil
}

// ---------------------------------------------------------------------------
// utf_8_encode(str, errors='strict') -> (bytes, length)
//
// CPython: Modules/_codecsmodule.c:218 _codecs_utf_8_encode_impl
// ---------------------------------------------------------------------------

func utf8Encode(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	s, errors, err := encodeArgs(args, "utf_8_encode")
	if err != nil {
		return nil, err
	}
	out, n, err := codecs.Encode(s, "utf-8", errors)
	if err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{objects.NewBytes(out), objects.NewInt(int64(n))}), nil
}

// ---------------------------------------------------------------------------
// utf_8_decode(bytes, errors='strict', final=False) -> (str, length)
//
// CPython: Modules/_codecsmodule.c:244 _codecs_utf_8_decode_impl
// ---------------------------------------------------------------------------

func utf8Decode(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	b, errors, err := decodeArgs(args, "utf_8_decode")
	if err != nil {
		return nil, err
	}
	out, n, err := codecs.Decode(b, "utf-8", errors)
	if err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{objects.NewStr(out), objects.NewInt(int64(n))}), nil
}

// ---------------------------------------------------------------------------
// ascii_encode(str, errors='strict') -> (bytes, length)
//
// CPython: Modules/_codecsmodule.c:339 _codecs_ascii_encode_impl
// ---------------------------------------------------------------------------

func asciiEncode(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	s, errors, err := encodeArgs(args, "ascii_encode")
	if err != nil {
		return nil, err
	}
	out, n, err := codecs.Encode(s, "ascii", errors)
	if err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{objects.NewBytes(out), objects.NewInt(int64(n))}), nil
}

// ---------------------------------------------------------------------------
// ascii_decode(bytes, errors='strict', final=False) -> (str, length)
//
// CPython: Modules/_codecsmodule.c:361 _codecs_ascii_decode_impl
// ---------------------------------------------------------------------------

func asciiDecode(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	b, errors, err := decodeArgs(args, "ascii_decode")
	if err != nil {
		return nil, err
	}
	out, n, err := codecs.Decode(b, "ascii", errors)
	if err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{objects.NewStr(out), objects.NewInt(int64(n))}), nil
}

// ---------------------------------------------------------------------------
// latin_1_encode(str, errors='strict') -> (bytes, length)
//
// CPython: Modules/_codecsmodule.c:383 _codecs_latin_1_encode_impl
// ---------------------------------------------------------------------------

func latin1Encode(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	s, errors, err := encodeArgs(args, "latin_1_encode")
	if err != nil {
		return nil, err
	}
	out, n, err := codecs.Encode(s, "latin-1", errors)
	if err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{objects.NewBytes(out), objects.NewInt(int64(n))}), nil
}

// ---------------------------------------------------------------------------
// latin_1_decode(bytes, errors='strict', final=False) -> (str, length)
//
// CPython: Modules/_codecsmodule.c:406 _codecs_latin_1_decode_impl
// ---------------------------------------------------------------------------

func latin1Decode(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	b, errors, err := decodeArgs(args, "latin_1_decode")
	if err != nil {
		return nil, err
	}
	out, n, err := codecs.Decode(b, "latin-1", errors)
	if err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{objects.NewStr(out), objects.NewInt(int64(n))}), nil
}

// ---------------------------------------------------------------------------
// charmap_encode(str, errors=None, mapping=None) -> (bytes, length)
// charmap_decode(data, errors=None, mapping=None) -> (str, length)
// charmap_build(map) -> dict
//
// CPython: Modules/_codecsmodule.c:883 _codecs_charmap_encode_impl
// CPython: Modules/_codecsmodule.c:571 _codecs_charmap_decode_impl
// CPython: Modules/_codecsmodule.c:901 _codecs_charmap_build_impl
// CPython: Objects/unicodeobject.c:8618 PyUnicode_DecodeCharmap
// CPython: Objects/unicodeobject.c:9114 _PyUnicode_EncodeCharmap
// CPython: Objects/unicodeobject.c:8694 PyUnicode_BuildEncodingMap
// ---------------------------------------------------------------------------

// charmapErrors normalises the errors argument: missing or None -> "strict".
func charmapErrors(args []objects.Object, idx int) (string, error) {
	if idx >= len(args) {
		return "strict", nil
	}
	a := args[idx]
	if objects.IsNone(a) {
		return "strict", nil
	}
	s, ok := a.(*objects.Unicode)
	if !ok {
		return "", fmt.Errorf("TypeError: errors must be str or None, not %s", a.Type().Name)
	}
	return s.Value(), nil
}

// charmapMapping pulls the optional mapping arg. None or missing -> nil
// (meaning: fall back to latin-1).
func charmapMapping(args []objects.Object, idx int) objects.Object {
	if idx >= len(args) {
		return nil
	}
	if objects.IsNone(args[idx]) {
		return nil
	}
	return args[idx]
}

func charmapDecode(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 || len(args) > 3 {
		return nil, fmt.Errorf("TypeError: charmap_decode() takes 1 to 3 arguments (%d given)", len(args))
	}
	data, err := toBytes(args[0], "charmap_decode", 1)
	if err != nil {
		return nil, err
	}
	errs, err := charmapErrors(args, 1)
	if err != nil {
		return nil, err
	}
	mapping := charmapMapping(args, 2)
	out, err := charmapDecodeImpl(data, errs, mapping)
	if err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{objects.NewStr(out), objects.NewInt(int64(len(data)))}), nil
}

func charmapEncode(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 || len(args) > 3 {
		return nil, fmt.Errorf("TypeError: charmap_encode() takes 1 to 3 arguments (%d given)", len(args))
	}
	str, ok := args[0].(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: charmap_encode() argument 1 must be str, not %s", args[0].Type().Name)
	}
	errs, err := charmapErrors(args, 1)
	if err != nil {
		return nil, err
	}
	mapping := charmapMapping(args, 2)
	out, n, err := charmapEncodeImpl(str.Value(), errs, mapping)
	if err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{objects.NewBytes(out), objects.NewInt(int64(n))}), nil
}

func charmapBuild(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: charmap_build() takes exactly 1 argument (%d given)", len(args))
	}
	s, ok := args[0].(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: charmap_build() argument must be str, not %s", args[0].Type().Name)
	}
	return charmapBuildImpl(s.Value())
}

// charmapDecodeImpl mirrors PyUnicode_DecodeCharmap. When mapping is a
// Unicode string it is treated as a 256-char table indexed by the input
// byte; 0xFFFE entries are "undefined" and routed through the error
// handler. Mapping objects (dicts) map int->int/str/None.
//
// CPython: Objects/unicodeobject.c:8618 PyUnicode_DecodeCharmap
func charmapDecodeImpl(data []byte, errors string, mapping objects.Object) (string, error) {
	if mapping == nil {
		// Default to Latin-1.
		out, _, err := codecs.Decode(data, "latin-1", errors)
		return out, err
	}
	switch m := mapping.(type) {
	case *objects.Unicode:
		return charmapDecodeFromString(data, errors, []rune(m.Value()))
	case *objects.Dict:
		return charmapDecodeFromMapping(data, errors, m)
	default:
		return "", fmt.Errorf("TypeError: charmap_decode() mapping must be str or mapping, not %s", mapping.Type().Name)
	}
}

func charmapDecodeFromString(data []byte, errors string, table []rune) (string, error) {
	var b []rune
	i := 0
	for i < len(data) {
		ch := data[i]
		var x rune = 0xFFFE
		if int(ch) < len(table) {
			x = table[ch]
		}
		if x == 0xFFFE {
			rep, newpos, err := callDecodeErrorHandler("charmap", "character maps to <undefined>", data, i, i+1, errors)
			if err != nil {
				return "", err
			}
			b = append(b, []rune(rep)...)
			i = newpos
			continue
		}
		b = append(b, x)
		i++
	}
	return string(b), nil
}

func charmapDecodeFromMapping(data []byte, errors string, mapping *objects.Dict) (string, error) {
	var b []rune
	i := 0
	for i < len(data) {
		ch := data[i]
		key := objects.NewInt(int64(ch))
		item, err := mapping.GetItem(key)
		if err != nil || item == nil || objects.IsNone(item) {
			rep, newpos, herr := callDecodeErrorHandler("charmap", "character maps to <undefined>", data, i, i+1, errors)
			if herr != nil {
				return "", herr
			}
			b = append(b, []rune(rep)...)
			i = newpos
			continue
		}
		switch v := item.(type) {
		case *objects.Int:
			val, ok := v.Int64()
			if !ok || val < 0 || val > 0x10FFFF {
				return "", fmt.Errorf("TypeError: character mapping must be in range(0x110000)")
			}
			if val == 0xFFFE {
				rep, newpos, herr := callDecodeErrorHandler("charmap", "character maps to <undefined>", data, i, i+1, errors)
				if herr != nil {
					return "", herr
				}
				b = append(b, []rune(rep)...)
				i = newpos
				continue
			}
			b = append(b, rune(val))
		case *objects.Unicode:
			b = append(b, []rune(v.Value())...)
		default:
			return "", fmt.Errorf("TypeError: character mapping must return integer, None or str")
		}
		i++
	}
	return string(b), nil
}

// charmapEncodeImpl mirrors _PyUnicode_EncodeCharmap. When mapping is
// nil falls back to latin-1. When mapping is a Unicode string we use
// the inverse table built on the fly (codepoint -> byte index). When
// mapping is a Dict it is used directly: int(codepoint) -> int|bytes|None.
//
// CPython: Objects/unicodeobject.c:9114 _PyUnicode_EncodeCharmap
func charmapEncodeImpl(s, errors string, mapping objects.Object) ([]byte, int, error) {
	runes := []rune(s)
	if mapping == nil {
		// Default to Latin-1.
		out, _, err := codecs.Encode(s, "latin-1", errors)
		return out, len(runes), err
	}
	var inv map[rune]byte
	if u, ok := mapping.(*objects.Unicode); ok {
		inv = invertTable([]rune(u.Value()))
	}
	var out []byte
	i := 0
	for i < len(runes) {
		r := runes[i]
		mapped, ok, err := charmapEncodeLookup(r, mapping, inv)
		if err != nil {
			return nil, 0, err
		}
		if !ok {
			more, newpos, herr := charmapEncodeError(runes, i, mapping, inv, errors, s)
			if herr != nil {
				return nil, 0, herr
			}
			out = append(out, more...)
			i = newpos
			continue
		}
		out = append(out, mapped...)
		i++
	}
	return out, len(runes), nil
}

// charmapEncodeError mirrors CPython's charmap_encoding_error: collect
// the run of consecutive unencodable chars, dispatch on the named error
// handler, and return the bytes to append plus the input position to
// resume from.
//
// CPython: Objects/unicodeobject.c:8972 charmap_encoding_error
func charmapEncodeError(runes []rune, i int, mapping objects.Object, inv map[rune]byte, errors, source string) ([]byte, int, error) {
	start := i
	end := i + 1
	for end < len(runes) {
		_, ok, err := charmapEncodeLookup(runes[end], mapping, inv)
		if err != nil {
			return nil, 0, err
		}
		if ok {
			break
		}
		end++
	}
	switch errors {
	case "strict":
		return nil, 0, fmt.Errorf("UnicodeEncodeError: 'charmap' codec can't encode character %q in position %d: character maps to <undefined>", runes[start], start)
	case "ignore":
		return nil, end, nil
	case "replace":
		buf, _, err := encodeReplaceRun(end-start, '?', mapping, inv)
		return buf, end, err
	case "xmlcharrefreplace":
		buf, _, err := encodeXMLCharRefRun(runes[start:end], mapping, inv)
		return buf, end, err
	}
	rep, newpos, herr := callEncodeErrorHandler("charmap", "character maps to <undefined>", source, start, end, errors)
	if herr != nil {
		return nil, 0, herr
	}
	var out []byte
	for _, rr := range rep {
		m2, ok2, err := charmapEncodeLookup(rr, mapping, inv)
		if err != nil {
			return nil, 0, err
		}
		if !ok2 {
			return nil, 0, fmt.Errorf("UnicodeEncodeError: 'charmap' codec can't encode character %q in position %d: character maps to <undefined>", rr, start)
		}
		out = append(out, m2...)
	}
	return out, newpos, nil
}

func encodeReplaceRun(n int, ch rune, mapping objects.Object, inv map[rune]byte) ([]byte, int, error) {
	m2, ok, err := charmapEncodeLookup(ch, mapping, inv)
	if err != nil {
		return nil, 0, err
	}
	if !ok {
		return nil, 0, fmt.Errorf("UnicodeEncodeError: 'charmap' codec can't encode character %q (replacement)", ch)
	}
	var out []byte
	for j := 0; j < n; j++ {
		out = append(out, m2...)
	}
	return out, 0, nil // newpos handled by caller via len(out) is unused here; caller advances by end
}

func encodeXMLCharRefRun(runes []rune, mapping objects.Object, inv map[rune]byte) ([]byte, int, error) {
	var out []byte
	for _, r := range runes {
		for _, b := range []byte(fmt.Sprintf("&#%d;", r)) {
			m2, ok, err := charmapEncodeLookup(rune(b), mapping, inv)
			if err != nil {
				return nil, 0, err
			}
			if !ok {
				return nil, 0, fmt.Errorf("UnicodeEncodeError: 'charmap' codec can't encode character %q (xml escape)", rune(b))
			}
			out = append(out, m2...)
		}
	}
	return out, 0, nil
}

// charmapEncodeLookup resolves a single codepoint against the mapping.
// Returns (bytes, found, err).
func charmapEncodeLookup(r rune, mapping objects.Object, inv map[rune]byte) ([]byte, bool, error) {
	if inv != nil {
		b, ok := inv[r]
		return []byte{b}, ok, nil
	}
	d, ok := mapping.(*objects.Dict)
	if !ok {
		return nil, false, fmt.Errorf("TypeError: charmap_encode() mapping must be str or mapping, not %s", mapping.Type().Name)
	}
	item, lookupErr := d.GetItem(objects.NewInt(int64(r)))
	_ = lookupErr // key-not-found is "unmapped"; surface only the type-error paths below
	if item == nil || objects.IsNone(item) {
		return nil, false, nil
	}
	switch v := item.(type) {
	case *objects.Int:
		val, ok := v.Int64()
		if !ok || val < 0 || val > 255 {
			return nil, false, fmt.Errorf("TypeError: character mapping must be in range(256)")
		}
		return []byte{byte(val)}, true, nil
	case *objects.Bytes:
		return v.Bytes(), true, nil
	default:
		return nil, false, fmt.Errorf("TypeError: character mapping must return integer, bytes or None, not %s", item.Type().Name)
	}
}

// invertTable builds a reverse lookup (codepoint -> byte) from a 256
// char decoding table. 0xFFFE entries are skipped (unmapped).
func invertTable(table []rune) map[rune]byte {
	m := make(map[rune]byte, len(table))
	for i, r := range table {
		if i >= 256 {
			break
		}
		if r == 0xFFFE {
			continue
		}
		if _, dup := m[r]; dup {
			continue
		}
		m[r] = byte(i)
	}
	return m
}

// charmapBuildImpl mirrors PyUnicode_BuildEncodingMap's dict path:
// map codepoint -> byte index for each char in the input string. CPython
// also has a tighter trie format, but it is purely an internal storage
// optimisation; the dict path is functionally identical and is what
// CPython falls back to whenever the trie can't represent the table.
//
// CPython: Objects/unicodeobject.c:8694 PyUnicode_BuildEncodingMap
func charmapBuildImpl(s string) (objects.Object, error) {
	if s == "" {
		return nil, fmt.Errorf("TypeError: bad argument type for built-in operation")
	}
	d := objects.NewDict()
	i := 0
	for _, r := range s {
		if i >= 256 {
			break
		}
		if err := d.SetItem(objects.NewInt(int64(r)), objects.NewInt(int64(i))); err != nil {
			return nil, err
		}
		i++
	}
	return d, nil
}

// callDecodeErrorHandler invokes the named error handler for a decode
// error and returns (replacement, new_position).
func callDecodeErrorHandler(enc, reason string, input []byte, start, end int, errors string) (string, int, error) {
	h, err := codecs.LookupError(errors)
	if err != nil {
		return "", 0, err
	}
	return h(enc, reason, input, start, end)
}

// callEncodeErrorHandler invokes the named error handler for an encode
// error. The input is supplied as a string; we pass its UTF-8 bytes
// since the handler signature is byte-oriented.
func callEncodeErrorHandler(enc, reason, input string, start, end int, errors string) (string, int, error) {
	h, err := codecs.LookupError(errors)
	if err != nil {
		return "", 0, err
	}
	return h(enc, reason, []byte(input), start, end)
}

// ---------------------------------------------------------------------------
// register(search_function) — append search_function to the codec
// registry so codecs.Lookup will consult it.
//
// CPython: Modules/_codecsmodule.c:76 _codecs_register_impl (and
// Python/codecs.c:50 PyCodec_Register)
// ---------------------------------------------------------------------------

func codecsRegister(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: register() takes exactly 1 argument (%d given)", len(args))
	}
	search := args[0]
	if !objects.Callable(search) {
		return nil, fmt.Errorf("TypeError: argument must be callable")
	}
	codecs.Register(pythonSearchFunc(search))
	return objects.None(), nil
}

// pythonSearchFunc adapts a Python callable into a Go-side SearchFunc.
// The Python side returns either None / a CodecInfo / a 4-7-tuple of
// callables. We extract `encode` and `decode` and wire them into a
// codecs.CodecInfo so the Go-side lookup chain can serve the encoding.
//
// CPython: Python/codecs.c:99 _PyCodec_Lookup (calls each registered
// search function with the normalised encoding name)
func pythonSearchFunc(fn objects.Object) codecs.SearchFunc {
	return func(name string) (*codecs.CodecInfo, error) {
		ret, err := objects.Call(fn, objects.NewTuple([]objects.Object{objects.NewStr(name)}), nil)
		if err != nil {
			return nil, err
		}
		if ret == nil || ret == objects.None() {
			return nil, nil
		}
		return codecInfoFromPython(ret, name)
	}
}

// codecInfoFromPython unpacks the encode / decode callables from a
// Python CodecInfo (or 4-7-tuple) and wraps them in a Go CodecInfo.
//
// CPython: Lib/encodings/__init__.py:120 search_function (entry =
// getregentry(); coercion to codecs.CodecInfo)
func codecInfoFromPython(info objects.Object, name string) (*codecs.CodecInfo, error) {
	getAttr := func(key string) (objects.Object, error) {
		// CodecInfo from Lib/codecs.py: namedtuple-like instance with
		// attribute access on encode / decode. Our stdlib CodecInfo
		// implements __getattr__ via the standard generic getattr path.
		v, err := objects.GetAttr(info, objects.NewStr(key))
		if err != nil {
			return nil, err
		}
		return v, nil
	}
	enc, err := getAttr("encode")
	if err != nil {
		return nil, err
	}
	dec, err := getAttr("decode")
	if err != nil {
		return nil, err
	}
	ci := &codecs.CodecInfo{
		Name: name,
		Encode: func(input string, errors string) ([]byte, int, error) {
			ret, err := objects.Call(enc,
				objects.NewTuple([]objects.Object{objects.NewStr(input), objects.NewStr(errors)}), nil)
			if err != nil {
				return nil, 0, err
			}
			return unpackEncodeResult(ret)
		},
		Decode: func(input []byte, errors string) (string, int, error) {
			ret, err := objects.Call(dec,
				objects.NewTuple([]objects.Object{objects.NewBytes(input), objects.NewStr(errors)}), nil)
			if err != nil {
				return "", 0, err
			}
			return unpackDecodeResult(ret)
		},
	}
	return ci, nil
}

// unpackEncodeResult expects (bytes, n) and returns the two parts.
func unpackEncodeResult(o objects.Object) ([]byte, int, error) {
	t, ok := o.(*objects.Tuple)
	if !ok || t.Len() != 2 {
		return nil, 0, fmt.Errorf("TypeError: encoder must return a (bytes, int) tuple")
	}
	bobj, ok := t.Item(0).(*objects.Bytes)
	if !ok {
		return nil, 0, fmt.Errorf("TypeError: encoder must return bytes, got %s", t.Item(0).Type().Name)
	}
	nobj, ok := t.Item(1).(*objects.Int)
	if !ok {
		return nil, 0, fmt.Errorf("TypeError: encoder must return (bytes, int)")
	}
	n, _ := nobj.Int64()
	return bobj.Bytes(), int(n), nil
}

// unpackDecodeResult expects (str, n) and returns the two parts.
func unpackDecodeResult(o objects.Object) (string, int, error) {
	t, ok := o.(*objects.Tuple)
	if !ok || t.Len() != 2 {
		return "", 0, fmt.Errorf("TypeError: decoder must return a (str, int) tuple")
	}
	sobj, ok := t.Item(0).(*objects.Unicode)
	if !ok {
		return "", 0, fmt.Errorf("TypeError: decoder must return str, got %s", t.Item(0).Type().Name)
	}
	nobj, ok := t.Item(1).(*objects.Int)
	if !ok {
		return "", 0, fmt.Errorf("TypeError: decoder must return (str, int)")
	}
	n, _ := nobj.Int64()
	return sobj.Value(), int(n), nil
}

// ---------------------------------------------------------------------------
// register_error(name, handler) / lookup_error(name)
//
// CPython: Modules/_codecsmodule.c:489 _codecs_register_error_impl
// CPython: Modules/_codecsmodule.c:498 _codecs_lookup_error_impl
// ---------------------------------------------------------------------------

func codecsRegisterError(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: register_error() takes exactly 2 arguments (%d given)", len(args))
	}
	nameObj, ok := args[0].(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: register_error() argument 1 must be str, not %s", args[0].Type().Name)
	}
	handler := args[1]
	name := nameObj.Value()
	pyErrMu.Lock()
	pyErrHandlers[name] = handler
	pyErrMu.Unlock()
	// Bridge the Python callable into the Go error handler registry.
	codecs.RegisterError(name, func(enc, reason string, input []byte, start, end int) (string, int, error) {
		pyErrMu.RLock()
		h := pyErrHandlers[name]
		pyErrMu.RUnlock()
		if h == nil {
			return "", 0, fmt.Errorf("LookupError: unknown error handler name %q", name)
		}
		if reason == "" {
			reason = "codec error"
		}
		// Build a minimal UnicodeDecodeError-like tuple to pass to the handler.
		errArg := objects.NewTuple([]objects.Object{
			objects.NewStr(enc),
			objects.NewBytes(input),
			objects.NewInt(int64(start)),
			objects.NewInt(int64(end)),
			objects.NewStr(reason),
		})
		result, cerr := objects.Call(h, objects.NewTuple([]objects.Object{errArg}), nil)
		if cerr != nil {
			return "", 0, cerr
		}
		tup, ok := result.(*objects.Tuple)
		if !ok || tup.Len() != 2 {
			return "", 0, fmt.Errorf("TypeError: error handler must return a (str, int) tuple")
		}
		rep, ok := tup.Item(0).(*objects.Unicode)
		if !ok {
			return "", 0, fmt.Errorf("TypeError: error handler replacement must be str")
		}
		pos, ok := tup.Item(1).(*objects.Int)
		if !ok {
			return "", 0, fmt.Errorf("TypeError: error handler new position must be int")
		}
		newpos, _ := pos.Int64()
		return rep.Value(), int(newpos), nil
	})
	return objects.None(), nil
}

func codecsLookupError(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: lookup_error() takes exactly 1 argument (%d given)", len(args))
	}
	nameObj, ok := args[0].(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: lookup_error() argument must be str, not %s", args[0].Type().Name)
	}
	name := nameObj.Value()
	pyErrMu.RLock()
	h, found := pyErrHandlers[name]
	pyErrMu.RUnlock()
	if found {
		return h, nil
	}
	// Check if it is a built-in Go-level handler.
	_, err := codecs.LookupError(name)
	if err != nil {
		return nil, fmt.Errorf("LookupError: unknown error handler name %q", name)
	}
	// Return a wrapped callable for the built-in handler.
	return objects.NewBuiltinFunction(name, func(args2 []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return nil, fmt.Errorf("LookupError: built-in error handler %q is not directly callable from Python", name)
	}), nil
}

// ---------------------------------------------------------------------------
// Helpers shared by the per-codec functions.
// ---------------------------------------------------------------------------

// encodeArgs pulls (str_or_bytes, errors='strict') from args.
func encodeArgs(args []objects.Object, fname string) (string, string, error) {
	if len(args) < 1 || len(args) > 2 {
		return "", "", fmt.Errorf("TypeError: %s() takes 1 to 2 arguments (%d given)", fname, len(args))
	}
	str, ok := args[0].(*objects.Unicode)
	if !ok {
		return "", "", fmt.Errorf("TypeError: %s() argument 1 must be str, not %s", fname, args[0].Type().Name)
	}
	errors := "strict"
	if len(args) == 2 {
		e, ok := args[1].(*objects.Unicode)
		if !ok {
			return "", "", fmt.Errorf("TypeError: %s() argument 2 must be str, not %s", fname, args[1].Type().Name)
		}
		errors = e.Value()
	}
	return str.Value(), errors, nil
}

// decodeArgs pulls (bytes_obj, errors='strict'[, final=False]) from args.
func decodeArgs(args []objects.Object, fname string) ([]byte, string, error) {
	if len(args) < 1 || len(args) > 3 {
		return nil, "", fmt.Errorf("TypeError: %s() takes 1 to 3 arguments (%d given)", fname, len(args))
	}
	b, err := toBytes(args[0], fname, 1)
	if err != nil {
		return nil, "", err
	}
	errors := "strict"
	if len(args) >= 2 {
		e, ok := args[1].(*objects.Unicode)
		if !ok {
			return nil, "", fmt.Errorf("TypeError: %s() argument 2 must be str, not %s", fname, args[1].Type().Name)
		}
		errors = e.Value()
	}
	return b, errors, nil
}

// toBytes coerces obj to a []byte, accepting bytes and bytearray.
func toBytes(obj objects.Object, fname string, pos int) ([]byte, error) {
	switch v := obj.(type) {
	case *objects.Bytes:
		return v.Bytes(), nil
	case *objects.ByteArray:
		return v.Bytes(), nil
	default:
		return nil, fmt.Errorf("TypeError: %s() argument %d must be bytes or bytearray, not %s", fname, pos, obj.Type().Name)
	}
}
