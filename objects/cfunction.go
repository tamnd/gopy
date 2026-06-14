// CFunction is the gopy analog of PyCFunctionObject: a built-in
// function or method described by a MethodDef and bound to an
// optional self. The MethodDef's Flags decide which Meth* closure
// the dispatcher reads, exactly the way ml_flags type-puns the C
// function pointer in CPython.
//
// CPython: Objects/methodobject.c:34 PyCFunction_New

package objects

import (
	"fmt"
)

// MethFlag is the bit field stored in MethodDef.Flags. Mirrors the
// METH_* constants in Include/methodobject.h: each value labels which
// closure the dispatcher should call and what argument shape it
// expects.
//
// CPython: Include/methodobject.h:95 METH_VARARGS
type MethFlag int

const (
	MethVarargs  MethFlag = 1 << 0
	MethKeywords MethFlag = 1 << 1
	MethNoArgs   MethFlag = 1 << 2
	MethO        MethFlag = 1 << 3
	MethClass    MethFlag = 1 << 4
	MethStatic   MethFlag = 1 << 5
	MethCoexist  MethFlag = 1 << 6
	MethFastcall MethFlag = 1 << 7
	MethMethod   MethFlag = 1 << 9
)

// MethodDef is the gopy analog of PyMethodDef. The mask of Flags
// against the calling-convention bits picks which of the closures
// is consulted. Every variant is optional, but exactly one must be
// set for the chosen flag combo or NewCFunction returns SystemError
// the way PyCMethod_New does.
//
// CPython: Include/methodobject.h:71 PyMethodDef
type MethodDef struct {
	Name  string
	Doc   string
	Flags MethFlag

	// MethNoArgs: f(self).
	NoArgs func(self Object) (Object, error)
	// MethO: f(self, arg).
	O func(self, arg Object) (Object, error)
	// MethVarargs: f(self, args).
	Varargs func(self Object, args *Tuple) (Object, error)
	// MethVarargs|MethKeywords: f(self, args, kwargs).
	VarargsKw func(self Object, args *Tuple, kwargs *Dict) (Object, error)
	// MethFastcall: f(self, args).
	Fastcall func(self Object, args []Object) (Object, error)
	// MethFastcall|MethKeywords: f(self, args, kwnames).
	FastcallKw func(self Object, args []Object, kwnames *Tuple) (Object, error)
	// MethMethod|MethFastcall|MethKeywords: f(self, cls, args, kwnames).
	Method func(self Object, cls *Type, args []Object, kwnames *Tuple) (Object, error)
}

// CFunction wraps a MethodDef plus a bound self. Callers reach the
// underlying closure via the type's Vectorcall slot; legacy paths
// land in the tp_call slot which forwards the same way cfunction_call
// does in CPython.
//
// CPython: Include/cpython/methodobject.h PyCFunctionObject
type CFunction struct {
	Header
	Def    *MethodDef
	Self   Object
	Module Object
	Cls    *Type
}

// CFunctionType is the type singleton for CFunction. CPython uses
// PyCFunction_Type and a sibling PyCMethod_Type for MethMethod; gopy
// folds them into one type with a Cls field, since the dispatch
// difference is on the MethodDef flag set, not on the object layout.
//
// CPython: Objects/methodobject.c:357 PyCFunction_Type
var CFunctionType = NewType("builtin_function_or_method", []*Type{objectType})

func init() {
	// PyCFunction_Type advertises Py_TPFLAGS_HAVE_VECTORCALL
	// (tp_vectorcall_offset = vectorcall) but NOT METHOD_DESCRIPTOR.
	//
	// CPython: Objects/methodobject.c:357 PyCFunction_Type tp_flags
	CFunctionType.TpFlags |= TpFlagHaveVectorcall
	// PyCFunction_Type omits Py_TPFLAGS_BASETYPE, so it cannot be subclassed.
	//
	// CPython: Objects/methodobject.c:357 PyCFunction_Type tp_flags
	CFunctionType.TpFlags &^= TpFlagBasetype
	CFunctionType.Repr = cfunctionRepr
	CFunctionType.Str = cfunctionRepr
	CFunctionType.Vectorcall = cfunctionVectorcall
	CFunctionType.Call = cfunctionCall
}

// NewCFunction binds def to self; module and cls are optional. cls
// must be set when def carries MethMethod, and absent otherwise,
// matching the SystemError shape of PyCMethod_New.
//
// CPython: Objects/methodobject.c:46 PyCMethod_New
func NewCFunction(def *MethodDef, self Object, module Object, cls *Type) (*CFunction, error) {
	if def == nil {
		return nil, fmt.Errorf("SystemError: NewCFunction: nil MethodDef")
	}
	if def.Flags&MethMethod != 0 {
		if cls == nil {
			return nil, fmt.Errorf("SystemError: attempting to create PyCMethod with a MethMethod flag but no class")
		}
	} else if cls != nil {
		return nil, fmt.Errorf("SystemError: attempting to create PyCFunction with class but no MethMethod flag")
	}
	if err := checkMethodDef(def); err != nil {
		return nil, err
	}
	cf := &CFunction{Def: def, Self: self, Module: module, Cls: cls}
	cf.init(CFunctionType)
	return cf, nil
}

// checkMethodDef verifies the calling-convention bits in flags name
// exactly one supported shape and that the matching closure field is
// populated. Mirrors the switch in PyCMethod_New that picks a
// vectorcall function and otherwise raises SystemError "bad call
// flags".
//
// CPython: Objects/methodobject.c:51 PyCMethod_New (switch on ml_flags)
func checkMethodDef(def *MethodDef) error {
	calling := def.Flags & (MethVarargs | MethKeywords | MethNoArgs | MethO | MethFastcall | MethMethod)
	switch calling {
	case MethNoArgs:
		if def.NoArgs == nil {
			return badMethodDef(def, "MethNoArgs but NoArgs closure is nil")
		}
	case MethO:
		if def.O == nil {
			return badMethodDef(def, "MethO but O closure is nil")
		}
	case MethVarargs:
		if def.Varargs == nil {
			return badMethodDef(def, "MethVarargs but Varargs closure is nil")
		}
	case MethVarargs | MethKeywords:
		if def.VarargsKw == nil {
			return badMethodDef(def, "MethVarargs|MethKeywords but VarargsKw closure is nil")
		}
	case MethFastcall:
		if def.Fastcall == nil {
			return badMethodDef(def, "MethFastcall but Fastcall closure is nil")
		}
	case MethFastcall | MethKeywords:
		if def.FastcallKw == nil {
			return badMethodDef(def, "MethFastcall|MethKeywords but FastcallKw closure is nil")
		}
	case MethMethod | MethFastcall | MethKeywords:
		if def.Method == nil {
			return badMethodDef(def, "MethMethod|MethFastcall|MethKeywords but Method closure is nil")
		}
	default:
		return badMethodDef(def, "bad call flags")
	}
	return nil
}

func badMethodDef(def *MethodDef, msg string) error {
	return fmt.Errorf("SystemError: %s() method: %s", def.Name, msg)
}

func cfunctionRepr(o Object) (string, error) {
	cf := o.(*CFunction)
	return "<built-in function " + cf.Def.Name + ">", nil
}

// cfunctionVectorcall is the Vectorcall slot. It picks the matching
// dispatch path the way the per-flavor cfunction_vectorcall_* funcs
// do in CPython, then unpacks the vectorcall args into the closure's
// expected shape.
//
// CPython: Objects/methodobject.c:434 cfunction_vectorcall_FASTCALL
// (and the four other cfunction_vectorcall_* variants).
func cfunctionVectorcall(callable Object, args []Object, nargsf uint, kwnames *Tuple) (Object, error) {
	cf := callable.(*CFunction)
	def := cf.Def
	nargs := VectorcallNargs(nargsf)
	calling := def.Flags & (MethVarargs | MethKeywords | MethNoArgs | MethO | MethFastcall | MethMethod)

	switch calling {
	case MethNoArgs:
		if err := cfunctionCheckKwargs(cf, kwnames); err != nil {
			return nil, err
		}
		if nargs != 0 {
			funcstr, err := FunctionStr(cf)
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("TypeError: %s takes no arguments (%d given)", funcstr, nargs)
		}
		return def.NoArgs(cf.Self)

	case MethO:
		if err := cfunctionCheckKwargs(cf, kwnames); err != nil {
			return nil, err
		}
		if nargs != 1 {
			funcstr, err := FunctionStr(cf)
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("TypeError: %s takes exactly one argument (%d given)", funcstr, nargs)
		}
		return def.O(cf.Self, args[0])

	case MethFastcall:
		if err := cfunctionCheckKwargs(cf, kwnames); err != nil {
			return nil, err
		}
		return def.Fastcall(cf.Self, args[:nargs])

	case MethFastcall | MethKeywords:
		return def.FastcallKw(cf.Self, args[:nargs+kwnamesLen(kwnames)], kwnames)

	case MethMethod | MethFastcall | MethKeywords:
		return def.Method(cf.Self, cf.Cls, args[:nargs+kwnamesLen(kwnames)], kwnames)

	case MethVarargs, MethVarargs | MethKeywords:
		return cfunctionVarargsFromVector(cf, args, nargs, kwnames)
	}
	return nil, badMethodDef(def, "bad call flags")
}

// cfunctionVarargsFromVector materializes a positional tuple and a
// kwargs dict so a MethVarargs or MethVarargs|MethKeywords callee
// can receive its expected (args, kwargs) shape when reached via the
// vectorcall path. CPython sets vectorcall=NULL for MethVarargs so
// this branch only fires when callers go through Vectorcall directly;
// it keeps the surface uniform.
func cfunctionVarargsFromVector(cf *CFunction, args []Object, nargs int, kwnames *Tuple) (Object, error) {
	posItems := make([]Object, nargs)
	copy(posItems, args[:nargs])
	posTuple := NewTuple(posItems)
	var kw *Dict
	if kwnames != nil && kwnames.Len() > 0 {
		kw = NewDict()
		for i := 0; i < kwnames.Len(); i++ {
			if err := kw.SetItem(kwnames.Item(i), args[nargs+i]); err != nil {
				return nil, err
			}
		}
	}
	if cf.Def.Flags&MethKeywords != 0 {
		return cf.Def.VarargsKw(cf.Self, posTuple, kw)
	}
	if kw != nil && kw.Len() != 0 {
		return nil, fmt.Errorf("TypeError: %s() takes no keyword arguments", cf.Def.Name)
	}
	return cf.Def.Varargs(cf.Self, posTuple)
}

// cfunctionCall is the tp_call slot. Mirrors cfunction_call: any
// non-MethVarargs shape forwards to the vectorcall path, and
// MethVarargs dispatches directly so vectorcall doesn't have to
// rebuild the tuple it already received.
//
// CPython: Objects/methodobject.c:543 cfunction_call
func cfunctionCall(o Object, posArgs []Object, kwmap map[string]Object) (Object, error) {
	cf := o.(*CFunction)
	def := cf.Def
	if def.Flags&MethVarargs == 0 {
		nargsf := uint(len(posArgs))
		var args []Object
		var kwnames *Tuple
		if len(kwmap) > 0 {
			args = make([]Object, len(posArgs)+len(kwmap))
			copy(args, posArgs)
			names := make([]Object, 0, len(kwmap))
			i := len(posArgs)
			for k, v := range kwmap {
				names = append(names, NewStr(k))
				args[i] = v
				i++
			}
			kwnames = NewTuple(names)
		} else {
			args = posArgs
		}
		return cfunctionVectorcall(cf, args, nargsf, kwnames)
	}
	posTuple := NewTuple(append([]Object(nil), posArgs...))
	if def.Flags&MethKeywords != 0 {
		var kw *Dict
		if len(kwmap) > 0 {
			kw = NewDict()
			for k, v := range kwmap {
				if err := kw.SetItem(NewStr(k), v); err != nil {
					return nil, err
				}
			}
		}
		return def.VarargsKw(cf.Self, posTuple, kw)
	}
	if len(kwmap) > 0 {
		return nil, fmt.Errorf("TypeError: %s() takes no keyword arguments", def.Name)
	}
	return def.Varargs(cf.Self, posTuple)
}

// cfunctionCheckKwargs raises TypeError when a kwargs-less calling
// convention received any keyword arguments. Mirrors the helper of
// the same name in methodobject.c.
//
// CPython: Objects/methodobject.c:399 cfunction_check_kwargs
func cfunctionCheckKwargs(callable Object, kwnames *Tuple) error {
	if kwnames != nil && kwnames.Len() > 0 {
		funcstr, err := FunctionStr(callable)
		if err != nil {
			return err
		}
		return fmt.Errorf("TypeError: %s takes no keyword arguments", funcstr)
	}
	return nil
}

func kwnamesLen(t *Tuple) int {
	if t == nil {
		return 0
	}
	return t.Len()
}
