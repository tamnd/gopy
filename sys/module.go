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
	return m, nil
}
