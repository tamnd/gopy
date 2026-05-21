// Phase 6 of P14.1 exposes the Pickler and Unpickler Python types so
// pickle.py's `from _pickle import (Pickler, Unpickler, dump, dumps,
// load, loads)` block resolves and pickle.dumps / pickle.loads route
// through the C-accelerated path on every call.
//
// CPython publishes these types from Modules/_pickle.c; the two
// constructors are the entry the pure-Python pickle.Pickler /
// pickle.Unpickler subclasses reach for via super().__init__. Both
// take a file-like first argument plus the same keyword knobs the
// module-level dump / load functions accept.
//
// CPython: Modules/_pickle.c:8090 Pickler_Type
// CPython: Modules/_pickle.c:7510 Unpickler_Type

package _pickle

import (
	"errors"
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// ---------------------------------------------------------------------------
// Pickler.
// ---------------------------------------------------------------------------

// Pickler is the Python `_pickle.Pickler` instance. It carries the
// file the encoder will write to plus the resolved protocol. The
// in-progress write buffer (memo, frame cursor) lives on a fresh
// pickler that .dump() allocates per call, matching CPython where
// state.memo is reused across .dump() calls but the wire frame state
// is reset every time.
//
// CPython: Modules/_pickle.c:614 PicklerObject
type Pickler struct {
	objects.Header

	file  objects.Object
	proto int
}

// picklerType is the *objects.Type for Pickler.
var picklerType *objects.Type

// picklerTpNew constructs a Pickler from `(file, protocol=None, *,
// fix_imports=True, buffer_callback=None)`.
//
// CPython: Modules/_pickle.c:7651 _pickle_Pickler___init___impl
func picklerTpNew(_ *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("TypeError: Pickler() takes 1 or 2 positional arguments (got %d)", len(args))
	}
	file := args[0]
	if _, err := objects.GetAttr(file, objects.NewStr("write")); err != nil {
		return nil, errors.New("TypeError: Pickler() file argument has no write() method")
	}
	proto, err := resolveProtocol(args, kwargs, 1)
	if err != nil {
		return nil, err
	}
	delete(kwargs, "fix_imports")
	delete(kwargs, "buffer_callback")
	for k := range kwargs {
		return nil, fmt.Errorf("TypeError: Pickler() got an unexpected keyword argument %q", k)
	}
	p := &Pickler{file: file, proto: proto}
	p.Init(picklerType)
	return p, nil
}

// picklerGetattr binds the Pickler instance methods (dump, clear_memo)
// on attribute lookup.
//
// CPython: Modules/_pickle.c:7280 Pickler_methods
func picklerGetattr(o objects.Object, name objects.Object) (objects.Object, error) {
	p, ok := o.(*Pickler)
	if !ok {
		return objects.GenericGetAttr(o, name)
	}
	n, ok := name.(*objects.Unicode)
	if !ok {
		return objects.GenericGetAttr(o, name)
	}
	switch n.Value() {
	case "dump":
		return objects.NewBuiltinFunction("dump", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return picklerDumpMethod(p, args)
		}), nil
	case "clear_memo":
		return objects.NewBuiltinFunction("clear_memo", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if len(args) != 0 {
				return nil, fmt.Errorf("TypeError: clear_memo() takes no arguments (got %d)", len(args))
			}
			// CPython's Pickler holds memo across .dump() calls. Our
			// implementation allocates a fresh pickler per .dump() so
			// clear_memo is a no-op; it stays exposed so callers that
			// invoke it for side effects do not raise.
			return objects.None(), nil
		}), nil
	case "bin":
		return objects.NewBool(p.proto > 0), nil
	case "proto":
		return objects.NewInt(int64(p.proto)), nil
	}
	return nil, fmt.Errorf("AttributeError: 'Pickler' object has no attribute %q", n.Value())
}

// picklerDumpMethod runs the encoder for `self.dump(obj)` and writes
// the resulting bytes to the file.
//
// CPython: Modules/_pickle.c:4655 _pickle_Pickler_dump_impl
func picklerDumpMethod(p *Pickler, args []objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: dump() takes 1 positional argument (got %d)", len(args))
	}
	data, err := dumpsAtom(args[0], p.proto)
	if err != nil {
		return nil, err
	}
	write, err := objects.GetAttr(p.file, objects.NewStr("write"))
	if err != nil {
		return nil, errors.New("TypeError: Pickler file has no write() method")
	}
	if _, err := objects.CallOneArg(write, objects.NewBytes(data)); err != nil {
		return nil, err
	}
	return objects.None(), nil
}

// ---------------------------------------------------------------------------
// Unpickler.
// ---------------------------------------------------------------------------

// Unpickler is the Python `_pickle.Unpickler` instance. Like Pickler
// it just holds the file reference, the decoder state is allocated
// fresh on every .load() call.
//
// CPython: Modules/_pickle.c:843 UnpicklerObject
type Unpickler struct {
	objects.Header

	file objects.Object
}

var unpicklerType *objects.Type

func init() {
	picklerType = objects.NewType("Pickler", []*objects.Type{objects.ObjectType()})
	picklerType.Getattro = picklerGetattr
	picklerType.TpNew = picklerTpNew

	unpicklerType = objects.NewType("Unpickler", []*objects.Type{objects.ObjectType()})
	unpicklerType.Getattro = unpicklerGetattr
	unpicklerType.TpNew = unpicklerTpNew
}

// unpicklerTpNew constructs an Unpickler from `(file, *,
// fix_imports=True, encoding='ASCII', errors='strict', buffers=None)`.
//
// CPython: Modules/_pickle.c:7426 _pickle_Unpickler___init___impl
func unpicklerTpNew(_ *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: Unpickler() takes 1 positional argument (got %d)", len(args))
	}
	file := args[0]
	if _, err := objects.GetAttr(file, objects.NewStr("read")); err != nil {
		return nil, errors.New("TypeError: Unpickler() file argument has no read() method")
	}
	for _, ignored := range []string{"fix_imports", "encoding", "errors", "buffers"} {
		delete(kwargs, ignored)
	}
	for k := range kwargs {
		return nil, fmt.Errorf("TypeError: Unpickler() got an unexpected keyword argument %q", k)
	}
	u := &Unpickler{file: file}
	u.Init(unpicklerType)
	return u, nil
}

// unpicklerGetattr binds the Unpickler instance methods (load,
// persistent_load, memo).
//
// CPython: Modules/_pickle.c:7180 Unpickler_methods
func unpicklerGetattr(o objects.Object, name objects.Object) (objects.Object, error) {
	u, ok := o.(*Unpickler)
	if !ok {
		return objects.GenericGetAttr(o, name)
	}
	n, ok := name.(*objects.Unicode)
	if !ok {
		return objects.GenericGetAttr(o, name)
	}
	switch n.Value() {
	case "load":
		return objects.NewBuiltinFunction("load", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return unpicklerLoadMethod(u, args)
		}), nil
	}
	return nil, fmt.Errorf("AttributeError: 'Unpickler' object has no attribute %q", n.Value())
}

// unpicklerLoadMethod runs `self.load()`: read the whole stream via
// file.read(-1) and route it through the decoder.
//
// CPython: Modules/_pickle.c:6950 _pickle_Unpickler_load_impl
func unpicklerLoadMethod(u *Unpickler, args []objects.Object) (objects.Object, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("TypeError: load() takes no arguments (got %d)", len(args))
	}
	read, err := objects.GetAttr(u.file, objects.NewStr("read"))
	if err != nil {
		return nil, errors.New("TypeError: Unpickler file has no read() method")
	}
	out, err := objects.CallOneArg(read, objects.NewInt(-1))
	if err != nil {
		return nil, err
	}
	buf, err := asReadBuffer(out)
	if err != nil {
		return nil, err
	}
	return loadsAtom(buf)
}
