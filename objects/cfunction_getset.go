package objects

import "fmt"

// CPython's PyCFunction_Type carries meth_getsets so a built-in
// function or method reports __name__, __qualname__, __self__,
// __module__, __doc__ and __text_signature__. gopy's CFunction grew
// up only as a call target, so the getsets were never wired; that left
// _PyObject_FunctionStr falling back to str(cf) and rendering call-time
// TypeErrors as "<built-in function meth_o> takes ..." instead of the
// "meth_o() takes ..." CPython produces. Porting the getset table fixes
// the message shape and gives CFunction the same introspection surface
// PyCFunctionObject exposes.
//
// CPython: Objects/methodobject.c:265 meth_getsets

func init() {
	SetTypeDescr(CFunctionType, "__name__", NewGetSetDescr("__name__",
		func(o Object) (Object, error) {
			if cf, ok := o.(*CFunction); ok {
				return NewStr(cf.Def.Name), nil
			}
			return None(), nil
		},
		nil,
	))
	// CPython: Objects/methodobject.c:231 meth_get__qualname__
	SetTypeDescr(CFunctionType, "__qualname__", NewGetSetDescr("__qualname__",
		cfunctionQualname,
		nil,
	))
	// __self__ exposes m_self. A METH_STATIC method masks it to None, a
	// module-level function returns its module, and a bound method
	// returns the instance or owning type.
	//
	// CPython: Objects/methodobject.c:218 meth_get__self__
	SetTypeDescr(CFunctionType, "__self__", NewGetSetDescr("__self__",
		func(o Object) (Object, error) {
			cf, ok := o.(*CFunction)
			if !ok || cf.Self == nil {
				return None(), nil
			}
			return cf.Self, nil
		},
		nil,
	))
	// CPython: Objects/methodobject.c:233 meth_get__module__
	SetTypeDescr(CFunctionType, "__module__", NewGetSetDescr("__module__",
		func(o Object) (Object, error) {
			if cf, ok := o.(*CFunction); ok && cf.Module != nil {
				return cf.Module, nil
			}
			return None(), nil
		},
		nil,
	))
	SetTypeDescr(CFunctionType, "__doc__", NewGetSetDescr("__doc__",
		func(o Object) (Object, error) {
			if cf, ok := o.(*CFunction); ok && cf.Def.Doc != "" {
				return NewStr(cf.Def.Doc), nil
			}
			return None(), nil
		},
		nil,
	))
}

// cfunctionQualname mirrors meth_get__qualname__ over a CFunction: a
// module-bound or self-less function is its bare name, a method bound to
// a type is "type.__qualname__.name", and an instance-bound method is
// "type(self).__qualname__.name".
//
// CPython: Objects/methodobject.c:231 meth_get__qualname__
func cfunctionQualname(o Object) (Object, error) {
	cf, ok := o.(*CFunction)
	if !ok {
		return None(), nil
	}
	if cf.Self == nil {
		return NewStr(cf.Def.Name), nil
	}
	if _, isMod := cf.Self.(*Module); isMod {
		return NewStr(cf.Def.Name), nil
	}
	owner := cf.Self
	if _, isType := cf.Self.(*Type); !isType {
		owner = cf.Self.Type()
	}
	tq, err := GetAttr(owner, NewStr("__qualname__"))
	if err != nil {
		return nil, err
	}
	tqStr, ok := tq.(*Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: <method>.__class__.__qualname__ is not a unicode object")
	}
	return NewStr(tqStr.Value() + "." + cf.Def.Name), nil
}
