// TemplateStr is the runtime object produced by a t-string literal
// (PEP 750). The object holds the interleaved string parts and
// Interpolation values from the literal; it is iterable and the
// individual pieces are accessed through the Template protocol.
//
// CPython: Objects/templateobject.c _PyTemplate_Build
//
// The full Template protocol (iteration, __class_getitem__, etc.) is
// left for a later pass. What matters here is that the type exists so
// that `type(t"")` in annotationlib.py returns a real type object
// rather than crashing.

package objects

import "fmt"

// TemplateStrType is the type singleton for t-string objects.
//
// CPython: Objects/templateobject.c PyTemplate_Type
var TemplateStrType = NewType("string.templatestring", []*Type{objectType})

func init() {
	TemplateStrType.Repr = templateStrRepr
	TemplateStrType.Str = templateStrRepr
}

// TemplateStr holds the two parallel sequences that BUILD_TEMPLATE
// receives: the interleaved string Constant parts and the evaluated
// Interpolation values.
type TemplateStr struct {
	Header
	strings        Object // tuple of str
	interpolations Object // tuple of Interpolation
}

// NewTemplateStr constructs a TemplateStr from the two BUILD_TEMPLATE
// arguments. Either may be nil if the t-string has no interpolations or
// no literal parts.
//
// CPython: Objects/templateobject.c _PyTemplate_Build
func NewTemplateStr(strings, interpolations Object) *TemplateStr {
	ts := &TemplateStr{strings: strings, interpolations: interpolations}
	ts.Header.init(TemplateStrType)
	return ts
}

func templateStrRepr(o Object) (string, error) {
	ts, ok := o.(*TemplateStr)
	if !ok {
		return "", fmt.Errorf("TypeError: expected TemplateStr")
	}
	_ = ts
	return "templatestring(...)", nil
}
