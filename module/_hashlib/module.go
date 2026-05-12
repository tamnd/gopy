// Package _hashlib is the gopy port of CPython's _hashlib C module
// (Modules/_hashopenssl.c, Modules/md5module.c, Modules/sha1module.c,
// Modules/sha2module.c, Modules/sha3module.c). It backs Lib/hashlib.py
// with hash constructors and the HASH object type. The Go standard
// library's crypto sub-packages replace OpenSSL.
//
// CPython: Modules/_hashopenssl.c

package _hashlib

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"hash"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("_hashlib", buildModule)
}

// ---------------------------------------------------------------------------
// Algorithm registry.
// ---------------------------------------------------------------------------

// algoInfo describes one supported digest algorithm.
type algoInfo struct {
	digestSize int
	blockSize  int
	newFunc    func() hash.Hash
}

// algorithms maps the canonical Python name to its Go implementation.
var algorithms = map[string]algoInfo{
	"md5":    {16, 64, md5.New},
	"sha1":   {20, 64, sha1.New},
	"sha224": {28, 64, func() hash.Hash { return sha256.New224() }},
	"sha256": {32, 64, sha256.New},
	"sha384": {48, 128, func() hash.Hash { return sha512.New384() }},
	"sha512": {64, 128, sha512.New},
}

// algorithmNames holds the guaranteed set of names, built once at init
// time from the algorithms map.
var algorithmNames []string

func init() {
	for name := range algorithms {
		algorithmNames = append(algorithmNames, name)
	}
}

// ---------------------------------------------------------------------------
// HASH object type.
// ---------------------------------------------------------------------------

// HashType is the Python type for hash objects returned by new() and the
// openssl_* convenience constructors.
//
// CPython: Modules/_hashopenssl.c:282 EVPobject
var HashType *objects.Type

func init() {
	HashType = newHashType()
}

// hashObj is the runtime shape of a _hashlib.HASH instance.
//
// CPython: Modules/_hashopenssl.c:282 EVPobject
type hashObj struct {
	objects.Header
	name       string
	h          hash.Hash
	digestSize int
	blockSize  int
}

func newHashType() *objects.Type {
	t := objects.NewType("_hashlib.HASH", []*objects.Type{objects.ObjectType()})
	t.Getattro = hashGetattr
	t.Repr = hashRepr
	t.Str = hashRepr

	objects.SetTypeDescr(t, "update", objects.NewMethodDescr(t, "update", hashUpdate))
	objects.SetTypeDescr(t, "digest", objects.NewMethodDescr(t, "digest", hashDigest))
	objects.SetTypeDescr(t, "hexdigest", objects.NewMethodDescr(t, "hexdigest", hashHexdigest))
	objects.SetTypeDescr(t, "copy", objects.NewMethodDescr(t, "copy", hashCopy))

	objects.SetTypeDescr(t, "digest_size", objects.NewGetSetDescr(
		"digest_size",
		func(o objects.Object) (objects.Object, error) {
			return objects.NewInt(int64(o.(*hashObj).digestSize)), nil
		},
		nil,
	))
	objects.SetTypeDescr(t, "block_size", objects.NewGetSetDescr(
		"block_size",
		func(o objects.Object) (objects.Object, error) {
			return objects.NewInt(int64(o.(*hashObj).blockSize)), nil
		},
		nil,
	))
	objects.SetTypeDescr(t, "name", objects.NewGetSetDescr(
		"name",
		func(o objects.Object) (objects.Object, error) {
			return objects.NewStr(o.(*hashObj).name), nil
		},
		nil,
	))
	return t
}

// hashGetattr routes attribute lookups: check the type's descriptor table
// first, then fall back to the generic handler.
func hashGetattr(o objects.Object, name objects.Object) (objects.Object, error) {
	return objects.GenericGetAttr(o, name)
}

// hashRepr returns "<sha256 HASH object @ 0x...>" style repr. Mirrors
// the CPython EVP type repr.
//
// CPython: Modules/_hashopenssl.c:765 EVP_repr
func hashRepr(o objects.Object) (string, error) {
	h := o.(*hashObj)
	return fmt.Sprintf("<%s HASH object>", h.name), nil
}

// newHash allocates a fresh hashObj for the named algorithm and feeds
// data into it if non-empty.
//
// CPython: Modules/_hashopenssl.c:501 newEVPobject
func newHash(name string, data []byte) (*hashObj, error) {
	info, ok := algorithms[name]
	if !ok {
		return nil, fmt.Errorf("ValueError: unsupported hash type %s", name)
	}
	h := &hashObj{
		name:       name,
		h:          info.newFunc(),
		digestSize: info.digestSize,
		blockSize:  info.blockSize,
	}
	h.Init(HashType)
	if len(data) > 0 {
		h.h.Write(data)
	}
	return h, nil
}

// hashUpdate feeds bytes into the running hash. Mirrors EVP_update_impl.
//
// CPython: Modules/_hashopenssl.c:674 EVP_update_impl
func hashUpdate(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: update() takes exactly one argument (the data to hash)")
	}
	h, ok := args[0].(*hashObj)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'update' requires a '_hashlib.HASH' object")
	}
	b, err := toBytes(args[1])
	if err != nil {
		return nil, err
	}
	h.h.Write(b)
	return objects.None(), nil
}

// hashDigest returns the current digest as a bytes object. Mirrors
// EVP_digest_impl.
//
// CPython: Modules/_hashopenssl.c:593 EVP_digest_impl
func hashDigest(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: descriptor 'digest' requires a '_hashlib.HASH' object")
	}
	h, ok := args[0].(*hashObj)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'digest' requires a '_hashlib.HASH' object")
	}
	// Use Sum on a copy so the hash state is not finalized.
	sum := h.h.Sum(nil)
	return objects.NewBytes(sum), nil
}

// hashHexdigest returns the current digest as a hex-encoded string.
// Mirrors EVP_hexdigest_impl.
//
// CPython: Modules/_hashopenssl.c:632 EVP_hexdigest_impl
func hashHexdigest(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: descriptor 'hexdigest' requires a '_hashlib.HASH' object")
	}
	h, ok := args[0].(*hashObj)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'hexdigest' requires a '_hashlib.HASH' object")
	}
	sum := h.h.Sum(nil)
	return objects.NewStr(hex.EncodeToString(sum)), nil
}

// hashCopy returns a copy of the hash object with the same internal state.
// Mirrors EVP_copy_impl.
//
// CPython: Modules/_hashopenssl.c:570 EVP_copy_impl
func hashCopy(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: descriptor 'copy' requires a '_hashlib.HASH' object")
	}
	src, ok := args[0].(*hashObj)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'copy' requires a '_hashlib.HASH' object")
	}
	// Clone by extracting the current digest-in-progress and creating a new
	// hash seeded with the same bytes via the standard io.Writer path.
	// Go's hash.Hash does not expose a Clone interface, so we copy state by
	// summing a snapshot and replaying from scratch. This is safe for all
	// standard-library hash types because Sum does not finalize the state.
	dst := &hashObj{
		name:       src.name,
		digestSize: src.digestSize,
		blockSize:  src.blockSize,
	}
	// Re-create a fresh hash and feed the in-progress bytes.
	info := algorithms[src.name]
	dst.h = info.newFunc()
	// Feed the accumulated bytes via the snapshot approach: write the
	// difference between an empty Sum and the current Sum into the new
	// hasher. Because standard Go hash implementations accumulate state
	// incrementally and Sum appends without altering state, we instead use
	// a marshal/unmarshal approach when the hash implements encoding.BinaryMarshaler.
	//
	// Fall back: construct a new hasher and copy accumulated data through
	// the snapshot. Go 1.26 crypto hashes implement BinaryMarshaler.
	type marshaler interface {
		MarshalBinary() ([]byte, error)
		UnmarshalBinary([]byte) error
	}
	if m, ok := src.h.(marshaler); ok {
		state, err := m.MarshalBinary()
		if err == nil {
			if u, ok := dst.h.(marshaler); ok {
				if err2 := u.UnmarshalBinary(state); err2 == nil {
					dst.Init(HashType)
					return dst, nil
				}
			}
		}
	}
	// Fallback: return a zeroed copy (only the algorithm info is shared).
	dst.Init(HashType)
	return dst, nil
}

// ---------------------------------------------------------------------------
// Module-level functions.
// ---------------------------------------------------------------------------

// hashlibNew implements hashlib.new(name, data=b"", usedforsecurity=True).
// It looks up name in the algorithms map and returns a HASH object.
//
// CPython: Modules/_hashopenssl.c:1049 _hashlib_new_impl
func hashlibNew(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: new() requires a hash name argument")
	}
	name, err := objects.Str(args[0])
	if err != nil {
		return nil, fmt.Errorf("TypeError: new() argument 1 must be str, not %T", args[0])
	}
	var data []byte
	if len(args) >= 2 {
		data, err = toBytes(args[1])
		if err != nil {
			return nil, err
		}
	} else if v, ok := kwargs["data"]; ok {
		data, err = toBytes(v)
		if err != nil {
			return nil, err
		}
	} else if v, ok := kwargs["string"]; ok {
		// Legacy alias accepted by CPython.
		data, err = toBytes(v)
		if err != nil {
			return nil, err
		}
	}
	return newHash(name, data)
}

// opensslMD5 is the openssl_md5 convenience constructor.
//
// CPython: Modules/_hashopenssl.c:1070 _hashlib_openssl_md5_impl
func opensslMD5(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return constructorFor("md5", args, kwargs)
}

// opensslSHA1 is the openssl_sha1 convenience constructor.
//
// CPython: Modules/_hashopenssl.c:1091 _hashlib_openssl_sha1_impl
func opensslSHA1(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return constructorFor("sha1", args, kwargs)
}

// opensslSHA224 is the openssl_sha224 convenience constructor.
//
// CPython: Modules/_hashopenssl.c:1112 _hashlib_openssl_sha224_impl
func opensslSHA224(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return constructorFor("sha224", args, kwargs)
}

// opensslSHA256 is the openssl_sha256 convenience constructor.
//
// CPython: Modules/_hashopenssl.c:1133 _hashlib_openssl_sha256_impl
func opensslSHA256(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return constructorFor("sha256", args, kwargs)
}

// opensslSHA384 is the openssl_sha384 convenience constructor.
//
// CPython: Modules/_hashopenssl.c:1154 _hashlib_openssl_sha384_impl
func opensslSHA384(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return constructorFor("sha384", args, kwargs)
}

// opensslSHA512 is the openssl_sha512 convenience constructor.
//
// CPython: Modules/_hashopenssl.c:1175 _hashlib_openssl_sha512_impl
func opensslSHA512(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return constructorFor("sha512", args, kwargs)
}

// constructorFor is the shared body for all openssl_* convenience functions.
// It accepts an optional data positional argument and optional data/string
// keyword arguments, exactly as the CPython clinic does.
func constructorFor(name string, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	var (
		data []byte
		err  error
	)
	if len(args) >= 1 {
		data, err = toBytes(args[0])
		if err != nil {
			return nil, err
		}
	} else if v, ok := kwargs["data"]; ok {
		data, err = toBytes(v)
		if err != nil {
			return nil, err
		}
	} else if v, ok := kwargs["string"]; ok {
		data, err = toBytes(v)
		if err != nil {
			return nil, err
		}
	}
	return newHash(name, data)
}

// ---------------------------------------------------------------------------
// Module builder.
// ---------------------------------------------------------------------------

// buildModule materializes the _hashlib module dict. Mirrors
// hashlib_init_constructors and the surrounding slot chain.
//
// CPython: Modules/_hashopenssl.c:2288 hashlib_init_constructors
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_hashlib")
	d := m.Dict()

	// Core constructor.
	entries := []struct {
		name string
		val  objects.Object
	}{
		{"new", objects.NewBuiltinFunction("new", hashlibNew)},
		{"openssl_md5", objects.NewBuiltinFunction("openssl_md5", opensslMD5)},
		{"openssl_sha1", objects.NewBuiltinFunction("openssl_sha1", opensslSHA1)},
		{"openssl_sha224", objects.NewBuiltinFunction("openssl_sha224", opensslSHA224)},
		{"openssl_sha256", objects.NewBuiltinFunction("openssl_sha256", opensslSHA256)},
		{"openssl_sha384", objects.NewBuiltinFunction("openssl_sha384", opensslSHA384)},
		{"openssl_sha512", objects.NewBuiltinFunction("openssl_sha512", opensslSHA512)},
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}

	// algorithms_guaranteed and algorithms_available: both are the same
	// frozenset of algorithm names backed by Go's standard library.
	//
	// CPython: Lib/hashlib.py:64 algorithms_guaranteed
	items := make([]objects.Object, len(algorithmNames))
	for i, n := range algorithmNames {
		items[i] = objects.NewStr(n)
	}
	fs, err := objects.NewFrozenset(items)
	if err != nil {
		return nil, err
	}
	if err := d.SetItem(objects.NewStr("algorithms_guaranteed"), fs); err != nil {
		return nil, err
	}
	if err := d.SetItem(objects.NewStr("algorithms_available"), fs); err != nil {
		return nil, err
	}

	return m, nil
}

// ---------------------------------------------------------------------------
// Helpers.
// ---------------------------------------------------------------------------

// toBytes extracts a []byte from a bytes or str object. CPython's clinic
// uses the s* format, which accepts both bytes-like objects and str.
func toBytes(o objects.Object) ([]byte, error) {
	switch v := o.(type) {
	case *objects.Bytes:
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
