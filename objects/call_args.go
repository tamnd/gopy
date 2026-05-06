// Argument-shape helpers for the call path. The vectorcall family in
// call.go expects (args, kwnames) layouts; CALL_FUNCTION_EX hands in
// an arbitrary iterable for *args and an arbitrary mapping for
// **kwargs. The helpers here bridge the two and emit the canonical
// TypeError messages the eval loop and PyArg-style binders raise.
//
// CPython: Objects/call.c (PyStack helpers) + Python/ceval.c
// (missing_arguments / too_many_positional / unexpected-keyword).

package objects

import (
	"errors"
	"fmt"
	"strings"
)

// UnpackKwargs unpacks a *Dict of keyword arguments into the
// (kwnames, values) layout the vectorcall slot expects: kwnames is a
// tuple of name objects and values lines up positionally with it. All
// keys must be string objects; otherwise TypeError "keywords must be
// strings" is returned.
//
// CPython: Objects/call.c:1038 _PyStack_UnpackDict
func UnpackKwargs(kwargs *Dict) (*Tuple, []Object, error) {
	if kwargs == nil || kwargs.Len() == 0 {
		return nil, nil, nil
	}
	keys := kwargs.Keys()
	values := make([]Object, len(keys))
	for i, k := range keys {
		if !isString(k) {
			return nil, nil, errors.New("TypeError: keywords must be strings")
		}
		v, err := kwargs.GetItem(k)
		if err != nil {
			return nil, nil, err
		}
		values[i] = v
	}
	return NewTuple(keys), values, nil
}

// CoerceStarArgs takes the iterable feeding a `f(*args)` call and
// produces a real *Tuple. Tuples pass through. Other iterables get
// drained into a fresh tuple via Iter/IterNext, mirroring the
// PySequence_Tuple coercion CPython performs in CALL_FUNCTION_EX.
//
// CPython: Python/bytecodes.c CALL_FUNCTION_EX (forces tuple via
// _PySequence_Tuple before dispatch).
func CoerceStarArgs(o Object) (*Tuple, error) {
	if o == nil {
		return NewTuple(nil), nil
	}
	if t, ok := o.(*Tuple); ok {
		return t, nil
	}
	items, err := iterToSliceObj(o)
	if err != nil {
		return nil, fmt.Errorf("TypeError: argument after * must be an iterable, not %s", o.Type().Name)
	}
	return NewTuple(items), nil
}

// CoerceStarKwargs takes the mapping feeding `f(**kwargs)` and
// produces a *Dict. Dicts pass through. Other mappings get copied via
// Keys/GetItem; non-string keys raise the canonical TypeError.
//
// CPython: Python/bytecodes.c CALL_FUNCTION_EX (PyDict_Update path
// when kwargs is not exactly a dict).
func CoerceStarKwargs(o Object) (*Dict, error) {
	if o == nil {
		return nil, nil
	}
	if d, ok := o.(*Dict); ok {
		return d, nil
	}
	tp := o.Type()
	if tp.Mapping == nil || tp.Mapping.GetItem == nil {
		return nil, fmt.Errorf("TypeError: argument after ** must be a mapping, not %s", tp.Name)
	}
	out := NewDict()
	keysObj, err := MappingKeys(o)
	if err != nil {
		return nil, err
	}
	keys, err := iterToSliceObj(keysObj)
	if err != nil {
		return nil, err
	}
	for _, k := range keys {
		if !isString(k) {
			return nil, errors.New("TypeError: keywords must be strings")
		}
		v, err := tp.Mapping.GetItem(o, k)
		if err != nil {
			return nil, err
		}
		if err := out.SetItem(k, v); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// MissingArgumentsError formats the TypeError raised when one or more
// required parameters didn't get bound. kind is "positional" or
// "keyword-only"; names are the unbound parameter names in declared
// order.
//
// CPython: Python/ceval.c:1448 missing_arguments / format_missing
func MissingArgumentsError(qualname, kind string, names []string) error {
	if len(names) == 0 {
		return nil
	}
	plural := ""
	if len(names) != 1 {
		plural = "s"
	}
	return fmt.Errorf("TypeError: %s() missing %d required %s argument%s: %s",
		qualname, len(names), kind, plural, joinQuotedNames(names))
}

// TooManyPositionalError formats the TypeError raised when *args
// supplies more positional values than the signature accepts (and the
// function has no *args bucket). atLeast equals max minus the number
// of positional defaults; pass max twice for the no-default case.
// kwonlyGiven counts already-bound keyword-only parameters and
// triggers the parenthetical "(and N keyword-only argument)" tail.
//
// CPython: Python/ceval.c:1487 too_many_positional
func TooManyPositionalError(qualname string, given, atLeast, max, kwonlyGiven int) error {
	var sig string
	plural := ""
	if atLeast != max {
		sig = fmt.Sprintf("from %d to %d", atLeast, max)
		plural = "s"
	} else {
		sig = fmt.Sprintf("%d", max)
		if max != 1 {
			plural = "s"
		}
	}
	verb := "were"
	if given == 1 && kwonlyGiven == 0 {
		verb = "was"
	}
	return fmt.Errorf("TypeError: %s() takes %s positional argument%s but %d%s %s given",
		qualname, sig, plural, given, tailKwonly(kwonlyGiven), verb)
}

// UnexpectedKeywordError is the TypeError raised when a caller passes
// a keyword the signature doesn't declare (and there's no **kwargs).
//
// CPython: Python/ceval.c (kwonly mismatch branch) + Objects/call.c
func UnexpectedKeywordError(qualname, name string) error {
	return fmt.Errorf("TypeError: %s() got an unexpected keyword argument '%s'", qualname, name)
}

// MultipleValuesForArgumentError is the TypeError raised when the
// same parameter is bound twice, once positionally and once by
// keyword.
//
// CPython: Python/ceval.c (positional-already-set branch)
func MultipleValuesForArgumentError(qualname, name string) error {
	return fmt.Errorf("TypeError: %s() got multiple values for argument '%s'", qualname, name)
}

// PositionalOnlyAsKeywordError is the TypeError raised when caller
// keywords collide with positional-only parameter names.
//
// CPython: Python/ceval.c:1546 positional_only_passed_as_keyword
func PositionalOnlyAsKeywordError(qualname string, names []string) error {
	if len(names) == 0 {
		return nil
	}
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = "'" + n + "'"
	}
	return fmt.Errorf("TypeError: %s() got some positional-only arguments passed as keyword arguments: %s",
		qualname, strings.Join(quoted, ", "))
}

// joinQuotedNames stitches a list of identifier strings into the
// natural-language phrasing CPython uses in TypeError messages: a
// single name stands alone, two are joined by " and ", three or more
// take a final ", and " before the last item.
//
// CPython: Python/ceval.c:1385 format_missing (the switch on len)
func joinQuotedNames(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = "'" + n + "'"
	}
	switch len(quoted) {
	case 1:
		return quoted[0]
	case 2:
		return quoted[0] + " and " + quoted[1]
	default:
		head := strings.Join(quoted[:len(quoted)-1], ", ")
		return head + ", and " + quoted[len(quoted)-1]
	}
}

// tailKwonly returns the empty string when kwonlyGiven==0; otherwise
// the parenthetical "(and N keyword-only argumentS)" that
// too_many_positional appends. Lifted out so the percent-substitution
// in TooManyPositionalError stays readable.
func tailKwonly(kwonlyGiven int) string {
	if kwonlyGiven <= 0 {
		return ""
	}
	plural := ""
	if kwonlyGiven != 1 {
		plural = "s"
	}
	return fmt.Sprintf(" (and %d keyword-only argument%s)", kwonlyGiven, plural)
}

// iterToSliceObj is the objects-package twin of vm.iterToSlice. The
// vm helper isn't exported, and call_args.go can't import vm without
// a cycle, so the same drain-the-iterator routine lives here.
func iterToSliceObj(o Object) ([]Object, error) {
	if o == nil {
		return nil, errors.New("TypeError: cannot iterate nil")
	}
	switch v := o.(type) {
	case *Tuple:
		out := make([]Object, v.Len())
		for i := range out {
			out[i] = v.Item(i)
		}
		return out, nil
	case *List:
		out := make([]Object, v.Len())
		for i := range out {
			out[i] = v.Item(i)
		}
		return out, nil
	}
	it, err := Iter(o)
	if err != nil {
		return nil, err
	}
	var out []Object
	for {
		v, err := IterNext(it)
		if errors.Is(err, ErrStopIteration) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
}

// isString returns true when o is a Python str. Used by the kwarg
// validators in this file to enforce "keywords must be strings".
func isString(o Object) bool {
	switch o.(type) {
	case *strStub:
		return true
	}
	return o.Type() != nil && o.Type().Name == "str"
}
