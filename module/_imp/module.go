// Package _imp is the gopy port of CPython's Modules/_imp module (the
// builtin half lives in Python/import.c). Only the slice consumed by
// the vendored importlib._bootstrap_external is materialized:
//
//   - source_hash(key, source)           Python/import.c:4869
//   - pyc_magic_number_token (int)       Python/import.c:4926
//   - check_hash_based_pycs (str)        Python/import.c:4920
//
// The rest of the C module (lock_held, find_frozen, create_builtin,
// ...) is intentionally absent — gopy's own imp package already serves
// those roles and importlib does not need _imp to reach them.
//
// CPython: Python/import.c:4943 imp_module
package _imp

import (
	"encoding/binary"
	"fmt"

	"github.com/tamnd/gopy/hash"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/marshal"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("_imp", buildModule)
}

func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_imp")
	d := m.Dict()

	if err := d.SetItem(objects.NewStr("source_hash"), objects.NewBuiltinFunction("source_hash", sourceHash)); err != nil {
		return nil, err
	}
	if err := d.SetItem(objects.NewStr("pyc_magic_number_token"), objects.NewInt(int64(marshal.MagicNumber))); err != nil {
		return nil, err
	}
	// check_hash_based_pycs mirrors PyConfig.check_hash_based_pycs. CPython
	// defaults to "default" which means: honor the flag bit in the .pyc
	// header. gopy currently has no override path so always reports the
	// default value.
	//
	// CPython: Python/import.c:4920 imp_module_exec
	if err := d.SetItem(objects.NewStr("check_hash_based_pycs"), objects.NewStr("default")); err != nil {
		return nil, err
	}
	return m, nil
}

// sourceHash mirrors _imp.source_hash(key, source). It hashes the
// source buffer with SipHash-1-3 keyed by `key` and returns the result
// as 8 little-endian bytes.
//
// CPython: Python/import.c:4869 _imp_source_hash_impl
func sourceHash(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: source_hash() takes no keyword arguments")
	}
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: source_hash() takes 2 positional arguments but %d were given", len(args))
	}
	keyInt, ok := args[0].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: source_hash() key must be int, not '%T'", args[0])
	}
	key, ok := keyInt.Int64()
	if !ok {
		return nil, fmt.Errorf("OverflowError: source_hash() key too large for int64")
	}
	src, err := toBuffer(args[1])
	if err != nil {
		return nil, err
	}
	h := hash.KeyedHash(uint64(key), src)
	var out [8]byte
	binary.LittleEndian.PutUint64(out[:], h)
	return objects.NewBytes(out[:]), nil
}

// toBuffer extracts a []byte from a bytes-like object. CPython's
// Py_buffer protocol accepts bytes, bytearray, and any object with a
// readable buffer; the gopy port covers what importlib actually feeds
// in (bytes / bytearray / str).
func toBuffer(o objects.Object) ([]byte, error) {
	switch v := o.(type) {
	case *objects.Bytes:
		return v.Bytes(), nil
	case *objects.ByteArray:
		return v.Bytes(), nil
	case *objects.Unicode:
		s, err := objects.Str(o)
		if err != nil {
			return nil, err
		}
		return []byte(s), nil
	}
	return nil, fmt.Errorf("TypeError: a bytes-like object is required, not '%T'", o)
}
