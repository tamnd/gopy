// Mapping protocol entry points. PyMapping_* functions hand off to
// the type's MappingMethods slot bundle; Keys/Values/Items reach into
// dict directly (the PyDict_CheckExact branch in CPython) and fall
// through to a `o.<name>()` method call otherwise.
//
// CPython: Objects/abstract.c:2260 PyMapping_Check

package objects

import (
	"errors"
	"fmt"
)

// MappingCheck reports whether o implements the mapping protocol.
// Mirrors the test CPython performs: mp_subscript must be wired.
//
// CPython: Objects/abstract.c:2260 PyMapping_Check
func MappingCheck(o Object) bool {
	if o == nil {
		return false
	}
	mp := o.Type().Mapping
	return mp != nil && mp.GetItem != nil
}

// MappingSize returns len(o) when o implements the mapping protocol.
// Falls through to a TypeError when o looks like a sequence instead,
// matching the message PySequence_Size uses to disambiguate.
//
// CPython: Objects/abstract.c:2267 PyMapping_Size
func MappingSize(o Object) (int, error) {
	if o == nil {
		return 0, errors.New("TypeError: NoneType has no len()")
	}
	tp := o.Type()
	if mp := tp.Mapping; mp != nil && mp.Length != nil {
		return mp.Length(o)
	}
	if sq := tp.Sequence; sq != nil && sq.Length != nil {
		return 0, fmt.Errorf("TypeError: %s is not a mapping", tp.Name)
	}
	return 0, fmt.Errorf("TypeError: object of type '%s' has no len()", tp.Name)
}

// MappingLength is the historical alias for MappingSize.
//
// CPython: Objects/abstract.c:2292 PyMapping_Length
func MappingLength(o Object) (int, error) {
	return MappingSize(o)
}

// MappingGetItemString returns o[key] where key is a Go string.
// Mirrors PyMapping_GetItemString: builds a transient str and routes
// through PyObject_GetItem.
//
// CPython: Objects/abstract.c:2299 PyMapping_GetItemString
func MappingGetItemString(o Object, key string) (Object, error) {
	return GetItem(o, NewStr(key))
}

// MappingSetItemString assigns o[key] = value where key is a Go
// string. value==nil signals delete (matches CPython routing
// through PyObject_SetItem with NULL).
//
// CPython: Objects/abstract.c:2334 PyMapping_SetItemString
func MappingSetItemString(o Object, key string, value Object) error {
	return SetItem(o, NewStr(key), value)
}

// MappingDelItemString removes o[key].
//
// CPython: Objects/abstract.c PyMapping_DelItemString (forwards to PyObject_DelItem)
func MappingDelItemString(o Object, key string) error {
	return DelItem(o, NewStr(key))
}

// MappingDelItem removes o[key].
//
// CPython: Objects/abstract.c PyMapping_DelItem (forwards to PyObject_DelItem)
func MappingDelItem(o, key Object) error {
	return DelItem(o, key)
}

// MappingGetOptionalItem returns (value, true, nil) when o[key]
// exists, (nil, false, nil) when the lookup raises KeyError, and
// (nil, false, err) for any other error. Lets callers distinguish
// "absent" from "broken" without burning an exception. Matches
// PyMapping_GetOptionalItem's int return: 1, 0, -1.
//
// CPython: Objects/abstract.c:207 PyMapping_GetOptionalItem
func MappingGetOptionalItem(o, key Object) (Object, bool, error) {
	if d, ok := o.(*Dict); ok {
		v, err := d.GetItem(key)
		if err != nil {
			if errors.Is(err, errKeyNotFound) {
				return nil, false, nil
			}
			return nil, false, err
		}
		return v, true, nil
	}
	v, err := GetItem(o, key)
	if err != nil {
		if isKeyError(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return v, true, nil
}

// MappingGetOptionalItemString is the string-key form of
// MappingGetOptionalItem.
//
// CPython: Objects/abstract.c:2316 PyMapping_GetOptionalItemString
func MappingGetOptionalItemString(o Object, key string) (Object, bool, error) {
	return MappingGetOptionalItem(o, NewStr(key))
}

// MappingHasKeyWithError reports whether key is in o, surfacing any
// non-KeyError as an actual error.
//
// CPython: Objects/abstract.c:2362 PyMapping_HasKeyWithError
func MappingHasKeyWithError(o, key Object) (bool, error) {
	_, ok, err := MappingGetOptionalItem(o, key)
	return ok, err
}

// MappingHasKeyStringWithError is the string-key form.
//
// CPython: Objects/abstract.c:2353 PyMapping_HasKeyStringWithError
func MappingHasKeyStringWithError(o Object, key string) (bool, error) {
	return MappingHasKeyWithError(o, NewStr(key))
}

// MappingHasKey reports whether key is in o, swallowing any error
// (CPython logs the unraisable; gopy returns false). Use the
// WithError variant when callers need to surface failures.
//
// CPython: Objects/abstract.c:2396 PyMapping_HasKey
func MappingHasKey(o, key Object) bool {
	ok, err := MappingHasKeyWithError(o, key)
	if err != nil {
		return false
	}
	return ok
}

// MappingHasKeyString is the string-key form.
//
// CPython: Objects/abstract.c:2371 PyMapping_HasKeyString
func MappingHasKeyString(o Object, key string) bool {
	return MappingHasKey(o, NewStr(key))
}

// MappingKeys returns list(o.keys()). Dict gets a fast path that
// reads its entry table directly; everything else dispatches through
// a `o.keys()` method call and materializes the result via
// SequenceList.
//
// CPython: Objects/abstract.c:2453 PyMapping_Keys
func MappingKeys(o Object) (*List, error) {
	if d, ok := o.(*Dict); ok {
		return NewList(d.Keys()), nil
	}
	return mappingMethodOutputAsList(o, "keys")
}

// MappingValues returns list(o.values()).
//
// CPython: Objects/abstract.c:2477 PyMapping_Values
func MappingValues(o Object) (*List, error) {
	if d, ok := o.(*Dict); ok {
		out := make([]Object, 0, d.Len())
		for _, slot := range d.order {
			if d.slotIsLive(slot) {
				out = append(out, d.slotValue(slot))
			}
		}
		return NewList(out), nil
	}
	return mappingMethodOutputAsList(o, "values")
}

// MappingItems returns list(o.items()) as a list of (key, value)
// 2-tuples. Dict uses the fast path; otherwise dispatches through
// `o.items()`.
//
// CPython: Objects/abstract.c:2465 PyMapping_Items
func MappingItems(o Object) (*List, error) {
	if d, ok := o.(*Dict); ok {
		out := make([]Object, 0, d.Len())
		for _, slot := range d.order {
			if d.slotIsLive(slot) {
				out = append(out, NewTuple([]Object{d.slotKey(slot), d.slotValue(slot)}))
			}
		}
		return NewList(out), nil
	}
	return mappingMethodOutputAsList(o, "items")
}

// mappingMethodOutputAsList calls o.<method>() and returns the result
// as a fresh list. Mirrors the static helper method_output_as_list:
// list-exact results are returned unchanged, anything else is run
// through PySequence_List.
//
// CPython: Objects/abstract.c:2424 method_output_as_list
func mappingMethodOutputAsList(o Object, method string) (*List, error) {
	fn, err := GetAttr(o, NewStr(method))
	if err != nil {
		return nil, err
	}
	out, err := CallNoArgs(fn)
	if err != nil {
		return nil, err
	}
	if l, ok := out.(*List); ok {
		return l, nil
	}
	return SequenceList(out)
}

// isKeyError reports whether err is a KeyError-shaped failure. gopy
// signals dict misses with errKeyNotFound and the user-facing layer
// raises strings prefixed with "KeyError"; cover both.
func isKeyError(err error) bool {
	if errors.Is(err, errKeyNotFound) {
		return true
	}
	if err == nil {
		return false
	}
	s := err.Error()
	if len(s) >= 8 && s[:8] == "KeyError" {
		return true
	}
	return false
}
