// Package _interpreters is the Go port of CPython's _interpreters C
// extension (Modules/_interpretersmodule.c), the low-level surface that
// Lib/concurrent/interpreters/__init__.py wraps. CPython spins up a
// fully isolated subinterpreter per id; gopy is single-interpreter, so
// each "interpreter" here is a distinct __main__ namespace running on
// the one shared VM. That is observably faithful for the cross-
// interpreter code the standard library drives: code execs against its
// own globals, and static type objects (and their slot wrappers) are
// shared, exactly as CPython shares immortal static types across
// subinterpreters.
//
// CPython: Modules/_interpretersmodule.c
package _interpreters

import (
	"fmt"
	"sync"

	"github.com/tamnd/gopy/builtins"
	pyerrors "github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("_interpreters", buildModule)
}

// whence constants record how an interpreter came to exist. gopy only
// ever reports the main interpreter (WHENCE_RUNTIME) and stdlib-created
// ones (WHENCE_STDLIB), but the full set is published so the high-level
// wrapper's _WHENCE_TO_STR table resolves every value.
//
// CPython: Include/internal/pycore_interp.h _PyInterpreterState whence
const (
	whenceUnknown    = 0
	whenceRuntime    = 1
	whenceLegacyCAPI = 2
	whenceCAPI       = 3
	whenceXI         = 4
	whenceStdlib     = 5
)

// InterpreterError is the base for the module's interpreter failures.
//
// CPython: Modules/_interpretersmodule.c:2376 state->PyExc_InterpreterError
var InterpreterError = pyerrors.NewExcType("interpreters.InterpreterError", []*objects.Type{pyerrors.PyExc_Exception})

// InterpreterNotFoundError is raised when an id names no live interpreter.
//
// CPython: Modules/_interpretersmodule.c:2382 state->PyExc_InterpreterNotFoundError
var InterpreterNotFoundError = pyerrors.NewExcType("interpreters.InterpreterNotFoundError", []*objects.Type{InterpreterError})

// NotShareableError is raised when an object cannot cross interpreters.
// In CPython it subclasses TypeError.
//
// CPython: Modules/_interpretersmodule.c:2389 state->PyExc_NotShareableError
var NotShareableError = pyerrors.NewExcType("interpreters.NotShareableError", []*objects.Type{pyerrors.PyExc_TypeError})

// interp is one logical interpreter: an id, its provenance, a refcount
// matching the high-level wrapper's incref/decref bookkeeping, and the
// __main__ namespace its exec()'d code runs against.
//
// CPython: Modules/_interpretersmodule.c interpid + PyInterpreterState
type interp struct {
	id     int64
	whence int
	refs   int64
	ns     *objects.Dict
}

var (
	mu       sync.Mutex
	registry = map[int64]*interp{}
	nextID   int64 = 1
)

// mainInterp is interpreter 0, the runtime-created main interpreter.
// It is registered eagerly so get_main / get_current resolve before any
// create() call.
//
// CPython: Modules/_interpretersmodule.c:1003 interp_get_main
var mainInterp = func() *interp {
	m := &interp{id: 0, whence: whenceRuntime, refs: 1, ns: objects.NewDict()}
	registry[0] = m
	return m
}()

func lookup(id int64) (*interp, error) {
	it, ok := registry[id]
	if !ok {
		return nil, raise(InterpreterNotFoundError, fmt.Sprintf("%d", id))
	}
	return it, nil
}

func raise(t *objects.Type, msg string) error {
	exc := pyerrors.New(t, objects.NewTuple([]objects.Object{objects.NewStr(msg)}))
	return objects.NewRaisedError(exc, "")
}

func argInt(args []objects.Object, i int) (int64, error) {
	if i >= len(args) {
		return 0, fmt.Errorf("TypeError: missing required argument")
	}
	n, ok := args[i].(*objects.Int)
	if !ok {
		return 0, fmt.Errorf("TypeError: id must be an int, got %s", args[i].Type().Name)
	}
	v, _ := n.Int64()
	return v, nil
}

// create allocates a new interpreter and returns its id.
//
// CPython: Modules/_interpretersmodule.c:768 interp_create
func create(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	mu.Lock()
	defer mu.Unlock()
	id := nextID
	nextID++
	ns := objects.NewDict()
	_ = ns.SetItem(objects.NewStr("__name__"), objects.NewStr("__main__"))
	registry[id] = &interp{id: id, whence: whenceStdlib, refs: 0, ns: ns}
	return objects.NewInt(id), nil
}

// destroy finalizes and removes an interpreter.
//
// CPython: Modules/_interpretersmodule.c:874 interp_destroy
func destroy(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	id, err := argInt(args, 0)
	if err != nil {
		return nil, err
	}
	mu.Lock()
	defer mu.Unlock()
	if _, err := lookup(id); err != nil {
		return nil, err
	}
	if id == 0 {
		return nil, raise(InterpreterError, "cannot destroy the main interpreter")
	}
	delete(registry, id)
	return objects.None(), nil
}

// listAll returns (id, whence) for every live interpreter.
//
// CPython: Modules/_interpretersmodule.c:946 interp_list_all
func listAll(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	mu.Lock()
	defer mu.Unlock()
	var items []objects.Object
	// Main first, then created interpreters in id order.
	for id := int64(0); id < nextID; id++ {
		it, ok := registry[id]
		if !ok {
			continue
		}
		items = append(items, objects.NewTuple([]objects.Object{
			objects.NewInt(it.id), objects.NewInt(int64(it.whence)),
		}))
	}
	return objects.NewList(items), nil
}

func getCurrent(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	// gopy runs everything on the main interpreter.
	//
	// CPython: Modules/_interpretersmodule.c:990 interp_get_current
	return objects.NewTuple([]objects.Object{objects.NewInt(0), objects.NewInt(whenceRuntime)}), nil
}

func getMain(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	// CPython: Modules/_interpretersmodule.c:1003 interp_get_main
	return objects.NewTuple([]objects.Object{objects.NewInt(0), objects.NewInt(whenceRuntime)}), nil
}

func whence(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	id, err := argInt(args, 0)
	if err != nil {
		return nil, err
	}
	mu.Lock()
	defer mu.Unlock()
	it, err := lookup(id)
	if err != nil {
		return objects.NewInt(whenceUnknown), nil
	}
	return objects.NewInt(int64(it.whence)), nil
}

func incref(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	id, err := argInt(args, 0)
	if err != nil {
		return nil, err
	}
	mu.Lock()
	defer mu.Unlock()
	it, err := lookup(id)
	if err != nil {
		return nil, err
	}
	it.refs++
	return objects.None(), nil
}

func decref(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	id, err := argInt(args, 0)
	if err != nil {
		return nil, err
	}
	mu.Lock()
	defer mu.Unlock()
	it, err := lookup(id)
	if err != nil {
		return nil, err
	}
	if it.refs > 0 {
		it.refs--
	}
	return objects.None(), nil
}

func isRunning(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if _, err := argInt(args, 0); err != nil {
		return nil, err
	}
	// gopy never leaves an interpreter mid-exec across a call boundary.
	//
	// CPython: Modules/_interpretersmodule.c:1066 interp_is_running
	return objects.NewBool(false), nil
}

// setMainAttrs binds shareable values into an interpreter's __main__.
//
// CPython: Modules/_interpretersmodule.c:1140 interp_set___main___attrs
func setMainAttrs(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	id, err := argInt(args, 0)
	if err != nil {
		return nil, err
	}
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: set___main___attrs() missing 'updates'")
	}
	updates, ok := args[1].(*objects.Dict)
	if !ok {
		return nil, fmt.Errorf("TypeError: updates must be a dict, not %s", args[1].Type().Name)
	}
	mu.Lock()
	it, err := lookup(id)
	mu.Unlock()
	if err != nil {
		return nil, err
	}
	for _, k := range updates.Keys() {
		v, err := updates.GetItem(k)
		if err != nil {
			return nil, err
		}
		if err := it.ns.SetItem(k, v); err != nil {
			return nil, err
		}
	}
	return objects.None(), nil
}

// execCode runs source in the interpreter's __main__ namespace. It
// returns None on success or, on an unhandled exception, an excinfo
// namespace the high-level wrapper turns into ExecutionFailed.
//
// CPython: Modules/_interpretersmodule.c:1218 interp_exec
func execCode(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	id, err := argInt(args, 0)
	if err != nil {
		return nil, err
	}
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: exec() missing 'code'")
	}
	code := args[1]
	mu.Lock()
	it, err := lookup(id)
	mu.Unlock()
	if err != nil {
		return nil, err
	}
	if _, err := builtins.Exec([]objects.Object{code, it.ns}, nil); err != nil {
		return excinfoFor(err), nil
	}
	return objects.None(), nil
}

// excinfoFor packages a Go-side execution error as the minimal excinfo
// namespace concurrent.interpreters.ExecutionFailed reads (type, msg,
// formatted, errdisplay).
//
// CPython: Modules/_interpretersmodule.c _PyXI_excinfo
func excinfoFor(err error) objects.Object {
	typeName := "Exception"
	msg := err.Error()
	if re, ok := err.(*objects.RaisedError); ok {
		if re.Exc != nil {
			typeName = re.Exc.Type().Name
		}
		if re.Msg != "" {
			msg = re.Msg
		}
	}
	ns := objects.NewNamespace()
	_ = objects.SetAttr(ns, objects.NewStr("type"), objects.NewStr(typeName))
	_ = objects.SetAttr(ns, objects.NewStr("msg"), objects.NewStr(msg))
	formatted := fmt.Sprintf("%s: %s", typeName, msg)
	_ = objects.SetAttr(ns, objects.NewStr("formatted"), objects.NewStr(formatted))
	_ = objects.SetAttr(ns, objects.NewStr("errdisplay"), objects.NewStr(formatted))
	return ns
}

func isShareable(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	// gopy shares object references directly, so everything is shareable.
	//
	// CPython: Modules/_interpretersmodule.c:1660 interp_is_shareable
	return objects.NewBool(true), nil
}

func fn(name string, f func([]objects.Object, map[string]objects.Object) (objects.Object, error)) *objects.BuiltinFunction {
	return objects.NewBuiltinFunction(name, f)
}

// buildModule materializes the _interpreters module dict.
//
// CPython: Modules/_interpretersmodule.c:2400 module_exec
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_interpreters")
	d := m.Dict()
	set := func(name string, v objects.Object) error {
		return d.SetItem(objects.NewStr(name), v)
	}
	entries := []struct {
		name string
		v    objects.Object
	}{
		{"InterpreterError", InterpreterError},
		{"InterpreterNotFoundError", InterpreterNotFoundError},
		{"NotShareableError", NotShareableError},
		{"WHENCE_UNKNOWN", objects.NewInt(whenceUnknown)},
		{"WHENCE_RUNTIME", objects.NewInt(whenceRuntime)},
		{"WHENCE_LEGACY_CAPI", objects.NewInt(whenceLegacyCAPI)},
		{"WHENCE_CAPI", objects.NewInt(whenceCAPI)},
		{"WHENCE_XI", objects.NewInt(whenceXI)},
		{"WHENCE_STDLIB", objects.NewInt(whenceStdlib)},
		{"create", fn("create", create)},
		{"destroy", fn("destroy", destroy)},
		{"list_all", fn("list_all", listAll)},
		{"get_current", fn("get_current", getCurrent)},
		{"get_main", fn("get_main", getMain)},
		{"whence", fn("whence", whence)},
		{"incref", fn("incref", incref)},
		{"decref", fn("decref", decref)},
		{"is_running", fn("is_running", isRunning)},
		{"set___main___attrs", fn("set___main___attrs", setMainAttrs)},
		{"exec", fn("exec", execCode)},
		{"is_shareable", fn("is_shareable", isShareable)},
	}
	for _, e := range entries {
		if err := set(e.name, e.v); err != nil {
			return nil, err
		}
	}
	return m, nil
}
