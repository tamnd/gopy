package pystrconv

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"unicode"
)

// Locale-independent float parsing. Ported from
// cpython/Python/pystrtod.c. The dtoa.c bignum machinery is replaced
// with Go's strconv (Ryu/Grisu plus bignum fallback), which is also
// IEEE-754 round-to-nearest-even, so the resulting uint64 bit pattern
// is identical for every input CPython accepts.

// ErrInvalidFloat is returned by ParseFloat when the input is not a
// valid Python float literal.
var ErrInvalidFloat = errors.New("could not convert string to float")

// ErrFloatOverflow is returned by ParseFloat when the literal is
// finite but its magnitude exceeds the float64 range.
var ErrFloatOverflow = errors.New("value too large to convert to float")

// ParseFloat mirrors PyOS_string_to_double. The string must already be
// stripped of outer whitespace by the caller; ParseFloat itself does
// not strip. Underscores following PEP 515 placement are accepted.
// Unicode decimal digits and Unicode spaces are normalized to ASCII
// before parsing, mirroring _PyUnicode_TransformDecimalAndSpaceToASCII.
//
// CPython: Python/pystrtod.c:L298 PyOS_string_to_double
// CPython: Objects/unicodeobject.c:9612 _PyUnicode_TransformDecimalAndSpaceToASCII
func ParseFloat(s string) (float64, error) {
	if s == "" {
		return 0, ErrInvalidFloat
	}
	// Transform Unicode decimal digits and spaces to ASCII, then strip
	// spaces again (the caller's trimSpace only handles ASCII spaces).
	s = strings.TrimSpace(transformDecimalAndSpaceToASCII(s))
	cleaned, ok := stripUnderscores(s)
	if !ok {
		return 0, ErrInvalidFloat
	}
	if v, ok := parseInfNan(cleaned); ok {
		return v, nil
	}
	// Reject parenthesised NaN payload form that Go strconv accepts.
	if strings.ContainsAny(cleaned, "()") {
		return 0, ErrInvalidFloat
	}
	// CPython: Python/pystrtod.c:298 PyOS_string_to_double never accepts
	// hex-float notation; only float.fromhex() does. Go's strconv.ParseFloat
	// (1.13+) accepts "0x…" hex floats so we must reject them first.
	{
		rest := cleaned
		if rest != "" && (rest[0] == '+' || rest[0] == '-') {
			rest = rest[1:]
		}
		if strings.HasPrefix(strings.ToLower(rest), "0x") {
			return 0, ErrInvalidFloat
		}
	}
	v, err := strconv.ParseFloat(cleaned, 64)
	if err != nil {
		var ne *strconv.NumError
		if errors.As(err, &ne) && errors.Is(ne.Err, strconv.ErrRange) {
			// strconv reports ErrRange for both overflow (returning
			// +/-Inf) and underflow (returning a subnormal or 0).
			// CPython only flags overflow; underflow silently yields 0.
			if v != 0 {
				return v, ErrFloatOverflow
			}
			return v, nil
		}
		return 0, ErrInvalidFloat
	}
	return v, nil
}

// StripUnderscores is the exported wrapper around stripUnderscores. Used
// by callers like complex_from_string_inner that need to scrub
// underscores from the whole literal before sub-parsing components.
//
// CPython: Python/pystrtod.c:344 _Py_string_to_number_with_underscores
func StripUnderscores(s string) (string, bool) { return stripUnderscores(s) }

// ParseFloatPrefix mirrors PyOS_string_to_double's strtod-like behaviour:
// it parses the longest valid float at the start of s, returning the
// value and the byte index of the first character that is not part of
// the float. When the prefix is not a float, the returned end index is
// 0 (matching the CPython convention where complex_from_string_inner
// checks "end != s"). Underscores are NOT consumed here because
// PyOS_string_to_double goes straight to strtod without the
// _Py_string_to_number_with_underscores wrapper; the caller (e.g.
// complex_from_string_inner) is expected to have stripped underscores
// from the full literal beforehand. Overflow returns +/-Inf with a nil
// error, matching PyOS_string_to_double(s, &end, NULL).
//
// CPython: Python/pystrtod.c:298 PyOS_string_to_double
func ParseFloatPrefix(s string) (float64, int, error) {
	end := scanFloatPrefix(s)
	if end == 0 {
		return 0, 0, nil
	}
	v, err := ParseFloat(s[:end])
	if err != nil {
		if errors.Is(err, ErrFloatOverflow) {
			return v, end, nil
		}
		return 0, 0, nil
	}
	return v, end, nil
}

// scanFloatPrefix returns the length of the longest float syntactically
// at the start of s, or 0 when no float is present. The grammar matches
// strtod: optional sign, then (inf|infinity|nan) or a decimal mantissa
// (digits, optional fraction, optional exponent). Hex floats are not
// part of the prefix: PyOS_string_to_double rejects them.
func scanFloatPrefix(s string) int {
	i := 0
	n := len(s)
	if i < n && (s[i] == '+' || s[i] == '-') {
		i++
	}
	if i < n {
		switch lower := asciiLower(s[i:]); {
		case strings.HasPrefix(lower, "infinity"):
			return i + 8
		case strings.HasPrefix(lower, "inf"):
			return i + 3
		case strings.HasPrefix(lower, "nan"):
			return i + 3
		}
	}
	hasDigits := false
	for i < n && isDigit(s[i]) {
		hasDigits = true
		i++
	}
	if i < n && s[i] == '.' {
		i++
		for i < n && isDigit(s[i]) {
			hasDigits = true
			i++
		}
	}
	if !hasDigits {
		return 0
	}
	if i < n && (s[i] == 'e' || s[i] == 'E') {
		j := i + 1
		if j < n && (s[j] == '+' || s[j] == '-') {
			j++
		}
		if j < n && isDigit(s[j]) {
			j++
			for j < n && isDigit(s[j]) {
				j++
			}
			i = j
		}
	}
	return i
}

func asciiLower(s string) string {
	needs := false
	for i := 0; i < len(s); i++ {
		if s[i] >= 'A' && s[i] <= 'Z' {
			needs = true
			break
		}
	}
	if !needs {
		return s
	}
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

// stripUnderscores removes PEP 515 digit-grouping underscores. Returns
// the cleaned string and true, or ("", false) if any underscore is
// misplaced. The placement rule: an underscore must have a digit on
// both sides.
//
// CPython: Python/pystrtod.c:L344 _Py_string_to_number_with_underscores
func stripUnderscores(s string) (string, bool) {
	if !strings.Contains(s, "_") {
		return s, true
	}
	var b strings.Builder
	b.Grow(len(s))
	prev := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '_' {
			if !isDigit(prev) {
				return "", false
			}
			next := byte(0)
			if i+1 < len(s) {
				next = s[i+1]
			}
			if !isDigit(next) {
				return "", false
			}
			continue
		}
		b.WriteByte(c)
		prev = c
	}
	return b.String(), true
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// TransformDecimalAndSpaceToASCII is the exported wrapper. Callers
// outside pystrconv (e.g. complex_from_string_inner) need to fold
// Unicode digits/spaces before scanning byte positions.
//
// CPython: Objects/unicodeobject.c:9612 _PyUnicode_TransformDecimalAndSpaceToASCII
func TransformDecimalAndSpaceToASCII(s string) string {
	return transformDecimalAndSpaceToASCII(s)
}

// transformDecimalAndSpaceToASCII maps each rune in s to an ASCII byte:
//   - runes < 128 pass through unchanged
//   - Unicode whitespace maps to ' '
//   - Unicode decimal digits (category Nd) map to their ASCII equivalent
//   - anything else truncates the output with '?'
//
// CPython: Objects/unicodeobject.c:9612 _PyUnicode_TransformDecimalAndSpaceToASCII
func transformDecimalAndSpaceToASCII(s string) string {
	isASCII := true
	for _, r := range s {
		if r >= 128 {
			isASCII = false
			break
		}
	}
	if isASCII {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r < 128:
			b.WriteRune(r)
		case unicode.IsSpace(r):
			b.WriteByte(' ')
		case unicode.IsDigit(r):
			// Decimal digit blocks are runs of exactly 10 consecutive
			// code points (0–9). Walk down to find the '0' of the block.
			base := r
			for base > 0 && unicode.IsDigit(base-1) {
				base--
			}
			b.WriteByte(byte('0' + (r - base)))
		default:
			b.WriteByte('?')
			return b.String()
		}
	}
	return b.String()
}

// parseInfNan recognizes the case-insensitive inf/infinity/nan tokens
// with optional leading sign. Mirrors _Py_parse_inf_or_nan.
//
// CPython: Python/pystrtod.c:L28 _Py_parse_inf_or_nan
func parseInfNan(s string) (float64, bool) {
	sign := 1.0
	rest := s
	switch {
	case strings.HasPrefix(rest, "+"):
		rest = rest[1:]
	case strings.HasPrefix(rest, "-"):
		sign = -1
		rest = rest[1:]
	}
	low := strings.ToLower(rest)
	switch low {
	case "inf", "infinity":
		return math.Inf(int(sign)), true
	case "nan":
		// CPython: Python/pystrtod.c:28 _Py_parse_inf_or_nan preserves sign
		return math.Copysign(math.NaN(), sign), true
	}
	return 0, false
}
