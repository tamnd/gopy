// Python-visible attribute surface for code objects. Mirrors the
// getsetlist + memberlist + methodlist tables in
// Objects/codeobject.c so dis, traceback, inspect, and user code can
// read co_code, co_consts, co_names, co_varnames, co_freevars,
// co_cellvars, co_linetable, co_exceptiontable, co_nlocals, plus the
// co_lines / co_positions / _varname_from_oparg method trio.
//
// CPython: Objects/codeobject.c:2724 code_memberlist
// CPython: Objects/codeobject.c:2791 code_getsetlist
// CPython: Objects/codeobject.c:2967 code_methods

package objects

import (
	"fmt"
	"math/big"
)

// DeprecWarnHook is set by module/_warnings at init time so the objects
// package can emit DeprecationWarning without a circular import. The
// function receives the warning message and returns non-nil if the filter
// elevates the warning to an error.
//
// CPython: Objects/codeobject.c:2744 code_getlnotab (PyErr_WarnEx call)
var DeprecWarnHook func(msg string) error

// RuntimeWarnHook is set by module/_warnings at init time so the objects
// package can emit a RuntimeWarning (for example the non-string class
// __dict__ key warning) without a circular import. Returns non-nil if
// the filter elevates the warning to an error.
//
// CPython: Objects/typeobject.c:4667 type_new (PyErr_WarnFormat RuntimeWarning)
var RuntimeWarnHook func(msg string) error

func init() {
	SetTypeDescr(CodeType, "co_lines", NewMethodDescr(CodeType, "co_lines", codeCoLinesMethod))
	SetTypeDescr(CodeType, "co_positions", NewMethodDescr(CodeType, "co_positions", codeCoPositionsMethod))
	SetTypeDescr(CodeType, "_varname_from_oparg", NewMethodDescr(CodeType, "_varname_from_oparg", codeVarnameFromOpargMethod))
}

// codeAttrLookup answers the read-only co_* attribute surface that the
// getsetlist + memberlist tables in codeobject.c expose. Returns nil
// when name is not one of the co_* attributes, so the caller can fall
// through to GenericGetAttr.
func codeAttrLookup(c *Code, name string) (Object, bool) {
	switch name {
	case "co_code":
		return NewBytes(c.Code), true
	case "_co_code_adaptive":
		// CPython: Objects/codeobject.c:2777 code_getcodeadaptive
		return NewBytes(c.Code), true
	case "co_consts":
		return constsAsTuple(c.Consts), true
	case "co_names":
		return stringsAsTuple(c.Names), true
	case "co_varnames":
		return stringsAsTuple(c.Varnames), true
	case "co_freevars":
		return stringsAsTuple(c.Freevars), true
	case "co_cellvars":
		return stringsAsTuple(c.Cellvars), true
	case "co_nlocals":
		// CPython: Include/cpython/code.h:87 co_nlocals
		if c.Nlocalsplus != 0 {
			return NewInt(int64(c.Nlocals)), true
		}
		return NewInt(int64(len(c.Varnames))), true
	case "co_nlocalsplus":
		// CPython: Include/cpython/code.h:84 co_nlocalsplus
		return NewInt(int64(c.Nlocalsplus)), true
	case "co_ncellvars":
		// CPython: Include/cpython/code.h:88 co_ncellvars
		if c.Nlocalsplus != 0 {
			return NewInt(int64(c.Ncellvars)), true
		}
		return NewInt(int64(len(c.Cellvars))), true
	case "co_nfreevars":
		// CPython: Include/cpython/code.h:89 co_nfreevars
		if c.Nlocalsplus != 0 {
			return NewInt(int64(c.Nfreevars)), true
		}
		return NewInt(int64(len(c.Freevars))), true
	case "co_localsplusnames":
		// CPython: Include/cpython/code.h:92 co_localsplusnames
		return stringsAsTuple(c.LocalsplusNames), true
	case "co_linetable":
		return NewBytes(c.Linetable), true
	case "co_lnotab":
		// Deprecated since Python 3.12; emit DeprecationWarning.
		// CPython: Objects/codeobject.c:2744 code_getlnotab
		if DeprecWarnHook != nil {
			_ = DeprecWarnHook("co_lnotab is deprecated. Use co_lines instead.")
		}
		return NewBytes(c.Linetable), true
	case "co_exceptiontable":
		return NewBytes(c.ExceptionTable), true
	}
	return nil, false
}

// constsAsTuple converts Code.Consts ([]any) into a Python tuple. The
// raw storage is the same Go-typed scalar set the marshal layer emits,
// so the conversion here is the inverse of marshal's r_object scalar
// recovery.
func constsAsTuple(consts []any) *Tuple {
	items := make([]Object, len(consts))
	for i, v := range consts {
		items[i] = wrapConstAttr(v)
	}
	return NewTuple(items)
}

// stringsAsTuple converts []string into a tuple of Python strs.
func stringsAsTuple(ss []string) *Tuple {
	items := make([]Object, len(ss))
	for i, s := range ss {
		items[i] = NewStr(s)
	}
	return NewTuple(items)
}

// WrapConstStr lifts a string constant into a *Unicode, interning it
// when it qualifies. CPython interns identifier-like string constants
// when the code object is built (intern_constants, recursing into
// tuples and frozensets); gopy materializes consts lazily, so every
// path that turns a const string into an Object routes through here so
// the interning is uniform whether the const is read via co_consts or
// loaded by the VM.
//
// CPython: Objects/codeobject.c:203 intern_constants
func WrapConstStr(s string) Object {
	if shouldInternString(s) {
		return InternFromString(s)
	}
	return NewStr(s)
}

// shouldInternString reports whether a string constant qualifies for
// interning: ASCII and made up only of [a-zA-Z0-9_]. This is the
// default-build (non free-threaded) predicate; the free-threaded build
// interns every string constant.
//
// CPython: Objects/codeobject.c:117 should_intern_string
func shouldInternString(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 0x80 {
			return false
		}
		isalnum := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		if !isalnum && c != '_' {
			return false
		}
	}
	return true
}

// wrapConstAttr lifts one raw constant into an Object. Mirrors
// vm.wrapConst for the scalar types marshal recovers, but lives in
// objects/ so codeGetAttr can use it without an import cycle. Compile
// pipeline value types (*compile.Code, *compile.ConstTuple) reach
// users through LOAD_CONST, not through co_consts inspection, so this
// helper does not need to know about them; an Object passthrough
// covers the case where the compiler stored an already-wrapped value.
func wrapConstAttr(v any) Object {
	switch x := v.(type) {
	case nil:
		return None()
	case bool:
		return NewBool(x)
	case int:
		return NewInt(int64(x))
	case int64:
		return NewInt(x)
	case *big.Int:
		return NewIntFromBig(x)
	case float64:
		return NewFloat(x)
	case complex128:
		return NewComplex(real(x), imag(x))
	case string:
		return WrapConstStr(x)
	case []byte:
		return NewBytes(x)
	case *Code:
		return x
	case []any:
		// Lifted tuple constants (see vm.liftConsts) arrive here when
		// dis.py reads co_consts on a code whose nested tuples were
		// already flattened for marshal.
		items := make([]Object, len(x))
		for i, raw := range x {
			items[i] = wrapConstAttr(raw)
		}
		return NewTuple(items)
	case Object:
		return x
	}
	if ConstWrapHook != nil {
		if o, ok := ConstWrapHook(v); ok {
			return o
		}
	}
	return NewStr(fmt.Sprintf("%v", v))
}

// codeCoLinesMethod backs code.co_lines(). Returns an iterator of
// (start, end, line) tuples; dis.py drives it with next().
//
// CPython: Objects/codeobject.c:2817 code_linesiterator
func codeCoLinesMethod(args []Object, kwargs map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: co_lines() takes no arguments (%d given)", len(args)-1)
	}
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: co_lines() takes no keyword arguments")
	}
	c := args[0].(*Code)
	entries := CoLines(c)
	items := make([]Object, len(entries))
	for i, e := range entries {
		line := None()
		if e.Line >= 0 {
			line = NewInt(int64(e.Line))
		}
		items[i] = NewTuple([]Object{
			NewInt(int64(e.Start)),
			NewInt(int64(e.End)),
			line,
		})
	}
	return listIter(NewList(items))
}

// codeCoPositionsMethod backs code.co_positions(). Returns an iterator
// of (line, end_line, col, end_col) tuples, one tuple per codeunit
// (2 bytes), not per coalesced linetable run. dis.py and other
// consumers call next() once per instruction plus once per inline cache
// slot, so a coalesced run that covers N codeunits must yield N copies.
//
// CPython: Objects/codeobject.c:1492 positionsiter_next advances
// pi_offset by 2 (one codeunit) on every call and only re-decodes the
// linetable when the cursor reaches the end of the current range.
//
// CPython: Objects/codeobject.c:1554 code_positionsiterator
func codeCoPositionsMethod(args []Object, kwargs map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: co_positions() takes no arguments (%d given)", len(args)-1)
	}
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: co_positions() takes no keyword arguments")
	}
	c := args[0].(*Code)
	entries := CoPositions(c)
	posField := func(v int) Object {
		if v < 0 {
			return None()
		}
		return NewInt(int64(v))
	}
	var items []Object
	for _, p := range entries {
		tup := NewTuple([]Object{
			posField(p.Line),
			posField(p.EndLine),
			posField(p.Column),
			posField(p.EndColumn),
		})
		units := max((p.End-p.Start)/2, 1)
		for range units {
			items = append(items, tup)
		}
	}
	return listIter(NewList(items))
}

// codeVarnameFromOpargMethod returns the local-variable name for the
// given oparg. The CPython implementation indexes
// co_localsplusnames; gopy stores varnames + cellvars + freevars
// separately, so we concatenate to match the locals-plus layout.
//
// CPython: Objects/codeobject.c:2955 code__varname_from_oparg_impl
func codeVarnameFromOpargMethod(args []Object, kwargs map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: _varname_from_oparg() takes exactly one argument (%d given)", len(args)-1)
	}
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: _varname_from_oparg() takes no keyword arguments")
	}
	c := args[0].(*Code)
	idxObj, ok := args[1].(*Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: _varname_from_oparg() argument must be int, not %s", args[1].Type().Name)
	}
	i64, _ := idxObj.Int64()
	idx := int(i64)
	if idx < 0 {
		return nil, fmt.Errorf("IndexError: _varname_from_oparg(): oparg out of range")
	}
	if idx < len(c.Varnames) {
		return NewStr(c.Varnames[idx]), nil
	}
	idx -= len(c.Varnames)
	if idx < len(c.Cellvars) {
		return NewStr(c.Cellvars[idx]), nil
	}
	idx -= len(c.Cellvars)
	if idx < len(c.Freevars) {
		return NewStr(c.Freevars[idx]), nil
	}
	return nil, fmt.Errorf("IndexError: _varname_from_oparg(): oparg out of range")
}
