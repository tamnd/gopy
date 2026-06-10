package objects

import "fmt"

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
// Module is the name string of the owning module (m_module). CPython
// stamps this at PyCFunction_NewEx time and PyModule_AddFunctions
// passes the parent module's __name__ here, so pickle's whichmodule
// can resolve codecs.encode to "_codecs" without scanning sys.modules.
//
// CPython: Include/cpython/methodobject.h PyCFunctionObject
type BuiltinFunction struct {
	Header
	Name   string
	Module string
	Conv   MethFlag
	// Doc is the optional docstring (ml_doc in PyMethodDef). When non-empty,
	// __doc__ returns this string; when empty __doc__ returns None, matching
	// CPython's meth_get__doc__ falling back to NULL ml_doc -> Py_None.
	//
	// CPython: Objects/methodobject.c:286 meth_getsets (__doc__ getset)
	Doc string
	// TextSignature is the Argument Clinic '__text_signature__' string.
	// When non-empty, inspect.Signature.from_callable reads it to construct
	// a Signature object. The '$' prefix on a parameter marks the implicit
	// self/module argument that inspect strips when skip_bound_arg=True.
	//
	// CPython: Objects/methodobject.c ml_doc first-line protocol
	TextSignature string
	// Self mirrors PyCFunctionObject.m_self. For a static method bound
	// to a type, type_add_method stores the owning type here via
	// PyCFunction_NewEx(meth, (PyObject*)type, NULL); meth_get__qualname__
	// reads it directly to yield e.g. 'str.maketrans'. The METH_STATIC
	// flag still masks it from __self__, which stays None.
	//
	// CPython: Objects/typeobject.c:8026 type_add_method (METH_STATIC)
	Self Object
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
	AddCallSlotWrapper(BuiltinFunctionType)
	// Identity hash so builtin functions are usable as set/dict keys.
	// CPython inherits tp_hash from object for cfunction objects.
	//
	// CPython: Objects/methodobject.c:357 PyCFunction_Type (tp_hash slot
	// inherited from object_hash)
	BuiltinFunctionType.Hash = identityHash

	// meth_getsets: __doc__, __name__, __qualname__, __self__, __module__
	// CPython: Objects/methodobject.c:286 meth_getsets
	SetTypeDescr(BuiltinFunctionType, "__doc__", NewGetSetDescr("__doc__",
		func(o Object) (Object, error) {
			if bf, ok := o.(*BuiltinFunction); ok && bf.Doc != "" {
				return NewStr(bf.Doc), nil
			}
			return None(), nil
		},
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
		builtinFunctionQualname,
		nil,
	))
	SetTypeDescr(BuiltinFunctionType, "__self__", NewGetSetDescr("__self__",
		func(o Object) (Object, error) { return None(), nil },
		nil,
	))
	// __module__ returns the owning module's name (m_module) when one
	// was stamped at registration, else None. Pickle's whichmodule
	// reads this first; returning None makes it fall back to scanning
	// sys.modules for an attribute that matches.
	//
	// CPython: Objects/methodobject.c:233 meth_get__module__
	SetTypeDescr(BuiltinFunctionType, "__module__", NewGetSetDescr("__module__",
		func(o Object) (Object, error) {
			if bf, ok := o.(*BuiltinFunction); ok && bf.Module != "" {
				return NewStr(bf.Module), nil
			}
			return None(), nil
		},
		nil,
	))
	// __text_signature__ is the Argument Clinic signature string.
	// inspect._signature_from_builtin reads this to construct a Signature
	// object for builtins that don't have an introspectable code object.
	//
	// CPython: Objects/methodobject.c:286 meth_getsets (__text_signature__
	// is accessed via the generic ml_doc first-line protocol; gopy
	// exposes it explicitly as a getset instead)
	SetTypeDescr(BuiltinFunctionType, "__text_signature__", NewGetSetDescr("__text_signature__",
		func(o Object) (Object, error) {
			if bf, ok := o.(*BuiltinFunction); ok && bf.TextSignature != "" {
				return NewStr(bf.TextSignature), nil
			}
			return None(), nil
		},
		nil,
	))
	// meth_methods: __reduce__ returns the bare name string when the
	// function is module-bound (m_self is the module or NULL). Pickle's
	// save() inspects __reduce__ via __reduce_ex__ and treats a str
	// return as "look this up by name in self.__module__". Without
	// this, codecs.encode falls through into copyreg._reduce_ex which
	// can't pickle a BIF.
	//
	// CPython: Objects/methodobject.c:192 meth_reduce
	SetTypeDescr(BuiltinFunctionType, "__reduce__", NewMethodDescrConv(BuiltinFunctionType, "__reduce__", MethNoArgs, builtinFunctionReduce))
}

// builtinFunctionQualname mirrors meth_get__qualname__. If m_self is
// NULL or a module the qualname is the bare name (e.g. len.__qualname__
// == 'len'). If m_self is a type the result is type.__qualname__ + '.'
// + name (e.g. str.maketrans.__qualname__ == 'str.maketrans'), and
// otherwise type(m_self).__qualname__ + '.' + name.
//
// CPython: Objects/methodobject.c:231 meth_get__qualname__
func builtinFunctionQualname(o Object) (Object, error) {
	bf, ok := o.(*BuiltinFunction)
	if !ok {
		return None(), nil
	}
	if bf.Self == nil {
		return NewStr(bf.Name), nil
	}
	if _, isMod := bf.Self.(*Module); isMod {
		return NewStr(bf.Name), nil
	}
	owner := bf.Self
	if _, isType := bf.Self.(*Type); !isType {
		owner = bf.Self.Type()
	}
	tq, err := GetAttr(owner, NewStr("__qualname__"))
	if err != nil {
		return nil, err
	}
	tqStr, ok := tq.(*Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: <method>.__class__.__qualname__ is not a unicode object")
	}
	return NewStr(tqStr.Value() + "." + bf.Name), nil
}

// builtinFunctionReduce is meth_reduce. For module-level functions
// (m_self == NULL or a module), CPython returns just the name string,
// which pickle takes as "save_global by name". gopy's BuiltinFunction
// has no m_self slot yet, so we always take the module-bound branch.
//
// CPython: Objects/methodobject.c:192 meth_reduce
func builtinFunctionReduce(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __reduce__() takes no arguments (%d given)", len(args)-1)
	}
	bf, ok := args[0].(*BuiltinFunction)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__reduce__' requires a 'builtin_function_or_method' object")
	}
	return NewStr(bf.Name), nil
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
// always carries a (slice, map) closure. For kwargs-less conventions
// (MethNoArgs / MethO / MethFastcall) it raises TypeError on any
// keyword argument the way cfunction_check_kwargs does in CPython, so
// builtins like id() reject id(1, foo=2) without each impl having to
// check.
//
// CPython: Objects/methodobject.c:544 cfunction_call
// CPython: Objects/methodobject.c:399 cfunction_check_kwargs
func builtinFunctionCall(o Object, args []Object, kwargs map[string]Object) (Object, error) {
	bf := o.(*BuiltinFunction)
	if err := builtinFunctionCheckKwargs(bf, len(kwargs)); err != nil {
		return nil, err
	}
	return bf.Fn(args, kwargs)
}

// builtinFunctionCheckKwargs raises TypeError when a kwargs-less
// calling convention received any keyword arguments. Mirrors
// cfunction_check_kwargs over BuiltinFunction.Conv.
//
// CPython: Objects/methodobject.c:399 cfunction_check_kwargs
func builtinFunctionCheckKwargs(bf *BuiltinFunction, nkw int) error {
	if nkw == 0 {
		return nil
	}
	calling := bf.Conv & (MethVarargs | MethKeywords | MethNoArgs | MethO | MethFastcall | MethMethod)
	switch calling {
	case MethNoArgs, MethO, MethFastcall:
		return fmt.Errorf("TypeError: %s() takes no keyword arguments", bf.Name)
	}
	return nil
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
		if err := builtinFunctionCheckKwargs(bf, nkw); err != nil {
			return nil, err
		}
		kwargs = make(map[string]Object, nkw)
		for i := range nkw {
			name, err := Str(kwnames.Item(i))
			if err != nil {
				return nil, err
			}
			kwargs[name] = args[nargs+i]
		}
	}
	return bf.Fn(pos, kwargs)
}
