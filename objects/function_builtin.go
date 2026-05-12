package objects

// BuiltinFunction wraps a Go function so the VM can call it through
// the type's Vectorcall / Call slots. The Fn closure shape mirrors
// METH_VARARGS|METH_KEYWORDS (positional slice plus a kwargs map),
// which is the convention CPython's cfunction_call dispatches when
// vectorcall is unavailable.
//
// CPython: Include/cpython/methodobject.h PyCFunctionObject
type BuiltinFunction struct {
	Header
	Name string
	Fn   func(args []Object, kwargs map[string]Object) (Object, error)
}

// BuiltinFunctionType is the type singleton for built-in functions.
//
// CPython: Objects/methodobject.c:357 PyCFunction_Type
var BuiltinFunctionType = NewType("builtin_function_or_method", []*Type{objectType})

func init() {
	BuiltinFunctionType.Repr = builtinFunctionRepr
	BuiltinFunctionType.Str = builtinFunctionRepr
	BuiltinFunctionType.Call = builtinFunctionCall
	BuiltinFunctionType.Vectorcall = builtinFunctionVectorcall
	// Identity hash so builtin functions are usable as set/dict keys.
	// CPython inherits tp_hash from object for cfunction objects.
	//
	// CPython: Objects/methodobject.c:357 PyCFunction_Type (tp_hash slot
	// inherited from object_hash)
	BuiltinFunctionType.Hash = identityHash
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

// builtinFunctionCall is the tp_call slot. It mirrors cfunction_call
// for the METH_VARARGS|METH_KEYWORDS case, since gopy's BuiltinFunction
// always carries a (slice, map) closure.
//
// CPython: Objects/methodobject.c:544 cfunction_call
func builtinFunctionCall(o Object, args []Object, kwargs map[string]Object) (Object, error) {
	bf := o.(*BuiltinFunction)
	return bf.Fn(args, kwargs)
}

// builtinFunctionVectorcall is the Vectorcall slot for built-in
// functions. It unpacks args[]+kwnames into the (positional, kwargs)
// shape bf.Fn expects, the same way cfunction_vectorcall_FASTCALL_KEYWORDS
// hands its operands to a METH_FASTCALL|METH_KEYWORDS C function.
//
// CPython: Objects/methodobject.c:454 cfunction_vectorcall_FASTCALL_KEYWORDS
func builtinFunctionVectorcall(callable Object, args []Object, nargsf uint, kwnames *Tuple) (Object, error) {
	bf := callable.(*BuiltinFunction)
	nargs := VectorcallNargs(nargsf)
	pos := make([]Object, nargs)
	if nargs > 0 {
		copy(pos, args[:nargs])
	}
	var kwargs map[string]Object
	if kwnames != nil && kwnames.Len() > 0 {
		nkw := kwnames.Len()
		kwargs = make(map[string]Object, nkw)
		for i := 0; i < nkw; i++ {
			name, err := Str(kwnames.Item(i))
			if err != nil {
				return nil, err
			}
			kwargs[name] = args[nargs+i]
		}
	}
	return bf.Fn(pos, kwargs)
}
