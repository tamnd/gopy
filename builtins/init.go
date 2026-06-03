// Port of Python/bltinmodule.c _PyBuiltin_Init. The C version creates
// the builtins module via _PyModule_CreateInitialized and stamps every
// constant + every PyMethodDef from builtin_methods into its dict.
// gopy lands the surface in slices: this file populates the dict the
// VM reads as the implicit __builtins__ for module-level code, and
// covers the constants plus the print() builtin. The remaining
// builtins (input, len, abs, type, ...) follow.
//
// CPython: Python/bltinmodule.c:3425 _PyBuiltin_Init

package builtins

import (
	"fmt"
	"io"
	"sync"

	"github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/module/sys"
	"github.com/tamnd/gopy/objects"
)

var wireOnce sync.Once

// Init constructs the builtins dict and stamps the v0.6 surface into
// it: None / True / False / NotImplemented as named constants, and
// print as the single callable. defaultFile is the io.Writer the
// caller wants exposed as sys.stdout; it is also the writer print()
// will land on when the call site omits file=. CPython's
// Py_InitializeFromConfig sequences sys_init_streams before
// _PyBuiltin_Init runs, so gopy mirrors that ordering here: plumb
// PyConfig.stdout into sys before any builtin can fire.
//
// CPython: Python/bltinmodule.c:3425 _PyBuiltin_Init body, "SETBUILTIN" macro
// CPython: Python/sysmodule.c:3795 sys_init_streams
func Init(defaultFile io.Writer) (*objects.Dict, error) {
	wireTypeCalls()
	if defaultFile != nil {
		sys.SetStdio(defaultFile, nil)
	}
	dict := objects.NewDict()
	// CPython's _PyBuiltin_Init stamps the builtins module into
	// interp->modules so any later `import builtins` returns the
	// dict that builtins.__import__ was already populating. Without
	// this, the import machinery would fall through to the
	// module/builtins inittab and rebuild the dict with the default
	// os.Stdout, clobbering anything callers wired via defaultFile.
	//
	// CPython: Python/pylifecycle.c:1413 init_interp_main (builtins registration)
	imp.AddModule("builtins", objects.NewModuleWithDict("builtins", dict))

	if err := setBuiltin(dict, "None", objects.None()); err != nil {
		return nil, err
	}
	if err := setBuiltin(dict, "True", objects.True()); err != nil {
		return nil, err
	}
	if err := setBuiltin(dict, "False", objects.False()); err != nil {
		return nil, err
	}
	if err := setBuiltin(dict, "NotImplemented", objects.NotImplemented()); err != nil {
		return nil, err
	}
	if err := setBuiltin(dict, "Ellipsis", objects.Ellipsis()); err != nil {
		return nil, err
	}

	for _, t := range typeSingletons() {
		if err := setBuiltin(dict, t.name, t.t); err != nil {
			return nil, err
		}
	}

	for _, t := range exceptionSingletons() {
		if err := setBuiltin(dict, t.name, t.t); err != nil {
			return nil, err
		}
	}

	printFn := objects.NewBuiltinFunction("print", Print)
	if err := setBuiltin(dict, "print", printFn); err != nil {
		return nil, err
	}

	inputFn := objects.NewBuiltinFunction("input", Input)
	if err := setBuiltin(dict, "input", inputFn); err != nil {
		return nil, err
	}

	for _, fn := range iterationPanel() {
		bf := objects.NewBuiltinFunctionConv(fn.name, fn.conv, fn.impl)
		if fn.cacheHook != nil {
			fn.cacheHook(bf)
		}
		if err := setBuiltin(dict, fn.name, bf); err != nil {
			return nil, err
		}
	}
	for _, fn := range reflectionPanel() {
		bf := objects.NewBuiltinFunctionConv(fn.name, fn.conv, fn.impl)
		if fn.cacheHook != nil {
			fn.cacheHook(bf)
		}
		if err := setBuiltin(dict, fn.name, bf); err != nil {
			return nil, err
		}
	}
	for _, fn := range attributePanel() {
		if err := setBuiltin(dict, fn.name, objects.NewBuiltinFunction(fn.name, fn.impl)); err != nil {
			return nil, err
		}
	}
	for _, fn := range aggregationPanel() {
		if err := setBuiltin(dict, fn.name, objects.NewBuiltinFunction(fn.name, fn.impl)); err != nil {
			return nil, err
		}
	}
	for _, fn := range numericPanel() {
		if err := setBuiltin(dict, fn.name, objects.NewBuiltinFunction(fn.name, fn.impl)); err != nil {
			return nil, err
		}
	}
	for _, fn := range scopePanel() {
		if err := setBuiltin(dict, fn.name, objects.NewBuiltinFunction(fn.name, fn.impl)); err != nil {
			return nil, err
		}
	}
	for _, fn := range asyncIterPanel() {
		if err := setBuiltin(dict, fn.name, objects.NewBuiltinFunction(fn.name, fn.impl)); err != nil {
			return nil, err
		}
	}

	importFn := objects.NewBuiltinFunction("__import__", Import)
	if err := setBuiltin(dict, "__import__", importFn); err != nil {
		return nil, err
	}

	// breakpoint() forwards to sys.breakpointhook. Register the builtin
	// here and hand the default hook to sys so sys.breakpointhook and
	// sys.__breakpointhook__ resolve once the sys module is built.
	//
	// CPython: Python/bltinmodule.c:3359 breakpoint PyMethodDef
	breakpointFn := objects.NewBuiltinFunction("breakpoint", Breakpoint)
	if err := setBuiltin(dict, "breakpoint", breakpointFn); err != nil {
		return nil, err
	}
	sys.SetBreakpointHook(objects.NewBuiltinFunction("breakpointhook", breakpointHook))

	compileFn := objects.NewBuiltinFunction("compile", Compile)
	if err := setBuiltin(dict, "compile", compileFn); err != nil {
		return nil, err
	}

	openFn := objects.NewBuiltinFunction("open", Open)
	if err := setBuiltin(dict, "open", openFn); err != nil {
		return nil, err
	}

	evalFn := objects.NewBuiltinFunction("eval", Eval)
	if err := setBuiltin(dict, "eval", evalFn); err != nil {
		return nil, err
	}
	execFn := objects.NewBuiltinFunction("exec", Exec)
	if err := setBuiltin(dict, "exec", execFn); err != nil {
		return nil, err
	}

	if buildClass != nil {
		bcFn := objects.NewBuiltinFunction("__build_class__", buildClass)
		if err := setBuiltin(dict, "__build_class__", bcFn); err != nil {
			return nil, err
		}
	}

	return dict, nil
}

// typeSingletons returns the type-object names CPython exposes
// directly via SETBUILTIN. Each row's Call slot is wired below in
// wireTypeCalls so calling the name (e.g. int(x)) goes through the
// metaclass tp_call path the same way it does in CPython.
//
// CPython: Python/bltinmodule.c:3461 SETBUILTIN block
func typeSingletons() []struct {
	name string
	t    *objects.Type
} {
	return []struct {
		name string
		t    *objects.Type
	}{
		{"object", objects.ObjectType()},
		{"type", objects.TypeType()},
		{"str", objects.StrType()},
		{"int", objects.IntType},
		{"float", objects.FloatType},
		{"bool", objects.BoolType},
		{"list", objects.ListType},
		{"tuple", objects.TupleType},
		{"dict", objects.DictType},
		{"bytes", objects.BytesType},
		{"bytearray", objects.ByteArrayType},
		{"complex", objects.ComplexType},
		{"frozenset", objects.FrozensetType},
		{"set", objects.SetType},
		{"slice", objects.SliceType},
		{"property", objects.PropertyType},
		{"classmethod", objects.ClassMethodType},
		{"staticmethod", objects.StaticMethodType},
		{"super", objects.SuperType},
		// Iteration types: CPython exposes these as classes, not functions.
		// CPython: Python/bltinmodule.c:3461 SETBUILTIN block
		{"range", objects.RangeType},
		{"zip", ZipType},
		{"map", objects.MapType},
		{"filter", objects.FilterType},
		{"enumerate", objects.EnumerateType},
		{"reversed", objects.ReversedType},
		{"memoryview", objects.MemoryViewType},
		// CPython: Python/bltinmodule.c:3461 SETBUILTIN block
		// NoneType and ellipsis are exposed so pickle/copyreg can find them.
		{"NoneType", objects.NoneType()},
		{"ellipsis", objects.EllipsisType()},
	}
}

// wireTypeCalls hooks each built-in type's tp_new slot to its
// constructor wrapper. Once wired, `int(x)` reaches typeCall, which
// dispatches through cls.TpNew when the metaclass call kicks in; the
// type itself stays the value bound to the builtin name, so
// isinstance(x, int) keeps treating int as a *Type. tp_call stays nil
// on these types so callable(1) returns False, matching CPython where
// PyLong_Type.tp_call is NULL while PyLong_Type.tp_new (long_new) does
// the construction.
//
// CPython: Objects/longobject.c:6438 PyLong_Type (tp_new = long_new, tp_call NULL)
func wireTypeCalls() {
	wireOnce.Do(func() {
		objects.SetStrTpNewBase(StrOf)
		bindCtorDescr(objects.StrType(), StrOf)
		objects.SetIntTpNewBase(IntCtor)
		bindCtorDescr(objects.IntType, IntCtor)
		objects.SetFloatTpNewBase(FloatCtor)
		bindCtorDescr(objects.FloatType, FloatCtor)
		bindCtor(objects.BoolType, BoolCtor)
		bindListCtor(objects.ListType)
		// tuple's TpNew is set in objects/tuple.go (subtype-aware,
		// populates items inline because tuple is immutable). Only the
		// __new__ descriptor needs binding here.
		bindCtorDescr(objects.TupleType, TupleCtor)
		bindDictCtor(objects.DictType)
		objects.SetComplexTpNewBase(ComplexCtor)
		bindCtorDescr(objects.ComplexType, ComplexCtor)
		// set/frozenset use subtype-aware TpNew so that class H(set): ...
		// instances carry their own type (CPython: set_new passes subtype).
		objects.SetType.TpNew = func(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
			return setCtorWithType(cls, args)
		}
		bindCtorDescr(objects.SetType, SetCtor)
		// frozenset uses a subtype-aware TpNew so frozenset subclass instances
		// carry their own type (CPython: frozenset_new passes subtype).
		objects.FrozensetType.TpNew = frozensetCtorWithType
		bindCtorDescr(objects.FrozensetType, FrozensetCtor)
		// bytes uses a subtype-aware TpNew so that class B(bytes): ...
		// instances carry their own type and can hold instance attributes
		// (CPython: bytes_subtype_new passes subtype to tp_alloc).
		objects.BytesType.TpNew = func(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
			return bytesNewObject(cls, args, kwargs)
		}
		bindCtorDescr(objects.BytesType, BytesCtor)
		bindByteArrayCtor(objects.ByteArrayType)
		bindCtor(objects.RangeType, Range)
		bindCtor(ZipType, Zip)
		bindCtor(objects.MapType, Map)
		bindCtor(objects.FilterType, Filter)
		// enumerate uses a subtype-aware TpNew so class E(enumerate): ...
		// instances carry their own type (CPython: enum_new_impl allocates
		// with type->tp_alloc(type)).
		objects.EnumerateType.TpNew = Enumerate
		bindCtorDescr(objects.EnumerateType, func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
			return Enumerate(objects.EnumerateType, args, kwargs)
		})
		bindCtor(objects.ReversedType, Reversed)
		bindCtor(objects.MemoryViewType, memoryViewCtor)
	})
}

// bindCtor wires the type's tp_new slot to fn and exposes the matching
// __new__ descriptor in the type's own __dict__. CPython's
// add_tp_new_wrapper does the latter automatically for every type whose
// tp_new is non-NULL, which is how `'__new__' in int.__dict__` is True
// and how enum's `_find_data_type_` distinguishes data-bearing mixins.
//
// CPython: Objects/typeobject.c:9952 tp_new_wrapper + add_tp_new_wrapper
func bindCtor(t *objects.Type, fn func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error)) {
	t.TpNew = func(_ *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
		return fn(args, kwargs)
	}
	bindCtorDescr(t, fn)
}

// bindCtorDescr installs the __new__ descriptor without touching
// TpNew. Used when a type already wired a subtype-aware TpNew (e.g.
// str via SetStrTpNewBase) and only the descriptor exposure is left.
func bindCtorDescr(t *objects.Type, fn func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error)) {
	wrapperName := t.Name + ".__new__"
	objects.SetTypeDescr(t, "__new__", objects.NewBuiltinFunction(wrapperName,
		func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
			if len(args) < 1 {
				return nil, fmt.Errorf("TypeError: %s(): not enough arguments", wrapperName)
			}
			cls, ok := args[0].(*objects.Type)
			if !ok {
				return nil, fmt.Errorf("TypeError: %s(X): X is not a type object", wrapperName)
			}
			if tn := t.TpNew; tn != nil {
				return tn(cls, args[1:], kwargs)
			}
			return fn(args[1:], kwargs)
		}))
}

// exceptionSingletons returns the exception-class names CPython
// exposes as plain builtins. Constructor dispatch (raise OSError("x"))
// is wired separately by the exception panel; this entry only makes
// the names visible so user code referencing them at module scope
// (e.g. `BlockingIOError = BlockingIOError` in io.py) resolves at
// import time.
//
// CPython: Python/bltinmodule.c:3461 SETBUILTIN block (the exception
// rows generated by Objects/exceptions.c)
func exceptionSingletons() []struct {
	name string
	t    *objects.Type
} {
	return []struct {
		name string
		t    *objects.Type
	}{
		{"BaseException", errors.PyExc_BaseException},
		{"Exception", errors.PyExc_Exception},
		{"ArithmeticError", errors.PyExc_ArithmeticError},
		{"AttributeError", errors.PyExc_AttributeError},
		{"FloatingPointError", errors.PyExc_FloatingPointError},
		{"ImportError", errors.PyExc_ImportError},
		{"IndexError", errors.PyExc_IndexError},
		{"KeyError", errors.PyExc_KeyError},
		{"KeyboardInterrupt", errors.PyExc_KeyboardInterrupt},
		{"LookupError", errors.PyExc_LookupError},
		{"ModuleNotFoundError", errors.PyExc_ModuleNotFoundError},
		{"NameError", errors.PyExc_NameError},
		{"NotImplementedError", errors.PyExc_NotImplementedError},
		{"OverflowError", errors.PyExc_OverflowError},
		{"RuntimeError", errors.PyExc_RuntimeError},
		{"StopAsyncIteration", errors.PyExc_StopAsyncIteration},
		{"StopIteration", errors.PyExc_StopIteration},
		{"SystemExit", errors.PyExc_SystemExit},
		{"TypeError", errors.PyExc_TypeError},
		{"ValueError", errors.PyExc_ValueError},
		{"ZeroDivisionError", errors.PyExc_ZeroDivisionError},
		// OSError + family. IOError is the classic alias kept around
		// for legacy code.
		// CPython: Objects/exceptions.c:3461 PyExc_OSError + alias
		{"OSError", errors.PyExc_OSError},
		{"IOError", errors.PyExc_OSError},
		{"EnvironmentError", errors.PyExc_OSError},
		{"BlockingIOError", errors.PyExc_BlockingIOError},
		{"ChildProcessError", errors.PyExc_ChildProcessError},
		{"ConnectionError", errors.PyExc_ConnectionError},
		{"BrokenPipeError", errors.PyExc_BrokenPipeError},
		{"ConnectionAbortedError", errors.PyExc_ConnectionAbortedError},
		{"ConnectionRefusedError", errors.PyExc_ConnectionRefusedError},
		{"ConnectionResetError", errors.PyExc_ConnectionResetError},
		{"FileExistsError", errors.PyExc_FileExistsError},
		{"FileNotFoundError", errors.PyExc_FileNotFoundError},
		{"InterruptedError", errors.PyExc_InterruptedError},
		{"IsADirectoryError", errors.PyExc_IsADirectoryError},
		{"NotADirectoryError", errors.PyExc_NotADirectoryError},
		{"PermissionError", errors.PyExc_PermissionError},
		{"ProcessLookupError", errors.PyExc_ProcessLookupError},
		{"TimeoutError", errors.PyExc_TimeoutError},
		// Warning hierarchy.
		// CPython: Objects/exceptions.c:2865 PyExc_Warning + subclasses
		{"Warning", errors.PyExc_Warning},
		{"UserWarning", errors.PyExc_UserWarning},
		{"DeprecationWarning", errors.PyExc_DeprecationWarning},
		{"PendingDeprecationWarning", errors.PyExc_PendingDeprecationWarning},
		{"SyntaxWarning", errors.PyExc_SyntaxWarning},
		{"RuntimeWarning", errors.PyExc_RuntimeWarning},
		{"FutureWarning", errors.PyExc_FutureWarning},
		{"ImportWarning", errors.PyExc_ImportWarning},
		{"UnicodeWarning", errors.PyExc_UnicodeWarning},
		{"BytesWarning", errors.PyExc_BytesWarning},
		{"ResourceWarning", errors.PyExc_ResourceWarning},
		{"EncodingWarning", errors.PyExc_EncodingWarning},
		{"AssertionError", errors.PyExc_AssertionError},
		{"MemoryError", errors.PyExc_MemoryError},
		{"EOFError", errors.PyExc_EOFError},
		{"BufferError", errors.PyExc_BufferError},
		{"RecursionError", errors.PyExc_RecursionError},
		{"UnboundLocalError", errors.PyExc_UnboundLocalError},
		{"ReferenceError", errors.PyExc_ReferenceError},
		{"SystemError", errors.PyExc_SystemError},
		{"GeneratorExit", errors.PyExc_GeneratorExit},
		{"BaseExceptionGroup", errors.PyExc_BaseExceptionGroup},
		{"ExceptionGroup", errors.PyExc_ExceptionGroup},
		{"SyntaxError", errors.PyExc_SyntaxError},
		{"IndentationError", errors.PyExc_IndentationError},
		{"TabError", errors.PyExc_TabError},
		// CPython: Python/bltinmodule.c _IncompleteInputError registration
		{"_IncompleteInputError", errors.PyExc_IncompleteInputError},
		{"UnicodeError", errors.PyExc_UnicodeError},
		{"UnicodeDecodeError", errors.PyExc_UnicodeDecodeError},
		{"UnicodeEncodeError", errors.PyExc_UnicodeEncodeError},
		{"UnicodeTranslateError", errors.PyExc_UnicodeTranslateError},
	}
}

// asyncIterPanel returns the async iteration builtins: aiter, anext.
//
// CPython: Python/bltinmodule.c builtin_methods aiter / anext
func asyncIterPanel() []struct {
	name string
	impl func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error)
} {
	return []struct {
		name string
		impl func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error)
	}{
		{"aiter", AIter},
		{"anext", ANext},
	}
}

// scopePanel returns the introspection builtins that read the running
// frame: globals(), locals().
//
// CPython: Python/bltinmodule.c builtin_methods globals / locals
func scopePanel() []struct {
	name string
	impl func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error)
} {
	return []struct {
		name string
		impl func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error)
	}{
		{"globals", Globals},
		{"locals", Locals},
		{"vars", Vars},
	}
}

// numericPanel returns the v0.7 numeric / formatting builtins
// (1651-builtins-E).
//
// CPython: Python/bltinmodule.c builtin_methods abs / divmod / pow /
// chr / ord / bin / oct / hex / ascii / format
func numericPanel() []struct {
	name string
	impl func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error)
} {
	return []struct {
		name string
		impl func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error)
	}{
		{"abs", Abs},
		{"divmod", Divmod},
		{"pow", Pow},
		{"chr", Chr},
		{"ord", Ord},
		{"bin", Bin},
		{"oct", Oct},
		{"hex", Hex},
		{"ascii", ASCII},
		{"format", Format},
		{"round", Round},
	}
}

// aggregationPanel returns the v0.7 aggregation builtins (1651-builtins-D).
//
// CPython: Python/bltinmodule.c builtin_methods sum / min / max / any
// / all / sorted
func aggregationPanel() []struct {
	name string
	impl func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error)
} {
	return []struct {
		name string
		impl func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error)
	}{
		{"sum", Sum},
		{"min", MinOf},
		{"max", MaxOf},
		{"any", Any},
		{"all", All},
		{"sorted", Sorted},
	}
}

// attributePanel returns the v0.7 attribute builtins (1651-builtins-C).
//
// CPython: Python/bltinmodule.c builtin_methods getattr / hasattr /
// setattr / delattr
func attributePanel() []struct {
	name string
	impl func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error)
} {
	return []struct {
		name string
		impl func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error)
	}{
		{"getattr", GetAttr},
		{"hasattr", HasAttr},
		{"setattr", SetAttr},
		{"delattr", DelAttr},
		{"dir", Dir},
	}
}

// builtinRow is one row of the static builtin registration table.
// conv carries the METH_* tag the specializer reads off
// BuiltinFunction.Conv. cacheHook (nil for most rows) lets a panel
// stash the resulting BuiltinFunction pointer into the callable
// cache so specialize_c_call's identity comparisons fire.
//
// CPython: Python/bltinmodule.c builtin_methods rows + init_interp_main
// assigning interp->callable_cache.{len, isinstance}
type builtinRow struct {
	name      string
	conv      objects.MethFlag
	impl      func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error)
	cacheHook func(*objects.BuiltinFunction)
}

// reflectionPanel returns the v0.7 reflection builtins (1651-builtins-B).
//
// CPython: Python/bltinmodule.c builtin_methods type / isinstance /
// issubclass / callable / id / hash / repr / str
func reflectionPanel() []builtinRow {
	return []builtinRow{
		// isinstance is METH_FASTCALL in CPython; tagging it lets the
		// specializer emit CALL_ISINSTANCE for the `isinstance(o, T)`
		// shape and CALL_BUILTIN_FAST for arities the fast arm can't
		// handle.
		//
		// CPython: Python/bltinmodule.c:1330 builtin_isinstance (METH_FASTCALL)
		{name: "isinstance", conv: objects.MethFastcall, impl: IsInstance, cacheHook: objects.RegisterCallableCacheIsinstance},
		{name: "issubclass", conv: objects.MethFastcall, impl: IsSubclass},
		{name: "callable", conv: objects.MethO, impl: Callable},
		{name: "id", conv: objects.MethO, impl: ID},
		{name: "hash", conv: objects.MethO, impl: Hash},
		{name: "repr", conv: objects.MethO, impl: Repr},
	}
}

// iterationPanel returns the v0.7 iteration builtins (1651-builtins-A).
// Listed here so init.go stays the single registration point without
// growing one entry per builtin.
//
// CPython: Python/bltinmodule.c builtin_methods iter / next / len /
// reversed / enumerate / zip / range / map / filter
func iterationPanel() []builtinRow {
	return []builtinRow{
		// len is METH_O in CPython; the specializer compares against
		// the cached pointer and emits CALL_LEN for the `len(o)` shape.
		//
		// CPython: Python/bltinmodule.c:1862 builtin_len (METH_O)
		{name: "len", conv: objects.MethO, impl: Len, cacheHook: objects.RegisterCallableCacheLen},
		{name: "iter", conv: objects.MethFastcall, impl: Iter},
		{name: "next", conv: objects.MethFastcall, impl: Next},
	}
}

// setBuiltin is the SETBUILTIN macro from _PyBuiltin_Init: stash
// value under name in dict and propagate the dict-set error.
//
// CPython: Python/bltinmodule.c:3451 SETBUILTIN
func setBuiltin(dict *objects.Dict, name string, value objects.Object) error {
	if bf, ok := value.(*objects.BuiltinFunction); ok && bf.Module == "" {
		bf.Module = "builtins"
	}
	return dict.SetItem(objects.NewStr(name), value)
}
