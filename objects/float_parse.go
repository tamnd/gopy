// Float parser used by float(s). Mirrors PyFloat_FromString:
// optional surrounding whitespace, optional sign, the inf/infinity/
// nan tokens (case-insensitive), and PEP 515 underscores between
// digits. The actual digit-to-double step goes through Go's
// strconv.ParseFloat which uses the same shortest-roundtrip algorithm
// CPython's _Py_dg_strtod ends up at.
//
// CPython: Objects/floatobject.c:172 PyFloat_FromString

package objects

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// errFloatInvalid is the shape float(s) raises when the literal
// cannot be parsed.
//
// CPython: Objects/floatobject.c:172 PyFloat_FromString (error path)
var errFloatInvalid = errors.New("ValueError: could not convert string to float")

// FloatFromString parses a Python float literal. Whitespace on either
// side is ignored, sign and inf/nan tokens are recognized
// case-insensitively, and underscores are allowed between digits.
//
// CPython: Objects/floatobject.c:172 PyFloat_FromString
func FloatFromString(s string) (*Float, error) {
	rest := strings.TrimSpace(s)
	if rest == "" {
		return nil, fmtFloatInvalid(s)
	}

	negative := false
	switch rest[0] {
	case '+':
		rest = rest[1:]
	case '-':
		negative = true
		rest = rest[1:]
	}
	if rest == "" {
		return nil, fmtFloatInvalid(s)
	}

	if v, ok := parseFloatSpecial(rest); ok {
		if negative {
			v = -v
		}
		return NewFloat(v), nil
	}

	digits, ok := stripFloatUnderscores(rest)
	if !ok || digits == "" {
		return nil, fmtFloatInvalid(s)
	}

	// CPython: Objects/floatobject.c:172 PyFloat_FromString never accepts
	// hex-float notation ("0x…"); only float.fromhex() does.
	low := strings.ToLower(digits)
	if strings.HasPrefix(low, "0x") || strings.HasPrefix(low, "0X") {
		return nil, fmtFloatInvalid(s)
	}

	v, err := strconv.ParseFloat(digits, 64)
	if err != nil {
		// ErrRange means the value is finite but rounds to +/-Inf or 0;
		// CPython treats overflow as +/-Inf rather than ValueError so
		// float('1e500') == inf. Only ErrSyntax stays a ValueError.
		// CPython: Python/pystrtod.c:418 _PyOS_ascii_strtod (ERANGE path)
		var ne *strconv.NumError
		if !errors.As(err, &ne) || !errors.Is(ne.Err, strconv.ErrRange) {
			return nil, fmtFloatInvalid(s)
		}
	}
	if negative {
		v = -v
	}
	return NewFloat(v), nil
}

// parseFloatSpecial recognizes the inf/infinity/nan tokens. Returns
// the resulting double and true on a match.
//
// CPython: Python/pystrtod.c:131 _Py_parse_inf_or_nan
func parseFloatSpecial(s string) (float64, bool) {
	low := strings.ToLower(s)
	switch low {
	case "inf", "infinity":
		return math.Inf(1), true
	case "nan":
		return math.NaN(), true
	}
	return 0, false
}

// stripFloatUnderscores removes PEP 515 underscores from a numeric
// literal. Underscores are only legal between digits; adjacent to a
// sign, decimal point, exponent marker, or another underscore is a
// parse error.
//
// CPython: Objects/floatobject.c:222 _Py_string_to_number_with_underscores
func stripFloatUnderscores(s string) (string, bool) {
	if !strings.Contains(s, "_") {
		return s, true
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '_' {
			if i == 0 || i == len(s)-1 {
				return "", false
			}
			if !isFloatDigit(s[i-1]) || !isFloatDigit(s[i+1]) {
				return "", false
			}
			continue
		}
		b.WriteByte(c)
	}
	return b.String(), true
}

// isFloatDigit reports whether the byte is a decimal digit. The PEP
// 515 underscore rule for floats is "digit on both sides" only;
// hex floats aren't part of float() input.
//
// CPython: Objects/floatobject.c:222 _Py_string_to_number_with_underscores
func isFloatDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

func fmtFloatInvalid(raw string) error {
	return fmt.Errorf("%w: %q", errFloatInvalid, raw)
}
