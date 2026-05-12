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

func init() {
	// Seed the well-known names so lookup_error returns them.
	for _, name := range []string{"strict", "ignore", "replace"} {
		n := name // capture
		codecs.RegisterError(n, func(enc string, input []byte, start, end int) (string, int, error) {
			h, err := codecs.LookupError(n)
			if err != nil {
				return "", 0, err
			}
			return h(enc, input, start, end)
		})
	}
}

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

	d := inst.Dict()
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
// charmap_encode / charmap_decode — stubs
//
// CPython: Modules/_codecsmodule.c:427 _codecs_charmap_encode_impl
// ---------------------------------------------------------------------------

func charmapEncode(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return nil, fmt.Errorf("NotImplementedError: charmap_encode is not supported")
}

func charmapDecode(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return nil, fmt.Errorf("NotImplementedError: charmap_decode is not supported")
}

// ---------------------------------------------------------------------------
// register(search_function) — no-op stub for now
//
// CPython: Modules/_codecsmodule.c:76 _codecs_register_impl
// ---------------------------------------------------------------------------

func codecsRegister(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: register() takes exactly 1 argument (%d given)", len(args))
	}
	// Stub: user-supplied search functions are not yet plumbed into the
	// codecs.Lookup path. The registration succeeds silently.
	return objects.None(), nil
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
	codecs.RegisterError(name, func(enc string, input []byte, start, end int) (string, int, error) {
		pyErrMu.RLock()
		h := pyErrHandlers[name]
		pyErrMu.RUnlock()
		if h == nil {
			return "", 0, fmt.Errorf("LookupError: unknown error handler name %q", name)
		}
		// Build a minimal UnicodeDecodeError-like tuple to pass to the handler.
		errArg := objects.NewTuple([]objects.Object{
			objects.NewStr(enc),
			objects.NewBytes(input),
			objects.NewInt(int64(start)),
			objects.NewInt(int64(end)),
			objects.NewStr("codec error"),
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
