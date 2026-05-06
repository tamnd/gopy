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

	return d, nil
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

// implementation returns sys.implementation as a 4-element tuple
// (name, version, hexversion, cache_tag). CPython uses a SimpleNamespace
// here; the named-attribute facade lands once SimpleNamespace does.
//
// CPython: Python/sysmodule.c:3889 make_impl_info
func implementation() *objects.Tuple {
	return objects.NewTuple([]objects.Object{
		objects.NewStr(ImplementationName),
		versionInfo(),
		objects.NewInt(hexVersion()),
		objects.NewStr(CacheTag),
	})
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

// hashInfo is sys.hash_info as a tuple. The field order matches
// CPython's Hash_InfoType; the struct-sequence wrapper lands with
// 1651-sys-C.
//
// CPython: Python/sysmodule.c:hash_info_desc
func hashInfo() *objects.Tuple {
	algo := "siphash13"
	return objects.NewTuple([]objects.Object{
		objects.NewInt(64),
		objects.NewInt(0),
		objects.NewInt(0),
		objects.NewInt(0),
		objects.NewInt(0),
		objects.NewStr(algo),
		objects.NewInt(128),
		objects.NewInt(64),
		objects.NewInt(0),
	})
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
