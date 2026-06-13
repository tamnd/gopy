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
	"sort"
	"strings"
)

func init() {
	// PyType_Type advertises Py_TPFLAGS_HAVE_VECTORCALL
	// (tp_vectorcall_offset = tp_vectorcall) so type(...) and class
	// instantiation take the fast path.
	//
	// CPython: Objects/typeobject.c type_ready sets HAVE_VECTORCALL
	typeType.TpFlags |= TpFlagHaveVectorcall
	typeType.Call = typeCall
	typeType.Vectorcall = typeVectorcall
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

	// User-defined metaclasses (subtypes of type with IsUser=true) must go
	// through typeMetaclassCall so ABCMeta.__new__ and similar overrides
	// are honored. The TpNew check below fires first because user
	// metaclasses inherit objectNew from objectType via the MRO (typeType
	// leaves TpNew nil), and objectNew would create an *Instance instead
	// of a *Type. CPython avoids this by always routing through type_new
	// for metaclass calls.
	//
	// CPython: Objects/typeobject.c:1748 type_call (metaclass path)
	if cls.IsUser && IsSubtype(cls, typeType) {
		return typeMetaclassCall(cls, args, kwargs)
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

	if err := checkNotAbstract(cls); err != nil {
		return nil, err
	}

	inst := NewInstance(cls)
	if init, _ := LookupDescriptor(cls, "__init__"); init != nil {
		bound := bindDescr(init, inst, cls)
		res, err := callBound(bound, args, kwargs)
		if bound != init {
			Decref(bound)
		}
		if err != nil {
			return nil, err
		}
		if err := checkInitResult(res); err != nil {
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
		res, err := callBound(bound, args, kwargs)
		if bound != init {
			Decref(bound)
		}
		if err != nil {
			return nil, err
		}
		if err := checkInitResult(res); err != nil {
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
	// CPython joins "', '".join(sorted(__abstractmethods__)) and wraps
	// the result in a single pair of quotes.
	//
	// CPython: Objects/typeobject.c:6857 object_new
	sort.Strings(names)
	word := "method"
	if len(names) != 1 {
		word = "methods"
	}
	return fmt.Errorf("TypeError: Can't instantiate abstract class %s without an implementation for abstract %s '%s'",
		cls.Name, word, strings.Join(names, "', '"))
}

// checkInitResult enforces slot_tp_init's rule that a Python-level
// __init__ must return None. A non-None return (e.g. `def __init__:
// return 10`) raises TypeError, matching CPython.
//
// CPython: Objects/typeobject.c:10493 slot_tp_init
func checkInitResult(res Object) error {
	if res == nil || IsNone(res) {
		return nil
	}
	return fmt.Errorf("TypeError: __init__() should return None, not '%.200s'", res.Type().Name)
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
			res, err := callBound(bound, args, kwargs)
			if err != nil {
				return nil, err
			}
			if err := checkInitResult(res); err != nil {
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
	// type.__new__ parses (name, bases, dict) off args; the metatype is
	// args[0] and is not counted. CPython's PyArg_ParseTuple("UO!O!")
	// rejects any other arity with "takes exactly 3 arguments (N given)",
	// where N excludes the metatype. The one-argument type(x) form never
	// reaches here: it is served at the call layer for the exact `type`.
	//
	// CPython: Objects/typeobject.c:4766 type_new (PyArg_ParseTuple)
	if len(args) != 4 {
		return nil, fmt.Errorf("TypeError: type.__new__() takes exactly 3 arguments (%d given)", len(args)-1)
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
		// CPython: Objects/abstract.c:2526 PyObject_Type — Py_INCREF before return.
		t := args[0].Type()
		Incref(t)
		return t, nil
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
			// A non-type base carrying __mro_entries__ is the class-statement
			// case (e.g. a typing.Generic alias). The 3-arg type() call does
			// not run MRO entry resolution, so CPython rejects it outright and
			// points the caller at types.new_class().
			//
			// CPython: Objects/typeobject.c:3640 type_new_get_bases
			entries, err := LookupAttrString(basesT.Item(i), "__mro_entries__")
			if err != nil {
				return nil, err
			}
			if entries != nil {
				return nil, fmt.Errorf("TypeError: type() doesn't support MRO entry resolution; use types.new_class()")
			}
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
	newType, err := NewUserTypeMetaE(nameObj.v, bases, ns, kwargs, nil)
	if err != nil {
		return nil, err
	}
	// Preserve a tuple subclass as the bases object (gh-132176): CPython
	// stores the passed tuple verbatim in tp_bases. We only stash it when
	// it is not the exact built-in tuple, since the plain case rebuilds
	// identically from Bases.
	if newType != nil && basesT.Type() != TupleType {
		Incref(basesT)
		newType.BasesObj = basesT
	}
	return newType, nil
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
// Routes through VectorcallDict (not Call) so the args slice is
// consumed in place. A NewTuple wrapper here would survive the call
// with refcount=1, stay in the GC tracked set, and pin its elements,
// blocking gc.collect from reaping user-class self-cycles.
//
// CPython: Objects/call.c:154 PyObject_VectorcallDict
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
	return VectorcallDict(fn, args, uint(len(args)), kwd)
}

// typeVectorcall is the PEP 590 vectorcall entry for type objects. It
// reconstructs a *Dict from kwnames (preserving original key objects like
// FakeStr that compare equal to parameter names via __eq__ but are not
// plain str instances) and dispatches through typeCallWithDict so those
// original keys reach __init__'s **kwargs dict without being converted to
// their Str() repr by dictToMap.
//
// CPython: Objects/typeobject.c:1748 type_call (the path that preserves
// kwdict identity when vectorcall is used from Python/ceval.c)
func typeVectorcall(callable Object, args []Object, nargsf uint, kwnames *Tuple) (Object, error) {
	npos := VectorcallNargs(nargsf)
	posArgs := args[:npos]
	var kwd *Dict
	if kwnames != nil && kwnames.Len() > 0 {
		nkw := kwnames.Len()
		kwd = NewDict()
		for i := range nkw {
			if err := kwd.SetItem(kwnames.Item(i), args[npos+i]); err != nil {
				return nil, err
			}
		}
	}
	return typeCallWithDict(callable, posArgs, kwd)
}

// typeCallWithDict mirrors typeCall but accepts a *Dict for kwargs so the
// original key objects (non-str types that Python-compare equal to parameter
// names) are preserved through to __init__'s **kwargs slot.
//
// CPython: Objects/typeobject.c:1748 type_call
func typeCallWithDict(callable Object, args []Object, kwargs *Dict) (Object, error) {
	cls, ok := callable.(*Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: type.__call__ on non-type object")
	}
	var kwmap map[string]Object
	if kwargs != nil && kwargs.Len() > 0 {
		var merr error
		kwmap, merr = dictToMap(kwargs)
		if merr != nil {
			return nil, merr
		}
	}
	if cls == typeType {
		return typeMetaCall(args, kwmap)
	}
	if cls.IsUser && IsSubtype(cls, typeType) {
		return typeMetaclassCallWithDict(cls, args, kwargs)
	}
	if cls.TpNew != nil {
		return typeCallViaTpNewWithDict(cls, args, kwargs)
	}
	if cls.Call != nil && !cls.IsUser {
		return cls.Call(callable, args, kwmap)
	}
	if !cls.IsUser {
		return nil, fmt.Errorf("TypeError: cannot create '%s' instances directly", cls.Name)
	}
	if err := checkNotAbstract(cls); err != nil {
		return nil, err
	}
	inst := NewInstance(cls)
	if init, _ := LookupDescriptor(cls, "__init__"); init != nil {
		bound := bindDescr(init, inst, cls)
		res, err := VectorcallDict(bound, args, uint(len(args)), kwargs)
		if bound != init {
			Decref(bound)
		}
		if err != nil {
			return nil, err
		}
		if err := checkInitResult(res); err != nil {
			return nil, err
		}
	}
	return inst, nil
}

// typeCallViaTpNewWithDict is typeCallViaTpNew with the kwargs preserved as
// a *Dict so __init__ receives original key objects.
//
// CPython: Objects/typeobject.c:2331 type_call (tp_new + tp_init path)
func typeCallViaTpNewWithDict(cls *Type, args []Object, kwargs *Dict) (Object, error) {
	var inst Object
	if cls.TpNewWithDict != nil {
		var err error
		inst, err = cls.TpNewWithDict(cls, args, kwargs)
		if err != nil {
			return nil, err
		}
	} else {
		var kwmap map[string]Object
		if kwargs != nil && kwargs.Len() > 0 {
			var merr error
			kwmap, merr = dictToMap(kwargs)
			if merr != nil {
				return nil, merr
			}
		}
		var err error
		inst, err = cls.TpNew(cls, args, kwmap)
		if err != nil {
			return nil, err
		}
	}
	if inst == nil {
		return nil, nil
	}
	if !IsSubtype(inst.Type(), cls) {
		return inst, nil
	}
	actual := inst.Type()
	if init, _ := LookupDescriptor(actual, "__init__"); init != nil {
		bound := bindDescr(init, inst, actual)
		res, callErr := VectorcallDict(bound, args, uint(len(args)), kwargs)
		if bound != init {
			Decref(bound)
		}
		if callErr != nil {
			return nil, callErr
		}
		if err := checkInitResult(res); err != nil {
			return nil, err
		}
	}
	return inst, nil
}

// typeMetaclassCallWithDict is typeMetaclassCall with the kwargs preserved
// as a *Dict for the __init__ call.
//
// CPython: Objects/typeobject.c:1748 type_call
func typeMetaclassCallWithDict(cls *Type, args []Object, kwargs *Dict) (Object, error) {
	var kwmap map[string]Object
	if kwargs != nil && kwargs.Len() > 0 {
		var merr error
		kwmap, merr = dictToMap(kwargs)
		if merr != nil {
			return nil, merr
		}
	}
	newArgs := make([]Object, len(args)+1)
	newArgs[0] = cls
	copy(newArgs[1:], args)

	var result Object
	newDescr, _ := LookupDescriptor(cls, "__new__")
	if typeNewDescr, _ := LookupDescriptor(typeType, "__new__"); newDescr == typeNewDescr {
		newDescr = nil
	}
	if newDescr != nil {
		var err error
		result, err = callBound(newDescr, newArgs, kwmap)
		if err != nil {
			return nil, err
		}
	} else {
		var err error
		result, err = typeNewBuiltin(newArgs, kwmap)
		if err != nil {
			return nil, err
		}
	}
	if resultType, ok := result.(*Type); ok && IsSubtype(resultType.Type(), cls) {
		if init, _ := LookupDescriptor(cls, "__init__"); init != nil {
			bound := bindDescr(init, result, cls)
			res, err := VectorcallDict(bound, args, uint(len(args)), kwargs)
			if err != nil {
				return nil, err
			}
			if err := checkInitResult(res); err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}
