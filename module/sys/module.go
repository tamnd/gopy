// Inittab registration for the sys module. CPython's sys is a
// built-in module: Modules/config.c.in lists "sys" with the init
// function PyInit_sys, and the import system finds it through the
// inittab miss path before consulting sys.path. gopy mirrors that
// here: stdlibinit blank-imports this package, the init() below
// registers buildModule under "sys", and `import sys` resolves
// straight out of the inittab without ever touching the filesystem.
//
// CPython: Modules/config.c.in:42 {"sys", _PyImport_BuiltinSys}
// CPython: Python/sysmodule.c:4131 _PySys_Create
package sys

import (
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

// pendingArgv holds the process argv until the sys module is built
// the first time. Callers that own the argv (cmd/gopy, lifecycle) set
// it before any Python code runs; buildModule reads it once and
// stamps sys.argv. The simpler alternative would be to call
// UpdateConfig on the live sys module, but the cmd binary doesn't run
// through lifecycle yet, so a package-level hand-off is enough.
var pendingArgv []string

// SetArgv records the argv the next sys-module build should expose as
// sys.argv. Calling more than once before the first import overwrites
// the previous value; calling after the module has already been built
// has no effect on the live module.
func SetArgv(argv []string) {
	pendingArgv = append(pendingArgv[:0], argv...)
}

func init() {
	_ = imp.AppendInittab("sys", buildModule)
}

// buildModule wraps the static-attribute slice produced by Init in a
// Module so the import machinery can hand it to user code. The
// PyConfig-driven attributes (argv, path, executable, ...) land
// through UpdateConfig once initconfig wires up; a fresh module
// without those is enough for `import sys` to succeed today.
//
// CPython: Python/sysmodule.c:4131 _PySys_Create body
func buildModule() (*objects.Module, error) {
	d, err := Init()
	if err != nil {
		return nil, err
	}
	m := objects.NewModule("sys")
	md := m.Dict()
	for _, k := range d.Keys() {
		v, err := d.GetItem(k)
		if err != nil {
			return nil, err
		}
		if err := md.SetItem(k, v); err != nil {
			return nil, err
		}
	}
	argv := pendingArgv
	if argv == nil {
		argv = []string{""}
	}
	if err := setItem(md, "argv", strList(argv)); err != nil {
		return nil, err
	}
	if err := setItem(md, "orig_argv", strList(argv)); err != nil {
		return nil, err
	}
	if err := setItem(md, "warnoptions", strList(nil)); err != nil {
		return nil, err
	}
	if err := setItem(md, "_xoptions", objects.NewDict()); err != nil {
		return nil, err
	}
	return m, nil
}
