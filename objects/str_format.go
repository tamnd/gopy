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
	out, err := format.FormatString(u.v, parsed)
	if err != nil {
		return "", fmt.Errorf("ValueError: %w", err)
	}
	return out, nil
}
