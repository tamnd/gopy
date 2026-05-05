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

	return dict, nil
}

// setBuiltin is the SETBUILTIN macro from _PyBuiltin_Init: stash
// value under name in dict and propagate the dict-set error.
//
// CPython: Python/bltinmodule.c:3451 SETBUILTIN
func setBuiltin(dict *objects.Dict, name string, value objects.Object) error {
	return dict.SetItem(objects.NewStr(name), value)
}
