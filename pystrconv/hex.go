package pystrconv

// Bytes-to-hex with optional separator. Ported from
// cpython/Python/pystrhex.c. Used by bytes.hex() and memoryview.hex()
// in the eventual unicode port.

const hexDigits = "0123456789abcdef"

// Hex returns the hex of buf without a separator. Mirrors _Py_strhex.
//
// CPython: Python/pystrhex.c:L145 _Py_strhex
func Hex(buf []byte) string {
	return string(HexBytes(buf))
}

// HexBytes is the []byte-returning variant of Hex. Mirrors
// _Py_strhex_bytes. The returned slice is freshly allocated and owned
// by the caller.
//
// CPython: Python/pystrhex.c:L152 _Py_strhex_bytes
func HexBytes(buf []byte) []byte {
	out := make([]byte, len(buf)*2)
	for i, c := range buf {
		out[2*i] = hexDigits[c>>4]
		out[2*i+1] = hexDigits[c&0x0f]
	}
	return out
}

// HexWithSep returns the hex of buf with sep inserted between groups
// of bytesPerGroup bytes. Positive bytesPerGroup groups from the
// right (the bytes.hex default), negative groups from the left.
// Mirrors _Py_strhex_with_sep.
//
// CPython: Python/pystrhex.c:L159 _Py_strhex_with_sep
func HexWithSep(buf []byte, sep byte, bytesPerGroup int) string {
	return string(HexBytesWithSep(buf, sep, bytesPerGroup))
}

// HexBytesWithSep is the []byte-returning variant of HexWithSep.
// Mirrors _Py_strhex_bytes_with_sep.
//
// CPython: Python/pystrhex.c:L167 _Py_strhex_bytes_with_sep
func HexBytesWithSep(buf []byte, sep byte, bytesPerGroup int) []byte {
	if bytesPerGroup == 0 || len(buf) == 0 {
		return HexBytes(buf)
	}
	abs := bytesPerGroup
	if abs < 0 {
		abs = -abs
	}
	if abs >= len(buf) {
		return HexBytes(buf)
	}
	chunks := (len(buf) - 1) / abs
	resultlen := chunks + len(buf)*2
	out := make([]byte, resultlen)

	if bytesPerGroup < 0 {
		// Left-grouped: [a b][c d][e f].
		i, j := 0, 0
		for range chunks {
			for range abs {
				c := buf[i]
				i++
				out[j] = hexDigits[c>>4]
				out[j+1] = hexDigits[c&0x0f]
				j += 2
			}
			out[j] = sep
			j++
		}
		for i < len(buf) {
			c := buf[i]
			i++
			out[j] = hexDigits[c>>4]
			out[j+1] = hexDigits[c&0x0f]
			j += 2
		}
		return out
	}

	// Right-grouped: write from the tail.
	i := len(buf) - 1
	j := resultlen - 1
	for range chunks {
		for range abs {
			c := buf[i]
			i--
			out[j] = hexDigits[c&0x0f]
			out[j-1] = hexDigits[c>>4]
			j -= 2
		}
		out[j] = sep
		j--
	}
	for i >= 0 {
		c := buf[i]
		i--
		out[j] = hexDigits[c&0x0f]
		out[j-1] = hexDigits[c>>4]
		j -= 2
	}
	return out
}
