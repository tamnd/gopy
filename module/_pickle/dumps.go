// Module entry points for the proto-5 encoder. Phase 4 of P14.1
// wires `_pickle.dumps(obj, protocol=None)` and
// `_pickle.dump(obj, file, protocol=None)` onto the module dict so
// Python code can drive the Go-native encoder. The function shape
// matches CPython's argument-clinic signatures: `dumps` returns a
// bytes object, `dump` writes to a file-like via `file.write(bytes)`.
//
// pickle.py guards its C-import with a try/except ImportError, so
// merely exposing `dumps` is not enough to flip pickle.dumps over
// to the C path. The full set (Pickler / Unpickler / dump / dumps /
// load / loads) has to resolve before the second
// `from _pickle import (...)` block in pickle.py succeeds. Phase 4
// publishes `dumps` / `dump` directly, the Pickler class and the
// decoder land in later phases.
//
// CPython: Modules/_pickle.c:7865 _pickle_dumps_impl
// CPython: Modules/_pickle.c:7799 _pickle_dump_impl

package _pickle

import (
	"errors"
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// pickleDumps is the Python-callable entry. The C signature is
// `dumps(obj, protocol=None, *, fix_imports=True, buffer_callback=None)`.
// fix_imports / buffer_callback are accepted and currently ignored
// since they only matter for proto < 3 (fix_imports) and out-of-band
// buffers (buffer_callback), neither of which the proto-5 byte-
// equality fixtures exercise.
//
// CPython: Modules/_pickle.c:7865 _pickle_dumps_impl
func pickleDumps(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("TypeError: dumps() takes 1 or 2 positional arguments (got %d)", len(args))
	}
	obj := args[0]
	proto, err := resolveProtocol(args, kwargs, 1)
	if err != nil {
		return nil, err
	}
	if _, ok := kwargs["fix_imports"]; ok {
		// Accepted for signature parity; proto >= 3 ignores it.
		delete(kwargs, "fix_imports")
	}
	if _, ok := kwargs["buffer_callback"]; ok {
		// Accepted for signature parity; out-of-band buffers not
		// implemented yet.
		delete(kwargs, "buffer_callback")
	}
	for k := range kwargs {
		return nil, fmt.Errorf("TypeError: dumps() got an unexpected keyword argument %q", k)
	}
	data, err := dumpsAtom(obj, proto)
	if err != nil {
		return nil, err
	}
	return objects.NewBytes(data), nil
}

// pickleDump is the file-output variant. The C signature is
// `dump(obj, file, protocol=None, *, fix_imports=True, buffer_callback=None)`.
// The file object must expose a `.write(bytes)` method; pickle.py
// uses io.BufferedWriter or io.BytesIO, both of which satisfy that.
//
// CPython: Modules/_pickle.c:7799 _pickle_dump_impl
func pickleDump(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("TypeError: dump() takes 2 or 3 positional arguments (got %d)", len(args))
	}
	obj := args[0]
	file := args[1]
	proto, err := resolveProtocol(args, kwargs, 2)
	if err != nil {
		return nil, err
	}
	if _, ok := kwargs["fix_imports"]; ok {
		delete(kwargs, "fix_imports")
	}
	if _, ok := kwargs["buffer_callback"]; ok {
		delete(kwargs, "buffer_callback")
	}
	for k := range kwargs {
		return nil, fmt.Errorf("TypeError: dump() got an unexpected keyword argument %q", k)
	}
	data, err := dumpsAtom(obj, proto)
	if err != nil {
		return nil, err
	}
	write, err := objects.GetAttr(file, objects.NewStr("write"))
	if err != nil {
		return nil, errors.New("TypeError: dump() file argument has no write() method")
	}
	if _, err := objects.CallOneArg(write, objects.NewBytes(data)); err != nil {
		return nil, err
	}
	return objects.None(), nil
}

// resolveProtocol picks the protocol value from the positional slot
// at `protoArgIdx` (None / missing -> DEFAULT_PROTOCOL) or from the
// `protocol` keyword argument. CPython performs the same lookup via
// argparse clinic; we replicate it inline.
//
// CPython: Modules/_pickle.c:1391 _Pickler_SetProtocol
func resolveProtocol(args []objects.Object, kwargs map[string]objects.Object, protoArgIdx int) (int, error) {
	var raw objects.Object
	if len(args) > protoArgIdx {
		raw = args[protoArgIdx]
	}
	if kv, ok := kwargs["protocol"]; ok {
		if raw != nil {
			return 0, errors.New("TypeError: argument for 'protocol' given by name and position")
		}
		raw = kv
		delete(kwargs, "protocol")
	}
	if raw == nil || objects.IsNone(raw) {
		return defaultProtocol, nil
	}
	i, ok := raw.(*objects.Int)
	if !ok {
		return 0, errors.New("TypeError: an integer is required")
	}
	val, fits := i.Int64()
	if !fits {
		return 0, errors.New("OverflowError: protocol out of range")
	}
	if val < 0 {
		// CPython: protocol < 0 picks HIGHEST_PROTOCOL.
		return highestProtocol, nil
	}
	if val > int64(highestProtocol) {
		return 0, fmt.Errorf("ValueError: pickle protocol must be <= %d", highestProtocol)
	}
	return int(val), nil
}
