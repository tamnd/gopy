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
// CPython: Objects/funcobject.c:1419 classmethod struct
type ClassMethod struct {
	Header
	cmCallable Object
	cmDict     *Dict // lazily allocated, mirrors classmethod.cm_dict
}

// ClassMethodType is the type singleton for classmethod.
//
// CPython: Objects/funcobject.c:1594 PyClassMethod_Type
var ClassMethodType = NewType("classmethod", []*Type{objectType})

func init() {
	ClassMethodType.Repr = classMethodRepr
	ClassMethodType.Str = classMethodRepr
	ClassMethodType.DescrGet = classMethodDescrGet
	ClassMethodType.TpTraverse = classMethodTraverse
	ClassMethodType.Getattro = classMethodGetattro
	// classmethod(fn): wrap fn so attribute access binds to the class.
	//
	// CPython: Objects/funcobject.c:1487 cm_init
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

	// cm_memberlist + cm_getsetlist + cm_methodlist, all in one
	// init pass. __func__ and __wrapped__ both expose cm_callable; the
	// `is` identity tests in the Phase 2 gates require returning the
	// exact same Go pointer for each call.
	//
	// CPython: Objects/funcobject.c:1504 cm_memberlist
	// CPython: Objects/funcobject.c:1551 cm_getsetlist
	// CPython: Objects/funcobject.c:1559 cm_methodlist
	SetTypeDescr(ClassMethodType, "__func__", NewGetSetDescr("__func__", classMethodGetFunc, nil))
	SetTypeDescr(ClassMethodType, "__wrapped__", NewGetSetDescr("__wrapped__", classMethodGetFunc, nil))
	SetTypeDescr(ClassMethodType, "__isabstractmethod__", NewGetSetDescr(
		"__isabstractmethod__", classMethodGetIsAbstract, nil))
	SetTypeDescr(ClassMethodType, "__dict__", NewGetSetDescr(
		"__dict__", classMethodGetDict, classMethodSetDict))
	SetTypeDescr(ClassMethodType, "__annotations__", NewGetSetDescr(
		"__annotations__",
		func(o Object) (Object, error) {
			return descriptorGetWrappedAttribute(o.(*ClassMethod).cmCallable, o, "__annotations__")
		},
		func(o Object, v Object) error {
			return descriptorSetWrappedAttribute(o, "__annotations__", v, "classmethod")
		}))
	SetTypeDescr(ClassMethodType, "__annotate__", NewGetSetDescr(
		"__annotate__",
		func(o Object) (Object, error) {
			return descriptorGetWrappedAttribute(o.(*ClassMethod).cmCallable, o, "__annotate__")
		},
		func(o Object, v Object) error {
			return descriptorSetWrappedAttribute(o, "__annotate__", v, "classmethod")
		}))
	bindClassGetitem(ClassMethodType)
}

// classMethodTraverse visits the wrapped callable and instance dict.
// Mirrors cm_traverse.
//
// CPython: Objects/funcobject.c:1440 cm_traverse
func classMethodTraverse(o Object, visit Visitor) error {
	cm := o.(*ClassMethod)
	if cm.cmCallable != nil {
		if err := visit(cm.cmCallable); err != nil {
			return err
		}
	}
	if cm.cmDict != nil {
		return visit(cm.cmDict)
	}
	return nil
}

// NewClassMethod wraps fn so attribute access binds it to the class
// rather than the instance. The constructor also runs functools_wraps
// so __module__ / __name__ / __qualname__ / __doc__ propagate from
// the wrapped callable to the classmethod itself, exactly like
// CPython's cm_init.
//
// CPython: Objects/funcobject.c:1487 cm_init
func NewClassMethod(fn Object) *ClassMethod {
	cm := &ClassMethod{cmCallable: fn}
	cm.init(ClassMethodType)
	functoolsWraps(cm, fn)
	return cm
}

// Func returns the wrapped callable.
func (cm *ClassMethod) Func() Object { return cm.cmCallable }

// classMethodRepr formats the classmethod the way CPython does:
// `<classmethod(REPR_OF_CALLABLE)>`. The earlier `<classmethod object>`
// placeholder dropped the wrapped callable from the output, so
// debugging "what did I just decorate?" needed an extra .__func__
// dereference.
//
// CPython: Objects/funcobject.c:1565 cm_repr
func classMethodRepr(o Object) (string, error) {
	cm := o.(*ClassMethod)
	inner, err := Repr(cm.cmCallable)
	if err != nil {
		return "", err
	}
	return "<classmethod(" + inner + ")>", nil
}

// classMethodDescrGet returns a bound method whose self is the
// owning type rather than the instance. CPython falls back to the
// instance's type when no explicit type was passed.
//
// CPython: Objects/funcobject.c:1459 cm_descr_get
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

// classMethodGetattro layers the per-instance __dict__ on top of
// GenericGetAttr so attributes copied by functools_wraps (__name__,
// __module__, etc.) become directly accessible on the classmethod
// without each one needing its own getset. CPython gets this for free
// from tp_dictoffset + the generic lookup path.
//
// CPython: Objects/object.c:1932 PyObject_GenericGetAttr (dict arm)
func classMethodGetattro(o Object, name Object) (Object, error) {
	cm := o.(*ClassMethod)
	if name != nil && name.Type() == strType {
		key := attrNameStr(name)
		descr, _ := LookupDescriptor(o.Type(), key)
		if descr != nil {
			dt := descr.Type()
			if dt.DescrSet != nil && dt.DescrGet != nil {
				return dt.DescrGet(descr, o, o.Type())
			}
		}
		if cm.cmDict != nil {
			if v, _ := cm.cmDict.GetItem(name); v != nil {
				return v, nil
			}
		}
	}
	return GenericGetAttr(o, name)
}

func classMethodGetFunc(o Object) (Object, error) {
	return o.(*ClassMethod).cmCallable, nil
}

// classMethodGetIsAbstract reports whether the wrapped callable is
// flagged as abstract, mirroring _PyObject_IsAbstract: read
// __isabstractmethod__ off the callable and coerce to bool. A missing
// attribute (or one that evaluates falsy) reads as False.
//
// CPython: Objects/funcobject.c:1511 cm_get___isabstractmethod__
// CPython: Objects/object.c:1235 _PyObject_IsAbstract
func classMethodGetIsAbstract(o Object) (Object, error) {
	cm := o.(*ClassMethod)
	v, err := GetAttr(cm.cmCallable, NewStr("__isabstractmethod__"))
	if err != nil {
		return False(), nil
	}
	t, err := IsTruthy(v)
	if err != nil {
		return nil, err
	}
	return NewBool(t), nil
}

func classMethodGetDict(o Object) (Object, error) {
	cm := o.(*ClassMethod)
	if cm.cmDict == nil {
		cm.cmDict = NewDict()
	}
	return cm.cmDict, nil
}

func classMethodSetDict(o Object, v Object) error {
	cm := o.(*ClassMethod)
	if v == nil {
		cm.cmDict = nil
		return nil
	}
	d, ok := v.(*Dict)
	if !ok {
		return fmt.Errorf("TypeError: __dict__ must be set to a dict object")
	}
	cm.cmDict = d
	return nil
}

// descriptorGetWrappedAttribute reads name from the descriptor's own
// __dict__, falling back to the wrapped callable and caching the
// result on the descriptor. Mirrors CPython's helper for
// __annotations__ / __annotate__ on classmethod and staticmethod.
//
// CPython: Objects/funcobject.c:1337 descriptor_get_wrapped_attribute
func descriptorGetWrappedAttribute(wrapped Object, owner Object, name string) (Object, error) {
	dict, err := genericGetInstanceDict(owner)
	if err != nil {
		return nil, err
	}
	if v, _ := dict.GetItem(NewStr(name)); v != nil {
		return v, nil
	}
	v, err := GetAttr(wrapped, NewStr(name))
	if err != nil {
		return nil, err
	}
	if err := dict.SetItem(NewStr(name), v); err != nil {
		return nil, err
	}
	return v, nil
}

// descriptorSetWrappedAttribute writes name onto the descriptor's
// __dict__, or deletes it when value is nil. CPython raises an
// AttributeError ("'TYPE' object has no attribute 'NAME'") when the
// delete misses; mirror that surface.
//
// CPython: Objects/funcobject.c:1367 descriptor_set_wrapped_attribute
func descriptorSetWrappedAttribute(owner Object, name string, value Object, typeName string) error {
	dict, err := genericGetInstanceDict(owner)
	if err != nil {
		return err
	}
	if value == nil {
		if _, err := dict.GetItem(NewStr(name)); err != nil {
			return fmt.Errorf("AttributeError: '%s' object has no attribute '%s'", typeName, name)
		}
		return dict.DelItem(NewStr(name))
	}
	return dict.SetItem(NewStr(name), value)
}

// genericGetInstanceDict returns the per-instance __dict__ for
// objects that store it on a dedicated field. classmethod and
// staticmethod both keep theirs in cmDict / smDict; this helper
// centralises the lookup so descriptor_get_wrapped_attribute works
// for both.
//
// CPython: Objects/object.c PyObject_GenericGetDict
func genericGetInstanceDict(o Object) (*Dict, error) {
	switch v := o.(type) {
	case *ClassMethod:
		if v.cmDict == nil {
			v.cmDict = NewDict()
		}
		return v.cmDict, nil
	case *StaticMethod:
		if v.smDict == nil {
			v.smDict = NewDict()
		}
		return v.smDict, nil
	}
	return nil, fmt.Errorf("TypeError: '%s' object has no __dict__", o.Type().Name)
}

// functoolsWraps copies the wrapper attributes from src onto dst.
// Same set CPython hardcodes in functools_wraps: any missing source
// attribute is silently skipped so wrapping a builtin (which has no
// __module__) is not an error.
//
// CPython: Objects/funcobject.c:1316 functools_wraps
func functoolsWraps(dst *ClassMethod, src Object) {
	if src == nil {
		return
	}
	for _, attr := range [...]string{"__module__", "__name__", "__qualname__", "__doc__"} {
		v, err := GetAttr(src, NewStr(attr))
		if err != nil || v == nil {
			continue
		}
		if dst.cmDict == nil {
			dst.cmDict = NewDict()
		}
		_ = dst.cmDict.SetItem(NewStr(attr), v)
	}
}

// StaticMethod is the descriptor that strips the descriptor protocol
// off a function: __get__ just returns the wrapped callable, so
// calling it through an instance does not prepend self.
//
// CPython: Include/cpython/funcobject.h StaticMethod (analog)
type StaticMethod struct {
	Header
	smCallable Object
	smDict     *Dict // lazily allocated, mirrors staticmethod.sm_dict
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
