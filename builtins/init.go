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
	"io"
	"os"

	"github.com/tamnd/gopy/objects"
)

// Init constructs the builtins dict and stamps the v0.6 surface into
// it: None / True / False / NotImplemented as named constants, and
// print as the single callable. defaultFile is the io.Writer the
// print builtin uses when the call site does not pass file=.
//
// CPython: Python/bltinmodule.c:3425 _PyBuiltin_Init body, "SETBUILTIN" macro
func Init(defaultFile io.Writer) (*objects.Dict, error) {
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
	for _, fn := range constructorPanel() {
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

	return dict, nil
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

// constructorPanel returns the v0.7 constructor wrappers
// (1651-builtins-F): int, float, bool, list, tuple, dict. set lands
// alongside the set / frozenset port.
//
// CPython: Python/bltinmodule.c the type singletons exposed as
// builtins through _PyBuiltin_Init's SETBUILTIN macro
func constructorPanel() []struct {
	name string
	impl func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error)
} {
	return []struct {
		name string
		impl func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error)
	}{
		{"int", IntCtor},
		{"float", FloatCtor},
		{"bool", BoolCtor},
		{"list", ListCtor},
		{"tuple", TupleCtor},
		{"dict", DictCtor},
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
		{"type", TypeOf},
		{"isinstance", IsInstance},
		{"issubclass", IsSubclass},
		{"callable", Callable},
		{"id", ID},
		{"hash", Hash},
		{"repr", Repr},
		{"str", StrOf},
	}
}

// iterationPanel returns the v0.7 iteration builtins (1651-builtins-A).
// Listed here so init.go stays the single registration point without
// growing one entry per builtin.
//
// CPython: Python/bltinmodule.c builtin_methods iter / next / len /
// reversed / enumerate / zip / range
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
		{"reversed", Reversed},
		{"enumerate", Enumerate},
		{"zip", Zip},
		{"range", Range},
	}
}

// setBuiltin is the SETBUILTIN macro from _PyBuiltin_Init: stash
// value under name in dict and propagate the dict-set error.
//
// CPython: Python/bltinmodule.c:3451 SETBUILTIN
func setBuiltin(dict *objects.Dict, name string, value objects.Object) error {
	return dict.SetItem(objects.NewStr(name), value)
}
