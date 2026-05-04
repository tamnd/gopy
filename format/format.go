// Package format ports cpython/Python/formatter_unicode.c. It
// implements the format-spec mini-language used by str.format,
// f-strings, and the format() builtin:
//
//	[[fill]align][sign][z][#][0][width][grouping][.precision][type]
//
// All four formatters - string, int, float, and (later) complex -
// share the same parser. The float formatter delegates digit
// generation to pystrconv.FormatFloat for IEEE-754 round-trip parity
// with CPython.
package format

import (
	"errors"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/tamnd/gopy/pystrconv"
)

// ErrInvalidSpec is returned by ParseSpec when the spec violates the
// grammar.
var ErrInvalidSpec = errors.New("format: invalid format specifier")

// Spec is the parsed form of a format-spec mini-language string.
//
// CPython: Python/formatter_unicode.c:L18 InternalFormatSpec
type Spec struct {
	Fill      rune
	Align     byte // '<', '>', '=', '^', or 0 if default
	Sign      byte // '+', '-', ' ', or 0
	NoNegZero bool
	Alt       bool
	Zero      bool
	Width     int  // -1 if unspecified
	Thousands byte // ',', '_', or 0
	Precision int  // -1 if unspecified
	Type      byte // 0 if unspecified
}

func isAlignToken(c byte) bool {
	return c == '<' || c == '>' || c == '=' || c == '^'
}

func isSignToken(c byte) bool {
	return c == '+' || c == '-' || c == ' '
}

// ParseSpec mirrors parse_internal_render_format_spec.
//
// CPython: Python/formatter_unicode.c:L150 parse_internal_render_format_spec
func ParseSpec(s string) (Spec, error) {
	spec := Spec{Width: -1, Precision: -1}

	i := 0
	// [[fill]align]
	if len(s)-i >= 2 && isAlignToken(s[i+1]) {
		spec.Fill = rune(s[i])
		spec.Align = s[i+1]
		i += 2
	} else if len(s)-i >= 1 && isAlignToken(s[i]) {
		spec.Align = s[i]
		i++
	}

	// [sign]
	if i < len(s) && isSignToken(s[i]) {
		spec.Sign = s[i]
		i++
	}

	// [z] - coerce -0.0 to 0.0 for floats
	if i < len(s) && s[i] == 'z' {
		spec.NoNegZero = true
		i++
	}

	// [#]
	if i < len(s) && s[i] == '#' {
		spec.Alt = true
		i++
	}

	// [0] zero-pad shortcut
	if i < len(s) && s[i] == '0' && spec.Align == 0 {
		spec.Zero = true
		spec.Fill = '0'
		spec.Align = '='
		i++
	}

	// [width]
	width, consumed, err := parseInt(s, i)
	if err != nil {
		return spec, err
	}
	if consumed > 0 {
		spec.Width = width
		i += consumed
	}

	// [grouping]
	if i < len(s) && (s[i] == ',' || s[i] == '_') {
		spec.Thousands = s[i]
		i++
	}

	// [.precision]
	if i < len(s) && s[i] == '.' {
		i++
		prec, consumed, err := parseInt(s, i)
		if err != nil || consumed == 0 {
			return spec, ErrInvalidSpec
		}
		spec.Precision = prec
		i += consumed
	}

	// [type]
	if i < len(s) {
		spec.Type = s[i]
		i++
	}

	if i != len(s) {
		return spec, ErrInvalidSpec
	}
	return spec, nil
}

func parseInt(s string, i int) (val, consumed int, err error) {
	start := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == start {
		return 0, 0, nil
	}
	v, err := strconv.Atoi(s[start:i])
	if err != nil {
		return 0, 0, ErrInvalidSpec
	}
	return v, i - start, nil
}

// FormatString renders s under spec. Mirrors format_string_internal.
//
// CPython: Python/formatter_unicode.c:L848 format_string_internal
func FormatString(s string, spec Spec) (string, error) {
	if spec.Sign != 0 || spec.Alt || spec.Zero || spec.Thousands != 0 {
		return "", ErrInvalidSpec
	}
	if spec.Type != 0 && spec.Type != 's' {
		return "", ErrInvalidSpec
	}
	body := s
	if spec.Precision >= 0 && spec.Precision < len(body) {
		body = body[:spec.Precision]
	}
	align := spec.Align
	if align == 0 {
		align = '<'
	}
	fill := spec.Fill
	if fill == 0 {
		fill = ' '
	}
	return pad(body, spec.Width, align, fill), nil
}

func pad(body string, width int, align byte, fill rune) string {
	if width <= len(body) {
		return body
	}
	missing := width - len(body)
	fillStr := strings.Repeat(string(fill), missing)
	switch align {
	case '<':
		return body + fillStr
	case '>', '=':
		return fillStr + body
	case '^':
		left := missing / 2
		right := missing - left
		return strings.Repeat(string(fill), left) + body + strings.Repeat(string(fill), right)
	}
	return fillStr + body
}

// FormatInt renders v under spec. Mirrors format_long_internal.
//
// CPython: Python/formatter_unicode.c:L959 format_long_internal
func FormatInt(v *big.Int, spec Spec) (string, error) {
	t := spec.Type
	if t == 0 {
		t = 'd'
	}
	if spec.Precision != -1 && t != 'c' {
		// Precision is only valid for non-numeric types when formatting ints.
		return "", ErrInvalidSpec
	}

	var digits string
	var prefix string
	switch t {
	case 'd', 'n':
		digits = absBigInt(v).Text(10)
	case 'b':
		digits = absBigInt(v).Text(2)
		if spec.Alt {
			prefix = "0b"
		}
	case 'o':
		digits = absBigInt(v).Text(8)
		if spec.Alt {
			prefix = "0o"
		}
	case 'x':
		digits = absBigInt(v).Text(16)
		if spec.Alt {
			prefix = "0x"
		}
	case 'X':
		digits = strings.ToUpper(absBigInt(v).Text(16))
		if spec.Alt {
			prefix = "0X"
		}
	case 'c':
		if !v.IsInt64() {
			return "", ErrInvalidSpec
		}
		r := rune(v.Int64())
		if r < 0 || r > 0x10FFFF {
			return "", ErrInvalidSpec
		}
		return pad(string(r), spec.Width, defaultStringAlign(spec.Align), defaultFill(spec.Fill, ' ')), nil
	default:
		return "", ErrInvalidSpec
	}

	if spec.Thousands != 0 {
		groupSize := 3
		if t == 'b' || t == 'o' || t == 'x' || t == 'X' {
			groupSize = 4
			if spec.Thousands == ',' {
				return "", ErrInvalidSpec
			}
		}
		digits = insertGrouping(digits, spec.Thousands, groupSize)
	}

	sign := signString(v.Sign(), spec.Sign)
	body := sign + prefix + digits

	if spec.Zero && spec.Align == '=' {
		// Pad zeros between prefix and digits, respecting width.
		if spec.Width > len(body) {
			pad := spec.Width - len(body)
			if spec.Thousands != 0 {
				digits = zeroPadGrouped(digits, pad, spec.Thousands, groupingForType(t))
				body = sign + prefix + digits
			} else {
				digits = strings.Repeat("0", pad) + digits
				body = sign + prefix + digits
			}
		}
	}

	return pad(body, spec.Width, defaultNumericAlign(spec.Align), defaultFill(spec.Fill, ' ')), nil
}

func defaultStringAlign(a byte) byte {
	if a == 0 {
		return '<'
	}
	return a
}

func defaultNumericAlign(a byte) byte {
	if a == 0 {
		return '>'
	}
	return a
}

func defaultFill(f rune, dflt rune) rune {
	if f == 0 {
		return dflt
	}
	return f
}

func absBigInt(v *big.Int) *big.Int {
	out := new(big.Int).Abs(v)
	return out
}

func signString(sign int, mode byte) string {
	if sign < 0 {
		return "-"
	}
	switch mode {
	case '+':
		return "+"
	case ' ':
		return " "
	}
	return ""
}

func groupingForType(t byte) int {
	switch t {
	case 'b', 'o', 'x', 'X':
		return 4
	}
	return 3
}

// insertGrouping inserts sep every groupSize digits from the right.
//
// CPython: Python/formatter_unicode.c:L671 _PyUnicode_InsertThousandsGrouping
func insertGrouping(digits string, sep byte, groupSize int) string {
	if len(digits) <= groupSize {
		return digits
	}
	first := len(digits) % groupSize
	if first == 0 {
		first = groupSize
	}
	var b strings.Builder
	b.Grow(len(digits) + (len(digits)-1)/groupSize)
	b.WriteString(digits[:first])
	for i := first; i < len(digits); i += groupSize {
		b.WriteByte(sep)
		b.WriteString(digits[i : i+groupSize])
	}
	return b.String()
}

// zeroPadGrouped left-pads digits with zeros to add `pad` more
// characters total (digits + separators), respecting groupSize.
func zeroPadGrouped(digits string, pad int, sep byte, groupSize int) string {
	// Count current length and add zeros + separators until we reach
	// the target. This is a simplified version that produces a
	// canonically grouped result.
	target := len(digits) + pad
	stripped := strings.ReplaceAll(digits, string(sep), "")
	// Add zeros up front, then re-group.
	for {
		regrouped := insertGrouping(stripped, sep, groupSize)
		if len(regrouped) >= target {
			return regrouped
		}
		stripped = "0" + stripped
	}
}

// FormatFloat renders v under spec. Mirrors format_float_internal.
//
// CPython: Python/formatter_unicode.c:L1290 format_float_internal
func FormatFloat(v float64, spec Spec) (string, error) {
	t := spec.Type
	if t == 0 {
		t = 'r'
	}

	precision := spec.Precision
	flags := pystrconv.FloatFormatFlag(0)
	switch spec.Sign {
	case '+':
		flags |= pystrconv.FlagAlwaysSign
	case ' ':
		flags |= pystrconv.FlagSpaceSign
	}
	if spec.Alt {
		flags |= pystrconv.FlagAlternate
	}
	if spec.NoNegZero {
		flags |= pystrconv.FlagNoNegZero
	}

	switch t {
	case 'e', 'E', 'f', 'F':
		if precision < 0 {
			precision = 6
		}
	case 'g', 'G':
		if precision < 0 {
			precision = 6
		} else if precision == 0 {
			precision = 1
		}
	case '%':
		if precision < 0 {
			precision = 6
		}
	case 'r':
		// repr: AddDotZero, precision unused.
		flags |= pystrconv.FlagAddDotZero
	default:
		return "", ErrInvalidSpec
	}

	body := pystrconv.FormatFloat(v, t, precision, flags)

	// Grouping for the integer part of finite numbers.
	if spec.Thousands != 0 && !math.IsNaN(v) && !math.IsInf(v, 0) {
		body = applyFloatGrouping(body, spec.Thousands)
	}

	if spec.Zero && spec.Align == '=' && spec.Width > len(body) {
		// Pad zeros after the sign/prefix.
		body = zeroPadFloat(body, spec.Width, spec.Thousands)
	}

	return pad(body, spec.Width, defaultNumericAlign(spec.Align), defaultFill(spec.Fill, ' ')), nil
}

func applyFloatGrouping(body string, sep byte) string {
	// Locate the integer part: skip optional sign, then digits up to
	// '.', 'e', 'E', or end.
	i := 0
	if i < len(body) && (body[i] == '+' || body[i] == '-' || body[i] == ' ') {
		i++
	}
	start := i
	for i < len(body) && body[i] >= '0' && body[i] <= '9' {
		i++
	}
	intPart := body[start:i]
	rest := body[i:]
	grouped := insertGrouping(intPart, sep, 3)
	return body[:start] + grouped + rest
}

func zeroPadFloat(body string, width int, sep byte) string {
	if len(body) >= width {
		return body
	}
	// Find sign prefix (none, "+", "-", " ").
	prefix := ""
	rest := body
	if rest != "" && (rest[0] == '+' || rest[0] == '-' || rest[0] == ' ') {
		prefix = body[:1]
		rest = body[1:]
	}
	pad := width - len(body)
	if sep != 0 {
		// Re-group to honor the separator.
		// Locate integer part of rest.
		i := 0
		for i < len(rest) && (rest[i] >= '0' && rest[i] <= '9' || rest[i] == sep) {
			i++
		}
		intPart := strings.ReplaceAll(rest[:i], string(sep), "")
		tail := rest[i:]
		for len(prefix)+groupedLen(intPart)+len(tail) < width {
			intPart = "0" + intPart
		}
		return prefix + insertGrouping(intPart, sep, 3) + tail
	}
	return prefix + strings.Repeat("0", pad) + rest
}

func groupedLen(intPart string) int {
	if len(intPart) <= 3 {
		return len(intPart)
	}
	return len(intPart) + (len(intPart)-1)/3
}
