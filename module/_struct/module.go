// Package _struct is the gopy port of CPython's _struct C extension.
// It provides functions and the Struct class for packing and unpacking
// binary data according to a format string.
//
// CPython: Modules/_struct.c

package _struct

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("_struct", buildModule)
}

// buildModule materializes the _struct module dict. Mirrors the
// PyInit__struct / struct_exec entry point.
//
// CPython: Modules/_struct.c:1834 struct_exec
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_struct")
	d := m.Dict()
	entries := []struct {
		name string
		val  objects.Object
	}{
		{"calcsize", objects.NewBuiltinFunction("calcsize", moduleCalcsize)},
		{"pack", objects.NewBuiltinFunction("pack", modulePack)},
		{"pack_into", objects.NewBuiltinFunction("pack_into", modulePackInto)},
		{"unpack", objects.NewBuiltinFunction("unpack", moduleUnpack)},
		{"unpack_from", objects.NewBuiltinFunction("unpack_from", moduleUnpackFrom)},
		{"iter_unpack", objects.NewBuiltinFunction("iter_unpack", moduleIterUnpack)},
		{"_clearcache", objects.NewBuiltinFunction("_clearcache", moduleClearcache)},
		{"Struct", StructType},
		{"error", objects.NewType("struct.error", []*objects.Type{objects.ObjectType()})},
		{"__doc__", objects.NewStr(structDoc)},
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Byte-order and alignment.
// ---------------------------------------------------------------------------

// byteOrder describes which endian and native-alignment policy a
// format string selects.
//
// CPython: Modules/_struct.c:98 formatcode
type byteOrder struct {
	order binary.ByteOrder
	// native is true for '@' and '=' (native byte order).
	native bool
	// aligned is true for '@' (native size and alignment).
	aligned bool
}

var (
	boNativeAligned = byteOrder{order: nativeByteOrder(), native: true, aligned: true}
	boNative        = byteOrder{order: nativeByteOrder(), native: true, aligned: false}
	boLittle        = byteOrder{order: binary.LittleEndian}
	boBig           = byteOrder{order: binary.BigEndian}
)

// nativeByteOrder returns binary.LittleEndian or binary.BigEndian
// based on runtime detection.
func nativeByteOrder() binary.ByteOrder {
	// A little-endian machine stores 0x0001 as [0x01, 0x00].
	v := uint16(0x0100)
	b := [2]byte{byte(v >> 8), byte(v)}
	if b[0] == 0x01 {
		return binary.BigEndian
	}
	return binary.LittleEndian
}

// parseByteOrder reads the optional byte-order prefix character from
// fmtStr and returns the selected byteOrder plus the remaining format string.
//
// CPython: Modules/_struct.c:474 whichtable
func parseByteOrder(fmtStr string) (byteOrder, string) {
	if len(fmtStr) == 0 {
		return boNativeAligned, fmtStr
	}
	switch fmtStr[0] {
	case '@':
		return boNativeAligned, fmtStr[1:]
	case '=':
		return boNative, fmtStr[1:]
	case '<':
		return boLittle, fmtStr[1:]
	case '>', '!':
		return boBig, fmtStr[1:]
	default:
		return boNativeAligned, fmtStr
	}
}

// ---------------------------------------------------------------------------
// Format parsing.
// ---------------------------------------------------------------------------

// fmtCode is one decoded entry from a format string after expanding
// repeat counts.
type fmtCode struct {
	code  byte
	count int // 1 unless a numeric repeat prefix was given
}

// parseFmt decodes a format string (without the byte-order prefix)
// into a slice of fmtCode. "4s" becomes one fmtCode{code:'s',count:4}.
// Spaces are ignored. 'x' is a pad byte.
//
// CPython: Modules/_struct.c:1124 calcsize
func parseFmt(fmtStr string) ([]fmtCode, error) {
	var codes []fmtCode
	i := 0
	for i < len(fmtStr) {
		c := fmtStr[i]
		if c == ' ' {
			i++
			continue
		}
		// Parse optional numeric repeat count.
		count := 1
		if c >= '0' && c <= '9' {
			count = 0
			for i < len(fmtStr) && fmtStr[i] >= '0' && fmtStr[i] <= '9' {
				count = count*10 + int(fmtStr[i]-'0')
				i++
			}
			if i >= len(fmtStr) {
				return nil, fmt.Errorf("struct.error: repeat count given without format character")
			}
			c = fmtStr[i]
		}
		switch c {
		case 'x', 'c', 'b', 'B', 'h', 'H', 'i', 'I', 'l', 'L', 'q', 'Q',
			'f', 'd', 's', 'p', '?', 'n', 'N', 'e':
			codes = append(codes, fmtCode{code: c, count: count})
		default:
			return nil, fmt.Errorf("struct.error: bad char ('%c') in struct format", c)
		}
		i++
	}
	return codes, nil
}

// codeSize returns the byte size of one instance of format code c
// (ignoring the repeat count). Native sizes match what CPython uses
// for '@' and '='; network/explicit formats use the standard widths.
//
// CPython: Modules/_struct.c:152 native_fmttable / Modules/_struct.c:240 standardfmttable
func codeSize(c byte) (int, error) {
	switch c {
	case 'x':
		return 1, nil
	case 'c':
		return 1, nil
	case 'b', 'B', '?':
		return 1, nil
	case 'h', 'H':
		return 2, nil
	case 'i', 'I', 'l', 'L', 'f':
		return 4, nil
	case 'q', 'Q', 'd':
		return 8, nil
	case 'n', 'N':
		// ssize_t / size_t — use 8 bytes on 64-bit platforms.
		return 8, nil
	case 'e':
		return 2, nil
	case 's', 'p':
		return 1, nil
	default:
		return 0, fmt.Errorf("struct.error: bad char in struct format")
	}
}

// calcSize returns the total byte size for the given pre-parsed codes.
//
// CPython: Modules/_struct.c:1124 s_struct_calcsize
func calcSize(codes []fmtCode) (int, error) {
	total := 0
	for _, fc := range codes {
		sz, err := codeSize(fc.code)
		if err != nil {
			return 0, err
		}
		if fc.code == 's' || fc.code == 'p' {
			total += fc.count
		} else {
			total += sz * fc.count
		}
	}
	return total, nil
}

// ---------------------------------------------------------------------------
// Pack helpers.
// ---------------------------------------------------------------------------

// packValue encodes a single Python object obj according to format code c
// into buf starting at offset off. Returns the new offset.
//
// CPython: Modules/_struct.c:690 s_pack_internal
func packValue(buf []byte, off int, bo byteOrder, c byte, count int, obj objects.Object) (int, error) {
	switch c {
	case 'x':
		// Pad bytes: write zeros, no corresponding value.
		for i := 0; i < count; i++ {
			buf[off] = 0
			off++
		}
		return off, nil

	case '?':
		for i := 0; i < count; i++ {
			b, err := objects.IsTruthy(obj)
			if err != nil {
				return 0, err
			}
			if b {
				buf[off] = 1
			} else {
				buf[off] = 0
			}
			off++
		}
		return off, nil

	case 'c':
		// Each value is a bytes object of length 1.
		for i := 0; i < count; i++ {
			b, ok := obj.(*objects.Bytes)
			if !ok || b.Len() != 1 {
				return 0, fmt.Errorf("struct.error: char format requires a bytes object of length 1")
			}
			buf[off] = b.Bytes()[0]
			off++
		}
		return off, nil

	case 'b':
		v, err := extractInt(obj)
		if err != nil {
			return 0, err
		}
		for i := 0; i < count; i++ {
			buf[off] = byte(int8(v))
			off++
		}
		return off, nil

	case 'B':
		v, err := extractInt(obj)
		if err != nil {
			return 0, err
		}
		for i := 0; i < count; i++ {
			buf[off] = byte(uint8(v))
			off++
		}
		return off, nil

	case 'h':
		v, err := extractInt(obj)
		if err != nil {
			return 0, err
		}
		for i := 0; i < count; i++ {
			bo.order.PutUint16(buf[off:], uint16(int16(v)))
			off += 2
		}
		return off, nil

	case 'H':
		v, err := extractInt(obj)
		if err != nil {
			return 0, err
		}
		for i := 0; i < count; i++ {
			bo.order.PutUint16(buf[off:], uint16(v))
			off += 2
		}
		return off, nil

	case 'i', 'l':
		v, err := extractInt(obj)
		if err != nil {
			return 0, err
		}
		for i := 0; i < count; i++ {
			bo.order.PutUint32(buf[off:], uint32(int32(v)))
			off += 4
		}
		return off, nil

	case 'I', 'L':
		v, err := extractInt(obj)
		if err != nil {
			return 0, err
		}
		for i := 0; i < count; i++ {
			bo.order.PutUint32(buf[off:], uint32(v))
			off += 4
		}
		return off, nil

	case 'q', 'n':
		v, err := extractInt(obj)
		if err != nil {
			return 0, err
		}
		for i := 0; i < count; i++ {
			bo.order.PutUint64(buf[off:], uint64(v))
			off += 8
		}
		return off, nil

	case 'Q', 'N':
		v, err := extractInt(obj)
		if err != nil {
			return 0, err
		}
		for i := 0; i < count; i++ {
			bo.order.PutUint64(buf[off:], uint64(v))
			off += 8
		}
		return off, nil

	case 'f':
		v, err := extractFloat(obj)
		if err != nil {
			return 0, err
		}
		for i := 0; i < count; i++ {
			bo.order.PutUint32(buf[off:], math.Float32bits(float32(v)))
			off += 4
		}
		return off, nil

	case 'd':
		v, err := extractFloat(obj)
		if err != nil {
			return 0, err
		}
		for i := 0; i < count; i++ {
			bo.order.PutUint64(buf[off:], math.Float64bits(v))
			off += 8
		}
		return off, nil

	case 'e':
		// Half-precision float.
		v, err := extractFloat(obj)
		if err != nil {
			return 0, err
		}
		for i := 0; i < count; i++ {
			bo.order.PutUint16(buf[off:], float32ToHalf(float32(v)))
			off += 2
		}
		return off, nil

	case 's':
		// 's' with count N writes N bytes from a single bytes/str object
		// (padded or truncated to exactly N bytes).
		bs, err := extractBytes(obj)
		if err != nil {
			return 0, err
		}
		n := count
		copy(buf[off:off+n], bs)
		if len(bs) < n {
			for j := len(bs); j < n; j++ {
				buf[off+j] = 0
			}
		}
		off += n
		return off, nil

	case 'p':
		// Pascal string: first byte is length (max 255), rest is payload.
		bs, err := extractBytes(obj)
		if err != nil {
			return 0, err
		}
		n := count
		maxLen := n - 1
		if maxLen < 0 {
			maxLen = 0
		}
		if maxLen > 255 {
			maxLen = 255
		}
		l := len(bs)
		if l > maxLen {
			l = maxLen
		}
		buf[off] = byte(l)
		copy(buf[off+1:off+n], bs[:l])
		for j := l + 1; j < n; j++ {
			buf[off+j] = 0
		}
		off += n
		return off, nil
	}
	return 0, fmt.Errorf("struct.error: bad char in struct format")
}

// extractInt extracts an int64 from a Python Int or Bool object.
//
// CPython: Modules/_struct.c:690 get_long / get_longlong
func extractInt(o objects.Object) (int64, error) {
	switch v := o.(type) {
	case *objects.Int:
		i, ok := v.Int64()
		if !ok {
			return 0, fmt.Errorf("struct.error: integer argument out of range")
		}
		return i, nil
	default:
		return 0, fmt.Errorf("struct.error: required argument is not an integer")
	}
}

// extractFloat extracts a float64 from a Python Float or Int object.
//
// CPython: Modules/_struct.c:690 get_double
func extractFloat(o objects.Object) (float64, error) {
	switch v := o.(type) {
	case *objects.Float:
		return v.Float64(), nil
	case *objects.Int:
		i, ok := v.Int64()
		if !ok {
			return 0, fmt.Errorf("struct.error: integer argument out of range for float")
		}
		return float64(i), nil
	default:
		return 0, fmt.Errorf("struct.error: required argument is not a float")
	}
}

// extractBytes returns the raw bytes from a Bytes or ByteArray object.
//
// CPython: Modules/_struct.c:690 (bytes extraction inline)
func extractBytes(o objects.Object) ([]byte, error) {
	switch v := o.(type) {
	case *objects.Bytes:
		return v.Bytes(), nil
	case *objects.ByteArray:
		return v.Bytes(), nil
	default:
		return nil, fmt.Errorf("struct.error: a bytes-like object is required, not '%T'", o)
	}
}

// float32ToHalf converts a float32 to IEEE 754 half-precision (16-bit).
//
// CPython: Modules/_struct.c:200 pack_halffloat
func float32ToHalf(f float32) uint16 {
	b := math.Float32bits(f)
	sign := uint16((b >> 31) & 0x1)
	exp := int((b >> 23) & 0xFF)
	mant := b & 0x7FFFFF
	if exp == 255 { // NaN or Inf
		if mant != 0 {
			return sign<<15 | 0x7C00 | 0x0200 // NaN
		}
		return sign<<15 | 0x7C00 // Inf
	}
	exp -= 127 // remove single bias
	if exp < -24 {
		return sign << 15 // underflow to zero
	}
	if exp < -14 {
		// Subnormal half.
		shift := uint(-1 - exp)
		mant = (mant | 0x800000) >> shift
		return sign<<15 | uint16(mant>>13)
	}
	if exp > 15 {
		return sign<<15 | 0x7C00 // overflow to Inf
	}
	halfExp := uint16(exp + 15)
	return sign<<15 | halfExp<<10 | uint16(mant>>13)
}

// halfToFloat32 converts an IEEE 754 half-precision (16-bit) to float32.
//
// CPython: Modules/_struct.c:220 unpack_halffloat
func halfToFloat32(h uint16) float32 {
	sign := uint32((h >> 15) & 0x1)
	exp := int((h >> 10) & 0x1F)
	mant := uint32(h & 0x3FF)
	var f uint32
	switch exp {
	case 0:
		if mant == 0 {
			f = sign << 31
		} else {
			// Subnormal.
			exp2 := -14
			for mant&0x400 == 0 {
				mant <<= 1
				exp2--
			}
			mant &= 0x3FF
			f = sign<<31 | uint32(exp2+127)<<23 | mant<<13
		}
	case 31:
		f = sign<<31 | 0x7F800000 | (mant << 13) // Inf / NaN
	default:
		f = sign<<31 | uint32(exp+127-15)<<23 | mant<<13
	}
	return math.Float32frombits(f)
}

// ---------------------------------------------------------------------------
// Pack driver: builds the byte buffer from a pre-compiled format.
// ---------------------------------------------------------------------------

// doPack encodes args into a fresh []byte using bo + codes.
//
// CPython: Modules/_struct.c:848 s_pack
func doPack(bo byteOrder, codes []fmtCode, args []objects.Object) ([]byte, error) {
	size, err := calcSize(codes)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, size)
	off := 0
	ai := 0 // index into args; 'x' does not consume an arg
	for _, fc := range codes {
		if fc.code == 'x' {
			// Pad bytes consume no arg.
			newOff, perr := packValue(buf, off, bo, 'x', fc.count, nil)
			if perr != nil {
				return nil, perr
			}
			off = newOff
			continue
		}
		if fc.code == 's' || fc.code == 'p' {
			// 's' / 'p' with a count is a single string arg.
			if ai >= len(args) {
				return nil, fmt.Errorf("struct.error: pack expected %d items for packing (got %d)", countArgs(codes), len(args))
			}
			newOff, perr := packValue(buf, off, bo, fc.code, fc.count, args[ai])
			if perr != nil {
				return nil, perr
			}
			ai++
			off = newOff
			continue
		}
		// All other codes: fc.count separate values.
		for i := 0; i < fc.count; i++ {
			if ai >= len(args) {
				return nil, fmt.Errorf("struct.error: pack expected %d items for packing (got %d)", countArgs(codes), len(args))
			}
			newOff, perr := packValue(buf, off, bo, fc.code, 1, args[ai])
			if perr != nil {
				return nil, perr
			}
			ai++
			off = newOff
		}
	}
	if ai != len(args) {
		return nil, fmt.Errorf("struct.error: pack expected %d items for packing (got %d)", ai, len(args))
	}
	return buf, nil
}

// countArgs returns how many Python objects a format codes slice consumes.
func countArgs(codes []fmtCode) int {
	n := 0
	for _, fc := range codes {
		if fc.code == 'x' {
			continue
		}
		if fc.code == 's' || fc.code == 'p' {
			n++
			continue
		}
		n += fc.count
	}
	return n
}

// ---------------------------------------------------------------------------
// Unpack helpers.
// ---------------------------------------------------------------------------

// unpackValue decodes one value from buf at offset off according to
// format code c (single instance). Returns (value, newOff).
//
// CPython: Modules/_struct.c:990 s_unpack_internal
func unpackValue(buf []byte, off int, bo byteOrder, c byte) (objects.Object, int, error) {
	switch c {
	case 'x':
		return nil, off + 1, nil
	case '?':
		v := buf[off] != 0
		return objects.NewBool(v), off + 1, nil
	case 'c':
		return objects.NewBytes(buf[off : off+1]), off + 1, nil
	case 'b':
		return objects.NewInt(int64(int8(buf[off]))), off + 1, nil
	case 'B':
		return objects.NewInt(int64(buf[off])), off + 1, nil
	case 'h':
		v := int16(bo.order.Uint16(buf[off:]))
		return objects.NewInt(int64(v)), off + 2, nil
	case 'H':
		v := bo.order.Uint16(buf[off:])
		return objects.NewInt(int64(v)), off + 2, nil
	case 'i', 'l':
		v := int32(bo.order.Uint32(buf[off:]))
		return objects.NewInt(int64(v)), off + 4, nil
	case 'I', 'L':
		v := bo.order.Uint32(buf[off:])
		return objects.NewInt(int64(v)), off + 4, nil
	case 'q', 'n':
		v := int64(bo.order.Uint64(buf[off:]))
		return objects.NewInt(v), off + 8, nil
	case 'Q', 'N':
		v := bo.order.Uint64(buf[off:])
		return objects.NewInt(int64(v)), off + 8, nil
	case 'f':
		bits := bo.order.Uint32(buf[off:])
		return objects.NewFloat(float64(math.Float32frombits(bits))), off + 4, nil
	case 'd':
		bits := bo.order.Uint64(buf[off:])
		return objects.NewFloat(math.Float64frombits(bits)), off + 8, nil
	case 'e':
		bits := bo.order.Uint16(buf[off:])
		return objects.NewFloat(float64(halfToFloat32(bits))), off + 2, nil
	}
	return nil, 0, fmt.Errorf("struct.error: bad char in struct format")
}

// doUnpack decodes the byte buffer buf (starting at startOff) into a
// tuple of Python objects using bo + codes.
//
// CPython: Modules/_struct.c:990 s_unpack_from
func doUnpack(bo byteOrder, codes []fmtCode, buf []byte, startOff int) (*objects.Tuple, error) {
	var items []objects.Object
	off := startOff
	for _, fc := range codes {
		switch fc.code {
		case 's':
			n := fc.count
			if off+n > len(buf) {
				return nil, fmt.Errorf("struct.error: unpack requires a buffer of %d bytes", off+n)
			}
			items = append(items, objects.NewBytes(buf[off:off+n]))
			off += n
		case 'p':
			n := fc.count
			if n < 1 || off+n > len(buf) {
				return nil, fmt.Errorf("struct.error: unpack requires a buffer of %d bytes", off+n)
			}
			l := int(buf[off])
			if l > n-1 {
				l = n - 1
			}
			items = append(items, objects.NewBytes(buf[off+1:off+1+l]))
			off += n
		case 'x':
			for i := 0; i < fc.count; i++ {
				if off >= len(buf) {
					return nil, fmt.Errorf("struct.error: unpack requires a buffer of %d bytes", off+1)
				}
				off++
			}
		default:
			sz, err := codeSize(fc.code)
			if err != nil {
				return nil, err
			}
			for i := 0; i < fc.count; i++ {
				if off+sz > len(buf) {
					return nil, fmt.Errorf("struct.error: unpack requires a buffer of %d bytes", off+sz)
				}
				v, newOff, err := unpackValue(buf, off, bo, fc.code)
				if err != nil {
					return nil, err
				}
				if v != nil {
					items = append(items, v)
				}
				off = newOff
			}
		}
	}
	return objects.NewTuple(items), nil
}

// ---------------------------------------------------------------------------
// Module-level functions.
// ---------------------------------------------------------------------------

// moduleCalcsize implements struct.calcsize(fmt).
//
// CPython: Modules/_struct.c:1124 s_struct_calcsize
func moduleCalcsize(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("struct.error: calcsize() takes exactly one argument (%d given)", len(args))
	}
	fmtStr, err := objects.Str(args[0])
	if err != nil {
		return nil, fmt.Errorf("struct.error: calcsize() argument 1 must be str, not %T", args[0])
	}
	bo, rest := parseByteOrder(fmtStr)
	_ = bo
	codes, err := parseFmt(rest)
	if err != nil {
		return nil, err
	}
	size, err := calcSize(codes)
	if err != nil {
		return nil, err
	}
	return objects.NewInt(int64(size)), nil
}

// modulePack implements struct.pack(fmt, *args).
//
// CPython: Modules/_struct.c:848 s_pack
func modulePack(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("struct.error: pack() takes at least one argument")
	}
	fmtStr, err := objects.Str(args[0])
	if err != nil {
		return nil, fmt.Errorf("struct.error: pack() argument 1 must be str, not %T", args[0])
	}
	bo, rest := parseByteOrder(fmtStr)
	codes, err := parseFmt(rest)
	if err != nil {
		return nil, err
	}
	buf, err := doPack(bo, codes, args[1:])
	if err != nil {
		return nil, err
	}
	return objects.NewBytes(buf), nil
}

// modulePackInto implements struct.pack_into(fmt, buffer, offset, *args).
//
// CPython: Modules/_struct.c:876 s_pack_into
func modulePackInto(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 3 {
		return nil, fmt.Errorf("struct.error: pack_into() takes at least 3 arguments")
	}
	fmtStr, err := objects.Str(args[0])
	if err != nil {
		return nil, fmt.Errorf("struct.error: pack_into() argument 1 must be str")
	}
	ba, ok := args[1].(*objects.ByteArray)
	if !ok {
		return nil, fmt.Errorf("struct.error: pack_into() argument 2 must be a bytearray")
	}
	off, err := extractInt(args[2])
	if err != nil {
		return nil, fmt.Errorf("struct.error: pack_into() argument 3 must be an integer")
	}
	offset := int(off)
	bo, rest := parseByteOrder(fmtStr)
	codes, err := parseFmt(rest)
	if err != nil {
		return nil, err
	}
	packed, err := doPack(bo, codes, args[3:])
	if err != nil {
		return nil, err
	}
	dst := ba.Bytes()
	if offset < 0 {
		offset = len(dst) + offset
	}
	if offset < 0 || offset+len(packed) > len(dst) {
		return nil, fmt.Errorf("struct.error: pack_into requires a buffer of at least %d bytes for packing %d bytes at offset %d", offset+len(packed), len(packed), offset)
	}
	copy(dst[offset:], packed)
	return objects.None(), nil
}

// moduleUnpack implements struct.unpack(fmt, buffer).
//
// CPython: Modules/_struct.c:990 s_unpack
func moduleUnpack(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("struct.error: unpack() takes exactly 2 arguments (%d given)", len(args))
	}
	fmtStr, err := objects.Str(args[0])
	if err != nil {
		return nil, fmt.Errorf("struct.error: unpack() argument 1 must be str")
	}
	buf, err := extractBytes(args[1])
	if err != nil {
		return nil, err
	}
	bo, rest := parseByteOrder(fmtStr)
	codes, err := parseFmt(rest)
	if err != nil {
		return nil, err
	}
	size, err := calcSize(codes)
	if err != nil {
		return nil, err
	}
	if len(buf) != size {
		return nil, fmt.Errorf("struct.error: unpack requires a buffer of %d bytes", size)
	}
	return doUnpack(bo, codes, buf, 0)
}

// moduleUnpackFrom implements struct.unpack_from(fmt, buffer, offset=0).
//
// CPython: Modules/_struct.c:1030 s_unpack_from
func moduleUnpackFrom(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("struct.error: unpack_from() takes at least 2 arguments")
	}
	fmtStr, err := objects.Str(args[0])
	if err != nil {
		return nil, fmt.Errorf("struct.error: unpack_from() argument 1 must be str")
	}
	buf, err := extractBytes(args[1])
	if err != nil {
		return nil, err
	}
	offset := 0
	if len(args) >= 3 {
		off, err := extractInt(args[2])
		if err != nil {
			return nil, fmt.Errorf("struct.error: offset must be an integer")
		}
		offset = int(off)
	} else if v, ok := kwargs["offset"]; ok {
		off, err := extractInt(v)
		if err != nil {
			return nil, fmt.Errorf("struct.error: offset must be an integer")
		}
		offset = int(off)
	}
	if offset < 0 {
		offset = len(buf) + offset
	}
	bo, rest := parseByteOrder(fmtStr)
	codes, err := parseFmt(rest)
	if err != nil {
		return nil, err
	}
	size, err := calcSize(codes)
	if err != nil {
		return nil, err
	}
	if offset < 0 || offset+size > len(buf) {
		return nil, fmt.Errorf("struct.error: unpack_from requires a buffer of at least %d bytes for unpacking %d bytes at offset %d (actual buffer size is %d)", offset+size, size, offset, len(buf))
	}
	return doUnpack(bo, codes, buf, offset)
}

// moduleClearcache implements struct._clearcache(). CPython caches
// compiled Struct objects in a module-level dict and exposes this
// helper to flush it; our parser is allocation-free per call, so the
// hook is a no-op. We still expose it because struct.py imports it
// unconditionally at module load.
//
// CPython: Modules/_struct.c:2541 _clearcache_impl
func moduleClearcache(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.None(), nil
}

// structDoc is the module-level docstring CPython attaches to _struct
// via PyModuleDef.m_doc. struct.py copies it through
// `from _struct import __doc__`.
//
// CPython: Modules/_struct.c:2569 module_doc
const structDoc = `Functions to convert between Python values and C structs.
Python bytes objects are used to hold the data representing the C struct
and also as format strings (explained below) to describe the layout of data
in the C struct.

The optional first format char indicates byte order, size and alignment:
  @: native order, size & alignment (default)
  =: native order, std. size & alignment
  <: little-endian, std. size & alignment
  >: big-endian, std. size & alignment
  !: same as >

The remaining chars indicate types of args and must match exactly;
these can be preceded by a decimal repeat count:
  x: pad byte (no data); c:char; b:signed byte; B:unsigned byte;
  ?: _Bool (requires C99; if not available, char is used instead)
  h:short; H:unsigned short; i:int; I:unsigned int;
  l:long; L:unsigned long; f:float; d:double; e:half-float.
Special cases (preceding decimal count indicates length):
  s:string (array of char); p: pascal string (with count byte).
Special cases (only available in native format):
  n:ssize_t; N:size_t;
  P:an integer type that is wide enough to hold a pointer.
Special case (not in native mode unless 'long long' in platform C):
  q:long long; Q:unsigned long long
Whitespace between formats is ignored.

The variable struct.error is an exception raised on errors.
`

// moduleIterUnpack implements struct.iter_unpack(fmt, buffer). Returns
// an iterator yielding successive tuples until the buffer is exhausted.
//
// CPython: Modules/_struct.c:1064 s_iter_unpack
func moduleIterUnpack(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("struct.error: iter_unpack() takes exactly 2 arguments (%d given)", len(args))
	}
	fmtStr, err := objects.Str(args[0])
	if err != nil {
		return nil, fmt.Errorf("struct.error: iter_unpack() argument 1 must be str")
	}
	buf, err := extractBytes(args[1])
	if err != nil {
		return nil, err
	}
	bo, rest := parseByteOrder(fmtStr)
	codes, err := parseFmt(rest)
	if err != nil {
		return nil, err
	}
	size, err := calcSize(codes)
	if err != nil {
		return nil, err
	}
	if size == 0 {
		return nil, fmt.Errorf("struct.error: cannot iteratively unpack with a struct of length 0")
	}
	it := &structIterUnpack{bo: bo, codes: codes, buf: buf, size: size}
	it.Init(structIterUnpackType)
	return it, nil
}

// ---------------------------------------------------------------------------
// iter_unpack iterator.
// ---------------------------------------------------------------------------

// structIterUnpack is the iterator object returned by iter_unpack.
//
// CPython: Modules/_struct.c:1050 unpackiterobject
type structIterUnpack struct {
	objects.Header
	bo    byteOrder
	codes []fmtCode
	buf   []byte
	size  int
	off   int
}

// structIterUnpackType is the type for iter_unpack iterators.
var structIterUnpackType = func() *objects.Type {
	t := objects.NewType("unpack_iterator", []*objects.Type{objects.ObjectType()})
	t.Iter = func(o objects.Object) (objects.Object, error) { return o, nil }
	t.IterNext = structIterUnpackNext
	objects.AddIterSlotWrappers(t)
	return t
}()

// structIterUnpackNext advances the iterator by one struct-size step.
//
// CPython: Modules/_struct.c:1064 unpackiter_iternext
func structIterUnpackNext(o objects.Object) (objects.Object, error) {
	it := o.(*structIterUnpack)
	if it.off+it.size > len(it.buf) {
		return nil, objects.ErrStopIteration
	}
	tup, err := doUnpack(it.bo, it.codes, it.buf, it.off)
	if err != nil {
		return nil, err
	}
	it.off += it.size
	return tup, nil
}

// ---------------------------------------------------------------------------
// Struct class.
// ---------------------------------------------------------------------------

// StructType is the Python-visible Struct class.
//
// CPython: Modules/_struct.c:1200 Struct_Type
var StructType = newStructType()

// Struct is the Go backing for a Struct instance.
//
// CPython: Modules/_struct.c:1144 PyStructObject
type Struct struct {
	objects.Header
	fmtStr string
	bo     byteOrder
	codes  []fmtCode
	size   int
}

func newStructType() *objects.Type {
	t := objects.NewType("Struct", []*objects.Type{objects.ObjectType()})
	t.TpNew = structNew
	t.Getattro = structGetattr
	t.Repr = structRepr
	t.Str = structRepr
	objects.SetTypeDescr(t, "pack", objects.NewMethodDescr(t, "pack", structPack))
	objects.SetTypeDescr(t, "unpack", objects.NewMethodDescr(t, "unpack", structUnpack))
	objects.SetTypeDescr(t, "pack_into", objects.NewMethodDescr(t, "pack_into", structPackInto))
	objects.SetTypeDescr(t, "unpack_from", objects.NewMethodDescr(t, "unpack_from", structUnpackFrom))
	objects.SetTypeDescr(t, "iter_unpack", objects.NewMethodDescr(t, "iter_unpack", structIterUnpackMethod))
	return t
}

// structNew implements Struct.__new__(cls, fmt).
//
// CPython: Modules/_struct.c:1165 s_new
func structNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("struct.error: Struct() takes exactly one argument")
	}
	fmtStr, err := objects.Str(args[0])
	if err != nil {
		return nil, fmt.Errorf("struct.error: Struct() argument must be str")
	}
	bo, rest := parseByteOrder(fmtStr)
	codes, err := parseFmt(rest)
	if err != nil {
		return nil, err
	}
	size, err := calcSize(codes)
	if err != nil {
		return nil, err
	}
	s := &Struct{fmtStr: fmtStr, bo: bo, codes: codes, size: size}
	s.Init(cls)
	return s, nil
}

// structRepr mirrors Struct.__repr__.
//
// CPython: Modules/_struct.c:1208 s_repr
func structRepr(o objects.Object) (string, error) {
	s := o.(*Struct)
	return fmt.Sprintf("Struct(%q)", s.fmtStr), nil
}

// structGetattr exposes .size and .format as read-only attributes.
//
// CPython: Modules/_struct.c:1226 s_sizeof
func structGetattr(o objects.Object, name objects.Object) (objects.Object, error) {
	s := o.(*Struct)
	n, err := objects.Str(name)
	if err != nil {
		return nil, err
	}
	switch n {
	case "size":
		return objects.NewInt(int64(s.size)), nil
	case "format":
		return objects.NewStr(s.fmtStr), nil
	}
	return objects.GenericGetAttr(o, name)
}

// structPack implements Struct.pack(self, *args). args[0] is self.
//
// CPython: Modules/_struct.c:848 s_pack
func structPack(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("struct.error: pack() takes at least 1 argument")
	}
	s := args[0].(*Struct)
	buf, err := doPack(s.bo, s.codes, args[1:])
	if err != nil {
		return nil, err
	}
	return objects.NewBytes(buf), nil
}

// structUnpack implements Struct.unpack(self, buffer).
//
// CPython: Modules/_struct.c:990 s_unpack
func structUnpack(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("struct.error: unpack() takes exactly 2 arguments")
	}
	s := args[0].(*Struct)
	buf, err := extractBytes(args[1])
	if err != nil {
		return nil, err
	}
	if len(buf) != s.size {
		return nil, fmt.Errorf("struct.error: unpack requires a buffer of %d bytes", s.size)
	}
	return doUnpack(s.bo, s.codes, buf, 0)
}

// structPackInto implements Struct.pack_into(self, buffer, offset, *args).
//
// CPython: Modules/_struct.c:876 s_pack_into
func structPackInto(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 3 {
		return nil, fmt.Errorf("struct.error: pack_into() takes at least 3 arguments")
	}
	s := args[0].(*Struct)
	ba, ok := args[1].(*objects.ByteArray)
	if !ok {
		return nil, fmt.Errorf("struct.error: pack_into() argument 2 must be a bytearray")
	}
	off, err := extractInt(args[2])
	if err != nil {
		return nil, fmt.Errorf("struct.error: offset must be an integer")
	}
	offset := int(off)
	packed, err := doPack(s.bo, s.codes, args[3:])
	if err != nil {
		return nil, err
	}
	dst := ba.Bytes()
	if offset < 0 {
		offset = len(dst) + offset
	}
	if offset < 0 || offset+len(packed) > len(dst) {
		return nil, fmt.Errorf("struct.error: pack_into requires a buffer of at least %d bytes", offset+len(packed))
	}
	copy(dst[offset:], packed)
	return objects.None(), nil
}

// structUnpackFrom implements Struct.unpack_from(self, buffer, offset=0).
//
// CPython: Modules/_struct.c:1030 s_unpack_from
func structUnpackFrom(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("struct.error: unpack_from() takes at least 2 arguments")
	}
	s := args[0].(*Struct)
	buf, err := extractBytes(args[1])
	if err != nil {
		return nil, err
	}
	offset := 0
	if len(args) >= 3 {
		off, err := extractInt(args[2])
		if err != nil {
			return nil, fmt.Errorf("struct.error: offset must be an integer")
		}
		offset = int(off)
	} else if v, ok := kwargs["offset"]; ok {
		off, err := extractInt(v)
		if err != nil {
			return nil, fmt.Errorf("struct.error: offset must be an integer")
		}
		offset = int(off)
	}
	if offset < 0 {
		offset = len(buf) + offset
	}
	if offset < 0 || offset+s.size > len(buf) {
		return nil, fmt.Errorf("struct.error: unpack_from requires a buffer of at least %d bytes", offset+s.size)
	}
	return doUnpack(s.bo, s.codes, buf, offset)
}

// structIterUnpackMethod implements Struct.iter_unpack(self, buffer).
//
// CPython: Modules/_struct.c:1064 s_iter_unpack
func structIterUnpackMethod(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("struct.error: iter_unpack() takes exactly 2 arguments")
	}
	s := args[0].(*Struct)
	buf, err := extractBytes(args[1])
	if err != nil {
		return nil, err
	}
	if s.size == 0 {
		return nil, fmt.Errorf("struct.error: cannot iteratively unpack with a struct of length 0")
	}
	it := &structIterUnpack{bo: s.bo, codes: s.codes, buf: buf, size: s.size}
	it.Init(structIterUnpackType)
	return it, nil
}
