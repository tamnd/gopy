// Bound methods, classmethod, and staticmethod. Each one is a
// descriptor whose __get__ rewires the call to put the right
// argument in front:
//
//   method:        f.__get__(obj, type) → bound(f, obj)
//   classmethod:   f.__get__(obj, type) → bound(f, type)
//   staticmethod:  f.__get__(obj, type) → f
//
// CPython: Objects/classobject.c:75 method_getattro

package objects

import "fmt"

// BoundMethod is the value attribute access produces when a function
// is fetched off an instance: it pairs the function with the instance
// so calling it prepends the instance to the positional args.
//
// CPython: Include/cpython/classobject.h:7 PyMethodObject
type BoundMethod struct {
	Header
	imFunc Object
	imSelf Object
}

// BoundMethodType is the type singleton for bound methods.
//
// CPython: Objects/classobject.c:268 PyMethod_Type
var BoundMethodType = NewType("method", []*Type{objectType})

func init() {
	BoundMethodType.Repr = boundMethodRepr
	BoundMethodType.Str = boundMethodRepr
	BoundMethodType.Vectorcall = boundMethodVectorcall
	BoundMethodType.TpTraverse = boundMethodTraverse
	BoundMethodType.Getattro = boundMethodGetattro
	SetTypeDescr(BoundMethodType, "__func__", NewGetSetDescr("__func__",
		func(o Object) (Object, error) { return o.(*BoundMethod).imFunc, nil },
		nil))
	SetTypeDescr(BoundMethodType, "__self__", NewGetSetDescr("__self__",
		func(o Object) (Object, error) { return o.(*BoundMethod).imSelf, nil },
		nil))
}

// boundMethodGetattro looks for name on the method type first (so
// __func__ / __self__ resolve through their getset descriptors),
// then falls through to GetAttr on the wrapped function. The fall
// through is what lets m.__doc__, m.__name__, and m.__module__ pull
// from the underlying function the way CPython exposes them.
//
// CPython: Objects/classobject.c:75 method_getattro
func boundMethodGetattro(o Object, name Object) (Object, error) {
	m := o.(*BoundMethod)
	descr, _ := LookupDescriptor(o.Type(), attrNameStr(name))
	if descr != nil {
		if dg := descr.Type().DescrGet; dg != nil {
			return dg(descr, o, o.Type())
		}
		return descr, nil
	}
	return GetAttr(m.imFunc, name)
}

// boundMethodTraverse visits imFunc and imSelf. Mirrors method_traverse.
//
// CPython: Objects/classobject.c:262 method_traverse
func boundMethodTraverse(o Object, visit Visitor) error {
	m := o.(*BoundMethod)
	if m.imFunc != nil {
		if err := visit(m.imFunc); err != nil {
			return err
		}
	}
	if m.imSelf != nil {
		return visit(m.imSelf)
	}
	return nil
}

// NewBoundMethod pairs fn with self.
//
// CPython: Objects/classobject.c:38 PyMethod_New
func NewBoundMethod(fn Object, self Object) *BoundMethod {
	m := &BoundMethod{imFunc: fn, imSelf: self}
	m.init(BoundMethodType)
	return m
}

// Func returns the underlying function.
func (m *BoundMethod) Func() Object { return m.imFunc }

// Self returns the bound instance.
func (m *BoundMethod) Self() Object { return m.imSelf }

func boundMethodRepr(o Object) (string, error) {
	m := o.(*BoundMethod)
	return fmt.Sprintf("<bound method of %s>", m.imSelf.Type().Name), nil
}

// boundMethodVectorcall prepends self to the positional args, then
// hands control to the wrapped function. Mirrors the inline logic in
// CPython's method_vectorcall.
//
// CPython: Objects/classobject.c:51 method_vectorcall
func boundMethodVectorcall(callable Object, args []Object, nargsf uint, kwnames *Tuple) (Object, error) {
	m := callable.(*BoundMethod)
	nargs := VectorcallNargs(nargsf)
	nkw := 0
	if kwnames != nil {
		nkw = kwnames.Len()
	}
	stack := make([]Object, 1+nargs+nkw)
	stack[0] = m.imSelf
	copy(stack[1:], args[:nargs+nkw])
	return Vectorcall(m.imFunc, stack, uint(nargs+1), kwnames)
}

// ClassMethod is the descriptor that turns @classmethod-decorated
// functions into "type-bound" methods. The class is what gets
// prepended to the call rather than the instance.
//
// CPython: Include/cpython/funcobject.h ClassMethod (analog)
type ClassMethod struct {
	Header
	cmCallable Object
}

// ClassMethodType is the type singleton for classmethod.
//
// CPython: Objects/funcobject.c:1119 PyClassMethod_Type
var ClassMethodType = NewType("classmethod", []*Type{objectType})

func init() {
	ClassMethodType.Repr = classMethodRepr
	ClassMethodType.Str = classMethodRepr
	ClassMethodType.DescrGet = classMethodDescrGet
	ClassMethodType.TpTraverse = classMethodTraverse
	// classmethod(fn): wrap fn so attribute access binds to the class.
	//
	// CPython: Objects/funcobject.c:1059 cm_init
	ClassMethodType.TpNew = func(_ *Type, args []Object, _ map[string]Object) (Object, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("TypeError: classmethod expected 1 argument, got 0")
		}
		return NewClassMethod(args[0]), nil
	}
	StaticMethodType.TpNew = func(_ *Type, args []Object, _ map[string]Object) (Object, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("TypeError: staticmethod expected 1 argument, got 0")
		}
		return NewStaticMethod(args[0]), nil
	}
}

// classMethodTraverse visits the wrapped callable. Mirrors cm_traverse.
//
// CPython: Objects/funcobject.c:1106 cm_traverse
func classMethodTraverse(o Object, visit Visitor) error {
	cm := o.(*ClassMethod)
	if cm.cmCallable == nil {
		return nil
	}
	return visit(cm.cmCallable)
}

// NewClassMethod wraps fn so attribute access binds it to the class
// rather than the instance.
//
// CPython: Objects/funcobject.c:1059 cm_init
func NewClassMethod(fn Object) *ClassMethod {
	cm := &ClassMethod{cmCallable: fn}
	cm.init(ClassMethodType)
	return cm
}

// Func returns the wrapped callable.
func (cm *ClassMethod) Func() Object { return cm.cmCallable }

func classMethodRepr(o Object) (string, error) {
	return "<classmethod object>", nil
}

// classMethodDescrGet returns a bound method whose self is the
// owning type rather than the instance. CPython falls back to the
// instance's type when no explicit type was passed.
//
// CPython: Objects/funcobject.c:1024 cm_descr_get
func classMethodDescrGet(descr Object, owner Object, ownerType *Type) (Object, error) {
	cm := descr.(*ClassMethod)
	t := ownerType
	if t == nil && owner != nil {
		t = owner.Type()
	}
	if t == nil {
		return cm.cmCallable, nil
	}
	return NewBoundMethod(cm.cmCallable, t), nil
}

// StaticMethod is the descriptor that strips the descriptor protocol
// off a function: __get__ just returns the wrapped callable, so
// calling it through an instance does not prepend self.
//
// CPython: Include/cpython/funcobject.h StaticMethod (analog)
type StaticMethod struct {
	Header
	smCallable Object
}

// StaticMethodType is the type singleton for staticmethod.
//
// CPython: Objects/funcobject.c:1233 PyStaticMethod_Type
var StaticMethodType = NewType("staticmethod", []*Type{objectType})

func init() {
	StaticMethodType.Repr = staticMethodRepr
	StaticMethodType.Str = staticMethodRepr
	StaticMethodType.DescrGet = staticMethodDescrGet
	StaticMethodType.TpTraverse = staticMethodTraverse
}

// staticMethodTraverse visits the wrapped callable. Mirrors sm_traverse.
//
// CPython: Objects/funcobject.c:1220 sm_traverse
func staticMethodTraverse(o Object, visit Visitor) error {
	sm := o.(*StaticMethod)
	if sm.smCallable == nil {
		return nil
	}
	return visit(sm.smCallable)
}

// NewStaticMethod wraps fn so attribute access on an instance returns
// the callable directly without binding self.
//
// CPython: Objects/funcobject.c:1184 sm_init
func NewStaticMethod(fn Object) *StaticMethod {
	sm := &StaticMethod{smCallable: fn}
	sm.init(StaticMethodType)
	return sm
}

// Func returns the wrapped callable.
func (sm *StaticMethod) Func() Object { return sm.smCallable }

func staticMethodRepr(o Object) (string, error) {
	return "<staticmethod object>", nil
}

// staticMethodDescrGet just hands back the wrapped callable.
//
// CPython: Objects/funcobject.c:1167 sm_descr_get
func staticMethodDescrGet(descr Object, _ Object, _ *Type) (Object, error) {
	return descr.(*StaticMethod).smCallable, nil
}
