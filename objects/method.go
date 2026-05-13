// Bound methods, instancemethod, classmethod, and staticmethod. Each
// one is a descriptor whose __get__ rewires the call to put the right
// argument in front:
//
//   method:         f.__get__(obj, type) returns self (already bound)
//   instancemethod: f.__get__(obj, type) returns bound(func, obj)
//   classmethod:    f.__get__(obj, type) returns bound(func, type)
//   staticmethod:   f.__get__(obj, type) returns func
//
// CPython: Objects/classobject.c:140 method_getattro
// CPython: Objects/classobject.c:373 instancemethod_getattro

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
	BoundMethodType.Setattro = GenericSetAttr
	// method_descr_get just returns self with an extra reference:
	// looking up a bound method on a class returns the binding as-is
	// rather than re-wrapping it.
	//
	// CPython: Objects/classobject.c:292 method_descr_get
	BoundMethodType.DescrGet = boundMethodDescrGet
	// method_richcompare: == / != compare (im_func, im_self) pairs.
	// All other ops return NotImplemented so that comparing a bound
	// method against, say, an int gives the regular TypeError instead
	// of a spurious False.
	//
	// CPython: Objects/classobject.c:210 method_richcompare
	BoundMethodType.RichCmp = boundMethodRichCompare
	// method_hash: pointer hash of im_self XOR hash(im_func). The
	// pointer half lets every distinct binding hash differently even
	// when the function and self compare equal in some weird way.
	//
	// CPython: Objects/classobject.c:266 method_hash
	BoundMethodType.Hash = boundMethodHash
	// method.__new__: clinic-generated wrapper that validates the
	// (function, instance) pair before delegating to PyMethod_New.
	//
	// CPython: Objects/classobject.c:180 method_new_impl
	BoundMethodType.TpNew = boundMethodTpNew
	// method_memberlist: __func__ and __self__ are exposed as readonly
	// member descriptors with CPython's docstrings.
	//
	// CPython: Objects/classobject.c:114 method_memberlist
	SetTypeDescr(BoundMethodType, "__func__", NewGetSetDescr("__func__",
		func(o Object) (Object, error) { return o.(*BoundMethod).imFunc, nil },
		nil))
	SetTypeDescr(BoundMethodType, "__self__", NewGetSetDescr("__self__",
		func(o Object) (Object, error) { return o.(*BoundMethod).imSelf, nil },
		nil))
	// method_getset: __doc__ proxies to the wrapped function so a
	// bound method shows its target's docstring.
	//
	// CPython: Objects/classobject.c:134 method_getset
	SetTypeDescr(BoundMethodType, "__doc__", NewGetSetDescr("__doc__",
		boundMethodGetDoc, nil))
	// method_methods: __reduce__ returns (getattr, (self, funcname))
	// so pickle can rebuild the bound method by re-fetching the
	// attribute off the instance.
	//
	// CPython: Objects/classobject.c:103 method_methods
	SetTypeDescr(BoundMethodType, "__reduce__",
		NewMethodDescr(BoundMethodType, "__reduce__", boundMethodReduceMethod))
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

// boundMethodRepr matches CPython's
// `<bound method QUALNAME_OR_NAME of REPR_OF_SELF>`. The qualname
// preference (over name) is what makes nested class methods print as
// `Outer.Inner.f` instead of just `f`.
//
// CPython: Objects/classobject.c:280 method_repr
func boundMethodRepr(o Object) (string, error) {
	m := o.(*BoundMethod)
	name := boundMethodFuncName(m.imFunc)
	selfRepr, err := Repr(m.imSelf)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("<bound method %s of %s>", name, selfRepr), nil
}

// boundMethodFuncName fetches __qualname__ off the wrapped function,
// falling back to __name__, falling back to "?" the way CPython's
// method_repr does. AttributeError is swallowed because builtins
// frequently omit one or both.
//
// CPython: Objects/classobject.c:280 method_repr
func boundMethodFuncName(fn Object) string {
	if fn == nil {
		return "?"
	}
	if v, err := GetAttr(fn, NewStr("__qualname__")); err == nil {
		if s, ok := v.(*Unicode); ok {
			return s.Value()
		}
	}
	if v, err := GetAttr(fn, NewStr("__name__")); err == nil {
		if s, ok := v.(*Unicode); ok {
			return s.Value()
		}
	}
	return "?"
}

// boundMethodRichCompare ports method_richcompare. Only == and !=
// are meaningful; everything else returns NotImplemented so the
// regular protocol fallback kicks in. Equality holds when both the
// wrapped function and the bound self compare equal pairwise.
//
// CPython: Objects/classobject.c:206 method_richcompare
func boundMethodRichCompare(a, b Object, op CompareOp) (Object, error) {
	if op != CompareEQ && op != CompareNE {
		return NotImplemented(), nil
	}
	ma, ok := a.(*BoundMethod)
	if !ok {
		return NotImplemented(), nil
	}
	mb, ok := b.(*BoundMethod)
	if !ok {
		return NotImplemented(), nil
	}
	eq, err := RichCmpBool(ma.imFunc, mb.imFunc, CompareEQ)
	if err != nil {
		return nil, err
	}
	if eq {
		switch {
		case ma.imSelf == nil || mb.imSelf == nil:
			eq = ma.imSelf == mb.imSelf
		default:
			eq, err = RichCmpBool(ma.imSelf, mb.imSelf, CompareEQ)
			if err != nil {
				return nil, err
			}
		}
	}
	if op == CompareNE {
		eq = !eq
	}
	return NewBool(eq), nil
}

// boundMethodHash mirrors method_hash. Identity hash of im_self
// (because two methods bound off different instances should land in
// different buckets even when those instances compare equal) XORed
// with the function's own hash.
//
// CPython: Objects/classobject.c:230 method_hash
func boundMethodHash(o Object) (int64, error) {
	m := o.(*BoundMethod)
	x, err := identityHash(m.imSelf)
	if err != nil {
		return 0, err
	}
	y, err := Hash(m.imFunc)
	if err != nil {
		return 0, err
	}
	x ^= y
	if x == -1 {
		x = -2
	}
	return x, nil
}

// boundMethodDescrGet returns the bound method as-is, mirroring the
// `Py_NewRef(meth)` in CPython's method_descr_get. Looking up a bound
// method as a class attribute on the wrapped self's type does not
// re-bind: the descriptor protocol short-circuits.
//
// CPython: Objects/classobject.c:292 method_descr_get
func boundMethodDescrGet(descr Object, _ Object, _ *Type) (Object, error) {
	return descr, nil
}

// boundMethodGetDoc proxies to the wrapped function's __doc__. The
// docstring of a bound method is always the wrapped function's, never
// the method type's, so introspecting `bm.__doc__` mirrors what
// `bm.__func__.__doc__` would return.
//
// CPython: Objects/classobject.c:127 method_get_doc
func boundMethodGetDoc(o Object) (Object, error) {
	m := o.(*BoundMethod)
	return GetAttr(m.imFunc, NewStr("__doc__"))
}

// boundMethodTpNew validates the (function, instance) pair and
// delegates to NewBoundMethod. Mirrors method_new_impl: the function
// has to be callable and the instance has to be neither nil nor None,
// otherwise CPython raises TypeError.
//
// CPython: Objects/classobject.c:180 method_new_impl
func boundMethodTpNew(_ *Type, args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: method expected 2 arguments, got %d", len(args))
	}
	fn, instance := args[0], args[1]
	if fn == nil || fn.Type().Call == nil && fn.Type().Vectorcall == nil {
		return nil, fmt.Errorf("TypeError: first argument must be callable")
	}
	if instance == nil || instance == None() {
		return nil, fmt.Errorf("TypeError: instance must not be None")
	}
	return NewBoundMethod(fn, instance), nil
}

// boundMethodReduceMethod is method.__reduce__(): returns
// (builtin getattr, (self, funcname)) so pickle can reconstitute the
// binding by re-resolving the attribute on the instance.
//
// CPython: Objects/classobject.c:90 method___reduce___impl
func boundMethodReduceMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __reduce__() takes no arguments (%d given)", len(args)-1)
	}
	m, ok := args[0].(*BoundMethod)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__reduce__' requires a 'method' object")
	}
	funcname, err := GetAttr(m.imFunc, NewStr("__name__"))
	if err != nil {
		return nil, err
	}
	return NewTuple([]Object{
		builtinGetattrCallable,
		NewTuple([]Object{m.imSelf, funcname}),
	}), nil
}

// builtinGetattrCallable is the reducer's first element: a callable
// pickle can dump and load that, when called with (self, name),
// returns whatever the attribute lookup yields. Mirrors
// _PyEval_GetBuiltin(&_Py_ID(getattr)) on the CPython side. Kept here
// instead of in builtins/ so the objects package has no import cycle.
//
// CPython: Python/bltinmodule.c:1228 builtin_getattr
var builtinGetattrCallable = NewBuiltinFunction("getattr",
	func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) < 2 || len(args) > 3 {
			return nil, fmt.Errorf("TypeError: getattr expected 2 or 3 arguments, got %d", len(args))
		}
		v, err := GetAttr(args[0], args[1])
		if err == nil {
			return v, nil
		}
		if len(args) == 3 {
			return args[2], nil
		}
		return nil, err
	})

// boundMethodVectorcall prepends self to the positional args, then
// hands control to the wrapped function. Mirrors the inline logic in
// CPython's method_vectorcall.
//
// CPython: Objects/classobject.c:46 method_vectorcall
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
	addDescriptorSlotWrappers(ClassMethodType)
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
		//nolint:nilerr // missing __isabstractmethod__ is False, per _PyObject_IsAbstract
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
// centralizes the lookup so descriptor_get_wrapped_attribute works
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
// __module__) is not an error. dst must be an object whose dict is
// reachable through genericGetInstanceDict.
//
// CPython: Objects/funcobject.c:1316 functools_wraps
func functoolsWraps(dst Object, src Object) {
	if src == nil {
		return
	}
	dict, err := genericGetInstanceDict(dst)
	if err != nil {
		return
	}
	for _, attr := range [...]string{"__module__", "__name__", "__qualname__", "__doc__"} {
		v, err := GetAttr(src, NewStr(attr))
		if err != nil || v == nil {
			continue
		}
		_ = dict.SetItem(NewStr(attr), v)
	}
}

// StaticMethod is the descriptor that strips the descriptor protocol
// off a function: __get__ just returns the wrapped callable, so
// calling it through an instance does not prepend self.
//
// CPython: Objects/funcobject.c:1666 staticmethod struct
type StaticMethod struct {
	Header
	smCallable Object
	smDict     *Dict // lazily allocated, mirrors staticmethod.sm_dict
}

// StaticMethodType is the type singleton for staticmethod.
//
// CPython: Objects/funcobject.c:1842 PyStaticMethod_Type
var StaticMethodType = NewType("staticmethod", []*Type{objectType})

func init() {
	StaticMethodType.Repr = staticMethodRepr
	StaticMethodType.Str = staticMethodRepr
	StaticMethodType.DescrGet = staticMethodDescrGet
	StaticMethodType.TpTraverse = staticMethodTraverse
	StaticMethodType.Getattro = staticMethodGetattro
	// sm_call: calling a staticmethod directly forwards to the
	// wrapped callable. CPython 3.10 added this so the @staticmethod
	// descriptor itself is callable, not just the bound result.
	//
	// CPython: Objects/funcobject.c:1749 sm_call
	StaticMethodType.Call = staticMethodCall

	// sm_memberlist + sm_getsetlist + sm_methodlist, mirroring the
	// classmethod surface.
	//
	// CPython: Objects/funcobject.c:1755 sm_memberlist
	// CPython: Objects/funcobject.c:1801 sm_getsetlist
	// CPython: Objects/funcobject.c:1809 sm_methodlist
	SetTypeDescr(StaticMethodType, "__func__", NewGetSetDescr("__func__", staticMethodGetFunc, nil))
	SetTypeDescr(StaticMethodType, "__wrapped__", NewGetSetDescr("__wrapped__", staticMethodGetFunc, nil))
	SetTypeDescr(StaticMethodType, "__isabstractmethod__", NewGetSetDescr(
		"__isabstractmethod__", staticMethodGetIsAbstract, nil))
	SetTypeDescr(StaticMethodType, "__dict__", NewGetSetDescr(
		"__dict__", staticMethodGetDict, staticMethodSetDict))
	SetTypeDescr(StaticMethodType, "__annotations__", NewGetSetDescr(
		"__annotations__",
		func(o Object) (Object, error) {
			return descriptorGetWrappedAttribute(o.(*StaticMethod).smCallable, o, "__annotations__")
		},
		func(o Object, v Object) error {
			return descriptorSetWrappedAttribute(o, "__annotations__", v, "staticmethod")
		}))
	SetTypeDescr(StaticMethodType, "__annotate__", NewGetSetDescr(
		"__annotate__",
		func(o Object) (Object, error) {
			return descriptorGetWrappedAttribute(o.(*StaticMethod).smCallable, o, "__annotate__")
		},
		func(o Object, v Object) error {
			return descriptorSetWrappedAttribute(o, "__annotate__", v, "staticmethod")
		}))
	bindClassGetitem(StaticMethodType)
	addDescriptorSlotWrappers(StaticMethodType)
}

// staticMethodTraverse visits the wrapped callable and instance dict.
//
// CPython: Objects/funcobject.c:1687 sm_traverse
func staticMethodTraverse(o Object, visit Visitor) error {
	sm := o.(*StaticMethod)
	if sm.smCallable != nil {
		if err := visit(sm.smCallable); err != nil {
			return err
		}
	}
	if sm.smDict != nil {
		return visit(sm.smDict)
	}
	return nil
}

// NewStaticMethod wraps fn so attribute access on an instance returns
// the callable directly without binding self. Like classmethod, the
// constructor also runs functools_wraps so __module__ / __name__ /
// __qualname__ / __doc__ propagate from the wrapped callable.
//
// CPython: Objects/funcobject.c:1731 sm_init
func NewStaticMethod(fn Object) *StaticMethod {
	sm := &StaticMethod{smCallable: fn}
	sm.init(StaticMethodType)
	functoolsWraps(sm, fn)
	return sm
}

// Func returns the wrapped callable.
func (sm *StaticMethod) Func() Object { return sm.smCallable }

// staticMethodRepr matches CPython's <staticmethod(REPR_OF_CALLABLE)>.
//
// CPython: Objects/funcobject.c:1815 sm_repr
func staticMethodRepr(o Object) (string, error) {
	sm := o.(*StaticMethod)
	inner, err := Repr(sm.smCallable)
	if err != nil {
		return "", err
	}
	return "<staticmethod(" + inner + ")>", nil
}

// staticMethodDescrGet just hands back the wrapped callable.
//
// CPython: Objects/funcobject.c:1705 sm_descr_get
func staticMethodDescrGet(descr Object, _ Object, _ *Type) (Object, error) {
	return descr.(*StaticMethod).smCallable, nil
}

// staticMethodCall forwards a direct call on a staticmethod object to
// the wrapped callable. CPython 3.10 made staticmethod instances
// callable so `staticmethod(fn)(args)` works without going through
// __get__ first.
//
// CPython: Objects/funcobject.c:1749 sm_call
func staticMethodCall(callable Object, args []Object, kwargs map[string]Object) (Object, error) {
	sm := callable.(*StaticMethod)
	if sm.smCallable == nil {
		return nil, fmt.Errorf("RuntimeError: uninitialized staticmethod object")
	}
	tup := NewTuple(args)
	var kw *Dict
	if len(kwargs) > 0 {
		kw = NewDict()
		for k, v := range kwargs {
			if err := kw.SetItem(NewStr(k), v); err != nil {
				return nil, err
			}
		}
	}
	return Call(sm.smCallable, tup, kw)
}

// staticMethodGetattro mirrors classMethodGetattro: layer the
// per-instance __dict__ (populated by functools_wraps) on top of
// GenericGetAttr so __name__, __module__, etc. flow through without a
// dedicated getset for each.
//
// CPython: Objects/object.c:1932 PyObject_GenericGetAttr (dict arm)
func staticMethodGetattro(o Object, name Object) (Object, error) {
	sm := o.(*StaticMethod)
	if name != nil && name.Type() == strType {
		key := attrNameStr(name)
		descr, _ := LookupDescriptor(o.Type(), key)
		if descr != nil {
			dt := descr.Type()
			if dt.DescrSet != nil && dt.DescrGet != nil {
				return dt.DescrGet(descr, o, o.Type())
			}
		}
		if sm.smDict != nil {
			if v, _ := sm.smDict.GetItem(name); v != nil {
				return v, nil
			}
		}
	}
	return GenericGetAttr(o, name)
}

func staticMethodGetFunc(o Object) (Object, error) {
	return o.(*StaticMethod).smCallable, nil
}

// staticMethodGetIsAbstract mirrors classMethodGetIsAbstract but
// reads off the staticmethod's wrapped callable.
//
// CPython: Objects/funcobject.c:1762 sm_get___isabstractmethod__
func staticMethodGetIsAbstract(o Object) (Object, error) {
	sm := o.(*StaticMethod)
	v, err := GetAttr(sm.smCallable, NewStr("__isabstractmethod__"))
	if err != nil {
		//nolint:nilerr // missing __isabstractmethod__ is False, per _PyObject_IsAbstract
		return False(), nil
	}
	t, err := IsTruthy(v)
	if err != nil {
		return nil, err
	}
	return NewBool(t), nil
}

func staticMethodGetDict(o Object) (Object, error) {
	sm := o.(*StaticMethod)
	if sm.smDict == nil {
		sm.smDict = NewDict()
	}
	return sm.smDict, nil
}

func staticMethodSetDict(o Object, v Object) error {
	sm := o.(*StaticMethod)
	if v == nil {
		sm.smDict = nil
		return nil
	}
	d, ok := v.(*Dict)
	if !ok {
		return fmt.Errorf("TypeError: __dict__ must be set to a dict object")
	}
	sm.smDict = d
	return nil
}

// InstanceMethod is a callable wrapper around a function where, when
// fetched off an instance, __get__ binds the function into a regular
// PyMethod. CPython exposes it as `types.InstanceMethodType`; tools
// like `inspect` use it as a lightweight descriptor that defers to the
// normal function machinery once bound.
//
// CPython: Include/cpython/classobject.h:30 PyInstanceMethodObject
type InstanceMethod struct {
	Header
	function Object
}

// InstanceMethodType is the type singleton for instancemethod.
//
// CPython: Objects/classobject.c:501 PyInstanceMethod_Type
var InstanceMethodType = NewType("instancemethod", []*Type{objectType})

func init() {
	InstanceMethodType.Repr = instanceMethodRepr
	InstanceMethodType.Str = instanceMethodRepr
	InstanceMethodType.Call = instanceMethodCall
	InstanceMethodType.Getattro = instanceMethodGetattro
	InstanceMethodType.Setattro = GenericSetAttr
	InstanceMethodType.TpTraverse = instanceMethodTraverse
	InstanceMethodType.RichCmp = instanceMethodRichCompare
	InstanceMethodType.DescrGet = instanceMethodDescrGet
	InstanceMethodType.TpNew = instanceMethodTpNew

	// instancemethod_memberlist: __func__ exposes the wrapped
	// callable as a readonly member.
	//
	// CPython: Objects/classobject.c:354 instancemethod_memberlist
	SetTypeDescr(InstanceMethodType, "__func__", NewGetSetDescr("__func__",
		func(o Object) (Object, error) { return o.(*InstanceMethod).function, nil },
		nil))
	// instancemethod_getset: __doc__ proxies to the wrapped callable.
	//
	// CPython: Objects/classobject.c:367 instancemethod_getset
	SetTypeDescr(InstanceMethodType, "__doc__", NewGetSetDescr("__doc__",
		instanceMethodGetDoc, nil))
}

// NewInstanceMethod wraps fn so attribute access through an instance
// binds the callable into a regular method. Used by `types` and the
// abc machinery.
//
// CPython: Objects/classobject.c:332 PyInstanceMethod_New
func NewInstanceMethod(fn Object) *InstanceMethod {
	im := &InstanceMethod{function: fn}
	im.init(InstanceMethodType)
	return im
}

// Func returns the wrapped callable.
//
// CPython: Objects/classobject.c:342 PyInstanceMethod_Function
func (im *InstanceMethod) Func() Object { return im.function }

// instanceMethodRepr matches CPython's `<instancemethod NAME at PTR>`.
// CPython falls back to "?" when the wrapped callable has no __name__,
// mirroring instancemethod_repr.
//
// CPython: Objects/classobject.c:453 instancemethod_repr
func instanceMethodRepr(o Object) (string, error) {
	im := o.(*InstanceMethod)
	name := boundMethodFuncName(im.function)
	return fmt.Sprintf("<instancemethod %s at %p>", name, im), nil
}

// instanceMethodCall forwards a direct call to the wrapped callable.
// Calling an instancemethod that has not been bound first is identical
// to calling its underlying function.
//
// CPython: Objects/classobject.c:412 instancemethod_call
func instanceMethodCall(callable Object, args []Object, kwargs map[string]Object) (Object, error) {
	im := callable.(*InstanceMethod)
	if im.function == nil {
		return nil, fmt.Errorf("RuntimeError: uninitialized instancemethod object")
	}
	tup := NewTuple(args)
	var kw *Dict
	if len(kwargs) > 0 {
		kw = NewDict()
		for k, v := range kwargs {
			if err := kw.SetItem(NewStr(k), v); err != nil {
				return nil, err
			}
		}
	}
	return Call(im.function, tup, kw)
}

// instanceMethodGetattro looks for name on the instancemethod type
// first (so __func__ / __doc__ resolve through their getset
// descriptors), then falls through to GetAttr on the wrapped callable.
//
// CPython: Objects/classobject.c:373 instancemethod_getattro
func instanceMethodGetattro(o Object, name Object) (Object, error) {
	im := o.(*InstanceMethod)
	descr, _ := LookupDescriptor(o.Type(), attrNameStr(name))
	if descr != nil {
		if dg := descr.Type().DescrGet; dg != nil {
			return dg(descr, o, o.Type())
		}
		return descr, nil
	}
	return GetAttr(im.function, name)
}

// instanceMethodTraverse visits the wrapped callable.
//
// CPython: Objects/classobject.c:406 instancemethod_traverse
func instanceMethodTraverse(o Object, visit Visitor) error {
	im := o.(*InstanceMethod)
	if im.function != nil {
		return visit(im.function)
	}
	return nil
}

// instanceMethodRichCompare ports instancemethod_richcompare. Only ==
// and != are meaningful; CPython compares wrapped callables and folds
// the result into a bool.
//
// CPython: Objects/classobject.c:428 instancemethod_richcompare
func instanceMethodRichCompare(a, b Object, op CompareOp) (Object, error) {
	if op != CompareEQ && op != CompareNE {
		return NotImplemented(), nil
	}
	ima, ok := a.(*InstanceMethod)
	if !ok {
		return NotImplemented(), nil
	}
	imb, ok := b.(*InstanceMethod)
	if !ok {
		return NotImplemented(), nil
	}
	eq, err := RichCmpBool(ima.function, imb.function, CompareEQ)
	if err != nil {
		return nil, err
	}
	if op == CompareNE {
		eq = !eq
	}
	return NewBool(eq), nil
}

// instanceMethodDescrGet binds the wrapped callable to obj. With
// obj=nil (class-level access) the instancemethod hands back the
// underlying callable. Otherwise it returns a freshly minted bound
// method, exactly like CPython's instancemethod_descr_get.
//
// CPython: Objects/classobject.c:418 instancemethod_descr_get
func instanceMethodDescrGet(descr Object, owner Object, _ *Type) (Object, error) {
	im := descr.(*InstanceMethod)
	if owner == nil {
		return im.function, nil
	}
	return NewBoundMethod(im.function, owner), nil
}

// instanceMethodGetDoc proxies __doc__ to the wrapped callable.
//
// CPython: Objects/classobject.c:360 instancemethod_get_doc
func instanceMethodGetDoc(o Object) (Object, error) {
	im := o.(*InstanceMethod)
	return GetAttr(im.function, NewStr("__doc__"))
}

// instanceMethodTpNew is the clinic-generated __new__: validate the
// callable then construct the wrapper.
//
// CPython: Objects/classobject.c:488 instancemethod_new_impl
func instanceMethodTpNew(_ *Type, args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: instancemethod expected 1 argument, got %d", len(args))
	}
	fn := args[0]
	if fn == nil || fn.Type().Call == nil && fn.Type().Vectorcall == nil {
		return nil, fmt.Errorf("TypeError: first argument must be callable")
	}
	return NewInstanceMethod(fn), nil
}
