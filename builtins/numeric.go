// Port of the numeric / formatting panel from Python/bltinmodule.c:
// abs, divmod, pow, chr, ord, bin, oct, hex, ascii, format.

package builtins

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/tamnd/gopy/abstract"
	"github.com/tamnd/gopy/objects"
)

// Abs ports builtin_abs: PyNumber_Absolute.
//
// CPython: Python/bltinmodule.c:435 builtin_abs
func Abs(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: abs() takes exactly one argument (%d given)", len(args))
	}
	return abstract.Absolute(args[0])
}

// Divmod ports builtin_divmod: PyNumber_Divmod.
//
// CPython: Python/bltinmodule.c:786 builtin_divmod
func Divmod(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: divmod expected 2 arguments, got %d", len(args))
	}
	return abstract.Divmod(args[0], args[1])
}

// Pow ports builtin_pow_impl: PyNumber_Power. base, exp, and mod are all
// positional-or-keyword (mod defaults to None), matching the clinic
// signature so pow(base=2, exp=4) and functools.partial(pow, mod=10)
// both work.
//
// CPython: Python/bltinmodule.c:1559 builtin_pow_impl
func Pow(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	base, exp, mod, err := bindPowArgs(args, kwargs)
	if err != nil {
		return nil, err
	}
	return abstract.Power(base, exp, mod)
}

// bindPowArgs maps positional/keyword arguments onto (base, exp, mod),
// rejecting duplicates and unknown keywords the way the clinic-generated
// parser does. mod defaults to None (the two-argument power form).
//
// CPython: Python/clinic/bltinmodule.c.h builtin_pow
func bindPowArgs(args []objects.Object, kwargs map[string]objects.Object) (base, exp, mod objects.Object, err error) {
	mod = objects.None()
	if len(args) > 3 {
		return nil, nil, nil, fmt.Errorf("TypeError: pow expected at most 3 arguments, got %d", len(args))
	}
	modSet := false
	if len(args) >= 1 {
		base = args[0]
	}
	if len(args) >= 2 {
		exp = args[1]
	}
	if len(args) >= 3 {
		mod = args[2]
		modSet = true
	}
	for k, v := range kwargs {
		switch k {
		case "base":
			if base != nil {
				return nil, nil, nil, fmt.Errorf("TypeError: argument for pow() given by name ('base') and position (1)")
			}
			base = v
		case "exp":
			if exp != nil {
				return nil, nil, nil, fmt.Errorf("TypeError: argument for pow() given by name ('exp') and position (2)")
			}
			exp = v
		case "mod":
			if modSet {
				return nil, nil, nil, fmt.Errorf("TypeError: argument for pow() given by name ('mod') and position (3)")
			}
			mod = v
			modSet = true
		default:
			return nil, nil, nil, fmt.Errorf("TypeError: '%s' is an invalid keyword argument for pow()", k)
		}
	}
	if base == nil {
		return nil, nil, nil, fmt.Errorf("TypeError: pow() missing required argument 'base' (pos 1)")
	}
	if exp == nil {
		return nil, nil, nil, fmt.Errorf("TypeError: pow() missing required argument 'exp' (pos 2)")
	}
	return base, exp, mod, nil
}

// Chr ports builtin_chr: returns the one-character string for the
// Unicode code point i. Range is 0..0x10FFFF.
//
// CPython: Python/bltinmodule.c:797 builtin_chr_impl
func Chr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: chr() takes exactly one argument (%d given)", len(args))
	}
	i, err := indexAsInt(args[0])
	if err != nil {
		return nil, err
	}
	v, ok := i.Int64()
	if !ok || v < 0 || v > 0x10FFFF {
		return nil, fmt.Errorf("ValueError: chr() arg not in range(0x110000)")
	}
	// CPython: Python/bltinmodule.c:809 PyUnicode_FromOrdinal short-circuit
	// to get_latin1_char for ordinals < 256.
	if v < 256 {
		return objects.GetLatin1Char(int(v)), nil
	}
	return objects.NewStr(string(rune(v))), nil
}

// Ord ports builtin_ord: takes a one-character string and returns its
// code point.
//
// CPython: Python/bltinmodule.c:1532 builtin_ord
func Ord(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: ord() takes exactly one argument (%d given)", len(args))
	}
	// CPython: Python/bltinmodule.c:1507 builtin_ord_impl
	// Accepts str (length-1), bytes (length-1), or bytearray (length-1).
	// CPython: Python/bltinmodule.c:1507 builtin_ord_impl
	// Accepts str (length-1), bytes (length-1), or bytearray (length-1).
	switch v := args[0].(type) {
	case *objects.Unicode:
		s := v.Value()
		if utf8.RuneCountInString(s) != 1 {
			return nil, fmt.Errorf("TypeError: ord() expected a character, but string of length %d found", utf8.RuneCountInString(s))
		}
		r, _ := utf8.DecodeRuneInString(s)
		return objects.NewInt(int64(r)), nil
	case *objects.Bytes:
		b := v.Bytes()
		if len(b) != 1 {
			return nil, fmt.Errorf("TypeError: ord() expected a character, but bytes of length %d found", len(b))
		}
		return objects.NewInt(int64(b[0])), nil
	case *objects.ByteArray:
		b := v.Bytes()
		if len(b) != 1 {
			return nil, fmt.Errorf("TypeError: ord() expected a character, but bytearray of length %d found", len(b))
		}
		return objects.NewInt(int64(b[0])), nil
	}
	return nil, fmt.Errorf("TypeError: ord() expected string of length 1, but %s found", args[0].Type().Name)
}

// Bin ports builtin_bin: PyNumber_ToBase(x, 2) with the "0b" prefix
// and a leading "-" for negatives.
//
// CPython: Python/bltinmodule.c:551 builtin_bin
func Bin(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return formatIntBase(args, "bin", 2, "0b")
}

// Oct ports builtin_oct: PyNumber_ToBase(x, 8).
//
// CPython: Python/bltinmodule.c:1492 builtin_oct
func Oct(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return formatIntBase(args, "oct", 8, "0o")
}

// Hex ports builtin_hex: PyNumber_ToBase(x, 16).
//
// CPython: Python/bltinmodule.c:1395 builtin_hex
func Hex(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return formatIntBase(args, "hex", 16, "0x")
}

func formatIntBase(args []objects.Object, name string, base int, prefix string) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: %s() takes exactly one argument (%d given)", name, len(args))
	}
	i, ok := args[0].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: %s argument must be an integer, not '%s'", name, args[0].Type().Name)
	}
	bi := i.BigInt()
	sign := ""
	if bi.Sign() < 0 {
		sign = "-"
		bi.Neg(bi)
	}
	return objects.NewStr(sign + prefix + bi.Text(base)), nil
}

// Ascii ports builtin_ascii: PyObject_ASCII. Returns repr(o) with any
// non-ASCII code points replaced by \xHH / \uHHHH / \UHHHHHHHH escapes.
// When repr is already all-ASCII, returns the repr Object directly so
// str subclass types are preserved.
//
// CPython: Python/bltinmodule.c:540 builtin_ascii
func ASCII(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: ascii() takes exactly one argument (%d given)", len(args))
	}
	reprObj, err := objects.ReprObject(args[0])
	if err != nil {
		return nil, err
	}
	reprStr := reprObj.(*objects.Unicode).Value()
	escaped := asciiEscape(reprStr)
	if escaped == reprStr {
		// All-ASCII: return repr object as-is, preserving any str subclass.
		// CPython: Python/bltinmodule.c:555 PyUnicode_IS_ASCII fast path
		return reprObj, nil
	}
	return objects.NewStr(escaped), nil
}

func asciiEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r < 0x80:
			b.WriteRune(r)
		case r <= 0xFF:
			fmt.Fprintf(&b, `\x%02x`, r)
		case r <= 0xFFFF:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			fmt.Fprintf(&b, `\U%08x`, r)
		}
	}
	return b.String()
}

// Format ports builtin_format: PyObject_Format. The empty spec is a
// shortcut for str(value); a non-empty spec routes through the
// format-spec mini-language for str / int / float values.
//
// CPython: Python/bltinmodule.c:797 builtin_format_impl
func Format(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("TypeError: format expected 1 or 2 arguments, got %d", len(args))
	}
	specStr := ""
	if len(args) == 2 {
		if args[1].Type() != objects.StrType() {
			return nil, fmt.Errorf("TypeError: format() argument 2 must be str, not %s", args[1].Type().Name)
		}
		specStr, _ = objects.Str(args[1])
	}
	out, err := objects.Format(args[0], specStr)
	if err != nil {
		return nil, err
	}
	return objects.NewStr(out), nil
}
