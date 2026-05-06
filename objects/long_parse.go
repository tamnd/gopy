// Big-int parser used by int(s, base). Mirrors PyLong_FromString:
// optional leading/trailing whitespace, optional sign, optional 0x /
// 0o / 0b prefix when base is 0 or matches, PEP 515 underscores
// between digits, and arbitrary precision through math/big.
//
// CPython: Objects/longobject.c:2693 PyLong_FromString

package objects

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// errIntInvalid is the shape int(s) raises when the literal cannot be
// parsed under the requested base.
//
// CPython: Objects/longobject.c:2693 PyLong_FromString (error path)
var errIntInvalid = errors.New("ValueError: invalid literal for int()")

// IntFromString mirrors PyLong_FromString. base==0 triggers prefix
// detection (0x, 0o, 0b, or decimal). Otherwise base must be in 2..36
// and a matching prefix is allowed but optional.
//
// CPython: Objects/longobject.c:2693 PyLong_FromString
func IntFromString(s string, base int) (*Int, error) {
	if base != 0 && (base < 2 || base > 36) {
		return nil, fmt.Errorf("ValueError: int() base must be >= 2 and <= 36, or 0")
	}

	rest := strings.TrimSpace(s)
	if rest == "" {
		return nil, fmtIntInvalid(base, s)
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
		return nil, fmtIntInvalid(base, s)
	}

	rest, base, ok := stripBasePrefix(rest, base)
	if !ok {
		return nil, fmtIntInvalid(base, s)
	}
	if base == 0 {
		base = 10
	}

	// PEP 515: underscores allowed between digits, never adjacent to
	// the prefix or the start/end of the digit run. After prefix
	// stripping, rest is purely digits with optional underscores.
	digits, ok := stripIntUnderscores(rest)
	if !ok || digits == "" {
		return nil, fmtIntInvalid(base, s)
	}

	v := new(big.Int)
	if _, ok := v.SetString(digits, base); !ok {
		return nil, fmtIntInvalid(base, s)
	}
	if negative {
		v.Neg(v)
	}
	return NewIntFromBig(v), nil
}

// stripBasePrefix peels 0x / 0o / 0b when the base allows it. Returns
// the trimmed rest and the resolved base. ok is false when the
// requested base disagrees with the prefix or when the prefix has no
// digits after it.
//
// CPython: Objects/longobject.c:2735 long_from_string_base
func stripBasePrefix(rest string, base int) (out string, resolved int, ok bool) {
	if len(rest) < 2 || rest[0] != '0' {
		return rest, base, true
	}
	prefixBase := 0
	switch rest[1] {
	case 'x', 'X':
		prefixBase = 16
	case 'o', 'O':
		prefixBase = 8
	case 'b', 'B':
		prefixBase = 2
	default:
		return rest, base, true
	}
	if base != 0 && base != prefixBase {
		return rest, base, true
	}
	rest = rest[2:]
	if rest == "" {
		return rest, prefixBase, false
	}
	return rest, prefixBase, true
}

// stripIntUnderscores removes PEP 515 underscores, enforcing the
// digit-on-both-sides rule. Underscores at the boundary or doubled up
// are rejected.
//
// CPython: Objects/longobject.c:2812 _PyLong_FromString underscore loop
func stripIntUnderscores(s string) (string, bool) {
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
			if s[i-1] == '_' || s[i+1] == '_' {
				return "", false
			}
			continue
		}
		b.WriteByte(c)
	}
	return b.String(), true
}

func fmtIntInvalid(base int, raw string) error {
	return fmt.Errorf("%w with base %d: %q", errIntInvalid, base, raw)
}
