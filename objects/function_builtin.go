package objects

// BuiltinFunction wraps a Go function so the VM can call it through
// the type's Vectorcall / Call slots. The Fn closure shape mirrors
// METH_VARARGS|METH_KEYWORDS (positional slice plus a kwargs map),
// which is the convention CPython's cfunction_call dispatches when
// vectorcall is unavailable.
//
// Conv is the METH_* calling-convention tag. The CALL specializer
// reads it to pick CALL_BUILTIN_O / CALL_BUILTIN_FAST /
// CALL_BUILTIN_FAST_WITH_KEYWORDS / CALL_LEN / CALL_ISINSTANCE. Conv
// defaults to MethVarargs|MethKeywords so legacy registration sites
// keep matching the closure shape they always passed.
//
// CPython: Include/cpython/methodobject.h PyCFunctionObject
type BuiltinFunction struct {
	Header
	Name string
	Conv MethFlag
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

	// meth_getsets: __doc__, __name__, __qualname__, __self__, __module__
	// CPython: Objects/methodobject.c:286 meth_getsets
	SetTypeDescr(BuiltinFunctionType, "__doc__", NewGetSetDescr("__doc__",
		func(o Object) (Object, error) { return None(), nil },
		nil,
	))
	SetTypeDescr(BuiltinFunctionType, "__name__", NewGetSetDescr("__name__",
		func(o Object) (Object, error) {
			if bf, ok := o.(*BuiltinFunction); ok {
				return NewStr(bf.Name), nil
			}
			return None(), nil
		},
		nil,
	))
	SetTypeDescr(BuiltinFunctionType, "__qualname__", NewGetSetDescr("__qualname__",
		func(o Object) (Object, error) {
			if bf, ok := o.(*BuiltinFunction); ok {
				return NewStr(bf.Name), nil
			}
			return None(), nil
		},
		nil,
	))
	SetTypeDescr(BuiltinFunctionType, "__self__", NewGetSetDescr("__self__",
		func(o Object) (Object, error) { return None(), nil },
		nil,
	))
	SetTypeDescr(BuiltinFunctionType, "__module__", NewGetSetDescr("__module__",
		func(o Object) (Object, error) { return NewStr(""), nil },
		nil,
	))
}

// NewBuiltinFunction wraps fn under name. Conv defaults to
// MethVarargs|MethKeywords so legacy registration sites match the
// closure shape they always passed.
func NewBuiltinFunction(name string, fn func(args []Object, kwargs map[string]Object) (Object, error)) *BuiltinFunction {
	bf := &BuiltinFunction{Name: name, Conv: MethVarargs | MethKeywords, Fn: fn}
	bf.init(BuiltinFunctionType)
	return bf
}

// NewBuiltinFunctionConv wraps fn with an explicit METH_* tag. The
// specializer reads bf.Conv to pick CALL_BUILTIN_O /
// CALL_BUILTIN_FAST / CALL_BUILTIN_FAST_WITH_KEYWORDS / CALL_LEN /
// CALL_ISINSTANCE, matching specialize_c_call's switch on
// PyCFunction_GET_FLAGS.
//
// CPython: Python/specialize.c:2137 specialize_c_call
func NewBuiltinFunctionConv(name string, conv MethFlag, fn func(args []Object, kwargs map[string]Object) (Object, error)) *BuiltinFunction {
	bf := &BuiltinFunction{Name: name, Conv: conv, Fn: fn}
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
