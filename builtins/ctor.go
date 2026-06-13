// Port of the constructor wrappers from Python/bltinmodule.c. The
// type singletons (PyLong_Type, PyFloat_Type, PyBool_Type, PyList_Type,
// PyTuple_Type, PyDict_Type) are exposed as builtins; calling them
// runs the constructor shape CPython exposes through tp_call. v0.7
// covers the call surface that user code relies on; the type
// singletons themselves stay parked until the type port lands.
//
// CPython: Objects/longobject.c long_new, Objects/floatobject.c
// float_new, Objects/boolobject.c bool_new, Objects/listobject.c
// list_init, Objects/tupleobject.c tuple_new, Objects/dictobject.c
// dict_init.

package builtins

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"

	"github.com/tamnd/gopy/abstract"
	"github.com/tamnd/gopy/codecs"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/pystrconv"
)

// IntCtor ports long_new. 0 args returns 0; one positional converts
// via PyNumber_Long; two arguments parse a string in the given base.
// Keyword arguments are limited to {x, base}: anything else is
// TypeError, and base= without an x value is also TypeError (an empty
// int() returns 0, but int(base=0) does not).
//
// CPython: Objects/longobject.c:3389 long_new_impl
func IntCtor(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	// long_new_impl declares x as positional-only (the "/" in the
	// argument clinic), so x= is rejected and any other keyword is
	// TypeError-rejected too. Only base= is accepted by keyword.
	//
	// CPython: Objects/longobject.c:3389 long_new (argument clinic)
	for k := range kwargs {
		if k != "base" {
			return nil, fmt.Errorf("TypeError: 'int' is an invalid keyword argument for int()")
		}
	}
	if base, ok := kwargs["base"]; ok {
		if len(args) == 0 {
			return nil, fmt.Errorf("TypeError: int() missing string argument")
		}
		args = append(args, base)
	}
	if len(args) == 0 {
		return objects.NewInt(0), nil
	}
	if len(args) == 1 {
		return numberToInt(args[0])
	}
	if len(args) == 2 {
		base, err := indexAsBase(args[1])
		if err != nil {
			return nil, err
		}
		// long_new_impl restricts the value to str/bytes/bytearray
		// when a base is given. memoryview and the buffer protocol
		// are not accepted, which test_non_numeric_input_types relies
		// on. CPython: Objects/longobject.c:3469 long_new_impl.
		if objects.IsSubtype(args[0].Type(), objects.StrType()) {
			s, _ := objects.Str(args[0])
			return parseIntStringNormalized(s, base)
		}
		if v, ok := args[0].(*objects.Bytes); ok {
			return parseIntStringFrom(string(v.Bytes()), base, true)
		}
		if v, ok := args[0].(*objects.ByteArray); ok {
			return parseIntStringFrom(string(v.Bytes()), base, true)
		}
		return nil, fmt.Errorf("TypeError: int() can't convert non-string with explicit base")
	}
	return nil, fmt.Errorf("TypeError: int expected at most 2 arguments, got %d", len(args))
}

// indexAsBase normalizes the base argument: an exact int, or anything
// with __index__. Mirrors _PyEval_SliceIndex / PyNumber_Index handling
// in long_new_impl for the base parameter.
//
// CPython: Objects/longobject.c:3464 long_new_impl (PyNumber_Index on base)
func indexAsBase(b objects.Object) (int, error) {
	var n int64
	switch v := b.(type) {
	case *objects.Int:
		nv, fits := v.Int64()
		if !fits {
			return 0, fmt.Errorf("ValueError: int() base must be 0 or 2-36")
		}
		n = nv
	case *objects.Bool:
		nv, _ := v.Int64()
		n = nv
	default:
		num := b.Type().Number
		if num == nil || num.Index == nil {
			return 0, fmt.Errorf("TypeError: int() base must be an integer")
		}
		idx, err := num.Index(b)
		if err != nil {
			return 0, err
		}
		iv, ok := idx.(*objects.Int)
		if !ok {
			return 0, fmt.Errorf("TypeError: __index__ returned non-int (type %s)", idx.Type().Name)
		}
		nv, fits := iv.Int64()
		if !fits {
			return 0, fmt.Errorf("ValueError: int() base must be 0 or 2-36")
		}
		n = nv
	}
	if n != 0 && (n < 2 || n > 36) {
		return 0, fmt.Errorf("ValueError: int() base must be 0 or 2-36")
	}
	return int(n), nil
}

// numberToInt mirrors PyNumber_Long: exact int passes through, then
// try nb_int (__int__), then nb_index (__index__), then string (with
// Unicode normalization), then bytes/buffer. Strict subclasses that
// override __int__ still dispatch through the slot.
//
// CPython: Objects/abstract.c:1571 PyNumber_Long
func numberToInt(o objects.Object) (objects.Object, error) {
	if i, ok := o.(*objects.Int); ok && o.Type() == objects.IntType {
		return i, nil
	}
	if v, ok := o.(*objects.Float); ok && o.Type() == objects.FloatType {
		// CPython: Objects/longobject.c:456 PyLong_FromDouble
		f := v.Float64()
		if math.IsInf(f, 0) {
			return nil, fmt.Errorf("OverflowError: cannot convert float infinity to integer")
		}
		if math.IsNaN(f) {
			return nil, fmt.Errorf("ValueError: cannot convert float NaN to integer")
		}
		out, _ := new(big.Float).SetFloat64(f).Int(nil)
		return objects.NewIntFromBig(out), nil
	}
	if n := o.Type().Number; n != nil {
		if n.Int != nil {
			res, err := n.Int(o)
			if err != nil {
				return nil, err
			}
			return unwrapIntResult(res, "__int__")
		}
		if n.Index != nil {
			res, err := n.Index(o)
			if err != nil {
				return nil, err
			}
			return unwrapIntResult(res, "__index__")
		}
	}
	if objects.IsSubtype(o.Type(), objects.StrType()) {
		s, _ := objects.Str(o)
		return parseIntStringNormalized(s, 10)
	}
	if v, ok := o.(*objects.Bytes); ok {
		return parseIntStringFrom(string(v.Bytes()), 10, true)
	}
	if v, ok := o.(*objects.ByteArray); ok {
		return parseIntStringFrom(string(v.Bytes()), 10, true)
	}
	if buf, ok := objects.AsBytesLike(o); ok {
		return parseIntStringFrom(string(buf), 10, true)
	}
	return nil, fmt.Errorf("TypeError: int() argument must be a string, a bytes-like object or a real number, not '%s'", o.Type().Name)
}

// unwrapIntResult validates that the nb_int / nb_index slot returned
// an int (or int subclass, incl. bool), and downcasts subclass results
// to a plain int with a DeprecationWarning. Mirrors
// _PyLong_FromNbIntOrNbIndex.
//
// CPython: Objects/abstract.c:1546 _PyLong_FromNbIntOrNbIndex
func unwrapIntResult(res objects.Object, slot string) (objects.Object, error) {
	var i *objects.Int
	switch v := res.(type) {
	case *objects.Int:
		i = v
	case *objects.Bool:
		i = &v.Int
	default:
		return nil, fmt.Errorf("TypeError: %s returned non-int (type %s)", slot, res.Type().Name)
	}
	if res.Type() == objects.IntType {
		return i, nil
	}
	// Subclass of int (incl. bool): emit DeprecationWarning and unwrap.
	msg := fmt.Sprintf("%s returned non-int (type %s).  "+
		"The ability to return an instance of a strict subclass of int "+
		"is deprecated, and may be removed in a future version of Python.",
		slot, res.Type().Name)
	if objects.DeprecWarnHook != nil {
		if werr := objects.DeprecWarnHook(msg); werr != nil {
			return nil, werr
		}
	}
	return objects.NewIntFromBig(i.BigInt()), nil
}

// parseIntStringNormalized strips Unicode whitespace and translates
// Unicode decimal digits to ASCII before delegating to parseIntString.
// Mirrors _PyUnicode_TransformDecimalAndSpaceToASCII applied by
// PyLong_FromUnicodeObject.
//
// CPython: Objects/longobject.c:2992 PyLong_FromUnicodeObject,
// Objects/unicodeobject.c:11075 _PyUnicode_TransformDecimalAndSpaceToASCII
func parseIntStringNormalized(s string, base int) (objects.Object, error) {
	return parseIntString(normalizeIntString(s), base)
}

// normalizeIntString converts Unicode whitespace to ASCII space and
// Unicode decimal-digit characters (Nd category) to ASCII '0'-'9'.
// Non-decimal, non-whitespace characters pass through; parseIntString
// catches anything that big.Int.SetString rejects.
//
// CPython: Objects/unicodeobject.c:11075 _PyUnicode_TransformDecimalAndSpaceToASCII
func normalizeIntString(s string) string {
	// Walk the WTF-8 bytes by hand rather than ranging: Go's range loop
	// turns a lone surrogate (stored as 3-byte pseudo-UTF-8) into three
	// U+FFFD runes, which would destroy the original code point before the
	// invalid-literal error message reprs it. Decimal and space code points
	// are folded to ASCII; everything else (surrogates included) passes
	// through with its original bytes intact.
	b := []byte(s)
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); {
		r, size := lenientDecodeRune(b[i:])
		switch {
		case r < 128:
			out = append(out, byte(r))
		case isUnicodeSpace(r):
			out = append(out, ' ')
		case unicodeDecimalValue(r) >= 0:
			out = append(out, '0'+byte(unicodeDecimalValue(r)))
		default:
			out = append(out, b[i:i+size]...)
		}
		i += size
	}
	return string(out)
}

// lenientDecodeRune decodes one code point from WTF-8 bytes, accepting
// lone surrogates stored as 3-byte pseudo-UTF-8 instead of rejecting them
// the way Go's utf8.DecodeRune does. It returns the code point and the
// number of bytes consumed (always at least 1 so callers make progress).
//
// CPython: Objects/unicodeobject.c:1696 find_maxchar_surrogates
func lenientDecodeRune(b []byte) (rune, int) {
	c := b[0]
	switch {
	case c < 0x80:
		return rune(c), 1
	case c&0xE0 == 0xC0 && len(b) >= 2 && b[1]&0xC0 == 0x80:
		return rune(c&0x1F)<<6 | rune(b[1]&0x3F), 2
	case c&0xF0 == 0xE0 && len(b) >= 3 && b[1]&0xC0 == 0x80 && b[2]&0xC0 == 0x80:
		return rune(c&0x0F)<<12 | rune(b[1]&0x3F)<<6 | rune(b[2]&0x3F), 3
	case c&0xF8 == 0xF0 && len(b) >= 4 && b[1]&0xC0 == 0x80 && b[2]&0xC0 == 0x80 && b[3]&0xC0 == 0x80:
		return rune(c&0x07)<<18 | rune(b[1]&0x3F)<<12 | rune(b[2]&0x3F)<<6 | rune(b[3]&0x3F), 4
	default:
		return 0xFFFD, 1
	}
}

// isUnicodeSpace reports whether r is Unicode whitespace as recognized
// by Py_UNICODE_ISSPACE. Covers the BMP whitespace block CPython
// classifies in _PyUnicode_IsWhitespace.
//
// CPython: Objects/unicodetype_db.h _PyUnicode_IsWhitespace
func isUnicodeSpace(r rune) bool {
	switch r {
	case 0x0085, 0x00A0,
		0x1680,
		0x2000, 0x2001, 0x2002, 0x2003, 0x2004, 0x2005, 0x2006,
		0x2007, 0x2008, 0x2009, 0x200A,
		0x2028, 0x2029,
		0x202F, 0x205F, 0x3000:
		return true
	}
	return false
}

// unicodeDecimalValue returns the decimal-digit value of r in the
// Unicode Nd category, or -1 if r is not a decimal digit. The table
// below covers the Nd ranges used by test_int's test_unicode (DEVANAGARI,
// BENGALI, ARABIC-INDIC, etc.); the full Unicode database port lands
// with unicodedata.
//
// CPython: Objects/unicodetype_db.h _PyUnicode_ToDecimalDigit
func unicodeDecimalValue(r rune) int {
	switch {
	case r >= 0x0660 && r <= 0x0669: // ARABIC-INDIC
		return int(r - 0x0660)
	case r >= 0x06F0 && r <= 0x06F9: // EXTENDED ARABIC-INDIC
		return int(r - 0x06F0)
	case r >= 0x07C0 && r <= 0x07C9: // NKO
		return int(r - 0x07C0)
	case r >= 0x0966 && r <= 0x096F: // DEVANAGARI
		return int(r - 0x0966)
	case r >= 0x09E6 && r <= 0x09EF: // BENGALI
		return int(r - 0x09E6)
	case r >= 0x0A66 && r <= 0x0A6F: // GURMUKHI
		return int(r - 0x0A66)
	case r >= 0x0AE6 && r <= 0x0AEF: // GUJARATI
		return int(r - 0x0AE6)
	case r >= 0x0B66 && r <= 0x0B6F: // ORIYA
		return int(r - 0x0B66)
	case r >= 0x0BE6 && r <= 0x0BEF: // TAMIL
		return int(r - 0x0BE6)
	case r >= 0x0C66 && r <= 0x0C6F: // TELUGU
		return int(r - 0x0C66)
	case r >= 0x0CE6 && r <= 0x0CEF: // KANNADA
		return int(r - 0x0CE6)
	case r >= 0x0D66 && r <= 0x0D6F: // MALAYALAM
		return int(r - 0x0D66)
	case r >= 0x0DE6 && r <= 0x0DEF: // SINHALA
		return int(r - 0x0DE6)
	case r >= 0x0E50 && r <= 0x0E59: // THAI
		return int(r - 0x0E50)
	case r >= 0x0ED0 && r <= 0x0ED9: // LAO
		return int(r - 0x0ED0)
	case r >= 0x0F20 && r <= 0x0F29: // TIBETAN
		return int(r - 0x0F20)
	case r >= 0x1040 && r <= 0x1049: // MYANMAR
		return int(r - 0x1040)
	case r >= 0x1090 && r <= 0x1099: // MYANMAR SHAN
		return int(r - 0x1090)
	case r >= 0x17E0 && r <= 0x17E9: // KHMER
		return int(r - 0x17E0)
	case r >= 0x1810 && r <= 0x1819: // MONGOLIAN
		return int(r - 0x1810)
	case r >= 0xFF10 && r <= 0xFF19: // FULLWIDTH
		return int(r - 0xFF10)
	}
	return -1
}

func parseIntString(s string, base int) (objects.Object, error) {
	return parseIntStringFrom(s, base, false)
}

// parseIntStringFrom is parseIntString with a flag for whether the
// source was bytes-like (so error messages use the b'...' repr CPython
// emits when PyLong_FromBytes rejects a literal).
//
// CPython: Objects/longobject.c:3005 PyLong_FromBytes (error path)
func parseIntStringFrom(s string, base int, fromBytes bool) (objects.Object, error) {
	stripped, ok := stripIntLiteralChecked(s, base)
	if !ok {
		return nil, invalidLiteralError(base, s, fromBytes)
	}
	effBase := parseBase(s, base)
	// Limit check applies only to non-power-of-2 bases. CPython skips the
	// gate for binary bases because long_from_binary_base is linear.
	//
	// CPython: Objects/longobject.c:2936 long_from_string_base (limit check)
	if !isPowerOfTwoBase(effBase) && objects.IntMaxStrDigitsHook != nil {
		if limit := objects.IntMaxStrDigitsHook(); limit > 0 {
			n := digitCountForLimit(stripped)
			if int32(n) > limit {
				return nil, fmt.Errorf(
					"ValueError: Exceeds the limit (%d digits) for integer string conversion: value has %d digits; use sys.set_int_max_str_digits() to increase the limit",
					limit, n)
			}
		}
	}
	out := new(big.Int)
	_, parsed := out.SetString(stripped, effBase)
	if !parsed || stripped == "" || stripped == "+" || stripped == "-" {
		return nil, invalidLiteralError(base, s, fromBytes)
	}
	return objects.NewIntFromBig(out), nil
}

func invalidLiteralError(base int, s string, fromBytes bool) error {
	if fromBytes {
		return fmt.Errorf("ValueError: invalid literal for int() with base %d: b%s", base, pyReprBytes(s))
	}
	// Route the str through the real unicode repr so a lone surrogate
	// renders as \udXXX rather than the three replacement chars Go's range
	// loop produces for its WTF-8 bytes.
	if r, err := objects.Repr(objects.NewStr(s)); err == nil {
		return fmt.Errorf("ValueError: invalid literal for int() with base %d: %s", base, r)
	}
	return fmt.Errorf("ValueError: invalid literal for int() with base %d: %s", base, pyReprStr(s))
}

// pyReprBytes formats s as Python's bytes repr does: same quote rules as
// pyReprStr, but every byte that is not printable ASCII gets the \xHH
// escape regardless of whether it is a valid UTF-8 sequence head.
// PyBytes_Repr escapes byte-by-byte, so b'\xbd' renders as the literal
// six characters '\xbd' rather than the Unicode replacement char.
//
// CPython: Objects/bytesobject.c:1061 bytes_repr
func pyReprBytes(s string) string {
	hasSingle, hasDouble := false, false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\'':
			hasSingle = true
		case '"':
			hasDouble = true
		}
	}
	quote := byte('\'')
	if hasSingle && !hasDouble {
		quote = '"'
	}
	out := make([]byte, 0, len(s)+2)
	out = append(out, quote)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == quote:
			out = append(out, '\\', quote)
		case c == '\\':
			out = append(out, '\\', '\\')
		case c == '\t':
			out = append(out, '\\', 't')
		case c == '\n':
			out = append(out, '\\', 'n')
		case c == '\r':
			out = append(out, '\\', 'r')
		case c < 0x20 || c >= 0x7f:
			out = fmt.Appendf(out, "\\x%02x", c)
		default:
			out = append(out, c)
		}
	}
	out = append(out, quote)
	return string(out)
}

// pyReprStr formats s as Python's repr does: prefer single quotes
// unless s itself contains an unescaped single quote and no double
// quote, then switch to double quotes. The format mirrors
// PyUnicode_Repr for the test_int.test_error_message expectations.
//
// CPython: Objects/unicodeobject.c:11875 unicode_repr
func pyReprStr(s string) string {
	hasSingle, hasDouble := false, false
	for _, r := range s {
		switch r {
		case '\'':
			hasSingle = true
		case '"':
			hasDouble = true
		}
	}
	quote := byte('\'')
	if hasSingle && !hasDouble {
		quote = '"'
	}
	out := make([]byte, 0, len(s)+2)
	out = append(out, quote)
	for _, r := range s {
		switch {
		case r == rune(quote):
			out = append(out, '\\', quote)
		case r == '\\':
			out = append(out, '\\', '\\')
		case r == '\t':
			out = append(out, '\\', 't')
		case r == '\n':
			out = append(out, '\\', 'n')
		case r == '\r':
			out = append(out, '\\', 'r')
		case r < 0x20 || r == 0x7f:
			out = fmt.Appendf(out, "\\x%02x", r)
		default:
			// Non-ASCII passes through as-is (Python emits the literal
			// character, since unicode repr keeps printable non-ASCII).
			out = append(out, []byte(string(r))...)
		}
	}
	out = append(out, quote)
	return string(out)
}

// stripIntLiteralChecked strips whitespace, sign, and 0x/0o/0b prefix
// from s and applies PEP 515 underscore validation: underscores must
// lie between two digits, may appear after the prefix, and cannot
// repeat. Returns (stripped, false) on any malformed placement so the
// caller can raise ValueError. With base 0, a leading "0" followed by
// more digits (the old octal form) is also invalid per PEP 3127.
//
// CPython: Objects/longobject.c:2789 long_from_string_base (digit/underscore loop)
func stripIntLiteralChecked(s string, base int) (string, bool) {
	t := trimSpace(s)
	sign := ""
	if t != "" && (t[0] == '+' || t[0] == '-') {
		sign = string(t[0])
		t = t[1:]
	}
	hadPrefix := false
	if len(t) > 2 && t[0] == '0' {
		switch {
		case (base == 0 || base == 2) && (t[1] == 'b' || t[1] == 'B'):
			t = t[2:]
			hadPrefix = true
		case (base == 0 || base == 8) && (t[1] == 'o' || t[1] == 'O'):
			t = t[2:]
			hadPrefix = true
		case (base == 0 || base == 16) && (t[1] == 'x' || t[1] == 'X'):
			t = t[2:]
			hadPrefix = true
		}
	}
	// PEP 3127: with base=0, a "0..." literal without a recognized
	// prefix is invalid (it would be the legacy octal form).
	if !hadPrefix && base == 0 && len(t) > 1 && t[0] == '0' {
		for i := 1; i < len(t); i++ {
			if t[i] != '0' && t[i] != '_' {
				return "", false
			}
		}
	}
	if t == "" {
		return "", false
	}
	// PEP 515: cannot end with _, cannot start with _ unless a prefix
	// was just stripped (e.g. "0b_1010" is valid).
	if t[len(t)-1] == '_' {
		return "", false
	}
	if t[0] == '_' && !hadPrefix {
		return "", false
	}
	out := make([]byte, 0, len(t))
	prevUnderscore := false
	for i := 0; i < len(t); i++ {
		c := t[i]
		if c == '_' {
			if prevUnderscore {
				return "", false
			}
			prevUnderscore = true
			continue
		}
		prevUnderscore = false
		out = append(out, c)
	}
	return sign + string(out), true
}

// isPowerOfTwoBase reports whether base is one of the binary bases
// (2, 4, 8, 16, 32) that skip the digit-limit gate.
//
// CPython: Objects/longobject.c:2911 is_binary_base
func isPowerOfTwoBase(base int) bool {
	switch base {
	case 2, 4, 8, 16, 32:
		return true
	}
	return false
}

// digitCountForLimit returns the count of significant digit characters
// in a stripIntLiteral-output string, skipping a leading sign. Mirrors
// the `digits` counter CPython's long_from_string_base passes to its
// limit check (sign and prefix already removed, underscores stripped).
//
// CPython: Objects/longobject.c:2870 long_from_string_base (digits arg)
func digitCountForLimit(s string) int {
	if s == "" {
		return 0
	}
	if s[0] == '+' || s[0] == '-' {
		return len(s) - 1
	}
	return len(s)
}

func parseBase(s string, base int) int {
	if base != 0 {
		return base
	}
	t := trimSpace(s)
	if t != "" && (t[0] == '+' || t[0] == '-') {
		t = t[1:]
	}
	if len(t) > 1 && t[0] == '0' {
		switch t[1] {
		case 'b', 'B':
			return 2
		case 'o', 'O':
			return 8
		case 'x', 'X':
			return 16
		}
	}
	return 10
}

func trimSpace(s string) string {
	for s != "" && pystrconv.IsSpace(s[0]) {
		s = s[1:]
	}
	for s != "" && pystrconv.IsSpace(s[len(s)-1]) {
		s = s[:len(s)-1]
	}
	return s
}

// FloatCtor ports float_new. 0 args returns 0.0; one positional
// converts via PyNumber_Float, including PyOS_string_to_double for
// strings.
//
// CPython: Objects/floatobject.c float_new_impl
func FloatCtor(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: float() takes no keyword arguments")
	}
	if len(args) == 0 {
		return objects.NewFloat(0), nil
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: float expected at most 1 argument, got %d", len(args))
	}
	switch v := args[0].(type) {
	case *objects.Float:
		// CPython: Objects/floatobject.c:1595 float_new_impl
		// For exact float, return self. For subclasses, call __float__
		// if user-defined; otherwise extract value and return a new
		// plain float.
		if v.Type() == objects.FloatType {
			return v, nil
		}
		// Only dispatch through __float__ when the subclass defines it.
		// If not user-defined, the inherited slot returns self (a subclass
		// instance), which would produce a spurious DeprecationWarning.
		if objects.IsOwnDescriptor(v.Type(), "__float__") {
			if n := v.Type().Number; n != nil && n.Float != nil {
				res, err := n.Float(v)
				if err != nil {
					return nil, err
				}
				// __float__ must return a float.
				rf, isFloat := res.(*objects.Float)
				if !isFloat {
					return nil, fmt.Errorf("TypeError: __float__ returned non-float (type %s)", res.Type().Name)
				}
				if rf.Type() == objects.FloatType {
					return rf, nil
				}
				// __float__ returned a float subclass: emit DeprecationWarning
				// and unwrap to a plain float.
				// CPython: Objects/floatobject.c _PyFloat_FromNumberWithBase
				msg := fmt.Sprintf("__float__ returned non-float (type %s). "+
					"The ability to return an instance of a strict subclass of float "+
					"is deprecated, and may be removed in a future version of Python.",
					rf.Type().Name)
				if objects.DeprecWarnHook != nil {
					if werr := objects.DeprecWarnHook(msg); werr != nil {
						return nil, werr
					}
				}
				return objects.NewFloat(rf.Float64()), nil
			}
		}
		return objects.NewFloat(v.Float64()), nil
	case *objects.Int:
		// CPython: Objects/floatobject.c:1623 float_new_impl — PyNumber_Float
		// on a long routes through long___float__, which propagates the
		// OverflowError from PyLong_AsDouble when the magnitude exceeds
		// DBL_MAX. Going through Number.Float keeps that error path live
		// for the exact-int case here.
		if n := v.Type().Number; n != nil && n.Float != nil {
			res, err := n.Float(v)
			if err != nil {
				return nil, err
			}
			return res, nil
		}
		f, _ := new(big.Float).SetInt(v.BigInt()).Float64()
		if math.IsInf(f, 0) {
			return nil, fmt.Errorf("OverflowError: int too large to convert to float")
		}
		return objects.NewFloat(f), nil
	}
	// CPython: Objects/abstract.c:1592 PyNumber_Float — try nb_float first
	if n := args[0].Type().Number; n != nil {
		if n.Float != nil {
			res, err := n.Float(args[0])
			if err != nil {
				return nil, err
			}
			// __float__ must return a float; anything else is a TypeError.
			// CPython: Objects/abstract.c:1602 PyNumber_Float
			rf0, isFloat := res.(*objects.Float)
			if !isFloat {
				return nil, fmt.Errorf("TypeError: %s.__float__ returned non-float (type %s)", args[0].Type().Name, res.Type().Name)
			}
			// If __float__ returned a non-exact float subclass, emit
			// DeprecationWarning and unwrap.
			// CPython: Objects/abstract.c:1614 PyNumber_Float
			if rf := rf0; rf.Type() != objects.FloatType {
				msg := fmt.Sprintf("__float__ returned non-float (type %s). "+
					"The ability to return an instance of a strict subclass of float "+
					"is deprecated, and may be removed in a future version of Python.",
					rf.Type().Name)
				if objects.DeprecWarnHook != nil {
					if werr := objects.DeprecWarnHook(msg); werr != nil {
						return nil, werr
					}
				}
				return objects.NewFloat(rf.Float64()), nil
			}
			return res, nil
		}
		// CPython: Objects/abstract.c:1630 PyNumber_Float — nb_index fallback
		if n.Index != nil {
			idx, err := n.Index(args[0])
			if err != nil {
				return nil, err
			}
			i, ok := idx.(*objects.Int)
			if !ok {
				return nil, fmt.Errorf("TypeError: __index__ returned non-int (type %s)", idx.Type().Name)
			}
			f, _ := new(big.Float).SetInt(i.BigInt()).Float64()
			if math.IsInf(f, 0) {
				return nil, fmt.Errorf("OverflowError: int too large to convert to float")
			}
			return objects.NewFloat(f), nil
		}
	}
	// CPython: Objects/floatobject.c:190 PyFloat_FromString
	// Bytes, ByteArray, MemoryView, and buffer-protocol objects (array.array).
	if buf, ok := objects.AsBytesLike(args[0]); ok {
		f, err := pystrconv.ParseFloat(trimSpace(string(buf)))
		if err != nil && !errors.Is(err, pystrconv.ErrFloatOverflow) {
			r, _ := objects.Repr(args[0])
			return nil, fmt.Errorf("ValueError: could not convert string to float: %s", r)
		}
		return objects.NewFloat(f), nil
	}
	// CPython: Objects/floatobject.c:205 PyFloat_FromString — Unicode (str and subclasses)
	if objects.IsSubtype(args[0].Type(), objects.StrType()) {
		s, _ := objects.Str(args[0])
		f, err := pystrconv.ParseFloat(trimSpace(s))
		if err != nil && !errors.Is(err, pystrconv.ErrFloatOverflow) {
			r, _ := objects.Repr(args[0])
			return nil, fmt.Errorf("ValueError: could not convert string to float: %s", r)
		}
		return objects.NewFloat(f), nil
	}
	return nil, fmt.Errorf("TypeError: float() argument must be a string or a real number, not '%s'", args[0].Type().Name)
}

// BoolCtor ports bool_new. 0 args returns False; one positional runs
// through PyObject_IsTrue. Keyword arguments are rejected.
//
// CPython: Objects/boolobject.c bool_new
func BoolCtor(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(kwargs) > 0 {
		return nil, fmt.Errorf("TypeError: bool() takes no keyword arguments")
	}
	if len(args) == 0 {
		return objects.False(), nil
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: bool expected at most 1 argument, got %d", len(args))
	}
	t, err := objects.IsTruthy(args[0])
	if err != nil {
		return nil, err
	}
	return objects.NewBool(t), nil
}

// ListCtor ports list_init. 0 args returns []; one positional drains
// the iterable into a fresh list. Keyword arguments are rejected so
// the clinic signature "iterable: object = (), /" (positional-only)
// matches CPython.
//
// CPython: Objects/listobject.c list_init
// CPython: Objects/clinic/listobject.c.h list___init__ (_PyArg_NoKeywords)
func ListCtor(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(kwargs) > 0 {
		return nil, fmt.Errorf("TypeError: list() takes no keyword arguments")
	}
	if len(args) == 0 {
		return objects.NewList(nil), nil
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: list expected at most 1 argument, got %d", len(args))
	}
	items, err := drainIterable(args[0])
	if err != nil {
		return nil, err
	}
	l := objects.NewList(items)
	releaseDrained(items)
	return l, nil
}

// bindListCtor wires list's constructor as separate TpNew (allocate) and
// __init__ (populate). bindCtor would conflate them, so subclasses like
// `class S(list): pass` would lose their type because TpNew always
// returned a plain *List instead of binding the requested cls.
//
// CPython: Objects/listobject.c:3380 PyList_Type (tp_new = list_new, tp_init = list___init__)
func bindListCtor(t *objects.Type) {
	// TpNew is set in objects/list.go to allocate a bare *List bound to
	// the requested class. __init__ clears it, then drains an optional
	// iterable into it.
	//
	// CPython: Objects/listobject.c:2716 list___init___impl
	objects.SetTypeDescr(t, "__init__", objects.NewMethodDescr(t, "__init__", func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
		if len(args) < 1 || len(args) > 2 {
			return nil, fmt.Errorf("TypeError: list expected at most 1 argument, got %d", len(args)-1)
		}
		// CPython: Objects/clinic/listobject.c.h list___init__ skips
		// _PyArg_NoKeywords when the instance's class has overridden
		// __new__ (Py_TYPE(self)->tp_new != base_tp->tp_new). That lets a
		// subclass like `class S(list): def __new__(cls, seq, kw=None)`
		// pass keyword arguments through type.__call__ without tripping
		// the bare-list check.
		if len(kwargs) > 0 {
			selfTp := args[0].Type()
			ownNew := false
			if selfTp != t {
				if cd := selfTp.ClassAttrDict; cd != nil {
					if has, _ := cd.Contains(objects.NewStr("__new__")); has {
						ownNew = true
					}
				}
			}
			if !ownNew {
				return nil, fmt.Errorf("TypeError: list() takes no keyword arguments")
			}
		}
		l, ok := args[0].(*objects.List)
		if !ok {
			return nil, fmt.Errorf("TypeError: descriptor '__init__' requires a 'list' object but received a '%s'", args[0].Type().Name)
		}
		l.Clear()
		if len(args) == 2 {
			items, err := drainIterable(args[1])
			if err != nil {
				return nil, err
			}
			for _, v := range items {
				l.Append(v)
			}
			releaseDrained(items)
		}
		return objects.None(), nil
	}))
	bindCtorDescr(t, ListCtor)
}

// TupleCtor ports tuple_new. 0 args returns the empty tuple; one
// positional drains the iterable into a tuple.
//
// CPython: Objects/tupleobject.c tuple_new_impl
func TupleCtor(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) == 0 {
		return objects.NewTuple(nil), nil
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: tuple expected at most 1 argument, got %d", len(args))
	}
	items, err := drainIterable(args[0])
	if err != nil {
		return nil, err
	}
	t := objects.NewTuple(items)
	releaseDrained(items)
	return t, nil
}

// SetCtor ports set_new. Allocates an empty set of the correct subtype.
// Population is deferred to set_init (__init__) so single-pass iterables
// (map, filter, generator) are consumed only once.
//
// CPython: Objects/setobject.c:2436 set_new (allocate only, no iterable drain)
func SetCtor(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return setCtorWithType(objects.SetType, args)
}

func setCtorWithType(cls *objects.Type, args []objects.Object) (objects.Object, error) {
	if len(args) > 1 {
		return nil, fmt.Errorf("TypeError: set expected at most 1 argument, got %d", len(args))
	}
	return objects.NewSetOfType(cls), nil
}

// FrozensetCtor ports frozenset_new. 0 args returns an empty frozenset; one
// positional arg that is already an exact frozenset is returned as-is
// (CPython optimization: immutable object, no copy needed). cls is the
// concrete type to allocate so frozenset subclasses carry their own type.
//
// CPython: Objects/setobject.c:1195 frozenset_new
func FrozensetCtor(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return frozensetCtorWithType(objects.FrozensetType, args, kwargs)
}

// frozensetCtorWithType is the TpNew body for frozenset and its subclasses.
// Keyword arguments are rejected when cls is exact frozenset or when cls
// has not overridden __init__ (tp_init == NULL), matching CPython's
// frozenset_new guard:
//
//	if (type == &PyFrozenSet_Type || type->tp_init == PyFrozenSet_Type.tp_init)
//
// CPython: Objects/setobject.c:1195 frozenset_new
func frozensetCtorWithType(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(kwargs) > 0 {
		// Reject kwargs unless the subclass has a user-defined __init__.
		// CPython: Objects/setobject.c:1198-1201 frozenset_new kwds guard:
		//   (type == &PyFrozenSet_Type || type->tp_init == PyFrozenSet_Type.tp_init)
		// PyFrozenSet_Type.tp_init is NULL; the equivalent in gopy is that
		// no __init__ defined in the class's own MRO between the subclass
		// and object/frozenset. If __init__ is found only on object or not at
		// all, it counts as tp_init == NULL → reject kwargs.
		rejectKwargs := true
		if cls != objects.FrozensetType {
			if _, owner := objects.LookupDescriptor(cls, "__init__"); owner != nil &&
				owner != objects.FrozensetType && owner != objects.ObjectType() {
				rejectKwargs = false
			}
		}
		if rejectKwargs {
			return nil, fmt.Errorf("TypeError: frozenset() does not support keyword arguments")
		}
	}
	if len(args) > 1 {
		return nil, fmt.Errorf("TypeError: frozenset expected at most 1 argument, got %d", len(args))
	}
	if len(args) == 0 {
		return objects.NewFrozensetOfType(cls, nil)
	}
	// CPython: Objects/setobject.c:2375 frozenset_new — if the argument is
	// already an exact frozenset of the same type, return it unchanged.
	if fs, ok := args[0].(*objects.Set); ok && fs.Type() == cls && cls == objects.FrozensetType {
		return fs, nil
	}
	// Use SetUpdateFrom for dict/set fast path (no rehashing of cached hashes).
	fs, err := objects.NewFrozensetOfType(cls, nil)
	if err != nil {
		return nil, err
	}
	if err := objects.SetUpdateFrom(fs, args[0]); err != nil {
		return nil, err
	}
	return fs, nil
}

// DictCtor ports dict_init. Accepts a mapping or an iterable of
// 2-element iterables, and merges keyword arguments.
//
// CPython: Objects/dictobject.c dict_init
func DictCtor(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) > 1 {
		return nil, fmt.Errorf("TypeError: dict expected at most 1 argument, got %d", len(args))
	}
	out := objects.NewDict()
	if len(args) == 1 {
		if d, ok := args[0].(*objects.Dict); ok && !objects.DictIterOverridden(d.Type()) {
			if err := mergeDict(out, d); err != nil {
				return nil, err
			}
		} else if dictHasKeys(args[0]) {
			if err := mergeMappingInto(out, args[0]); err != nil {
				return nil, err
			}
		} else {
			if err := mergeFromPairs(out, args[0]); err != nil {
				return nil, err
			}
		}
	}
	for k, v := range kwargs {
		if err := out.SetItem(objects.NewStr(k), v); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// bindDictCtor wires dict's constructor as separate TpNew (allocate) and
// __init__ (populate). bindCtor would conflate them, breaking dict subclasses
// whose __init__ calls super().__init__() with extra args.
//
// CPython: Objects/dictobject.c:4023 PyDict_Type (tp_new = dict_new, tp_init = dict_init)
func bindDictCtor(t *objects.Type) {
	// TpNew is already set in objects/dict.go to allocate a bare *Dict.
	// __init__ populates it from an optional mapping/iterable + kwargs.
	objects.SetTypeDescr(t, "__init__", objects.NewMethodDescr(t, "__init__", func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
		if len(args) < 1 || len(args) > 2 {
			return nil, fmt.Errorf("TypeError: dict expected at most 1 argument, got %d", len(args)-1)
		}
		d := args[0].(*objects.Dict)
		if len(args) == 2 {
			if src, ok := args[1].(*objects.Dict); ok && !objects.DictIterOverridden(src.Type()) {
				if err := mergeDict(d, src); err != nil {
					return nil, err
				}
			} else if dictHasKeys(args[1]) {
				if err := mergeMappingInto(d, args[1]); err != nil {
					return nil, err
				}
			} else {
				if err := mergeFromPairs(d, args[1]); err != nil {
					return nil, err
				}
			}
		}
		for k, v := range kwargs {
			if err := d.SetItem(objects.NewStr(k), v); err != nil {
				return nil, err
			}
		}
		return objects.None(), nil
	}))
}

func dictHasKeys(o objects.Object) bool {
	_, err := objects.GetAttr(o, objects.NewStr("keys"))
	return err == nil
}

func mergeMappingInto(dst *objects.Dict, m objects.Object) error {
	keysAttr, err := objects.GetAttr(m, objects.NewStr("keys"))
	if err != nil {
		return err
	}
	keysObj, err := objects.Call(keysAttr, objects.NewTuple(nil), nil)
	if err != nil {
		return err
	}
	it, err := abstract.Iter(keysObj)
	if err != nil {
		return err
	}
	for {
		k, err := abstract.IterNext(it)
		if errors.Is(err, objects.ErrStopIteration) {
			return nil
		}
		if err != nil {
			return err
		}
		v, err := objects.GetItem(m, k)
		if err != nil {
			return err
		}
		if err := dst.SetItem(k, v); err != nil {
			return err
		}
	}
}

// releaseDrained drops the one owned reference drainIterable took on each
// collected element. Callers invoke it after they have built their own
// container (NewList/NewTuple/etc.), which takes its own counted reference.
func releaseDrained(items []objects.Object) {
	for _, v := range items {
		objects.Decref(v)
	}
}

func drainIterable(o objects.Object) ([]objects.Object, error) {
	it, err := abstract.Iter(o)
	if err != nil {
		return nil, err
	}
	// CPython: Objects/listobject.c:1078 list_extend_iter_lock_held calls
	// PyObject_LengthHint(iterable, 8) to size the result; a non-TypeError
	// from __len__/__length_hint__ propagates so list(BadLen()) raises
	// instead of silently producing an empty list.
	if _, err := objects.LengthHint(o, 8); err != nil {
		return nil, err
	}
	var items []objects.Object
	for {
		v, err := abstract.IterNext(it)
		if errors.Is(err, objects.ErrStopIteration) {
			return items, nil
		}
		if err != nil {
			// Release the owned references collected so far before bailing.
			for _, x := range items {
				objects.Decref(x)
			}
			return nil, err
		}
		// IterNext returns a borrowed reference (gopy's iterator convention,
		// see objects.IterNext). The batch must own each element so a
		// self-recycling slot such as the dict item iterator, which decrefs
		// the tuple it handed out on the next advance, cannot free an element
		// already collected here. CPython's PyIter_Next returns an owned
		// reference for the same reason. Callers release one count per
		// element after they have built their container.
		//
		// CPython: Objects/abstract.c:2852 PyIter_Next (owned return)
		objects.Incref(v)
		items = append(items, v)
	}
}

func mergeDict(dst, src *objects.Dict) error {
	keys := src.Keys()
	for _, k := range keys {
		v, err := src.GetItem(k)
		if err != nil {
			return err
		}
		if err := dst.SetItem(k, v); err != nil {
			return err
		}
	}
	return nil
}

// BytesCtor ports bytes_new.
// bytes()                       -> b""
// bytes(int)                    -> zero-filled bytes of that length
// bytes(iterable)               -> bytes from ints in iterable
// bytes(bytes/bytearray)        -> copy
// bytes(str, encoding[, errors]) -> str.encode(encoding, errors)
//
// CPython: Objects/bytesobject.c bytes_new_impl
func BytesCtor(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return bytesNewObject(objects.BytesType, args, kwargs)
}

// bytesNewObject ports the full bytes_new path: compute the bytes object via
// bytes_new_impl, then for a proper subclass wrap the buffer into that subtype
// (bytes_subtype_new). When cls is exactly bytes the computed object is
// returned as-is, which preserves a subclass instance handed back by an
// object's __bytes__ method.
//
// CPython: Objects/bytesobject.c:2754 bytes_new_impl + 3055 bytes_subtype_new
func bytesNewObject(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	args, kwargs = foldSourceKwarg(args, kwargs)
	obj, err := bytesNewContents(args, kwargs)
	if err != nil {
		return nil, err
	}
	if cls == objects.BytesType {
		return obj, nil
	}
	return objects.NewBytesSubtype(cls, obj.(*objects.Bytes).Bytes()), nil
}

// foldSourceKwarg moves a bytes(source=...) / bytearray(source=...) keyword
// into the positional slot when no positional source was supplied. The clinic
// signatures name the first parameter "source", so it may arrive either way.
//
// CPython: Objects/clinic/bytesobject.c.h bytes_new keywords
func foldSourceKwarg(args []objects.Object, kwargs map[string]objects.Object) ([]objects.Object, map[string]objects.Object) {
	src, ok := kwargs["source"]
	if !ok || len(args) != 0 {
		return args, kwargs
	}
	rest := make(map[string]objects.Object, len(kwargs))
	for k, v := range kwargs {
		if k != "source" {
			rest[k] = v
		}
	}
	return []objects.Object{src}, rest
}

// strArg reports the Go string behind a clinic 's' converter argument; the
// converter requires an exact str and rejects everything else.
func strArg(o objects.Object) (string, bool) {
	if o.Type() != objects.StrType() {
		return "", false
	}
	s, _ := objects.Str(o)
	return s, true
}

// parseBytesEncoding pulls the optional encoding/errors arguments out of the
// positional tail and keyword map for bytes()/bytearray(). The clinic 's'
// converters require str, so a non-str raises TypeError.
//
// CPython: Objects/clinic/bytesobject.c.h bytes_new (encoding/errors 's')
func parseBytesEncoding(args []objects.Object, kwargs map[string]objects.Object, typeName string) (encoding, errs string, encSet, errSet bool, err error) {
	errs = "strict"
	set := func(o objects.Object, what string) (string, bool, error) {
		s, ok := strArg(o)
		if !ok {
			return "", false, fmt.Errorf("TypeError: %s() %s must be str, not '%s'", typeName, what, o.Type().Name)
		}
		return s, true, nil
	}
	if len(args) > 1 {
		if encoding, encSet, err = set(args[1], "encoding"); err != nil {
			return encoding, errs, encSet, errSet, err
		}
	}
	if v, ok := kwargs["encoding"]; ok {
		if encoding, encSet, err = set(v, "encoding"); err != nil {
			return encoding, errs, encSet, errSet, err
		}
	}
	if len(args) > 2 {
		if errs, errSet, err = set(args[2], "errors"); err != nil {
			return encoding, errs, encSet, errSet, err
		}
	}
	if v, ok := kwargs["errors"]; ok {
		if errs, errSet, err = set(v, "errors"); err != nil {
			return encoding, errs, encSet, errSet, err
		}
	}
	return encoding, errs, encSet, errSet, nil
}

// bytesCoerceItem reads one element of an iterable/list source as a byte. The
// value is coerced through __index__ (PyNumber_AsSsize_t), so any index-like
// object is accepted; out-of-range values raise ValueError. bytes() words the
// range message with "bytes", bytearray() with "byte".
//
// CPython: Objects/bytesobject.c:2864 _PyBytes_FromList / Objects/bytearrayobject.c:26 _getbytevalue
func bytesCoerceItem(item objects.Object, typeName string) (byte, error) {
	iv, err := objects.NumberIndex(item)
	if err != nil {
		return 0, err
	}
	n, fits := iv.(*objects.Int).Int64()
	word := "bytes"
	if typeName == "bytearray" {
		word = "byte"
	}
	if !fits || n < 0 || n >= 256 {
		return 0, fmt.Errorf("ValueError: %s must be in range(0, 256)", word)
	}
	return byte(n), nil
}

// bytesFromObjectContents ports PyBytes_FromObject: build a raw byte buffer
// from a buffer-protocol object, or by coercing each item of an iterable
// through __index__. A non-iterable non-buffer source raises the
// "cannot convert 'X' object to <typeName>" TypeError.
//
// CPython: Objects/bytesobject.c:3005 PyBytes_FromObject
func bytesFromObjectContents(x objects.Object, typeName string) ([]byte, error) {
	// PyObject_GetBuffer is the first thing PyBytes_FromObject tries, so a
	// released memoryview raises before any copy or iteration fallback.
	//
	// CPython: Objects/bytesobject.c:2818 PyBytes_FromObject
	if err := objects.CheckBufferReleased(x); err != nil {
		return nil, err
	}
	if buf, ok := objects.AsBytesLike(x); ok {
		return append([]byte(nil), buf...), nil
	}
	if !objects.IsSubtype(x.Type(), objects.StrType()) {
		it, ierr := objects.Iter(x)
		if ierr == nil {
			var buf []byte
			for {
				item, err := objects.IterNext(it)
				if errors.Is(err, objects.ErrStopIteration) {
					break
				}
				if err != nil {
					return nil, err
				}
				bval, err := bytesCoerceItem(item, typeName)
				if err != nil {
					return nil, err
				}
				buf = append(buf, bval)
			}
			return buf, nil
		}
		if !isTypeError(ierr) {
			return nil, ierr
		}
	}
	return nil, fmt.Errorf("TypeError: cannot convert '%s' object to %s", x.Type().Name, typeName)
}

// isTypeError reports whether err carries a Python TypeError. A TypeError
// raised with no message formats as the bare type name ("TypeError" with no
// trailing colon), so a "TypeError:" prefix check alone misses it.
func isTypeError(err error) bool {
	s := err.Error()
	return s == "TypeError" || strings.HasPrefix(s, "TypeError:")
}

// bytesIntContents handles the "is it an integer?" branch shared by
// bytes_new_impl and bytearray___init___impl: when the source defines
// __index__, coerce it to a count and produce a zero-filled buffer. A
// TypeError out of __index__ means "fall through" (handled bool false); any
// other exception (ZeroDivisionError, OverflowError) propagates.
//
// CPython: Objects/bytesobject.c:2812 bytes_new_impl (_PyIndex_Check branch)
func bytesIntContents(arg objects.Object) (buf []byte, handled bool, err error) {
	if !objects.IndexCheck(arg) {
		return nil, false, nil
	}
	iv, ierr := objects.NumberIndex(arg)
	if ierr != nil {
		if isTypeError(ierr) {
			return nil, false, nil
		}
		return nil, true, ierr
	}
	n, fits := iv.(*objects.Int).Int64()
	if !fits {
		return nil, true, fmt.Errorf("OverflowError: cannot fit 'int' into an index-sized integer")
	}
	if n < 0 {
		return nil, true, fmt.Errorf("ValueError: negative count")
	}
	return make([]byte, n), true, nil
}

// bytesNewContents ports bytes_new_impl: parse (source, encoding, errors) and
// build the raw payload. The __bytes__ protocol is honored here (bytes only;
// bytearray's __init__ does not consult it).
//
// CPython: Objects/bytesobject.c:2754 bytes_new_impl
func bytesNewContents(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	args, kwargs = foldSourceKwarg(args, kwargs)
	encoding, errs, encSet, errSet, err := parseBytesEncoding(args, kwargs, "bytes")
	if err != nil {
		return nil, err
	}
	if len(args) == 0 {
		if encSet {
			return nil, fmt.Errorf("TypeError: encoding without a string argument")
		}
		if errSet {
			return nil, fmt.Errorf("TypeError: errors without a string argument")
		}
		return objects.NewBytes(nil), nil
	}
	x := args[0]
	isStr := objects.IsSubtype(x.Type(), objects.StrType())
	switch {
	case encSet:
		if !isStr {
			return nil, fmt.Errorf("TypeError: encoding without a string argument")
		}
		s, _ := objects.Str(x)
		// In development mode validate the encoding and error-handler
		// names up front so a bogus name raises LookupError even when the
		// payload is empty and the handler would never otherwise run.
		//
		// CPython: Objects/unicodeobject.c:3938 PyUnicode_AsEncodedString
		// (unicode_check_encoding_errors)
		if cerr := codecs.CheckEncodingErrors(encoding, errs); cerr != nil {
			return nil, cerr
		}
		out, _, encErr := codecs.Encode(s, encoding, errs)
		if encErr != nil {
			return nil, encErr
		}
		return objects.NewBytes(out), nil
	case errSet:
		if isStr {
			return nil, fmt.Errorf("TypeError: string argument without an encoding")
		}
		return nil, fmt.Errorf("TypeError: errors without a string argument")
	}
	// __bytes__ protocol: checked before the unicode/int branches, so a str
	// subclass that defines __bytes__ is honored. The returned object is
	// handed back unchanged (it may itself be a bytes subclass instance).
	if out, ok, berr := bytesViaDunderBytes(x); ok || berr != nil {
		return out, berr
	}
	if isStr {
		return nil, fmt.Errorf("TypeError: string argument without an encoding")
	}
	if buf, handled, ierr := bytesIntContents(x); handled {
		if ierr != nil {
			return nil, ierr
		}
		return objects.NewBytes(buf), nil
	}
	buf, ferr := bytesFromObjectContents(x, "bytes")
	if ferr != nil {
		return nil, ferr
	}
	return objects.NewBytes(buf), nil
}

// bytesViaDunderBytes calls x.__bytes__() when defined; the result must be a
// bytes object and is returned unchanged so a subclass instance survives. ok
// is false (with nil error) when x has no __bytes__.
//
// CPython: Objects/bytesobject.c:2791 bytes_new_impl (__bytes__ branch)
func bytesViaDunderBytes(x objects.Object) (out objects.Object, ok bool, err error) {
	fn, lerr := objects.LookupSpecial(x, "__bytes__")
	if lerr != nil || fn == nil {
		// A missing __bytes__ (AttributeError out of LookupSpecial) means
		// "no __bytes__ branch", not a failure: report absent with nil err.
		return nil, false, nil //nolint:nilerr // absent __bytes__ is not an error
	}
	res, cerr := objects.CallObject(fn, nil)
	if cerr != nil {
		return nil, true, cerr
	}
	if _, isBytes := res.(*objects.Bytes); !isBytes {
		return nil, true, fmt.Errorf("TypeError: __bytes__ returned non-bytes (type %s)", res.Type().Name)
	}
	return res, true, nil
}

// ByteArrayCtor ports bytearray_new: it only allocates an empty bytearray of
// the requested type. Population happens in __init__ (bytearray___init___impl).
//
// CPython: Objects/bytearrayobject.c bytearray_new_impl
func ByteArrayCtor(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewByteArray(nil), nil
}

// bytearrayInitContents ports bytearray___init___impl: parse the
// (source, encoding, errors) arguments into the bytes that fill self. Unlike
// bytes_new_impl it does not consult __bytes__.
//
// CPython: Objects/bytearrayobject.c:913 bytearray___init___impl
func bytearrayInitContents(args []objects.Object, kwargs map[string]objects.Object) ([]byte, error) {
	args, kwargs = foldSourceKwarg(args, kwargs)
	encoding, errs, encSet, errSet, err := parseBytesEncoding(args, kwargs, "bytearray")
	if err != nil {
		return nil, err
	}
	if len(args) == 0 {
		if encSet {
			return nil, fmt.Errorf("TypeError: encoding without a string argument")
		}
		if errSet {
			return nil, fmt.Errorf("TypeError: errors without a string argument")
		}
		return nil, nil
	}
	x := args[0]
	if objects.IsSubtype(x.Type(), objects.StrType()) {
		if !encSet {
			return nil, fmt.Errorf("TypeError: string argument without an encoding")
		}
		s, _ := objects.Str(x)
		// Development-mode eager validation of the encoding/errors names
		// (see bytesNewContents for the rationale).
		//
		// CPython: Objects/unicodeobject.c:3938 PyUnicode_AsEncodedString
		// (unicode_check_encoding_errors)
		if cerr := codecs.CheckEncodingErrors(encoding, errs); cerr != nil {
			return nil, cerr
		}
		out, _, encErr := codecs.Encode(s, encoding, errs)
		if encErr != nil {
			return nil, encErr
		}
		return out, nil
	}
	// Not unicode: there can't be an encoding or errors.
	if encSet {
		return nil, fmt.Errorf("TypeError: encoding without a string argument")
	}
	if errSet {
		return nil, fmt.Errorf("TypeError: errors without a string argument")
	}
	if buf, handled, ierr := bytesIntContents(x); handled {
		return buf, ierr
	}
	return bytesFromObjectContents(x, "bytearray")
}

// bindByteArrayCtor wires bytearray's constructor as separate TpNew
// (allocate, set in objects/bytearray.go) and __init__ (populate).
// bindCtor would conflate them, so subclasses like
// `class S(bytearray): pass` lose their type and cannot hold instance
// attributes because TpNew always returned a plain *ByteArray bound to
// ByteArrayType. The split mirrors list.
//
// CPython: Objects/bytearrayobject.c:2674 PyByteArray_Type (tp_new = bytearray_new, tp_init = bytearray___init__)
func bindByteArrayCtor(t *objects.Type) {
	objects.SetTypeDescr(t, "__init__", objects.NewMethodDescr(t, "__init__", func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("TypeError: descriptor '__init__' of 'bytearray' object needs an argument")
		}
		self, ok := args[0].(*objects.ByteArray)
		if !ok {
			return nil, fmt.Errorf("TypeError: descriptor '__init__' requires a 'bytearray' object but received a '%s'", args[0].Type().Name)
		}
		// bytearray___init___impl parses (source, encoding, errors) and
		// fills the already-allocated self.
		buf, err := bytearrayInitContents(args[1:], kwargs)
		if err != nil {
			return nil, err
		}
		if err := self.SetContents(buf); err != nil {
			return nil, err
		}
		return objects.None(), nil
	}))
	bindCtorDescr(t, ByteArrayCtor)
}

func mergeFromPairs(dst *objects.Dict, iterable objects.Object) error {
	it, err := abstract.Iter(iterable)
	if err != nil {
		return err
	}
	i := 0
	for {
		v, err := abstract.IterNext(it)
		if errors.Is(err, objects.ErrStopIteration) {
			return nil
		}
		if err != nil {
			return err
		}
		// CPython: Objects/dictobject.c:3823 merge_from_seq2_lock_held
		fast, err := objects.SequenceFast(v, "object is not iterable")
		if err != nil {
			if strings.HasPrefix(err.Error(), "TypeError:") && objects.FormatNoteHook != nil {
				objects.FormatNoteHook(fmt.Sprintf("Cannot convert dictionary update sequence element #%d to a sequence", i))
			}
			return err
		}
		pair, err := drainIterable(fast)
		if err != nil {
			return err
		}
		if len(pair) != 2 {
			releaseDrained(pair)
			return fmt.Errorf("ValueError: dictionary update sequence element #%d has length %d; 2 is required", i, len(pair))
		}
		err = dst.SetItem(pair[0], pair[1])
		releaseDrained(pair)
		if err != nil {
			return err
		}
		i++
	}
}

// ComplexCtor ports complex(). It mirrors CPython's two-stage split:
// actual_complex_new fast-paths the "no kwargs, single positional"
// shape (including exact-type passthrough, string parsing, __complex__
// dispatch, and PyNumber_Float coercion); every other shape goes
// through complex_new_impl which performs the cr/ci slot accounting
// with the PEP 387 DeprecationWarning emissions.
//
// CPython: Objects/complexobject.c:1094 actual_complex_new
// CPython: Objects/complexobject.c:1169 complex_new_impl
func ComplexCtor(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	for k := range kwargs {
		if k != "real" && k != "imag" {
			return nil, fmt.Errorf("TypeError: complex() got unexpected keyword argument '%s'", k)
		}
	}
	if len(args) > 2 {
		return nil, fmt.Errorf("TypeError: complex() takes at most 2 arguments (%d given)", len(args))
	}
	// actual_complex_new: triggered only when no kwargs and at most one
	// positional. Returns the exact-complex argument unchanged, parses
	// strings, calls __complex__, or coerces real-only objects via
	// PyNumber_Float. Never emits DeprecationWarning.
	if len(kwargs) == 0 {
		switch len(args) {
		case 0:
			return objects.NewComplex(0, 0), nil
		case 1:
			return actualComplexNew(args[0])
		}
	}
	// complex_new_impl: everything else. Resolve real and imag from the
	// merged positional/keyword bindings, then run the cr/ci slot
	// algorithm with deprecation warnings on complex-typed inputs.
	var realObj, imagObj objects.Object
	if v, ok := kwargs["real"]; ok {
		realObj = v
	}
	if v, ok := kwargs["imag"]; ok {
		imagObj = v
	}
	switch len(args) {
	case 0:
		// pure-kwargs form: realObj/imagObj already set above.
	case 1:
		if realObj != nil {
			return nil, fmt.Errorf("TypeError: complex() got multiple values for argument 'real'")
		}
		realObj = args[0]
	case 2:
		if realObj != nil {
			return nil, fmt.Errorf("TypeError: complex() got multiple values for argument 'real'")
		}
		if imagObj != nil {
			return nil, fmt.Errorf("TypeError: complex() got multiple values for argument 'imag'")
		}
		realObj = args[0]
		imagObj = args[1]
	}
	// CPython splits cr (from "real") and ci (from "imag") into
	// separate Py_complex slots and only mixes their cross-components
	// when one of the inputs is itself a complex. That ordering matters
	// for sign preservation: assigning float(arg) straight into the
	// slot keeps the -0 / +0 distinction, whereas an unconditional
	// "im += float(imag_arg)" would fold -0 into 0 via IEEE 754.
	//
	// CPython: Objects/complexobject.c:1169 complex_new_impl
	var crReal, crImag, ciReal, ciImag float64
	crIsComplex := false
	ciIsComplex := false
	origRealObj := realObj
	if realObj != nil {
		// try_complex_special_method first: if real defines __complex__,
		// retarget realObj to the resulting complex so the rest of the
		// algorithm picks up both components via the PyComplex_Check
		// branch.
		special, err := objects.TryComplexSpecialMethod(realObj)
		if err != nil {
			return nil, err
		}
		ownR := false
		if special != nil {
			realObj = special
			ownR = true
		}
		if cv, ok := realObj.(*objects.Complex); ok {
			crReal, crImag = real(cv.Complex128()), imag(cv.Complex128())
			crIsComplex = true
			// CPython: Objects/complexobject.c:1242 emit DeprecationWarning
			// whenever the post-special-method real is complex but the
			// original input cannot be coerced via nb_float/nb_index.
			// This fires for both one-arg-kwarg and two-arg shapes;
			// actual_complex_new handles the silent one-arg positional.
			_ = ownR
			if !hasRealNumberSlot(origRealObj) {
				msg := fmt.Sprintf("complex() argument 'real' must be a real number, not %s", origRealObj.Type().Name)
				if objects.DeprecWarnHook != nil {
					if werr := objects.DeprecWarnHook(msg); werr != nil {
						return nil, werr
					}
				}
			}
		} else {
			// CPython: Objects/complexobject.c:1196 TypeError when
			// post-special-method real has no nb_float, nb_index, and is
			// not complex. With try_complex_special_method already
			// applied above, "not complex" already failed for this arm,
			// so PyNumber_Float drives the conversion.
			if !hasRealNumberSlot(realObj) {
				return nil, fmt.Errorf("TypeError: complex() argument 'real' must be a real number, not %s", origRealObj.Type().Name)
			}
			rr, ferr := objects.PyNumberFloat(realObj)
			if ferr != nil {
				return nil, ferr
			}
			crReal, crImag = rr, 0
		}
	}
	if imagObj == nil {
		// "ci.real = cr.imag" — keeps the cross-term out of the
		// returned imaginary unless cr was itself complex.
		ciReal = crImag
	} else if cv, ok := imagObj.(*objects.Complex); ok {
		// CPython: Objects/complexobject.c:1269 always emit the
		// DeprecationWarning when imag is itself a complex.
		msg := fmt.Sprintf("complex() argument 'imag' must be a real number, not %s", imagObj.Type().Name)
		if objects.DeprecWarnHook != nil {
			if werr := objects.DeprecWarnHook(msg); werr != nil {
				return nil, werr
			}
		}
		ciReal, ciImag = real(cv.Complex128()), imag(cv.Complex128())
		ciIsComplex = true
	} else {
		// CPython: Objects/complexobject.c:1209 TypeError when imag has
		// no nb_float, nb_index, and is not complex. Unlike real, imag
		// is NOT routed through try_complex_special_method, so an object
		// like WithComplex (defines __complex__ only) must reject here.
		if !hasRealNumberSlot(imagObj) {
			return nil, fmt.Errorf("TypeError: complex() argument 'imag' must be a real number, not %s", imagObj.Type().Name)
		}
		ir, err := objects.PyNumberFloat(imagObj)
		if err != nil {
			return nil, err
		}
		ciReal = ir
	}
	if ciIsComplex {
		crReal -= ciImag
	}
	if crIsComplex && imagObj != nil {
		ciReal += crImag
	}
	return objects.NewComplex(crReal, ciReal), nil
}

// actualComplexNew ports actual_complex_new for the one-arg, no-kwargs
// shape. It does NOT emit DeprecationWarning; the silent fast-path is
// the whole point of the CPython split.
//
// CPython: Objects/complexobject.c:1094 actual_complex_new
func actualComplexNew(arg objects.Object) (objects.Object, error) {
	if cv, ok := arg.(*objects.Complex); ok && cv.Type() == objects.ComplexType {
		return cv, nil
	}
	if arg.Type() == objects.StrType() {
		s, _ := objects.Str(arg)
		c, perr := parseComplexString(s)
		if perr != nil {
			return nil, fmt.Errorf("ValueError: complex() arg is a malformed string")
		}
		return objects.NewComplex(real(c), imag(c)), nil
	}
	special, err := objects.TryComplexSpecialMethod(arg)
	if err != nil {
		return nil, err
	}
	if special != nil {
		return objects.NewComplex(real(special.Complex128()), imag(special.Complex128())), nil
	}
	if cv, ok := arg.(*objects.Complex); ok {
		// Complex subclass: strip subtype, return exact complex.
		return objects.NewComplex(real(cv.Complex128()), imag(cv.Complex128())), nil
	}
	if hasRealNumberSlot(arg) {
		f, ferr := objects.PyNumberFloat(arg)
		if ferr != nil {
			return nil, ferr
		}
		return objects.NewComplex(f, 0), nil
	}
	return nil, fmt.Errorf("TypeError: complex() argument must be a string or a number, not %s", arg.Type().Name)
}

// hasRealNumberSlot mirrors CPython's `nbr->nb_float || nbr->nb_index`
// check: returns true when the object's type can produce a plain real
// via __float__ or __index__. Complex objects deliberately leave both
// slots unfilled, so they fall through to the deprecation branch.
//
// CPython: Objects/complexobject.c:1242 nb_float/nb_index check
func hasRealNumberSlot(o objects.Object) bool {
	if o == nil {
		return false
	}
	nb := o.Type().Number
	if nb == nil {
		return false
	}
	return nb.Float != nil || nb.Index != nil
}

// parseComplexString ports complex_from_string_inner. It accepts every
// literal form the float constructor accepts plus the legacy "<float>j",
// "<float><signed-float>j", "<float><sign>j", "<sign>j", and "j" shapes.
// Surrounding whitespace and a single matched pair of parentheses are
// allowed. PEP 515 underscores are scrubbed from the whole literal
// before scanning, mirroring _Py_string_to_number_with_underscores.
// Unicode decimal digits and Unicode whitespace are folded to ASCII via
// pystrconv.TransformDecimalAndSpaceToASCII so byte-position scanning
// works the same way it does on a strtod-style C buffer.
//
// CPython: Objects/complexobject.c:931 complex_from_string_inner
func parseComplexString(s string) (complex128, error) {
	s = pystrconv.TransformDecimalAndSpaceToASCII(s)
	clean, ok := pystrconv.StripUnderscores(s)
	if !ok {
		return 0, errMalformedComplex
	}
	s = clean
	start := 0
	end := len(s)
	gotBracket := false
	i := 0
	for i < end && pystrconv.IsSpace(s[i]) {
		i++
	}
	if i < end && s[i] == '(' {
		gotBracket = true
		i++
		for i < end && pystrconv.IsSpace(s[i]) {
			i++
		}
	}
	var x, y float64
	// First look for forms starting with <float>.
	z, zEnd, err := pystrconv.ParseFloatPrefix(s[i:])
	if err != nil {
		return 0, err
	}
	if zEnd != 0 {
		i += zEnd
		switch {
		case i < end && (s[i] == '+' || s[i] == '-'):
			// <float><signed-float>j or <float><sign>j.
			x = z
			yv, yEnd, yerr := pystrconv.ParseFloatPrefix(s[i:])
			if yerr != nil {
				return 0, yerr
			}
			if yEnd != 0 {
				y = yv
				i += yEnd
			} else {
				if s[i] == '+' {
					y = 1.0
				} else {
					y = -1.0
				}
				i++
			}
			if i >= end || (s[i] != 'j' && s[i] != 'J') {
				return 0, errMalformedComplex
			}
			i++
		case i < end && (s[i] == 'j' || s[i] == 'J'):
			// <float>j.
			i++
			y = z
		default:
			// Bare <float>.
			x = z
		}
	} else {
		// Not starting with <float>: must be <sign>j or j.
		if i < end && (s[i] == '+' || s[i] == '-') {
			if s[i] == '+' {
				y = 1.0
			} else {
				y = -1.0
			}
			i++
		} else {
			y = 1.0
		}
		if i >= end || (s[i] != 'j' && s[i] != 'J') {
			return 0, errMalformedComplex
		}
		i++
	}
	for i < end && pystrconv.IsSpace(s[i]) {
		i++
	}
	if gotBracket {
		if i >= end || s[i] != ')' {
			return 0, errMalformedComplex
		}
		i++
		for i < end && pystrconv.IsSpace(s[i]) {
			i++
		}
	}
	if i-start != end {
		return 0, errMalformedComplex
	}
	return complex(x, y), nil
}

// errMalformedComplex is the sentinel for every parse_error path inside
// complex_from_string_inner. Callers translate it to the
// "complex() arg is a malformed string" ValueError.
var errMalformedComplex = errors.New("complex() arg is a malformed string")

// memoryViewCtor ports memoryview(object). The single argument is
// positional-or-keyword ("object"); it is wrapped in a MemoryView through
// the buffer protocol. Surplus positionals, an unknown keyword, or a missing
// argument each raise TypeError, matching the Argument Clinic signature.
//
// CPython: Objects/memoryobject.c:1006 memoryview.__new__
func memoryViewCtor(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	for k := range kwargs {
		if k != "object" {
			return nil, fmt.Errorf("TypeError: '%s' is an invalid keyword argument for memoryview()", k)
		}
	}
	if len(args) > 1 {
		return nil, fmt.Errorf("TypeError: memoryview() takes at most 1 argument (%d given)", len(args))
	}
	var obj objects.Object
	switch {
	case len(args) == 1:
		if _, ok := kwargs["object"]; ok {
			return nil, fmt.Errorf("TypeError: argument for memoryview() given by name ('object') and position (1)")
		}
		obj = args[0]
	case kwargs["object"] != nil:
		obj = kwargs["object"]
	default:
		return nil, fmt.Errorf("TypeError: memoryview() missing required argument 'object' (pos 1)")
	}
	return objects.NewMemoryView(obj)
}
