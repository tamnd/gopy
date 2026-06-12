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
	"math/big"
	"unsafe"

	pyerrors "github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/vm"
)

// structError is the module's exception type, exposed as struct.error.
// Functions raise it through the "struct.error:" message prefix, which
// the VM maps back to this type via RegisterErrorPrefix.
//
// CPython: Modules/_struct.c:1844 struct_exec (PyModule_AddObjectRef "error")
var structError = pyerrors.NewExcType("struct.error", []*objects.Type{pyerrors.PyExc_Exception})

func init() {
	vm.RegisterErrorPrefix("struct.error:", structError)
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
		{"error", structError},
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
// based on runtime detection of the host's byte order.
func nativeByteOrder() binary.ByteOrder {
	// Write a known 16-bit value through a uint16 pointer and inspect the
	// first byte of its memory image: a little-endian host stores 0xABCD
	// as [0xCD, 0xAB], so buf[0] is the low byte.
	buf := [2]byte{}
	*(*uint16)(unsafe.Pointer(&buf[0])) = uint16(0xABCD)
	if buf[0] == 0xCD {
		return binary.LittleEndian
	}
	return binary.BigEndian
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
// Spaces are ignored. 'x' is a pad byte. native selects the native
// format table: 'n', 'N', and 'P' only exist there, so they raise a
// bad-char error in standard ('<', '>', '=', '!') mode, matching the
// fact that CPython's standardfmttable omits them.
//
// CPython: Modules/_struct.c:1124 calcsize
func parseFmt(fmtStr string, native bool) ([]fmtCode, error) {
	// A NUL anywhere in the format is rejected before parsing, matching
	// prepare_s's strlen vs byte-length comparison.
	//
	// CPython: Modules/_struct.c:1636 prepare_s
	for k := 0; k < len(fmtStr); k++ {
		if fmtStr[k] == 0 {
			return nil, fmt.Errorf("struct.error: embedded null character")
		}
	}
	var codes []fmtCode
	i := 0
	for i < len(fmtStr) {
		c := fmtStr[i]
		if c == ' ' {
			i++
			continue
		}
		// Parse optional numeric repeat count, rejecting any count that
		// overflows a signed word the same way prepare_s does.
		count := 1
		if c >= '0' && c <= '9' {
			count = 0
			for i < len(fmtStr) && fmtStr[i] >= '0' && fmtStr[i] <= '9' {
				d := int(fmtStr[i] - '0')
				if count >= math.MaxInt64/10 && (count > math.MaxInt64/10 || d > math.MaxInt64%10) {
					return nil, fmt.Errorf("struct.error: total struct size too long")
				}
				count = count*10 + d
				i++
			}
			if i >= len(fmtStr) {
				return nil, fmt.Errorf("struct.error: repeat count given without format specifier")
			}
			c = fmtStr[i]
		}
		switch c {
		case 'x', 'c', 'b', 'B', 'h', 'H', 'i', 'I', 'l', 'L', 'q', 'Q',
			'f', 'd', 's', 'p', '?', 'e', 'F', 'D':
			codes = append(codes, fmtCode{code: c, count: count})
		case 'n', 'N', 'P':
			// Native-only codes (ssize_t, size_t, void*). They have no
			// standard-size form, so reject them outside native mode.
			if !native {
				return nil, fmt.Errorf("struct.error: bad char in struct format")
			}
			codes = append(codes, fmtCode{code: c, count: count})
		default:
			return nil, fmt.Errorf("struct.error: bad char in struct format")
		}
		i++
	}
	return codes, nil
}

// codeSize returns the byte size of one instance of format code c
// (ignoring the repeat count). Native sizes ('@') match the underlying C
// type widths on a 64-bit platform; standard formats ('=', '<', '>',
// '!') use fixed widths. The two tables agree everywhere except 'l'/'L',
// which is sizeof(long) == 8 in native mode but a fixed 4 bytes in
// standard mode.
//
// CPython: Modules/_struct.c:152 native_table / Modules/_struct.c:240 lilendian_table
func codeSize(c byte, native bool) (int, error) {
	switch c {
	case 'x':
		return 1, nil
	case 'c':
		return 1, nil
	case 'b', 'B', '?':
		return 1, nil
	case 'h', 'H':
		return 2, nil
	case 'l', 'L':
		// sizeof(long): 8 bytes native on a 64-bit platform, 4 standard.
		if native {
			return 8, nil
		}
		return 4, nil
	case 'i', 'I', 'f':
		return 4, nil
	case 'q', 'Q', 'd':
		return 8, nil
	case 'n', 'N', 'P':
		// ssize_t / size_t / void* are pointer-width; 8 bytes on 64-bit.
		return 8, nil
	case 'e':
		return 2, nil
	case 'F':
		// complex float: two 32-bit floats.
		return 8, nil
	case 'D':
		// complex double: two 64-bit doubles.
		return 16, nil
	case 's', 'p':
		return 1, nil
	default:
		return 0, fmt.Errorf("struct.error: bad char in struct format")
	}
}

// codeAlign returns the natural alignment of format code c. In standard
// mode every entry aligns to 0 (no padding), matching CPython's
// standard tables. In native mode the alignment is _Alignof the
// underlying C type on a 64-bit platform.
//
// CPython: Modules/_struct.c:152 native_table (alignment column)
func codeAlign(c byte, native bool) int {
	if !native {
		return 0
	}
	switch c {
	case 'h', 'H', 'e':
		return 2
	case 'i', 'I', 'f', 'F':
		// 'F' (complex float) aligns to _Alignof(float) == 4.
		return 4
	case 'l', 'L', 'q', 'Q', 'n', 'N', 'P', 'd', 'D':
		// 'D' (complex double) aligns to _Alignof(double) == 8.
		return 8
	case '?':
		return 1
	default:
		// x, c, b, B, s, p align to 0 (no padding).
		return 0
	}
}

// alignOffset rounds size up to the alignment boundary of code c,
// mirroring CPython's align(). Codes with alignment 0 or 1 never pad.
//
// CPython: Modules/_struct.c:60 align
func alignOffset(size int, c byte, native bool) int {
	a := codeAlign(c, native)
	if a > 1 && size > 0 {
		extra := (a - 1) - (size-1)%a
		size += extra
	}
	return size
}

// calcSize returns the total byte size for the given pre-parsed codes,
// inserting native alignment padding between fields when native is set.
//
// CPython: Modules/_struct.c:1124 s_struct_calcsize / prepare_s
func calcSize(codes []fmtCode, native bool) (int, error) {
	size := 0
	for _, fc := range codes {
		sz, err := codeSize(fc.code, native)
		if err != nil {
			return 0, err
		}
		size = alignOffset(size, fc.code, native)
		// Reject any field whose contribution would push the running
		// total past the signed-word ceiling, matching prepare_s.
		//
		// CPython: Modules/_struct.c:1702 prepare_s overflow guard
		var add int
		if fc.code == 's' || fc.code == 'p' {
			add = fc.count
		} else {
			if sz != 0 && fc.count > (math.MaxInt64-size)/sz {
				return 0, fmt.Errorf("struct.error: total struct size too long")
			}
			add = sz * fc.count
		}
		if add > math.MaxInt64-size {
			return 0, fmt.Errorf("struct.error: total struct size too long")
		}
		size += add
	}
	return size, nil
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
		v, err := packInt(obj, c, 1, false)
		if err != nil {
			return 0, err
		}
		for i := 0; i < count; i++ {
			buf[off] = byte(v)
			off++
		}
		return off, nil

	case 'B':
		v, err := packInt(obj, c, 1, true)
		if err != nil {
			return 0, err
		}
		for i := 0; i < count; i++ {
			buf[off] = byte(v)
			off++
		}
		return off, nil

	case 'h':
		v, err := packInt(obj, c, 2, false)
		if err != nil {
			return 0, err
		}
		for i := 0; i < count; i++ {
			bo.order.PutUint16(buf[off:], uint16(v))
			off += 2
		}
		return off, nil

	case 'H':
		v, err := packInt(obj, c, 2, true)
		if err != nil {
			return 0, err
		}
		for i := 0; i < count; i++ {
			bo.order.PutUint16(buf[off:], uint16(v))
			off += 2
		}
		return off, nil

	case 'i', 'l':
		// Native 'l' is sizeof(long) == 8 bytes; standard 'l' is 4.
		size := 4
		if c == 'l' && bo.aligned {
			size = 8
		}
		v, err := packInt(obj, c, size, false)
		if err != nil {
			return 0, err
		}
		if size == 8 {
			for i := 0; i < count; i++ {
				bo.order.PutUint64(buf[off:], v)
				off += 8
			}
			return off, nil
		}
		for i := 0; i < count; i++ {
			bo.order.PutUint32(buf[off:], uint32(v))
			off += 4
		}
		return off, nil

	case 'I', 'L':
		size := 4
		if c == 'L' && bo.aligned {
			size = 8
		}
		v, err := packInt(obj, c, size, true)
		if err != nil {
			return 0, err
		}
		if size == 8 {
			for i := 0; i < count; i++ {
				bo.order.PutUint64(buf[off:], v)
				off += 8
			}
			return off, nil
		}
		for i := 0; i < count; i++ {
			bo.order.PutUint32(buf[off:], uint32(v))
			off += 4
		}
		return off, nil

	case 'q', 'n':
		v, err := packInt(obj, c, 8, false)
		if err != nil {
			return 0, err
		}
		for i := 0; i < count; i++ {
			bo.order.PutUint64(buf[off:], v)
			off += 8
		}
		return off, nil

	case 'Q', 'N', 'P':
		v, err := packInt(obj, c, 8, true)
		if err != nil {
			return 0, err
		}
		for i := 0; i < count; i++ {
			bo.order.PutUint64(buf[off:], v)
			off += 8
		}
		return off, nil

	case 'f':
		v, err := extractFloat(obj)
		if err != nil {
			return 0, err
		}
		// A finite double that rounds to an infinite float32 is too large
		// to pack. CPython: Objects/floatobject.c:2184 PyFloat_Pack4
		// (isinf(y) && !isinf(x)).
		y := float32(v)
		if math.IsInf(float64(y), 0) && !math.IsInf(v, 0) {
			return 0, fmt.Errorf("OverflowError: float too large to pack with f format")
		}
		for i := 0; i < count; i++ {
			bo.order.PutUint32(buf[off:], math.Float32bits(y))
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

	case 'F':
		// Complex float: real then imaginary as two 32-bit floats.
		//
		// CPython: Modules/_struct.c:787 np_float_complex /
		// Modules/_struct.c:1128 bp_float_complex
		re, im, err := extractComplex(obj)
		if err != nil {
			return 0, err
		}
		for i := 0; i < count; i++ {
			for _, part := range [2]float64{re, im} {
				y := float32(part)
				// Standard byte order packs through PyFloat_Pack4 and
				// rejects a finite value that rounds to infinity; native
				// memcpy just truncates.
				if !bo.native && math.IsInf(float64(y), 0) && !math.IsInf(part, 0) {
					return 0, fmt.Errorf("OverflowError: float too large to pack with f format")
				}
				bo.order.PutUint32(buf[off:], math.Float32bits(y))
				off += 4
			}
		}
		return off, nil

	case 'D':
		// Complex double: real then imaginary as two 64-bit doubles.
		//
		// CPython: Modules/_struct.c:803 np_double_complex /
		// Modules/_struct.c:1143 bp_double_complex
		re, im, err := extractComplex(obj)
		if err != nil {
			return 0, err
		}
		for i := 0; i < count; i++ {
			bo.order.PutUint64(buf[off:], math.Float64bits(re))
			bo.order.PutUint64(buf[off+8:], math.Float64bits(im))
			off += 16
		}
		return off, nil

	case 'e':
		// Half-precision float.
		v, err := extractFloat(obj)
		if err != nil {
			return 0, err
		}
		half, overflow := floatPack2(v)
		if overflow {
			return 0, fmt.Errorf("OverflowError: float too large to pack with e format")
		}
		for i := 0; i < count; i++ {
			bo.order.PutUint16(buf[off:], half)
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
		// A zero-count code ("0p") has size 0, so it writes nothing and the
		// length byte is never stored.
		//
		// CPython: Modules/_struct.c:838 s_pack_internal 'p' branch
		bs, err := extractBytes(obj)
		if err != nil {
			return 0, err
		}
		n := count
		l := len(bs)
		if n == 0 {
			l = 0
		} else if l > n-1 {
			l = n - 1
		}
		if l > 0 {
			copy(buf[off+1:off+n], bs[:l])
		}
		if l > 255 {
			l = 255
		}
		if n > 0 {
			buf[off] = byte(l)
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

// packOffset coerces the pack_into offset argument through __index__,
// matching the clinic Py_ssize_t conversion: None and float raise
// TypeError, an int too large for a machine word raises OverflowError.
//
// CPython: Modules/_struct.c:2320 Struct_pack_into (offset: Py_ssize_t)
func packOffset(o objects.Object) (int, error) {
	idx, err := objects.NumberIndex(o)
	if err != nil {
		return 0, err
	}
	iv, ok := idx.(*objects.Int)
	if !ok {
		return 0, fmt.Errorf("TypeError: '%s' object cannot be interpreted as an integer", o.Type().Name)
	}
	i, ok := iv.Int64()
	if !ok {
		return 0, fmt.Errorf("OverflowError: Python int too large to convert to C ssize_t")
	}
	return int(i), nil
}

// getPyLong coerces o to an integer, applying __index__ when o is not
// already an int, exactly as CPython does before any range check.
//
// CPython: Modules/_struct.c:295 get_pylong
func getPyLong(o objects.Object) (*big.Int, error) {
	if _, ok := o.(*objects.Int); !ok {
		if _, isBool := o.(*objects.Bool); !isBool {
			idx, err := objects.NumberIndex(o)
			if err != nil {
				return nil, fmt.Errorf("struct.error: required argument is not an integer")
			}
			o = idx
		}
	}
	switch v := o.(type) {
	case *objects.Int:
		return v.BigInt(), nil
	case *objects.Bool:
		if v == objects.True() {
			return big.NewInt(1), nil
		}
		return big.NewInt(0), nil
	default:
		return nil, fmt.Errorf("struct.error: required argument is not an integer")
	}
}

// packInt coerces obj to an integer, range-checks it for a field of the
// given byte size and signedness, and returns the value reduced to its
// low 64 bits (two's complement) so the caller's PutUintN truncation
// writes the correct bytes. Fields narrower than 8 bytes report the
// CPython _range_error bounds; 8-byte fields that overflow a machine
// word report "argument out of range", matching get_longlong.
//
// CPython: Modules/_struct.c:313 _range_error, get_long, get_longlong
func packInt(obj objects.Object, code byte, size int, unsigned bool) (uint64, error) {
	b, err := getPyLong(obj)
	if err != nil {
		return 0, err
	}
	one := big.NewInt(1)
	// ulargest = 2**(size*8) - 1, the largest value the field can hold.
	ulargest := new(big.Int).Sub(new(big.Int).Lsh(one, uint(size*8)), one)
	if size >= 8 {
		// 8-byte fields delegate to PyLong_AsLongLong / AsUnsignedLongLong,
		// which raise a plain "argument out of range" on overflow.
		if unsigned {
			if b.Sign() < 0 || b.Cmp(ulargest) > 0 {
				return 0, fmt.Errorf("struct.error: argument out of range")
			}
		} else {
			largest := new(big.Int).Rsh(ulargest, 1)
			smallest := new(big.Int).Not(largest)
			if b.Cmp(smallest) < 0 || b.Cmp(largest) > 0 {
				return 0, fmt.Errorf("struct.error: argument out of range")
			}
		}
		return bigLowBits64(b), nil
	}
	if unsigned {
		if b.Sign() < 0 || b.Cmp(ulargest) > 0 {
			return 0, fmt.Errorf("struct.error: '%c' format requires 0 <= number <= %s", code, ulargest.String())
		}
	} else {
		largest := new(big.Int).Rsh(ulargest, 1)
		smallest := new(big.Int).Not(largest)
		if b.Cmp(smallest) < 0 || b.Cmp(largest) > 0 {
			return 0, fmt.Errorf("struct.error: '%c' format requires %s <= number <= %s", code, smallest.String(), largest.String())
		}
	}
	return bigLowBits64(b), nil
}

// newUint wraps an unsigned 64-bit value as a Python int, preserving
// values above math.MaxInt64 that a signed cast would corrupt.
func newUint(v uint64) *objects.Int {
	return objects.NewIntFromBig(new(big.Int).SetUint64(v))
}

// bigLowBits64 returns the low 64 bits of b in two's-complement form, so
// a negative value yields the same bit pattern a C cast to uint64 would.
func bigLowBits64(b *big.Int) uint64 {
	mask := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 64), big.NewInt(1))
	return new(big.Int).And(b, mask).Uint64()
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

// extractComplex coerces o to a (real, imag) pair via the same dispatch
// as PyComplex_AsCComplex (a complex, then __complex__, then __float__).
// A failure becomes the struct-specific "required argument is not a
// complex" error.
//
// CPython: Modules/_struct.c:791 np_float_complex (PyComplex_AsCComplex)
func extractComplex(o objects.Object) (float64, float64, error) {
	re, im, err := objects.PyComplexAsCComplex(o)
	if err != nil {
		return 0, 0, fmt.Errorf("struct.error: required argument is not a complex")
	}
	return re, im, nil
}

// extractBytes returns the raw bytes from a Bytes or ByteArray object.
//
// CPython: Modules/_struct.c:690 (bytes extraction inline)
func extractBytes(o objects.Object) ([]byte, error) {
	// Accept any contiguous buffer (bytes, bytearray, memoryview, or a
	// buffer-protocol type such as array.array) via the shared unwrap.
	if b, ok := objects.AsBytesLike(o); ok {
		return b, nil
	}
	return nil, fmt.Errorf("TypeError: a bytes-like object is required, not '%s'", o.Type().Name)
}

// floatPack2 encodes x as an IEEE 754 half-precision (16-bit) value with
// round-to-even, returning overflow=true when a finite value is too large
// to represent. The result is the logical 16-bit pattern; the caller
// writes it in the requested byte order.
//
// CPython: Objects/floatobject.c:1993 PyFloat_Pack2
func floatPack2(x float64) (bits uint16, overflow bool) {
	var sign uint16
	var e int
	switch {
	case x == 0.0:
		if math.Signbit(x) {
			sign = 1
		}
		e = 0
		bits = 0
	case math.IsInf(x, 0):
		if x < 0.0 {
			sign = 1
		}
		e = 0x1f
		bits = 0
	case math.IsNaN(x):
		if math.Signbit(x) {
			sign = 1
		}
		e = 0x1f
		v := math.Float64bits(x)
		v &= 0xffc0000000000
		bits = uint16(v >> 42) // NaN's type and payload
		if bits == 0 {
			bits |= 1 << 9 // set qNaN if no payload
		}
	default:
		if x < 0.0 {
			sign = 1
			x = -x
		}
		f, exp := math.Frexp(x)
		// Normalize f to be in the range [1.0, 2.0).
		f *= 2.0
		e = exp - 1
		switch {
		case e >= 16:
			return 0, true
		case e < -25:
			// |x| < 2**-25. Underflow to zero.
			f = 0.0
			e = 0
		case e < -14:
			// |x| < 2**-14. Gradual underflow.
			f = math.Ldexp(f, 14+e)
			e = 0
		default:
			e += 15
			f -= 1.0 // Get rid of leading 1
		}
		f *= 1024.0 // 2**10
		bits = uint16(f)
		// Round to even.
		if (f-float64(bits) > 0.5) || ((f-float64(bits) == 0.5) && (bits%2 == 1)) {
			bits++
			if bits == 1024 {
				// The carry propagated out of a string of 10 1 bits.
				bits = 0
				e++
				if e == 31 {
					return 0, true
				}
			}
		}
	}
	bits |= uint16(e<<10) | (sign << 15)
	return bits, false
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
	size, err := calcSize(codes, bo.aligned)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, size)
	off := 0
	ai := 0 // index into args; 'x' does not consume an arg
	for _, fc := range codes {
		// Skip native alignment padding (already zero in buf).
		off = alignOffset(off, fc.code, bo.aligned)
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
		// Native 'l' is sizeof(long) == 8 bytes; standard 'l' is 4.
		if c == 'l' && bo.aligned {
			v := int64(bo.order.Uint64(buf[off:]))
			return objects.NewInt(v), off + 8, nil
		}
		v := int32(bo.order.Uint32(buf[off:]))
		return objects.NewInt(int64(v)), off + 4, nil
	case 'I', 'L':
		if c == 'L' && bo.aligned {
			v := bo.order.Uint64(buf[off:])
			return newUint(v), off + 8, nil
		}
		v := bo.order.Uint32(buf[off:])
		return objects.NewInt(int64(v)), off + 4, nil
	case 'q', 'n':
		v := int64(bo.order.Uint64(buf[off:]))
		return objects.NewInt(v), off + 8, nil
	case 'Q', 'N', 'P':
		v := bo.order.Uint64(buf[off:])
		return newUint(v), off + 8, nil
	case 'f':
		bits := bo.order.Uint32(buf[off:])
		return objects.NewFloat(float64(math.Float32frombits(bits))), off + 4, nil
	case 'd':
		bits := bo.order.Uint64(buf[off:])
		return objects.NewFloat(math.Float64frombits(bits)), off + 8, nil
	case 'e':
		bits := bo.order.Uint16(buf[off:])
		return objects.NewFloat(float64(halfToFloat32(bits))), off + 2, nil
	case 'F':
		// Complex float: two 32-bit floats (real, imag).
		//
		// CPython: Modules/_struct.c:1118 bu_float_complex
		re := float64(math.Float32frombits(bo.order.Uint32(buf[off:])))
		im := float64(math.Float32frombits(bo.order.Uint32(buf[off+4:])))
		return objects.NewComplex(re, im), off + 8, nil
	case 'D':
		// Complex double: two 64-bit doubles (real, imag).
		//
		// CPython: Modules/_struct.c:1442 bu_double_complex
		re := math.Float64frombits(bo.order.Uint64(buf[off:]))
		im := math.Float64frombits(bo.order.Uint64(buf[off+8:]))
		return objects.NewComplex(re, im), off + 16, nil
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
		// Skip native alignment padding between fields.
		off = alignOffset(off, fc.code, bo.aligned)
		switch fc.code {
		case 's':
			n := fc.count
			if off+n > len(buf) {
				return nil, fmt.Errorf("struct.error: unpack requires a buffer of %d bytes", off+n)
			}
			items = append(items, objects.NewBytes(buf[off:off+n]))
			off += n
		case 'p':
			// A zero-width Pascal field carries no length byte and unpacks
			// to an empty bytes object, matching pack("0p", ...) == b"".
			n := fc.count
			if off+n > len(buf) {
				return nil, fmt.Errorf("struct.error: unpack requires a buffer of %d bytes", off+n)
			}
			if n == 0 {
				items = append(items, objects.NewBytes(nil))
				break
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
			sz, err := codeSize(fc.code, bo.aligned)
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
	fmtStr, err := structFormatString(args[0])
	if err != nil {
		return nil, fmt.Errorf("struct.error: calcsize() argument 1 must be str, not %T", args[0])
	}
	bo, rest := parseByteOrder(fmtStr)
	codes, err := parseFmt(rest, bo.aligned)
	if err != nil {
		return nil, err
	}
	size, err := calcSize(codes, bo.aligned)
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
	fmtStr, err := structFormatString(args[0])
	if err != nil {
		return nil, fmt.Errorf("struct.error: pack() argument 1 must be str, not %T", args[0])
	}
	bo, rest := parseByteOrder(fmtStr)
	codes, err := parseFmt(rest, bo.aligned)
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
	fmtStr, err := structFormatString(args[0])
	if err != nil {
		return nil, fmt.Errorf("struct.error: pack_into() argument 1 must be str")
	}
	dst, ok := objects.AsWritableBuffer(args[1])
	if !ok {
		return nil, fmt.Errorf("TypeError: argument must be read-write bytes-like object, not %s", args[1].Type().Name)
	}
	offset, err := packOffset(args[2])
	if err != nil {
		return nil, err
	}
	bo, rest := parseByteOrder(fmtStr)
	codes, err := parseFmt(rest, bo.aligned)
	if err != nil {
		return nil, err
	}
	packed, err := doPack(bo, codes, args[3:])
	if err != nil {
		return nil, err
	}
	size := len(packed)
	bufLen := len(dst)
	// Negative offsets count from the end. The two guards below reject an
	// offset whose data would run off either side of the buffer; the
	// boundary check that follows is written as a subtraction so a huge
	// positive offset cannot overflow Py_ssize_t.
	//
	// CPython: Modules/_struct.c:1979 s_pack_into
	if offset < 0 {
		if offset+size > 0 {
			return nil, fmt.Errorf("struct.error: no space to pack %d bytes at offset %d", size, offset)
		}
		if offset+bufLen < 0 {
			return nil, fmt.Errorf("struct.error: offset %d out of range for %d-byte buffer", offset, bufLen)
		}
		offset += bufLen
	}
	if bufLen-offset < size {
		// CPython reports the required size as an unsigned sum so an
		// overflowing offset+size still prints a meaningful figure.
		atLeast := uint64(size) + uint64(offset)
		return nil, fmt.Errorf("struct.error: pack_into requires a buffer of at least %d bytes for packing %d bytes at offset %d (actual buffer size is %d)", atLeast, size, offset, bufLen)
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
	fmtStr, err := structFormatString(args[0])
	if err != nil {
		return nil, fmt.Errorf("struct.error: unpack() argument 1 must be str")
	}
	buf, err := extractBytes(args[1])
	if err != nil {
		return nil, err
	}
	bo, rest := parseByteOrder(fmtStr)
	codes, err := parseFmt(rest, bo.aligned)
	if err != nil {
		return nil, err
	}
	size, err := calcSize(codes, bo.aligned)
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
	fmtStr, err := structFormatString(args[0])
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
	bo, rest := parseByteOrder(fmtStr)
	codes, err := parseFmt(rest, bo.aligned)
	if err != nil {
		return nil, err
	}
	size, err := calcSize(codes, bo.aligned)
	if err != nil {
		return nil, err
	}
	bufLen := len(buf)
	// Mirror s_pack_into's boundary handling: clamp negative offsets to
	// the end and reject any offset whose read would run off the buffer,
	// using subtraction so a huge positive offset cannot overflow.
	//
	// CPython: Modules/_struct.c:2065 s_unpack_from
	if offset < 0 {
		if offset+size > 0 {
			return nil, fmt.Errorf("struct.error: not enough data to unpack %d bytes at offset %d", size, offset)
		}
		if offset+bufLen < 0 {
			return nil, fmt.Errorf("struct.error: offset %d out of range for %d-byte buffer", offset, bufLen)
		}
		offset += bufLen
	}
	if bufLen-offset < size {
		atLeast := uint64(size) + uint64(offset)
		return nil, fmt.Errorf("struct.error: unpack_from requires a buffer of at least %d bytes for unpacking %d bytes at offset %d (actual buffer size is %d)", atLeast, size, offset, bufLen)
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
	fmtStr, err := structFormatString(args[0])
	if err != nil {
		return nil, fmt.Errorf("struct.error: iter_unpack() argument 1 must be str")
	}
	buf, err := extractBytes(args[1])
	if err != nil {
		return nil, err
	}
	bo, rest := parseByteOrder(fmtStr)
	codes, err := parseFmt(rest, bo.aligned)
	if err != nil {
		return nil, err
	}
	size, err := calcSize(codes, bo.aligned)
	if err != nil {
		return nil, err
	}
	if size == 0 {
		return nil, fmt.Errorf("struct.error: cannot iteratively unpack with a struct of length 0")
	}
	if len(buf)%size != 0 {
		return nil, fmt.Errorf("struct.error: iterative unpacking requires a buffer of a multiple of %d bytes", size)
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
	objects.SetTypeDescr(t, "__length_hint__", objects.NewMethodDescrConv(t, "__length_hint__", objects.MethNoArgs, func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("TypeError: __length_hint__ takes no arguments")
		}
		it := args[0].(*structIterUnpack)
		return objects.NewInt(int64((len(it.buf) - it.off) / it.size)), nil
	}))
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

// Struct is the Go backing for a Struct instance. A struct allocated via
// __new__ but not yet run through __init__ stays !ready with size == -1,
// matching CPython's s_codes == NULL state.
//
// CPython: Modules/_struct.c:1144 PyStructObject
type Struct struct {
	objects.Header
	fmtStr string
	bo     byteOrder
	codes  []fmtCode
	size   int
	ready  bool
}

func newStructType() *objects.Type {
	t := objects.NewType("Struct", []*objects.Type{objects.ObjectType()})
	t.TpNew = structNew
	t.Getattro = structGetattr
	t.Repr = structRepr
	t.Str = structRepr
	objects.SetTypeDescr(t, "__new__", objects.NewBuiltinFunction("Struct.__new__", func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("TypeError: Struct.__new__(): not enough arguments")
		}
		cls, ok := args[0].(*objects.Type)
		if !ok {
			return nil, fmt.Errorf("TypeError: Struct.__new__(X): X is not a type object (%s)", args[0].Type().Name)
		}
		return structNew(cls, args[1:], kwargs)
	}))
	objects.SetTypeDescr(t, "__init__", objects.NewMethodDescr(t, "__init__", structInit))
	objects.SetTypeDescr(t, "__sizeof__", objects.NewMethodDescrConv(t, "__sizeof__", objects.MethNoArgs, structSizeof))
	objects.SetTypeDescr(t, "pack", objects.NewMethodDescr(t, "pack", structPack))
	objects.SetTypeDescr(t, "unpack", objects.NewMethodDescr(t, "unpack", structUnpack))
	objects.SetTypeDescr(t, "pack_into", objects.NewMethodDescr(t, "pack_into", structPackInto))
	objects.SetTypeDescr(t, "unpack_from", objects.NewMethodDescr(t, "unpack_from", structUnpackFrom))
	objects.SetTypeDescr(t, "iter_unpack", objects.NewMethodDescr(t, "iter_unpack", structIterUnpackMethod))
	return t
}

// ensureReady mirrors the ENSURE_STRUCT_IS_READY guard: methods that touch
// the parsed codes raise RuntimeError on a struct built via __new__ that has
// not been through __init__.
//
// CPython: Modules/_struct.c:1914 ENSURE_STRUCT_IS_READY
func (s *Struct) ensureReady() error {
	if !s.ready {
		return fmt.Errorf("RuntimeError: Struct object is not initialized")
	}
	return nil
}

// structNew allocates an uninitialized Struct. Like s_new it ignores the
// format argument (Struct.__init__ parses it), so Struct.__new__(Struct)
// yields a half-initialized object with size == -1.
//
// CPython: Modules/_struct.c:1784 s_new
func structNew(cls *objects.Type, _ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	s := &Struct{size: -1}
	s.Init(cls)
	return s, nil
}

// structInit parses the format and commits the result to self. It mirrors
// prepare_s, which only swaps in the new codes after a successful parse, so
// a failed re-initialization leaves the previous format intact.
//
// CPython: Modules/_struct.c:1739 Struct___init___impl / prepare_s
func structInit(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: descriptor '__init__' of 'Struct' object needs an argument")
	}
	self, ok := args[0].(*Struct)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__init__' requires a 'Struct' object but received a '%s'", args[0].Type().Name)
	}
	if len(args) != 2 {
		return nil, fmt.Errorf("struct.error: Struct() takes exactly 1 argument (%d given)", len(args)-1)
	}
	fmtStr, err := structFormatString(args[1])
	if err != nil {
		return nil, err
	}
	bo, rest := parseByteOrder(fmtStr)
	codes, perr := parseFmt(rest, bo.aligned)
	if perr != nil {
		return nil, perr
	}
	size, serr := calcSize(codes, bo.aligned)
	if serr != nil {
		return nil, serr
	}
	self.fmtStr = fmtStr
	self.bo = bo
	self.codes = codes
	self.size = size
	self.ready = true
	return objects.None(), nil
}

// structFormatString coerces the __init__ argument to a format string. A
// str is encoded as ASCII (a non-ASCII code point raises UnicodeEncodeError
// just like PyUnicode_AsASCIIString); a bytes/bytearray is taken verbatim.
//
// CPython: Modules/_struct.c:1746 Struct___init___impl
func structFormatString(o objects.Object) (string, error) {
	switch v := o.(type) {
	case *objects.Unicode:
		if !v.IsASCII() {
			return "", fmt.Errorf("UnicodeEncodeError: 'ascii' codec can't encode character in position 0: ordinal not in range(128)")
		}
		return v.Value(), nil
	case *objects.Bytes:
		return string(v.Bytes()), nil
	case *objects.ByteArray:
		return string(v.Bytes()), nil
	default:
		return "", fmt.Errorf("TypeError: Struct() argument 1 must be a str or bytes object, not %s", o.Type().Name)
	}
}

// structSizeof implements Struct.__sizeof__.
//
// CPython: Modules/_struct.c:2416 s_sizeof
func structSizeof(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	s, ok := args[0].(*Struct)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__sizeof__' requires a 'Struct' object but received a '%s'", args[0].Type().Name)
	}
	if err := s.ensureReady(); err != nil {
		return nil, err
	}
	// _PyObject_SIZE(Struct) + sizeof(formatcode) per code plus the
	// trailing sentinel. The concrete value is a CPython implementation
	// detail (the value test is skipped); the layout below keeps the
	// shape so __sizeof__ grows with the format.
	const structObjSize = 2*8 + 3*8 // 2n3P header CPython uses for PyStructObject
	const formatcodeSize = 8*3 + 8  // P3n0P entries
	size := structObjSize + formatcodeSize*(len(s.codes)+1)
	return objects.NewInt(int64(size)), nil
}

// structRepr mirrors Struct.__repr__.
//
// CPython: Modules/_struct.c:1208 s_repr
func structRepr(o objects.Object) (string, error) {
	s := o.(*Struct)
	if err := s.ensureReady(); err != nil {
		return "", err
	}
	r, err := objects.Repr(objects.NewStr(s.fmtStr))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Struct(%s)", r), nil
}

// structGetattr exposes .size and .format as read-only attributes.
//
// CPython: Modules/_struct.c:1226 s_sizeof
func structGetattr(o objects.Object, name objects.Object) (objects.Object, error) {
	s, ok := o.(*Struct)
	if !ok {
		return objects.GenericGetAttr(o, name)
	}
	n, err := objects.Str(name)
	if err != nil {
		return nil, err
	}
	switch n {
	case "size":
		// s_get_size has no readiness guard; an uninitialized struct
		// reports size -1.
		return objects.NewInt(int64(s.size)), nil
	case "format":
		if err := s.ensureReady(); err != nil {
			return nil, err
		}
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
	if err := s.ensureReady(); err != nil {
		return nil, err
	}
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
	if err := s.ensureReady(); err != nil {
		return nil, err
	}
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
	if err := s.ensureReady(); err != nil {
		return nil, err
	}
	dst, ok := objects.AsWritableBuffer(args[1])
	if !ok {
		return nil, fmt.Errorf("TypeError: argument must be read-write bytes-like object, not %s", args[1].Type().Name)
	}
	offset, err := packOffset(args[2])
	if err != nil {
		return nil, err
	}
	packed, err := doPack(s.bo, s.codes, args[3:])
	if err != nil {
		return nil, err
	}
	size := len(packed)
	bufLen := len(dst)
	// Negative offsets count from the end, with the same two guards as the
	// module-level pack_into: data must not run off either end, and the
	// boundary test below is a subtraction so a huge offset cannot wrap.
	//
	// CPython: Modules/_struct.c:2344 Struct_pack_into_impl
	if offset < 0 {
		if offset+size > 0 {
			return nil, fmt.Errorf("struct.error: no space to pack %d bytes at offset %d", size, offset)
		}
		if offset+bufLen < 0 {
			return nil, fmt.Errorf("struct.error: offset %d out of range for %d-byte buffer", offset, bufLen)
		}
		offset += bufLen
	}
	if bufLen-offset < size {
		atLeast := uint64(size) + uint64(offset)
		return nil, fmt.Errorf("struct.error: pack_into requires a buffer of at least %d bytes for packing %d bytes at offset %d (actual buffer size is %d)", atLeast, size, offset, bufLen)
	}
	copy(dst[offset:], packed)
	return objects.None(), nil
}

// structUnpackFrom implements Struct.unpack_from(self, buffer, offset=0).
//
// CPython: Modules/_struct.c:1030 s_unpack_from
func structUnpackFrom(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("struct.error: unpack_from() takes at least 1 argument")
	}
	s := args[0].(*Struct)
	if err := s.ensureReady(); err != nil {
		return nil, err
	}
	// buffer and offset are both positional-or-keyword in CPython, so
	// accept either as a keyword when not given positionally.
	bufObj := objects.Object(nil)
	if len(args) >= 2 {
		bufObj = args[1]
	} else if v, ok := kwargs["buffer"]; ok {
		bufObj = v
	} else {
		return nil, fmt.Errorf("struct.error: unpack_from() missing required argument 'buffer' (pos 1)")
	}
	buf, err := extractBytes(bufObj)
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
	if err := s.ensureReady(); err != nil {
		return nil, err
	}
	buf, err := extractBytes(args[1])
	if err != nil {
		return nil, err
	}
	if s.size == 0 {
		return nil, fmt.Errorf("struct.error: cannot iteratively unpack with a struct of length 0")
	}
	if len(buf)%s.size != 0 {
		return nil, fmt.Errorf("struct.error: iterative unpacking requires a buffer of a multiple of %d bytes", s.size)
	}
	it := &structIterUnpack{bo: s.bo, codes: s.codes, buf: buf, size: s.size}
	it.Init(structIterUnpackType)
	return it, nil
}
