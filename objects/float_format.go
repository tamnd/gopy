// __format__ wiring for float. The format-spec parser and IEEE-754
// renderer live in the format package; this file glues the
// FloatType.Format slot to it.
//
// Findings worth noting for future readers:
//
//  1. The format-spec parser and float renderer in format/format.go
//     pre-dated this wiring. The only gap was that FloatType.Format
//     was left at its zero value, so the protocol-level Format helper
//     fell back to objectFormatDescr, which rejects every non-empty
//     spec with TypeError. Wiring this slot is enough to make
//     '{:.2f}'.format(3.14) and f'{x:10.4g}' work end-to-end. The
//     same gap affected IntType (see objects/long_format.go).
//
//  2. The "empty spec calls str()" rule is enforced by the caller
//     (Format() in objects/format.go). We never see the empty-string
//     case here; format.ParseSpec accepts it harmlessly but the
//     caller routes around us first.
//
// CPython: Python/formatter_unicode.c:1665 _PyFloat_FormatAdvancedWriter

package objects

import (
	"fmt"

	"github.com/tamnd/gopy/format"
)

func init() {
	FloatType.Format = floatFormat
	// As with int, _PyFloat_FormatAdvancedWriter is reachable through
	// instance.__format__(spec) as well as PyObject_Format. Without the
	// descriptor the MRO inherits objectFormatDescr and rejects every
	// non-empty spec.
	//
	// CPython: Objects/floatobject.c:1832 float_methods __format__
	SetTypeDescr(FloatType, "__format__", NewMethodDescrConv(FloatType, "__format__", MethO, floatFormatMethod))
}

// floatFormatMethod is float.__format__(self, format_spec). An empty
// spec returns str(self); a non-empty spec runs the float renderer.
//
// CPython: Python/formatter_unicode.c:1665 _PyFloat_FormatAdvancedWriter
func floatFormatMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __format__() takes exactly one argument (%d given)", len(args)-1)
	}
	spec, ok := args[1].(*Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: __format__() argument 1 must be str, not %s", typeNameOf(args[1]))
	}
	if spec.v == "" {
		return StrObject(args[0])
	}
	out, err := floatFormat(args[0], spec.v)
	if err != nil {
		return nil, err
	}
	return NewStr(out), nil
}

// floatFormat ports _PyFloat_FormatAdvancedWriter. Non-empty spec must
// use one of 'e'/'E'/'f'/'F'/'g'/'G'/'n'/'%' or the empty type code.
// The renderer in format.FormatFloat handles fill/align/sign/width/
// precision/group/alt-form per the mini-language.
//
// CPython: Python/formatter_unicode.c:1665 _PyFloat_FormatAdvancedWriter
func floatFormat(o Object, spec string) (string, error) {
	f, ok := o.(*Float)
	if !ok {
		return "", fmt.Errorf(
			"TypeError: descriptor '__format__' requires a 'float' object but received a '%s'",
			o.Type().Name)
	}
	parsed, err := format.ParseSpec(spec)
	if err != nil {
		return "", fmt.Errorf("ValueError: %w", err)
	}
	switch parsed.Type {
	case 0, 'e', 'E', 'f', 'F', 'g', 'G', '%', 'n':
		// CPython's float formatter accepts these presentation
		// types; everything else falls through to the
		// unknown-format-code branch.
	default:
		return "", unknownPresentationType(parsed.Type, o.Type().Name)
	}
	out, err := format.FormatFloat(f.Float64(), parsed)
	if err != nil {
		return "", fmt.Errorf("ValueError: %w", err)
	}
	return out, nil
}
