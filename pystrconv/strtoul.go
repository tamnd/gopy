package pystrconv

import "math/bits"

// Locale-independent integer parsing. Ported from
// cpython/Python/mystrtoul.c. Used by int(s, base), the lexer, and a
// handful of config parsers. Supports base 0 (autodetect 0x/0o/0b),
// bases 2..36, leading whitespace, optional sign, leading-zero
// stripping, and overflow detection. PEP 515 underscores between
// digits land alongside the parser callers in v0.5; v0.4 keeps the
// shape one-to-one with the C source.

// IntParseStatus is the parse outcome.
type IntParseStatus int

const (
	// IntOK means the number parsed cleanly.
	IntOK IntParseStatus = iota
	// IntInvalid means the input had no digits at the expected position.
	IntInvalid
	// IntOverflow means the number is too large for the target type.
	IntOverflow
)

// digitValue mirrors _PyLong_DigitValue. Returns 37 for non-digits so
// any base 2..36 lookup naturally rejects them.
//
// CPython: Objects/longobject.c:L48 _PyLong_DigitValue
func digitValue(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'z':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'Z':
		return int(c-'A') + 10
	}
	return 37
}

// ParseUint mirrors PyOS_strtoul. Returns the parsed value, the
// number of bytes consumed from s (the index of the first byte not
// part of the number after leading whitespace), and a status code.
//
// CPython: Python/mystrtoul.c:L100 PyOS_strtoul
func ParseUint(s string, base int) (val uint64, n int, status IntParseStatus) {
	i := 0
	for i < len(s) && IsSpace(s[i]) {
		i++
	}
	start := i

	switch base {
	case 0:
		switch {
		case i < len(s) && s[i] == '0':
			i++
			switch {
			case i < len(s) && (s[i] == 'x' || s[i] == 'X'):
				if i+1 >= len(s) || digitValue(s[i+1]) >= 16 {
					return 0, i, IntInvalid
				}
				i++
				base = 16
			case i < len(s) && (s[i] == 'o' || s[i] == 'O'):
				if i+1 >= len(s) || digitValue(s[i+1]) >= 8 {
					return 0, i, IntInvalid
				}
				i++
				base = 8
			case i < len(s) && (s[i] == 'b' || s[i] == 'B'):
				if i+1 >= len(s) || digitValue(s[i+1]) >= 2 {
					return 0, i, IntInvalid
				}
				i++
				base = 2
			default:
				for i < len(s) && s[i] == '0' {
					i++
				}
				for i < len(s) && IsSpace(s[i]) {
					i++
				}
				return 0, i, IntOK
			}
		default:
			base = 10
		}
	case 16:
		if i+1 < len(s) && s[i] == '0' && (s[i+1] == 'x' || s[i+1] == 'X') {
			if i+2 >= len(s) || digitValue(s[i+2]) >= 16 {
				return 0, i, IntInvalid
			}
			i += 2
		}
	case 8:
		if i+1 < len(s) && s[i] == '0' && (s[i+1] == 'o' || s[i+1] == 'O') {
			if i+2 >= len(s) || digitValue(s[i+2]) >= 8 {
				return 0, i, IntInvalid
			}
			i += 2
		}
	case 2:
		if i+1 < len(s) && s[i] == '0' && (s[i+1] == 'b' || s[i+1] == 'B') {
			if i+2 >= len(s) || digitValue(s[i+2]) >= 2 {
				return 0, i, IntInvalid
			}
			i += 2
		}
	}

	if base < 2 || base > 36 {
		return 0, i, IntInvalid
	}

	for i < len(s) && s[i] == '0' {
		i++
	}

	digitsStart := i
	var result uint64
	overflow := false
	for i < len(s) {
		d := digitValue(s[i])
		if d >= base {
			break
		}
		hi, lo := bits.Mul64(result, uint64(base))
		if hi != 0 {
			overflow = true
		}
		nr := lo + uint64(d)
		if nr < lo {
			overflow = true
		}
		result = nr
		i++
	}

	if overflow {
		// spool through the rest of the digits
		for i < len(s) && digitValue(s[i]) < base {
			i++
		}
		return 0, i, IntOverflow
	}

	if digitsStart == start && i == start {
		// no digits seen at all
		return 0, i, IntInvalid
	}

	return result, i, IntOK
}

// ParseInt mirrors PyOS_strtol. Accepts an optional leading sign.
//
// CPython: Python/mystrtoul.c:L268 PyOS_strtol
func ParseInt(s string, base int) (val int64, n int, status IntParseStatus) {
	i := 0
	for i < len(s) && IsSpace(s[i]) {
		i++
	}
	negative := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		negative = s[i] == '-'
		i++
	}
	uval, n, status := ParseUint(s[i:], base)
	if status != IntOK {
		return 0, i + n, status
	}
	consumed := i + n
	if uval <= 1<<63-1 {
		v := int64(uval)
		if negative {
			v = -v
		}
		return v, consumed, IntOK
	}
	if negative && uval == 1<<63 {
		return -1 << 63, consumed, IntOK
	}
	return 0, consumed, IntOverflow
}
