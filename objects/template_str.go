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
var TemplateStrType = NewType("string.templatelib.Template", []*Type{objectType})

func init() {
	TemplateStrType.Repr = templateStrRepr
	TemplateStrType.Str = templateStrRepr
	TemplateStrType.Iter = templateStrIter
	// strings / interpolations / values match the PyMemberDef +
	// PyGetSetDef rows on PyTemplate_Type so attribute access from
	// templatelib.py (`t.interpolations[0]`) resolves to the tuples
	// the BUILD_TEMPLATE opcode stamped onto the object.
	//
	// CPython: Objects/templateobject.c:333 template_members
	// CPython: Objects/templateobject.c:339 template_getset
	SetTypeDescr(TemplateStrType, "strings", NewGetSetDescr("strings",
		func(o Object) (Object, error) {
			ts, ok := o.(*TemplateStr)
			if !ok {
				return nil, errTypeMismatchForTemplate("strings")
			}
			if ts.strings == nil {
				return NewTuple(nil), nil
			}
			return ts.strings, nil
		}, nil))
	SetTypeDescr(TemplateStrType, "interpolations", NewGetSetDescr("interpolations",
		func(o Object) (Object, error) {
			ts, ok := o.(*TemplateStr)
			if !ok {
				return nil, errTypeMismatchForTemplate("interpolations")
			}
			if ts.interpolations == nil {
				return NewTuple(nil), nil
			}
			return ts.interpolations, nil
		}, nil))
	SetTypeDescr(TemplateStrType, "values", NewGetSetDescr("values",
		func(o Object) (Object, error) {
			ts, ok := o.(*TemplateStr)
			if !ok {
				return nil, errTypeMismatchForTemplate("values")
			}
			return templateValues(ts)
		}, nil))
	AddIterSlotWrappers(TemplateStrType)
	// sq_concat lets `t1 + t2` produce a new Template with the joined
	// strings (last(left) + first(right) merged) and concatenated
	// interpolations.
	//
	// CPython: Objects/templateobject.c:345 template_as_sequence
	TemplateStrType.Sequence = &SequenceMethods{
		Concat: templateConcat,
	}
}

// templateConcat is sq_concat for Template. Templates concatenate
// only with templates; any other RHS type raises TypeError that names
// both operand types in CPython's message format.
//
// CPython: Objects/templateobject.c:301 _PyTemplate_Concat
func templateConcat(a, b Object) (Object, error) {
	left, ok := a.(*TemplateStr)
	if !ok {
		return nil, fmt.Errorf(
			"TypeError: can only concatenate string.templatelib.Template (not \"%s\") to string.templatelib.Template",
			typeNameOf(a))
	}
	right, ok := b.(*TemplateStr)
	if !ok {
		return nil, fmt.Errorf(
			"TypeError: can only concatenate string.templatelib.Template (not \"%s\") to string.templatelib.Template",
			typeNameOf(b))
	}
	newStrings, err := templateStringsConcat(left.strings, right.strings)
	if err != nil {
		return nil, err
	}
	newInterps, err := SequenceConcat(left.interpolations, right.interpolations)
	if err != nil {
		return nil, err
	}
	return NewTemplateStr(newStrings, newInterps), nil
}

// templateStringsConcat folds the boundary entries of the two
// strings tuples into a single concatenated str, returning a tuple of
// length (len(left) + len(right) - 1).
//
// CPython: Objects/templateobject.c:250 template_strings_concat
func templateStringsConcat(left, right Object) (Object, error) {
	leftTuple, ok := left.(*Tuple)
	if !ok {
		return nil, fmt.Errorf("TypeError: Template.strings is not a tuple")
	}
	rightTuple, ok := right.(*Tuple)
	if !ok {
		return nil, fmt.Errorf("TypeError: Template.strings is not a tuple")
	}
	leftLen := leftTuple.Len()
	rightLen := rightTuple.Len()
	if leftLen == 0 || rightLen == 0 {
		return nil, fmt.Errorf("ValueError: Template.strings must not be empty")
	}
	leftLast, ok := leftTuple.Item(leftLen - 1).(*Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: Template.strings entries must be str")
	}
	rightFirst, ok := rightTuple.Item(0).(*Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: Template.strings entries must be str")
	}
	concat := NewStr(leftLast.Value() + rightFirst.Value())
	out := make([]Object, 0, leftLen+rightLen-1)
	for i := 0; i < leftLen-1; i++ {
		out = append(out, leftTuple.Item(i))
	}
	out = append(out, concat)
	for i := 1; i < rightLen; i++ {
		out = append(out, rightTuple.Item(i))
	}
	return NewTuple(out), nil
}

func errTypeMismatchForTemplate(attr string) error {
	return fmt.Errorf("TypeError: descriptor '%s' for 'Template' objects doesn't apply to a different type", attr)
}

// templateValues mirrors template_values_get. Returns a tuple of the
// .value field of each Interpolation in self.interpolations.
//
// CPython: Objects/templateobject.c:314 template_values_get
func templateValues(ts *TemplateStr) (Object, error) {
	if ts.interpolations == nil {
		return NewTuple(nil), nil
	}
	tup, ok := ts.interpolations.(*Tuple)
	if !ok {
		return nil, fmt.Errorf("TypeError: interpolations is not a tuple")
	}
	out := make([]Object, tup.Len())
	for i := 0; i < tup.Len(); i++ {
		item := tup.Item(i)
		if interp, isInterp := item.(*Interpolation); isInterp {
			out[i] = interp.Value
		} else {
			out[i] = item
		}
	}
	return NewTuple(out), nil
}

// templateStrIter mirrors template_iter: yields interleaved string
// parts and Interpolation objects, skipping zero-length strings.
//
// CPython: Objects/templateobject.c:221 template_iter
func templateStrIter(o Object) (Object, error) {
	ts := o.(*TemplateStr)
	var strs, interps []Object
	if tup, ok := ts.strings.(*Tuple); ok {
		strs = make([]Object, tup.Len())
		for i := 0; i < tup.Len(); i++ {
			strs[i] = tup.Item(i)
		}
	}
	if tup, ok := ts.interpolations.(*Tuple); ok {
		interps = make([]Object, tup.Len())
		for i := 0; i < tup.Len(); i++ {
			interps[i] = tup.Item(i)
		}
	}
	out := make([]Object, 0, len(strs)+len(interps))
	for i, s := range strs {
		if u, ok := s.(*Unicode); ok && u.Value() == "" {
			// empty string -> skip and emit the matching interp
		} else {
			out = append(out, s)
		}
		if i < len(interps) {
			out = append(out, interps[i])
		}
	}
	return Iter(NewTuple(out))
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
	ts.init(TemplateStrType)
	return ts
}

func templateStrRepr(o Object) (string, error) {
	ts, ok := o.(*TemplateStr)
	if !ok {
		return "", fmt.Errorf("TypeError: expected TemplateStr")
	}
	strs := "()"
	interps := "()"
	if ts.strings != nil {
		s, err := Repr(ts.strings)
		if err != nil {
			return "", err
		}
		strs = s
	}
	if ts.interpolations != nil {
		s, err := Repr(ts.interpolations)
		if err != nil {
			return "", err
		}
		interps = s
	}
	return fmt.Sprintf("Template(strings=%s, interpolations=%s)", strs, interps), nil
}

// Interpolation is the runtime value produced by BUILD_INTERPOLATION
// for each `{expr}` inside a t-string literal. It is constructed by
// _PyInterpolation_Build and exposed to user code through
// Template.interpolations.
//
// CPython: Objects/interpolationobject.c interpolationobject
type Interpolation struct {
	Header
	Value      Object
	Expression Object
	Conversion Object
	FormatSpec Object
}

// InterpolationType is the type singleton for Interpolation.
//
// CPython: Objects/interpolationobject.c:145 _PyInterpolation_Type
var InterpolationType = NewType("string.templatelib.Interpolation", []*Type{objectType})

func init() {
	InterpolationType.Repr = interpolationRepr
	InterpolationType.Str = interpolationRepr
	// PyMemberDef rows from interpolation_members. All four are
	// READONLY, so DescrSet is omitted.
	//
	// CPython: Objects/interpolationobject.c:120 interpolation_members
	for _, attr := range []struct {
		name string
		get  func(*Interpolation) Object
	}{
		{"value", func(i *Interpolation) Object { return i.Value }},
		{"expression", func(i *Interpolation) Object { return i.Expression }},
		{"conversion", func(i *Interpolation) Object { return i.Conversion }},
		{"format_spec", func(i *Interpolation) Object { return i.FormatSpec }},
	} {
		name := attr.name
		getter := attr.get
		SetTypeDescr(InterpolationType, name, NewGetSetDescr(name,
			func(o Object) (Object, error) {
				ip, ok := o.(*Interpolation)
				if !ok {
					return nil, fmt.Errorf("TypeError: descriptor '%s' for 'Interpolation' objects doesn't apply", name)
				}
				if v := getter(ip); v != nil {
					return v, nil
				}
				return None(), nil
			}, nil))
	}
	// __match_args__ tuple registered at type-init time by
	// _PyInterpolation_InitTypes.
	//
	// CPython: Objects/interpolationobject.c:163 _PyInterpolation_InitTypes
	SetTypeDescr(InterpolationType, "__match_args__", NewTuple([]Object{
		NewStr("value"),
		NewStr("expression"),
		NewStr("conversion"),
		NewStr("format_spec"),
	}))
}

// NewInterpolation mirrors _PyInterpolation_Build. conversion encodes
// the FVC_* tag in CPython: 0 = no conversion, 1 = !s, 2 = !r, 3 = !a.
//
// CPython: Objects/interpolationobject.c:188 _PyInterpolation_Build
func NewInterpolation(value, expr Object, conversion int, formatSpec Object) (*Interpolation, error) {
	if expr == nil {
		expr = NewStr("")
	}
	if formatSpec == nil {
		formatSpec = NewStr("")
	}
	var conv Object
	switch conversion {
	case 0:
		conv = None()
	case 1:
		conv = NewStr("s")
	case 2:
		conv = NewStr("r")
	case 3:
		conv = NewStr("a")
	default:
		return nil, fmt.Errorf("SystemError: Interpolation() argument 'conversion' must be one of 's', 'a' or 'r'")
	}
	ip := &Interpolation{
		Value:      value,
		Expression: expr,
		Conversion: conv,
		FormatSpec: formatSpec,
	}
	ip.init(InterpolationType)
	return ip, nil
}

func interpolationRepr(o Object) (string, error) {
	ip, ok := o.(*Interpolation)
	if !ok {
		return "", fmt.Errorf("TypeError: expected Interpolation")
	}
	val, err := Repr(ip.Value)
	if err != nil {
		return "", err
	}
	expr, err := Repr(ip.Expression)
	if err != nil {
		return "", err
	}
	conv, err := Repr(ip.Conversion)
	if err != nil {
		return "", err
	}
	fmtSpec, err := Repr(ip.FormatSpec)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Interpolation(%s, %s, %s, %s)", val, expr, conv, fmtSpec), nil
}
