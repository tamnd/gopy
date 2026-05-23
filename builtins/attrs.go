// Port of the attribute panel from Python/bltinmodule.c: getattr,
// hasattr, setattr, delattr. vars() and dir() depend on frame locals
// and type-dict introspection that lands later; they stay parked
// until that machinery is ready.

package builtins

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tamnd/gopy/objects"
)

// GetAttr ports builtin_getattr. Two-argument form returns
// PyObject_GetAttr; three-argument form catches AttributeError and
// returns the default. Other errors propagate. When the
// AttributeError is swallowed, the thread-state's current exception
// is cleared to match CPython's PyObject_GetOptionalAttr +
// PyErr_Clear pattern: a Python-level __getattr__ that raised
// AttributeError must not leak its exception state past the catch.
//
// CPython: Python/bltinmodule.c:1228 builtin_getattr
// CPython: Objects/object.c:1392 PyObject_GetOptionalAttr (PyErr_Clear)
func GetAttr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("TypeError: getattr expected 2 or 3 arguments, got %d", len(args))
	}
	v, err := objects.GetAttr(args[0], args[1])
	if err == nil {
		return v, nil
	}
	if len(args) == 3 && isAttributeError(err) {
		if objects.ClearCurrentExceptionHook != nil {
			objects.ClearCurrentExceptionHook()
		}
		return args[2], nil
	}
	return nil, err
}

// HasAttr ports builtin_hasattr_impl. CPython uses
// PyObject_GetOptionalAttr; gopy emulates by calling GetAttr and
// folding AttributeError into False. Non-AttributeError failures
// still propagate, matching CPython's "raise other exceptions" path.
// The PyErr_Clear inside PyObject_GetOptionalAttr is ported via the
// thread-state clear hook so a Python-level __getattr__ that raised
// AttributeError does not leak its exception state past the catch.
//
// CPython: Python/bltinmodule.c:1301 builtin_hasattr_impl
// CPython: Objects/object.c:1392 PyObject_GetOptionalAttr (PyErr_Clear)
func HasAttr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: hasattr expected 2 arguments, got %d", len(args))
	}
	_, err := objects.GetAttr(args[0], args[1])
	if err == nil {
		return objects.True(), nil
	}
	if isAttributeError(err) {
		if objects.ClearCurrentExceptionHook != nil {
			objects.ClearCurrentExceptionHook()
		}
		return objects.False(), nil
	}
	return nil, err
}

// SetAttr ports builtin_setattr_impl: PyObject_SetAttr with the
// (obj, name, value) triple.
//
// CPython: Python/bltinmodule.c:1727 builtin_setattr_impl
func SetAttr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 3 {
		return nil, fmt.Errorf("TypeError: setattr expected 3 arguments, got %d", len(args))
	}
	if err := objects.SetAttr(args[0], args[1], args[2]); err != nil {
		return nil, err
	}
	return objects.None(), nil
}

// DelAttr ports builtin_delattr_impl: PyObject_DelAttr.
//
// CPython: Python/bltinmodule.c:1750 builtin_delattr_impl
func DelAttr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: delattr expected 2 arguments, got %d", len(args))
	}
	if err := objects.DelAttr(args[0], args[1]); err != nil {
		return nil, err
	}
	return objects.None(), nil
}

// Dir ports builtin_dir. With one argument, returns the sorted list
// of attribute names CPython would: instance __dict__ keys, the
// names registered on the type's MRO, and for modules the module
// dict keys. Without an argument the current frame's locals are
// not yet exposed, so callers must pass an explicit object.
//
// CPython: Python/bltinmodule.c:1001 builtin_dir
func Dir(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) > 1 {
		return nil, fmt.Errorf("TypeError: dir expected at most 1 argument, got %d", len(args))
	}
	if len(args) == 0 {
		return dirLocals()
	}
	seen := map[string]struct{}{}
	collect := func(d *objects.Dict) {
		if d == nil {
			return
		}
		for _, k := range d.Keys() {
			if s, err := objects.Str(k); err == nil {
				seen[s] = struct{}{}
			}
		}
	}
	switch v := args[0].(type) {
	case *objects.Module:
		collect(v.Dict())
	case *objects.Type:
		for _, name := range objects.TypeDescrNames(v) {
			seen[name] = struct{}{}
		}
	case *objects.Instance:
		collect(v.Dict())
		for _, name := range objects.TypeDescrNames(v.Type()) {
			seen[name] = struct{}{}
		}
	default:
		for _, name := range objects.TypeDescrNames(v.Type()) {
			seen[name] = struct{}{}
		}
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]objects.Object, len(names))
	for i, n := range names {
		out[i] = objects.NewStr(n)
	}
	return objects.NewList(out), nil
}

// dirLocals returns the running frame's local-scope keys, sorted.
// Used by the no-argument dir() form. Matches CPython _dir_locals
// which reads f_locals (== f_globals at module scope).
//
// CPython: Objects/object.c:2110 _dir_locals
func dirLocals() (objects.Object, error) {
	if currentScope == nil {
		return objects.NewList(nil), nil
	}
	_, locals := currentScope()
	d, ok := locals.(*objects.Dict)
	if !ok {
		return objects.NewList(nil), nil
	}
	keys := d.Keys()
	names := make([]string, 0, len(keys))
	for _, k := range keys {
		s, err := objects.Str(k)
		if err != nil {
			continue
		}
		names = append(names, s)
	}
	sort.Strings(names)
	out := make([]objects.Object, len(names))
	for i, n := range names {
		out[i] = objects.NewStr(n)
	}
	return objects.NewList(out), nil
}

// isAttributeError matches the sentinel string the protocol layer
// emits. Once gopy grows real PyExc_AttributeError, this collapses to
// errors.Is against that singleton.
func isAttributeError(err error) bool {
	return err != nil && strings.HasPrefix(err.Error(), "AttributeError")
}
