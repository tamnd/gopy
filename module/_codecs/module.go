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
	"reflect"
	"sync"
	"unicode/utf8"

	"github.com/tamnd/gopy/codecs"
	pyerrors "github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/imp"
	mbcodec "github.com/tamnd/gopy/module/_multibytecodec"
	pywarn "github.com/tamnd/gopy/module/_warnings"
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
		{"utf_7_encode", utf7Encode},
		{"utf_7_decode", utf7Decode},
		{"utf_16_encode", utf16Encode},
		{"utf_16_decode", utf16Decode},
		{"utf_16_le_encode", utf16LEEncode},
		{"utf_16_le_decode", utf16LEDecode},
		{"utf_16_be_encode", utf16BEEncode},
		{"utf_16_be_decode", utf16BEDecode},
		{"utf_16_ex_decode", utf16ExDecode},
		{"utf_32_encode", utf32Encode},
		{"utf_32_decode", utf32Decode},
		{"utf_32_le_encode", utf32LEEncode},
		{"utf_32_le_decode", utf32LEDecode},
		{"utf_32_be_encode", utf32BEEncode},
		{"utf_32_be_decode", utf32BEDecode},
		{"utf_32_ex_decode", utf32ExDecode},
		{"ascii_encode", asciiEncode},
		{"ascii_decode", asciiDecode},
		{"latin_1_encode", latin1Encode},
		{"latin_1_decode", latin1Decode},
		{"unicode_escape_encode", unicodeEscapeEncode},
		{"unicode_escape_decode", unicodeEscapeDecode},
		{"raw_unicode_escape_encode", rawUnicodeEscapeEncode},
		{"raw_unicode_escape_decode", rawUnicodeEscapeDecode},
		{"escape_encode", escapeEncode},
		{"escape_decode", escapeDecode},
		{"readbuffer_encode", readbufferEncode},
		{"charmap_encode", charmapEncode},
		{"charmap_decode", charmapDecode},
		{"charmap_build", charmapBuild},
		{"register", codecsRegister},
		{"unregister", codecsUnregister},
		{"register_error", codecsRegisterError},
		{"lookup_error", codecsLookupError},
		{"_unregister_error", codecsUnregisterError},
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

// pyCodecEncodeMu guards pyCodecEncodeMap and pyCodecDecodeMap.
var pyCodecEncodeMu sync.RWMutex

// pyCodecEncodeMap caches the raw Python encode callable for each codec name,
// populated by codecInfoFromPython. Used by codecsEncode for non-str objects
// (transform/binary codecs) so we bypass the Go text-codec wrappers that
// require str input.
// CPython: Modules/_codecsmodule.c:127 _codecs_encode_impl accepts PyObject*
var pyCodecEncodeMap = map[string]objects.Object{}

// pyCodecDecodeMap caches the raw Python decode callable for each codec name.
// Used by codecsDecode for binary transform codecs (base64, hex, etc.) whose
// decoder returns (bytes, int) instead of (str, int).
// CPython: Modules/_codecsmodule.c:165 _codecs_decode_impl accepts PyObject*
var pyCodecDecodeMap = map[string]objects.Object{}

// pyCodecIncrementalEncoderMap caches the Python IncrementalEncoder class for
// each Python-registered codec. Used so text-transform codecs (rot-13) and
// binary transforms get their own Python IncrementalEncoder instead of our
// Go MultibyteIncrementalEncoder wrapper.
var pyCodecIncrementalEncoderMap = map[string]objects.Object{}

// pyCodecIncrementalDecoderMap caches the Python IncrementalDecoder class.
var pyCodecIncrementalDecoderMap = map[string]objects.Object{}

// pyCodecStreamReaderMap caches the Python StreamReader class.
var pyCodecStreamReaderMap = map[string]objects.Object{}

// pyCodecStreamWriterMap caches the Python StreamWriter class.
var pyCodecStreamWriterMap = map[string]objects.Object{}

// pyCodecObjectMap caches the original Python CodecInfo object (or raw tuple)
// returned by a search function, keyed by normalized codec name. Returned
// verbatim by codecsLookup so that CodecInfo equality vs plain tuples works.
// CPython: Python/codecs.c:99 _PyCodec_Lookup caches and returns the raw
// object the search function produced.
var pyCodecObjectMap = map[string]objects.Object{}

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
	// CodecInfo is iterable as (encode, decode, streamreader, streamwriter)
	// mirroring CPython's tuple-subtype behavior.
	// CPython: Python/codecs.c codec_info_new (tuple subtype)
	codecInfoType.Iter = func(o objects.Object) (objects.Object, error) {
		inst, ok := o.(*objects.Instance)
		if !ok {
			return nil, fmt.Errorf("TypeError: expected CodecInfo")
		}
		d := inst.EnsureDict()
		getItem := func(key string) objects.Object {
			v, _ := d.GetItem(objects.NewStr(key))
			if v == nil {
				return objects.None()
			}
			return v
		}
		items := objects.NewList([]objects.Object{
			getItem("encode"),
			getItem("decode"),
			getItem("streamreader"),
			getItem("streamwriter"),
		})
		return objects.Iter(items)
	}
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
	// Return the original Python CodecInfo when available so that equality
	// comparisons with the original search-function result work correctly.
	// CPython: Python/codecs.c:99 _PyCodec_Lookup returns the raw object.
	normName := codecs.NormalizeName(name.Value())
	pyCodecEncodeMu.RLock()
	pyObj := pyCodecObjectMap[normName]
	pyCodecEncodeMu.RUnlock()
	if pyObj != nil {
		return pyObj, nil
	}
	return makeCodecInfo(ci), nil
}

// makeCodecInfo wraps a Go CodecInfo in a Python Instance whose dict
// exposes encoder/decoder callables, mirroring the CodecInfo namedtuple.
//
// CPython: Modules/_codecsmodule.c:34 codec_info_new
func makeCodecInfo(ci *codecs.CodecInfo) objects.Object {
	inst := objects.NewInstance(codecInfoType)

	encodeFn := objects.NewBuiltinFunction("encode", func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
		if len(args) == 0 {
			return nil, fmt.Errorf("TypeError: encode() takes 1 to 2 arguments (0 given)")
		}
		errors := "strict"
		if len(args) >= 2 {
			if e, ok := args[1].(*objects.Unicode); ok {
				errors = e.Value()
			}
		}
		// Use the Python callable directly when available. This handles both
		// binary-transform codecs (bytes→bytes) and text-transform codecs
		// (str→str, e.g. rot-13) without imposing a bytes-return requirement.
		// CPython: Modules/_codecsmodule.c:127 _codecs_encode_impl accepts any PyObject*
		pyCodecEncodeMu.RLock()
		pyFn := pyCodecEncodeMap[ci.Name]
		pyCodecEncodeMu.RUnlock()
		if pyFn != nil {
			result, rerr := objects.Call(pyFn, objects.NewTuple([]objects.Object{args[0], objects.NewStr(errors)}), nil)
			if rerr != nil {
				return nil, rerr
			}
			return result, nil
		}
		s, errorsStr, err := encodeArgs(args, "encode")
		if err != nil {
			return nil, err
		}
		out, n, err := ci.Encode(s, errorsStr)
		if err != nil {
			return nil, err
		}
		return objects.NewTuple([]objects.Object{
			objects.NewBytes(out),
			objects.NewInt(int64(n)),
		}), nil
	})

	decodeFn := objects.NewBuiltinFunction("decode", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		// Use the Python callable directly when available so binary-transform
		// codecs (base64, hex, zlib, uu, quopri) can return bytes without
		// failing the str-return check.
		// CPython: Modules/_codecsmodule.c:165 _codecs_decode_impl
		pyCodecEncodeMu.RLock()
		pyFn := pyCodecDecodeMap[ci.Name]
		pyCodecEncodeMu.RUnlock()
		if pyFn != nil {
			if len(args) == 0 {
				return nil, fmt.Errorf("TypeError: decode() takes 1 to 2 arguments (0 given)")
			}
			errors := "strict"
			if len(args) >= 2 {
				if e, ok := args[1].(*objects.Unicode); ok {
					errors = e.Value()
				}
			}
			result, rerr := objects.Call(pyFn, objects.NewTuple([]objects.Object{args[0], objects.NewStr(errors)}), nil)
			if rerr != nil {
				return nil, rerr
			}
			return result, nil
		}
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

	// Use the Python IncrementalEncoder/Decoder/StreamReader/StreamWriter classes
	// when available (Python-registered codecs like rot-13, base64, etc.).
	// Fall back to our Go MultibyteIncrementalEncoder for built-in Go codecs.
	pyCodecEncodeMu.RLock()
	pyIE := pyCodecIncrementalEncoderMap[ci.Name]
	pyID := pyCodecIncrementalDecoderMap[ci.Name]
	pySR := pyCodecStreamReaderMap[ci.Name]
	pySW := pyCodecStreamWriterMap[ci.Name]
	pyCodecEncodeMu.RUnlock()

	var ieObj, idObj, srObj, swObj objects.Object
	if pyIE != nil {
		ieObj = pyIE
	} else {
		ieObj = mbcodec.MakeIncrementalEncoderClass(ci)
	}
	if pyID != nil {
		idObj = pyID
	} else {
		idObj = mbcodec.MakeIncrementalDecoderClass(ci)
	}
	if pySR != nil {
		srObj = pySR
	} else {
		srObj = mbcodec.MakeStreamReaderClass(ci)
	}
	if pySW != nil {
		swObj = pySW
	} else {
		swObj = mbcodec.MakeStreamWriterClass(ci)
	}

	d := inst.EnsureDict()
	_ = d.SetItem(objects.NewStr("name"), objects.NewStr(ci.Name))
	_ = d.SetItem(objects.NewStr("encode"), encodeFn)
	_ = d.SetItem(objects.NewStr("decode"), decodeFn)
	_ = d.SetItem(objects.NewStr("incrementalencoder"), ieObj)
	_ = d.SetItem(objects.NewStr("incrementaldecoder"), idObj)
	_ = d.SetItem(objects.NewStr("streamreader"), srObj)
	_ = d.SetItem(objects.NewStr("streamwriter"), swObj)
	return inst
}

// ---------------------------------------------------------------------------
// encode(obj, encoding='utf-8', errors='strict') -> bytes
//
// CPython: Modules/_codecsmodule.c:127 _codecs_encode_impl
// ---------------------------------------------------------------------------

func codecsEncode(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	// Merge kwargs into positional: obj=, encoding=, errors=
	// CPython: Modules/_codecsmodule.c:127 _codecs_encode_impl
	if kv, ok := kwargs["obj"]; ok && len(args) == 0 {
		args = append(args, kv)
	}
	if kv, ok := kwargs["encoding"]; ok && len(args) <= 1 {
		for len(args) < 1 {
			args = append(args, nil)
		}
		args = append(args, kv)
	}
	if kv, ok := kwargs["errors"]; ok && len(args) <= 2 {
		for len(args) < 2 {
			args = append(args, nil)
		}
		args = append(args, kv)
	}
	if len(args) < 1 || args[0] == nil {
		return nil, fmt.Errorf("TypeError: encode() takes 1 to 3 arguments (%d given)", len(args))
	}
	if len(args) > 3 {
		return nil, fmt.Errorf("TypeError: encode() takes 1 to 3 arguments (%d given)", len(args))
	}
	encoding := "utf-8"
	if len(args) >= 2 && args[1] != nil {
		e, ok := args[1].(*objects.Unicode)
		if !ok {
			return nil, fmt.Errorf("TypeError: encode() argument 2 must be str, not %s", args[1].Type().Name)
		}
		encoding = e.Value()
	}
	errors := "strict"
	if len(args) >= 3 && args[2] != nil {
		e, ok := args[2].(*objects.Unicode)
		if !ok {
			return nil, fmt.Errorf("TypeError: encode() argument 3 must be str, not %s", args[2].Type().Name)
		}
		errors = e.Value()
	}
	// Always try the Python callable first so str→str codecs (rot-13) and
	// bytes→bytes codecs work without the bytes-return constraint imposed by
	// the Go CodecInfo wrapper. Built-in Go codecs (utf-8, ascii, latin-1)
	// are not in pyCodecEncodeMap and fall through to codecs.Encode.
	// CPython: Modules/_codecsmodule.c:127 _codecs_encode_impl accepts any PyObject*
	normName := codecs.NormalizeName(encoding)
	pyCodecEncodeMu.RLock()
	encFn := pyCodecEncodeMap[normName]
	pyCodecEncodeMu.RUnlock()
	if encFn == nil {
		if _, err := codecs.Lookup(encoding); err != nil {
			return nil, err
		}
		pyCodecEncodeMu.RLock()
		encFn = pyCodecEncodeMap[normName]
		pyCodecEncodeMu.RUnlock()
	}
	if encFn != nil {
		result, rerr := objects.Call(encFn, objects.NewTuple([]objects.Object{args[0], objects.NewStr(errors)}), nil)
		if rerr != nil {
			if objects.FormatNoteHook != nil {
				objects.FormatNoteHook(fmt.Sprintf("encoding with '%s' codec failed", encoding))
			}
			return nil, rerr
		}
		// codec.encode returns (encoded, length); return the encoded object.
		if tup, ok := result.(*objects.Tuple); ok && tup.Len() >= 1 {
			return tup.Item(0), nil
		}
		return result, nil
	}
	// Built-in Go codec (utf-8, ascii, latin-1, etc.): str input only.
	str, ok := args[0].(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: encoder requires str input, not %s", args[0].Type().Name)
	}
	out, _, err := codecs.Encode(str.Value(), encoding, errors)
	if err != nil {
		if objects.FormatNoteHook != nil {
			objects.FormatNoteHook(fmt.Sprintf("encoding with '%s' codec failed", encoding))
		}
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
	// Merge kwargs into positional: obj=, encoding=, errors=
	// CPython: Modules/_codecsmodule.c:165 _codecs_decode_impl
	if kv, ok := kwargs["obj"]; ok && len(args) == 0 {
		args = append(args, kv)
	}
	if kv, ok := kwargs["encoding"]; ok && len(args) <= 1 {
		for len(args) < 1 {
			args = append(args, nil)
		}
		args = append(args, kv)
	}
	if kv, ok := kwargs["errors"]; ok && len(args) <= 2 {
		for len(args) < 2 {
			args = append(args, nil)
		}
		args = append(args, kv)
	}
	if len(args) < 1 || args[0] == nil {
		return nil, fmt.Errorf("TypeError: decode() takes 1 to 3 arguments (%d given)", len(args))
	}
	if len(args) > 3 {
		return nil, fmt.Errorf("TypeError: decode() takes 1 to 3 arguments (%d given)", len(args))
	}
	encoding := "utf-8"
	if len(args) >= 2 && args[1] != nil {
		e, ok := args[1].(*objects.Unicode)
		if !ok {
			return nil, fmt.Errorf("TypeError: decode() argument 2 must be str, not %s", args[1].Type().Name)
		}
		encoding = e.Value()
	}
	errors := "strict"
	if len(args) >= 3 && args[2] != nil {
		e, ok := args[2].(*objects.Unicode)
		if !ok {
			return nil, fmt.Errorf("TypeError: decode() argument 3 must be str, not %s", args[2].Type().Name)
		}
		errors = e.Value()
	}
	// Try the Python callable first — text-transform codecs (rot-13) accept str,
	// binary transforms (base64, hex) return bytes rather than str. Pass the raw
	// input object so the callable receives whatever type it expects.
	// CPython: Modules/_codecsmodule.c:165 _codecs_decode_impl accepts any PyObject*
	normName := codecs.NormalizeName(encoding)
	pyCodecEncodeMu.RLock()
	decFn := pyCodecDecodeMap[normName]
	pyCodecEncodeMu.RUnlock()
	if decFn == nil {
		if _, lerr := codecs.Lookup(encoding); lerr == nil {
			pyCodecEncodeMu.RLock()
			decFn = pyCodecDecodeMap[normName]
			pyCodecEncodeMu.RUnlock()
		}
	}
	if decFn != nil {
		result, rerr := objects.Call(decFn, objects.NewTuple([]objects.Object{args[0], objects.NewStr(errors)}), nil)
		if rerr != nil {
			if objects.FormatNoteHook != nil {
				objects.FormatNoteHook(fmt.Sprintf("decoding with '%s' codec failed", encoding))
			}
			return nil, rerr
		}
		if tup, ok := result.(*objects.Tuple); ok && tup.Len() >= 1 {
			return tup.Item(0), nil
		}
		return result, nil
	}
	// Built-in Go codec: requires bytes input.
	b, err := toBytes(args[0], "decode", 1)
	if err != nil {
		return nil, err
	}
	out, _, derr := codecs.Decode(b, encoding, errors)
	if derr != nil {
		if objects.FormatNoteHook != nil {
			objects.FormatNoteHook(fmt.Sprintf("decoding with '%s' codec failed", encoding))
		}
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
	// Handle optional final parameter (default False).
	// CPython: Modules/_codecsmodule.c:244 _codecs_utf_8_decode_impl
	final := false
	if len(args) >= 3 {
		final = objects.IsTrue(args[2])
	}
	if !final {
		out, remaining, err2 := codecs.DecodeUTF8Incremental(b, errors, false)
		if err2 != nil {
			return nil, err2
		}
		n := len(b) - len(remaining)
		return objects.NewTuple([]objects.Object{objects.NewStr(out), objects.NewInt(int64(n))}), nil
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
// UTF-7 encode/decode
//
// CPython: Modules/_codecsmodule.c:146 _codecs_utf_7_encode_impl
// CPython: Modules/_codecsmodule.c:164 _codecs_utf_7_decode_impl
// ---------------------------------------------------------------------------

func utf7Encode(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	s, errors, err := encodeArgs(args, "utf_7_encode")
	if err != nil {
		return nil, err
	}
	out, n, err := codecs.Encode(s, "utf-7", errors)
	if err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{objects.NewBytes(out), objects.NewInt(int64(n))}), nil
}

func utf7Decode(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	b, errors, err := decodeArgs(args, "utf_7_decode")
	if err != nil {
		return nil, err
	}
	out, n, err := codecs.Decode(b, "utf-7", errors)
	if err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{objects.NewStr(out), objects.NewInt(int64(n))}), nil
}

// ---------------------------------------------------------------------------
// UTF-16 encode/decode variants
//
// CPython: Modules/_codecsmodule.c:181 _codecs_utf_16_encode_impl
// CPython: Modules/_codecsmodule.c:201 _codecs_utf_16_decode_impl
// CPython: Modules/_codecsmodule.c:225 _codecs_utf_16_le_encode_impl
// CPython: Modules/_codecsmodule.c:243 _codecs_utf_16_le_decode_impl
// CPython: Modules/_codecsmodule.c:263 _codecs_utf_16_be_encode_impl
// CPython: Modules/_codecsmodule.c:281 _codecs_utf_16_be_decode_impl
// CPython: Modules/_codecsmodule.c:301 _codecs_utf_16_ex_decode_impl
// ---------------------------------------------------------------------------

func utf16Encode(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	s, errors, err := encodeArgs(args, "utf_16_encode")
	if err != nil {
		return nil, err
	}
	out, n, err := codecs.Encode(s, "utf-16", errors)
	if err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{objects.NewBytes(out), objects.NewInt(int64(n))}), nil
}

func utf16Decode(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	b, errors, err := decodeArgs(args, "utf_16_decode")
	if err != nil {
		return nil, err
	}
	out, n, err := codecs.Decode(b, "utf-16", errors)
	if err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{objects.NewStr(out), objects.NewInt(int64(n))}), nil
}

func utf16LEEncode(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	s, errors, err := encodeArgs(args, "utf_16_le_encode")
	if err != nil {
		return nil, err
	}
	out, n, err := codecs.Encode(s, "utf-16-le", errors)
	if err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{objects.NewBytes(out), objects.NewInt(int64(n))}), nil
}

func utf16LEDecode(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	b, errors, err := decodeArgs(args, "utf_16_le_decode")
	if err != nil {
		return nil, err
	}
	out, n, err := codecs.Decode(b, "utf-16-le", errors)
	if err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{objects.NewStr(out), objects.NewInt(int64(n))}), nil
}

func utf16BEEncode(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	s, errors, err := encodeArgs(args, "utf_16_be_encode")
	if err != nil {
		return nil, err
	}
	out, n, err := codecs.Encode(s, "utf-16-be", errors)
	if err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{objects.NewBytes(out), objects.NewInt(int64(n))}), nil
}

func utf16BEDecode(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	b, errors, err := decodeArgs(args, "utf_16_be_decode")
	if err != nil {
		return nil, err
	}
	out, n, err := codecs.Decode(b, "utf-16-be", errors)
	if err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{objects.NewStr(out), objects.NewInt(int64(n))}), nil
}

// utf16ExDecode decodes UTF-16 bytes and also returns the detected byte order.
// byteorder arg: 0=BOM, -1=little, 1=big. Returns (str, length, byteorder).
//
// CPython: Modules/_codecsmodule.c:301 _codecs_utf_16_ex_decode_impl
func utf16ExDecode(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	b, errors, err := decodeArgs(args, "utf_16_ex_decode")
	if err != nil {
		return nil, err
	}
	byteorder := 0
	if len(args) >= 3 {
		if bo, ok := args[2].(*objects.Int); ok {
			v, _ := bo.Int64()
			byteorder = int(v)
		}
	}
	var enc string
	detectedOrder := byteorder
	switch byteorder {
	case -1:
		enc = "utf-16-le"
	case 1:
		enc = "utf-16-be"
	default:
		if len(b) >= 2 {
			if b[0] == 0xFF && b[1] == 0xFE {
				enc = "utf-16-le"
				detectedOrder = -1
				b = b[2:]
			} else if b[0] == 0xFE && b[1] == 0xFF {
				enc = "utf-16-be"
				detectedOrder = 1
				b = b[2:]
			} else {
				enc = "utf-16-le"
				detectedOrder = -1
			}
		} else {
			enc = "utf-16-le"
			detectedOrder = -1
		}
	}
	out, n, err := codecs.Decode(b, enc, errors)
	if err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{
		objects.NewStr(out),
		objects.NewInt(int64(n)),
		objects.NewInt(int64(detectedOrder)),
	}), nil
}

// ---------------------------------------------------------------------------
// UTF-32 encode/decode variants
//
// CPython: Modules/_codecsmodule.c:351 _codecs_utf_32_encode_impl
// CPython: Modules/_codecsmodule.c:371 _codecs_utf_32_decode_impl
// CPython: Modules/_codecsmodule.c:393 _codecs_utf_32_le_encode_impl
// CPython: Modules/_codecsmodule.c:411 _codecs_utf_32_le_decode_impl
// CPython: Modules/_codecsmodule.c:431 _codecs_utf_32_be_encode_impl
// CPython: Modules/_codecsmodule.c:449 _codecs_utf_32_be_decode_impl
// CPython: Modules/_codecsmodule.c:469 _codecs_utf_32_ex_decode_impl
// ---------------------------------------------------------------------------

func utf32Encode(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	s, errors, err := encodeArgs(args, "utf_32_encode")
	if err != nil {
		return nil, err
	}
	out, n, err := codecs.Encode(s, "utf-32", errors)
	if err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{objects.NewBytes(out), objects.NewInt(int64(n))}), nil
}

func utf32Decode(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	b, errors, err := decodeArgs(args, "utf_32_decode")
	if err != nil {
		return nil, err
	}
	out, n, err := codecs.Decode(b, "utf-32", errors)
	if err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{objects.NewStr(out), objects.NewInt(int64(n))}), nil
}

func utf32LEEncode(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	s, errors, err := encodeArgs(args, "utf_32_le_encode")
	if err != nil {
		return nil, err
	}
	out, n, err := codecs.Encode(s, "utf-32-le", errors)
	if err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{objects.NewBytes(out), objects.NewInt(int64(n))}), nil
}

func utf32LEDecode(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	b, errors, err := decodeArgs(args, "utf_32_le_decode")
	if err != nil {
		return nil, err
	}
	out, n, err := codecs.Decode(b, "utf-32-le", errors)
	if err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{objects.NewStr(out), objects.NewInt(int64(n))}), nil
}

func utf32BEEncode(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	s, errors, err := encodeArgs(args, "utf_32_be_encode")
	if err != nil {
		return nil, err
	}
	out, n, err := codecs.Encode(s, "utf-32-be", errors)
	if err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{objects.NewBytes(out), objects.NewInt(int64(n))}), nil
}

func utf32BEDecode(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	b, errors, err := decodeArgs(args, "utf_32_be_decode")
	if err != nil {
		return nil, err
	}
	out, n, err := codecs.Decode(b, "utf-32-be", errors)
	if err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{objects.NewStr(out), objects.NewInt(int64(n))}), nil
}

// utf32ExDecode decodes UTF-32 and also returns the detected byte order.
// Returns (str, length, byteorder).
//
// CPython: Modules/_codecsmodule.c:469 _codecs_utf_32_ex_decode_impl
func utf32ExDecode(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	b, errors, err := decodeArgs(args, "utf_32_ex_decode")
	if err != nil {
		return nil, err
	}
	byteorder := 0
	if len(args) >= 3 {
		if bo, ok := args[2].(*objects.Int); ok {
			v, _ := bo.Int64()
			byteorder = int(v)
		}
	}
	var enc string
	detectedOrder := byteorder
	switch byteorder {
	case -1:
		enc = "utf-32-le"
	case 1:
		enc = "utf-32-be"
	default:
		if len(b) >= 4 {
			if b[0] == 0xFF && b[1] == 0xFE && b[2] == 0x00 && b[3] == 0x00 {
				enc = "utf-32-le"
				detectedOrder = -1
				b = b[4:]
			} else if b[0] == 0x00 && b[1] == 0x00 && b[2] == 0xFE && b[3] == 0xFF {
				enc = "utf-32-be"
				detectedOrder = 1
				b = b[4:]
			} else {
				enc = "utf-32-le"
				detectedOrder = -1
			}
		} else {
			enc = "utf-32-le"
			detectedOrder = -1
		}
	}
	out, n, err := codecs.Decode(b, enc, errors)
	if err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{
		objects.NewStr(out),
		objects.NewInt(int64(n)),
		objects.NewInt(int64(detectedOrder)),
	}), nil
}

// ---------------------------------------------------------------------------
// unicode_escape_encode / unicode_escape_decode
//
// CPython: Modules/_codecsmodule.c:543 _codecs_unicode_escape_encode_impl
// CPython: Modules/_codecsmodule.c:557 _codecs_unicode_escape_decode_impl
// ---------------------------------------------------------------------------

func unicodeEscapeEncode(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	s, errors, err := encodeArgs(args, "unicode_escape_encode")
	if err != nil {
		return nil, err
	}
	out, n, err := codecs.Encode(s, "unicode-escape", errors)
	if err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{objects.NewBytes(out), objects.NewInt(int64(n))}), nil
}

func unicodeEscapeDecode(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 || len(args) > 3 {
		return nil, fmt.Errorf("TypeError: unicode_escape_decode() takes 1 to 3 arguments (%d given)", len(args))
	}
	b, err := toBytesBuffer(args[0], "unicode_escape_decode", 1)
	if err != nil {
		return nil, err
	}
	errors := "strict"
	if len(args) >= 2 {
		if e, ok := args[1].(*objects.Unicode); ok {
			errors = e.Value()
		}
	}
	out, n, err := codecs.Decode(b, "unicode-escape", errors)
	if err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{objects.NewStr(out), objects.NewInt(int64(n))}), nil
}

// ---------------------------------------------------------------------------
// raw_unicode_escape_encode / raw_unicode_escape_decode
//
// CPython: Modules/_codecsmodule.c:571 _codecs_raw_unicode_escape_encode_impl
// CPython: Modules/_codecsmodule.c:585 _codecs_raw_unicode_escape_decode_impl
// ---------------------------------------------------------------------------

func rawUnicodeEscapeEncode(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	s, errors, err := encodeArgs(args, "raw_unicode_escape_encode")
	if err != nil {
		return nil, err
	}
	out, n, err := codecs.Encode(s, "raw-unicode-escape", errors)
	if err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{objects.NewBytes(out), objects.NewInt(int64(n))}), nil
}

func rawUnicodeEscapeDecode(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	b, errors, err := decodeArgs(args, "raw_unicode_escape_decode")
	if err != nil {
		return nil, err
	}
	out, n, err := codecs.Decode(b, "raw-unicode-escape", errors)
	if err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{objects.NewStr(out), objects.NewInt(int64(n))}), nil
}

// ---------------------------------------------------------------------------
// escape_encode(data, errors='strict') -> (bytes, length)
// escape_decode(data, errors='strict') -> (bytes, length)
//
// Operates on bytes objects. escape_encode converts each byte to its C-style
// escape sequence. escape_decode converts C-style escape sequences back to
// bytes.
//
// CPython: Modules/_codecsmodule.c:599 _codecs_escape_encode_impl
// CPython: Modules/_codecsmodule.c:627 _codecs_escape_decode_impl
// ---------------------------------------------------------------------------

func escapeEncode(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("TypeError: escape_encode() takes 1 to 2 arguments (%d given)", len(args))
	}
	bv, ok := args[0].(*objects.Bytes)
	if !ok {
		return nil, fmt.Errorf("TypeError: escape_encode() argument 1 must be bytes, not %s", args[0].Type().Name)
	}
	b := bv.Bytes()
	var out []byte
	for _, c := range b {
		switch c {
		case '\n':
			out = append(out, '\\', 'n')
		case '\r':
			out = append(out, '\\', 'r')
		case '\t':
			out = append(out, '\\', 't')
		case '\\':
			out = append(out, '\\', '\\')
		case '\'':
			out = append(out, '\\', '\'')
		default:
			if c < 0x20 || c >= 0x7f {
				out = fmt.Appendf(out, "\\x%02x", c)
			} else {
				out = append(out, c)
			}
		}
	}
	return objects.NewTuple([]objects.Object{objects.NewBytes(out), objects.NewInt(int64(len(b)))}), nil
}

func escapeDecode(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("TypeError: escape_decode() takes 1 to 2 arguments (%d given)", len(args))
	}
	var b []byte
	switch v := args[0].(type) {
	case *objects.Bytes:
		b = v.Bytes()
	case *objects.ByteArray:
		b = v.Bytes()
	case *objects.Unicode:
		b = []byte(v.Value())
	default:
		return nil, fmt.Errorf("TypeError: escape_decode() argument 1 must be bytes or str, not %s", args[0].Type().Name)
	}
	errors := "strict"
	if len(args) >= 2 {
		if s, ok := args[1].(*objects.Unicode); ok {
			errors = s.Value()
		}
	}
	out, err := decodeEscapeBytes(b, errors)
	if err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{objects.NewBytes(out), objects.NewInt(int64(len(b)))}), nil
}

// decodeEscapeBytes mirrors PyBytes_DecodeEscape: processes \xNN, \n, \r, \\
// etc. in a bytes/str literal body and returns the raw byte slice.
//
// CPython: Objects/bytesobject.c:123 PyBytes_DecodeEscape
func decodeEscapeBytes(b []byte, errmode string) ([]byte, error) {
	out := make([]byte, 0, len(b))
	i := 0
	for i < len(b) {
		c := b[i]
		if c != '\\' {
			out = append(out, c)
			i++
			continue
		}
		startPos := i
		i++
		if i >= len(b) {
			return nil, fmt.Errorf("ValueError: Trailing backslash in string")
		}
		esc := b[i]
		i++
		switch esc {
		case '\n':
			// \<newline> is a line continuation: consume both, output nothing.
			// CPython: Objects/bytesobject.c:130 PyBytes_DecodeEscape
		case '\\':
			out = append(out, '\\')
		case '\'':
			out = append(out, '\'')
		case '"':
			out = append(out, '"')
		case 'b':
			out = append(out, '\b')
		case 'f':
			out = append(out, '\f')
		case 't':
			out = append(out, '\t')
		case 'n':
			out = append(out, '\n')
		case 'r':
			out = append(out, '\r')
		case 'v':
			out = append(out, '\v')
		case 'a':
			out = append(out, '\a')
		case '0', '1', '2', '3', '4', '5', '6', '7':
			val := int(esc - '0')
			octStart := int(esc - '0')
			_ = octStart
			digits := 1
			for j := 0; j < 2 && i < len(b) && b[i] >= '0' && b[i] <= '7'; j++ {
				val = val*8 + int(b[i]-'0')
				digits++
				i++
			}
			// Octal escapes >= 0o400 are deprecated in CPython 3.12+
			if val >= 0o400 {
				msg := fmt.Sprintf("b\"\\%o\" is an invalid octal escape sequence. Such sequences will not work in the future. ", val)
				_ = pywarn.WarnUnicode(pyerrors.PyExc_DeprecationWarning, msg, 2, nil)
				_ = digits
			}
			out = append(out, byte(val))
		case 'x':
			// CPython: Objects/bytesobject.c:1145 _PyBytes_DecodeEscape2 case 'x'
			if i+1 < len(b) && hexVal(b[i]) >= 0 && hexVal(b[i+1]) >= 0 {
				out = append(out, byte(hexVal(b[i])<<4|hexVal(b[i+1])))
				i += 2
			} else {
				// Invalid \x escape
				switch errmode {
				case "ignore":
					// Skip one extra hex digit if present (CPython: Objects/bytesobject.c:1176)
					if i < len(b) && hexVal(b[i]) >= 0 {
						i++
					}
				case "replace":
					out = append(out, '?')
					if i < len(b) && hexVal(b[i]) >= 0 {
						i++
					}
				default:
					return nil, fmt.Errorf("ValueError: invalid \\x escape at position %d", startPos)
				}
			}
		default:
			// Unknown escape: emit DeprecationWarning and pass through.
			// CPython: Objects/bytesobject.c:1223 PyBytes_DecodeEscape
			msg := fmt.Sprintf("b\"\\%c\" is an invalid escape sequence. Such sequences will not work in the future. ", esc)
			_ = pywarn.WarnUnicode(pyerrors.PyExc_DeprecationWarning, msg, 2, nil)
			out = append(out, '\\', esc)
		}
	}
	return out, nil
}

func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

// ---------------------------------------------------------------------------
// readbuffer_encode(data, errors='strict') -> (bytes, length)
//
// Deprecated: returns bytes(data) for any buffer-compatible object.
//
// CPython: Modules/_codecsmodule.c:701 _codecs_readbuffer_encode_impl
// ---------------------------------------------------------------------------

func readbufferEncode(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("TypeError: readbuffer_encode() takes 1 to 2 arguments (%d given)", len(args))
	}
	b, err := toBytesBuffer(args[0], "readbuffer_encode", 1)
	if err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{objects.NewBytes(b), objects.NewInt(int64(len(b)))}), nil
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
			// A single U+FFFE means "undefined" (CPython Objects/unicodeobject.c:8680).
			if v.Length() == 1 && []rune(v.Value())[0] == 0xFFFE {
				rep, newpos, herr := callDecodeErrorHandler("charmap", "character maps to <undefined>", data, i, i+1, errors)
				if herr != nil {
					return "", herr
				}
				b = append(b, []rune(rep)...)
				i = newpos
				continue
			}
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

// pySearchRegistry tracks the mapping from Python callable object identity
// to the registry handle returned by codecs.Register. Required so that
// codecs.unregister(fn) can find the exact Go-side entry to remove.
var (
	pySearchMu       sync.Mutex
	pySearchRegistry = map[uintptr]uint64{} // object pointer -> registry handle
)

func codecsRegister(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: register() takes exactly 1 argument (%d given)", len(args))
	}
	search := args[0]
	if !objects.Callable(search) {
		return nil, fmt.Errorf("TypeError: argument must be callable")
	}
	id := codecs.Register(pythonSearchFunc(search))
	pySearchMu.Lock()
	pySearchRegistry[reflect.ValueOf(search).Pointer()] = id
	pySearchMu.Unlock()
	return objects.None(), nil
}

// codecsUnregister removes a previously registered search function.
//
// CPython: Python/codecs.c:68 PyCodec_Unregister
func codecsUnregister(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: unregister() takes exactly 1 argument (%d given)", len(args))
	}
	search := args[0]
	if !objects.Callable(search) {
		return nil, fmt.Errorf("TypeError: argument must be callable")
	}
	key := reflect.ValueOf(search).Pointer()
	pySearchMu.Lock()
	id, ok := pySearchRegistry[key]
	if ok {
		delete(pySearchRegistry, key)
	}
	pySearchMu.Unlock()
	if ok {
		codecs.UnregisterByID(id)
	}
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
	// Try attribute access first (CodecInfo namedtuple has .encode/.decode).
	// Fall back to positional indexing for plain 4-tuples returned by
	// legacy search functions (e.g. testcodec.getregentry()).
	//
	// CPython: Python/codecs.c:99 _PyCodec_Lookup normalises via PySequence_GetItem
	getCallable := func(attrName string, idx int) (objects.Object, error) {
		v, err := objects.GetAttr(info, objects.NewStr(attrName))
		if err == nil {
			return v, nil
		}
		if t, ok := info.(*objects.Tuple); ok && t.Len() > idx {
			return t.Item(idx), nil
		}
		return nil, fmt.Errorf("TypeError: codec search function must return a CodecInfo or 4-tuple")
	}
	enc, err := getCallable("encode", 0)
	if err != nil {
		return nil, err
	}
	dec, err := getCallable("decode", 1)
	if err != nil {
		return nil, err
	}
	// Read the codec's own .name attribute for use in CodecInfo.Name and makeCodecInfo.
	// Note: we do NOT change the map key (name) so alias lookups like 'base64'
	// still find the cached entry for 'base64_codec'.
	// CPython: Python/codecs.c:99 _PyCodec_Lookup
	canonicalName := name
	if nameAttr, e2 := objects.GetAttr(info, objects.NewStr("name")); e2 == nil {
		if nameStr, ok := nameAttr.(*objects.Unicode); ok && nameStr.Value() != "" {
			canonicalName = nameStr.Value()
		}
	}
	// Cache the raw Python callables so encode/decode can bypass the Go text-codec
	// wrappers for binary transform codecs (base64, hex, zlib, uu, quopri).
	// CPython: Modules/_codecsmodule.c:127 _codecs_encode_impl accepts any PyObject*
	pyCodecEncodeMu.Lock()
	// Store under both the lookup name (e.g. "base64_codec") and the canonical name
	// (e.g. "base64") so makeCodecInfo and codecsEncode can find the entry regardless
	// of which form was used in the original lookup.
	for _, k := range []string{name, canonicalName} {
		pyCodecEncodeMap[k] = enc
		pyCodecDecodeMap[k] = dec
		pyCodecObjectMap[k] = info // cache original Python CodecInfo for verbatim return
	}
	// Also store Python incremental encoder/decoder/stream classes so makeCodecInfo
	// can expose the real Python classes (e.g. rot_13.IncrementalEncoder) instead of
	// wrapping them in our Go MultibyteIncrementalEncoder.
	ie, _ := objects.GetAttr(info, objects.NewStr("incrementalencoder"))
	id2, _ := objects.GetAttr(info, objects.NewStr("incrementaldecoder"))
	sr, _ := objects.GetAttr(info, objects.NewStr("streamreader"))
	sw, _ := objects.GetAttr(info, objects.NewStr("streamwriter"))
	for _, k := range []string{name, canonicalName} {
		if ie != nil && ie != objects.None() {
			pyCodecIncrementalEncoderMap[k] = ie
		}
		if id2 != nil && id2 != objects.None() {
			pyCodecIncrementalDecoderMap[k] = id2
		}
		if sr != nil && sr != objects.None() {
			pyCodecStreamReaderMap[k] = sr
		}
		if sw != nil && sw != objects.None() {
			pyCodecStreamWriterMap[k] = sw
		}
	}
	// Propagate _is_text_encoding=False to the codec registry so bytes.decode()
	// can reject binary-transform and text-transform codecs with LookupError.
	// CPython: Lib/encodings/__init__.py CodecInfo._is_text_encoding
	if attr, e2 := objects.GetAttr(info, objects.NewStr("_is_text_encoding")); e2 == nil {
		if b, e3 := objects.IsTruthy(attr); e3 == nil && !b {
			codecs.MarkNonTextEncoding(name)
			codecs.MarkNonTextEncoding(canonicalName)
		}
	}
	pyCodecEncodeMu.Unlock()

	ci := &codecs.CodecInfo{
		Name: canonicalName,
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
	// The handler receives byte-offset positions (matching the Go
	// ErrorHandler contract), but we build a UnicodeEncodeError with
	// character positions so Python-level handlers see exc.start/end as
	// character indices, matching CPython's UnicodeError contract.
	//
	// CPython: Objects/exceptions.c:3040 UnicodeError_init
	// CPython: Modules/cjkcodecs/multibytecodec.c:507 error handler call
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
		// Convert byte offsets to character positions for the Python exc.
		inputStr := string(input)
		inputRunes := []rune(inputStr)
		startChar := len([]rune(string(input[:start])))
		endChar := len([]rune(string(input[:end])))
		exc := pyerrors.NewUnicodeEncodeError(enc, objects.NewStr(inputStr), startChar, endChar, reason)
		result, cerr := objects.Call(h, objects.NewTuple([]objects.Object{exc}), nil)
		if cerr != nil {
			return "", 0, cerr
		}
		tup, ok := result.(*objects.Tuple)
		if !ok || tup.Len() != 2 {
			return "", 0, fmt.Errorf("TypeError: error handler must return a (str, int) tuple")
		}
		// Accept str or bytes as the replacement.
		var repStr string
		switch r := tup.Item(0).(type) {
		case *objects.Unicode:
			repStr = r.Value()
		case *objects.Bytes:
			repStr = string(r.Bytes())
		default:
			return "", 0, fmt.Errorf("TypeError: error handler replacement must be str or bytes")
		}
		posObj, ok := tup.Item(1).(*objects.Int)
		if !ok {
			return "", 0, fmt.Errorf("TypeError: error handler new position must be int")
		}
		newposChar, ok := posObj.Int64()
		if !ok {
			return "", 0, fmt.Errorf("IndexError: new position is out of bounds")
		}
		// Convert returned character index to byte offset.
		nChar := int(newposChar)
		if nChar < 0 {
			nChar += len(inputRunes)
		}
		if nChar < 0 || nChar > len(inputRunes) {
			return "", 0, fmt.Errorf("IndexError: new position is out of bounds")
		}
		newposByte := charToByte(inputRunes, nChar)
		return repStr, newposByte, nil
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
	goHandler, err := codecs.LookupError(name)
	if err != nil {
		return nil, fmt.Errorf("LookupError: unknown error handler name %q", name)
	}
	// Return a callable wrapper that bridges a Python UnicodeError argument
	// to the Go-level handler. CPython exposes built-in handlers as Python
	// callables (e.g. codecs.ignore_errors) so user code can pass them to
	// register_error.
	//
	// CPython: Python/codecs.c:893 PyCodec_LookupError (returns Python callable)
	return objects.NewBuiltinFunction(name, func(args2 []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		if len(args2) != 1 {
			return nil, fmt.Errorf("TypeError: %s() takes exactly 1 argument (%d given)", name, len(args2))
		}
		exc := args2[0]
		encObj, _ := objects.GetAttr(exc, objects.NewStr("encoding"))
		reasonObj, _ := objects.GetAttr(exc, objects.NewStr("reason"))
		inputObj, _ := objects.GetAttr(exc, objects.NewStr("object"))
		startObj, _ := objects.GetAttr(exc, objects.NewStr("start"))
		endObj, _ := objects.GetAttr(exc, objects.NewStr("end"))
		enc, _ := objects.Str(encObj)
		reason, _ := objects.Str(reasonObj)
		var input []byte
		switch v := inputObj.(type) {
		case *objects.Bytes:
			input = v.Bytes()
		case *objects.Unicode:
			input = []byte(v.Value())
		}
		startN := int64(0)
		if si, ok := startObj.(*objects.Int); ok {
			startN, _ = si.Int64()
		}
		endN := int64(0)
		if ei, ok := endObj.(*objects.Int); ok {
			endN, _ = ei.Int64()
		}
		rep, newpos, err := goHandler(enc, reason, input, int(startN), int(endN))
		if err != nil {
			return nil, err
		}
		return objects.NewTuple([]objects.Object{objects.NewStr(rep), objects.NewInt(int64(newpos))}), nil
	}), nil
}

// codecsUnregisterError implements _codecs._unregister_error(name).
// Removes a previously registered error handler. Raises ValueError for
// built-in handler names (strict, ignore, replace, etc.).
//
// CPython: Modules/_codecsmodule.c:480 _codecs__unregister_error_impl
// CPython: Python/codecs.c:637 _PyCodec_UnregisterError
func codecsUnregisterError(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: _unregister_error() takes exactly 1 argument (%d given)", len(args))
	}
	nameObj, ok := args[0].(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: _unregister_error() argument must be str, not %s", args[0].Type().Name)
	}
	name := nameObj.Value()
	// Remove from the Python-level handler map too.
	pyErrMu.Lock()
	delete(pyErrHandlers, name)
	pyErrMu.Unlock()
	if err := codecs.UnregisterError(name); err != nil {
		return nil, err
	}
	return objects.None(), nil
}

// charToByte returns the byte offset of the nth rune in runes, which is
// the UTF-8 encoding of the rune slice as a flat byte string.
// charToByte(runes, len(runes)) returns the total byte length.
func charToByte(runes []rune, n int) int {
	off := 0
	for i := 0; i < n && i < len(runes); i++ {
		sz := utf8.RuneLen(runes[i])
		if sz < 0 {
			sz = 3 // replacement for invalid rune (CESU-8 surrogates etc.)
		}
		off += sz
	}
	return off
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

// toBytesBuffer coerces obj to []byte accepting str (UTF-8), bytes,
// bytearray, and any buffer-protocol object with a tobytes() method.
//
// CPython: Modules/_codecsmodule.c uses s* (Py_buffer) which accepts all of these.
func toBytesBuffer(obj objects.Object, fname string, pos int) ([]byte, error) {
	switch v := obj.(type) {
	case *objects.Bytes:
		return v.Bytes(), nil
	case *objects.ByteArray:
		return v.Bytes(), nil
	case *objects.Unicode:
		return []byte(v.Value()), nil
	default:
		// Try tobytes() for buffer-protocol objects like array.array.
		tb, err := objects.GetAttr(obj, objects.NewStr("tobytes"))
		if err != nil {
			return nil, fmt.Errorf("TypeError: %s() argument %d must be a bytes-like object, not %s", fname, pos, obj.Type().Name)
		}
		res, err := objects.Call(tb, objects.NewTuple(nil), nil)
		if err != nil {
			return nil, fmt.Errorf("TypeError: %s() argument %d must be a bytes-like object, not %s", fname, pos, obj.Type().Name)
		}
		b, ok := res.(*objects.Bytes)
		if !ok {
			return nil, fmt.Errorf("TypeError: %s() argument %d must be a bytes-like object, not %s", fname, pos, obj.Type().Name)
		}
		return b.Bytes(), nil
	}
}
