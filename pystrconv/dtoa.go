package pystrconv

import (
	"math"
	"strconv"
	"strings"
)

// Locale-independent float formatting. Ported from
// cpython/Python/pystrtod.c (PyOS_double_to_string + format_float_short)
// plus the David Gay dtoa.c shortest-round-trip routine. The bignum
// machinery is replaced with Go's strconv (Ryu/Grisu plus a bignum
// fallback) which is also IEEE-754 round-to-nearest-even, so the
// digit string returned by shortestDigits matches _Py_dg_dtoa(mode=0).

// FloatFormatFlag mirrors the Py_DTSF_* flags PyOS_double_to_string
// accepts.
//
// CPython: Include/cpython/pyhash.h adjacent (Py_DTSF_* flags in pystrtod.h)
type FloatFormatFlag uint32

const (
	// FlagAddDotZero forces an integer-valued float to render with
	// a trailing ".0" so the type round-trips through repr.
	FlagAddDotZero FloatFormatFlag = 1 << iota
	// FlagAlternate mirrors Py_DTSF_ALT (use_alt_formatting).
	FlagAlternate
	// FlagAlwaysSign mirrors Py_DTSF_SIGN (always_add_sign).
	FlagAlwaysSign
	// FlagSpaceSign reserves a leading space for non-negative values.
	// Mirrors the ' ' format-spec sign mode.
	FlagSpaceSign
	// FlagNoNegZero mirrors Py_DTSF_NO_NEG_0.
	FlagNoNegZero
)

// FormatFloat mirrors PyOS_double_to_string. code is one of
// 'r', 's', 'g', 'G', 'e', 'E', 'f', 'F', '%'. precision is -1 to use
// the format's default; for 'r' it must be 0.
//
// CPython: Python/pystrtod.c:L757 PyOS_double_to_string
func FormatFloat(f float64, code byte, precision int, flags FloatFormatFlag) string {
	upper := false
	switch code {
	case 'E':
		upper = true
		code = 'e'
	case 'F':
		upper = true
		code = 'f'
	case 'G':
		upper = true
		code = 'g'
	case 'r':
		precision = 17
		code = 'g'
	case 's':
		precision = 12
		code = 'g'
	case 'e', 'f', 'g', '%':
	default:
		return ""
	}

	out := formatFloatShort(f, code, precision, flags)
	if upper {
		out = strings.ToUpper(out)
	}
	return out
}

// formatFloatShort is the renderer behind FormatFloat. It mirrors
// CPython's format_float_short with mode-0 dtoa.
//
// CPython: Python/pystrtod.c:L969 format_float_short
func formatFloatShort(f float64, code byte, precision int, flags FloatFormatFlag) string {
	var sign string
	switch {
	case math.Signbit(f) && !(flags&FlagNoNegZero != 0 && f == 0):
		sign = "-"
	case flags&FlagAlwaysSign != 0:
		sign = "+"
	case flags&FlagSpaceSign != 0:
		sign = " "
	}

	if math.IsNaN(f) {
		// CPython ignores the sign of NaN.
		out := "nan"
		if flags&FlagAlwaysSign != 0 {
			return "+" + out
		}
		if flags&FlagSpaceSign != 0 {
			return " " + out
		}
		return out
	}
	if math.IsInf(f, 0) {
		return sign + "inf"
	}

	addPercent := false
	if code == '%' {
		f *= 100
		code = 'f'
		addPercent = true
	}

	digits, decpt := shortestDigits(f, code, precision)

	body := layoutFloat(digits, decpt, code, precision, flags)
	if addPercent {
		body += "%"
	}
	return sign + body
}

// shortestDigits returns the digit string and the position of the
// implicit decimal point (decpt) for the shortest round-trip
// representation of f under format code. Mirrors _Py_dg_dtoa output.
//
// CPython: Python/dtoa.c _Py_dg_dtoa (mode 0, 2, 3 selected by code)
func shortestDigits(f float64, code byte, precision int) (digits string, decpt int) {
	if f == 0 {
		switch code {
		case 'e':
			if precision < 0 {
				precision = 6
			}
			return strings.Repeat("0", precision+1), 1
		case 'f':
			if precision < 0 {
				precision = 6
			}
			return strings.Repeat("0", precision+1), 1
		default:
			return "0", 1
		}
	}

	abs := math.Abs(f)
	switch code {
	case 'g':
		// Shortest round-trip plus an upper bound from precision.
		buf := strconv.AppendFloat(nil, abs, 'e', -1, 64)
		mantissa, exp := splitScientific(buf)
		if precision > 0 && len(mantissa) > precision {
			// Re-render at fixed precision when shorter than the natural one.
			buf = strconv.AppendFloat(nil, abs, 'e', precision-1, 64)
			mantissa, exp = splitScientific(buf)
		}
		return mantissa, exp + 1
	case 'e':
		buf := strconv.AppendFloat(nil, abs, 'e', precision, 64)
		mantissa, exp := splitScientific(buf)
		return mantissa, exp + 1
	case 'f':
		// CPython: Python/pystrtod.c:969 format_float_short mode=3
		// Go's strconv 'f' uses the same IEEE 754 round-half-even as CPython.
		if precision < 0 {
			precision = 6
		}
		buf := strconv.AppendFloat(nil, abs, 'f', precision, 64)
		s := string(buf)
		dotIdx := strings.IndexByte(s, '.')
		if dotIdx < 0 {
			// e.g. "0" or "123" — no fractional part (precision==0)
			return s, len(s)
		}
		intPart := s[:dotIdx]
		fracPart := s[dotIdx+1:]
		return intPart + fracPart, dotIdx
	}
	return "", 0
}

// splitScientific parses a Go-strconv 'e'-format byte slice (e.g.
// "1.5e+15", "5e-05", "-0e+00") and returns the digit string with the
// decimal point removed plus the integer exponent.
func splitScientific(buf []byte) (mantissa string, exp int) {
	s := string(buf)
	if s[0] == '-' || s[0] == '+' {
		s = s[1:]
	}
	m, expStr, _ := strings.Cut(s, "e")
	mantissa = strings.ReplaceAll(m, ".", "")
	mantissa = strings.TrimLeft(mantissa, "0")
	if mantissa == "" {
		mantissa = "0"
	}
	exp, _ = strconv.Atoi(expStr)
	return mantissa, exp
}

// layoutFloat assembles the body of the formatted float (no sign and
// no percent suffix) given the digit string and decpt position.
//
// CPython: Python/pystrtod.c:L1055 (the "we got digits back" block)
func layoutFloat(digits string, decpt int, code byte, precision int, flags FloatFormatFlag) string {
	useExp := false
	vdigitsEnd := len(digits)
	addDotZero := flags&FlagAddDotZero != 0
	useAlt := flags&FlagAlternate != 0

	switch code {
	case 'e':
		useExp = true
		vdigitsEnd = precision + 1
	case 'f':
		vdigitsEnd = decpt + precision
	case 'g':
		thresh := precision
		if addDotZero {
			thresh = precision - 1
		}
		if decpt <= -4 || decpt > thresh {
			useExp = true
		}
		if useAlt {
			vdigitsEnd = precision
		}
	}

	exp := 0
	if useExp {
		exp = decpt - 1
		decpt = 1
	}

	if !useExp && addDotZero {
		if vdigitsEnd <= decpt {
			vdigitsEnd = decpt + 1
		}
	} else if vdigitsEnd < decpt {
		vdigitsEnd = decpt
	}

	var b strings.Builder
	// Leading zeros (decpt <= 0 path) or padding before digits.
	if decpt <= 0 {
		b.WriteByte('0')
		b.WriteByte('.')
		for i := 0; i < -decpt; i++ {
			b.WriteByte('0')
		}
		writeDigitsClipped(&b, digits, 0, vdigitsEnd)
	} else {
		writeDigitsClipped(&b, digits, 0, decpt)
		if vdigitsEnd > decpt {
			b.WriteByte('.')
			writeDigitsClipped(&b, digits, decpt, vdigitsEnd)
		} else if useAlt {
			b.WriteByte('.')
		}
	}

	if useExp {
		b.WriteByte('e')
		if exp >= 0 {
			b.WriteByte('+')
		} else {
			b.WriteByte('-')
			exp = -exp
		}
		// CPython exponent uses at least 2 digits.
		expStr := strconv.Itoa(exp)
		if len(expStr) < 2 {
			expStr = "0" + expStr
		}
		b.WriteString(expStr)
	}

	return b.String()
}

func writeDigitsClipped(b *strings.Builder, digits string, start, end int) {
	for i := start; i < end; i++ {
		if i < 0 || i >= len(digits) {
			b.WriteByte('0')
			continue
		}
		b.WriteByte(digits[i])
	}
}
