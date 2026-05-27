// Package _multibytecodec is the gopy port of CPython's
// Modules/cjkcodecs/multibytecodec.c Python module surface.
// It exposes MultibyteStreamReader, MultibyteStreamWriter,
// MultibyteIncrementalEncoder, and MultibyteIncrementalDecoder types
// that wrap an arbitrary stream object or codec with a CJK codec.
//
// The stream encoding/decoding runtime lives in codecs/cjkcodecs/;
// this package is the Python-visible module that `import _multibytecodec`
// resolves to.
//
// CPython: Modules/cjkcodecs/multibytecodec.c:1 _multibytecodec module
package _multibytecodec

import (
	"fmt"

	"github.com/tamnd/gopy/codecs"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("_multibytecodec", buildModule)
}

var (
	// MultibyteStreamReaderType is the exported Python type for the reader.
	MultibyteStreamReaderType *objects.Type
	// MultibyteStreamWriterType is the exported Python type for the writer.
	MultibyteStreamWriterType *objects.Type
	// MultibyteIncrementalEncoderType is the exported Python type for incremental encoding.
	MultibyteIncrementalEncoderType *objects.Type
	// MultibyteIncrementalDecoderType is the exported Python type for incremental decoding.
	MultibyteIncrementalDecoderType *objects.Type
)

func init() {
	MultibyteStreamReaderType = objects.NewType(
		"MultibyteStreamReader",
		[]*objects.Type{objects.ObjectType()},
	)
	MultibyteStreamReaderType.TpNew = newMBStreamReader

	MultibyteStreamWriterType = objects.NewType(
		"MultibyteStreamWriter",
		[]*objects.Type{objects.ObjectType()},
	)
	MultibyteStreamWriterType.TpNew = newMBStreamWriter

	MultibyteIncrementalEncoderType = objects.NewType(
		"MultibyteIncrementalEncoder",
		[]*objects.Type{objects.ObjectType()},
	)
	MultibyteIncrementalEncoderType.Getattro = mbIncrementalEncoderGetattr
	MultibyteIncrementalEncoderType.Setattro = mbIncrementalEncoderSetattr
	objects.SetTypeDescr(MultibyteIncrementalEncoderType, "encode",
		objects.NewMethodDescr(MultibyteIncrementalEncoderType, "encode", mbIncrementalEncoderEncode))
	objects.SetTypeDescr(MultibyteIncrementalEncoderType, "reset",
		objects.NewMethodDescr(MultibyteIncrementalEncoderType, "reset", mbIncrementalEncoderReset))
	objects.SetTypeDescr(MultibyteIncrementalEncoderType, "getstate",
		objects.NewMethodDescr(MultibyteIncrementalEncoderType, "getstate", mbIncrementalEncoderGetstate))
	objects.SetTypeDescr(MultibyteIncrementalEncoderType, "setstate",
		objects.NewMethodDescr(MultibyteIncrementalEncoderType, "setstate", mbIncrementalEncoderSetstate))

	MultibyteIncrementalDecoderType = objects.NewType(
		"MultibyteIncrementalDecoder",
		[]*objects.Type{objects.ObjectType()},
	)
	MultibyteIncrementalDecoderType.Getattro = mbIncrementalDecoderGetattr
	MultibyteIncrementalDecoderType.Setattro = mbIncrementalDecoderSetattr
	objects.SetTypeDescr(MultibyteIncrementalDecoderType, "decode",
		objects.NewMethodDescr(MultibyteIncrementalDecoderType, "decode", mbIncrementalDecoderDecode))
	objects.SetTypeDescr(MultibyteIncrementalDecoderType, "reset",
		objects.NewMethodDescr(MultibyteIncrementalDecoderType, "reset", mbIncrementalDecoderReset))
	objects.SetTypeDescr(MultibyteIncrementalDecoderType, "getstate",
		objects.NewMethodDescr(MultibyteIncrementalDecoderType, "getstate", mbIncrementalDecoderGetstate))
	objects.SetTypeDescr(MultibyteIncrementalDecoderType, "setstate",
		objects.NewMethodDescr(MultibyteIncrementalDecoderType, "setstate", mbIncrementalDecoderSetstate))
}

// mbStreamReader is a Python object wrapping a readable stream with a
// CJK decoder.
//
// CPython: Modules/cjkcodecs/multibytecodec.c:891 MultibyteStreamReaderObject
type mbStreamReader struct {
	objects.Header
	stream objects.Object
}

// newMBStreamReader constructs a MultibyteStreamReader.
//
// CPython: Modules/cjkcodecs/multibytecodec.c:1028 mbstreamreader_init
// The stream argument must expose a 'read' method. Passing None raises
// AttributeError, matching CPython's _PyObject_LookupAttr("read") == 0 branch.
func newMBStreamReader(_ *objects.Type, args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: MultibyteStreamReader() takes at least 1 argument")
	}
	stream := args[0]
	_, err := objects.GetAttr(stream, objects.NewStr("read"))
	if err != nil {
		return nil, fmt.Errorf("AttributeError: stream argument must have a 'read' method")
	}
	r := &mbStreamReader{stream: stream}
	r.Header.Init(MultibyteStreamReaderType)
	return r, nil
}

// mbStreamWriter is a Python object wrapping a writable stream with a
// CJK encoder.
//
// CPython: Modules/cjkcodecs/multibytecodec.c:1105 MultibyteStreamWriterObject
type mbStreamWriter struct {
	objects.Header
	stream objects.Object
}

// newMBStreamWriter constructs a MultibyteStreamWriter.
//
// CPython: Modules/cjkcodecs/multibytecodec.c:1250 mbstreamwriter_init
// The stream argument must expose a 'write' method. Passing None raises
// AttributeError.
func newMBStreamWriter(_ *objects.Type, args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: MultibyteStreamWriter() takes at least 1 argument")
	}
	stream := args[0]
	_, err := objects.GetAttr(stream, objects.NewStr("write"))
	if err != nil {
		return nil, fmt.Errorf("AttributeError: stream argument must have a 'write' method")
	}
	w := &mbStreamWriter{stream: stream}
	w.Header.Init(MultibyteStreamWriterType)
	return w, nil
}

// mbIncrementalEncoder wraps a codec's Encode function for incremental use.
// encodeFn is a per-instance stateful encoder (non-nil for stateful codecs).
// getStateFn and setStateFn expose the internal pending+cBytes state for
// getstate()/setstate() support (non-nil when ci.NewIncrementalEncoderFull != nil).
//
// CPython: Modules/cjkcodecs/multibytecodec.c:735 MultibyteIncrementalEncoderObject
type mbIncrementalEncoder struct {
	objects.Header
	ci          *codecs.CodecInfo
	errors      string
	encodeFn    func(input string, errors string, final bool) ([]byte, int, error)
	getStateFn  func() ([]rune, [8]byte)
	setStateFn  func([]rune, [8]byte) error
}

// MakeIncrementalEncoderClass returns a Python callable that, when called,
// creates a MultibyteIncrementalEncoder bound to the given codec.
// The callable acts as the incrementalencoder class for a CodecInfo.
//
// CPython: Modules/cjkcodecs/multibytecodec.c:735 MultibyteIncrementalEncoderObject
func MakeIncrementalEncoderClass(ci *codecs.CodecInfo) objects.Object {
	return objects.NewBuiltinFunction("IncrementalEncoder",
		func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
			errors := "strict"
			if len(args) >= 1 {
				if s, ok := args[0].(*objects.Unicode); ok {
					errors = s.Value()
				}
			}
			if v, ok := kwargs["errors"]; ok {
				if s, ok := v.(*objects.Unicode); ok {
					errors = s.Value()
				}
			}
			enc := &mbIncrementalEncoder{ci: ci, errors: errors}
			if ci.NewIncrementalEncoderFull != nil {
				enc.encodeFn, enc.getStateFn, enc.setStateFn = ci.NewIncrementalEncoderFull()
			} else if ci.NewIncrementalEncoder != nil {
				enc.encodeFn = ci.NewIncrementalEncoder()
			}
			enc.Header.Init(MultibyteIncrementalEncoderType)
			return enc, nil
		})
}

func mbIncrementalEncoderGetattr(o objects.Object, name objects.Object) (objects.Object, error) {
	enc, ok := o.(*mbIncrementalEncoder)
	if !ok {
		return nil, fmt.Errorf("TypeError: expected MultibyteIncrementalEncoder")
	}
	s, _ := objects.Str(name)
	if s == "errors" {
		return objects.NewStr(enc.errors), nil
	}
	return objects.GenericGetAttr(o, name)
}

func mbIncrementalEncoderSetattr(o objects.Object, name objects.Object, value objects.Object) error {
	enc, ok := o.(*mbIncrementalEncoder)
	if !ok {
		return fmt.Errorf("TypeError: expected MultibyteIncrementalEncoder")
	}
	s, _ := objects.Str(name)
	if s == "errors" {
		if value == nil {
			return fmt.Errorf("AttributeError: cannot delete 'errors'")
		}
		sv, ok2 := value.(*objects.Unicode)
		if !ok2 {
			return fmt.Errorf("TypeError: errors must be str, not %s", value.Type().Name)
		}
		enc.errors = sv.Value()
		return nil
	}
	return fmt.Errorf("AttributeError: 'MultibyteIncrementalEncoder' object has no attribute '%s'", s)
}

// mbIncrementalEncoderEncode implements IncrementalEncoder.encode(input[, final]).
//
// CPython: Modules/cjkcodecs/multibytecodec.c:789 mbincrencoder_encode
func mbIncrementalEncoderEncode(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: encode() takes at least 1 argument")
	}
	self, ok := args[0].(*mbIncrementalEncoder)
	if !ok {
		return nil, fmt.Errorf("TypeError: expected MultibyteIncrementalEncoder")
	}
	input, ok := args[1].(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: encode() argument must be str, not %s", args[1].Type().Name)
	}
	final := false
	if len(args) >= 3 {
		final = objects.IsTrue(args[2])
	}
	// Also accept final as a keyword argument.
	// CPython: Modules/cjkcodecs/multibytecodec.c:789 mbincrencoder_encode
	if v, ok := kwargs["final"]; ok {
		final = objects.IsTrue(v)
	}
	var out []byte
	var err error
	if self.encodeFn != nil {
		out, _, err = self.encodeFn(input.Value(), self.errors, final)
	} else {
		out, _, err = self.ci.Encode(input.Value(), self.errors)
	}
	if err != nil {
		return nil, err
	}
	return objects.NewBytes(out), nil
}

// mbIncrementalEncoderReset implements IncrementalEncoder.reset().
//
// CPython: Modules/cjkcodecs/multibytecodec.c:826 mbincrencoder_reset
func mbIncrementalEncoderReset(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) >= 1 {
		if enc, ok := args[0].(*mbIncrementalEncoder); ok {
			// Recreate encoder state to reset pending + codec state.
			if enc.ci.NewIncrementalEncoderFull != nil {
				enc.encodeFn, enc.getStateFn, enc.setStateFn = enc.ci.NewIncrementalEncoderFull()
			} else if enc.ci.NewIncrementalEncoder != nil {
				enc.encodeFn = enc.ci.NewIncrementalEncoder()
			}
		}
	}
	return objects.None(), nil
}

// mbIncrementalEncoderGetstate implements IncrementalEncoder.getstate().
// The state is a single integer whose 9 little-endian bytes encode:
//   byte 0:   len(pending UTF-8 bytes) — 0 when no pending runes
//   bytes 1-8: if byte-0 > 0: UTF-8 of pending runes; else codec cBytes
//
// CPython: Modules/cjkcodecs/multibytecodec.c:852 mbincrencoder_getstate
func mbIncrementalEncoderGetstate(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: getstate() takes 1 argument")
	}
	enc, ok := args[0].(*mbIncrementalEncoder)
	if !ok {
		return nil, fmt.Errorf("TypeError: expected MultibyteIncrementalEncoder")
	}
	if enc.getStateFn == nil {
		return objects.NewInt(0), nil
	}
	pending, cBytes := enc.getStateFn()
	var buf [9]byte
	if len(pending) > 0 {
		utf8Bytes := []byte(string(pending))
		buf[0] = byte(len(utf8Bytes))
		copy(buf[1:], utf8Bytes)
	} else {
		buf[0] = 0
		copy(buf[1:], cBytes[:])
	}
	var n int64
	for i := 8; i >= 0; i-- {
		n = (n << 8) | int64(buf[i])
	}
	return objects.NewInt(n), nil
}

// mbIncrementalEncoderSetstate implements IncrementalEncoder.setstate(state).
// Validates the state integer, then restores the encoder's pending + cBytes.
//
// CPython: Modules/cjkcodecs/multibytecodec.c:873 mbincrencoder_setstate
func mbIncrementalEncoderSetstate(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: setstate() takes 2 arguments")
	}
	enc, ok := args[0].(*mbIncrementalEncoder)
	if !ok {
		return nil, fmt.Errorf("TypeError: expected MultibyteIncrementalEncoder")
	}
	stateObj, ok := args[1].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: setstate() argument must be int, not %s", args[1].Type().Name)
	}
	n, _ := stateObj.Int64()
	// Extract 9 little-endian bytes from the integer.
	var buf [9]byte
	tmp := n
	for i := 0; i < 9; i++ {
		buf[i] = byte(tmp & 0xff)
		tmp >>= 8
	}
	pendingSize := int(buf[0])
	if pendingSize >= 9 {
		return nil, fmt.Errorf("UnicodeError: pending buffer is larger than 8 bytes")
	}
	var pending []rune
	var cBytes [8]byte
	if pendingSize > 0 {
		utf8Bytes := buf[1 : 1+pendingSize]
		// Validate and decode as UTF-8.
		s := string(utf8Bytes)
		for _, r := range s {
			if r == '�' {
				return nil, fmt.Errorf("UnicodeDecodeError: 'utf-8' codec can't decode bytes in pending buffer")
			}
		}
		// Re-validate: ensure no replacement character appeared due to invalid bytes.
		encoded := []byte(s)
		for i, b := range utf8Bytes {
			if i < len(encoded) && encoded[i] != b {
				return nil, fmt.Errorf("UnicodeDecodeError: 'utf-8' codec can't decode bytes in pending buffer")
			}
		}
		if !isValidUTF8Bytes(utf8Bytes) {
			return nil, fmt.Errorf("UnicodeDecodeError: 'utf-8' codec can't decode bytes in pending buffer")
		}
		pending = []rune(s)
	} else {
		copy(cBytes[:], buf[1:9])
	}
	if enc.setStateFn != nil {
		if err := enc.setStateFn(pending, cBytes); err != nil {
			return nil, err
		}
	}
	return objects.None(), nil
}

// isValidUTF8Bytes returns true if b is valid UTF-8 with no replacement characters.
func isValidUTF8Bytes(b []byte) bool {
	for i := 0; i < len(b); {
		if b[i] < 0x80 {
			i++
			continue
		}
		if b[i] < 0xC2 || b[i] > 0xF4 {
			return false
		}
		var seqLen int
		switch {
		case b[i] < 0xE0:
			seqLen = 2
		case b[i] < 0xF0:
			seqLen = 3
		default:
			seqLen = 4
		}
		if i+seqLen > len(b) {
			return false
		}
		for j := 1; j < seqLen; j++ {
			if b[i+j]&0xC0 != 0x80 {
				return false
			}
		}
		i += seqLen
	}
	return true
}

// mbIncrementalDecoder wraps a codec's IncrementalDecode function.
// buf holds bytes that form an incomplete multibyte sequence.
// decodeFn is a per-instance stateful decoder (non-nil for stateful codecs).
// flags is an opaque integer preserved across getstate/setstate round-trips.
//
// CPython: Modules/cjkcodecs/multibytecodec.c:834 MultibyteIncrementalDecoderObject
type mbIncrementalDecoder struct {
	objects.Header
	ci       *codecs.CodecInfo
	errors   string
	buf      []byte
	flags    int64
	decodeFn func(input []byte, errors string, final bool) (string, []byte, error)
}

// MakeIncrementalDecoderClass returns a Python callable that, when called,
// creates a MultibyteIncrementalDecoder bound to the given codec.
//
// CPython: Modules/cjkcodecs/multibytecodec.c:834 MultibyteIncrementalDecoderObject
func MakeIncrementalDecoderClass(ci *codecs.CodecInfo) objects.Object {
	return objects.NewBuiltinFunction("IncrementalDecoder",
		func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
			errors := "strict"
			if len(args) >= 1 {
				if s, ok := args[0].(*objects.Unicode); ok {
					errors = s.Value()
				}
			}
			if v, ok := kwargs["errors"]; ok {
				if s, ok := v.(*objects.Unicode); ok {
					errors = s.Value()
				}
			}
			dec := &mbIncrementalDecoder{ci: ci, errors: errors}
			if ci.NewIncrementalDecoder != nil {
				dec.decodeFn = ci.NewIncrementalDecoder()
			}
			dec.Header.Init(MultibyteIncrementalDecoderType)
			return dec, nil
		})
}

func mbIncrementalDecoderGetattr(o objects.Object, name objects.Object) (objects.Object, error) {
	dec, ok := o.(*mbIncrementalDecoder)
	if !ok {
		return nil, fmt.Errorf("TypeError: expected MultibyteIncrementalDecoder")
	}
	s, _ := objects.Str(name)
	if s == "errors" {
		return objects.NewStr(dec.errors), nil
	}
	return objects.GenericGetAttr(o, name)
}

func mbIncrementalDecoderSetattr(o objects.Object, name objects.Object, value objects.Object) error {
	dec, ok := o.(*mbIncrementalDecoder)
	if !ok {
		return fmt.Errorf("TypeError: expected MultibyteIncrementalDecoder")
	}
	s, _ := objects.Str(name)
	if s == "errors" {
		if value == nil {
			return fmt.Errorf("AttributeError: cannot delete 'errors'")
		}
		sv, ok2 := value.(*objects.Unicode)
		if !ok2 {
			return fmt.Errorf("TypeError: errors must be str, not %s", value.Type().Name)
		}
		dec.errors = sv.Value()
		return nil
	}
	return fmt.Errorf("AttributeError: 'MultibyteIncrementalDecoder' object has no attribute '%s'", s)
}

// mbIncrementalDecoderDecode implements IncrementalDecoder.decode(input[, final]).
//
// CPython: Modules/cjkcodecs/multibytecodec.c:887 mbincrdecoder_decode
func mbIncrementalDecoderDecode(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: decode() takes at least 1 argument")
	}
	self, ok := args[0].(*mbIncrementalDecoder)
	if !ok {
		return nil, fmt.Errorf("TypeError: expected MultibyteIncrementalDecoder")
	}
	var inputBytes []byte
	switch v := args[1].(type) {
	case *objects.Bytes:
		inputBytes = v.Bytes()
	case *objects.ByteArray:
		inputBytes = v.Bytes()
	default:
		return nil, fmt.Errorf("TypeError: decode() argument must be bytes-like, not %s", args[1].Type().Name)
	}
	final := false
	if len(args) >= 3 {
		final = objects.IsTrue(args[2])
	}

	// Prepend any buffered incomplete bytes from the previous call.
	// Save the original buf; restore it on error (CPython preserves pending on decode failure).
	// CPython: Modules/cjkcodecs/multibytecodec.c:887 mbincrdecoder_decode
	savedBuf := self.buf
	data := append(self.buf, inputBytes...)
	self.buf = nil

	var out string
	var remaining []byte
	var err error
	if self.decodeFn != nil {
		// Stateful per-instance decoder (HZ, ISO-2022): codec state preserved.
		out, remaining, err = self.decodeFn(data, self.errors, final)
	} else if self.ci.IncrementalDecode != nil {
		out, remaining, err = self.ci.IncrementalDecode(data, self.errors, final)
	} else {
		// Fallback: use synchronous Decode (no partial-sequence buffering).
		out, _, err = self.ci.Decode(data, self.errors)
	}
	if err != nil {
		self.buf = savedBuf
		return nil, err
	}
	if len(remaining) > 0 {
		self.buf = make([]byte, len(remaining))
		copy(self.buf, remaining)
	}
	return objects.NewStr(out), nil
}

// mbIncrementalDecoderReset implements IncrementalDecoder.reset().
//
// CPython: Modules/cjkcodecs/multibytecodec.c:924 mbincrdecoder_reset
func mbIncrementalDecoderReset(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) >= 1 {
		if dec, ok := args[0].(*mbIncrementalDecoder); ok {
			dec.buf = nil
			// Recreate decodeFn to reset the captured codec state.
			if dec.ci.NewIncrementalDecoder != nil {
				dec.decodeFn = dec.ci.NewIncrementalDecoder()
			}
		}
	}
	return objects.None(), nil
}

// mbIncrementalDecoderGetstate implements IncrementalDecoder.getstate().
// Returns (pending_bytes, flags_int) matching CPython's mbincrdecoder_getstate.
//
// CPython: Modules/cjkcodecs/multibytecodec.c:928 mbincrdecoder_getstate
func mbIncrementalDecoderGetstate(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: getstate() takes 1 argument")
	}
	dec, ok := args[0].(*mbIncrementalDecoder)
	if !ok {
		return nil, fmt.Errorf("TypeError: expected MultibyteIncrementalDecoder")
	}
	var buf []byte
	if len(dec.buf) > 0 {
		buf = dec.buf
	} else {
		buf = []byte{}
	}
	return objects.NewTuple([]objects.Object{
		objects.NewBytes(buf),
		objects.NewInt(dec.flags),
	}), nil
}

// mbIncrementalDecoderSetstate implements IncrementalDecoder.setstate((bytes, int)).
// Validates the tuple and restores buf + flags. Raises UnicodeDecodeError if
// the pending buffer exceeds 8 bytes (MAXDECPENDING in CPython).
//
// CPython: Modules/cjkcodecs/multibytecodec.c:944 mbincrdecoder_setstate
func mbIncrementalDecoderSetstate(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: setstate() takes 2 arguments")
	}
	dec, ok := args[0].(*mbIncrementalDecoder)
	if !ok {
		return nil, fmt.Errorf("TypeError: expected MultibyteIncrementalDecoder")
	}
	tup, ok := args[1].(*objects.Tuple)
	if !ok {
		return nil, fmt.Errorf("TypeError: setstate() argument must be a tuple, not %s", args[1].Type().Name)
	}
	if tup.Len() != 2 {
		return nil, fmt.Errorf("TypeError: setstate() argument must be a 2-tuple")
	}
	pendingObj, ok := tup.Item(0).(*objects.Bytes)
	if !ok {
		return nil, fmt.Errorf("TypeError: setstate() first element must be bytes, not %s", tup.Item(0).Type().Name)
	}
	flagsObj, ok := tup.Item(1).(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: setstate() second element must be int, not %s", tup.Item(1).Type().Name)
	}
	pending := pendingObj.Bytes()
	if len(pending) > 8 {
		return nil, fmt.Errorf("UnicodeDecodeError: pending buffer is too large")
	}
	flags, _ := flagsObj.Int64()
	if len(pending) == 0 {
		dec.buf = nil
	} else {
		dec.buf = make([]byte, len(pending))
		copy(dec.buf, pending)
	}
	dec.flags = flags
	return objects.None(), nil
}

// mbStreamWriterInstance is a Python object that wraps a writable stream
// and encodes Unicode strings through a codec before writing.
// encodeFn is a per-instance stateful encoder (non-nil for stateful codecs).
//
// CPython: Modules/cjkcodecs/multibytecodec.c:1105 MultibyteStreamWriterObject (instance)
type mbStreamWriterInstance struct {
	objects.Header
	ci       *codecs.CodecInfo
	stream   objects.Object
	errors   string
	encodeFn func(input string, errors string, final bool) ([]byte, int, error)
}

var mbStreamWriterInstanceType *objects.Type

func init() {
	mbStreamWriterInstanceType = objects.NewType(
		"StreamWriter",
		[]*objects.Type{objects.ObjectType()},
	)
	mbStreamWriterInstanceType.Getattro = mbSWIGetattr
	mbStreamWriterInstanceType.Setattro = mbSWISetattr
	objects.SetTypeDescr(mbStreamWriterInstanceType, "write",
		objects.NewMethodDescr(mbStreamWriterInstanceType, "write", mbSWIWrite))
	objects.SetTypeDescr(mbStreamWriterInstanceType, "writelines",
		objects.NewMethodDescr(mbStreamWriterInstanceType, "writelines", mbSWIWritelines))
	objects.SetTypeDescr(mbStreamWriterInstanceType, "reset",
		objects.NewMethodDescr(mbStreamWriterInstanceType, "reset", mbSWIReset))
	objects.SetTypeDescr(mbStreamWriterInstanceType, "seek",
		objects.NewMethodDescr(mbStreamWriterInstanceType, "seek", mbSWISeek))
}

// MakeStreamWriterClass returns a Python callable that creates a StreamWriter
// instance bound to the given codec. Used by makeCodecInfo for the streamwriter field.
//
// CPython: Lib/codecs.py:349 StreamWriter
func MakeStreamWriterClass(ci *codecs.CodecInfo) objects.Object {
	return objects.NewBuiltinFunction("StreamWriter",
		func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
			if len(args) < 1 {
				return nil, fmt.Errorf("TypeError: StreamWriter() requires a stream argument")
			}
			errors := "strict"
			if len(args) >= 2 {
				if s, ok := args[1].(*objects.Unicode); ok {
					errors = s.Value()
				}
			}
			if v, ok := kwargs["errors"]; ok {
				if s, ok := v.(*objects.Unicode); ok {
					errors = s.Value()
				}
			}
			sw := &mbStreamWriterInstance{ci: ci, stream: args[0], errors: errors}
			sw.Header.Init(mbStreamWriterInstanceType)
			if ci.NewIncrementalEncoder != nil {
				sw.encodeFn = ci.NewIncrementalEncoder()
			}
			return sw, nil
		})
}

func mbSWIGetattr(o objects.Object, name objects.Object) (objects.Object, error) {
	sw, ok := o.(*mbStreamWriterInstance)
	if !ok {
		return nil, fmt.Errorf("TypeError: expected StreamWriter")
	}
	s, _ := objects.Str(name)
	switch s {
	case "errors":
		return objects.NewStr(sw.errors), nil
	case "stream":
		return sw.stream, nil
	}
	// Try type dict (methods) first, then proxy to stream.
	if v, err := objects.GenericGetAttr(o, name); err == nil {
		return v, nil
	}
	return objects.GetAttr(sw.stream, name)
}

func mbSWISetattr(o objects.Object, name objects.Object, value objects.Object) error {
	sw, ok := o.(*mbStreamWriterInstance)
	if !ok {
		return fmt.Errorf("TypeError: expected StreamWriter")
	}
	s, _ := objects.Str(name)
	if s == "errors" {
		if value == nil {
			return fmt.Errorf("AttributeError: cannot delete 'errors'")
		}
		sv, ok2 := value.(*objects.Unicode)
		if !ok2 {
			return fmt.Errorf("TypeError: errors must be str")
		}
		sw.errors = sv.Value()
		return nil
	}
	return fmt.Errorf("AttributeError: 'StreamWriter' object has no attribute '%s'", s)
}

// mbSWIWrite implements StreamWriter.write(object).
//
// CPython: Lib/codecs.py:366 StreamWriter.write
func mbSWIWrite(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: write() requires an argument")
	}
	sw, ok := args[0].(*mbStreamWriterInstance)
	if !ok {
		return nil, fmt.Errorf("TypeError: expected StreamWriter")
	}
	text, ok := args[1].(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: write() argument must be str, not %s", args[1].Type().Name)
	}
	var data []byte
	var err error
	if sw.encodeFn != nil {
		data, _, err = sw.encodeFn(text.Value(), sw.errors, false)
	} else {
		data, _, err = sw.ci.Encode(text.Value(), sw.errors)
	}
	if err != nil {
		return nil, err
	}
	_, err = objects.Call(
		mustAttr(sw.stream, "write"),
		objects.NewTuple([]objects.Object{objects.NewBytes(data)}),
		nil,
	)
	if err != nil {
		return nil, err
	}
	return objects.NewInt(int64(len(text.Value()))), nil
}

func mustAttr(o objects.Object, name string) objects.Object {
	v, _ := objects.GetAttr(o, objects.NewStr(name))
	return v
}

// asBytesSlice extracts the raw bytes from a bytes or bytearray object.
func asBytesSlice(o objects.Object) ([]byte, error) {
	switch v := o.(type) {
	case *objects.Bytes:
		return v.Bytes(), nil
	case *objects.ByteArray:
		return v.Bytes(), nil
	}
	return nil, fmt.Errorf("TypeError: expected bytes-like object, got %s", o.Type().Name)
}

// mbSWIWritelines implements StreamWriter.writelines(list).
//
// CPython: Lib/codecs.py:374 StreamWriter.writelines
func mbSWIWritelines(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: writelines() requires an argument")
	}
	lst, err := objects.SequenceList(args[1])
	if err != nil {
		return nil, err
	}
	for i := 0; i < lst.Len(); i++ {
		item := lst.Item(i)
		if _, err := mbSWIWrite([]objects.Object{args[0], item}, kwargs); err != nil {
			return nil, err
		}
	}
	return objects.None(), nil
}

// mbSWIReset implements StreamWriter.reset().
//
// CPython: Lib/codecs.py:379 StreamWriter.reset (no-op base)
func mbSWIReset(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) >= 1 {
		if sw, ok := args[0].(*mbStreamWriterInstance); ok && sw.ci.NewIncrementalEncoder != nil {
			sw.encodeFn = sw.ci.NewIncrementalEncoder()
		}
	}
	return objects.None(), nil
}

// mbSWISeek implements StreamWriter.seek().
//
// CPython: Lib/codecs.py:384 StreamWriter.seek
func mbSWISeek(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: seek() requires an argument")
	}
	sw, ok := args[0].(*mbStreamWriterInstance)
	if !ok {
		return nil, fmt.Errorf("TypeError: expected StreamWriter")
	}
	if sw.ci.NewIncrementalEncoder != nil {
		sw.encodeFn = sw.ci.NewIncrementalEncoder()
	}
	seekFn, err := objects.GetAttr(sw.stream, objects.NewStr("seek"))
	if err != nil {
		return nil, err
	}
	return objects.Call(seekFn, objects.NewTuple(args[1:]), nil)
}

// mbStreamReaderInstance is a Python object that wraps a readable stream
// and decodes bytes through a codec to Unicode strings.
//
// CPython: Modules/cjkcodecs/multibytecodec.c:891 MultibyteStreamReaderObject (instance)
type mbStreamReaderInstance struct {
	objects.Header
	ci       *codecs.CodecInfo
	stream   objects.Object
	errors   string
	buf      []byte
	charbuf  []rune                                     // excess chars decoded in a previous read-ahead
	decodeFn func(input []byte, errors string, final bool) (string, []byte, error) // per-instance stateful decoder
}

var mbStreamReaderInstanceType *objects.Type

func init() {
	mbStreamReaderInstanceType = objects.NewType(
		"StreamReader",
		[]*objects.Type{objects.ObjectType()},
	)
	mbStreamReaderInstanceType.Getattro = mbSRIGetattr
	mbStreamReaderInstanceType.Setattro = mbSRISetattr
	objects.SetTypeDescr(mbStreamReaderInstanceType, "read",
		objects.NewMethodDescr(mbStreamReaderInstanceType, "read", mbSRIRead))
	objects.SetTypeDescr(mbStreamReaderInstanceType, "readline",
		objects.NewMethodDescr(mbStreamReaderInstanceType, "readline", mbSRIReadline))
	objects.SetTypeDescr(mbStreamReaderInstanceType, "readlines",
		objects.NewMethodDescr(mbStreamReaderInstanceType, "readlines", mbSRIReadlines))
	objects.SetTypeDescr(mbStreamReaderInstanceType, "reset",
		objects.NewMethodDescr(mbStreamReaderInstanceType, "reset", mbSRIReset))
	objects.SetTypeDescr(mbStreamReaderInstanceType, "seek",
		objects.NewMethodDescr(mbStreamReaderInstanceType, "seek", mbSRISeek))
}

// MakeStreamReaderClass returns a Python callable that creates a StreamReader
// instance bound to the given codec.
//
// CPython: Lib/codecs.py:425 StreamReader
func MakeStreamReaderClass(ci *codecs.CodecInfo) objects.Object {
	return objects.NewBuiltinFunction("StreamReader",
		func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
			if len(args) < 1 {
				return nil, fmt.Errorf("TypeError: StreamReader() requires a stream argument")
			}
			errors := "strict"
			if len(args) >= 2 {
				if s, ok := args[1].(*objects.Unicode); ok {
					errors = s.Value()
				}
			}
			if v, ok := kwargs["errors"]; ok {
				if s, ok := v.(*objects.Unicode); ok {
					errors = s.Value()
				}
			}
			sr := &mbStreamReaderInstance{ci: ci, stream: args[0], errors: errors}
			if ci.NewIncrementalDecoder != nil {
				sr.decodeFn = ci.NewIncrementalDecoder()
			}
			sr.Header.Init(mbStreamReaderInstanceType)
			return sr, nil
		})
}

func mbSRIGetattr(o objects.Object, name objects.Object) (objects.Object, error) {
	sr, ok := o.(*mbStreamReaderInstance)
	if !ok {
		return nil, fmt.Errorf("TypeError: expected StreamReader")
	}
	s, _ := objects.Str(name)
	switch s {
	case "errors":
		return objects.NewStr(sr.errors), nil
	case "stream":
		return sr.stream, nil
	}
	// Try type dict (methods) first, then proxy to stream.
	if v, err := objects.GenericGetAttr(o, name); err == nil {
		return v, nil
	}
	return objects.GetAttr(sr.stream, name)
}

func mbSRISetattr(o objects.Object, name objects.Object, value objects.Object) error {
	sr, ok := o.(*mbStreamReaderInstance)
	if !ok {
		return fmt.Errorf("TypeError: expected StreamReader")
	}
	s, _ := objects.Str(name)
	if s == "errors" {
		if value == nil {
			return fmt.Errorf("AttributeError: cannot delete 'errors'")
		}
		sv, ok2 := value.(*objects.Unicode)
		if !ok2 {
			return fmt.Errorf("TypeError: errors must be str")
		}
		sr.errors = sv.Value()
		return nil
	}
	return fmt.Errorf("AttributeError: 'StreamReader' object has no attribute '%s'", s)
}

// mbSRIRead reads up to size Unicode chars from the stream.
// When size is -1 or None, reads until EOF.
//
// CPython: Lib/codecs.py:461 StreamReader.read
func mbSRIRead(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: read() requires self")
	}
	sr, ok := args[0].(*mbStreamReaderInstance)
	if !ok {
		return nil, fmt.Errorf("TypeError: expected StreamReader")
	}
	size := -1
	if len(args) >= 2 && args[1] != objects.None() {
		if n, ok := args[1].(*objects.Int); ok {
			n64, _ := n.Int64()
			size = int(n64)
		}
	}
	return mbSRIReadBytes(sr, size)
}

// mbSRIReadBytes reads from the stream and decodes up to size Unicode chars.
func mbSRIReadBytes(sr *mbStreamReaderInstance, size int) (objects.Object, error) {
	readFn, err := objects.GetAttr(sr.stream, objects.NewStr("read"))
	if err != nil {
		return nil, err
	}

	var buf []byte
	buf = append(buf, sr.buf...)
	sr.buf = nil

	if size < 0 {
		// Return any buffered chars first, then read all remaining bytes.
		var decoded []rune
		if len(sr.charbuf) > 0 {
			decoded = append(decoded, sr.charbuf...)
			sr.charbuf = nil
		}
		rawObj, err := objects.Call(readFn, objects.NewTuple(nil), nil)
		if err != nil {
			return nil, err
		}
		rawBytes, err := asBytesSlice(rawObj)
		if err != nil {
			return nil, err
		}
		buf = append(buf, rawBytes...)
		if len(buf) > 0 {
			var out string
			var remaining []byte
			if sr.decodeFn != nil {
				out, remaining, err = sr.decodeFn(buf, sr.errors, true)
			} else if sr.ci.IncrementalDecode != nil {
				out, remaining, err = sr.ci.IncrementalDecode(buf, sr.errors, true)
			} else {
				out, _, err = sr.ci.Decode(buf, sr.errors)
			}
			if err != nil {
				return nil, err
			}
			sr.buf = remaining
			decoded = append(decoded, []rune(out)...)
		}
		return objects.NewStr(string(decoded)), nil
	}

	// Drain the char buffer first.
	var decoded []rune
	if len(sr.charbuf) > 0 {
		decoded = append(decoded, sr.charbuf...)
		sr.charbuf = nil
	}

	// Read enough bytes to produce `size` Unicode chars. Read in chunks.
	for len(decoded) < size {
		// Read in chunks of max(1, size-len(decoded))*4 bytes for multibyte codecs.
		chunkSize := (size - len(decoded)) * 4
		if chunkSize < 4 {
			chunkSize = 4
		}
		rawObj, err := objects.Call(readFn,
			objects.NewTuple([]objects.Object{objects.NewInt(int64(chunkSize))}), nil)
		if err != nil {
			return nil, err
		}
		rawBytes, err := asBytesSlice(rawObj)
		if err != nil {
			return nil, err
		}
		if len(rawBytes) == 0 {
			// EOF: flush any remaining buffered bytes with final=true to trigger errors.
			// CPython: Lib/codecs.py:510 StreamReader.read flushes pending at EOF.
			if len(buf) > 0 {
				var out string
				var remaining []byte
				if sr.decodeFn != nil {
					out, remaining, err = sr.decodeFn(buf, sr.errors, true)
				} else if sr.ci.IncrementalDecode != nil {
					out, remaining, err = sr.ci.IncrementalDecode(buf, sr.errors, true)
				} else {
					out, _, err = sr.ci.Decode(buf, sr.errors)
				}
				if err != nil {
					return nil, err
				}
				decoded = append(decoded, []rune(out)...)
				buf = remaining
			}
			break
		}
		buf = append(buf, rawBytes...)
		var out string
		var remaining []byte
		if sr.decodeFn != nil {
			out, remaining, err = sr.decodeFn(buf, sr.errors, false)
		} else if sr.ci.IncrementalDecode != nil {
			out, remaining, err = sr.ci.IncrementalDecode(buf, sr.errors, false)
		} else {
			out, _, err = sr.ci.Decode(buf, sr.errors)
		}
		if err != nil {
			return nil, err
		}
		decoded = append(decoded, []rune(out)...)
		buf = remaining
	}

	sr.buf = buf

	// Trim to size; stash excess decoded chars for the next call.
	if len(decoded) > size {
		sr.charbuf = decoded[size:]
		decoded = decoded[:size]
	}
	return objects.NewStr(string(decoded)), nil
}

// mbSRIReadline reads one line from the stream.
//
// CPython: Lib/codecs.py:521 StreamReader.readline
func mbSRIReadline(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: readline() requires self")
	}
	// Delegate to the stream's readline and decode.
	sr, ok := args[0].(*mbStreamReaderInstance)
	if !ok {
		return nil, fmt.Errorf("TypeError: expected StreamReader")
	}
	readFn, err := objects.GetAttr(sr.stream, objects.NewStr("readline"))
	if err != nil {
		return nil, err
	}
	rawObj, err := objects.Call(readFn, objects.NewTuple(nil), nil)
	if err != nil {
		return nil, err
	}
	rawBytes, err := asBytesSlice(rawObj)
	if err != nil {
		return nil, err
	}
	if len(rawBytes) == 0 {
		return objects.NewStr(""), nil
	}
	buf := append(sr.buf, rawBytes...)
	sr.buf = nil
	var out string
	var remaining []byte
	var decErr error
	if sr.decodeFn != nil {
		out, remaining, decErr = sr.decodeFn(buf, sr.errors, true)
	} else if sr.ci.IncrementalDecode != nil {
		out, remaining, decErr = sr.ci.IncrementalDecode(buf, sr.errors, true)
	} else {
		out, _, decErr = sr.ci.Decode(buf, sr.errors)
	}
	if decErr != nil {
		return nil, decErr
	}
	sr.buf = remaining
	return objects.NewStr(out), nil
}

// mbSRIReadlines reads all lines from the stream.
//
// CPython: Lib/codecs.py:550 StreamReader.readlines
func mbSRIReadlines(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: readlines() requires self")
	}
	all, err := mbSRIRead([]objects.Object{args[0], objects.None()}, nil)
	if err != nil {
		return nil, err
	}
	text, _ := objects.Str(all)
	lines := splitLines(text)
	result := make([]objects.Object, len(lines))
	for i, l := range lines {
		result[i] = objects.NewStr(l)
	}
	return objects.NewList(result), nil
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i, r := range s {
		if r == '\n' {
			out = append(out, s[start:i+1])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

// mbSRIReset resets the StreamReader's pending byte buffer.
//
// CPython: Lib/codecs.py:559 StreamReader.reset
func mbSRIReset(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) >= 1 {
		if sr, ok := args[0].(*mbStreamReaderInstance); ok {
			sr.buf = nil
			sr.charbuf = nil
			if sr.ci.NewIncrementalDecoder != nil {
				sr.decodeFn = sr.ci.NewIncrementalDecoder()
			}
		}
	}
	return objects.None(), nil
}

// mbSRISeek implements StreamReader.seek().
//
// CPython: Lib/codecs.py:569 StreamReader.seek
func mbSRISeek(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: seek() requires an argument")
	}
	sr, ok := args[0].(*mbStreamReaderInstance)
	if !ok {
		return nil, fmt.Errorf("TypeError: expected StreamReader")
	}
	seekFn, err := objects.GetAttr(sr.stream, objects.NewStr("seek"))
	if err != nil {
		return nil, err
	}
	result, err := objects.Call(seekFn, objects.NewTuple(args[1:]), nil)
	if err != nil {
		return nil, err
	}
	sr.buf = nil
	sr.charbuf = nil
	if sr.ci.NewIncrementalDecoder != nil {
		sr.decodeFn = sr.ci.NewIncrementalDecoder()
	}
	return result, nil
}

// buildModule constructs the _multibytecodec module dict.
//
// CPython: Modules/cjkcodecs/multibytecodec.c:1450 _multibytecodec_exec
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_multibytecodec")
	d := m.Dict()
	if err := d.SetItem(objects.NewStr("MultibyteStreamReader"), MultibyteStreamReaderType); err != nil {
		return nil, err
	}
	if err := d.SetItem(objects.NewStr("MultibyteStreamWriter"), MultibyteStreamWriterType); err != nil {
		return nil, err
	}
	if err := d.SetItem(objects.NewStr("MultibyteIncrementalEncoder"), MultibyteIncrementalEncoderType); err != nil {
		return nil, err
	}
	if err := d.SetItem(objects.NewStr("MultibyteIncrementalDecoder"), MultibyteIncrementalDecoderType); err != nil {
		return nil, err
	}
	return m, nil
}
