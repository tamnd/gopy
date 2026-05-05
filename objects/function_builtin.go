package objects

import "errors"

// BuiltinFunction wraps a Go function so the VM can call it through the
// Type.Call slot. v0.6 only needs the positional-args fast path; kwargs
// and the bound-method shortcut wait on spec 1684.
//
// CPython: Include/cpython/methodobject.h PyCFunctionObject
type BuiltinFunction struct {
	Header
	Name string
	Fn   func(args []Object, kwargs map[string]Object) (Object, error)
}

// BuiltinFunctionType is the type singleton for built-in functions.
//
// CPython: Objects/methodobject.c PyCFunction_Type
var BuiltinFunctionType = NewType("builtin_function_or_method", []*Type{objectType})

func init() {
	BuiltinFunctionType.Repr = builtinFunctionRepr
	BuiltinFunctionType.Str = builtinFunctionRepr
	BuiltinFunctionType.Call = func(o Object, args []Object, kwargs map[string]Object) (Object, error) {
		bf := o.(*BuiltinFunction)
		return bf.Fn(args, kwargs)
	}
}

// NewBuiltinFunction wraps fn under name.
func NewBuiltinFunction(name string, fn func(args []Object, kwargs map[string]Object) (Object, error)) *BuiltinFunction {
	bf := &BuiltinFunction{Name: name, Fn: fn}
	bf.init(BuiltinFunctionType)
	return bf
}

func builtinFunctionRepr(o Object) (string, error) {
	bf := o.(*BuiltinFunction)
	return "<built-in function " + bf.Name + ">", nil
}

// Call routes through callable.Type().Call. Returns an error if the
// type does not implement tp_call.
//
// CPython: Objects/call.c PyObject_Call
func Call(callable Object, args []Object, kwargs map[string]Object) (Object, error) {
	t := callable.Type()
	if t.Call == nil {
		return nil, errors.New("TypeError: '" + t.Name + "' object is not callable")
	}
	return t.Call(callable, args, kwargs)
}
