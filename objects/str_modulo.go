// PyUnicode_Format ports the printf-style "%s %d" % (a, b) operator
// from CPython. The conversion characters s/r/a/d/i/u/o/x/X/e/E/f/F/g/G/c
// are supported, along with the flags - + space # 0, numeric width and
// precision (literal or "*"), and the "%(name)s" mapping syntax.
//
// CPython: Objects/unicodeobject.c:15500 PyUnicode_Format
// CPython: Objects/unicodeobject.c:14538 unicode_mod (the nb_remainder slot)

package objects

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
)

func init() {
	strType.Number = &NumberMethods{
		Remainder: unicodeModulo,
	}
}

// Format flag bits. Mirror CPython's F_LJUST/F_SIGN/F_BLANK/F_ALT/F_ZERO.
//
// CPython: Objects/unicodeobject.c:14638 (F_* macros)
const (
	fmtLJust = 1 << iota
	fmtSign
	fmtBlank
	fmtAlt
	fmtZero
)

// fmtArg pairs the parsed conversion spec with the per-arg sign flag.
//
// CPython: Objects/unicodeobject.c:14657 unicode_format_arg_t
type fmtArg struct {
	ch       rune
	flags    int
	width    int
	prec     int
	signable bool
}

// fmtCtx is the % walker's working state. Mirrors unicode_formatter_t,
// including the writer indirection: we accumulate into UnicodeWriter
// (spec 1712 P15.2) so Finish builds a *Unicode with kind / maxchar
// populated, skipping a second classify walk.
//
// CPython: Objects/unicodeobject.c:14643 unicode_formatter_t
type fmtCtx struct {
	fmt      []rune
	pos      int
	args     Object
	argTuple []Object
	argIdx   int
	argLen   int
	dict     Object
	owned    bool
}

// unicodeModulo is str's nb_remainder slot. Treats the lhs as a format
// string and the rhs as the argument set, returning the formatted str.
//
// CPython: Objects/unicodeobject.c:14538 unicode_mod
func unicodeModulo(a, b Object) (Object, error) {
	fmtStr, ok := a.(*Unicode)
	if !ok {
		return notImplemented(), nil
	}

	ctx := fmtCtx{fmt: []rune(fmtStr.v), args: b}
	switch t := b.(type) {
	case *Tuple:
		ctx.argTuple = make([]Object, t.Len())
		for i := 0; i < t.Len(); i++ {
			ctx.argTuple[i] = t.Item(i)
		}
		ctx.argLen = t.Len()
	default:
		ctx.argLen = -1
	}
	if d, ok := b.(*Dict); ok {
		ctx.dict = d
	}

	var out UnicodeWriter
	out.Init()
	out.overallocate = true
	// Pre-size: the literal portion of the format string plus a small
	// reserve for substitutions. PrepareInternal grows on demand.
	if err := out.PrepareInternal(len(fmtStr.v)+16, 127); err != nil {
		return nil, err
	}

	for ctx.pos < len(ctx.fmt) {
		ch := ctx.fmt[ctx.pos]
		if ch != '%' {
			if err := out.WriteChar(ch); err != nil {
				return nil, err
			}
			ctx.pos++
			continue
		}
		ctx.pos++
		if ctx.pos >= len(ctx.fmt) {
			return nil, fmt.Errorf("ValueError: incomplete format")
		}
		if ctx.fmt[ctx.pos] == '%' {
			if err := out.WriteChar('%'); err != nil {
				return nil, err
			}
			ctx.pos++
			continue
		}
		if err := formatOneArg(&ctx, &out); err != nil {
			return nil, err
		}
	}

	if ctx.dict == nil && ctx.argIdx < ctx.argLen {
		return nil, fmt.Errorf("TypeError: not all arguments converted during string formatting")
	}
	return out.Finish(), nil
}

// formatOneArg parses one %-spec at ctx.pos and writes the formatted
// argument into out. Mirrors unicode_format_arg.
//
// CPython: Objects/unicodeobject.c:15455 unicode_format_arg
func formatOneArg(ctx *fmtCtx, out *UnicodeWriter) error {
	arg := fmtArg{width: -1, prec: -1}

	if ctx.fmt[ctx.pos] == '(' {
		if ctx.dict == nil {
			return fmt.Errorf("TypeError: format requires a mapping")
		}
		ctx.pos++
		start := ctx.pos
		depth := 1
		for ctx.pos < len(ctx.fmt) && depth > 0 {
			switch ctx.fmt[ctx.pos] {
			case '(':
				depth++
			case ')':
				depth--
			}
			if depth == 0 {
				break
			}
			ctx.pos++
		}
		if ctx.pos >= len(ctx.fmt) || depth != 0 {
			return fmt.Errorf("ValueError: incomplete format key")
		}
		key := string(ctx.fmt[start:ctx.pos])
		ctx.pos++
		v, err := GetItem(ctx.dict, NewStr(key))
		if err != nil {
			return err
		}
		ctx.args = v
		ctx.argTuple = nil
		ctx.argLen = -1
		ctx.argIdx = -2
		ctx.owned = true
	}

	for ctx.pos < len(ctx.fmt) {
		switch ctx.fmt[ctx.pos] {
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
			goto widthParse
		}
		ctx.pos++
	}
widthParse:

	if err := readNumOrStar(ctx, &arg.width); err != nil {
		return err
	}
	if arg.width < 0 && arg.width != -1 {
		arg.flags |= fmtLJust
		arg.width = -arg.width
	}

	if ctx.pos < len(ctx.fmt) && ctx.fmt[ctx.pos] == '.' {
		ctx.pos++
		arg.prec = 0
		if err := readNumOrStar(ctx, &arg.prec); err != nil {
			return err
		}
		if arg.prec < 0 {
			arg.prec = 0
		}
	}

	if ctx.pos < len(ctx.fmt) {
		switch ctx.fmt[ctx.pos] {
		case 'h', 'l', 'L':
			ctx.pos++
		}
	}
	if ctx.pos >= len(ctx.fmt) {
		return fmt.Errorf("ValueError: incomplete format")
	}

	arg.ch = ctx.fmt[ctx.pos]
	ctx.pos++

	v, err := nextArg(ctx)
	if err != nil {
		return err
	}

	body, err := formatBody(&arg, v)
	if err != nil {
		return err
	}

	if err := writePadded(out, &arg, body); err != nil {
		return err
	}

	if ctx.dict != nil && ctx.owned && ctx.argIdx >= 0 && ctx.argIdx < ctx.argLen {
		return fmt.Errorf("TypeError: not all arguments converted during string formatting")
	}
	return nil
}

// readNumOrStar parses a width/precision token. "*" pulls the next arg
// out of ctx and converts it to int; a literal digit run reads as a
// decimal number; anything else leaves *dst untouched.
func readNumOrStar(ctx *fmtCtx, dst *int) error {
	if ctx.pos >= len(ctx.fmt) {
		return nil
	}
	if ctx.fmt[ctx.pos] == '*' {
		ctx.pos++
		v, err := nextArg(ctx)
		if err != nil {
			return err
		}
		i, ok := v.(*Int)
		if !ok {
			return fmt.Errorf("TypeError: * wants int")
		}
		n, fits := i.Int64()
		if !fits {
			return fmt.Errorf("OverflowError: width too big")
		}
		*dst = int(n)
		return nil
	}
	c := ctx.fmt[ctx.pos]
	if c < '0' || c > '9' {
		return nil
	}
	n := 0
	for ctx.pos < len(ctx.fmt) {
		c := ctx.fmt[ctx.pos]
		if c < '0' || c > '9' {
			break
		}
		if n > (1<<31-1-int(c-'0'))/10 {
			return fmt.Errorf("ValueError: width too big")
		}
		n = n*10 + int(c-'0')
		ctx.pos++
	}
	*dst = n
	return nil
}

// nextArg returns the next positional argument. Tuple args advance
// argIdx; a single non-tuple value is returned exactly once.
//
// CPython: Objects/unicodeobject.c:14665 unicode_format_getnextarg
func nextArg(ctx *fmtCtx) (Object, error) {
	if ctx.argLen < 0 {
		if ctx.argIdx > 0 {
			return nil, fmt.Errorf("TypeError: not enough arguments for format string")
		}
		ctx.argIdx++
		return ctx.args, nil
	}
	if ctx.argIdx >= ctx.argLen {
		return nil, fmt.Errorf("TypeError: not enough arguments for format string")
	}
	v := ctx.argTuple[ctx.argIdx]
	ctx.argIdx++
	return v, nil
}

// formatBody renders v according to arg.ch. The returned body has no
// width padding; writePadded handles flags after the fact.
//
// CPython: Objects/unicodeobject.c:15197 unicode_format_arg_format
func formatBody(arg *fmtArg, v Object) (string, error) {
	switch arg.ch {
	case 's':
		s, err := Str(v)
		if err != nil {
			return "", err
		}
		return truncatePrec(s, arg.prec), nil
	case 'r':
		s, err := Repr(v)
		if err != nil {
			return "", err
		}
		return truncatePrec(s, arg.prec), nil
	case 'a':
		s, err := Repr(v)
		if err != nil {
			return "", err
		}
		return truncatePrec(asciiEscape(s), arg.prec), nil
	case 'd', 'i', 'u':
		return formatLong(v, arg, 10)
	case 'o':
		return formatLong(v, arg, 8)
	case 'x':
		return formatLong(v, arg, 16)
	case 'X':
		s, err := formatLong(v, arg, 16)
		if err != nil {
			return "", err
		}
		return strings.ToUpper(s), nil
	case 'e', 'E', 'f', 'F', 'g', 'G':
		arg.signable = true
		return formatFloat(v, arg)
	case 'c':
		return formatChar(v)
	}
	return "", fmt.Errorf(
		"ValueError: unsupported format character '%c' (0x%x)",
		arg.ch, arg.ch)
}

// formatLong renders v in the given base with sign + #-prefix per
// arg.flags. precision controls the minimum digit count.
//
// CPython: Objects/unicodeobject.c:14743 _PyUnicode_FormatLong
// CPython: Objects/unicodeobject.c:14872 mainformatlong
func formatLong(v Object, arg *fmtArg, base int) (string, error) {
	bi, err := numberAsBigInt(v, arg.ch)
	if err != nil {
		return "", err
	}
	arg.signable = true
	neg := bi.Sign() < 0
	mag := new(big.Int).Abs(bi)
	digits := mag.Text(base)
	if arg.prec > len(digits) {
		digits = strings.Repeat("0", arg.prec-len(digits)) + digits
	}
	prefix := ""
	if arg.flags&fmtAlt != 0 {
		switch base {
		case 8:
			prefix = "0o"
		case 16:
			prefix = "0x"
		}
	}
	sign := ""
	switch {
	case neg:
		sign = "-"
	case arg.flags&fmtSign != 0:
		sign = "+"
	case arg.flags&fmtBlank != 0:
		sign = " "
	}
	return sign + prefix + digits, nil
}

// numberAsBigInt coerces v to a big.Int suitable for a numeric
// conversion. 'o', 'x', 'X' demand a true integer; 'd', 'i', 'u'
// accept floats (truncated like CPython's PyNumber_Long).
//
// CPython: Objects/unicodeobject.c:14872 mainformatlong (the index/long branches)
func numberAsBigInt(v Object, ch rune) (*big.Int, error) {
	switch x := v.(type) {
	case *Int:
		return new(big.Int).Set(&x.v), nil
	case *Bool:
		if x == True() {
			return big.NewInt(1), nil
		}
		return big.NewInt(0), nil
	case *Float:
		if ch == 'o' || ch == 'x' || ch == 'X' {
			return nil, fmt.Errorf(
				"TypeError: %%%c format: an integer is required, not %s",
				ch, v.Type().Name)
		}
		if math.IsNaN(x.v) || math.IsInf(x.v, 0) {
			return nil, fmt.Errorf(
				"OverflowError: cannot convert float %s to integer", formatFloatShort(x.v))
		}
		f, _ := new(big.Float).SetFloat64(x.v).Int(nil)
		return f, nil
	}
	tag := "an integer is required"
	if ch == 'd' || ch == 'i' || ch == 'u' {
		tag = "a real number is required"
	}
	return nil, fmt.Errorf("TypeError: %%%c format: %s, not %s",
		ch, tag, v.Type().Name)
}

// formatFloat renders v in the given conversion (e/E/f/F/g/G) at the
// requested precision. nan and inf normalize to the case of arg.ch
// (lowercase 'e' / 'f' / 'g' yield "nan"/"inf"; uppercase yield
// "NAN"/"INF") to match CPython.
//
// CPython: Objects/unicodeobject.c:14688 formatfloat
func formatFloat(v Object, arg *fmtArg) (string, error) {
	f, ok := asFloat(v)
	if !ok {
		return "", fmt.Errorf(
			"TypeError: %%%c format: a real number is required, not %s",
			arg.ch, v.Type().Name)
	}
	prec := arg.prec
	if prec < 0 {
		prec = 6
	}
	upper := arg.ch >= 'A' && arg.ch <= 'Z'
	if math.IsNaN(f) {
		if upper {
			return "NAN", nil
		}
		return "nan", nil
	}
	if math.IsInf(f, 0) {
		s := "inf"
		if upper {
			s = "INF"
		}
		switch {
		case f < 0:
			return "-" + s, nil
		case arg.flags&fmtSign != 0:
			return "+" + s, nil
		case arg.flags&fmtBlank != 0:
			return " " + s, nil
		}
		return s, nil
	}
	verb := byte(arg.ch)
	out := strconv.FormatFloat(f, verb, prec, 64)
	// Go's 'g'/'G' already match CPython for trailing-zero stripping
	// and bare-int presentation, so no post-processing is needed here.
	if f >= 0 {
		switch {
		case arg.flags&fmtSign != 0:
			out = "+" + out
		case arg.flags&fmtBlank != 0:
			out = " " + out
		}
	}
	return out, nil
}

// formatChar renders %c. Accepts a single-char str or an int-valued
// codepoint in [0, 0x110000).
//
// CPython: Objects/unicodeobject.c:14966 formatchar
func formatChar(v Object) (string, error) {
	if u, ok := v.(*Unicode); ok {
		if u.length != 1 {
			return "", fmt.Errorf(
				"TypeError: %%c requires an int or a unicode character, not a string of length %d",
				u.length)
		}
		return u.v, nil
	}
	if i, ok := v.(*Int); ok {
		n, fits := i.Int64()
		if !fits || n < 0 || n > 0x10FFFF {
			return "", fmt.Errorf("OverflowError: %%c arg not in range(0x110000)")
		}
		return string(rune(n)), nil
	}
	if b, ok := v.(*Bool); ok {
		n := 0
		if b == True() {
			n = 1
		}
		return string(rune(n)), nil
	}
	return "", fmt.Errorf(
		"TypeError: %%c requires an int or a unicode character, not %s",
		v.Type().Name)
}

// truncatePrec slices s to at most prec runes (s/r/a precision is
// codepoint-based, not byte-based). prec == -1 means no truncation.
func truncatePrec(s string, prec int) string {
	if prec < 0 {
		return s
	}
	count := 0
	for i := range s {
		if count == prec {
			return s[:i]
		}
		count++
	}
	return s
}

// asciiEscape backslash-escapes every non-ASCII rune in s. Used by the
// 'a' conversion to emulate PyObject_ASCII on top of repr.
//
// CPython: Objects/unicodeobject.c:15233 PyObject_ASCII (repr-then-escape)
func asciiEscape(s string) string {
	allASCII := true
	for _, r := range s {
		if r >= 0x80 {
			allASCII = false
			break
		}
	}
	if allASCII {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for _, r := range s {
		switch {
		case r < 0x80:
			b.WriteRune(r)
		case r < 0x100:
			fmt.Fprintf(&b, "\\x%02x", r)
		case r < 0x10000:
			fmt.Fprintf(&b, "\\u%04x", r)
		default:
			fmt.Fprintf(&b, "\\U%08x", r)
		}
	}
	return b.String()
}

// writePadded applies width and the F_LJUST/F_ZERO flags to body and
// appends the result to out. For numeric specs with F_ZERO, the zero
// padding lives between the sign and the digits.
//
// CPython: Objects/unicodeobject.c:15301 unicode_format_arg_output
func writePadded(out *UnicodeWriter, arg *fmtArg, body string) error {
	width := arg.width
	if width < 0 {
		width = 0
	}
	bodyLen := runeLen(body)
	if bodyLen >= width {
		return writeBodyChunk(out, body)
	}
	pad := width - bodyLen
	switch {
	case arg.flags&fmtLJust != 0:
		if err := writeBodyChunk(out, body); err != nil {
			return err
		}
		return writeRepeat(out, ' ', pad)
	case arg.flags&fmtZero != 0 && arg.signable:
		signEnd := 0
		if body != "" && (body[0] == '-' || body[0] == '+' || body[0] == ' ') {
			signEnd = 1
		}
		prefixEnd := signEnd
		if len(body) >= signEnd+2 && body[signEnd] == '0' &&
			(body[signEnd+1] == 'x' || body[signEnd+1] == 'X' ||
				body[signEnd+1] == 'o') {
			prefixEnd = signEnd + 2
		}
		if err := writeBodyChunk(out, body[:prefixEnd]); err != nil {
			return err
		}
		if err := writeRepeat(out, '0', pad); err != nil {
			return err
		}
		return writeBodyChunk(out, body[prefixEnd:])
	default:
		if err := writeRepeat(out, ' ', pad); err != nil {
			return err
		}
		return writeBodyChunk(out, body)
	}
}

// writeBodyChunk pushes a Go-string chunk through UnicodeWriter, using
// the ASCII fast path when the chunk is all-ASCII to skip rune decode.
func writeBodyChunk(out *UnicodeWriter, s string) error {
	if s == "" {
		return nil
	}
	isASCII := true
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			isASCII = false
			break
		}
	}
	if isASCII {
		return out.WriteASCIIString(s, len(s))
	}
	return out.WriteStr(NewStr(s).(*Unicode))
}

// writeRepeat writes ch n times into out.
func writeRepeat(out *UnicodeWriter, ch rune, n int) error {
	for range n {
		if err := out.WriteChar(ch); err != nil {
			return err
		}
	}
	return nil
}

// runeLen counts code points in s. width and precision in CPython are
// measured in characters, not bytes, so the padding math has to
// match.
func runeLen(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}
