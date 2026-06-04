// _contextvars built-in module. Mirrors the thin module-init wrapper
// from cpython/Python/_contextvars.c: it exposes the three public
// types (Context, ContextVar, Token) and the copy_context() helper,
// then registers itself in the inittab so import _contextvars resolves
// through the built-in path.
//
// CPython: Python/_contextvars.c

package contextvars

import (
	"fmt"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

// init wires the tp_call slots that turn the three exported types into
// constructors, then registers the module in the built-in inittab.
//
// CPython: Python/_contextvars.c:51 _contextvarsmodule_slots
func init() {
	ContextType.Call = contextCall
	ContextVarType.Call = contextVarCall
	TokenType.Call = tokenCall
	_ = imp.AppendInittab("_contextvars", buildModule)
}

// buildModule constructs the _contextvars module dict. Mirrors
// _contextvars_exec from the C side, plus the copy_context entry that
// belongs in the module's m_methods table.
//
// CPython: Python/_contextvars.c:23 _contextvars_exec
// CPython: Python/_contextvars.c:54 _contextvarsmodule_methods
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_contextvars")
	d := m.Dict()
	entries := []struct {
		name string
		val  objects.Object
	}{
		{"Context", ContextType},
		{"ContextVar", ContextVarType},
		{"Token", TokenType},
		{"copy_context", objects.NewBuiltinFunction("copy_context", copyContextBuiltin)},
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// contextCall implements Context() with no arguments. CPython rejects
// any positional or keyword argument and returns an empty context.
//
// CPython: Python/context.c:434 context_tp_new
func contextCall(_ objects.Object, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 0 || len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: Context() does not accept any arguments")
	}
	return NewContext(), nil
}

// contextVarCall implements ContextVar(name, *, default=...). The
// signature pins one positional argument (the name) and a single
// keyword (default); anything else raises TypeError.
//
// CPython: Python/context.c:879 contextvar_tp_new
func contextVarCall(_ objects.Object, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: ContextVar() takes exactly 1 positional argument (%d given)", len(args))
	}
	// CPython: contextvar_new rejects a non-str name before anything
	// else, then hashes the name (contextvar_generate_hash), which
	// propagates the unhashable-type error for weird str subclasses
	// (gh-132002).
	//
	// CPython: Python/context.c:866 contextvar_new (PyUnicode_Check)
	// CPython: Python/context.c:833 contextvar_generate_hash
	if !objects.IsSubtype(args[0].Type(), objects.StrType()) {
		return nil, fmt.Errorf("TypeError: context variable name must be a str")
	}
	if _, herr := objects.Hash(args[0]); herr != nil {
		return nil, herr
	}
	name, err := objects.Str(args[0])
	if err != nil {
		return nil, err
	}
	var (
		def    objects.Object
		hasDef bool
	)
	for k, v := range kwargs {
		if k != "default" {
			return nil, fmt.Errorf("TypeError: ContextVar() got an unexpected keyword argument %q", k)
		}
		def = v
		hasDef = true
	}
	return NewContextVar(name, def, hasDef), nil
}

// tokenCall rejects user-side instantiation. Tokens are minted by
// ContextVar.set; CPython exposes the type so isinstance() works but
// raises on tp_new.
//
// CPython: Python/context.c:1191 token_tp_new
func tokenCall(_ objects.Object, _ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return nil, fmt.Errorf("RuntimeError: Tokens can only be created by ContextVars")
}

// copyContextBuiltin is the module-level copy_context() entry. CPython
// reads the running thread via PyThreadState_GET and snapshots its
// context; gopy reaches the same thread state through the vm-installed
// CurrentThreadHook (the same hook ContextVar.get/set already use) and
// hands it to CopyCurrent.
//
// CPython: Python/context.c:114 _contextvars_copy_context_impl
func copyContextBuiltin(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 0 || len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: copy_context() takes no arguments")
	}
	ts, err := currentTS()
	if err != nil {
		return nil, err
	}
	return CopyCurrent(ts), nil
}
