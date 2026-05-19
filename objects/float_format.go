// __format__ wiring for float. The format-spec parser and IEEE-754
// renderer live in the format package; this file glues the
// FloatType.Format slot to it.
//
// CPython: Python/formatter_unicode.c:1665 _PyFloat_FormatAdvancedWriter

package objects

import (
	"fmt"

	"github.com/tamnd/gopy/format"
)

func init() {
	FloatType.Format = floatFormat
}

// floatFormat ports _PyFloat_FormatAdvancedWriter. Empty spec falls
// through to the protocol-level Format helper (which calls Str); a
// non-empty spec must use one of 'e'/'E'/'f'/'F'/'g'/'G'/'n'/'%' or
// the default ''.
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
	out, err := format.FormatFloat(f.Float64(), parsed)
	if err != nil {
		return "", fmt.Errorf("ValueError: %w", err)
	}
	return out, nil
}
