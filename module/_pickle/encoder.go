// Encoder primitives for the staged _pickle port. Phase 2 of P14.1
// covers the proto 5 binary write path for the atomic types: None,
// bool, int, float, bytes, str. Each Go writer corresponds to a
// CPython save_* function and emits the same bytes CPython does so
// the byte-equality fixture test in encoder_test.go can pin the
// output against `pickle.dumps(value, protocol=5)`.
//
// The pickler struct carries a single growing []byte plus a frame
// cursor. Framing follows Modules/_pickle.c verbatim: after PROTO
// fires, framing is enabled and the next _Pickler_Write reserves a
// 9-byte placeholder (FRAME + 8-byte length, deferred). On commit
// (after STOP), the frame body length is computed; if it cleared
// FRAME_SIZE_MIN (4) the FRAME header is patched in, otherwise the
// placeholder is moved out and only the raw bytes remain. That's why
// `pickle.dumps(None)` is 4 bytes (no FRAME) but `pickle.dumps(256)`
// gets a FRAME header (body crosses 4 bytes).
//
// CPython: Modules/_pickle.c:2115 save_none
// CPython: Modules/_pickle.c:2125 save_bool
// CPython: Modules/_pickle.c:2146 save_long
// CPython: Modules/_pickle.c:2312 save_float
// CPython: Modules/_pickle.c:2475 save_bytes
// CPython: Modules/_pickle.c:2781 save_unicode

package _pickle

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/big"

	"github.com/tamnd/gopy/objects"
)

// pickler holds the in-progress write buffer for a single dump cycle.
// The state corresponds to the fields of CPython's PicklerObject that
// are reachable from the proto 5 binary write path. Persistent ID,
// dispatch table, reducer override, and the file output stream live
// in later phases.
//
// CPython: Modules/_pickle.c:614 PicklerObject
type pickler struct {
	buf        []byte
	proto      int
	bin        bool
	framing    bool
	frameStart int
	// memo maps an object's identity to its memo index. The keys are
	// interface values whose dynamic type is a pointer, so map equality
	// degenerates into pointer identity. CPython's PyMemoTable uses raw
	// PyObject pointers for the same effect.
	//
	// CPython: Modules/_pickle.c:592 PyMemoTable
	memo map[objects.Object]int
}

// newPickler returns a pickler set for proto 5 binary output. The
// caller is responsible for writing the PROTO header.
//
// CPython: Modules/_pickle.c:1130 _Pickler_New
func newPickler(proto int) *pickler {
	p := &pickler{
		proto:      proto,
		bin:        proto > 0,
		frameStart: -1,
		memo:       make(map[objects.Object]int),
	}
	// Pre-size to the WRITE_BUF_SIZE CPython picks; small enough that
	// the typical short pickle does not grow the slice.
	p.buf = make([]byte, 0, writeBufSize)
	return p
}

// write appends raw bytes to the output buffer. If framing is enabled
// and no frame is currently open, an 8-byte placeholder is reserved
// before the bytes go in (later patched into FRAME + length, or
// elided if the frame is too small).
//
// CPython: Modules/_pickle.c:1081 _Pickler_Write
func (p *pickler) write(b []byte) {
	if p.framing && p.frameStart == -1 {
		p.frameStart = len(p.buf)
		p.buf = append(p.buf, make([]byte, frameHeaderSize)...)
	}
	p.buf = append(p.buf, b...)
}

// writeByte appends a single byte. Inlined since most opcode emits
// hit this path.
func (p *pickler) writeByte(b byte) {
	if p.framing && p.frameStart == -1 {
		p.frameStart = len(p.buf)
		p.buf = append(p.buf, make([]byte, frameHeaderSize)...)
	}
	p.buf = append(p.buf, b)
}

// commitFrame closes the current frame: if the body cleared
// FRAME_SIZE_MIN it patches in the FRAME opcode plus the 8-byte
// little-endian body length; otherwise it shifts the body bytes back
// over the placeholder so the wire form has no FRAME at all.
//
// CPython: Modules/_pickle.c:993 _Pickler_CommitFrame
func (p *pickler) commitFrame() {
	if !p.framing || p.frameStart == -1 {
		return
	}
	bodyLen := len(p.buf) - p.frameStart - frameHeaderSize
	if bodyLen >= frameSizeMin {
		p.buf[p.frameStart] = opFrame
		binary.LittleEndian.PutUint64(p.buf[p.frameStart+1:p.frameStart+1+8], uint64(bodyLen))
	} else {
		copy(p.buf[p.frameStart:], p.buf[p.frameStart+frameHeaderSize:])
		p.buf = p.buf[:len(p.buf)-frameHeaderSize]
	}
	p.frameStart = -1
}

// writeProtoHeader emits `PROTO <proto>` and (for proto >= 4) flips
// the framing bit so the next write reserves a frame placeholder.
//
// CPython: Modules/_pickle.c:4655 dump (PROTO emit)
func (p *pickler) writeProtoHeader() {
	p.buf = append(p.buf, opProto, byte(p.proto))
	if p.proto >= 4 {
		p.framing = true
	}
}

// writeStop emits STOP. CPython writes STOP before committing the
// frame, so STOP is the last byte inside the frame body.
//
// CPython: Modules/_pickle.c:4667 dump (STOP emit)
func (p *pickler) writeStop() {
	p.writeByte(opStop)
}

// ---------------------------------------------------------------------------
// Atom writers.
// ---------------------------------------------------------------------------

// saveNone writes the NONE opcode.
//
// CPython: Modules/_pickle.c:2115 save_none
func (p *pickler) saveNone() {
	p.writeByte(opNone)
}

// saveBool writes NEWTRUE / NEWFALSE on proto >= 2, falling back to
// the `I01\n` / `I00\n` legacy form on earlier protocols.
//
// CPython: Modules/_pickle.c:2125 save_bool
func (p *pickler) saveBool(v bool) {
	if p.proto >= 2 {
		if v {
			p.writeByte(opNewtrue)
		} else {
			p.writeByte(opNewfalse)
		}
		return
	}
	if v {
		p.write([]byte("I01\n"))
	} else {
		p.write([]byte("I00\n"))
	}
}

// saveLong picks the narrowest int opcode that fits the value. The
// 4-byte signed path (BININT / BININT1 / BININT2) covers all ints
// representable in 32 bits; the rest fall through to LONG1 (length
// fits in 1 byte) or LONG4 (length fits in 4 bytes) via the
// two's-complement little-endian byte encoding _PyLong_AsByteArray
// produces.
//
// CPython: Modules/_pickle.c:2146 save_long
func (p *pickler) saveLong(v *objects.Int) error {
	val, fitsInt64 := v.Int64()
	if fitsInt64 && val >= -0x80000000 && val <= 0x7fffffff {
		return p.saveLongInt32(int32(val))
	}
	return p.saveLongBig(v.BigInt())
}

// saveLongInt32 handles the int32 fast path: BININT for the full
// range, BININT2 if the high bytes are zero, BININT1 if even the
// second byte is zero.
//
// CPython: Modules/_pickle.c:2170 save_long fast-path (4-byte signed)
func (p *pickler) saveLongInt32(v int32) error {
	if !p.bin {
		// proto 0: INT opcode + decimal repr + '\n'.
		s := fmt.Sprintf("%c%d\n", opInt, v)
		p.write([]byte(s))
		return nil
	}
	var data [5]byte
	data[1] = byte(v & 0xff)
	data[2] = byte((v >> 8) & 0xff)
	data[3] = byte((v >> 16) & 0xff)
	data[4] = byte((v >> 24) & 0xff)
	switch {
	case data[4] != 0 || data[3] != 0:
		data[0] = opBinint
		p.write(data[:5])
	case data[2] != 0:
		data[0] = opBinint2
		p.write(data[:3])
	default:
		data[0] = opBinint1
		p.write(data[:2])
	}
	return nil
}

// saveLongBig handles values outside the int32 range via LONG1 /
// LONG4. The wire payload is the value as a little-endian, two's
// complement byte sequence, sized to be the minimum that preserves
// sign. Matches CPython's call to _PyLong_AsByteArray(...,
// little_endian=1, is_signed=1) followed by the optional trailing
// 0xff trim for negative values.
//
// CPython: Modules/_pickle.c:2200 save_long proto >= 2 path
func (p *pickler) saveLongBig(b *big.Int) error {
	if b.Sign() == 0 {
		// LONG1 with empty payload.
		p.write([]byte{opLong1, 0})
		return nil
	}
	payload := twosComplementBytesLE(b)
	switch {
	case len(payload) < 256:
		header := [2]byte{opLong1, byte(len(payload))}
		p.write(header[:])
	case len(payload) <= 0x7fffffff:
		var header [5]byte
		header[0] = opLong4
		binary.LittleEndian.PutUint32(header[1:], uint32(len(payload)))
		p.write(header[:])
	default:
		return errors.New("OverflowError: int too large to pickle")
	}
	p.write(payload)
	return nil
}

// twosComplementBytesLE returns the smallest little-endian two's
// complement byte slice that encodes b. Mirrors the
// _PyLong_AsByteArray + trailing-0xff trim CPython runs in
// save_long.
//
// CPython: Modules/_pickle.c:2245 save_long _PyLong_AsByteArray + trim
func twosComplementBytesLE(b *big.Int) []byte {
	// Number of bits in |b|.
	bits := b.BitLen()
	// We always need an extra byte for the sign bit; CPython picks
	// (nbits >> 3) + 1 bytes upfront and trims afterward.
	nbytes := bits/8 + 1
	out := make([]byte, nbytes)
	if b.Sign() >= 0 {
		// Big-endian magnitude into the tail, then reverse to LE.
		mag := b.Bytes()
		copy(out[nbytes-len(mag):], mag)
		reverseBytes(out)
	} else {
		// Two's complement of |b|: take 256^nbytes - |b|, then dump.
		mod := new(big.Int).Lsh(big.NewInt(1), uint(nbytes*8))
		tc := new(big.Int).Add(mod, b)
		mag := tc.Bytes()
		copy(out[nbytes-len(mag):], mag)
		reverseBytes(out)
	}
	// Trim a trailing 0xff for negative values when the next byte
	// already has the sign bit set (the MSB is then redundant).
	if b.Sign() < 0 && len(out) > 1 && out[len(out)-1] == 0xff && out[len(out)-2]&0x80 != 0 {
		out = out[:len(out)-1]
	}
	return out
}

func reverseBytes(b []byte) {
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
}

// saveFloat emits BINFLOAT plus 8 big-endian IEEE 754 bytes.
//
// CPython: Modules/_pickle.c:2312 save_float
func (p *pickler) saveFloat(v float64) {
	if !p.bin {
		// proto 0: FLOAT opcode + repr + '\n'.
		s := fmt.Sprintf("%c%s\n", opFloat, floatRepr(v))
		p.write([]byte(s))
		return
	}
	var data [9]byte
	data[0] = opBinfloat
	bits := math.Float64bits(v)
	binary.BigEndian.PutUint64(data[1:], bits)
	p.write(data[:])
}

func floatRepr(v float64) string {
	// CPython uses PyOS_double_to_string with Py_DTSF_ADD_DOT_0, which
	// for an integer-valued float appends ".0". Go's strconv.FormatFloat
	// does not, so we mimic the CPython rule: if no '.' or 'e' is in
	// the output, add ".0".
	s := fmt.Sprintf("%g", v)
	for i := 0; i < len(s); i++ {
		if s[i] == '.' || s[i] == 'e' || s[i] == 'n' || s[i] == 'i' {
			return s
		}
	}
	return s + ".0"
}

// saveBytes emits SHORT_BINBYTES / BINBYTES / BINBYTES8 plus the
// payload. Proto 5 unconditionally uses one of these three. CPython
// also emits MEMOIZE after the payload so callers reusing the same
// bytes object hit BINGET, the memoize step here lives in the caller
// (saveAtom) so we keep the writer narrowly focused.
//
// CPython: Modules/_pickle.c:2475 save_bytes
func (p *pickler) saveBytes(b []byte) error {
	size := len(b)
	switch {
	case size <= 0xff:
		header := [2]byte{opShortBinbytes, byte(size)}
		p.write(header[:])
	case size <= 0xffffffff:
		var header [5]byte
		header[0] = opBinbytes
		binary.LittleEndian.PutUint32(header[1:], uint32(size))
		p.write(header[:])
	default:
		var header [9]byte
		header[0] = opBinbytes8
		binary.LittleEndian.PutUint64(header[1:], uint64(size))
		p.write(header[:])
	}
	p.write(b)
	return nil
}

// saveUnicode emits SHORT_BINUNICODE / BINUNICODE / BINUNICODE8 plus
// the UTF-8 encoded payload.
//
// CPython: Modules/_pickle.c:2724 write_unicode_binary
func (p *pickler) saveUnicode(s string) error {
	data := []byte(s)
	size := len(data)
	switch {
	case size <= 0xff:
		header := [2]byte{opShortBinunicode, byte(size)}
		p.write(header[:])
	case size <= 0xffffffff:
		var header [5]byte
		header[0] = opBinunicode
		binary.LittleEndian.PutUint32(header[1:], uint32(size))
		p.write(header[:])
	default:
		var header [9]byte
		header[0] = opBinunicode8
		binary.LittleEndian.PutUint64(header[1:], uint64(size))
		p.write(header[:])
	}
	p.write(data)
	return nil
}

// memoPut records obj at the next available memo index and emits the
// matching memo opcode. Proto >= 4 uses the implicit MEMOIZE opcode;
// earlier protocols emit BINPUT / LONG_BINPUT with an explicit index.
//
// CPython: Modules/_pickle.c:1802 memo_put
func (p *pickler) memoPut(obj objects.Object) error {
	idx := len(p.memo)
	p.memo[obj] = idx
	if p.proto >= 4 {
		p.writeByte(opMemoize)
		return nil
	}
	if !p.bin {
		p.write([]byte(fmt.Sprintf("%c%d\n", opPut, idx)))
		return nil
	}
	if idx < 256 {
		p.write([]byte{opBinput, byte(idx)})
		return nil
	}
	if uint64(idx) <= 0xffffffff {
		var hdr [5]byte
		hdr[0] = opLongBinput
		binary.LittleEndian.PutUint32(hdr[1:], uint32(idx))
		p.write(hdr[:])
		return nil
	}
	return errors.New("PicklingError: memo id too large for LONG_BINPUT")
}

// memoGet emits a BINGET / LONG_BINGET for an object the memo has
// already recorded. Used when save() encounters a non-atom it has
// already pickled in this stream so it can re-use the prior position
// instead of re-serializing.
//
// CPython: Modules/_pickle.c:1754 memo_get
func (p *pickler) memoGet(obj objects.Object) error {
	idx, ok := p.memo[obj]
	if !ok {
		return errors.New("PicklingError: object missing from memo")
	}
	if !p.bin {
		p.write([]byte(fmt.Sprintf("%c%d\n", opGet, idx)))
		return nil
	}
	if idx < 256 {
		p.write([]byte{opBinget, byte(idx)})
		return nil
	}
	if uint64(idx) <= 0xffffffff {
		var hdr [5]byte
		hdr[0] = opLongBinget
		binary.LittleEndian.PutUint32(hdr[1:], uint32(idx))
		p.write(hdr[:])
		return nil
	}
	return errors.New("PicklingError: memo id too large for LONG_BINGET")
}

// ---------------------------------------------------------------------------
// Dispatch.
// ---------------------------------------------------------------------------

// save is the central dispatch. Atoms (None / bool / int / float)
// are never memoized; CPython skips the memo lookup for them entirely
// because they're cheap to re-encode and BINGET would be at best
// break-even. Everything else goes through the memo: a hit emits
// BINGET, a miss runs the per-type save_* and then memo_put.
//
// Bool subclasses Int in gopy (matching CPython), so the *Bool branch
// must come before the *Int branch in the type switch.
//
// CPython: Modules/_pickle.c:4401 save
func (p *pickler) save(obj objects.Object) error {
	if obj == nil {
		return errors.New("PicklingError: nil object")
	}
	// Atoms first, never memoized.
	if objects.IsNone(obj) {
		p.saveNone()
		return nil
	}
	if obj == objects.True() {
		p.saveBool(true)
		return nil
	}
	if obj == objects.False() {
		p.saveBool(false)
		return nil
	}
	if _, isBool := obj.(*objects.Bool); !isBool {
		if i, ok := obj.(*objects.Int); ok {
			return p.saveLong(i)
		}
	}
	if f, ok := obj.(*objects.Float); ok {
		p.saveFloat(f.Float64())
		return nil
	}

	// Memoized types: BINGET on hit.
	if _, hit := p.memo[obj]; hit {
		return p.memoGet(obj)
	}

	switch v := obj.(type) {
	case *objects.Bytes:
		if err := p.saveBytes(v.Bytes()); err != nil {
			return err
		}
		return p.memoPut(obj)
	case *objects.Unicode:
		if err := p.saveUnicode(v.Value()); err != nil {
			return err
		}
		return p.memoPut(obj)
	case *objects.List:
		return p.saveList(v)
	case *objects.Tuple:
		return p.saveTuple(v)
	case *objects.Dict:
		return p.saveDict(v)
	case *objects.Set:
		if v.Type() == objects.FrozensetType {
			return p.saveFrozenset(v)
		}
		return p.saveSet(v)
	}
	return errUnsupportedType
}

var errUnsupportedType = errors.New("PicklingError: unsupported type in encoder")

// dumpsAtom is the Phase 2/3 driver: PROTO header, save the object,
// STOP, commit the frame. The result is a fresh []byte holding the
// pickle stream.
//
// CPython: Modules/_pickle.c:7865 _pickle_dumps_impl (single-object path)
func dumpsAtom(obj objects.Object, proto int) ([]byte, error) {
	if proto < 0 || proto > highestProtocol {
		return nil, fmt.Errorf("PicklingError: pickle protocol must be <= %d", highestProtocol)
	}
	p := newPickler(proto)
	p.writeProtoHeader()
	if err := p.save(obj); err != nil {
		return nil, err
	}
	p.writeStop()
	p.commitFrame()
	return p.buf, nil
}
