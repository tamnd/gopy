package pystrconv

import (
	"errors"
	"math"
	"strconv"
	"strings"
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
//
// CPython: Python/pystrtod.c:L298 PyOS_string_to_double
func ParseFloat(s string) (float64, error) {
	if s == "" {
		return 0, ErrInvalidFloat
	}
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
		return math.NaN(), true
	}
	return 0, false
}
