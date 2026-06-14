// __format__ wiring for str. The actual mini-language parser lives in
// the format package (Spec, ParseSpec, FormatString); this file just
// glues the str type's Format slot to it.
//
// CPython: Python/formatter_unicode.c:1559 _PyUnicode_FormatAdvancedWriter

package objects

import (
	"fmt"

	"github.com/tamnd/gopy/format"
)

func init() {
	strType.Format = unicodeFormat
	// str.__format__ must be reachable both as the tp_str format slot
	// (above) and as an explicit method, because code like enum's
	// __format__ calls str.__format__(str(self), spec) directly. Without
	// this descriptor that lookup walks str -> object and lands on
	// object.__format__, which rejects any non-empty spec.
	//
	// CPython: Objects/unicodeobject.c:15564 unicode_methods __format__
	SetTypeDescr(strType, "__format__", NewMethodDescrConv(strType, "__format__", MethO, strDunderFormatMethod))
}

// strDunderFormatMethod is str.__format__(self, format_spec). An empty
// spec returns str(self); a non-empty spec runs the string renderer.
// (Distinct from strFormatMethod, which backs str.format().)
//
// CPython: Python/formatter_unicode.c:1559 _PyUnicode_FormatAdvancedWriter
func strDunderFormatMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __format__() takes exactly one argument (%d given)", len(args)-1)
	}
	spec, ok := args[1].(*Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: __format__() argument 1 must be str, not %s", typeNameOf(args[1]))
	}
	out, err := unicodeFormat(args[0], spec.v)
	if err != nil {
		return nil, err
	}
	return NewStr(out), nil
}

// unicodeFormat ports format_string_internal: parse the spec then run
// the string through it. An empty spec yields the raw value, matching
// CPython's fast path.
//
// CPython: Python/formatter_unicode.c:862 format_string_internal
// CPython: Python/formatter_unicode.c:1559 _PyUnicode_FormatAdvancedWriter
func unicodeFormat(o Object, spec string) (string, error) {
	u, ok := o.(*Unicode)
	if !ok {
		return "", fmt.Errorf(
			"TypeError: descriptor '__format__' requires a 'str' object but received a '%s'",
			o.Type().Name)
	}
	if spec == "" {
		return u.v, nil
	}
	parsed, err := format.ParseSpec(spec)
	if err != nil {
		return "", fmt.Errorf("ValueError: %w", err)
	}
	switch parsed.Type {
	case 0, 's':
		// CPython's string formatter only accepts the empty
		// presentation type and 's'. Anything else lands in
		// unknown_presentation_type so the rendered ValueError
		// names the unsupported code.
	default:
		return "", unknownPresentationType(parsed.Type, o.Type().Name)
	}
	out, err := format.FormatString(u.v, parsed)
	if err != nil {
		return "", fmt.Errorf("ValueError: %w", err)
	}
	return out, nil
}
