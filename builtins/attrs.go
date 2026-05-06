// Port of the attribute panel from Python/bltinmodule.c: getattr,
// hasattr, setattr, delattr. vars() and dir() depend on frame locals
// and type-dict introspection that lands later; they stay parked
// until that machinery is ready.

package builtins

import (
	"fmt"
	"strings"

	"github.com/tamnd/gopy/objects"
)

// GetAttr ports builtin_getattr. Two-argument form returns
// PyObject_GetAttr; three-argument form catches AttributeError and
// returns the default. Other errors propagate.
//
// CPython: Python/bltinmodule.c:1228 builtin_getattr
func GetAttr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("TypeError: getattr expected 2 or 3 arguments, got %d", len(args))
	}
	v, err := objects.GetAttr(args[0], args[1])
	if err == nil {
		return v, nil
	}
	if len(args) == 3 && isAttributeError(err) {
		return args[2], nil
	}
	return nil, err
}

// HasAttr ports builtin_hasattr_impl. CPython uses
// PyObject_GetOptionalAttr; gopy emulates by calling GetAttr and
// folding AttributeError into False. Non-AttributeError failures
// still propagate, matching CPython's "raise other exceptions" path.
//
// CPython: Python/bltinmodule.c:1301 builtin_hasattr_impl
func HasAttr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: hasattr expected 2 arguments, got %d", len(args))
	}
	_, err := objects.GetAttr(args[0], args[1])
	if err == nil {
		return objects.True(), nil
	}
	if isAttributeError(err) {
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

// isAttributeError matches the sentinel string the protocol layer
// emits. Once gopy grows real PyExc_AttributeError, this collapses to
// errors.Is against that singleton.
func isAttributeError(err error) bool {
	return err != nil && strings.HasPrefix(err.Error(), "AttributeError")
}
