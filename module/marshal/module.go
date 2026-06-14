// Package marshal is the gopy port of CPython's Python/marshal.c module
// surface. The wire-format encoder/decoder lives in the standalone
// marshal Go package; this file exposes the four Python entry points
// importlib._bootstrap_external + py_compile drive:
//
//   - marshal.dumps(value, version=Py_MARSHAL_VERSION) -> bytes
//   - marshal.loads(data) -> object
//   - marshal.dump(value, file, version=Py_MARSHAL_VERSION) -> None
//   - marshal.load(file) -> object
//
// Plus the version constant (currently 5 for CPython 3.14).
//
// CPython: Python/marshal.c:1949 marshal_methods
package marshal

import (
	"bytes"
	"fmt"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/marshal"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("marshal", buildModule)
}

// buildModule materializes the marshal module dict. Mirrors
// marshal_module_exec.
//
// CPython: Python/marshal.c:1975 marshal_module_exec
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("marshal")
	d := m.Dict()
	if err := d.SetItem(objects.NewStr("dumps"), objects.NewBuiltinFunction("dumps", dumps)); err != nil {
		return nil, err
	}
	if err := d.SetItem(objects.NewStr("loads"), objects.NewBuiltinFunction("loads", loads)); err != nil {
		return nil, err
	}
	if err := d.SetItem(objects.NewStr("dump"), objects.NewBuiltinFunction("dump", dump)); err != nil {
		return nil, err
	}
	if err := d.SetItem(objects.NewStr("load"), objects.NewBuiltinFunction("load", load)); err != nil {
		return nil, err
	}
	if err := d.SetItem(objects.NewStr("version"), objects.NewInt(marshal.Version)); err != nil {
		return nil, err
	}
	return m, nil
}

// dumps mirrors marshal.dumps(value, version=Py_MARSHAL_VERSION).
//
// CPython: Python/marshal.c:1782 marshal_dumps_impl
func dumps(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if err := requireNoKwargs("dumps", kwargs); err != nil {
		return nil, err
	}
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("TypeError: dumps() takes 1 or 2 positional arguments but %d were given", len(args))
	}
	// version is accepted for API compatibility; gopy's marshal package
	// only speaks version 5, matching CPython 3.14.
	if len(args) == 2 {
		if _, ok := args[1].(*objects.Int); !ok {
			return nil, fmt.Errorf("TypeError: dumps() version must be int, not '%T'", args[1])
		}
	}
	value, err := unwrap(args[0])
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := marshal.Dump(&buf, value); err != nil {
		return nil, fmt.Errorf("ValueError: %w", err)
	}
	return objects.NewBytes(buf.Bytes()), nil
}

// loads mirrors marshal.loads(bytes).
//
// CPython: Python/marshal.c:1922 marshal_loads_impl
func loads(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if err := requireNoKwargs("loads", kwargs); err != nil {
		return nil, err
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: loads() takes exactly 1 argument but %d were given", len(args))
	}
	src, err := bufferOf(args[0])
	if err != nil {
		return nil, err
	}
	val, err := marshal.Load(bytes.NewReader(src))
	if err != nil {
		return nil, fmt.Errorf("ValueError: %w", err)
	}
	return wrap(val), nil
}

// dump mirrors marshal.dump(value, file, version=Py_MARSHAL_VERSION).
//
// CPython: Python/marshal.c:1742 marshal_dump_impl
func dump(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if err := requireNoKwargs("dump", kwargs); err != nil {
		return nil, err
	}
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("TypeError: dump() takes 2 or 3 positional arguments but %d were given", len(args))
	}
	data, err := dumps([]objects.Object{args[0]}, nil)
	if err != nil {
		return nil, err
	}
	writeMethod, err := objects.GetAttr(args[1], objects.NewStr("write"))
	if err != nil {
		return nil, fmt.Errorf("TypeError: dump() file object has no 'write' method: %w", err)
	}
	if _, err := objects.Call(writeMethod, objects.NewTuple([]objects.Object{data}), nil); err != nil {
		return nil, err
	}
	return objects.None(), nil
}

// load mirrors marshal.load(file).
//
// CPython: Python/marshal.c:1864 marshal_load_impl
func load(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if err := requireNoKwargs("load", kwargs); err != nil {
		return nil, err
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: load() takes exactly 1 argument but %d were given", len(args))
	}
	readMethod, err := objects.GetAttr(args[0], objects.NewStr("read"))
	if err != nil {
		return nil, fmt.Errorf("TypeError: load() file object has no 'read' method: %w", err)
	}
	data, err := objects.Call(readMethod, objects.NewTuple(nil), nil)
	if err != nil {
		return nil, err
	}
	return loads([]objects.Object{data}, nil)
}

func requireNoKwargs(name string, kwargs map[string]objects.Object) error {
	if len(kwargs) != 0 {
		return fmt.Errorf("TypeError: %s() takes no keyword arguments", name)
	}
	return nil
}

func bufferOf(o objects.Object) ([]byte, error) {
	switch v := o.(type) {
	case *objects.Bytes:
		return v.Bytes(), nil
	case *objects.ByteArray:
		return v.Bytes(), nil
	case *objects.MemoryView:
		// Tobytes() serializes the exposed view (honoring offset/length),
		// matching how marshal.loads consumes any bytes-like buffer.
		return v.Tobytes().Bytes(), nil
	}
	return nil, fmt.Errorf("TypeError: a bytes-like object is required, not '%s'", o.Type().Name)
}

// unwrap converts a Python objects.Object into the native Go form the
// marshal package's encoder dispatches on. Mirrors the implicit unwrap
// CPython does when w_object reaches PyObject* and switches on the
// type pointer.
//
// CPython: Python/marshal.c:339 w_object
//
//nolint:gocyclo // mirrors w_object per-type dispatch
func unwrap(o objects.Object) (any, error) {
	if o == nil {
		return nil, nil
	}
	if o == objects.None() {
		return nil, nil
	}
	switch v := o.(type) {
	case *objects.Bool:
		i64, _ := v.Int64()
		return i64 != 0, nil
	case *objects.Int:
		if i64, ok := v.Int64(); ok {
			return i64, nil
		}
		return v.BigInt(), nil
	case *objects.Float:
		return v.Float64(), nil
	case *objects.Complex:
		return v.Complex128(), nil
	case *objects.Unicode:
		return v.Value(), nil
	case *objects.Bytes:
		return v.Bytes(), nil
	case *objects.ByteArray:
		return v.Bytes(), nil
	case *objects.Tuple:
		items := make([]any, v.Len())
		for i := 0; i < v.Len(); i++ {
			item, err := unwrap(v.Item(i))
			if err != nil {
				return nil, err
			}
			items[i] = item
		}
		return items, nil
	case *objects.List:
		items := make([]any, v.Len())
		for i := 0; i < v.Len(); i++ {
			item, err := unwrap(v.Item(i))
			if err != nil {
				return nil, err
			}
			items[i] = item
		}
		return items, nil
	case *objects.Dict:
		out := map[any]any{}
		for _, key := range v.Keys() {
			val, err := v.GetItem(key)
			if err != nil {
				return nil, err
			}
			k, err := unwrap(key)
			if err != nil {
				return nil, err
			}
			wv, err := unwrap(val)
			if err != nil {
				return nil, err
			}
			out[k] = wv
		}
		return out, nil
	case *objects.Set:
		return v, nil
	case *objects.Code:
		return v, nil
	}
	return nil, fmt.Errorf("TypeError: cannot marshal %T objects", o)
}

// wrap converts a Go-native value returned by marshal.Load back into a
// Python objects.Object. Inverse of unwrap.
//
// CPython: Python/marshal.c:1147 r_object (PyObject* return path)
func wrap(v any) objects.Object {
	switch x := v.(type) {
	case nil:
		return objects.None()
	case bool:
		if x {
			return objects.True()
		}
		return objects.False()
	case int64:
		return objects.NewInt(x)
	case float64:
		return objects.NewFloat(x)
	case complex128:
		return objects.NewComplex(real(x), imag(x))
	case string:
		return objects.NewStr(x)
	case []byte:
		return objects.NewBytes(x)
	case []any:
		items := make([]objects.Object, len(x))
		for i, item := range x {
			items[i] = wrap(item)
		}
		return objects.NewTuple(items)
	case map[any]any:
		d := objects.NewDict()
		for k, val := range x {
			_ = d.SetItem(wrap(k), wrap(val))
		}
		return d
	case *objects.Code:
		return x
	case *objects.Set:
		return x
	}
	return objects.None()
}
