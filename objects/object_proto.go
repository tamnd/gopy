// Object-protocol surface ported from Objects/object.c. These are the
// PyObject_* free functions that route through tp slots: optional
// getattr, callable checks, bytes / ASCII conversions, dir, etc. The
// existing protocol.go covers the basics (Repr, Str, Hash, GetAttr,
// SetAttr, RichCmp, IsTruthy) and this file fills in the rest of the
// public API from object.c so callers do not have to poke at type
// slots directly.

package objects

import (
	"errors"
	"fmt"
	"strings"
)

// ObjectBytes mirrors PyObject_Bytes: bytes(v). Bytes objects pass
// through; otherwise we look up __bytes__ and call it, requiring a
// bytes return. With no __bytes__ available we fall back to
// PyBytes_FromObject (gopy currently only handles the bytes-from-bytes
// and bytes-from-str shapes here). Named ObjectBytes because Bytes is
// the type singleton.
//
// CPython: Objects/object.c:870 PyObject_Bytes
func ObjectBytes(v Object) (*Bytes, error) {
	if v == nil {
		return NewBytesFromString("<NULL>"), nil
	}
	if b, ok := v.(*Bytes); ok {
		return b, nil
	}
	descr, _ := LookupDescriptor(v.Type(), "__bytes__")
	if descr != nil {
		fn, err := bindDescriptor(descr, v)
		if err != nil {
			return nil, err
		}
		out, err := Call(fn, NewTuple(nil), nil)
		if err != nil {
			return nil, err
		}
		b, ok := out.(*Bytes)
		if !ok {
			return nil, fmt.Errorf("TypeError: __bytes__ returned non-bytes (type %s)", out.Type().Name)
		}
		return b, nil
	}
	if s, ok := v.(*Unicode); ok {
		return NewBytesFromString(s.Value()), nil
	}
	return nil, fmt.Errorf("TypeError: cannot convert '%s' object to bytes", v.Type().Name)
}

// ASCII mirrors PyObject_ASCII: repr(v) with every non-ASCII
// codepoint backslash-escaped. CPython routes the escape through
// _PyUnicode_AsASCIIString / PyUnicode_DecodeASCII; the Go side does
// the same thing with strconv-style \xHH / \uHHHH / \UHHHHHHHH
// expansion so callers see the same shape.
//
// CPython: Objects/object.c:843 PyObject_ASCII
func ASCII(v Object) (string, error) {
	repr, err := Repr(v)
	if err != nil {
		return "", err
	}
	if isASCII(repr) {
		return repr, nil
	}
	var b strings.Builder
	b.Grow(len(repr))
	for _, r := range repr {
		switch {
		case r < 0x80:
			b.WriteByte(byte(r))
		case r < 0x100:
			fmt.Fprintf(&b, "\\x%02x", r)
		case r < 0x10000:
			fmt.Fprintf(&b, "\\u%04x", r)
		default:
			fmt.Fprintf(&b, "\\U%08x", r)
		}
	}
	return b.String(), nil
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// Not mirrors PyObject_Not: bool(not v). Delegates to IsTruthy and
// flips the result; an error from IsTruthy propagates.
//
// CPython: Objects/object.c:2088 PyObject_Not
func Not(v Object) (bool, error) {
	t, err := IsTruthy(v)
	if err != nil {
		return false, err
	}
	return !t, nil
}

// Callable mirrors PyCallable_Check: an object is callable when its
// type defines a Call or Vectorcall slot. Returns false for nil.
//
// CPython: Objects/object.c:2100 PyCallable_Check
func Callable(o Object) bool {
	if o == nil {
		return false
	}
	tp := o.Type()
	return tp.Call != nil || tp.Vectorcall != nil
}

// SelfIter mirrors PyObject_SelfIter: the canonical tp_iter
// implementation for iterators that act as their own iterator.
// CPython hands back a new strong reference; gopy returns the same
// object since references are tracked by the Go GC.
//
// CPython: Objects/object.c:1548 PyObject_SelfIter
func SelfIter(o Object) (Object, error) {
	return o, nil
}

// LookupAttr mirrors PyObject_GetOptionalAttr: like GetAttr but
// suppresses AttributeError. Returns (nil, nil) when the attribute
// is missing, (value, nil) when found, and (nil, err) for any other
// failure.
//
// CPython: Objects/object.c:1324 PyObject_GetOptionalAttr
func LookupAttr(o Object, name Object) (Object, error) {
	if name == nil || name.Type() != strType {
		return nil, fmt.Errorf("TypeError: attribute name must be string, not '%s'", typeNameOf(name))
	}
	v, err := GetAttr(o, name)
	if err != nil {
		if isAttributeError(err) {
			return nil, nil
		}
		return nil, err
	}
	return v, nil
}

// LookupAttrString is the C-string convenience of LookupAttr.
//
// CPython: Objects/object.c:1392 PyObject_GetOptionalAttrString
func LookupAttrString(o Object, name string) (Object, error) {
	return LookupAttr(o, NewStr(name))
}

// HasAttr mirrors PyObject_HasAttrWithError: routes through
// LookupAttr and reports whether the attribute is present. Errors
// other than AttributeError propagate.
//
// CPython: Objects/object.c:1417 PyObject_HasAttrWithError
func HasAttr(o Object, name Object) (bool, error) {
	v, err := LookupAttr(o, name)
	if err != nil {
		return false, err
	}
	return v != nil, nil
}

// HasAttrString is the C-string convenience of HasAttr.
//
// CPython: Objects/object.c:1189 PyObject_HasAttrStringWithError
func HasAttrString(o Object, name string) (bool, error) {
	return HasAttr(o, NewStr(name))
}

// Dir mirrors PyObject_Dir: introspect o's attributes. When o has a
// __dir__ method we call it and sort the result; otherwise we fall
// back to walking the type's descriptor table and the instance dict.
//
// CPython: Objects/object.c:2189 PyObject_Dir
func Dir(o Object) (*List, error) {
	if o == nil {
		return nil, fmt.Errorf("TypeError: dir(None) has no frame to inspect")
	}
	if descr, _ := LookupDescriptor(o.Type(), "__dir__"); descr != nil {
		fn, err := bindDescriptor(descr, o)
		if err != nil {
			return nil, err
		}
		out, err := Call(fn, NewTuple(nil), nil)
		if err != nil {
			return nil, err
		}
		l, err := dirToSortedList(out)
		if err != nil {
			return nil, err
		}
		return l, nil
	}
	return defaultDir(o), nil
}

// defaultDir is the fallback that runs when a type does not expose
// __dir__: walk the type's MRO descriptor table plus any instance
// dict and return a sorted unique list. Matches CPython's
// object___dir___impl shape, minus the __class__ chasing that the
// type-level dir() handles separately.
//
// CPython: Objects/typeobject.c:7906 object___dir___impl
func defaultDir(o Object) *List {
	seen := map[string]struct{}{}
	for _, base := range o.Type().MRO {
		if base == nil {
			continue
		}
		if descrs, ok := typeDescrTable[base]; ok {
			for name := range descrs {
				seen[name] = struct{}{}
			}
		}
	}
	if inst, ok := o.(*Instance); ok {
		if d := inst.Dict(); d != nil {
			for _, k := range d.Keys() {
				if s, ok := k.(*Unicode); ok {
					seen[s.Value()] = struct{}{}
				}
			}
		}
	}
	items := make([]Object, 0, len(seen))
	for name := range seen {
		items = append(items, NewStr(name))
	}
	l := NewList(items)
	_ = l.Sort(nil, false)
	return l
}

// dirToSortedList materializes __dir__'s return into a sorted list.
// CPython runs PySequence_List then PyList_Sort; gopy does the same
// in two steps.
//
// CPython: Objects/object.c:2156 _dir_object
func dirToSortedList(v Object) (*List, error) {
	it, err := Iter(v)
	if err != nil {
		return nil, err
	}
	next := it.Type().IterNext
	if next == nil {
		return nil, fmt.Errorf("TypeError: '%s' object is not iterable", v.Type().Name)
	}
	var items []Object
	for {
		x, err := next(it)
		if err != nil {
			if errors.Is(err, ErrStopIteration) {
				break
			}
			return nil, err
		}
		items = append(items, x)
	}
	l := NewList(items)
	if err := l.Sort(nil, false); err != nil {
		return nil, err
	}
	return l, nil
}

// GetMethod mirrors _PyObject_GetMethod: the fast path used by the
// LOAD_METHOD opcode. Returns (bound-or-callable, true) when the
// attribute resolves to a method descriptor and the caller can keep
// self separate, otherwise (value, false) for a regular attribute.
//
// CPython: Objects/object.c:1579 _PyObject_GetMethod
func GetMethod(o Object, name Object) (Object, bool, error) {
	if name == nil || name.Type() != strType {
		return nil, false, fmt.Errorf("TypeError: attribute name must be string, not '%s'", typeNameOf(name))
	}
	tp := o.Type()
	if tp.Getattro != nil && fnPtr(tp.Getattro) != fnPtr(GenericGetAttr) {
		v, err := GetAttr(o, name)
		return v, false, err
	}
	descr, _ := LookupDescriptor(tp, attrNameStr(name))
	if descr != nil {
		dtp := descr.Type()
		if dtp.DescrSet != nil && dtp.DescrGet != nil {
			v, err := dtp.DescrGet(descr, o, tp)
			return v, false, err
		}
		if isMethodLike(descr) {
			return descr, true, nil
		}
	}
	if inst, ok := o.(*Instance); ok {
		if d := inst.Dict(); d != nil {
			if v, _ := d.GetItem(name); v != nil {
				return v, false, nil
			}
		}
	}
	if descr != nil {
		if dg := descr.Type().DescrGet; dg != nil {
			v, err := dg(descr, o, tp)
			return v, false, err
		}
		return descr, false, nil
	}
	v, err := GetAttr(o, name)
	return v, false, err
}

// isMethodLike reports whether a descriptor behaves as an unbound
// method. CPython tags these with Py_TPFLAGS_METHOD_DESCRIPTOR; that
// flag is set on PyFunction_Type and PyMethodDescr_Type but NOT on
// PyCFunction_Type. A module-level builtin (e.g. select.select)
// stored as a class attribute therefore does not pick up self when
// accessed through an instance: `self._select(...)` must pass the
// same number of arguments as `select.select(...)`.
//
// CPython: Include/object.h Py_TPFLAGS_METHOD_DESCRIPTOR
// CPython: Objects/funcobject.c:1234 PyFunction_Type tp_flags
// CPython: Objects/descrobject.c:1480 PyMethodDescr_Type tp_flags
// CPython: Objects/methodobject.c:357 PyCFunction_Type tp_flags (no METHOD_DESCRIPTOR)
func isMethodLike(descr Object) bool {
	switch descr.(type) {
	case *Function, *MethodDescr:
		return true
	}
	return false
}

// bindDescriptor invokes DescrGet when the type supports it,
// otherwise returns the descriptor as-is. Used by the protocol-level
// helpers that need to call dunder methods without going through the
// full attribute machinery.
//
// CPython: Objects/typeobject.c:1976 _PyObject_LookupSpecial
func bindDescriptor(descr Object, instance Object) (Object, error) {
	tp := descr.Type()
	if tp.DescrGet != nil {
		return tp.DescrGet(descr, instance, instance.Type())
	}
	return descr, nil
}

// isAttributeError reports whether err is an AttributeError. gopy
// represents Python exceptions as Go errors whose Error() string
// carries the exception name prefix, so prefix matching is the
// portable check.
func isAttributeError(err error) bool {
	if err == nil {
		return false
	}
	return strings.HasPrefix(err.Error(), "AttributeError")
}
