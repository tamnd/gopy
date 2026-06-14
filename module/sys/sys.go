// Package sys ports cpython/Python/sysmodule.c. v0.7 lands the
// static-attribute slice (1651-sys-A): version strings, byteorder,
// maxunicode, builtin module names, hash info, the asyncgen hook
// slots. Each follow-on subtask attaches a slice of behavior:
//
//   - 1651-sys-B: argv, path, and modules from PyConfig
//   - 1651-sys-C: flags as a named tuple
//   - 1651-sys-D: stdout/stderr writers from the runtime
//   - 1651-sys-E: implementation tuple gopy fields
//
// The dict is built once per interpreter via Init. Until 1623 lands
// the import system the dict is exposed directly to consumers
// (lifecycle, builtins) rather than landing in sys.modules.
//
// CPython: Python/sysmodule.c:4131 _PySys_Create
package sys

import (
	"strconv"

	"github.com/tamnd/gopy/build"
	"github.com/tamnd/gopy/objects"
)

// Init builds the sys dict and stamps the static attributes that
// do not depend on PyConfig or runtime streams. Mirrors the
// _PySys_InitCore static slice.
//
// CPython: Python/sysmodule.c:3822 _PySys_InitCore
func Init() (*objects.Dict, error) {
	d := objects.NewDict()

	if err := setStr(d, "version", build.VersionString()); err != nil {
		return nil, err
	}
	if err := setStr(d, "platform", build.Platform()); err != nil {
		return nil, err
	}
	if err := setStr(d, "byteorder", byteorderName()); err != nil {
		return nil, err
	}
	if err := setStr(d, "copyright", build.Copyright); err != nil {
		return nil, err
	}
	if err := setStr(d, "_framework", ""); err != nil {
		return nil, err
	}
	if err := setStr(d, "abiflags", ""); err != nil {
		return nil, err
	}
	if err := setStr(d, "float_repr_style", "short"); err != nil {
		return nil, err
	}

	if err := setInt(d, "hexversion", hexVersion()); err != nil {
		return nil, err
	}
	if err := setInt(d, "api_version", apiVersion); err != nil {
		return nil, err
	}
	if err := setInt(d, "maxsize", maxsize()); err != nil {
		return nil, err
	}
	if err := setInt(d, "maxunicode", 0x10FFFF); err != nil {
		return nil, err
	}

	if err := setItem(d, "version_info", versionInfo()); err != nil {
		return nil, err
	}
	if err := setItem(d, "implementation", implementation()); err != nil {
		return nil, err
	}
	if err := setItem(d, "builtin_module_names", builtinModuleNames()); err != nil {
		return nil, err
	}
	if err := setItem(d, "stdlib_module_names", objects.NewTuple(nil)); err != nil {
		return nil, err
	}
	if err := setItem(d, "hash_info", hashInfo()); err != nil {
		return nil, err
	}
	if err := setItem(d, "float_info", floatInfo()); err != nil {
		return nil, err
	}
	if err := setItem(d, "int_info", intInfo()); err != nil {
		return nil, err
	}
	if err := setItem(d, "_jit", jitInfo()); err != nil {
		return nil, err
	}
	// Filesystem encoding helpers. CPython picks the value at startup
	// from PyConfig.filesystem_encoding (utf-8 on every modern target).
	// gopy hardcodes utf-8 / surrogateescape until PyConfig lands.
	//
	// CPython: Python/sysmodule.c sys_getfilesystemencoding_impl
	if err := setItem(d, "getfilesystemencoding", objects.NewBuiltinFunction("getfilesystemencoding", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return objects.NewStr("utf-8"), nil
	})); err != nil {
		return nil, err
	}
	if err := setItem(d, "getfilesystemencodeerrors", objects.NewBuiltinFunction("getfilesystemencodeerrors", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return objects.NewStr("surrogateescape"), nil
	})); err != nil {
		return nil, err
	}
	if err := setStr(d, "filesystemencoding", "utf-8"); err != nil {
		return nil, err
	}
	if err := setStr(d, "filesystemencodeerrors", "surrogateescape"); err != nil {
		return nil, err
	}
	// The interpreter's default string encoding. Since 3.0 this is fixed
	// at utf-8 and getdefaultencoding takes no arguments.
	//
	// CPython: Python/sysmodule.c sys_getdefaultencoding_impl
	if err := setItem(d, "getdefaultencoding", objects.NewBuiltinFunction("getdefaultencoding", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return objects.NewStr("utf-8"), nil
	})); err != nil {
		return nil, err
	}

	// Import-system state the runtime exposes at the top level. CPython
	// stamps these in PySys_Create / the import bootstrap; runpy and
	// pkgutil read them directly. gopy's import is Go-side so the hooks
	// list and the importer cache stay empty, and bytecode is never
	// written, but the attributes must exist with the right types.
	//
	// CPython: Python/sysmodule.c _PySys_AddObject path_hooks/path_importer_cache
	if err := setItem(d, "dont_write_bytecode", objects.NewBool(true)); err != nil {
		return nil, err
	}
	if err := setItem(d, "path_hooks", objects.NewList(nil)); err != nil {
		return nil, err
	}
	if err := setItem(d, "path_importer_cache", objects.NewDict()); err != nil {
		return nil, err
	}

	return d, nil
}

// jitInfo returns sys._jit. gopy has no JIT; is_available /
// is_enabled / is_active all answer False so test.support's
// requires_jit_enabled gates skip cleanly.
//
// CPython: Python/sysmodule.c:4064 _jit module
func jitInfo() *objects.Namespace {
	n := objects.NewNamespace()
	d := n.Dict()
	falseFn := objects.NewBuiltinFunction("is_enabled", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return objects.NewBool(false), nil
	})
	_ = d.SetItem(objects.NewStr("is_enabled"), falseFn)
	_ = d.SetItem(objects.NewStr("is_available"), objects.NewBuiltinFunction("is_available", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return objects.NewBool(false), nil
	}))
	_ = d.SetItem(objects.NewStr("is_active"), objects.NewBuiltinFunction("is_active", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return objects.NewBool(false), nil
	}))
	return n
}

// apiVersion is CPython's PYTHON_API_VERSION (Include/patchlevel.h).
// gopy carries it on the sys dict for parity with cpython tooling
// that sniffs the field; the value is informational only.
//
// CPython: Include/cpython/pylifecycle.h:11 PYTHON_API_VERSION
const apiVersion = 1013

// byteorderName returns the host byteorder string sys.byteorder
// reports. encoding/binary's runtime detection would do the job
// too; we keep it inline so the sys package has no extra deps.
//
// CPython: Python/sysmodule.c:3862 byteorder set
func byteorderName() string {
	var x uint16 = 0x0001
	low := byte(x)
	if low == 0x01 {
		return "little"
	}
	return "big"
}

// hexVersion returns the CPython PY_VERSION_HEX equivalent for the
// upstream version gopy tracks. Format is MAJOR<<24 | MINOR<<16 |
// MICRO<<8 | RELEASE_LEVEL<<4 | RELEASE_SERIAL.
//
// CPython: Include/patchlevel.h:31 PY_VERSION_HEX
func hexVersion() int64 {
	major := int64(build.PythonMajorVersion)
	minor := int64(build.PythonMinorVersion)
	const (
		micro    = 0
		serial   = 0
		levelHex = 0xF // alpha=0xA, beta=0xB, candidate=0xC, final=0xF
	)
	return major<<24 | minor<<16 | micro<<8 | levelHex<<4 | serial
}

// maxsize returns the host int's PY_SSIZE_T_MAX. On gopy that is
// the host int's max so callers see the same upper bound CPython
// reports for the running platform.
//
// CPython: Include/internal/pycore_pyhash.h indirectly via PY_SSIZE_T_MAX
func maxsize() int64 {
	if strconv.IntSize == 64 {
		return 1<<63 - 1
	}
	return 1<<31 - 1
}

// versionInfo returns sys.version_info as a five-tuple
// (major, minor, micro, releaselevel, serial). The struct-sequence
// named-tuple lands with 1651-sys-C; v0.7 uses a plain tuple.
//
// CPython: Python/sysmodule.c:3884 make_version_info
func versionInfo() *objects.Tuple {
	return objects.NewTuple([]objects.Object{
		objects.NewInt(int64(build.PythonMajorVersion)),
		objects.NewInt(int64(build.PythonMinorVersion)),
		objects.NewInt(0),
		objects.NewStr("final"),
		objects.NewInt(0),
	})
}

// ImplementationName is the gopy fingerprint sys.implementation.name
// reports. Pinned per spec 1622 so consumers (importlib, third-party
// runners) can branch off "gopy" cleanly.
//
// CPython: Python/sysmodule.c:3920 make_impl_info "name" entry
const ImplementationName = "gopy"

// CacheTag is the bytecode-cache-file tag CPython reports through
// sys.implementation.cache_tag. Encoded as "gopy-MMNNP" where MM is
// the upstream major, NN the minor, P the micro. The trailing zero
// is the third digit so .pyc files do not collide with cpython-NNN
// nor with future bumps to PY_MICRO_VERSION.
//
// CPython: Python/sysmodule.c:3920 make_impl_info "cache_tag" entry
var CacheTag = "gopy-" + strconv.Itoa(build.PythonMajorVersion) +
	strconv.Itoa(build.PythonMinorVersion) + "0"

// implementation returns sys.implementation as a SimpleNamespace
// carrying name / version / hexversion / cache_tag. CPython's
// make_impl_info populates the same fields on a namespace so
// test.support can do `sys.implementation.name`.
//
// CPython: Python/sysmodule.c:3889 make_impl_info
func implementation() *objects.Namespace {
	n := objects.NewNamespace()
	d := n.Dict()
	_ = d.SetItem(objects.NewStr("name"), objects.NewStr(ImplementationName))
	_ = d.SetItem(objects.NewStr("version"), versionInfo())
	_ = d.SetItem(objects.NewStr("hexversion"), objects.NewInt(hexVersion()))
	_ = d.SetItem(objects.NewStr("cache_tag"), objects.NewStr(CacheTag))
	_ = d.SetItem(objects.NewStr("_multiarch"), objects.NewStr(""))
	return n
}

// builtinModuleNames returns the tuple of module names that are
// compiled into the interpreter. Until 1623 lands the import system
// the list contains just the modules gopy initializes statically
// (builtins, sys). The slice grows as 1651 lands more modules.
//
// CPython: Python/sysmodule.c:3859 list_builtin_module_names
func builtinModuleNames() *objects.Tuple {
	return objects.NewTuple([]objects.Object{
		objects.NewStr("builtins"),
		objects.NewStr("sys"),
	})
}

// hashInfo is sys.hash_info as a SimpleNamespace. The field order
// matches CPython's Hash_InfoType (width, modulus, inf, nan, imag,
// algorithm, hash_bits, seed_bits, cutoff). Values mirror the 64-bit
// defaults (PyHASH_BITS=61, siphash13). _pydecimal reads modulus to
// build its hash table, so the field must carry the real prime.
//
// CPython: Python/sysmodule.c:1565 get_hash_info
// CPython: Include/cpython/pyhash.h:18 PyHASH_MODULUS
func hashInfo() *objects.Namespace {
	n := objects.NewNamespace()
	d := n.Dict()
	_ = d.SetItem(objects.NewStr("width"), objects.NewInt(64))
	_ = d.SetItem(objects.NewStr("modulus"), objects.NewInt((1<<61)-1))
	_ = d.SetItem(objects.NewStr("inf"), objects.NewInt(314159))
	_ = d.SetItem(objects.NewStr("nan"), objects.NewInt(0))
	_ = d.SetItem(objects.NewStr("imag"), objects.NewInt(1000003))
	_ = d.SetItem(objects.NewStr("algorithm"), objects.NewStr("siphash13"))
	_ = d.SetItem(objects.NewStr("hash_bits"), objects.NewInt(64))
	_ = d.SetItem(objects.NewStr("seed_bits"), objects.NewInt(128))
	_ = d.SetItem(objects.NewStr("cutoff"), objects.NewInt(0))
	return n
}

// floatInfo returns sys.float_info as a SimpleNamespace with the
// standard IEEE 754 double-precision constants. Field order matches
// CPython's Float_InfoType (max, max_exp, max_10_exp, min, min_exp,
// min_10_exp, dig, mant_dig, epsilon, radix, rounds).
//
// CPython: Objects/floatobject.c:82 PyFloat_GetInfo
// CPython: Python/sysmodule.c:3849 _PySys_InitCore
func floatInfo() *objects.Namespace {
	n := objects.NewNamespace()
	d := n.Dict()
	_ = d.SetItem(objects.NewStr("max"), objects.NewFloat(1.7976931348623157e+308))
	_ = d.SetItem(objects.NewStr("max_exp"), objects.NewInt(1024))
	_ = d.SetItem(objects.NewStr("max_10_exp"), objects.NewInt(308))
	_ = d.SetItem(objects.NewStr("min"), objects.NewFloat(2.2250738585072014e-308))
	_ = d.SetItem(objects.NewStr("min_exp"), objects.NewInt(-1021))
	_ = d.SetItem(objects.NewStr("min_10_exp"), objects.NewInt(-307))
	_ = d.SetItem(objects.NewStr("dig"), objects.NewInt(15))
	_ = d.SetItem(objects.NewStr("mant_dig"), objects.NewInt(53))
	_ = d.SetItem(objects.NewStr("epsilon"), objects.NewFloat(2.220446049250313e-16))
	_ = d.SetItem(objects.NewStr("radix"), objects.NewInt(2))
	_ = d.SetItem(objects.NewStr("rounds"), objects.NewInt(1))
	return n
}

// intInfo returns sys.int_info as a SimpleNamespace.
//
// CPython: Objects/longobject.c:6609 PyLong_GetInfo
func intInfo() *objects.Namespace {
	n := objects.NewNamespace()
	d := n.Dict()
	_ = d.SetItem(objects.NewStr("bits_per_digit"), objects.NewInt(30))
	_ = d.SetItem(objects.NewStr("sizeof_digit"), objects.NewInt(4))
	_ = d.SetItem(objects.NewStr("default_max_str_digits"), objects.NewInt(4300))
	_ = d.SetItem(objects.NewStr("str_digits_check_threshold"), objects.NewInt(640))
	return n
}

func setStr(d *objects.Dict, name, value string) error {
	return d.SetItem(objects.NewStr(name), objects.NewStr(value))
}

func setInt(d *objects.Dict, name string, value int64) error {
	return d.SetItem(objects.NewStr(name), objects.NewInt(value))
}

func setItem(d *objects.Dict, name string, value objects.Object) error {
	return d.SetItem(objects.NewStr(name), value)
}
