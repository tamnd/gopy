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
	"os"
	"sync"

	"github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/objects"
)

var wireOnce sync.Once

// Init constructs the builtins dict and stamps the v0.6 surface into
// it: None / True / False / NotImplemented as named constants, and
// print as the single callable. defaultFile is the io.Writer the
// print builtin uses when the call site does not pass file=.
//
// CPython: Python/bltinmodule.c:3425 _PyBuiltin_Init body, "SETBUILTIN" macro
func Init(defaultFile io.Writer) (*objects.Dict, error) {
	wireTypeCalls()
	dict := objects.NewDict()

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

	printFn := objects.NewBuiltinFunction("print", Print(defaultFile))
	if err := setBuiltin(dict, "print", printFn); err != nil {
		return nil, err
	}

	inputFn := objects.NewBuiltinFunction("input", Input(os.Stdin, defaultFile))
	if err := setBuiltin(dict, "input", inputFn); err != nil {
		return nil, err
	}

	for _, fn := range iterationPanel() {
		if err := setBuiltin(dict, fn.name, objects.NewBuiltinFunction(fn.name, fn.impl)); err != nil {
			return nil, err
		}
	}
	for _, fn := range reflectionPanel() {
		if err := setBuiltin(dict, fn.name, objects.NewBuiltinFunction(fn.name, fn.impl)); err != nil {
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
		bindCtor(objects.StrType(), StrOf)
		bindCtor(objects.IntType, IntCtor)
		bindCtor(objects.FloatType, FloatCtor)
		bindCtor(objects.BoolType, BoolCtor)
		bindCtor(objects.ListType, ListCtor)
		bindCtor(objects.TupleType, TupleCtor)
		bindDictCtor(objects.DictType)
		bindCtor(objects.SetType, SetCtor)
		bindCtor(objects.FrozensetType, FrozensetCtor)
		bindCtor(objects.BytesType, BytesCtor)
		bindCtor(objects.ByteArrayType, ByteArrayCtor)
		bindCtor(objects.RangeType, Range)
		bindCtor(ZipType, Zip)
		bindCtor(objects.MapType, Map)
		bindCtor(objects.FilterType, Filter)
		bindCtor(objects.EnumerateType, Enumerate)
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
	wrapperName := t.Name + ".__new__"
	objects.SetTypeDescr(t, "__new__", objects.NewBuiltinFunction(wrapperName,
		func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
			if len(args) < 1 {
				return nil, fmt.Errorf("TypeError: %s(): not enough arguments", wrapperName)
			}
			if _, ok := args[0].(*objects.Type); !ok {
				return nil, fmt.Errorf("TypeError: %s(X): X is not a type object", wrapperName)
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

// reflectionPanel returns the v0.7 reflection builtins (1651-builtins-B).
//
// CPython: Python/bltinmodule.c builtin_methods type / isinstance /
// issubclass / callable / id / hash / repr / str
func reflectionPanel() []struct {
	name string
	impl func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error)
} {
	return []struct {
		name string
		impl func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error)
	}{
		{"isinstance", IsInstance},
		{"issubclass", IsSubclass},
		{"callable", Callable},
		{"id", ID},
		{"hash", Hash},
		{"repr", Repr},
	}
}

// iterationPanel returns the v0.7 iteration builtins (1651-builtins-A).
// Listed here so init.go stays the single registration point without
// growing one entry per builtin.
//
// CPython: Python/bltinmodule.c builtin_methods iter / next / len /
// reversed / enumerate / zip / range / map / filter
func iterationPanel() []struct {
	name string
	impl func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error)
} {
	return []struct {
		name string
		impl func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error)
	}{
		{"len", Len},
		{"iter", Iter},
		{"next", Next},
	}
}

// setBuiltin is the SETBUILTIN macro from _PyBuiltin_Init: stash
// value under name in dict and propagate the dict-set error.
//
// CPython: Python/bltinmodule.c:3451 SETBUILTIN
func setBuiltin(dict *objects.Dict, name string, value objects.Object) error {
	return dict.SetItem(objects.NewStr(name), value)
}
