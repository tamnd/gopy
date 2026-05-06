// Package marshal is the Go port of cpython/Python/marshal.c. It
// reads and writes the .pyc wire format that the import system uses
// to cache compiled code objects.
//
// v0.8 adds: TYPE_LONG (arbitrary-precision ints via *big.Int), FLAG_REF
// back-reference dedup in the decoder, TYPE_REF, and the full set of
// interned-string tags (TYPE_ASCII, TYPE_ASCII_INTERNED,
// TYPE_SHORT_ASCII_INTERNED, TYPE_INTERNED).
//
// CPython: Python/marshal.c
package marshal

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"

	"github.com/tamnd/gopy/objects"
)

// Version is the wire-format version Dump/Load speak. CPython 3.14
// emits version 5; the v0.5 skeleton supports the version-5 framing
// for the value subset listed in the package doc.
//
// CPython: Include/marshal.h Py_MARSHAL_VERSION
const Version = 5

// Type tags from Python/marshal.c. Single-byte discriminators on the
// wire. The high bit (0x80) is the FLAG_REF marker.
//
// CPython: Python/marshal.c TYPE_*
const (
	typeNull               = '0'
	typeNone               = 'N'
	typeFalse              = 'F'
	typeTrue               = 'T'
	typeStopIter           = 'S'
	typeEllipsis           = '.'
	typeInt                = 'i'
	typeInt64              = 'I'
	typeFloat              = 'f'
	typeBinaryFloat        = 'g'
	typeLong               = 'l'
	typeString             = 's'
	typeInterned           = 't'
	typeRef                = 'r'
	typeTuple              = '('
	typeSmallTuple         = ')'
	typeList               = '['
	typeDict               = '{'
	typeCode               = 'c'
	typeUnicode            = 'u'
	typeASCII              = 'a'
	typeASCIIInterned      = 'A'
	typeShortASCII         = 'z'
	typeShortASCIIInterned = 'Z'
	typeSet                = '<'
	typeFrozenset          = '>'
	typeComplex            = 'x'
	typeBinaryComplex      = 'y'
)

// flagRef is OR'd onto a type tag to signal that the object should be
// added to the back-reference table at the current index slot.
//
// CPython: Python/marshal.c FLAG_REF
const flagRef = 0x80

// ErrUnmarshallable matches CPython's WFERR_UNMARSHALLABLE.
//
// CPython: Python/marshal.c WFERR_UNMARSHALLABLE
var ErrUnmarshallable = errors.New("marshal: object cannot be marshaled")

// Dump writes v to w in the version-5 wire format.
//
// CPython: Python/marshal.c PyMarshal_WriteObjectToFile
func Dump(w io.Writer, v any) error {
	enc := encoder{w: w}
	return enc.write(v)
}

// Load reads one object from r in the version-5 wire format.
//
// CPython: Python/marshal.c PyMarshal_ReadObjectFromFile
func Load(r io.Reader) (any, error) {
	dec := decoder{r: byteReaderOf(r)}
	return dec.read()
}

type encoder struct {
	w io.Writer
}

func (e *encoder) writeByte(b byte) error {
	_, err := e.w.Write([]byte{b})
	return err
}

func (e *encoder) writeInt32(v int32) error {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], uint32(v))
	_, err := e.w.Write(buf[:])
	return err
}

func (e *encoder) writeInt64(v int64) error {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], uint64(v))
	_, err := e.w.Write(buf[:])
	return err
}

// write dispatches on the dynamic type of v. The accepted set is
// documented in the package doc; anything else is a marshal error.
//
// CPython: Python/marshal.c w_object
//
//nolint:gocyclo // mirrors CPython's per-type wire dispatch.
func (e *encoder) write(v any) error {
	switch x := v.(type) {
	case nil:
		return e.writeByte(typeNone)
	case bool:
		if x {
			return e.writeByte(typeTrue)
		}
		return e.writeByte(typeFalse)
	case int:
		return e.writeIntLike(int64(x))
	case int32:
		return e.writeIntLike(int64(x))
	case int64:
		return e.writeIntLike(x)
	case *big.Int:
		return writeLong(e.w, x, typeLong)
	case *objects.Code:
		return marshalCode(e, x)
	case *objects.Set:
		return e.writeSet(x)
	case map[any]any:
		return e.writeDict(x)
	case complex128:
		return e.writeComplex(x)
	case float64:
		if err := e.writeByte(typeBinaryFloat); err != nil {
			return err
		}
		return e.writeInt64(int64(math.Float64bits(x)))
	case string:
		return e.writeUnicode(x)
	case []byte:
		if err := e.writeByte(typeString); err != nil {
			return err
		}
		if err := e.writeInt32(int32(len(x))); err != nil {
			return err
		}
		_, err := e.w.Write(x)
		return err
	case []any:
		if len(x) < 256 {
			if err := e.writeByte(typeSmallTuple); err != nil {
				return err
			}
			if err := e.writeByte(byte(len(x))); err != nil {
				return err
			}
		} else {
			if err := e.writeByte(typeTuple); err != nil {
				return err
			}
			if err := e.writeInt32(int32(len(x))); err != nil {
				return err
			}
		}
		for _, item := range x {
			if err := e.write(item); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("%w: %T", ErrUnmarshallable, v)
}

// writeIntLike picks TYPE_INT for values that fit in int32, falling
// back to TYPE_INT64 for the rest.
//
// CPython: Python/marshal.c w_object PyLong arm
func (e *encoder) writeIntLike(v int64) error {
	if v >= math.MinInt32 && v <= math.MaxInt32 {
		if err := e.writeByte(typeInt); err != nil {
			return err
		}
		return e.writeInt32(int32(v))
	}
	if err := e.writeByte(typeInt64); err != nil {
		return err
	}
	return e.writeInt64(v)
}

// writeUnicode emits a string as TYPE_SHORT_ASCII when length < 256
// and the bytes are 7-bit, else TYPE_UNICODE with a 4-byte length.
//
// CPython: Python/marshal.c w_PyUnicode arm
func (e *encoder) writeUnicode(s string) error {
	if len(s) < 256 && isASCII(s) {
		if err := e.writeByte(typeShortASCII); err != nil {
			return err
		}
		if err := e.writeByte(byte(len(s))); err != nil {
			return err
		}
		_, err := e.w.Write([]byte(s))
		return err
	}
	if err := e.writeByte(typeUnicode); err != nil {
		return err
	}
	if err := e.writeInt32(int32(len(s))); err != nil {
		return err
	}
	_, err := e.w.Write([]byte(s))
	return err
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

type decoder struct {
	r    io.ByteReader
	refs []any
}

func byteReaderOf(r io.Reader) io.ByteReader {
	if br, ok := r.(io.ByteReader); ok {
		return br
	}
	return &byteReader{r: r}
}

type byteReader struct {
	r io.Reader
}

func (b *byteReader) ReadByte() (byte, error) {
	var buf [1]byte
	_, err := io.ReadFull(b.r, buf[:])
	return buf[0], err
}

func (d *decoder) readByte() (byte, error) {
	return d.r.ReadByte()
}

func (d *decoder) readN(n int) ([]byte, error) {
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		b, err := d.r.ReadByte()
		if err != nil {
			return nil, err
		}
		out[i] = b
	}
	return out, nil
}

func (d *decoder) readInt32() (int32, error) {
	buf, err := d.readN(4)
	if err != nil {
		return 0, err
	}
	return int32(binary.LittleEndian.Uint32(buf)), nil
}

func (d *decoder) readInt64() (int64, error) {
	buf, err := d.readN(8)
	if err != nil {
		return 0, err
	}
	return int64(binary.LittleEndian.Uint64(buf)), nil
}

// read decodes the next value. FLAG_REF objects are appended to the
// refs slice so later TYPE_REF tags can resolve them.
//
// CPython: Python/marshal.c r_object
func (d *decoder) read() (any, error) {
	tag, err := d.readByte()
	if err != nil {
		return nil, err
	}

	hasRef := (tag & flagRef) != 0
	tag &^= flagRef

	// Reserve a slot in the ref table before decoding so that
	// self-referential structures work correctly.
	refIdx := -1
	if hasRef {
		refIdx = len(d.refs)
		d.refs = append(d.refs, nil)
	}

	var v any
	v, err = d.decodeTag(tag)
	if err != nil {
		return nil, err
	}
	if hasRef {
		d.refs[refIdx] = v
	}
	return v, nil
}

//nolint:gocyclo // mirrors CPython's per-tag wire dispatch.
func (d *decoder) decodeTag(tag byte) (any, error) {
	switch tag {
	case typeNone, typeNull:
		return nil, nil
	case typeTrue:
		return true, nil
	case typeFalse:
		return false, nil
	case typeInt:
		v, err := d.readInt32()
		return int64(v), err
	case typeInt64:
		return d.readInt64()
	case typeLong:
		return readLong(d.r)
	case typeBinaryFloat:
		bits, err := d.readInt64()
		if err != nil {
			return nil, err
		}
		return math.Float64frombits(uint64(bits)), nil
	case typeShortASCII, typeShortASCIIInterned:
		n, err := d.readByte()
		if err != nil {
			return nil, err
		}
		buf, err := d.readN(int(n))
		if err != nil {
			return nil, err
		}
		return string(buf), nil
	case typeASCII, typeASCIIInterned, typeInterned, typeUnicode:
		n, err := d.readInt32()
		if err != nil {
			return nil, err
		}
		buf, err := d.readN(int(n))
		if err != nil {
			return nil, err
		}
		return string(buf), nil
	case typeString:
		n, err := d.readInt32()
		if err != nil {
			return nil, err
		}
		return d.readN(int(n))
	case typeSmallTuple:
		n, err := d.readByte()
		if err != nil {
			return nil, err
		}
		return d.readTuple(int(n))
	case typeTuple:
		n, err := d.readInt32()
		if err != nil {
			return nil, err
		}
		return d.readTuple(int(n))
	case typeCode:
		return unmarshalCode(d)
	case typeSet:
		return d.readSet(false)
	case typeFrozenset:
		return d.readSet(true)
	case typeDict:
		return d.readDict()
	case typeComplex:
		return d.readComplexText()
	case typeBinaryComplex:
		return d.readBinaryComplex()
	case typeRef:
		n, err := d.readInt32()
		if err != nil {
			return nil, err
		}
		if int(n) < 0 || int(n) >= len(d.refs) {
			return nil, fmt.Errorf("marshal: invalid ref index %d (table size %d)", n, len(d.refs))
		}
		return d.refs[n], nil
	}
	return nil, fmt.Errorf("marshal: unknown type tag %q (0x%02x)", rune(tag), tag)
}

func (d *decoder) readTuple(n int) ([]any, error) {
	out := make([]any, n)
	for i := 0; i < n; i++ {
		v, err := d.read()
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

// writeSet encodes a set or frozenset.
//
// CPython: Python/marshal.c w_object PySet_Type / PyFrozenSet_Type
func (e *encoder) writeSet(s *objects.Set) error {
	tag := byte(typeSet)
	if s.Type() == objects.FrozensetType {
		tag = typeFrozenset
	}
	if err := e.writeByte(tag); err != nil {
		return err
	}
	items := s.Items()
	if err := e.writeInt32(int32(len(items))); err != nil {
		return err
	}
	for _, item := range items {
		v, err := fromObject(item)
		if err != nil {
			return err
		}
		if err := e.write(v); err != nil {
			return err
		}
	}
	return nil
}

// writeDict encodes a map[any]any as TYPE_DICT with null terminator.
//
// CPython: Python/marshal.c w_object PyDict_Type
func (e *encoder) writeDict(m map[any]any) error {
	if err := e.writeByte(typeDict); err != nil {
		return err
	}
	for k, v := range m {
		if err := e.write(k); err != nil {
			return err
		}
		if err := e.write(v); err != nil {
			return err
		}
	}
	return e.writeByte(typeNull)
}

// writeComplex encodes a complex128 as TYPE_BINARY_COMPLEX.
//
// CPython: Python/marshal.c w_object PyComplex_Type (binary form)
func (e *encoder) writeComplex(c complex128) error {
	if err := e.writeByte(typeBinaryComplex); err != nil {
		return err
	}
	if err := e.writeInt64(int64(math.Float64bits(real(c)))); err != nil {
		return err
	}
	return e.writeInt64(int64(math.Float64bits(imag(c))))
}

// readSet decodes a TYPE_SET or TYPE_FROZENSET. If frozen is true the
// result is an immutable frozenset.
//
// CPython: Python/marshal.c r_object TYPE_SET / TYPE_FROZENSET
func (d *decoder) readSet(frozen bool) (any, error) {
	n, err := d.readInt32()
	if err != nil {
		return nil, err
	}
	items := make([]objects.Object, 0, int(n))
	for i := int32(0); i < n; i++ {
		v, err := d.read()
		if err != nil {
			return nil, err
		}
		obj, err := toObject(v)
		if err != nil {
			return nil, fmt.Errorf("marshal: set element: %w", err)
		}
		items = append(items, obj)
	}
	if frozen {
		return objects.NewFrozenset(items)
	}
	s := objects.NewSet()
	for _, item := range items {
		if err := s.Add(item); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// toObject converts a plain Go marshal value to an objects.Object for
// use as a set key or dict key. Only hashable types are accepted.
func toObject(v any) (objects.Object, error) {
	switch x := v.(type) {
	case nil:
		return objects.None(), nil
	case bool:
		return objects.NewBool(x), nil
	case int64:
		return objects.NewInt(x), nil
	case float64:
		return objects.NewFloat(x), nil
	case string:
		return objects.NewStr(x), nil
	case objects.Object:
		return x, nil
	}
	return nil, fmt.Errorf("marshal: cannot convert %T to Object", v)
}

// fromObject converts an objects.Object back to a plain Go marshal
// value so that set and frozenset items can pass through write().
func fromObject(obj objects.Object) (any, error) {
	switch x := obj.(type) {
	case *objects.Int:
		if v, ok := x.Int64(); ok {
			return v, nil
		}
		return x.BigInt(), nil
	case *objects.Float:
		return x.Float64(), nil
	case *objects.Bool:
		v, _ := x.Int64()
		return v != 0, nil
	case *objects.Set:
		return x, nil
	case *objects.Code:
		return x, nil
	}
	// None and str use unexported concrete types; dispatch via type slots.
	if obj.Type() == objects.NoneType() {
		return nil, nil
	}
	if obj.Type().Str != nil {
		s, err := obj.Type().Str(obj)
		if err == nil {
			return s, nil
		}
	}
	return nil, fmt.Errorf("marshal: cannot convert %T to plain value", obj)
}

// readDict decodes a TYPE_DICT (null-terminated key/value pairs).
//
// CPython: Python/marshal.c r_object TYPE_DICT
func (d *decoder) readDict() (any, error) {
	m := make(map[any]any)
	for {
		k, err := d.read()
		if err != nil {
			return nil, err
		}
		if k == nil {
			break
		}
		v, err := d.read()
		if err != nil {
			return nil, err
		}
		m[k] = v
	}
	return m, nil
}

// readComplexText decodes a TYPE_COMPLEX (text-encoded float pair).
// Each component is a length-prefixed ASCII float string.
//
// CPython: Python/marshal.c r_object TYPE_COMPLEX
func (d *decoder) readComplexText() (any, error) {
	rn, err := d.readByte()
	if err != nil {
		return nil, err
	}
	rbuf, err := d.readN(int(rn))
	if err != nil {
		return nil, err
	}
	var r float64
	if _, err := fmt.Sscanf(string(rbuf), "%g", &r); err != nil {
		return nil, fmt.Errorf("marshal: complex real part %q: %w", rbuf, err)
	}
	in, err := d.readByte()
	if err != nil {
		return nil, err
	}
	ibuf, err := d.readN(int(in))
	if err != nil {
		return nil, err
	}
	var im float64
	if _, err := fmt.Sscanf(string(ibuf), "%g", &im); err != nil {
		return nil, fmt.Errorf("marshal: complex imag part %q: %w", ibuf, err)
	}
	return complex(r, im), nil
}

// readBinaryComplex decodes a TYPE_BINARY_COMPLEX (two IEEE 754 doubles).
//
// CPython: Python/marshal.c r_object TYPE_BINARY_COMPLEX
func (d *decoder) readBinaryComplex() (any, error) {
	rb, err := d.readInt64()
	if err != nil {
		return nil, err
	}
	ib, err := d.readInt64()
	if err != nil {
		return nil, err
	}
	return complex(math.Float64frombits(uint64(rb)), math.Float64frombits(uint64(ib))), nil
}
