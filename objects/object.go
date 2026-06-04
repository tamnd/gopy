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
	SetTypeDescr(objectType, "__init__", NewMethodDescr(objectType, "__init__", objectInitDescr))
	SetTypeDescr(objectType, "__reduce_ex__", NewMethodDescr(objectType, "__reduce_ex__", objectReduceExDescr))
	SetTypeDescr(objectType, "__reduce__", NewMethodDescr(objectType, "__reduce__", objectReduceDescr))
	SetTypeDescr(objectType, "__getstate__", NewMethodDescr(objectType, "__getstate__", objectGetstateDescr))
	SetTypeDescr(objectType, "__subclasshook__", NewClassMethod(
		NewBuiltinFunction("__subclasshook__", objectSubclasshook)))
	SetTypeDescr(objectType, "__init_subclass__", NewClassMethod(
		NewBuiltinFunction("__init_subclass__", objectInitSubclass)))
	SetTypeDescr(objectType, "__format__", NewMethodDescr(objectType, "__format__", objectFormatDescr))
	SetTypeDescr(objectType, "__sizeof__", NewMethodDescr(objectType, "__sizeof__", objectSizeofDescr))
	SetTypeDescr(objectType, "__dir__", NewMethodDescr(objectType, "__dir__", objectDirDescr))

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
	SetTypeDescr(objectType, "__repr__", NewMethodDescr(objectType, "__repr__", objectReprDescr))
	SetTypeDescr(objectType, "__str__", NewMethodDescr(objectType, "__str__", objectStrDescr))
	SetTypeDescr(objectType, "__hash__", NewMethodDescr(objectType, "__hash__", objectHashDescr))
	SetTypeDescr(objectType, "__getattribute__", NewMethodDescr(objectType, "__getattribute__", objectGetattributeDescr))
	SetTypeDescr(objectType, "__setattr__", NewMethodDescr(objectType, "__setattr__", objectSetattrDescr))
	SetTypeDescr(objectType, "__delattr__", NewMethodDescr(objectType, "__delattr__", objectDelattrDescr))

	// tp_richcompare slot wrappers: one per operator. object's
	// richcompare returns NotImplemented for non-EQ/NE and identity
	// for EQ/NE.
	//
	// CPython: Objects/typeobject.c:6950 object_richcompare
	SetTypeDescr(objectType, "__eq__", NewMethodDescr(objectType, "__eq__", richCompareDescr(CompareEQ)))
	SetTypeDescr(objectType, "__ne__", NewMethodDescr(objectType, "__ne__", richCompareDescr(CompareNE)))
	SetTypeDescr(objectType, "__lt__", NewMethodDescr(objectType, "__lt__", richCompareDescr(CompareLT)))
	SetTypeDescr(objectType, "__le__", NewMethodDescr(objectType, "__le__", richCompareDescr(CompareLE)))
	SetTypeDescr(objectType, "__gt__", NewMethodDescr(objectType, "__gt__", richCompareDescr(CompareGT)))
	SetTypeDescr(objectType, "__ge__", NewMethodDescr(objectType, "__ge__", richCompareDescr(CompareGE)))

	// __dict__ getset is installed by subtype_dict in CPython when the
	// type has tp_dictoffset != 0. gopy installs it on object so any
	// HasDict-bearing instance has a working accessor; non-dict
	// instances raise AttributeError, matching subtype_dict's
	// dispatch.
	//
	// CPython: Objects/typeobject.c subtype_dict / subtype_setdict
	SetTypeDescr(objectType, "__dict__", NewGetSetDescr("__dict__", objectGetDict, objectSetDict))
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
	d, ok := value.(*Dict)
	if !ok {
		return fmt.Errorf("TypeError: __dict__ must be set to a dictionary, not a '%s'", value.Type().Name)
	}
	v, ok := o.(*Instance)
	if !ok || !v.Type().HasDict {
		return fmt.Errorf("AttributeError: attribute '__dict__' of '%s' objects is not writable", o.Type().Name)
	}
	v.dict = d
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

// objectGetattributeDescr is the slot wrapper for tp_getattro.
func objectGetattributeDescr(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: expected 2 arguments, got %d", len(args))
	}
	return GenericGetAttr(args[0], args[1])
}

// objectSetattrDescr is the slot wrapper for tp_setattro (set branch).
func objectSetattrDescr(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 3 {
		return nil, fmt.Errorf("TypeError: expected 3 arguments, got %d", len(args))
	}
	if err := GenericSetAttr(args[0], args[1], args[2]); err != nil {
		return nil, err
	}
	return None(), nil
}

// objectDelattrDescr is the slot wrapper for tp_setattro (delete branch).
func objectDelattrDescr(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: expected 2 arguments, got %d", len(args))
	}
	if err := GenericSetAttr(args[0], args[1], nil); err != nil {
		return nil, err
	}
	return None(), nil
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
	// treated as empty, matching object___dir___impl.
	if d, err := GetAttr(self, NewStr("__dict__")); err == nil {
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
	if cls, err := GetAttr(self, NewStr("__class__")); err == nil {
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
	if d, err := GetAttr(args[0], NewStr("__dict__")); err == nil {
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
	// Detect __reduce__ override: when the type's __reduce__
	// descriptor is the one we installed on objectType, the
	// subclass inherits the default and CPython falls through to
	// _common_reduce. Otherwise call the override.
	clsReduce, _ := LookupDescriptor(self.Type(), "__reduce__")
	baseReduce, _ := LookupDescriptor(objectType, "__reduce__")
	if clsReduce != nil && clsReduce != baseReduce {
		fn, err := GetAttr(self, NewStr("__reduce__"))
		if err != nil {
			return nil, err
		}
		return Call(fn, NewTuple(nil), nil)
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
	return o.Type(), nil
}

// objectSetClass implements object.__class__ set. CPython checks that
// old and new types have compatible C-level memory layouts; gopy only
// supports this for *Instance objects (which share a uniform layout)
// and rejects built-in types.
//
// CPython: Objects/typeobject.c:7208 object_set_class
func objectSetClass(o Object, value Object) error {
	if value == nil {
		return fmt.Errorf("TypeError: can't delete __class__ attribute")
	}
	newType, ok := value.(*Type)
	if !ok {
		return fmt.Errorf("TypeError: __class__ must be set to a class, not '%s' object", value.Type().Name)
	}
	if newType.TpFlags&TpFlagImmutable != 0 {
		return fmt.Errorf("TypeError: can't set __class__: new type '%s' is not mutable", newType.Name)
	}
	inst, ok := o.(*Instance)
	if !ok {
		return fmt.Errorf("TypeError: __class__ assignment only supported for heap types, not '%s'", o.Type().Name)
	}
	inst.Header.typ = newType //nolint:staticcheck // QF1008: explicit Header field access mirrors CPython's ob_type layout
	return nil
}

// objectGetDict implements object.__dict__ get for HasDict-bearing
// instances. Mirrors subtype_dict installed by type_new when
// tp_dictoffset is non-zero.
//
// CPython: Objects/typeobject.c subtype_dict
func objectGetDict(o Object) (Object, error) {
	switch v := o.(type) {
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
