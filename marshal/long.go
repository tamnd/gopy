// TYPE_LONG limb arithmetic. CPython stores arbitrary-precision ints
// as a signed count of 15-bit limbs followed by the limbs themselves
// in little-endian order. The count is negative when the value is
// negative; zero encodes to count=0 with no limbs.
//
// CPython: Python/marshal.c:L145 w_PyLong / r_PyLong
package marshal

import (
	"encoding/binary"
	"fmt"
	"io"
	"math/big"
)

const longShift = 15 // bits per TYPE_LONG limb

// writeLong encodes v as the given tag byte followed by the TYPE_LONG
// count-and-limbs payload.
//
// CPython: Python/marshal.c:L145 w_PyLong
func writeLong(w io.Writer, v *big.Int, tag byte) error {
	abs := new(big.Int).Abs(v)
	limbs := longEncode(abs)
	count := int32(len(limbs))
	if v.Sign() < 0 {
		count = -count
	}
	if _, err := w.Write([]byte{tag}); err != nil {
		return err
	}
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], uint32(count))
	if _, err := w.Write(buf[:]); err != nil {
		return err
	}
	var lbuf [2]byte
	for _, limb := range limbs {
		binary.LittleEndian.PutUint16(lbuf[:], limb)
		if _, err := w.Write(lbuf[:]); err != nil {
			return err
		}
	}
	return nil
}

// readLong decodes a TYPE_LONG value. The tag byte has already been consumed.
// Uses io.ByteReader for consistency with the rest of the decoder.
//
// CPython: Python/marshal.c:L418 r_PyLong
func readLong(r io.ByteReader) (*big.Int, error) {
	// Read 4-byte signed count.
	b := make([]byte, 4)
	for i := range 4 {
		v, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		b[i] = v
	}
	count := int32(binary.LittleEndian.Uint32(b))
	abs := count
	if abs < 0 {
		abs = -abs
	}
	result := new(big.Int)
	var shift uint
	lb := make([]byte, 2)
	for i := int32(0); i < abs; i++ {
		for j := range 2 {
			v, err := r.ReadByte()
			if err != nil {
				return nil, err
			}
			lb[j] = v
		}
		limb := uint64(binary.LittleEndian.Uint16(lb))
		if limb >= (1 << longShift) {
			return nil, fmt.Errorf("marshal: TYPE_LONG limb out of range: %d", limb)
		}
		tmp := new(big.Int).SetUint64(limb)
		tmp.Lsh(tmp, shift)
		result.Or(result, tmp)
		shift += longShift
	}
	if count < 0 {
		result.Neg(result)
	}
	return result, nil
}

// longEncode splits abs into 15-bit limbs, least-significant first.
func longEncode(abs *big.Int) []uint16 {
	if abs.Sign() == 0 {
		return nil
	}
	tmp := new(big.Int).Set(abs)
	base := new(big.Int).SetUint64(1 << longShift)
	var limbs []uint16
	for tmp.Sign() > 0 {
		mod := new(big.Int)
		tmp.QuoRem(tmp, base, mod)
		limbs = append(limbs, uint16(mod.Uint64()))
	}
	return limbs
}
