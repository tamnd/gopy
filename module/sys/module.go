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
	"os"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/initconfig"
	"github.com/tamnd/gopy/objects"
)

// pendingArgv holds the process argv until the sys module is built
// the first time. Callers that own the argv (cmd/gopy, lifecycle) set
// it before any Python code runs; buildModule reads it once and
// stamps sys.argv. The simpler alternative would be to call
// UpdateConfig on the live sys module, but the cmd binary doesn't run
// through lifecycle yet, so a package-level hand-off is enough.
var pendingArgv []string

// pendingPath plays the same role for sys.path. unittest.loader walks
// sys.path during discovery, and the cmd binary owns the same path
// list it hands to imp.PathFinder, so the two surfaces are kept in
// sync via this hand-off until initconfig wires up end-to-end.
var pendingPath []string

// SetArgv records the argv the next sys-module build should expose as
// sys.argv. Calling more than once before the first import overwrites
// the previous value; calling after the module has already been built
// has no effect on the live module.
func SetArgv(argv []string) {
	pendingArgv = append(pendingArgv[:0], argv...)
}

// SetPath records the path entries the next sys-module build should
// expose as sys.path. Same semantics as SetArgv.
func SetPath(path []string) {
	pendingPath = append(pendingPath[:0], path...)
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
	if err := setItem(md, "path", strListAsList(pendingPath)); err != nil {
		return nil, err
	}
	// sys.modules is the same dict the import machinery writes to. The
	// pointer-share means Python-side mutations (sys.modules[name] = m,
	// del sys.modules[name]) drive future cache hits and misses.
	//
	// CPython: Python/sysmodule.c:3818 _PySys_InitMain (sys.modules = interp->modules)
	if err := setItem(md, "modules", imp.SysModules()); err != nil {
		return nil, err
	}
	// sys.flags is the struct-sequence warnings, traceback, and any
	// PEP 587 tooling reads. UpdateConfig also stamps this once
	// initconfig wires through; until then the inittab build hands a
	// default-config flags so `import warnings` sees flags.dev_mode,
	// flags.safe_path, flags.context_aware_warnings.
	//
	// CPython: Python/sysmodule.c:3478 set_flags_from_config
	defaultCfg := &initconfig.PyConfig{}
	defaultCfg.InitPythonConfig()
	if err := setItem(md, "flags", makeFlags(defaultCfg)); err != nil {
		return nil, err
	}
	// sys.exc_info reads the per-thread handled-exception slot the vm
	// maintains across PUSH_EXC_INFO / POP_EXCEPT. unittest's
	// _Outcome.testPartExecutor and traceback.format_exc both call it
	// to capture the active exception for reporting.
	//
	// CPython: Python/sysmodule.c:558 sys_exc_info_impl
	if err := setItem(md, "exc_info", objects.NewBuiltinFunction("exc_info", excInfo)); err != nil {
		return nil, err
	}
	// sys._getframe([depth]) returns the frame depth levels up the call
	// stack. depth=0 is the immediate caller's frame.
	//
	// CPython: Python/sysmodule.c:1180 sys__getframe_impl
	if err := setItem(md, "_getframe", objects.NewBuiltinFunction("_getframe", getFrame)); err != nil {
		return nil, err
	}
	// stdout/stderr/stdin wrap the process file descriptors. CPython
	// hands these to PyConfig and then PyConfig_InitPythonConfig
	// stamps them onto sys; gopy's PyConfig port is incomplete so the
	// inittab build wires the streams directly.
	//
	// CPython: Python/sysmodule.c:3795 sys_init_streams
	if err := setItem(md, "stdin", objects.NewFile(os.Stdin, "<stdin>", "r", false, true, false)); err != nil {
		return nil, err
	}
	if err := setItem(md, "stdout", objects.NewFile(os.Stdout, "<stdout>", "w", false, false, true)); err != nil {
		return nil, err
	}
	if err := setItem(md, "stderr", objects.NewFile(os.Stderr, "<stderr>", "w", false, false, true)); err != nil {
		return nil, err
	}
	if err := setItem(md, "__stdin__", objects.NewFile(os.Stdin, "<stdin>", "r", false, true, false)); err != nil {
		return nil, err
	}
	if err := setItem(md, "__stdout__", objects.NewFile(os.Stdout, "<stdout>", "w", false, false, true)); err != nil {
		return nil, err
	}
	if err := setItem(md, "__stderr__", objects.NewFile(os.Stderr, "<stderr>", "w", false, false, true)); err != nil {
		return nil, err
	}
	return m, nil
}

// strListAsList returns the path entries as a list (mutable, so user
// code can sys.path.insert / .remove). UpdateConfig still produces a
// tuple because the PyConfig-driven shape predates the live-list
// requirement unittest brings in.
func strListAsList(items []string) *objects.List {
	out := make([]objects.Object, len(items))
	for i, s := range items {
		out[i] = objects.NewStr(s)
	}
	return objects.NewList(out)
}
