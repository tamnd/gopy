package objects

import (
	"fmt"
	"reflect"
)

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

	// ownsSelf records that this BuiltinFunction took a reference on Self
	// at construction time (PyCMethod_New's Py_XNewRef), so its dealloc
	// must release exactly that one reference. Most BuiltinFunctions are
	// registered at init with a borrowed Self (a type or module held
	// elsewhere) and must NOT decref it; only the bound-method temporaries
	// minted by newBoundCMethod / classmethod binding own their receiver.
	//
	// CPython: Objects/methodobject.c:46 PyCMethod_New (Py_XNewRef(self))
	ownsSelf bool

	// boundDescr, when non-nil, is the method_descriptor this builtin
	// function was minted from when method_get bound it to a receiver
	// (PyCMethod_New). builtinFunctionVectorcall forwards straight to the
	// descriptor's FASTCALL_KEYWORDS path with m_self prepended and the
	// kwnames tuple intact, so keyword *order* survives. Without it the
	// generic arm flattens kwnames into a Go map, whose iteration order is
	// randomized, which scrambles order-sensitive impls (e.g. anything
	// that reconstructs an insertion-ordered dict from **kwargs).
	//
	// CPython: Objects/descrobject.c:230 method_get (PyCMethod_New)
	boundDescr *MethodDescr

	// methOrigin is the stable descriptor a builtin method was minted from
	// when no *MethodDescr drives its call path: classmethod_get binds a
	// classmethod_descriptor (PyCMethod_New) into a builtin_function_or_method
	// whose m_ml is the descriptor's PyMethodDef, shared across every binding.
	// methFuncIdentical / builtinFunctionHash use it as the m_ml proxy so
	// int.from_bytes == int.from_bytes even though the bindings are distinct.
	//
	// CPython: Objects/descrobject.c:95 classmethod_get (a->m_ml == b->m_ml)
	methOrigin Object

	// kwParams, when non-nil, names every keyword the Argument Clinic
	// signature accepts. builtinFunctionVectorcall runs the AC
	// extraneous-keyword scan over the original kwnames objects before
	// the impl runs, so a str-subclass key that Python-compares equal to
	// a real parameter name (yet hashes elsewhere, so it never binds)
	// yields "invalid keyword argument for f()" rather than a plain
	// "unexpected keyword argument", exactly as error_unexpected_keyword_arg
	// does. kwFname is the bare name the message prints.
	//
	// CPython: Python/getargs.c:1442 error_unexpected_keyword_arg
	kwParams []string
	kwFname  string
}

// WithKwParams declares the Argument Clinic keyword list so the
// vectorcall arm runs the faithful extraneous-keyword scan. It returns
// the receiver for chaining at registration time.
//
// CPython: Python/getargs.c:1442 error_unexpected_keyword_arg
func (bf *BuiltinFunction) WithKwParams(fname string, params []string) *BuiltinFunction {
	bf.kwFname = fname
	bf.kwParams = params
	return bf
}

// BuiltinFunctionType is the type singleton for built-in functions.
//
// CPython: Objects/methodobject.c:357 PyCFunction_Type
var BuiltinFunctionType = NewType("builtin_function_or_method", []*Type{objectType})

func init() {
	// gopy's BuiltinFunction stands in for PyCFunction_Type, which
	// advertises Py_TPFLAGS_HAVE_VECTORCALL.
	//
	// CPython: Objects/methodobject.c:357 PyCFunction_Type tp_flags
	BuiltinFunctionType.TpFlags |= TpFlagHaveVectorcall
	// PyCFunction_Type is not subclassable: it omits Py_TPFLAGS_BASETYPE, so
	// class C(type(len)) raises "is not an acceptable base type".
	//
	// CPython: Objects/methodobject.c:357 PyCFunction_Type tp_flags
	BuiltinFunctionType.TpFlags &^= TpFlagBasetype
	BuiltinFunctionType.Repr = builtinFunctionRepr
	BuiltinFunctionType.Str = builtinFunctionRepr
	BuiltinFunctionType.Call = builtinFunctionCall
	BuiltinFunctionType.Vectorcall = builtinFunctionVectorcall
	// meth_traverse / meth_dealloc visit and release m_self so a method
	// bound to a temporary (PyCMethod_New took a reference) drops it when
	// the bound method is collected. Decref on an immortal module/type
	// receiver is a no-op, so static-method selves stay pinned.
	//
	// CPython: Objects/methodobject.c:131 meth_dealloc / meth_traverse
	BuiltinFunctionType.Dealloc = builtinFunctionDealloc
	BuiltinFunctionType.TpTraverse = builtinFunctionTraverse
	AddCallSlotWrapper(BuiltinFunctionType)
	// meth_richcompare: two builtin_function_or_method objects are equal
	// when they share the same receiver (m_self, by identity) and the same
	// underlying C function (m_ml). Only ==/!= are defined; ordering yields
	// NotImplemented. meth_hash combines the receiver's identity hash with
	// the function's, so l.append == l.append yet hashes don't collapse onto
	// hash(l).
	//
	// CPython: Objects/methodobject.c:213 meth_richcompare / :245 meth_hash
	BuiltinFunctionType.RichCmp = builtinFunctionRichCompare
	BuiltinFunctionType.Hash = builtinFunctionHash

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
	// meth_get__self__ returns PyCFunction_GET_SELF(m): m_self unless the
	// method is METH_STATIC, in which case (and when m_self is NULL) it
	// returns None. For a module-level builtin like len, m_self is the
	// owning module, so len.__self__ is the builtins module.
	//
	// CPython: Objects/methodobject.c:174 meth_get__self__
	SetTypeDescr(BuiltinFunctionType, "__self__", NewGetSetDescr("__self__",
		func(o Object) (Object, error) {
			bf, ok := o.(*BuiltinFunction)
			if !ok || bf.Self == nil || bf.Conv&MethStatic != 0 {
				return None(), nil
			}
			Incref(bf.Self)
			return bf.Self, nil
		},
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

// builtinFunctionRepr mirrors meth_repr: a function whose m_self is NULL
// or a module reads "<built-in function NAME>"; one bound to any other
// object (an instance from a bound method_descriptor, or a type from a
// classmethod/staticmethod row) reads "<built-in method NAME of TYPE
// object at ADDR>".
//
// CPython: Objects/methodobject.c:155 meth_repr
// builtinFunctionTraverse visits m_self so a bound C method participates
// in cycle detection (a method bound to an object stored on that object
// forms a reference cycle).
//
// CPython: Objects/methodobject.c:126 meth_traverse
func builtinFunctionTraverse(o Object, visit Visitor) error {
	bf := o.(*BuiltinFunction)
	if bf.Self != nil {
		return visit(bf.Self)
	}
	return nil
}

// builtinFunctionDealloc releases the reference PyCMethod_New took on
// m_self. Decref on an immortal receiver (a module or type held by a
// static/classmethod row) is a no-op, so only bound-to-temporary methods
// actually release here.
//
// CPython: Objects/methodobject.c:113 meth_dealloc
func builtinFunctionDealloc(o Object) {
	bf := o.(*BuiltinFunction)
	if bf.ownsSelf && bf.Self != nil {
		Decref(bf.Self)
	}
}

// methFuncIdentical decides whether two builtin functions wrap the same
// underlying callable. CPython compares the m_ml PyMethodDef pointer, which
// is shared by every binding of a given method. gopy's closest stable proxy
// is the method_descriptor a bound c-method was minted from (boundDescr,
// shared across l.append accesses); functions without one (module-level
// singletons like len) are their own identity.
//
// CPython: Objects/methodobject.c:213 meth_richcompare (a->m_ml == b->m_ml)
func methFuncIdentical(a, b *BuiltinFunction) bool {
	if a.boundDescr != nil || b.boundDescr != nil {
		return a.boundDescr == b.boundDescr
	}
	if a.methOrigin != nil || b.methOrigin != nil {
		return a.methOrigin == b.methOrigin
	}
	return a == b
}

// builtinFunctionRichCompare ports meth_richcompare: only ==/!= are
// meaningful, and equality holds when the receiver (m_self, by identity) and
// the wrapped function match.
//
// CPython: Objects/methodobject.c:213 meth_richcompare
func builtinFunctionRichCompare(a, b Object, op CompareOp) (Object, error) {
	if op != CompareEQ && op != CompareNE {
		return NotImplemented(), nil
	}
	fa, ok := a.(*BuiltinFunction)
	if !ok {
		return NotImplemented(), nil
	}
	fb, ok := b.(*BuiltinFunction)
	if !ok {
		return NotImplemented(), nil
	}
	eq := fa.Self == fb.Self && methFuncIdentical(fa, fb)
	if op == CompareNE {
		eq = !eq
	}
	return NewBool(eq), nil
}

// builtinFunctionHash ports meth_hash: the identity hash of m_self XORed
// with the wrapped function's hash, so equal bindings hash alike while the
// receiver's own hash never leaks in (hash([].append) is not hash([])).
//
// CPython: Objects/methodobject.c:245 meth_hash
func builtinFunctionHash(o Object) (int64, error) {
	bf := o.(*BuiltinFunction)
	var x int64
	if bf.Self != nil {
		h, err := identityHash(bf.Self)
		if err != nil {
			return 0, err
		}
		x = h
	}
	var y int64
	var err error
	if bf.boundDescr != nil {
		y, err = identityHash(bf.boundDescr)
	} else if bf.methOrigin != nil {
		y, err = identityHash(bf.methOrigin)
	} else {
		y, err = identityHash(bf)
	}
	if err != nil {
		return 0, err
	}
	x ^= y
	if x == -1 {
		x = -2
	}
	return x, nil
}

func builtinFunctionRepr(o Object) (string, error) {
	bf := o.(*BuiltinFunction)
	if bf.Self == nil {
		return "<built-in function " + bf.Name + ">", nil
	}
	if _, isMod := bf.Self.(*Module); isMod {
		return "<built-in function " + bf.Name + ">", nil
	}
	addr := reflect.ValueOf(bf.Self).Pointer()
	return fmt.Sprintf("<built-in method %s of %s object at %#x>", bf.Name, bf.Self.Type().Name, addr), nil
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

// builtinFunctionNoKeywordsError renders the "takes no keyword
// arguments" TypeError through _PyObject_FunctionStr, so a module
// function reports its dotted name ("_struct.pack()") and a builtin
// keeps its bare name ("getattr()"), matching cfunction_check_kwargs.
//
// CPython: Objects/methodobject.c:399 cfunction_check_kwargs
func builtinFunctionNoKeywordsError(bf *BuiltinFunction) error {
	funcstr, err := FunctionStr(bf)
	if err != nil {
		funcstr = bf.Name + "()"
	}
	return fmt.Errorf("TypeError: %s takes no keyword arguments", funcstr)
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
		return builtinFunctionNoKeywordsError(bf)
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
	// A bound method_descriptor forwards through the descriptor's own
	// vectorcall with m_self prepended, keeping kwnames (and thus keyword
	// order) intact. CPython: method_vectorcall over PyCMethod m_self.
	if bf.boundDescr != nil {
		nargs := VectorcallNargs(nargsf)
		nkw := 0
		if kwnames != nil {
			nkw = kwnames.Len()
		}
		stack := make([]Object, 1+nargs+nkw)
		stack[0] = bf.Self
		copy(stack[1:], args[:nargs+nkw])
		return methodDescrVectorcall(bf.boundDescr, stack, uint(nargs+1), kwnames)
	}
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
		if err := builtinFunctionScanExtraneousKw(bf, kwnames); err != nil {
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

// builtinFunctionScanExtraneousKw ports error_unexpected_keyword_arg for
// built-ins that declared an Argument Clinic keyword list via
// WithKwParams. It only runs when at least one keyword's exact string is
// not a recognized parameter (the binder's error path). It then scans
// the keyword names in caller order: the first one that does not
// Python-compare equal to any parameter is the offending keyword and
// gets the unexpected-keyword TypeError. If every key compares equal
// (only reachable when a str subclass overrides __eq__), no single key
// can be named, so the generic "invalid keyword argument" is raised.
//
// CPython: Python/getargs.c:1442 error_unexpected_keyword_arg
func builtinFunctionScanExtraneousKw(bf *BuiltinFunction, kwnames *Tuple) error {
	if bf.kwParams == nil || kwnames == nil {
		return nil
	}
	n := kwnames.Len()
	extraneous := false
	for i := 0; i < n; i++ {
		s, err := Str(kwnames.Item(i))
		if err != nil {
			return err
		}
		if !containsString(bf.kwParams, s) {
			extraneous = true
			break
		}
	}
	if !extraneous {
		return nil
	}
	for i := 0; i < n; i++ {
		key := kwnames.Item(i)
		if !IsSubtype(key.Type(), StrType()) {
			return errKeywordsMustBeStrings
		}
		matched := false
		for _, p := range bf.kwParams {
			eq, err := RichCmpBool(NewStr(p), key, CompareEQ)
			if err != nil {
				return err
			}
			if eq {
				matched = true
				break
			}
		}
		if !matched {
			s, err := Str(key)
			if err != nil {
				return err
			}
			return UnexpectedKeywordError(bf.kwFname, s, bf.kwParams)
		}
	}
	return fmt.Errorf("TypeError: invalid keyword argument for %s()", bf.kwFname)
}

// containsString reports whether s appears in list.
func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
