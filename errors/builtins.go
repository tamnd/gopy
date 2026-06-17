package errors

import (
	stderrors "errors"

	"github.com/tamnd/gopy/objects"
)

// The exception class hierarchy ported in v0.3. Each Type entry has
// the same MRO shape as CPython. The class objects are created lazily
// at package init so the var-init dependency graph stays acyclic.

// Built-in exception type singletons. Names match CPython.
//
// CPython: Objects/exceptions.c:L1937 PyExc_BaseException and friends
var (
	PyExc_BaseException       = newExcType("BaseException", []*objects.Type{objects.ObjectType()})
	PyExc_Exception           = newExcType("Exception", []*objects.Type{PyExc_BaseException})
	PyExc_LookupError         = newExcType("LookupError", []*objects.Type{PyExc_Exception})
	PyExc_ArithmeticError     = newExcType("ArithmeticError", []*objects.Type{PyExc_Exception})
	PyExc_RuntimeError        = newExcType("RuntimeError", []*objects.Type{PyExc_Exception})
	PyExc_KeyError            = newExcType("KeyError", []*objects.Type{PyExc_LookupError})
	PyExc_IndexError          = newExcType("IndexError", []*objects.Type{PyExc_LookupError})
	PyExc_OverflowError       = newExcType("OverflowError", []*objects.Type{PyExc_ArithmeticError})
	PyExc_ZeroDivisionError   = newExcType("ZeroDivisionError", []*objects.Type{PyExc_ArithmeticError})
	PyExc_FloatingPointError  = newExcType("FloatingPointError", []*objects.Type{PyExc_ArithmeticError})
	PyExc_NotImplementedError = newExcType("NotImplementedError", []*objects.Type{PyExc_RuntimeError})
	PyExc_AttributeError      = newExcType("AttributeError", []*objects.Type{PyExc_Exception})
	PyExc_NameError           = newExcType("NameError", []*objects.Type{PyExc_Exception})
	PyExc_TypeError           = newExcType("TypeError", []*objects.Type{PyExc_Exception})
	PyExc_ValueError          = newExcType("ValueError", []*objects.Type{PyExc_Exception})
	PyExc_StopIteration       = newExcType("StopIteration", []*objects.Type{PyExc_Exception})
	PyExc_StopAsyncIteration  = newExcType("StopAsyncIteration", []*objects.Type{PyExc_Exception})
	PyExc_SystemExit          = newExcType("SystemExit", []*objects.Type{PyExc_BaseException})
	PyExc_KeyboardInterrupt   = newExcType("KeyboardInterrupt", []*objects.Type{PyExc_BaseException})
	PyExc_ImportError         = newExcType("ImportError", []*objects.Type{PyExc_Exception})
	PyExc_ModuleNotFoundError = newExcType("ModuleNotFoundError", []*objects.Type{PyExc_ImportError})

	// Additional exceptions covered by CPython's hierarchy, ported so
	// stdlib code paths that reference them by name resolve at import.
	//
	// CPython: Objects/exceptions.c:L2200 the remainder of the family
	PyExc_AssertionError    = newExcType("AssertionError", []*objects.Type{PyExc_Exception})
	PyExc_MemoryError       = newExcType("MemoryError", []*objects.Type{PyExc_Exception})
	PyExc_EOFError          = newExcType("EOFError", []*objects.Type{PyExc_Exception})
	PyExc_BufferError       = newExcType("BufferError", []*objects.Type{PyExc_Exception})
	PyExc_RecursionError    = newExcType("RecursionError", []*objects.Type{PyExc_RuntimeError})
	PyExc_UnboundLocalError = newExcType("UnboundLocalError", []*objects.Type{PyExc_NameError})
	PyExc_ReferenceError    = newExcType("ReferenceError", []*objects.Type{PyExc_Exception})
	PyExc_SystemError       = newExcType("SystemError", []*objects.Type{PyExc_Exception})
	PyExc_GeneratorExit     = newExcType("GeneratorExit", []*objects.Type{PyExc_BaseException})
)

func init() {
	// Wire AttributeErrorFactory so GenericGetAttr produces rich
	// AttributeError exceptions with .name and .obj populated.
	//
	// CPython: Objects/object.c:843 _PyObject_SetAttributeError
	objects.AttributeErrorFactory = func(obj objects.Object, attrName string) error {
		// CPython's generic getattr error formats the bare tp_name (the
		// short class name), not the module-qualified name that member_get
		// uses via %T. Keep both faithful: this path stays short.
		//
		// CPython: Objects/object.c:1662 _PyObject_GenericGetAttrWithDict
		msg := "'" + obj.Type().Name + "' object has no attribute '" + attrName + "'"
		exc := New(PyExc_AttributeError, objects.NewTuple([]objects.Object{objects.NewStr(msg)}))
		d := exc.EnsureAttrDict()
		_ = d.SetItem(objects.NewStr("name"), objects.NewStr(attrName))
		_ = d.SetItem(objects.NewStr("obj"), obj)
		return &objects.RaisedError{Exc: exc, Msg: "AttributeError: " + msg}
	}
	// KeyErrorFactory builds a Python KeyError whose args[0] is the
	// original key object so callers can recover `e.args[0] is key`.
	//
	// CPython: Objects/exceptions.c KeyError stores the key verbatim in
	// args[0] (set_discard_key / set_remove_key path)
	objects.KeyErrorFactory = func(key objects.Object) error {
		repr, _ := objects.Repr(key)
		exc := New(PyExc_KeyError, objects.NewTuple([]objects.Object{key}))
		return &objects.RaisedError{Exc: exc, Msg: "KeyError: " + repr}
	}
	objects.IsIndexErrorHook = func(err error) bool {
		if err == nil {
			return false
		}
		msg := err.Error()
		if msg == "IndexError" {
			return true
		}
		const p = "IndexError:"
		return len(msg) >= len(p) && msg[:len(p)] == p
	}
	// IsStopIterationHook lets IterNext convert a Python-level StopIteration
	// exception back to ErrStopIteration, mirroring PyIter_Next's
	// _PyErr_ExceptionMatches(PyExc_StopIteration) check.
	//
	// CPython: Objects/abstract.c:2856 PyIter_Next
	objects.IsStopIterationHook = func(err error) bool {
		if err == nil {
			return false
		}
		msg := err.Error()
		if msg == "StopIteration" {
			return true
		}
		const p = "StopIteration:"
		return len(msg) >= len(p) && msg[:len(p)] == p
	}
	// StopIteration.value exposes args[0] (or None) as a member, so
	// callers can read the generator/coroutine return value from
	// `except StopIteration as e: e.value`. CPython stores it in a
	// dedicated field stamped by StopIteration_init; gopy reads it back
	// off args so the same value flows through whether the exception
	// was constructed via StopIteration(retval) or args was rewritten.
	//
	// CPython: Objects/exceptions.c:711 StopIteration_members
	// CPython: Objects/exceptions.c:684 StopIteration_init
	objects.SetTypeDescr(PyExc_StopIteration, "value",
		objects.NewGetSetDescr("value", stopIterValueGet, stopIterValueSet))
	// SystemExit exposes a dedicated `code` member seeded by SystemExit_init.
	//
	// CPython: Objects/exceptions.c:880 SystemExit_members
	objects.SetTypeDescr(PyExc_SystemExit, "code",
		objects.NewGetSetDescr("code", sysExitCodeGet, sysExitCodeSet))
	// AsyncGenStopIterationHook lets objects/async_gen.go raise a typed
	// StopIteration(value) without importing this package. Mirrors
	// _PyGen_SetStopIterationValue in the async_gen_unwrap_value path.
	//
	// CPython: Objects/genobject.c:652 _PyGen_SetStopIterationValue
	objects.AsyncGenStopIterationHook = func(value objects.Object) error {
		if value == nil {
			value = objects.None()
		}
		exc := New(PyExc_StopIteration, objects.NewTuple([]objects.Object{value}))
		return &objects.RaisedError{Exc: exc, Msg: "StopIteration"}
	}
}

// stopIterValueGet returns the dedicated StopValue slot, seeded by
// StopIteration_init from args[0] or None at construction.
//
// CPython: Objects/exceptions.c:741 PyStopIterationObject value member
func stopIterValueGet(owner objects.Object) (objects.Object, error) {
	e, ok := owner.(*Exception)
	if !ok {
		return objects.None(), nil
	}
	if e.StopValue != nil {
		return e.StopValue, nil
	}
	return objects.None(), nil
}

// stopIterValueSet writes only the dedicated StopValue slot, leaving
// args untouched. Mirrors CPython's _Py_T_OBJECT setter on
// PyStopIterationObject.value: assigning .value does not rewrite args.
//
// CPython: Objects/exceptions.c:741 PyStopIterationObject value member
func stopIterValueSet(owner objects.Object, value objects.Object) error {
	e, ok := owner.(*Exception)
	if !ok {
		return stderrors.New("TypeError: descriptor 'value' requires StopIteration")
	}
	if value == nil {
		value = objects.None()
	}
	e.StopValue = value
	return nil
}

// NewExcType creates a named exception type that inherits from the
// given bases. It wires the same TpNew/Str/Repr/HasDict slots
// that all CPython exception types carry. Exported for use by C-extension
// ports (e.g. _pickle, _struct) that need proper exception classes.
//
// CPython: Objects/exceptions.c BaseException_Type setup
func NewExcType(name string, bases []*objects.Type) *objects.Type {
	return newExcType(name, bases)
}

func newExcType(name string, bases []*objects.Type) *objects.Type {
	t := objects.NewType(name, bases)
	// Construction routes through tp_new (type_call invokes it), exactly as
	// CPython's exception types carry tp_new but leave tp_call at 0. Wiring a
	// tp_call here would make instances look callable: PyCallable_Check(exc)
	// must be false so exceptiongroup get_matcher_type rejects an exception
	// instance as a predicate.
	//
	// CPython: Objects/exceptions.c:648 BaseException_Type (tp_call == 0)
	t.TpNew = excTpNew
	t.Str = excStr
	t.Repr = excRepr
	// BaseException carries a tp_dictoffset so per-instance attributes
	// stored through __init__ / __setstate__ / direct assignment land in
	// the AttrDictHolder dict on *Exception. Every subclass inherits
	// that storage; the flag has to be on every exception Type so
	// GenericSetAttr's tp.HasDict gate lets the AttrDictHolder branch
	// take the write.
	//
	// CPython: Objects/exceptions.c:648 BaseException_Type
	// (offsetof(PyBaseExceptionObject, dict))
	t.HasDict = true
	// tp_getattro / tp_setattro are PyObject_GenericGetAttr /
	// PyObject_GenericSetAttr on BaseException; subclasses inherit them
	// through inherit_slots. gopy's per-type inheritance copies the
	// slots when the child is built, but the BaseException singleton is
	// built before exception_attrs.init() runs, so subclasses see nil
	// and skip the copy. Stamp the slots in the constructor so every
	// exception Type carries them from creation time.
	//
	// CPython: Objects/exceptions.c:626 BaseException_Type (tp_getattro
	// = PyObject_GenericGetAttr, tp_setattro = PyObject_GenericSetAttr)
	t.Getattro = objects.GenericGetAttr
	t.Setattro = objects.GenericSetAttr
	// BaseException leaves tp_hash unset and so inherits object's
	// _Py_HashPointer (identity hash); BaseException_richcompare is also
	// 0, so exceptions hash and compare by identity. gopy's scalar-slot
	// inheritance does not run for these NewType-built singletons (the
	// same reason getattro/setattro are stamped above), so set the
	// identity hash explicitly. Without it every exception type reports
	// Hash == nil and instances raise "unhashable type", which breaks
	// e.g. building a set of exceptions: {ValueError(42)}.
	//
	// CPython: Objects/exceptions.c:648 BaseException_Type (tp_hash == 0,
	// tp_richcompare == 0 → inherits object identity hash/compare)
	t.Hash = objects.IdentityHash
	// TpTraverse visits every GC-visible field so the cycle collector can
	// walk the Exception→Traceback→Frame chain and identify unreachable
	// exception cycles. This matches CPython's BaseException_traverse which
	// visits tb, context, cause, notes, args, and dict.
	//
	// CPython: Objects/exceptions.c:121 BaseException_traverse
	t.TpTraverse = excTraverse
	// BaseException carries __repr__ / __str__ method descriptors that
	// wrap BaseException_repr / BaseException_str. Without these, a
	// user subclass like `class BozoError(Exception): pass` runs through
	// fixupCallReprStr which finds object.__repr__ via MRO and installs
	// slot_tp_repr, so repr(BozoError()) returns the generic
	// "<__main__.BozoError object at 0x...>" instead of "BozoError()".
	//
	// CPython: Objects/typeobject.c add_operators (slot wrapper for
	// each non-NULL slot in slotdefs). The repr/str rows install
	// __repr__ / __str__ descriptors that wrap tp_repr / tp_str.
	objects.SetTypeDescr(t, "__repr__", objects.NewMethodDescr(t, "__repr__", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		if len(args) != 1 {
			return nil, stderrors.New("TypeError: __repr__ expected 1 argument")
		}
		s, err := excRepr(args[0])
		if err != nil {
			return nil, err
		}
		return objects.NewStr(s), nil
	}))
	objects.SetTypeDescr(t, "__str__", objects.NewMethodDescr(t, "__str__", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		if len(args) != 1 {
			return nil, stderrors.New("TypeError: __str__ expected 1 argument")
		}
		s, err := excStr(args[0])
		if err != nil {
			return nil, err
		}
		return objects.NewStr(s), nil
	}))
	return t
}

// excTpNew is the tp_new slot for every built-in exception type. It
// mirrors BaseException_new: allocate a fresh PyBaseExceptionObject,
// store positional args on .args, and return. Keyword arguments that
// correspond to exception-specific attributes (name, obj, path) are
// stored in the exception's attr dict so code like
// `raise NameError("msg", name="x")` works correctly.
//
// CPython: Objects/exceptions.c:L42 BaseException_new
// CPython: Objects/exceptions.c:2503 NameError_init
// CPython: Objects/exceptions.c:2586 AttributeError_init
func excTpNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	// Allocate with empty args, mirroring BaseException_new's behavior when
	// the real positional arguments are about to be installed by tp_init
	// (typeCallViaTpNew always runs __init__, which lands on baseExceptionInit
	// and stores the args via setArgsSteal). Building the real tuple here too
	// would leave it orphaned the moment tp_init replaces it, stranding every
	// positional argument at a phantom reference the collector cannot reclaim.
	//
	// CPython: Objects/exceptions.c:48 BaseException_new (empty args; BaseException_init sets them)
	exc := New(cls, nil)
	if len(kwargs) > 0 {
		d := exc.EnsureAttrDict()
		for k, v := range kwargs {
			_ = d.SetItem(objects.NewStr(k), v)
		}
	}
	return exc, nil
}

// sysExitCodeGet returns SystemExit's `code`. An explicit assignment is
// preserved in the dedicated slot; otherwise the value is derived from the
// constructor args exactly as SystemExit_init seeds it (args[0] for one
// arg, the args tuple for several, None for none).
//
// CPython: Objects/exceptions.c:866 SystemExit_init
// CPython: Objects/exceptions.c:880 SystemExit_members (code)
func sysExitCodeGet(owner objects.Object) (objects.Object, error) {
	e, ok := owner.(*Exception)
	if !ok {
		return objects.None(), nil
	}
	if e.SysExitCode != nil {
		return e.SysExitCode, nil
	}
	if e.Args == nil {
		return objects.None(), nil
	}
	switch e.Args.Len() {
	case 0:
		return objects.None(), nil
	case 1:
		return e.Args.Item(0), nil
	default:
		return e.Args, nil
	}
}

// sysExitCodeSet writes only the dedicated SysExitCode slot, leaving
// args untouched. Mirrors the _Py_T_OBJECT member on PySystemExitObject.
//
// CPython: Objects/exceptions.c:880 SystemExit_members (code)
func sysExitCodeSet(owner objects.Object, value objects.Object) error {
	e, ok := owner.(*Exception)
	if !ok {
		return stderrors.New("TypeError: descriptor 'code' requires SystemExit")
	}
	if value == nil {
		value = objects.None()
	}
	e.SysExitCode = value
	return nil
}

// excStr ports BaseException_str: empty for no args, str(args[0]) for
// a single arg, repr(args) otherwise.
//
// CPython: Objects/exceptions.c:171 BaseException_str
func excStr(o objects.Object) (string, error) {
	e, ok := o.(*Exception)
	if !ok || e.Args == nil {
		return "", nil
	}
	switch e.Args.Len() {
	case 0:
		return "", nil
	case 1:
		return objects.Str(e.Args.Item(0))
	default:
		return objects.Repr(e.Args)
	}
}

// excRepr ports BaseException_repr: "TypeName(arg)" for a single arg,
// "TypeName(args)" otherwise.
//
// CPython: Objects/exceptions.c:193 BaseException_repr
func excRepr(o objects.Object) (string, error) {
	e, ok := o.(*Exception)
	if !ok {
		return "", nil
	}
	name := e.TypeName()
	if e.Args == nil || e.Args.Len() == 0 {
		return name + "()", nil
	}
	if e.Args.Len() == 1 {
		s, err := objects.Repr(e.Args.Item(0))
		if err != nil {
			return "", err
		}
		return name + "(" + s + ")", nil
	}
	s, err := objects.Repr(e.Args)
	if err != nil {
		return "", err
	}
	return name + s, nil
}

// IsSubtype reports whether sub inherits from super, walking the MRO.
//
// CPython: Objects/typeobject.c:L2556 PyType_IsSubtype
func IsSubtype(sub, super *objects.Type) bool {
	return objects.IsSubtype(sub, super)
}

// Match reports whether exc's type inherits from t. Mirrors
// PyErr_GivenExceptionMatches.
//
// CPython: Python/errors.c:L327 PyErr_GivenExceptionMatches
func Match(exc *Exception, t *objects.Type) bool {
	if exc == nil || exc.ExcType == nil {
		return false
	}
	return IsSubtype(exc.ExcType, t)
}

// excTraverse is the TpTraverse slot shared by all exception types. It visits
// every GC-visible field so the cycle collector can walk Exception→Traceback→Frame
// chains. Matches CPython's BaseException_traverse.
//
// CPython: Objects/exceptions.c:121 BaseException_traverse
func excTraverse(o objects.Object, visit objects.Visitor) error {
	exc, ok := o.(*Exception)
	if !ok {
		return nil
	}
	refs := []objects.Object{exc.TB, exc.Context, exc.Cause, exc.Notes, exc.NotesObj, exc.Args, exc.StopValue}
	if exc.EG != nil {
		refs = append(refs, exc.EG.Msg, exc.EG.Excs, exc.EG.ExcsStr)
	}
	for _, ref := range refs {
		if ref != nil {
			if err := visit(ref); err != nil {
				return err
			}
		}
	}
	return nil
}
