// Package _hashlib is the gopy port of CPython's _hashlib C module
// (Modules/_hashopenssl.c, Modules/md5module.c, Modules/sha1module.c,
// Modules/sha2module.c, Modules/sha3module.c). It backs Lib/hashlib.py
// with hash constructors and the HASH object type. The Go standard
// library's crypto sub-packages replace OpenSSL.
//
// CPython: Modules/_hashopenssl.c

package _hashlib

import (
	cryptohmac "crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha3"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"hash"
	"io"

	pyerrors "github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

// UnsupportedDigestmodError is raised when a digestmod is not available.
// It subclasses ValueError, mirroring CPython.
//
// CPython: Modules/_hashopenssl.c:2338 UnsupportedDigestmodError
var unsupportedDigestmodError = pyerrors.NewExcType("_hashlib.UnsupportedDigestmodError", []*objects.Type{pyerrors.PyExc_ValueError})

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
	"md5":      {16, 64, md5.New},
	"sha1":     {20, 64, sha1.New},
	"sha224":   {28, 64, func() hash.Hash { return sha256.New224() }},
	"sha256":   {32, 64, sha256.New},
	"sha384":   {48, 128, func() hash.Hash { return sha512.New384() }},
	"sha512":   {64, 128, sha512.New},
	"sha3_224": {28, 144, func() hash.Hash { return sha3.New224() }},
	"sha3_256": {32, 136, func() hash.Hash { return sha3.New256() }},
	"sha3_384": {48, 104, func() hash.Hash { return sha3.New384() }},
	"sha3_512": {64, 72, func() hash.Hash { return sha3.New512() }},
}

// shakeAlgoInfo describes a SHAKE (XOF) algorithm.
type shakeAlgoInfo struct {
	blockSize int
	newFunc   func() *sha3.SHAKE
}

// shakeAlgorithms maps SHAKE algorithm names.
//
// CPython: Modules/sha3module.c SHA3_sha3_224Type / shake_128 / shake_256
var shakeAlgorithms = map[string]shakeAlgoInfo{
	"shake_128": {168, sha3.NewSHAKE128},
	"shake_256": {136, sha3.NewSHAKE256},
}

// algorithmNames holds the guaranteed set of names (fixed-length + XOF),
// built once at init time.
var algorithmNames []string

func init() {
	for name := range algorithms {
		algorithmNames = append(algorithmNames, name)
	}
	for name := range shakeAlgorithms {
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
// For XOF types (shake_128, shake_256) the xof field is non-nil and h
// is nil; digest/hexdigest require an explicit length argument.
//
// CPython: Modules/_hashopenssl.c:282 EVPobject
type hashObj struct {
	objects.Header
	name       string
	h          hash.Hash
	xof        *sha3.SHAKE // non-nil for XOF (SHAKE) hashes
	digestSize int            // 0 for XOF hashes
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

// ---------------------------------------------------------------------------
// HMAC object type.
// ---------------------------------------------------------------------------

// HMACType is the Python type for HMAC objects returned by hmac_new.
//
// CPython: Modules/_hashopenssl.c:1907 HMACType
var HMACType *objects.Type

func init() {
	HMACType = newHMACType()
}

// hmacObjGoType holds the Go hash constructor (for copy/replay).
type hmacObjGoType struct {
	newFunc func() hash.Hash
}

// hmacObjState carries the per-instance HMAC state.
//
// CPython: Modules/_hashopenssl.c:288 HMACobject
type hmacObjState struct {
	objects.Header
	name       string
	key        []byte
	data       []byte // all bytes fed via update() – used by copy()
	h          hash.Hash
	goType     hmacObjGoType
	digestSize int
	blockSize  int
}

func newHMACType() *objects.Type {
	t := objects.NewType("_hashlib.HMAC", []*objects.Type{objects.ObjectType()})
	t.Getattro = func(o objects.Object, name objects.Object) (objects.Object, error) {
		return objects.GenericGetAttr(o, name)
	}
	t.Repr = func(o objects.Object) (string, error) {
		s := o.(*hmacObjState)
		// CPython: Modules/_hashopenssl.c:1766 _hashlib_HMAC_get_digest_name
		return fmt.Sprintf("<%s HMAC object @ %p>", s.name, s), nil
	}
	// Block direct instantiation: _hashlib.HMAC instances must be created
	// via hmac_new(). CPython marks this type as immutable/no-instantiation.
	//
	// CPython: Modules/_hashopenssl.c:1907 HMACType (tp_new = NULL)
	t.TpNew = func(cls *objects.Type, _ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return nil, fmt.Errorf("TypeError: cannot create '_hashlib.HMAC' instances")
	}
	objects.SetTypeDescr(t, "__new__", objects.NewBuiltinFunction("__new__", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return nil, fmt.Errorf("TypeError: cannot create '_hashlib.HMAC' instances")
	}))

	objects.SetTypeDescr(t, "update", objects.NewMethodDescr(t, "update", hmacUpdate))
	objects.SetTypeDescr(t, "digest", objects.NewMethodDescr(t, "digest", hmacDigest))
	objects.SetTypeDescr(t, "hexdigest", objects.NewMethodDescr(t, "hexdigest", hmacHexdigest))
	objects.SetTypeDescr(t, "copy", objects.NewMethodDescr(t, "copy", hmacCopy))

	objects.SetTypeDescr(t, "digest_size", objects.NewGetSetDescr(
		"digest_size",
		func(o objects.Object) (objects.Object, error) {
			return objects.NewInt(int64(o.(*hmacObjState).digestSize)), nil
		},
		nil,
	))
	objects.SetTypeDescr(t, "block_size", objects.NewGetSetDescr(
		"block_size",
		func(o objects.Object) (objects.Object, error) {
			return objects.NewInt(int64(o.(*hmacObjState).blockSize)), nil
		},
		nil,
	))
	objects.SetTypeDescr(t, "name", objects.NewGetSetDescr(
		"name",
		func(o objects.Object) (objects.Object, error) {
			return objects.NewStr("hmac-" + o.(*hmacObjState).name), nil
		},
		nil,
	))
	return t
}

// newHMACObj creates an hmacObjState for the named algorithm, initialised with key and optional msg.
//
// CPython: Modules/_hashopenssl.c:1577 _hashlib_hmac_new_impl
func newHMACObj(algoName string, key, msg []byte) (*hmacObjState, error) {
	info, ok := algorithms[algoName]
	if !ok {
		return nil, fmt.Errorf("ValueError: unsupported hash type %s", algoName)
	}
	goType := hmacObjGoType{newFunc: info.newFunc}
	h := cryptohmac.New(info.newFunc, key)
	s := &hmacObjState{
		name:       algoName,
		key:        append([]byte(nil), key...),
		h:          h,
		goType:     goType,
		digestSize: info.digestSize,
		blockSize:  info.blockSize,
	}
	s.Init(HMACType)
	if len(msg) > 0 {
		h.Write(msg)
		s.data = append(s.data, msg...)
	}
	return s, nil
}

// resolveAlgoName resolves a Python digestmod (str or callable openssl_xxx) to an algorithm name.
// Returns unsupportedDigestmodError for unknown digestmods.
//
// CPython: Modules/_hashopenssl.c:1577 _hashlib_hmac_new_impl
func resolveAlgoName(digestmod objects.Object) (string, error) {
	if digestmod == nil {
		// CPython: Modules/_hashopenssl.c:1605 - raises TypeError for missing digestmod
		return "", fmt.Errorf("TypeError: Missing required parameter 'digestmod'.")
	}
	if digestmod == objects.None() {
		msg := "unsupported digestmod: None"
		exc := pyerrors.New(unsupportedDigestmodError, objects.NewTuple([]objects.Object{objects.NewStr(msg)}))
		return "", objects.NewRaisedError(exc, msg)
	}
	switch v := digestmod.(type) {
	case *objects.Unicode:
		name, _ := objects.Str(v)
		// Normalize aliases like "SHA256" -> "sha256", "SHA-256" -> "sha256"
		name = normalizeAlgoName(name)
		if _, ok := algorithms[name]; ok {
			return name, nil
		}
		msg := fmt.Sprintf("unsupported digestmod: %s", name)
		exc := pyerrors.New(unsupportedDigestmodError, objects.NewTuple([]objects.Object{objects.NewStr(msg)}))
		return "", objects.NewRaisedError(exc, msg)
	case *objects.BuiltinFunction:
		// openssl_sha256 -> "sha256", openssl_md5 -> "md5"
		fname := v.Name
		const prefix = "openssl_"
		if len(fname) > len(prefix) && fname[:len(prefix)] == prefix {
			name := fname[len(prefix):]
			if _, ok := algorithms[name]; ok {
				return name, nil
			}
		}
		msg := fmt.Sprintf("unsupported digestmod function: %s", fname)
		exc := pyerrors.New(unsupportedDigestmodError, objects.NewTuple([]objects.Object{objects.NewStr(msg)}))
		return "", objects.NewRaisedError(exc, msg)
	default:
		msg := fmt.Sprintf("unsupported digestmod type: %T", digestmod)
		exc := pyerrors.New(unsupportedDigestmodError, objects.NewTuple([]objects.Object{objects.NewStr(msg)}))
		return "", objects.NewRaisedError(exc, msg)
	}
}

// normalizeAlgoName downcases and strips common prefixes/dashes.
func normalizeAlgoName(name string) string {
	// lowercase
	out := make([]byte, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		out[i] = c
	}
	s := string(out)
	// strip dashes: "sha-256" -> "sha256"
	result := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '-' {
			result = append(result, s[i])
		}
	}
	return string(result)
}

func hmacUpdate(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: update() requires a msg argument")
	}
	s, ok := args[0].(*hmacObjState)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'update' requires a '_hashlib.HMAC' object")
	}
	data, ok2 := bytesLike(args[1])
	if !ok2 {
		return nil, fmt.Errorf("TypeError: argument must be a bytes-like object, not '%T'", args[1])
	}
	s.h.Write(data)
	s.data = append(s.data, data...)
	return objects.None(), nil
}

func hmacDigest(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	s, ok := args[0].(*hmacObjState)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'digest' requires a '_hashlib.HMAC' object")
	}
	return objects.NewBytes(s.h.Sum(nil)), nil
}

func hmacHexdigest(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	s, ok := args[0].(*hmacObjState)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'hexdigest' requires a '_hashlib.HMAC' object")
	}
	return objects.NewStr(hex.EncodeToString(s.h.Sum(nil))), nil
}

func hmacCopy(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	s, ok := args[0].(*hmacObjState)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'copy' requires a '_hashlib.HMAC' object")
	}
	// CPython: Modules/_hashopenssl.c:1708 _hashlib_HMAC_copy_impl
	// Replay key + data into a fresh HMAC to clone state.
	h2 := cryptohmac.New(s.goType.newFunc, s.key)
	h2.Write(s.data)
	c := &hmacObjState{
		name:       s.name,
		key:        append([]byte(nil), s.key...),
		data:       append([]byte(nil), s.data...),
		h:          h2,
		goType:     s.goType,
		digestSize: s.digestSize,
		blockSize:  s.blockSize,
	}
	c.Init(HMACType)
	return c, nil
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
// data into it if non-empty. Handles both fixed-length and XOF types.
//
// CPython: Modules/_hashopenssl.c:501 newEVPobject
// CPython: Modules/sha3module.c py_sha3_new
func newHash(name string, data []byte) (*hashObj, error) {
	if sinfo, ok := shakeAlgorithms[name]; ok {
		h := &hashObj{
			name:      name,
			xof:       sinfo.newFunc(),
			blockSize: sinfo.blockSize,
		}
		h.Init(HashType)
		if len(data) > 0 {
			h.xof.Write(data)
		}
		return h, nil
	}
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

// Ensure io is used (needed for XOF Read calls inside digest/hexdigest).
var _ io.Writer = (*hashObj)(nil)

// Write implements io.Writer so hashObj satisfies the interface when h is nil.
func (ho *hashObj) Write(p []byte) (int, error) {
	if ho.xof != nil {
		return ho.xof.Write(p)
	}
	return ho.h.Write(p)
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
	if h.xof != nil {
		h.xof.Write(b)
	} else {
		h.h.Write(b)
	}
	return objects.None(), nil
}

// hashDigest returns the current digest as a bytes object. For XOF hashes
// (shake_128, shake_256), a length argument is required.
//
// CPython: Modules/_hashopenssl.c:593 EVP_digest_impl
// CPython: Modules/sha3module.c:576 py_shake_128_digest
func hashDigest(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: descriptor 'digest' requires a '_hashlib.HASH' object")
	}
	h, ok := args[0].(*hashObj)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'digest' requires a '_hashlib.HASH' object")
	}
	if h.xof != nil {
		if len(args) < 2 {
			return nil, fmt.Errorf("TypeError: digest() requires a length argument for %s", h.name)
		}
		length, err := xofLength(args[1])
		if err != nil {
			return nil, err
		}
		out := make([]byte, length)
		c := cloneShake(h)
		c.xof.Read(out)
		return objects.NewBytes(out), nil
	}
	sum := h.h.Sum(nil)
	return objects.NewBytes(sum), nil
}

// hashHexdigest returns the current digest as a hex-encoded string.
// For XOF hashes (shake_128, shake_256), a length argument is required.
//
// CPython: Modules/_hashopenssl.c:632 EVP_hexdigest_impl
// CPython: Modules/sha3module.c:596 py_shake_128_hexdigest
func hashHexdigest(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: descriptor 'hexdigest' requires a '_hashlib.HASH' object")
	}
	h, ok := args[0].(*hashObj)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'hexdigest' requires a '_hashlib.HASH' object")
	}
	if h.xof != nil {
		if len(args) < 2 {
			return nil, fmt.Errorf("TypeError: hexdigest() requires a length argument for %s", h.name)
		}
		length, err := xofLength(args[1])
		if err != nil {
			return nil, err
		}
		out := make([]byte, length)
		c := cloneShake(h)
		c.xof.Read(out)
		return objects.NewStr(hex.EncodeToString(out)), nil
	}
	sum := h.h.Sum(nil)
	return objects.NewStr(hex.EncodeToString(sum)), nil
}

// cloneShake creates a copy of a SHAKE hashObj by marshalling the XOF state.
// sha3.SHAKE implements encoding.BinaryMarshaler so state transfer is lossless.
//
// CPython: Modules/sha3module.c py_sha3_copy
func cloneShake(src *hashObj) *hashObj {
	info := shakeAlgorithms[src.name]
	newXOF := info.newFunc()
	if state, err := src.xof.MarshalBinary(); err == nil {
		_ = newXOF.UnmarshalBinary(state)
	}
	dst := &hashObj{
		name:      src.name,
		xof:       newXOF,
		blockSize: src.blockSize,
	}
	dst.Init(HashType)
	return dst
}

// xofLength extracts an integer byte-length from a Python int argument.
func xofLength(o objects.Object) (int, error) {
	i, ok := o.(*objects.Int)
	if !ok {
		return 0, fmt.Errorf("TypeError: length must be an integer, not '%T'", o)
	}
	n, ok2 := i.Int64()
	if !ok2 || n < 0 {
		return 0, fmt.Errorf("ValueError: length must be non-negative")
	}
	return int(n), nil
}

// hashCopy returns a copy of the hash object with the same internal state.
// Mirrors EVP_copy_impl. For XOF types, *sha3.SHAKE.Clone() is used.
//
// CPython: Modules/_hashopenssl.c:570 EVP_copy_impl
// CPython: Modules/sha3module.c py_sha3_copy
func hashCopy(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: descriptor 'copy' requires a '_hashlib.HASH' object")
	}
	src, ok := args[0].(*hashObj)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'copy' requires a '_hashlib.HASH' object")
	}
	dst := &hashObj{
		name:       src.name,
		digestSize: src.digestSize,
		blockSize:  src.blockSize,
	}
	if src.xof != nil {
		c := cloneShake(src)
		dst.xof = c.xof
		dst.Init(HashType)
		return dst, nil
	}
	info := algorithms[src.name]
	dst.h = info.newFunc()
	type marshaler interface {
		MarshalBinary() ([]byte, error)
		UnmarshalBinary([]byte) error
	}
	if m, ok2 := src.h.(marshaler); ok2 {
		state, err := m.MarshalBinary()
		if err == nil {
			if u, ok3 := dst.h.(marshaler); ok3 {
				if err2 := u.UnmarshalBinary(state); err2 == nil {
					dst.Init(HashType)
					return dst, nil
				}
			}
		}
	}
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

// openssl_sha3_224 / sha3_256 / sha3_384 / sha3_512 convenience constructors.
//
// CPython: Modules/sha3module.c sha3_224_new / sha3_256_new / ...
func opensslSHA3_224(args []objects.Object, kw map[string]objects.Object) (objects.Object, error) {
	return constructorFor("sha3_224", args, kw)
}
func opensslSHA3_256(args []objects.Object, kw map[string]objects.Object) (objects.Object, error) {
	return constructorFor("sha3_256", args, kw)
}
func opensslSHA3_384(args []objects.Object, kw map[string]objects.Object) (objects.Object, error) {
	return constructorFor("sha3_384", args, kw)
}
func opensslSHA3_512(args []objects.Object, kw map[string]objects.Object) (objects.Object, error) {
	return constructorFor("sha3_512", args, kw)
}

// openssl_shake_128 / openssl_shake_256 convenience constructors.
//
// CPython: Modules/sha3module.c shake_128_new / shake_256_new
func opensslShake128(args []objects.Object, kw map[string]objects.Object) (objects.Object, error) {
	return constructorFor("shake_128", args, kw)
}
func opensslShake256(args []objects.Object, kw map[string]objects.Object) (objects.Object, error) {
	return constructorFor("shake_256", args, kw)
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

// hmacDigestSingleShot computes a single-shot HMAC and returns raw bytes.
//
// CPython: Modules/_hashopenssl.c:1504 _hashlib_hmac_singleshot_impl
func hmacDigestSingleShot(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: hmac_digest() requires key and msg arguments")
	}
	key, ok2 := bytesLike(args[0])
	if !ok2 {
		return nil, fmt.Errorf("TypeError: key must be a bytes-like object, not '%T'", args[0])
	}
	msg, ok3 := bytesLike(args[1])
	if !ok3 {
		return nil, fmt.Errorf("TypeError: msg must be a bytes-like object, not '%T'", args[1])
	}
	var digestmod objects.Object
	if len(args) >= 3 {
		digestmod = args[2]
	} else if v, ok := kwargs["digest"]; ok {
		digestmod = v
	}
	algoName, err := resolveAlgoName(digestmod)
	if err != nil {
		return nil, err
	}
	info, ok := algorithms[algoName]
	if !ok {
		return nil, fmt.Errorf("ValueError: unsupported hash type %s", algoName)
	}
	h := cryptohmac.New(info.newFunc, key)
	h.Write(msg)
	return objects.NewBytes(h.Sum(nil)), nil
}

// hmacNew creates an _hashlib.HMAC object. Mirrors CPython's _hashlib_hmac_new_impl.
//
// CPython: Modules/_hashopenssl.c:1577 _hashlib_hmac_new_impl
func hmacNew(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	// Positional: key, [msg]. Keyword: digestmod=, msg=
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: hmac_new() missing required argument 'key'")
	}
	key, ok2 := bytesLike(args[0])
	if !ok2 {
		return nil, fmt.Errorf("TypeError: key must be a bytes-like object, not '%T'", args[0])
	}

	var msg []byte
	if len(args) >= 2 && args[1] != objects.None() {
		data, ok3 := bytesLike(args[1])
		if !ok3 {
			return nil, fmt.Errorf("TypeError: msg must be a bytes-like object, not '%T'", args[1])
		}
		msg = data
	} else if v, ok := kwargs["msg"]; ok && v != objects.None() {
		data, ok3 := bytesLike(v)
		if !ok3 {
			return nil, fmt.Errorf("TypeError: msg must be a bytes-like object, not '%T'", v)
		}
		msg = data
	}

	var digestmod objects.Object
	if v, ok := kwargs["digestmod"]; ok {
		digestmod = v
	} else if len(args) >= 3 {
		digestmod = args[2]
	}

	algoName, err := resolveAlgoName(digestmod)
	if err != nil {
		return nil, err
	}
	return newHMACObj(algoName, key, msg)
}

// compareDigest implements constant-time equality for str or bytes arguments.
// It is a Go port of _hashlib_compare_digest_impl and the _tscmp helper.
//
// CPython: Modules/_hashopenssl.c:2042 _tscmp
// CPython: Modules/_hashopenssl.c:2086 _hashlib_compare_digest_impl
func compareDigest(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: compare_digest() takes exactly 2 arguments (%d given)", len(args))
	}
	a, b := args[0], args[1]

	switch av := a.(type) {
	case *objects.Unicode:
		bv, ok := b.(*objects.Unicode)
		if !ok {
			return nil, fmt.Errorf("TypeError: unsupported operand types or combination of types: 'str' and '%T'", b)
		}
		sa, _ := objects.Str(av)
		sb, _ := objects.Str(bv)
		for _, r := range sa {
			if r > 127 {
				return nil, fmt.Errorf("TypeError: comparing strings with non-ASCII characters is not supported")
			}
		}
		for _, r := range sb {
			if r > 127 {
				return nil, fmt.Errorf("TypeError: comparing strings with non-ASCII characters is not supported")
			}
		}
		eq := subtle.ConstantTimeCompare([]byte(sa), []byte(sb))
		return objects.NewBool(eq == 1), nil

	case *objects.Bytes:
		bBytes, ok := bytesLike(b)
		if !ok {
			return nil, fmt.Errorf("TypeError: unsupported operand types or combination of types: 'bytes' and '%T'", b)
		}
		eq := subtle.ConstantTimeCompare(av.Bytes(), bBytes)
		return objects.NewBool(eq == 1), nil

	case *objects.ByteArray:
		bBytes, ok := bytesLike(b)
		if !ok {
			return nil, fmt.Errorf("TypeError: unsupported operand types or combination of types: 'bytearray' and '%T'", b)
		}
		eq := subtle.ConstantTimeCompare(av.Bytes(), bBytes)
		return objects.NewBool(eq == 1), nil

	default:
		return nil, fmt.Errorf("TypeError: unsupported operand types or combination of types: '%T' and '%T'", a, b)
	}
}

// bytesLike extracts a []byte from a bytes-like object (bytes, bytearray, memoryview).
func bytesLike(o objects.Object) ([]byte, bool) {
	return objects.AsBytesLike(o)
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
		{"openssl_sha3_224", objects.NewBuiltinFunction("openssl_sha3_224", opensslSHA3_224)},
		{"openssl_sha3_256", objects.NewBuiltinFunction("openssl_sha3_256", opensslSHA3_256)},
		{"openssl_sha3_384", objects.NewBuiltinFunction("openssl_sha3_384", opensslSHA3_384)},
		{"openssl_sha3_512", objects.NewBuiltinFunction("openssl_sha3_512", opensslSHA3_512)},
		{"openssl_shake_128", objects.NewBuiltinFunction("openssl_shake_128", opensslShake128)},
		{"openssl_shake_256", objects.NewBuiltinFunction("openssl_shake_256", opensslShake256)},
		{"compare_digest", objects.NewBuiltinFunction("compare_digest", compareDigest)},
		{"hmac_new", objects.NewBuiltinFunction("hmac_new", hmacNew)},
		{"hmac_digest", objects.NewBuiltinFunction("hmac_digest", hmacDigestSingleShot)},
		{"UnsupportedDigestmodError", unsupportedDigestmodError},
		{"HMAC", HMACType},
		{"_GIL_MINSIZE", objects.NewInt(2048)},
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
	// openssl_md_meth_names: frozenset of digest names understood by OpenSSL.
	// hashlib.py uses it to extend algorithms_available.
	// CPython: Modules/_hashopenssl.c PyDoc for METH_MD_METH_NAMES
	if err := d.SetItem(objects.NewStr("openssl_md_meth_names"), fs); err != nil {
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
