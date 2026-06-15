// Root object type. Mirrors PyBaseObject_Type and its method /
// getset tables. Every entry in CPython's object_methods (line
// 7950 of Objects/typeobject.c) and object_getsets (line 7254)
// lands here as a type-level descriptor, plus the C-level slot
// wrappers that add_operators auto-installs for tp_repr / tp_str /
// tp_hash / tp_richcompare / tp_getattro / tp_setattro.
//
// CPython: Objects/typeobject.c:7970 PyBaseObject_Type

package objects

import (
	"fmt"
	"reflect"
	"sort"
)

// objectType is the root type that every other type inherits from.
//
// CPython: Objects/typeobject.c:7970 PyBaseObject_Type
var objectType = func() *Type {
	t := &Type{Name: "object", TpFlags: TpFlagImmutable | TpFlagBasetype}
	t.init(typeType)
	t.Bases = []*Type{}
	t.MRO = []*Type{t}
	return t
}()

// ObjectType returns the root `object` type singleton.
//
// CPython: Objects/typeobject.c:7970 PyBaseObject_Type
func ObjectType() *Type { return objectType }

func init() {
	// C-level slots. add_operators auto-derives the matching Python
	// dunder descriptors below from these.
	objectType.TpNew = objectNew
	objectType.Repr = objectRepr
	objectType.Str = objectStr
	objectType.Hash = identityHash

	// object_methods table.
	//
	// CPython: Objects/typeobject.c:7950 object_methods
	SetTypeDescr(objectType, "__new__", NewBuiltinFunction("object.__new__", objectNewBuiltin))
	// object.__init__ stands in for slot_tp_init's wrapper_descriptor, so
	// its bound form is a method-wrapper (gopy's BoundMethod), matching
	// type(object().__init__).
	//
	// CPython: Objects/typeobject.c slotdefs tp_init (slot_tp_init)
	SetTypeDescr(objectType, "__init__", NewMethodDescr(objectType, "__init__", objectInitDescr).AsSlotWrapper())
	// METH_O / METH_NOARGS rows carry their clinic flag so
	// methodDescrCheckArity formats the arity TypeError through
	// _PyObject_FunctionStr, yielding "object.__reduce__() takes no
	// arguments (N given)" etc. for every type that inherits these.
	//
	// CPython: Objects/clinic/typeobject.c.h object_methods flags
	SetTypeDescr(objectType, "__reduce_ex__", NewMethodDescrConv(objectType, "__reduce_ex__", MethO, objectReduceExDescr))
	SetTypeDescr(objectType, "__reduce__", NewMethodDescrConv(objectType, "__reduce__", MethNoArgs, objectReduceDescr))
	SetTypeDescr(objectType, "__getstate__", NewMethodDescrConv(objectType, "__getstate__", MethNoArgs, objectGetstateDescr))
	SetTypeDescr(objectType, "__subclasshook__", NewClassMethod(
		NewBuiltinFunction("__subclasshook__", objectSubclasshook)))
	SetTypeDescr(objectType, "__init_subclass__", NewClassMethod(
		NewBuiltinFunction("__init_subclass__", objectInitSubclass)))
	SetTypeDescr(objectType, "__format__", NewMethodDescrConv(objectType, "__format__", MethO, objectFormatDescr))
	SetTypeDescr(objectType, "__sizeof__", NewMethodDescrConv(objectType, "__sizeof__", MethNoArgs, objectSizeofDescr))
	SetTypeDescr(objectType, "__dir__", NewMethodDescrConv(objectType, "__dir__", MethNoArgs, objectDirDescr))

	// type_ready_set_dict fills __doc__ from tp_doc for every static type.
	// object carries a real docstring, so it lands in object.__dict__ and,
	// through the MRO, in dir() of every type that does not override it.
	//
	// CPython: Objects/typeobject.c:8551 type_dict_set_doc (object_doc)
	SetTypeDescr(objectType, "__doc__", NewStr("The base class of the class hierarchy.\n\nWhen called, it accepts no arguments and returns a new featureless\ninstance that has no instance attributes and cannot be given any.\n"))

	// object_getsets table.
	//
	// CPython: Objects/typeobject.c:7254 object_getsets
	SetTypeDescr(objectType, "__class__", NewGetSetDescr("__class__", objectGetClass, objectSetClass))

	// Slot wrappers added by add_operators for the C-level slots that
	// object owns (tp_repr, tp_str, tp_hash, tp_getattro, tp_setattro,
	// tp_richcompare).
	//
	// CPython: Objects/typeobject.c add_operators (slot wrapper for
	// each non-NULL entry in slotdefs whose slot is tp_repr/tp_str/...)
	SetTypeDescr(objectType, "__repr__", NewMethodDescr(objectType, "__repr__", objectReprDescr).AsSlotWrapper())
	SetTypeDescr(objectType, "__str__", NewMethodDescr(objectType, "__str__", objectStrDescr).AsSlotWrapper())
	SetTypeDescr(objectType, "__hash__", NewMethodDescr(objectType, "__hash__", objectHashDescr).AsSlotWrapper())
	SetTypeDescr(objectType, "__getattribute__", NewMethodDescr(objectType, "__getattribute__", objectGetattributeDescr).AsSlotWrapper())
	SetTypeDescr(objectType, "__setattr__", NewMethodDescr(objectType, "__setattr__", objectSetattrDescr).AsSlotWrapper())
	SetTypeDescr(objectType, "__delattr__", NewMethodDescr(objectType, "__delattr__", objectDelattrDescr).AsSlotWrapper())

	// tp_richcompare slot wrappers: one per operator. object's
	// richcompare returns NotImplemented for non-EQ/NE and identity
	// for EQ/NE.
	//
	// CPython: Objects/typeobject.c:6950 object_richcompare
	SetTypeDescr(objectType, "__eq__", NewMethodDescr(objectType, "__eq__", richCompareDescr(CompareEQ)).AsSlotWrapper())
	SetTypeDescr(objectType, "__ne__", NewMethodDescr(objectType, "__ne__", richCompareDescr(CompareNE)).AsSlotWrapper())
	SetTypeDescr(objectType, "__lt__", NewMethodDescr(objectType, "__lt__", richCompareDescr(CompareLT)).AsSlotWrapper())
	SetTypeDescr(objectType, "__le__", NewMethodDescr(objectType, "__le__", richCompareDescr(CompareLE)).AsSlotWrapper())
	SetTypeDescr(objectType, "__gt__", NewMethodDescr(objectType, "__gt__", richCompareDescr(CompareGT)).AsSlotWrapper())
	SetTypeDescr(objectType, "__ge__", NewMethodDescr(objectType, "__ge__", richCompareDescr(CompareGE)).AsSlotWrapper())

	// __dict__ getset is installed by subtype_dict in CPython only on the
	// type that first introduces a managed dict (tp_dictoffset != 0), not
	// on object: plain object() has no instance dict, so dir(object) and
	// dir(list) must not list __dict__. installInstanceDictDescr stamps it
	// onto heap types from configureManagedDict instead; subclasses inherit
	// the descriptor through the MRO.
	//
	// CPython: Objects/typeobject.c subtype_dict / subtype_setdict
	// (type_new_descriptors only adds __dict__ when add_dict is set)

	// All static built-in types are immortal: CPython stamps them with
	// _Py_IMMORTAL_REFCNT in _PyStaticType_InitBuiltin so tp_dealloc
	// never fires. objectType is the only *Type whose initializer does
	// not go through NewType (which calls MakeImmortal), so we stamp it
	// here once all descriptors are registered.
	//
	// CPython: Objects/typeobject.c:352 _PyStaticType_InitBuiltin
	objectType.MakeImmortal()
}

// objectSetDict implements object.__dict__ set for HasDict-bearing
// instances. The new value must be a dictionary; a HasDict instance
// rebinds its attribute store to it, anything else raises the same
// "not writable" AttributeError CPython does for a type with no
// tp_dictoffset.
//
// CPython: Objects/typeobject.c:3795 subtype_setdict /
// Objects/object.c:_PyObject_SetDict
func objectSetDict(o Object, value Object) error {
	v, ok := o.(*Instance)
	if !ok {
		// Built-in subclass instances (Exception, numeric, dict subclasses)
		// carry their managed __dict__ behind AttrDictHolder rather than the
		// *Instance shape. Rebind it in place when the holder supports it.
		//
		// CPython: Objects/typeobject.c:3795 subtype_setdict
		if s, ok := o.(AttrDictSetter); ok && o.Type().HasDict {
			if value == nil {
				s.SetAttrDict(NewDict())
				return nil
			}
			d, ok := value.(*Dict)
			if !ok {
				return fmt.Errorf("TypeError: __dict__ must be set to a dictionary, not a '%s'", value.Type().Name)
			}
			s.SetAttrDict(d)
			return nil
		}
		return fmt.Errorf("AttributeError: attribute '__dict__' of '%s' objects is not writable", o.Type().Name)
	}
	if !v.Type().HasDict {
		return fmt.Errorf("AttributeError: attribute '__dict__' of '%s' objects is not writable", o.Type().Name)
	}
	// A nil value is `del obj.__dict__`: CPython's subtype_setdict passes
	// NULL to _PyObject_SetManagedDict, clearing the managed dict so the
	// next attribute access lazily rebuilds an empty one.
	if value == nil {
		// Detaching the managed dict drops the inline values, so the
		// instance is no longer in the WITH_VALUES shape.
		//
		// CPython: Objects/dictobject.c:7540 _PyObject_SetManagedDict (NULL)
		v.dict = NewDict()
		v.inlineValid = false
		return nil
	}
	d, ok := value.(*Dict)
	if !ok {
		return fmt.Errorf("TypeError: __dict__ must be set to a dictionary, not a '%s'", value.Type().Name)
	}
	// Binding an explicit dict materializes a combined dict in place of
	// the inline values.
	v.dict = d
	v.inlineValid = false
	return nil
}

// excessArgs reports whether args / kwargs carry any non-self payload.
//
// CPython: Objects/typeobject.c:6807 excess_args
func excessArgs(args []Object, kwargs map[string]Object) bool {
	return len(args) > 0 || len(kwargs) > 0
}

// fnPtr returns the program-counter address of a Go function value
// for slot-inheritance comparison. CPython uses raw C function
// pointer equality to detect "did the subclass override this slot".
func fnPtr(fn any) uintptr {
	if fn == nil {
		return 0
	}
	v := reflect.ValueOf(fn)
	if v.Kind() != reflect.Func {
		return 0
	}
	return v.Pointer()
}

// objectNew is object's tp_new slot. Allocates a fresh instance of
// cls; rejects extra arguments when both tp_new and tp_init come
// from object.
//
// CPython: Objects/typeobject.c:6835 object_new
func objectNew(cls *Type, args []Object, kwargs map[string]Object) (Object, error) {
	if excessArgs(args, kwargs) {
		if fnPtr(cls.TpNew) != fnPtr(objectNew) {
			return nil, fmt.Errorf("TypeError: object.__new__() takes exactly one argument (the type to instantiate)")
		}
		if initInheritedFromObject(cls) {
			return nil, fmt.Errorf("TypeError: %s() takes no arguments", cls.Name)
		}
	}
	// Abstract instantiation guard: refuse if __abstractmethods__ is
	// non-empty. gopy does not yet carry the Py_TPFLAGS_IS_ABSTRACT
	// bit, so the lookup-based version is the equivalent path.
	//
	// CPython: Objects/typeobject.c:6854 object_new (the
	// Py_TPFLAGS_IS_ABSTRACT branch)
	if err := checkNotAbstract(cls); err != nil {
		return nil, err
	}
	return NewInstance(cls), nil
}

// objectInit is object's tp_init slot: accepts the receiver only.
// CPython only rejects extra args when both tp_new and tp_init are
// inherited from object; otherwise the subclass owns the contract.
//
// CPython: Objects/typeobject.c:6814 object_init
func objectInit(self Object, args []Object, kwargs map[string]Object) error {
	if excessArgs(args, kwargs) {
		cls := self.Type()
		newInherited := fnPtr(cls.TpNew) == fnPtr(objectNew)
		initInherited := initInheritedFromObject(cls)
		if !initInherited {
			return fmt.Errorf("TypeError: object.__init__() takes exactly one argument (the instance to initialize)")
		}
		if newInherited {
			return fmt.Errorf("TypeError: %s.__init__() takes exactly one argument (the instance to initialize)", cls.Name)
		}
	}
	return nil
}

// initInheritedFromObject reports whether cls inherits __init__
// straight from object (i.e. has not overridden it). The MRO walk
// stops at the first __init__ descriptor.
func initInheritedFromObject(cls *Type) bool {
	descr, _ := LookupDescriptor(cls, "__init__")
	if descr == nil {
		return true
	}
	// The default object.__init__ is the very descriptor we install
	// in init() and store under typeDescrTable[objectType]. Compare
	// identity: any override produces a different MethodDescr instance.
	stored, _ := LookupDescriptor(objectType, "__init__")
	return descr == stored
}

// objectRepr formats an instance as "<module.qualname object at 0xADDR>".
//
// CPython: Objects/typeobject.c:6911 object_repr
func objectRepr(o Object) (string, error) {
	t := o.Type()
	mod := typeModule(t)
	name := typeQualname(t)
	addr := reflect.ValueOf(o).Pointer()
	if mod != "" && mod != "builtins" {
		return fmt.Sprintf("<%s.%s object at %#x>", mod, name, addr), nil
	}
	return fmt.Sprintf("<%s object at %#x>", t.Name, addr), nil
}

// objectStr falls back to objectRepr when the type does not
// override tp_str.
//
// CPython: Objects/typeobject.c:6938 object_str
func objectStr(o Object) (string, error) {
	t := o.Type()
	if t.Repr != nil {
		return t.Repr(o)
	}
	return objectRepr(o)
}

// typeModule returns the type's __module__ string, or "" if absent.
func typeModule(t *Type) string {
	descr, _ := LookupDescriptor(t, "__module__")
	if u, ok := descr.(*Unicode); ok {
		return u.v
	}
	return ""
}

// typeQualname returns the type's __qualname__ if set, falling back
// to tp_name.
func typeQualname(t *Type) string {
	descr, _ := LookupDescriptor(t, "__qualname__")
	if u, ok := descr.(*Unicode); ok {
		return u.v
	}
	return t.Name
}

// objectNewBuiltin is the Python-level wrapper for object.tp_new
// that add_tp_new_wrapper installs on objectType. The descriptor is
// bound to object, so the call always dispatches through object_new
// with the supplied subtype, not through subtype.tp_new (which is the
// caller's responsibility to choose).
//
// CPython: Objects/typeobject.c:9952 tp_new_wrapper
func objectNewBuiltin(args []Object, kwargs map[string]Object) (Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: object.__new__(): not enough arguments")
	}
	cls, ok := args[0].(*Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: object.__new__(X): X is not a type object (%s)", typeNameOf(args[0]))
	}
	// Walk to the most-derived static (non-heap) base. If that base
	// disallows instantiation its tp_new is NULL; CPython then refuses
	// object.__new__(cls). gopy models Py_TPFLAGS_DISALLOW_INSTANTIATION
	// by leaving TpNew / TpNewWithDict / Call all unset on the static
	// type (e.g. unicodedata.UCD, _hashlib.HMAC).
	//
	// CPython: Objects/typeobject.c:9843 tp_new_wrapper (staticbase walk)
	var staticbase *Type
	for _, b := range cls.MRO {
		if !b.IsUser {
			staticbase = b
			break
		}
	}
	if staticbase != nil && staticbase != objectType &&
		staticbase.TpNew == nil && staticbase.TpNewWithDict == nil && staticbase.Call == nil {
		return nil, fmt.Errorf("TypeError: cannot create '%s' instances", cls.Name)
	}
	return objectNew(cls, args[1:], kwargs)
}

// objectInitDescr is the slot wrapper for tp_init.
func objectInitDescr(args []Object, kwargs map[string]Object) (Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: descriptor '__init__' of 'object' object needs an argument")
	}
	if err := objectInit(args[0], args[1:], kwargs); err != nil {
		return nil, err
	}
	return None(), nil
}

// objectReprDescr is the slot wrapper for object.__repr__. It must
// call into objectRepr directly: routing through Repr() would
// re-enter slotTpRepr, which looks up __repr__ via the MRO and lands
// right back here, blowing the stack. CPython's object_repr is
// likewise a direct PyUnicode_FromFormat.
//
// CPython: Objects/typeobject.c:6911 object_repr
func objectReprDescr(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: expected 1 argument, got %d", len(args))
	}
	s, err := objectRepr(args[0])
	if err != nil {
		return nil, err
	}
	return NewStr(s), nil
}

// objectStrDescr is the slot wrapper for object.__str__. It calls
// tp_repr to match CPython's object_str behavior. For user-defined
// types, it calls ReprObject to preserve any str subclass returned by
// __repr__.
//
// CPython: Objects/typeobject.c:6938 object_str
func objectStrDescr(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: expected 1 argument, got %d", len(args))
	}
	o := args[0]
	t := o.Type()
	// For user-defined types, preserve any str subclass returned by __repr__.
	if t.IsUser {
		return ReprObject(o)
	}
	// For built-in types, call the repr slot and wrap normally.
	if t.Repr != nil {
		s, err := t.Repr(o)
		if err != nil {
			return nil, err
		}
		return NewStr(s), nil
	}
	s, err := objectRepr(o)
	if err != nil {
		return nil, err
	}
	return NewStr(s), nil
}

// objectHashDescr is the slot wrapper for tp_hash. Goes straight to
// the identity hash so the descriptor cannot loop back through Hash()
// when it is installed as the inherited __hash__ on a user metaclass.
//
// CPython: Objects/typeobject.c:6986 object___hash__
func objectHashDescr(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: expected 1 argument, got %d", len(args))
	}
	h, err := identityHash(args[0])
	if err != nil {
		return nil, err
	}
	return NewInt(h), nil
}

// checkNumArgs validates the user-supplied argument count for a slot
// wrapper. args includes the bound receiver at index 0, so the
// caller-visible count is len(args)-1; n is the number of arguments the
// wrapper accepts after self. The message omits the function name,
// exactly like CPython's check_num_args (the wrapper layer prepends
// nothing).
//
// CPython: Objects/typeobject.c:8847 check_num_args
func checkNumArgs(args []Object, n int) error {
	got := len(args) - 1
	if got == n {
		return nil
	}
	return fmt.Errorf("TypeError: expected %d argument%s, got %d", n, plural(n), got)
}

// objectGetattributeDescr is the slot wrapper for tp_getattro.
func objectGetattributeDescr(args []Object, _ map[string]Object) (Object, error) {
	if err := checkNumArgs(args, 1); err != nil {
		return nil, err
	}
	return GenericGetAttr(args[0], args[1])
}

// objectSetattrDescr is the slot wrapper for tp_setattro (set branch).
func objectSetattrDescr(args []Object, _ map[string]Object) (Object, error) {
	if err := checkNumArgs(args, 2); err != nil {
		return nil, err
	}
	if err := hackcheck(args[0], GenericSetAttr, "__setattr__"); err != nil {
		return nil, err
	}
	if err := GenericSetAttr(args[0], args[1], args[2]); err != nil {
		return nil, err
	}
	return None(), nil
}

// objectDelattrDescr is the slot wrapper for tp_setattro (delete branch).
func objectDelattrDescr(args []Object, _ map[string]Object) (Object, error) {
	if err := checkNumArgs(args, 1); err != nil {
		return nil, err
	}
	if err := hackcheck(args[0], GenericSetAttr, "__delattr__"); err != nil {
		return nil, err
	}
	if err := GenericSetAttr(args[0], args[1], nil); err != nil {
		return nil, err
	}
	return None(), nil
}

// hackcheck rejects object.__setattr__ / __delattr__ applied directly to a
// type to bypass its metatype's setattro (the Carlo Verre hack). It walks the
// metatype's MRO to find the type that defined the live setattro slot, then
// confirms fn is the slot that would be reached without jumping over an
// intermediate C-level override.
//
// CPython: Objects/typeobject.c:9513 hackcheck_unlocked
func hackcheck(self Object, fn func(o, name, value Object) error, what string) error {
	meta, ok := self.(*Type)
	if !ok {
		return nil
	}
	mt := meta.Type()
	if mt == nil || len(mt.MRO) == 0 {
		// Probably ok not to check the call in this case.
		return nil
	}
	// Find the (base) type that defined the metatype's slot function.
	defining := mt
	for i := len(mt.MRO) - 1; i >= 0; i-- {
		base := mt.MRO[i]
		if fnPtr(base.Setattro) == fnPtr(slotTpSetattroHook) {
			// Ignore Python classes: they never define a C-level setattro.
			continue
		}
		if fnPtr(base.Setattro) == fnPtr(mt.Setattro) {
			defining = base
			break
		}
	}
	// Reject calls that jump over intermediate C-level overrides.
	for base := defining; base != nil; base = primaryBase(base) {
		if fnPtr(base.Setattro) == fnPtr(fn) {
			// fn is the right slot function to call.
			return nil
		}
		if fnPtr(base.Setattro) != fnPtr(slotTpSetattroHook) {
			// base is not a Python class and overrides fn; its setattro
			// should be called instead.
			return fmt.Errorf("TypeError: can't apply this %s to %s object", what, mt.Name)
		}
	}
	return nil
}

// primaryBase returns t's first base (CPython tp_base), or nil for object.
func primaryBase(t *Type) *Type {
	if len(t.Bases) == 0 {
		return nil
	}
	return t.Bases[0]
}

// richCompareDescr returns the slot wrapper for the named rich
// comparison operator. EQ compares by identity (returns NotImplemented
// for non-identical so the reflected operator gets a turn); NE delegates
// to EQ and negates. Ordering ops return NotImplemented.
//
// CPython: Objects/typeobject.c:6950 object_richcompare
func richCompareDescr(op CompareOp) func(args []Object, kwargs map[string]Object) (Object, error) {
	return func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("TypeError: expected 2 arguments, got %d", len(args))
		}
		switch op {
		case CompareEQ:
			if args[0] == args[1] {
				return True(), nil
			}
			return notImplemented(), nil
		case CompareNE:
			// Call tp_richcompare directly, not PyObject_RichCompare, so
			// that reflected __eq__ is never invoked from inside object.__ne__.
			// CPython: Objects/typeobject.c:6963 object_richcompare NE
			if args[0] == args[1] {
				return False(), nil
			}
			rcmp := args[0].Type().RichCmp
			if rcmp == nil {
				return notImplemented(), nil
			}
			eq, err := rcmp(args[0], args[1], CompareEQ)
			if err != nil {
				return nil, err
			}
			if IsNotImplemented(eq) {
				return notImplemented(), nil
			}
			b, berr := IsTruthy(eq)
			if berr != nil {
				return nil, berr
			}
			return NewBool(!b), nil
		default:
			return notImplemented(), nil
		}
	}
}

// BindRichCmpDescriptors installs type-specific __lt__/__le__/__gt__/__ge__
// slot wrappers on t. CPython does this via slot_tp_richcompare for every
// type whose tp_richcompare overrides object's; the wrappers are different
// objects from object.__lt__ etc. so that functools.total_ordering (which
// uses `getattr(cls, op) is not getattr(object, op)`) can detect
// inheritance-through-subclassing correctly.
//
// CPython: Objects/typeobject.c:7094 slot_tp_richcompare wrapper installation
func BindRichCmpDescriptors(t *Type) {
	for _, pair := range []struct {
		name string
		op   CompareOp
	}{
		{"__lt__", CompareLT},
		{"__le__", CompareLE},
		{"__gt__", CompareGT},
		{"__ge__", CompareGE},
		{"__eq__", CompareEQ},
		{"__ne__", CompareNE},
	} {
		op := pair.op
		SetTypeDescr(t, pair.name, NewMethodDescr(t, pair.name,
			func(args []Object, _ map[string]Object) (Object, error) {
				if len(args) != 2 {
					return nil, fmt.Errorf("TypeError: expected 2 arguments, got %d", len(args))
				}
				rcmp := args[0].Type().RichCmp
				if rcmp == nil {
					return notImplemented(), nil
				}
				return rcmp(args[0], args[1], op)
			},
		).AsSlotWrapper())
	}
}

// objectFormatDescr is object.__format__(self, format_spec). Empty
// format_spec returns str(self); a non-empty spec raises TypeError
// (the default formatter does not understand format codes).
//
// CPython: Objects/typeobject.c:7858 object___format___impl
func objectFormatDescr(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __format__() takes exactly one argument (%d given)", len(args)-1)
	}
	spec, ok := args[1].(*Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: __format__() argument 1 must be str, not %s", typeNameOf(args[1]))
	}
	if spec.v != "" {
		return nil, fmt.Errorf("TypeError: unsupported format string passed to %s.__format__", args[0].Type().Name)
	}
	return StrObject(args[0])
}

// objectSizeofDescr returns the basic byte footprint of self. gopy
// does not track tp_basicsize the way CPython does (instance layout
// is decided by the Go runtime), so the return value is a synthetic
// pointer-size constant rather than CPython's exact bytes. The
// descriptor exists so pickle / introspection paths that call
// __sizeof__ never raise AttributeError.
//
// CPython: Objects/typeobject.c:7879 object___sizeof___impl
func objectSizeofDescr(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __sizeof__() takes no arguments (%d given)", len(args)-1)
	}
	return NewInt(int64(reflect.TypeOf(args[0]).Size())), nil
}

// objectDirDescr returns the merged list of names visible on self:
// keys of self.__dict__ plus every descriptor reachable through the
// type's MRO. CPython does the merge via merge_class_dict but ends
// up with the same set.
//
// CPython: Objects/typeobject.c:7906 object___dir___impl
func objectDirDescr(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __dir__() takes no arguments (%d given)", len(args)-1)
	}
	self := args[0]
	names := map[string]struct{}{}
	// Instance __dict__ keys. A __dict__ that is not a real dict is
	// treated as empty, matching object___dir___impl. The optional-lookup
	// form mirrors _PyObject_LookupAttr: a __slots__ object (e.g. a typing
	// _SpecialForm) has no __dict__ and its __getattr__ raises
	// AttributeError, which object___dir___impl swallows rather than
	// leaving pending on the thread state.
	if d, err := LookupAttr(self, NewStr("__dict__")); err == nil {
		if dict, ok := d.(*Dict); ok {
			for _, k := range dict.Keys() {
				if u, ok := k.(*Unicode); ok {
					names[u.v] = struct{}{}
				}
			}
		}
	}
	// Merge in attrs reachable from __class__. The class is fetched with
	// getattr, so the "__class__ in __slots__ but unset" trick makes the
	// lookup raise AttributeError and the class names are skipped: dir()
	// then reports only the instance dict.
	//
	// CPython: Objects/typeobject.c:7906 object___dir___impl
	if cls, err := LookupAttr(self, NewStr("__class__")); err == nil {
		if t, ok := cls.(*Type); ok {
			for _, base := range t.MRO {
				for _, n := range descriptorNames(base) {
					names[n] = struct{}{}
				}
			}
		}
	}
	out := make([]string, 0, len(names))
	for n := range names {
		out = append(out, n)
	}
	sort.Strings(out)
	items := make([]Object, len(out))
	for i, n := range out {
		items[i] = NewStr(n)
	}
	return NewList(items), nil
}

// descriptorNames returns the names typeDescrTable holds for t.
func descriptorNames(t *Type) []string {
	descrs, ok := typeDescrTable[t]
	if !ok {
		return nil
	}
	out := make([]string, 0, len(descrs))
	for n := range descrs {
		out = append(out, n)
	}
	return out
}

// objectGetstateDescr returns the picklable state of self. The full
// CPython implementation looks up __slotnames__ via copyreg and
// merges slot values; copyreg is not yet ported in gopy, so this
// stops at the simple-case branch (return self.__dict__ when
// non-empty, else None).
//
// CPython: Objects/typeobject.c:7496 object___getstate___impl
// CPython: Objects/typeobject.c:7335 object_getstate_default
func objectGetstateDescr(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __getstate__() takes no arguments (%d given)", len(args)-1)
	}
	// Optional-lookup form (_PyObject_LookupAttr): a __slots__ object has no
	// __dict__ and its __getattr__ raises AttributeError, which
	// object_getstate_default swallows instead of leaving it pending.
	if d, err := LookupAttr(args[0], NewStr("__dict__")); err == nil {
		if dict, ok := d.(*Dict); ok && dict.Len() > 0 {
			return dict, nil
		}
	}
	return None(), nil
}

// objectReduceDescr is object.__reduce__(self): forwards to
// __reduce_ex__(2). Mirrors CPython's object___reduce___impl which
// hands off to _common_reduce(self, 0); _reduce_ex_ takes the same
// path so the two methods stay in lockstep.
//
// CPython: Objects/typeobject.c:7771 object___reduce___impl
func objectReduceDescr(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __reduce__() takes no arguments (%d given)", len(args)-1)
	}
	return commonReduce(args[0], 0)
}

// objectReduceExDescr is object.__reduce_ex__(self, protocol). The
// CPython implementation checks whether __reduce__ has been
// overridden on the type (vs inherited from object) and dispatches
// to it; otherwise falls into _common_reduce. gopy ports the
// override-detection prologue and the full reducer pipeline through
// commonReduce / reduceNewobj.
//
// CPython: Objects/typeobject.c:7787 object___reduce_ex___impl
func objectReduceExDescr(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __reduce_ex__() takes exactly one argument (%d given)", len(args)-1)
	}
	self := args[0]
	proto, ok := args[1].(*Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: __reduce_ex__() argument 1 must be int, not %s", args[1].Type().Name)
	}
	protoVal, fits := proto.Int64()
	if !fits {
		return nil, fmt.Errorf("OverflowError: __reduce_ex__() protocol out of range")
	}
	// Detect __reduce__ override: compare the __reduce__ descriptor on
	// type(self) against the one on objectType.
	// CPython: Objects/typeobject.c:8062 object_reduce_ex_impl
	clsReduce, _ := LookupDescriptor(self.Type(), "__reduce__")
	baseReduce, _ := LookupDescriptor(objectType, "__reduce__")
	if clsReduce != nil && clsReduce != baseReduce {
		fn, err := GetAttr(self, NewStr("__reduce__"))
		if err != nil {
			return nil, err
		}
		if err := EnterRecursiveCall("while reducing an object"); err != nil {
			return nil, err
		}
		result, callErr := Call(fn, NewTuple(nil), nil)
		LeaveRecursiveCall()
		return result, callErr
	}
	return commonReduce(self, int(protoVal))
}

// objectSubclasshook is object.__subclasshook__(cls, subclass). The
// default returns NotImplemented; abc.ABCMeta uses this hook to
// customize issubclass().
//
// CPython: Objects/typeobject.c:7821 object_subclasshook
func objectSubclasshook(_ []Object, _ map[string]Object) (Object, error) {
	return notImplemented(), nil
}

// objectInitSubclass is object.__init_subclass__(cls). No-op
// accepting no extra arguments by default; subclasses override to
// accept keyword arguments.
//
// CPython: Objects/typeobject.c:7835 object_init_subclass
func objectInitSubclass(args []Object, kwargs map[string]Object) (Object, error) {
	if len(args) > 1 {
		return nil, fmt.Errorf("TypeError: object.__init_subclass__() takes no arguments (%d given)", len(args)-1)
	}
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: object.__init_subclass__() takes no keyword arguments")
	}
	return None(), nil
}

// objectGetClass implements object.__class__ get. CPython routes
// through PyObject_GenericGetAttr's special-case for __class__.
//
// CPython: Objects/typeobject.c:7000 object_get_class
func objectGetClass(o Object) (Object, error) {
	t := o.Type()
	Incref(t)
	return t, nil
}

// objectSetClass implements object.__class__ set. It mirrors
// object_set_class_world_stopped: the swap is allowed for HEAPTYPE ->
// HEAPTYPE, or between two ModuleType subtypes (the allowlist for mutable
// module objects), provided neither side is an immutable type. The two
// layouts must then be compatible. The retag writes ob_type on the shared
// Header, so it works for any object representation, not only *Instance.
//
// CPython: Objects/typeobject.c:7108 object_set_class_world_stopped
func objectSetClass(o Object, value Object) error {
	if value == nil {
		return fmt.Errorf("TypeError: can't delete __class__ attribute")
	}
	newType, ok := value.(*Type)
	if !ok {
		return fmt.Errorf("TypeError: __class__ must be set to a class, not '%s' object", value.Type().Name)
	}
	oldType := o.Type()
	// HEAPTYPE -> HEAPTYPE or ModuleType subtype -> ModuleType subtype.
	// Immutable (statically interned) types are excluded so code cannot
	// repaint the type of a shared singleton such as the int 1.
	//
	// CPython: Objects/typeobject.c:7161 object_set_class_world_stopped
	bothModule := IsSubtype(newType, ModuleType) && IsSubtype(oldType, ModuleType)
	if !bothModule && (newType.TpFlags&TpFlagImmutable != 0 || oldType.TpFlags&TpFlagImmutable != 0) {
		return fmt.Errorf("TypeError: __class__ assignment only supported for mutable types or ModuleType subclasses")
	}
	if !compatibleForAssignment(oldType, newType) {
		return fmt.Errorf("TypeError: __class__ assignment: '%s' object layout differs from '%s'", newType.Name, oldType.Name)
	}
	o.Hdr().typ = newType
	return nil
}

// compatibleForAssignment reports whether an instance may have its
// __class__ switched from oldto to newto. It walks each type up to the
// most-derived ancestor that still changed its instance layout, then
// accepts the swap when those two ancestors are the same type or share a
// base and added the same slots. This guards the member-descriptor
// offsets so a reassigned instance keeps reading its slots correctly.
//
// CPython: Objects/typeobject.c:7155 compatible_for_assignment
func compatibleForAssignment(oldto, newto *Type) bool {
	newbase := newto
	for compatibleWithTpBase(newbase) {
		b, _ := bestBase(newbase.Bases)
		if b == nil {
			break
		}
		newbase = b
	}
	oldbase := oldto
	for compatibleWithTpBase(oldbase) {
		b, _ := bestBase(oldbase.Bases)
		if b == nil {
			break
		}
		oldbase = b
	}
	if newbase == oldbase {
		return true
	}
	nb, _ := bestBase(newbase.Bases)
	ob, _ := bestBase(oldbase.Bases)
	return nb == ob && sameSlotsAdded(newbase, oldbase)
}

// compatibleWithTpBase reports whether child lays its instances out
// identically to its base: it adds no named slots and introduces no new
// managed dict or weakref storage. Mirrors the tp_basicsize /
// tp_dictoffset / tp_weaklistoffset equality test.
//
// CPython: Objects/typeobject.c:7136 compatible_with_tp_base
func compatibleWithTpBase(child *Type) bool {
	parent, _ := bestBase(child.Bases)
	if parent == nil {
		return false
	}
	// A built-in such as dict or list differs from object only in its
	// tp_basicsize, which the slot/dict/weakref comparison alone misses;
	// shapeDiffers catches that so dict does not collapse to object during
	// the layout walk. CPython compares tp_basicsize/tp_itemsize directly.
	//
	// CPython: Objects/typeobject.c:7136 compatible_with_tp_base
	return !shapeDiffers(child, parent) &&
		len(child.Slots) == 0 &&
		child.HasDict == parent.HasDict &&
		child.HasWeakref == parent.HasWeakref
}

// sameSlotsAdded reports whether two types that share a base contribute
// the same instance-layout extension: identical (already sorted) slot
// names plus matching managed-dict and weakref additions.
//
// CPython: Objects/typeobject.c:7100 same_slots_added
func sameSlotsAdded(a, b *Type) bool {
	// Two built-in (non-heap) types never share an instance layout even when
	// neither adds named slots: their differing tp_basicsize is not modeled
	// here, so list and dict must read as incompatible. CPython bails the same
	// way by requiring both operands carry Py_TPFLAGS_HEAPTYPE.
	//
	// CPython: Objects/typeobject.c:7113 same_slots_added
	if !a.IsUser || !b.IsUser {
		return false
	}
	if len(a.Slots) != len(b.Slots) {
		return false
	}
	for i := range a.Slots {
		if a.Slots[i] != b.Slots[i] {
			return false
		}
	}
	return a.HasDict == b.HasDict && a.HasWeakref == b.HasWeakref
}

// objectGetDict implements object.__dict__ get for HasDict-bearing
// instances. Mirrors subtype_dict installed by type_new when
// tp_dictoffset is non-zero.
//
// CPython: Objects/typeobject.c subtype_dict
// installInstanceDictDescr stamps the __dict__ getset onto t. CPython's
// type_new_descriptors adds it only to the type that first introduces a
// managed dict; subclasses inherit it through the MRO. Skipped if t (or a
// base, via the SetTypeDescr-on-self check at the call site) already
// carries one, so the descriptor lands on exactly one class per chain.
//
// CPython: Objects/typeobject.c subtype_dict (added when add_dict is set)
func installInstanceDictDescr(t *Type) {
	SetTypeDescr(t, "__dict__", NewGetSetDescr("__dict__", objectGetDict, objectSetDict))
}

// InstallInstanceDictDescr is the exported entry to installInstanceDictDescr
// for built-in types defined outside this package (BaseException) that carry
// a tp_dictoffset and so must expose the read/write __dict__ getset. Without
// it `exc.__dict__ = d` would be stored as a plain attribute named
// "__dict__" rather than rebinding the managed dict.
//
// CPython: Objects/typeobject.c subtype_dict
func InstallInstanceDictDescr(t *Type) { installInstanceDictDescr(t) }

// installInstanceWeakrefDescr stamps the read-only __weakref__ getset on
// the class that first introduces weak-reference support, mirroring the
// subtype_getsets row CPython's type_new adds when may_add_weak fires.
//
// CPython: Objects/typeobject.c:3847 subtype_getsets_weakref_only
func installInstanceWeakrefDescr(t *Type) {
	SetTypeDescr(t, "__weakref__", NewGetSetDescr("__weakref__", objectGetWeakref, nil))
}

// objectGetWeakref backs the __weakref__ getset. It returns the head of
// the referent's weakref list (the first weak reference), or None when no
// weak reference exists yet. The descriptor is installed only on types
// whose instances carry weakref support, so a missing list reads as None
// rather than raising.
//
// CPython: Objects/typeobject.c:3818 subtype_getweakref
func objectGetWeakref(o Object) (Object, error) {
	if !o.Type().HasWeakref {
		return nil, fmt.Errorf("AttributeError: '%s' object has no attribute '__weakref__'", o.Type().Name)
	}
	h := o.Hdr()
	if h.weakrefs == nil || h.weakrefs.head == nil {
		return None(), nil
	}
	return h.weakrefs.head.asObject(), nil
}

func objectGetDict(o Object) (Object, error) {
	switch v := o.(type) {
	case *Module:
		// A module always carries md_dict, even a user subclass of
		// ModuleType that never sets tp_dictoffset (HasDict false). The
		// generic object.__getattribute__ path reaches here for
		// `object.__getattribute__(mod, '__dict__')` (importlib's
		// _LazyModule does exactly this), so return md_dict directly
		// rather than gating on HasDict like the AttrDictHolder arm below.
		//
		// CPython: Objects/moduleobject.c module_dict getset (md_dict)
		return v.Dict(), nil
	case *Instance:
		if v.dict == nil {
			if !v.Type().HasDict {
				return nil, fmt.Errorf("AttributeError: '%s' object has no attribute '__dict__'", o.Type().Name)
			}
			// Lazily materialize the instance dict, matching CPython's
			// tp_dictoffset lazy-alloc path.
			//
			// CPython: Objects/typeobject.c:6776 subtype_dict
			v.dict = NewDict()
		}
		// A Python-visible mapping now aliases the instance's attribute
		// storage; the detach owed at dealloc (and the fault it may take)
		// is gated on this flag.
		//
		// CPython: Objects/dictobject.c:6776 subtype_dict materializes the
		// managed dict over the inline values, leaving them to be detached
		// in _PyObject_FreeInstanceAttributes at dealloc.
		v.dictExposed = true
		// Handing the dict to Python code drops the inline-values fast
		// path: code can now store straight into the mapping (e.g.
		// vars(self).update(...)) without routing through instanceSetAttr,
		// so gopy can no longer keep the type's cached keys in sync. CPython
		// materializes a combined dict here and clears values->valid, which
		// deopts the LOAD_ATTR_*_WITH_VALUES arms; mirror that by flipping
		// inlineValid so a class attribute can no longer be served from the
		// cache while a direct instance store shadows it.
		//
		// CPython: Objects/dictobject.c:6857 make_dict_from_instance_attributes
		//          (PyDictValues stops being valid once the dict is built)
		v.inlineValid = false
		return v.dict, nil
	case *Int:
		// The builtin int type has no tp_dictoffset, so (42).__dict__
		// must raise AttributeError. Only an int subclass declaring a
		// dict gets the lazily-allocated attribute store.
		if !v.Type().HasDict {
			return nil, fmt.Errorf("AttributeError: '%s' object has no attribute '__dict__'", o.Type().Name)
		}
		if v.attrs == nil {
			v.attrs = NewDict()
		}
		return v.attrs, nil
	case *Unicode:
		if !v.Type().HasDict {
			return nil, fmt.Errorf("AttributeError: '%s' object has no attribute '__dict__'", o.Type().Name)
		}
		if v.attrs == nil {
			v.attrs = NewDict()
		}
		return v.attrs, nil
	case AttrDictHolder:
		// Subclasses of C-port types (list, bytearray, ...) carry their
		// per-instance dict through the AttrDictHolder interface. When the
		// subtype has a non-zero tp_dictoffset (HasDict), expose and lazily
		// materialize it just like subtype_dict does for *Instance.
		//
		// CPython: Objects/typeobject.c:6776 subtype_dict
		if !o.Type().HasDict {
			return nil, fmt.Errorf("AttributeError: '%s' object has no attribute '__dict__'", o.Type().Name)
		}
		return v.EnsureAttrDict(), nil
	}
	return nil, fmt.Errorf("AttributeError: '%s' object has no attribute '__dict__'", o.Type().Name)
}
