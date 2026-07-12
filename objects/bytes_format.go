// Port of bytes/bytearray printf-style formatting (the % operator). The
// conversion set differs from str %: %b and %s consume a bytes-like object
// (or one implementing __bytes__), %a and %r emit an ASCII repr, and %c
// takes a byte value or an integer in range(256). The numeric conversions
// (d/i/u/o/x/X and e/E/f/F/g/G) reuse the str formatters since their output
// is pure ASCII.
//
// CPython: Objects/bytesobject.c:598 _PyBytes_FormatEx

package objects

import (
	"bytes"
	"fmt"
	"math"
	"strings"
)

// bytesFormat ports _PyBytes_FormatEx. format is the raw format buffer, args
// is the right-hand operand, and useByteArray only selects the result type
// (bytes vs bytearray).
//
// CPython: Objects/bytesobject.c:598 _PyBytes_FormatEx
func bytesFormat(format []byte, args Object, useByteArray bool) (Object, error) {
	out := make([]byte, 0, len(format))
	pos := 0

	var argTuple []Object
	argLen := -1
	argIdx := -2
	if t, ok := args.(*Tuple); ok {
		argTuple = make([]Object, t.Len())
		for i := 0; i < t.Len(); i++ {
			argTuple[i] = t.Item(i)
		}
		argLen = t.Len()
		argIdx = 0
	}

	// A mapping argument enables the %(key)b syntax. CPython treats any
	// object with mp_subscript that is not a tuple/bytes/str/bytearray as a
	// mapping.
	var dict Object
	if bytesArgIsMapping(args) {
		dict = args
	}

	// getnextarg ports getnextarg: pull the next positional argument.
	getnextarg := func() (Object, error) {
		if argIdx < argLen {
			cur := argIdx
			argIdx++
			if argLen < 0 {
				return args, nil
			}
			return argTuple[cur], nil
		}
		return nil, fmt.Errorf("TypeError: not enough arguments for format string")
	}

	for pos < len(format) {
		if format[pos] != '%' {
			next := bytesIndexByte(format[pos+1:], '%')
			var seg int
			if next >= 0 {
				seg = next + 1
			} else {
				seg = len(format) - pos
			}
			out = append(out, format[pos:pos+seg]...)
			pos += seg
			continue
		}
		// A format specifier. pos points at '%'.
		pos++
		if pos >= len(format) {
			return nil, fmt.Errorf("ValueError: incomplete format")
		}
		if format[pos] == '%' {
			out = append(out, '%')
			pos++
			continue
		}

		arg := fmtArg{width: -1, prec: -1}

		// %(key) mapping lookup.
		if format[pos] == '(' {
			if dict == nil {
				return nil, fmt.Errorf("TypeError: format requires a mapping")
			}
			pos++
			start := pos
			depth := 1
			for pos < len(format) && depth > 0 {
				switch format[pos] {
				case '(':
					depth++
				case ')':
					depth--
				}
				if depth == 0 {
					break
				}
				pos++
			}
			if pos >= len(format) || depth != 0 {
				return nil, fmt.Errorf("ValueError: incomplete format key")
			}
			key := append([]byte(nil), format[start:pos]...)
			pos++
			v, err := GetItem(dict, NewBytes(key))
			if err != nil {
				return nil, err
			}
			args = v
			argTuple = nil
			argLen = -1
			argIdx = -2
		}

		// Flags.
		for pos < len(format) {
			switch format[pos] {
			case '-':
				arg.flags |= fmtLJust
			case '+':
				arg.flags |= fmtSign
			case ' ':
				arg.flags |= fmtBlank
			case '#':
				arg.flags |= fmtAlt
			case '0':
				arg.flags |= fmtZero
			default:
				goto bytesWidth
			}
			pos++
		}
	bytesWidth:
		if err := bytesReadNumOrStar(format, &pos, &arg.width, false, getnextarg); err != nil {
			return nil, err
		}
		if arg.width < 0 && arg.width != -1 {
			arg.flags |= fmtLJust
			arg.width = -arg.width
		}

		// Precision.
		if pos < len(format) && format[pos] == '.' {
			pos++
			arg.prec = 0
			if err := bytesReadNumOrStar(format, &pos, &arg.prec, true, getnextarg); err != nil {
				return nil, err
			}
			if arg.prec < 0 {
				arg.prec = 0
			}
		}

		// Length modifier (ignored, as in CPython).
		if pos < len(format) {
			switch format[pos] {
			case 'h', 'l', 'L':
				pos++
			}
		}
		if pos >= len(format) {
			return nil, fmt.Errorf("ValueError: incomplete format")
		}

		arg.ch = rune(format[pos])
		convPos := pos
		pos++

		v, err := getnextarg()
		if err != nil {
			return nil, err
		}

		body, err := bytesFormatBody(&arg, v, convPos)
		if err != nil {
			return nil, err
		}
		out, err = bytesWritePadded(out, &arg, body)
		if err != nil {
			return nil, err
		}

		if dict != nil && argIdx < argLen {
			return nil, fmt.Errorf("TypeError: not all arguments converted during bytes formatting")
		}
	}

	if dict == nil && argIdx < argLen {
		return nil, fmt.Errorf("TypeError: not all arguments converted during bytes formatting")
	}

	if useByteArray {
		return NewByteArray(out), nil
	}
	return NewBytes(out), nil
}

// byteArrayModulo implements bytearray % args. The bytearray's export count
// is bumped for the duration of the format so a callback that mutates the
// bytearray (e.g. a __repr__ that clears it) is rejected with BufferError
// instead of corrupting the buffer mid-format.
//
// CPython: Objects/bytearrayobject.c:2789 bytearray_mod_lock_held
func byteArrayModulo(a, b Object) (Object, error) {
	ba, ok := a.(*ByteArray)
	if !ok {
		return notImplemented(), nil
	}
	ba.ExportInc()
	defer ba.ExportDec()
	return bytesFormat(ba.v, b, true)
}

// bytesFormatBody renders v according to arg.ch and returns the raw body
// bytes (without width padding). convPos is the index of the conversion
// character in format, used for the index-bearing error message.
//
// CPython: Objects/bytesobject.c:786 _PyBytes_FormatEx (the switch on c)
func bytesFormatBody(arg *fmtArg, v Object, convPos int) ([]byte, error) {
	switch arg.ch {
	case 'r', 'a':
		s, err := Repr(v)
		if err != nil {
			return nil, err
		}
		return []byte(truncatePrec(asciiEscape(s), arg.prec)), nil
	case 's', 'b':
		buf, err := bytesFormatObj(v)
		if err != nil {
			return nil, err
		}
		if arg.prec >= 0 && len(buf) > arg.prec {
			buf = buf[:arg.prec]
		}
		return append([]byte(nil), buf...), nil
	case 'i', 'd', 'u':
		s, err := formatLong(v, arg, 10)
		if err != nil {
			return nil, err
		}
		return []byte(s), nil
	case 'o':
		s, err := formatLong(v, arg, 8)
		if err != nil {
			return nil, err
		}
		return []byte(s), nil
	case 'x':
		s, err := formatLong(v, arg, 16)
		if err != nil {
			return nil, err
		}
		return []byte(s), nil
	case 'X':
		s, err := formatLong(v, arg, 16)
		if err != nil {
			return nil, err
		}
		return []byte(strings.ToUpper(s)), nil
	case 'e', 'E', 'f', 'F', 'g', 'G':
		arg.signable = true
		s, err := formatFloat(v, arg)
		if err != nil {
			return nil, err
		}
		return []byte(s), nil
	case 'c':
		b, err := bytesByteConverter(v)
		if err != nil {
			return nil, err
		}
		return []byte{b}, nil
	}
	return nil, fmt.Errorf(
		"ValueError: unsupported format character '%c' (0x%x) at index %d",
		arg.ch, arg.ch, convPos)
}

// bytesFormatObj ports format_obj: produce the byte buffer for a %b / %s
// conversion. bytes and bytearray expose their buffer directly; otherwise
// __bytes__ is honored, then the buffer protocol.
//
// CPython: Objects/bytesobject.c:546 format_obj
func bytesFormatObj(v Object) ([]byte, error) {
	switch x := v.(type) {
	case *Bytes:
		return x.v, nil
	case *ByteArray:
		return x.v, nil
	}
	if fn, err := LookupSpecial(v, "__bytes__"); err == nil && fn != nil {
		res, cerr := CallObject(fn, nil)
		if cerr != nil {
			return nil, cerr
		}
		b, ok := res.(*Bytes)
		if !ok {
			return nil, fmt.Errorf("TypeError: __bytes__ returned non-bytes (type %s)", res.Type().Name)
		}
		return b.v, nil
	}
	if buf, ok := AsBytesLike(v); ok {
		return buf, nil
	}
	return nil, fmt.Errorf(
		"TypeError: %%b requires a bytes-like object, or an object that implements __bytes__, not '%s'",
		v.Type().Name)
}

// bytesByteConverter ports byte_converter: resolve a %c argument to a single
// byte. A length-1 bytes/bytearray yields its lone byte; an index-bearing
// object must fall in range(256).
//
// CPython: Objects/bytesobject.c:496 byte_converter
func bytesByteConverter(v Object) (byte, error) {
	switch x := v.(type) {
	case *Bytes:
		if len(x.v) != 1 {
			return 0, fmt.Errorf(
				"TypeError: %%c requires an integer in range(256) or a single byte, not a bytes object of length %d",
				len(x.v))
		}
		return x.v[0], nil
	case *ByteArray:
		if len(x.v) != 1 {
			return 0, fmt.Errorf(
				"TypeError: %%c requires an integer in range(256) or a single byte, not a bytearray object of length %d",
				len(x.v))
		}
		return x.v[0], nil
	}
	if IndexCheck(v) {
		iv, err := NumberIndex(v)
		if err != nil {
			return 0, err
		}
		n, fits := iv.(*Int).Int64()
		if !fits || n < 0 || n > 255 {
			return 0, fmt.Errorf("OverflowError: %%c arg not in range(256)")
		}
		return byte(n), nil
	}
	return 0, fmt.Errorf(
		"TypeError: %%c requires an integer in range(256) or a single byte, not %s",
		typeFullNameOf(v))
}

// bytesWritePadded applies width and the F_LJUST/F_ZERO flags to body and
// appends the result to out. Width is measured in bytes. For numeric specs
// (signable) with F_ZERO the zero padding lives between the sign/prefix and
// the digits. A width that demands more padding than can be allocated
// surfaces as MemoryError, the way _PyBytesWriter reports a failed malloc.
//
// CPython: Objects/bytesobject.c:980 _PyBytes_FormatEx (the padding tail)
func bytesWritePadded(out []byte, arg *fmtArg, body []byte) ([]byte, error) {
	width := arg.width
	if width < 0 {
		width = 0
	}
	if len(body) >= width {
		return append(out, body...), nil
	}
	pad := width - len(body)
	switch {
	case arg.flags&fmtLJust != 0:
		out = append(out, body...)
		return appendRepeatByte(out, ' ', pad)
	case arg.flags&fmtZero != 0 && arg.signable:
		signEnd := 0
		if len(body) > 0 && (body[0] == '-' || body[0] == '+' || body[0] == ' ') {
			signEnd = 1
		}
		prefixEnd := signEnd
		if len(body) >= signEnd+2 && body[signEnd] == '0' &&
			(body[signEnd+1] == 'x' || body[signEnd+1] == 'X' || body[signEnd+1] == 'o') {
			prefixEnd = signEnd + 2
		}
		out = append(out, body[:prefixEnd]...)
		out, err := appendRepeatByte(out, '0', pad)
		if err != nil {
			return nil, err
		}
		return append(out, body[prefixEnd:]...), nil
	default:
		out, err := appendRepeatByte(out, ' ', pad)
		if err != nil {
			return nil, err
		}
		return append(out, body...), nil
	}
}

// bytesReadNumOrStar parses a width/precision token at *pos. "*" pulls the
// next argument; a digit run reads as decimal.
//
// asInt selects the C type the "*" argument is coerced into: width goes
// through PyLong_AsSsize_t and precision through PyLong_AsInt, so a
// precision above INT_MAX overflows even though it fits a ssize_t. A
// literal digit run is bounded by PY_SSIZE_T_MAX for width and INT_MAX
// for precision, matching the two overflow checks below.
//
// CPython: Objects/bytesobject.c:746 (width PyLong_AsSsize_t),
//
//	Objects/bytesobject.c:787 (prec PyLong_AsInt)
func bytesReadNumOrStar(format []byte, pos *int, dst *int, asInt bool, getnextarg func() (Object, error)) error {
	if *pos >= len(format) {
		return nil
	}
	if format[*pos] == '*' {
		*pos++
		v, err := getnextarg()
		if err != nil {
			return err
		}
		i, ok := v.(*Int)
		if !ok {
			return fmt.Errorf("TypeError: * wants int")
		}
		n, fits := i.Int64()
		if !fits {
			return fmt.Errorf("OverflowError: Python int too large to convert to C ssize_t")
		}
		if asInt && (n > math.MaxInt32 || n < math.MinInt32) {
			return fmt.Errorf("OverflowError: Python int too large to convert to C int")
		}
		*dst = int(n)
		return nil
	}
	c := format[*pos]
	if c < '0' || c > '9' {
		return nil
	}
	limit := int64(math.MaxInt64)
	overMsg := "ValueError: width too big"
	if asInt {
		limit = math.MaxInt32
		overMsg = "ValueError: prec too big"
	}
	var n int64
	for *pos < len(format) {
		c := format[*pos]
		if c < '0' || c > '9' {
			break
		}
		if n > (limit-int64(c-'0'))/10 {
			return fmt.Errorf("%s", overMsg)
		}
		n = n*10 + int64(c-'0')
		*pos++
	}
	*dst = int(n)
	return nil
}

// bytesArgIsMapping reports whether args should be treated as a mapping for
// the %(key)b syntax: any object exposing mp_subscript other than a tuple,
// bytes, str, or bytearray.
//
// CPython: Objects/bytesobject.c:633 _PyBytes_FormatEx (the dict test)
func bytesArgIsMapping(o Object) bool {
	switch o.(type) {
	case *Tuple, *Bytes, *Unicode, *ByteArray:
		return false
	}
	if m := o.Type().Mapping; m != nil && m.GetItem != nil {
		return true
	}
	return false
}

// bytesIndexByte returns the index of the first occurrence of c in buf, or
// -1. Mirrors memchr.
func bytesIndexByte(buf []byte, c byte) int {
	for i := 0; i < len(buf); i++ {
		if buf[i] == c {
			return i
		}
	}
	return -1
}

// appendRepeatByte appends n copies of c to out in a single allocation.
// A width large enough that the padding cannot be allocated (e.g. a "*"
// width of PY_SSIZE_T_MAX) makes the runtime reject the make; the recover
// turns that into MemoryError, matching CPython where _PyBytesWriter's
// failed realloc raises MemoryError rather than spinning.
//
// CPython: Objects/bytesobject.c _PyBytesWriter_Prepare (PyErr_NoMemory)
func appendRepeatByte(out []byte, c byte, n int) (result []byte, err error) {
	if n <= 0 {
		return out, nil
	}
	defer func() {
		if r := recover(); r != nil {
			result, err = nil, fmt.Errorf("MemoryError")
		}
	}()
	return append(out, bytes.Repeat([]byte{c}, n)...), nil
}
