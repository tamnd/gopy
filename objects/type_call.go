// type.__call__: this is the slot CPython invokes when source like
// `MyClass(args)` runs. The `type` singleton's call splits two ways:
// type(x) returns the type of x, type(name, bases, dict) builds a new
// class. Calls on any other type allocate an instance and run
// __init__.
//
// The v0.10.1 cut covers the user-class path (NewInstance + __init__);
// built-in types still go through their dedicated constructor wrappers
// in builtins/, so wiring this slot does not change their behavior.
//
// CPython: Objects/typeobject.c:1748 type_call

package objects

import (
	"fmt"
	"strings"
)

func init() {
	typeType.Call = typeCall
	// Register type.__new__ so super().__new__() inside a metaclass
	// __new__ body can resolve it through the MRO.
	//
	// CPython: Objects/typeobject.c:4153 type_new
	SetTypeDescr(typeType, "__new__", NewBuiltinFunction("type.__new__", typeNewBuiltin))
	// Register type.__init__ so a metaclass that calls
	// `super().__init__(*args, **kwargs)` resolves through type rather
	// than tumbling through to object.__init__, which rejects extra args.
	// type_init is a near no-op: it only validates the call shape is
	// type(obj) or type(name, bases, ns).
	//
	// CPython: Objects/typeobject.c:4036 type_init
	SetTypeDescr(typeType, "__init__", NewMethodDescr(typeType, "__init__", typeInitDescr))
	// type.__prepare__(name, bases, /, **kwds) is the default classmethod
	// that returns a fresh empty dict. CPython exposes it via the
	// METH_CLASS row in type_methods so subclass-metaclasses that fall
	// back to super().__prepare__() actually have something to resolve.
	//
	// CPython: Objects/typeobject.c:6609 type_methods __prepare__
	// CPython: Objects/typeobject.c:6580 type_prepare
	SetTypeDescr(typeType, "__prepare__", NewClassMethod(NewBuiltinFunction("type.__prepare__",
		func(_ []Object, _ map[string]Object) (Object, error) {
			return NewDict(), nil
		})))
}

// typeInitDescr is the tp_init slot wrapper for type. Accepts 1 or 3
// positional args beyond self (type(x) or type(name, bases, ns)) and
// ignores keyword arguments, just like CPython.
//
// CPython: Objects/typeobject.c:4036 type_init
func typeInitDescr(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: descriptor '__init__' of 'type' object needs an argument")
	}
	rest := len(args) - 1
	if rest != 1 && rest != 3 {
		return nil, fmt.Errorf("TypeError: type.__init__() takes 1 or 3 arguments")
	}
	return None(), nil
}

// typeCall is the tp_call slot for `type` itself (and, by inheritance,
// every class). Behavior:
//
//   - When the receiver is the `type` singleton:
//
//   - type(x)               -> Py_TYPE(x)
//
//   - type(name, bases, ns) -> NewUserType(name, bases, ns)
//
//   - When the receiver is a user-defined class (IsUser==true): call
//     __new__ if present, otherwise allocate an Instance, then run
//     __init__ with the args.
//
//   - Built-in types reach the same slot but are not callable through
//     it: they are constructed via their named builtin (int(), list(),
//     ...) which routes through builtins/ctor.go.
//
// CPython: Objects/typeobject.c:1748 type_call
func typeCall(callable Object, args []Object, kwargs map[string]Object) (Object, error) {
	cls, ok := callable.(*Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: type.__call__ on non-type object")
	}

	if cls == typeType {
		return typeMetaCall(args, kwargs)
	}

	// Types that publish a tp_new slot: allocate via TpNew, then call
	// __init__ if the type has one registered as a descriptor. For most
	// built-in types TpNew handles everything itself and there is no
	// __init__ descriptor, so the check is a no-op.  For dict (and
	// user-defined subclasses of built-ins) TpNew is allocation-only and
	// __init__ does the population/initialisation work.
	//
	// CPython: Objects/typeobject.c:1748 type_call
	if cls.TpNew != nil {
		return typeCallViaTpNew(cls, args, kwargs)
	}
	// Some built-ins still expose construction through Call (super
	// landed before TpNew did); honor it as a fallback. Skip the
	// fallback for user classes, where cls.Call is the slotTpCall
	// dispatcher installed by fixupSlotDispatchers for instances and
	// would otherwise re-enter __call__ at construction time.
	if cls.Call != nil && !cls.IsUser {
		return cls.Call(callable, args, kwargs)
	}

	if !cls.IsUser {
		return nil, fmt.Errorf("TypeError: cannot create '%s' instances directly", cls.Name)
	}

	// If cls is a subtype of type (a metaclass), dispatch through __new__
	// to build a new *Type. This covers user-defined metaclasses like
	// ABCMeta that override type.__new__ to customize class creation.
	//
	// CPython: Objects/typeobject.c:1748 type_call (tp_new/tp_init path)
	if IsSubtype(cls, typeType) {
		return typeMetaclassCall(cls, args, kwargs)
	}

	if err := checkNotAbstract(cls); err != nil {
		return nil, err
	}

	inst := NewInstance(cls)
	if init, _ := LookupDescriptor(cls, "__init__"); init != nil {
		bound := bindDescr(init, inst, cls)
		if _, err := callBound(bound, args, kwargs); err != nil {
			return nil, err
		}
	}
	return inst, nil
}

// typeCallViaTpNew allocates through the type's tp_new slot, then
// runs __init__ when the type has one registered as a descriptor.
// CPython skips __init__ entirely when the value returned from tp_new
// is not an instance of the calling type (e.g. a metaclass __new__
// that returns an int); the result is passed back untouched.
//
// CPython: Objects/typeobject.c:2331 type_call (tp_new + tp_init path)
func typeCallViaTpNew(cls *Type, args []Object, kwargs map[string]Object) (Object, error) {
	inst, err := cls.TpNew(cls, args, kwargs)
	if err != nil {
		return nil, err
	}
	if !IsSubtype(inst.Type(), cls) {
		return inst, nil
	}
	actual := inst.Type()
	if init, _ := LookupDescriptor(actual, "__init__"); init != nil {
		bound := bindDescr(init, inst, actual)
		if _, err := callBound(bound, args, kwargs); err != nil {
			return nil, err
		}
	}
	return inst, nil
}

// checkNotAbstract refuses to instantiate a class whose
// __abstractmethods__ frozenset is non-empty. Mirrors the
// Py_TPFLAGS_IS_ABSTRACT branch of object_new.
//
// CPython: Objects/typeobject.c:3550 object_new
func checkNotAbstract(cls *Type) error {
	abs, _ := LookupDescriptor(cls, "__abstractmethods__")
	if abs == nil {
		return nil
	}
	s, ok := abs.(*Set)
	if !ok || s.Len() == 0 {
		return nil
	}
	names := make([]string, 0, s.Len())
	for _, o := range s.Items() {
		if u, ok := o.(*Unicode); ok {
			names = append(names, u.Value())
		}
	}
	word := "method"
	if len(names) != 1 {
		word = "methods"
	}
	return fmt.Errorf("TypeError: Can't instantiate abstract class %s without an implementation for abstract %s '%s'",
		cls.Name, word, strings.Join(names, ", "))
}

// typeMetaclassCall handles calling a user-defined metaclass (a subclass
// of type). It dispatches through the metaclass's __new__ to produce a
// new *Type, then calls __init__ on the result.
//
// CPython: Objects/typeobject.c:1748 type_call
func typeMetaclassCall(cls *Type, args []Object, kwargs map[string]Object) (Object, error) {
	// Prepend cls as the first arg for __new__(mcls, name, bases, ns).
	newArgs := make([]Object, len(args)+1)
	newArgs[0] = cls
	copy(newArgs[1:], args)

	var result Object
	newDescr, _ := LookupDescriptor(cls, "__new__")
	// Skip type.__new__ itself: we handle that path directly.
	if typeNewDescr, _ := LookupDescriptor(typeType, "__new__"); newDescr == typeNewDescr {
		newDescr = nil
	}
	if newDescr != nil {
		// __new__ is implicitly static: call it directly with (cls, *args)
		// rather than through the descriptor protocol (which would bind cls
		// as self and produce a double-prepend).
		//
		// CPython: Objects/typeobject.c:1748 type_call (tp_new call)
		var err error
		result, err = callBound(newDescr, newArgs, kwargs)
		if err != nil {
			return nil, err
		}
	} else {
		// No __new__ override: use type.__new__ directly.
		var err error
		result, err = typeNewBuiltin(newArgs, kwargs)
		if err != nil {
			return nil, err
		}
	}

	// Call __init__ only when the result is an instance of cls.
	// Mirrors CPython's Py_TYPE(self) is type check in type_call.
	//
	// CPython: Objects/typeobject.c:1800 type_call (__init__ guard)
	if resultType, ok := result.(*Type); ok && IsSubtype(resultType.Type(), cls) {
		if init, _ := LookupDescriptor(cls, "__init__"); init != nil {
			bound := bindDescr(init, result, cls)
			if _, err := callBound(bound, args, kwargs); err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}

// typeNewBuiltin implements type.__new__(mcls, name, bases, ns). Called
// when a metaclass __new__ body executes super().__new__(mcls, ...) and
// the super lookup resolves to type.__new__ in typeType's descriptor
// table. Creates a new user type with mcls as the metaclass.
//
// CPython: Objects/typeobject.c:4153 type_new
func typeNewBuiltin(args []Object, kwargs map[string]Object) (Object, error) {
	if len(args) < 4 {
		return nil, fmt.Errorf("TypeError: type.__new__() takes exactly 4 arguments (%d given)", len(args))
	}
	meta, ok := args[0].(*Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: type.__new__() argument 1 must be a type, not %s", typeNameOf(args[0]))
	}
	nameObj, ok := args[1].(*Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: type.__new__() argument 2 must be str, not %s", typeNameOf(args[1]))
	}
	basesT, ok := args[2].(*Tuple)
	if !ok {
		return nil, fmt.Errorf("TypeError: type.__new__() argument 3 must be tuple, not %s", typeNameOf(args[2]))
	}
	ns, ok := args[3].(*Dict)
	if !ok {
		return nil, fmt.Errorf("TypeError: type.__new__() argument 4 must be dict, not %s", typeNameOf(args[3]))
	}
	bases := make([]*Type, 0, basesT.Len())
	for i := 0; i < basesT.Len(); i++ {
		t, ok := basesT.Item(i).(*Type)
		if !ok {
			return nil, fmt.Errorf("TypeError: bases must contain types, got %s", typeNameOf(basesT.Item(i)))
		}
		bases = append(bases, t)
	}
	// NewUserTypeMeta stamps Py_TYPE(t) = meta before the namespace
	// copy and __set_name__ pass, so PEP 487 hooks that resolve
	// metaclass-defined methods via cls.<method> see the right
	// metatype.
	//
	// CPython: Objects/typeobject.c:4153 type_new (Py_TYPE(type) = metatype)
	t, err := NewUserTypeMetaE(nameObj.v, bases, ns, kwargs, meta)
	if err != nil {
		return nil, err
	}
	return t, nil
}

// typeMetaCall handles the two `type(...)` forms.
//
// CPython: Objects/typeobject.c:1817 type_call (the type==&PyType_Type branch)
func typeMetaCall(args []Object, kwargs map[string]Object) (Object, error) {
	if len(args) == 1 && len(kwargs) == 0 {
		return args[0].Type(), nil
	}
	if len(args) != 3 {
		return nil, fmt.Errorf("TypeError: type() takes 1 or 3 arguments")
	}
	nameObj, ok := args[0].(*Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: type() argument 1 must be str, not %s", typeNameOf(args[0]))
	}
	basesT, ok := args[1].(*Tuple)
	if !ok {
		return nil, fmt.Errorf("TypeError: type() argument 2 must be tuple, not %s", typeNameOf(args[1]))
	}
	ns, ok := args[2].(*Dict)
	if !ok {
		return nil, fmt.Errorf("TypeError: type() argument 3 must be dict, not %s", typeNameOf(args[2]))
	}
	bases := make([]*Type, 0, basesT.Len())
	for i := 0; i < basesT.Len(); i++ {
		t, ok := basesT.Item(i).(*Type)
		if !ok {
			return nil, fmt.Errorf("TypeError: type() bases must contain types, got %s", typeNameOf(basesT.Item(i)))
		}
		bases = append(bases, t)
	}
	// type(name, bases, ns, **kw) might be invoked directly while one of
	// the bases carries a richer metaclass (e.g. enum.EnumType). CPython
	// recalculates the winning metaclass from the bases and re-dispatches
	// to that metaclass's tp_new so the kwargs flow to the override that
	// knows how to consume them. Without this, kwargs like `boundary=`
	// fall through to object.__init_subclass__ and crash.
	//
	// CPython: Objects/typeobject.c:4728 type_new (winner = _PyType_CalculateMetaclass)
	winner, err := calculateMetaclass(typeType, bases)
	if err != nil {
		return nil, err
	}
	if winner != typeType {
		return typeMetaclassCall(winner, args, kwargs)
	}
	return NewUserTypeMetaE(nameObj.v, bases, ns, kwargs, nil)
}

// calculateMetaclass picks the most derived metaclass among metatype
// and the types of bases, returning a TypeError on metaclass conflict.
//
// CPython: Objects/typeobject.c:3921 _PyType_CalculateMetaclass
func calculateMetaclass(metatype *Type, bases []*Type) (*Type, error) {
	winner := metatype
	for _, b := range bases {
		bt := b.Type()
		if IsSubtype(winner, bt) {
			continue
		}
		if IsSubtype(bt, winner) {
			winner = bt
			continue
		}
		return nil, fmt.Errorf("TypeError: metaclass conflict: the metaclass of a derived class must be a (non-strict) subclass of the metaclasses of all its bases")
	}
	return winner, nil
}

// bindDescr applies the descriptor protocol to descr if its type
// implements DescrGet, otherwise returns descr unchanged.
func bindDescr(descr Object, owner Object, ownerType *Type) Object {
	dt := descr.Type()
	if dt.DescrGet == nil {
		return descr
	}
	v, err := dt.DescrGet(descr, owner, ownerType)
	if err != nil {
		return descr
	}
	return v
}

// callBound invokes a bound callable with positional args and kwargs.
func callBound(fn Object, args []Object, kwargs map[string]Object) (Object, error) {
	var kwd *Dict
	if len(kwargs) > 0 {
		kwd = NewDict()
		for k, v := range kwargs {
			if err := kwd.SetItem(NewStr(k), v); err != nil {
				return nil, err
			}
		}
	}
	return Call(fn, NewTuple(args), kwd)
}
