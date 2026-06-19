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
	"fmt"
	"math"
	"math/big"
	"strings"
	"unicode/utf8"

	"github.com/tamnd/gopy/pystrconv"
)

// ErrInvalidSpec is returned by ParseSpec when the spec violates the
// grammar.
var ErrInvalidSpec = errors.New("format: invalid format specifier")

// ErrInvalidSpecifier is returned by ParseSpec when more than one
// character remains for the type field, i.e. the spec is unparseable.
// CPython raises this with the offending object's type name appended,
// so callers (which hold the object) wrap it into the full
// "Invalid format specifier '<spec>' for object of type '<type>'"
// ValueError.
//
// CPython: Python/formatter_unicode.c:305 (end-pos > 1 branch)
var ErrInvalidSpecifier = errors.New("format: invalid format specifier (trailing characters)")

// ErrTooManyDigits mirrors the "Too many decimal digits in format
// string" ValueError get_integer raises when a width or precision digit
// run overflows the platform integer.
//
// CPython: Python/formatter_unicode.c:78 Too many decimal digits
var ErrTooManyDigits = errors.New("Too many decimal digits in format string") //nolint:staticcheck // Mirror CPython error text.

// ErrPrecisionTooBig mirrors the "precision too big" ValueError raised
// when the requested float/complex precision exceeds INT_MAX.
//
// CPython: Python/formatter_unicode.c:1166 precision too big
var ErrPrecisionTooBig = errors.New("precision too big") //nolint:staticcheck // Mirror CPython error text.

// ErrMissingPrecision mirrors the "Format specifier missing precision"
// ValueError raised when a '.' is not followed by a precision or a
// fractional grouping separator.
//
// CPython: Python/formatter_unicode.c:296 Format specifier missing precision
var ErrMissingPrecision = errors.New("Format specifier missing precision") //nolint:staticcheck // Mirror CPython error text.

// errCommaAndUnderscore mirrors invalid_comma_and_underscore from
// formatter_unicode.c: the user requested both grouping styles in the
// same spec.
//
// CPython: Python/formatter_unicode.c:47 invalid_comma_and_underscore
var errCommaAndUnderscore = errors.New("Cannot specify both ',' and '_'.") //nolint:staticcheck // Mirror CPython error text.

// invalidThousandsSeparator mirrors invalid_thousands_separator_type:
// the grouping character is incompatible with the presentation type.
// The %c/\\x%x split matches CPython's PyErr_Format branches so the
// rendered ValueError reads identically.
//
// CPython: Python/formatter_unicode.c:33 invalid_thousands_separator_type
func invalidThousandsSeparator(sep byte, t byte) error {
	if t > 32 && t < 128 {
		return fmt.Errorf("Cannot specify '%c' with '%c'.", sep, t) //nolint:staticcheck // Mirror CPython error text.
	}
	return fmt.Errorf("Cannot specify '%c' with '\\x%x'.", sep, uint(t)) //nolint:staticcheck // Mirror CPython error text.
}

// validateThousands enforces the PEP 378/PEP 515 type/grouping pairing
// rules. It is a 1:1 port of the post-parse switch in
// parse_internal_render_format_spec: 'd'/'e'/'f'/'g'/'E'/'G'/'%'/'F'/0
// allow either separator; 'b'/'o'/'x'/'X' allow only underscore (with
// a 4-digit group); everything else is rejected with the codec-style
// message.
//
// CPython: Python/formatter_unicode.c:331 thousands-separator switch
func validateThousands(thousands, t byte) error {
	if thousands == 0 {
		return nil
	}
	switch t {
	case 'd', 'e', 'f', 'g', 'E', 'G', '%', 'F', 0:
		return nil
	case 'b', 'o', 'x', 'X':
		if thousands == '_' {
			return nil
		}
	}
	return invalidThousandsSeparator(thousands, t)
}

// Spec is the parsed form of a format-spec mini-language string.
//
// CPython: Python/formatter_unicode.c:L18 InternalFormatSpec
type Spec struct {
	Fill          rune // -1 means unspecified (allows '\x00' as a real fill char)
	Align         byte // '<', '>', '=', '^', or 0 if default
	Sign          byte // '+', '-', ' ', or 0
	NoNegZero     bool
	Alt           bool
	Zero          bool
	Width         int  // -1 if unspecified
	Thousands     byte // ',', '_', or 0
	FracThousands byte // ',', '_', or 0 (CPython 3.14 frac_thousands_separator)
	Precision     int  // -1 if unspecified
	Type          byte // 0 if unspecified
}

func isAlignToken(c rune) bool {
	return c == '<' || c == '>' || c == '=' || c == '^'
}

func isSignToken(c rune) bool {
	return c == '+' || c == '-' || c == ' '
}

// ParseSpec mirrors parse_internal_render_format_spec.
//
// The spec is decoded to code points up front: every grammar element
// except the fill character is ASCII, but the fill character may be any
// code point (e.g. a multi-byte alignment fill like "🖤>6"), so the
// parser must index by rune, not byte, exactly like CPython's
// READ_spec over a PyUnicode buffer.
//
// CPython: Python/formatter_unicode.c:L150 parse_internal_render_format_spec
func ParseSpec(s string) (Spec, error) {
	spec := Spec{Fill: -1, Width: -1, Precision: -1}
	r := []rune(s)

	i := 0
	// [[fill]align]
	if len(r)-i >= 2 && isAlignToken(r[i+1]) {
		spec.Fill = r[i]
		spec.Align = byte(r[i+1])
		i += 2
	} else if len(r)-i >= 1 && isAlignToken(r[i]) {
		spec.Align = byte(r[i])
		i++
	}

	// [sign]
	if i < len(r) && isSignToken(r[i]) {
		spec.Sign = byte(r[i])
		i++
	}

	// [z] - coerce -0.0 to 0.0 for floats
	if i < len(r) && r[i] == 'z' {
		spec.NoNegZero = true
		i++
	}

	// [#]
	if i < len(r) && r[i] == '#' {
		spec.Alt = true
		i++
	}

	// [0] zero-pad shortcut
	// CPython: Python/formatter_unicode.c:213 !fill_char_specified
	// Always sets fill='0'; only sets align='=' if align was not explicit.
	if i < len(r) && r[i] == '0' && spec.Fill == -1 {
		spec.Fill = '0'
		if spec.Align == 0 {
			spec.Zero = true
			spec.Align = '='
		}
		i++
	}

	// [width]
	width, consumed, err := parseInt(r, i)
	if err != nil {
		return spec, err
	}
	if consumed > 0 {
		spec.Width = width
		i += consumed
	}

	// [grouping]: comma + underscore are parsed in stages so the
	// distinct "Cannot specify both" / "Cannot specify '_' with '_'."
	// messages can fire.
	//
	// CPython: Python/formatter_unicode.c:236 grouping section
	if i < len(r) && r[i] == ',' {
		spec.Thousands = ','
		i++
	}
	if i < len(r) && r[i] == '_' {
		if spec.Thousands != 0 {
			return spec, errCommaAndUnderscore
		}
		spec.Thousands = '_'
		i++
	}
	if i < len(r) && r[i] == ',' {
		if spec.Thousands == '_' {
			return spec, errCommaAndUnderscore
		}
		// Leave the comma in place; it becomes the type and is
		// rejected by validateThousands with the codec-style
		// "Cannot specify ',' with ','." message.
	}

	// [.precision][frac_thousands]
	// CPython: Python/formatter_unicode.c:257 Parse field precision
	// CPython 3.14: Python/formatter_unicode.c:265 frac_thousands_separator
	if i < len(r) && r[i] == '.' {
		i++
		prec, consumed, err := parseInt(r, i)
		if err != nil {
			return spec, err
		}
		if consumed > 0 {
			spec.Precision = prec
			i += consumed
		}
		// Optional frac thousands separator after the (optional)
		// precision. CPython parses the comma and the underscore in two
		// separate `if` blocks (not else-if) so a "comma then
		// underscore" run (e.g. ".,_f") hits the
		// invalid_comma_and_underscore branch instead of silently
		// taking only the comma.
		//
		// CPython: Python/formatter_unicode.c:266 frac comma/underscore
		if i < len(r) && r[i] == ',' {
			if consumed == 0 {
				spec.Precision = -1
			}
			spec.FracThousands = ','
			i++
			consumed++
		}
		if i < len(r) && r[i] == '_' {
			if spec.FracThousands != 0 {
				return spec, errCommaAndUnderscore
			}
			if consumed == 0 {
				spec.Precision = -1
			}
			spec.FracThousands = '_'
			i++
			consumed++
		}
		// Trailing comma after underscore → error
		if i < len(r) && r[i] == ',' && spec.FracThousands == '_' {
			return spec, errCommaAndUnderscore
		}
		if consumed == 0 {
			return spec, ErrMissingPrecision
		}
	}

	// [type]
	if i < len(r) {
		spec.Type = byte(r[i])
		i++
	}

	// More than one character remains for the type field: the spec is
	// invalid. CPython raises "Invalid format specifier '<spec>' for
	// object of type '<type>'"; ParseSpec is type-agnostic, so it
	// returns the sentinel and the caller appends the type name.
	//
	// CPython: Python/formatter_unicode.c:305 (end-pos > 1 branch)
	if i != len(r) {
		return spec, ErrInvalidSpecifier
	}
	if spec.Type != 0 {
		if err := validateThousands(spec.Thousands, spec.Type); err != nil {
			return spec, err
		}
	}
	// CPython: Python/formatter_unicode.c:362 frac_thousands with 'n' is invalid
	if spec.FracThousands != 0 && spec.Type == 'n' {
		return spec, invalidThousandsSeparator(spec.FracThousands, spec.Type)
	}
	return spec, nil
}

// parseInt ports get_integer: it consumes a run of decimal digits and
// accumulates them as a (signed) integer, detecting overflow before it
// happens. Overflow past the platform word size raises "Too many
// decimal digits in format string" rather than a generic invalid-spec
// error, matching CPython's PY_SSIZE_T_MAX guard.
//
// CPython: Python/formatter_unicode.c:61 get_integer
func parseInt(r []rune, i int) (val, consumed int, err error) {
	start := i
	acc := 0
	for i < len(r) && r[i] >= '0' && r[i] <= '9' {
		d := int(r[i] - '0')
		if acc > (math.MaxInt-d)/10 {
			return 0, i - start, ErrTooManyDigits
		}
		acc = acc*10 + d
		i++
	}
	if i == start {
		return 0, 0, nil
	}
	return acc, i - start, nil
}

// FormatString renders s under spec. Mirrors format_string_internal.
//
// CPython: Python/formatter_unicode.c:848 format_string_internal
func FormatString(s string, spec Spec) (string, error) {
	t := spec.Type
	if t == 0 {
		t = 's'
	}
	if err := validateThousands(spec.Thousands, t); err != nil {
		return "", err
	}
	// CPython: Python/formatter_unicode.c:888 negative-0 coercion is not
	// allowed on strings.
	if spec.NoNegZero {
		return "", errors.New("Negative zero coercion (z) not allowed in string format specifier") //nolint:staticcheck // Mirror CPython error text.
	}
	if spec.Sign == ' ' {
		return "", fmt.Errorf("ValueError: Space not allowed in string format specifier")
	}
	if spec.Sign != 0 {
		return "", fmt.Errorf("ValueError: Sign not allowed in string format specifier")
	}
	if spec.Alt {
		return "", fmt.Errorf("ValueError: Alternate form (#) not allowed in string format specifier")
	}
	if spec.Type != 0 && spec.Type != 's' {
		return "", ErrInvalidSpec
	}
	if spec.Align == '=' && !spec.Zero {
		return "", fmt.Errorf("ValueError: '=' alignment not allowed in string format specification")
	}
	body := s
	if spec.Precision >= 0 && spec.Precision < utf8.RuneCountInString(body) {
		body = truncateRunes(body, spec.Precision)
	}
	align := spec.Align
	// The '0' shortcut sets align='=' (numeric zero-fill convention), but
	// for strings the default alignment is '<'. If the caller wrote just
	// "0Ns" without an explicit align character, the '=' here was
	// synthetic; treat it as left-align so "08s" on "result" gives
	// "result00" not "00result".
	//
	// CPython: Python/formatter_unicode.c:862 format_string_internal (align default)
	if align == 0 || (spec.Zero && align == '=') {
		align = '<'
	}
	fill := spec.Fill
	if fill < 0 {
		fill = ' '
	}
	return pad(body, spec.Width, align, fill), nil
}

// truncateRunes returns the prefix of s containing at most n code
// points, mirroring CPython's "Truncate to the precision" step on
// string targets where precision counts characters, not bytes.
//
// CPython: Python/formatter_unicode.c:872 format_string_internal
func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	count := 0
	for i := range s {
		if count == n {
			return s[:i]
		}
		count++
	}
	return s
}

func pad(body string, width int, align byte, fill rune) string {
	bodyLen := utf8.RuneCountInString(body)
	if width <= bodyLen {
		return body
	}
	missing := width - bodyLen
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
	// CPython: Python/formatter_unicode.c:1370 parse_internal_render_format_spec
	// rejects the 'z' coerce-negative-zero modifier on integer types.
	if spec.NoNegZero {
		return "", errors.New("Negative zero coercion (z) not allowed with integer format specifier") //nolint:staticcheck // Mirror CPython error text.
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
		// CPython: Python/formatter_unicode.c:1020 format_long_internal ('c' branch)
		// rejects '+' / ' ' sign with the character format and only allows '-'
		// (the default for ints).
		if spec.Sign != 0 && spec.Sign != '-' {
			return "", errors.New("Sign not allowed with integer format specifier 'c'") //nolint:staticcheck // Mirror CPython error text.
		}
		if spec.Alt {
			return "", errors.New("Alternate form (#) not allowed with integer format specifier 'c'") //nolint:staticcheck // Mirror CPython error text.
		}
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
		if err := validateThousands(spec.Thousands, t); err != nil {
			return "", err
		}
		groupSize := 3
		if t == 'b' || t == 'o' || t == 'x' || t == 'X' {
			groupSize = 4
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
	if f < 0 {
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
// CPython: Python/formatter_unicode.c:1290 format_float_internal
func FormatFloat(v float64, spec Spec) (string, error) {
	t := spec.Type
	if t == 0 {
		t = 'r'
	}
	// 'r' is gopy's internal stand-in for CPython's "no type" float
	// path. validateThousands matches CPython by treating type==0 as
	// allowed, so canonicalize before the check.
	checkType := t
	if t == 'r' {
		checkType = 0
	}
	if err := validateThousands(spec.Thousands, checkType); err != nil {
		return "", err
	}

	// A precision wider than a C int cannot be honoured by the digit
	// generator. CPython rejects it up front with "precision too big".
	//
	// CPython: Python/formatter_unicode.c:1165 precision > INT_MAX
	if spec.Precision > math.MaxInt32 {
		return "", ErrPrecisionTooBig
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
	case 'n':
		// 'n' is 'g' plus LC_NUMERIC grouping. Render through the 'g'
		// path; locale grouping is applied via spec.Thousands (empty in
		// the C locale, so the digits come out ungrouped).
		//
		// CPython: Python/formatter_unicode.c:1290 format_float_internal ('n')
		if precision < 0 {
			precision = 6
		} else if precision == 0 {
			precision = 1
		}
		t = 'g'
	case '%':
		if precision < 0 {
			precision = 6
		}
	case 'r':
		// CPython: Python/formatter_unicode.c:1176 format_float_internal
		// ADD_DOT_0 is always set for type=='\0', regardless of precision.
		flags |= pystrconv.FlagAddDotZero
		if precision >= 0 {
			t = 'g'
			if precision == 0 {
				precision = 1
			}
		}
	default:
		return "", ErrInvalidSpec
	}

	body := pystrconv.FormatFloat(v, t, precision, flags)

	// Grouping for the integer part of finite numbers.
	if spec.Thousands != 0 && !math.IsNaN(v) && !math.IsInf(v, 0) {
		body = applyFloatGrouping(body, spec.Thousands)
	}

	// Grouping for the fractional part (CPython 3.14 frac_thousands_separator).
	if spec.FracThousands != 0 && !math.IsNaN(v) && !math.IsInf(v, 0) {
		body = applyFracGrouping(body, spec.FracThousands)
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

// insertFracGrouping inserts sep every 3 fractional digits, left-to-right.
// Fractional grouping goes left-to-right so the trailing group is the
// residual (unlike integer grouping where the leading group is residual).
//
// CPython: Python/formatter_unicode.c:686 _PyUnicode_InsertThousandsGrouping
// (frac_thousands_separator path, 3.14+)
func insertFracGrouping(fracPart string, sep byte) string {
	if len(fracPart) <= 3 {
		return fracPart
	}
	var b strings.Builder
	b.Grow(len(fracPart) + (len(fracPart)-1)/3)
	for i := 0; i < len(fracPart); i += 3 {
		if i > 0 {
			b.WriteByte(sep)
		}
		b.WriteString(fracPart[i:min(i+3, len(fracPart))])
	}
	return b.String()
}

// applyFracGrouping applies insertFracGrouping to the fractional portion of
// body. Handles sign prefix, integer part, dot, fractional digits, and an
// optional exponent or trailing '%'.
//
// CPython: Python/formatter_unicode.c:1308 format_float_internal
// (frac_thousands_separator application, 3.14+)
func applyFracGrouping(body string, sep byte) string {
	dotIdx := strings.IndexByte(body, '.')
	if dotIdx < 0 {
		return body
	}
	fracStart := dotIdx + 1
	fracEnd := fracStart
	for fracEnd < len(body) && body[fracEnd] >= '0' && body[fracEnd] <= '9' {
		fracEnd++
	}
	fracPart := body[fracStart:fracEnd]
	if len(fracPart) <= 3 {
		return body
	}
	grouped := insertFracGrouping(fracPart, sep)
	return body[:fracStart] + grouped + body[fracEnd:]
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
