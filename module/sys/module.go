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
	"fmt"
	"io"
	"os"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/initconfig"
	"github.com/tamnd/gopy/intrinsics"
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

// pendingExecutable / pendingBaseExecutable mirror the role of
// pendingArgv for sys.executable. cmd/gopy stamps the os.Executable()
// result here before bootstrapping so subprocess-spawning stdlib code
// (subprocess.Popen([sys.executable, ...])) finds the running binary.
//
// CPython: Python/sysmodule.c:3951 _PySys_UpdateConfig (executable)
var (
	pendingExecutable     string
	pendingBaseExecutable string
)

// SetExecutable records the path the next sys-module build should
// expose as sys.executable and sys._base_executable. If sys is
// already imported, the live attributes refresh in place. Mirrors
// _PySys_UpdateConfig's executable branch.
//
// CPython: Python/sysmodule.c:3951 _PySys_UpdateConfig (executable)
func SetExecutable(exe, base string) {
	pendingExecutable = exe
	pendingBaseExecutable = base
	if md := liveSysDict(); md != nil {
		_ = md.SetItem(objects.NewStr("executable"), objects.NewStr(exe))
		_ = md.SetItem(objects.NewStr("_base_executable"), objects.NewStr(base))
	}
}

// SetArgv records the argv the next sys-module build should expose as
// sys.argv. If sys is already in the module cache, the live argv /
// orig_argv attributes are refreshed in place so subsequent reads
// observe the new value. Mirrors _PySys_UpdateConfig's argv branch.
//
// CPython: Python/sysmodule.c:3951 _PySys_UpdateConfig (argv)
func SetArgv(argv []string) {
	pendingArgv = append(pendingArgv[:0], argv...)
	if md := liveSysDict(); md != nil {
		_ = md.SetItem(objects.NewStr("argv"), strListAsList(argv))
		_ = md.SetItem(objects.NewStr("orig_argv"), strListAsList(argv))
	}
}

// SetPath records the path entries the next sys-module build should
// expose as sys.path. If sys is already in the module cache, the live
// path list is refreshed in place. Mirrors _PySys_UpdateConfig's path
// branch.
//
// CPython: Python/sysmodule.c:3951 _PySys_UpdateConfig (path)
func SetPath(path []string) {
	pendingPath = append(pendingPath[:0], path...)
	if md := liveSysDict(); md != nil {
		_ = md.SetItem(objects.NewStr("path"), strListAsList(path))
	}
}

// pendingStdlibDir records the stdlib root the next sys-module build
// should expose as sys._stdlib_dir. FrozenImporter._resolve_filename
// reads it to locate the on-disk copy of a frozen module. SetStdlibDir
// also refreshes the live attribute when sys is already imported.
//
// CPython: Python/sysmodule.c:3951 _PySys_UpdateConfig (stdlib_dir)
var pendingStdlibDir string

// SetStdlibDir records the stdlib root and exposes it as
// sys._stdlib_dir, refreshing the live attribute when sys is already
// imported.
//
// CPython: Python/sysmodule.c:3951 _PySys_UpdateConfig (stdlib_dir)
func SetStdlibDir(dir string) {
	pendingStdlibDir = dir
	if md := liveSysDict(); md != nil {
		_ = md.SetItem(objects.NewStr("_stdlib_dir"), objects.NewStr(dir))
	}
}

// pendingSafePath records the safe_path flag supplied on the command
// line (-P / -I / PYTHONSAFEPATH) before sys is built. buildModule
// reads it when stamping sys.flags; SetSafePath also refreshes the live
// flags struct-sequence when sys is already imported.
//
// CPython: Python/initconfig.c:1828 config_init_safe_path
var pendingSafePath bool

// SetSafePath records safe_path and, when sys is already live, rebuilds
// sys.flags so sys.flags.safe_path reads True.
//
// CPython: Python/sysmodule.c:3478 set_flags_from_config (safe_path)
func SetSafePath(on bool) {
	pendingSafePath = on
	if md := liveSysDict(); md != nil {
		cfg := &initconfig.PyConfig{}
		cfg.InitPythonConfig()
		if on {
			cfg.SafePath = 1
		}
		_ = md.SetItem(objects.NewStr("flags"), makeFlags(cfg))
	}
}

// LivePath returns the current sys.path entries as a Go slice, or nil
// when sys has not been imported yet (PathFinder then falls back to
// its static Paths snapshot, which is what unit tests that drive
// PathFinder directly rely on). It reads the live sys module dict so
// user-code mutations (sys.path.insert, sys.path.append) are visible
// to callers. PathFinder.FindModule consults this so the meta-path
// import honors PEP 328 + 451 sys.path semantics.
//
// CPython: Lib/importlib/_bootstrap_external.py:1290 path = sys.path
func LivePath() []string {
	md := liveSysDict()
	if md == nil {
		return nil
	}
	v, err := md.GetItem(objects.NewStr("path"))
	if err != nil || v == nil {
		return nil
	}
	switch lst := v.(type) {
	case *objects.List:
		out := make([]string, 0, lst.Len())
		for i := 0; i < lst.Len(); i++ {
			if s, ok := lst.Item(i).(*objects.Unicode); ok {
				out = append(out, s.Value())
			}
		}
		return out
	case *objects.Tuple:
		out := make([]string, 0, lst.Len())
		for i := 0; i < lst.Len(); i++ {
			if s, ok := lst.Item(i).(*objects.Unicode); ok {
				out = append(out, s.Value())
			}
		}
		return out
	}
	return nil
}

// liveSysDict returns the dict of the cached sys module, or nil when
// sys has not been imported yet. Used by SetArgv / SetPath / SetStdio
// to keep the live module in sync with config changes that arrive
// after the inittab build.
func liveSysDict() *objects.Dict {
	mod, ok := imp.GetModule("sys")
	if !ok || mod == nil {
		return nil
	}
	return mod.Dict()
}

// pendingStdout / pendingStderr override the default os.Stdout /
// os.Stderr wrappers for sys.stdout / sys.stderr. lifecycle.Main sets
// them so tests can capture print() output by feeding their own
// io.Writer. The PyConfig analog is PyConfig.stdout / stderr, which
// _PySys_InitStreams reads at startup.
//
// CPython: Python/sysmodule.c:3795 sys_init_streams
var (
	pendingStdout io.Writer
	pendingStderr io.Writer
)

// pendingBreakpointHook holds the default breakpoint hook supplied by
// the builtins package before sys is built. The hook lives in builtins
// because its body imports modules and calls warnings.warn, neither of
// which module/sys can reach without an import cycle. SetBreakpointHook
// records it here so buildModule can stamp sys.breakpointhook and
// sys.__breakpointhook__ once the module dict exists.
//
// CPython: Python/sysmodule.c:2826 breakpointhook PyMethodDef
var pendingBreakpointHook objects.Object

// SetBreakpointHook records the default breakpoint hook and, when sys
// is already live, installs it as both sys.breakpointhook and
// sys.__breakpointhook__. builtins.Init calls this with its
// sys_breakpointhook port so breakpoint() has a hook to dispatch to.
//
// CPython: Python/sysmodule.c:2826 breakpointhook PyMethodDef
func SetBreakpointHook(hook objects.Object) {
	pendingBreakpointHook = hook
	d := liveSysDict()
	if d == nil {
		return
	}
	_ = d.SetItem(objects.NewStr("breakpointhook"), hook)
	_ = d.SetItem(objects.NewStr("__breakpointhook__"), hook)
}

// SetStdio records the io.Writers the next sys-module build should
// expose as sys.stdout and sys.stderr. nil means "use os.Stdout /
// os.Stderr". If sys is already in the module cache, the live
// stdout/stderr/__stdout__/__stderr__ attributes are refreshed in
// place so subsequent prints reach the new writers. This mirrors
// CPython, where each Py_InitializeFromConfig run re-stamps sys
// streams from PyConfig.
//
// CPython: Python/sysmodule.c:3795 sys_init_streams (re-stamp path)
func SetStdio(stdout, stderr io.Writer) {
	pendingStdout = stdout
	pendingStderr = stderr
	d := liveSysDict()
	if d == nil {
		return
	}
	out := makeStdoutFile()
	err := makeStderrFile()
	_ = d.SetItem(objects.NewStr("stdout"), out)
	_ = d.SetItem(objects.NewStr("__stdout__"), out)
	_ = d.SetItem(objects.NewStr("stderr"), err)
	_ = d.SetItem(objects.NewStr("__stderr__"), err)
}

func makeStdoutFile() *objects.File {
	if pendingStdout != nil {
		return objects.NewWriterFile(pendingStdout, "<stdout>", "w")
	}
	return objects.NewFile(os.Stdout, "<stdout>", "w", false, false, true)
}

func makeStderrFile() *objects.File {
	if pendingStderr != nil {
		return objects.NewWriterFile(pendingStderr, "<stderr>", "w")
	}
	return objects.NewFile(os.Stderr, "<stderr>", "w", false, false, true)
}

func init() {
	_ = imp.AppendInittab("sys", buildModule)
	// CALL_INTRINSIC_1 PRINT (PRINT_EXPR pre-3.12) must reach the live
	// sys.displayhook so user code (and doctest's runner) can intercept
	// it; intrinsics is a lower layer and cannot import module/sys
	// directly without a cycle, so the link is via a function variable.
	//
	// CPython: Python/intrinsics.c:28 print_expr (sys_displayhook lookup)
	intrinsics.PrintExprHook = printExprViaSysDisplayhook
}

// printExprViaSysDisplayhook resolves sys.displayhook fresh on every
// call so a user-installed hook (sys.displayhook = ...) takes effect.
// CPython does the same with _PySys_GetRequiredAttr.
//
// CPython: Python/intrinsics.c:30 _PySys_GetRequiredAttr(displayhook)
func printExprViaSysDisplayhook(v objects.Object) (objects.Object, error) {
	d := liveSysDict()
	if d == nil {
		return nil, fmt.Errorf("RuntimeError: lost sys.displayhook")
	}
	hook, _ := d.GetItem(objects.NewStr("displayhook"))
	if hook == nil || hook == objects.None() {
		return nil, fmt.Errorf("RuntimeError: lost sys.displayhook")
	}
	return objects.Call(hook, objects.NewTuple([]objects.Object{v}), nil)
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
	if err := setItem(md, "argv", strListAsList(argv)); err != nil {
		return nil, err
	}
	if err := setItem(md, "orig_argv", strListAsList(argv)); err != nil {
		return nil, err
	}
	if err := setItem(md, "warnoptions", strListAsList(nil)); err != nil {
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
	if pendingSafePath {
		defaultCfg.SafePath = 1
	}
	if err := setItem(md, "flags", makeFlags(defaultCfg)); err != nil {
		return nil, err
	}
	// Path-config attributes populated by UpdateConfig when lifecycle runs.
	// Stamp empty-string defaults here so stdlib modules that read them at
	// import time (e.g. gettext reads sys.base_prefix) don't see
	// AttributeError. UpdateConfig will overwrite these with real values
	// once initconfig wires through.
	//
	// CPython: Python/sysmodule.c:3951 _PySys_UpdateConfig
	for _, name := range []string{
		"executable", "_base_executable",
		"prefix", "base_prefix", "exec_prefix", "base_exec_prefix",
		"platlibdir",
	} {
		if has, _ := md.Contains(objects.NewStr(name)); !has {
			if err := setStr(md, name, ""); err != nil {
				return nil, err
			}
		}
	}
	// Stamp the executable hand-off recorded by cmd/gopy. Until
	// initconfig wires through end-to-end this is the only path that
	// gives subprocess.Popen([sys.executable, ...]) a real argv[0].
	//
	// CPython: Python/sysmodule.c:3951 _PySys_UpdateConfig (executable)
	if pendingExecutable != "" {
		if err := setStr(md, "executable", pendingExecutable); err != nil {
			return nil, err
		}
	}
	if pendingBaseExecutable != "" {
		if err := setStr(md, "_base_executable", pendingBaseExecutable); err != nil {
			return nil, err
		}
	}
	// sys._stdlib_dir lets FrozenImporter._resolve_filename find the
	// on-disk copy of a frozen module.
	//
	// CPython: Python/sysmodule.c:3951 _PySys_UpdateConfig (stdlib_dir)
	if pendingStdlibDir != "" {
		if err := setStr(md, "_stdlib_dir", pendingStdlibDir); err != nil {
			return nil, err
		}
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
	// sys.exception() is the Python 3.11+ replacement for sys.exc_info()[1].
	// Returns the currently handled exception, or None.
	//
	// CPython: Python/sysmodule.c:573 sys_exception_impl
	if err := setItem(md, "exception", objects.NewBuiltinFunction("exception", sysException)); err != nil {
		return nil, err
	}
	// sys.monitoring is the PEP 669 instrumentation namespace. gopy
	// hasn't wired the bytecode interpreter to actually fire events
	// yet, so the public surface is a stub: the constants are
	// byte-identical to CPython (bdb does `E.PY_START | E.LINE`
	// arithmetic at import time), and every callable returns None.
	//
	// CPython: Python/instrumentation.c:3001 _PyMonitoring_PrintEvents
	if err := setItem(md, "monitoring", makeMonitoring()); err != nil {
		return nil, err
	}
	// sys.intern interns a str object. The dedicated unicodeobject port
	// will route through the global interned table; for now the helper
	// returns the input unchanged so collections.namedtuple's typename /
	// field-name pipeline sees round-trip semantics.
	//
	// CPython: Python/sysmodule.c:1004 sys_intern_impl
	if err := setItem(md, "intern", objects.NewBuiltinFunction("intern", internShim)); err != nil {
		return nil, err
	}
	// sys.exit, sys.setrecursionlimit, sys.getrecursionlimit, and
	// sys.getrefcount are wired here. helpers.Bind installs the
	// thread-aware forms when lifecycle constructs the module; the
	// inittab path reaches sys.exit via the CurrentThreadHook bridge
	// instead. unittest's tear-down path calls sys.exit, so any gate
	// that goes through unittest.main needs this row.
	//
	// CPython: Python/sysmodule.c:915 sys_exit_impl,
	// Python/sysmodule.c:1352 sys_setrecursionlimit_impl,
	// Python/sysmodule.c:1612 sys_getrecursionlimit_impl,
	// Python/sysmodule.c:2022 sys_getrefcount_impl
	// sys.excepthook is invoked by the interpreter on an unhandled
	// exception; threading.py captures it at module load and threads
	// use it to print uncaught exceptions raised in their target. The
	// default mirrors CPython's PyErr_Display: print "Traceback ..."
	// followed by the exception repr to sys.stderr.
	//
	// CPython: Python/sysmodule.c sys_excepthook_impl
	excepthookFn := objects.NewBuiltinFunction("excepthook", excepthookShim)
	if err := setItem(md, "excepthook", excepthookFn); err != nil {
		return nil, err
	}
	if err := setItem(md, "__excepthook__", excepthookFn); err != nil {
		return nil, err
	}
	// sys.unraisablehook is invoked by the runtime when an exception
	// occurs in a destructor, in a generator close callback, or during
	// GC, where there is no caller to re-raise into.
	//
	// CPython: Python/sysmodule.c:893 sys_unraisablehook
	// CPython: Python/errors.c:1600 _PyErr_WriteUnraisableDefaultHook
	unraisableHookFn := objects.NewBuiltinFunction("unraisablehook", unraisableHookShim)
	if err := setItem(md, "unraisablehook", unraisableHookFn); err != nil {
		return nil, err
	}
	if err := setItem(md, "__unraisablehook__", unraisableHookFn); err != nil {
		return nil, err
	}
	if err := setItem(md, "exit", objects.NewBuiltinFunction("exit", exitShim)); err != nil {
		return nil, err
	}
	if err := setItem(md, "setrecursionlimit", objects.NewBuiltinFunction("setrecursionlimit", setRecursionLimitShim)); err != nil {
		return nil, err
	}
	if err := setItem(md, "getrecursionlimit", objects.NewBuiltinFunction("getrecursionlimit", getRecursionLimit)); err != nil {
		return nil, err
	}
	if err := setItem(md, "getrefcount", objects.NewBuiltinFunction("getrefcount", getRefcount)); err != nil {
		return nil, err
	}
	if err := setItem(md, "getsizeof", objects.NewBuiltinFunction("getsizeof", getSizeof)); err != nil {
		return nil, err
	}
	if err := setItem(md, "is_finalizing", objects.NewBuiltinFunction("is_finalizing", isFinalizing)); err != nil {
		return nil, err
	}
	// sys.getswitchinterval / sys.setswitchinterval expose the GIL switch
	// interval in seconds. gopy runs the GIL-free model so the value is a
	// no-op sentinel but must exist so threading code can read/write it.
	//
	// CPython: Python/sysmodule.c:1261 sys_setswitchinterval_impl,
	// Python/sysmodule.c:1278 sys_getswitchinterval_impl
	if err := setItem(md, "getswitchinterval", objects.NewBuiltinFunction("getswitchinterval", getSwitchInterval)); err != nil {
		return nil, err
	}
	if err := setItem(md, "setswitchinterval", objects.NewBuiltinFunction("setswitchinterval", setSwitchInterval)); err != nil {
		return nil, err
	}
	// sys.get_int_max_str_digits / sys.set_int_max_str_digits guard
	// integer-to-string conversion length. Added in CPython 3.11 to
	// mitigate quadratic-time attacks via enormous int repr.
	//
	// CPython: Python/sysmodule.c:2001 sys_get_int_max_str_digits_impl,
	// Python/sysmodule.c:2026 sys_set_int_max_str_digits_impl
	if err := setItem(md, "get_int_max_str_digits", objects.NewBuiltinFunction("get_int_max_str_digits", getIntMaxStrDigits)); err != nil {
		return nil, err
	}
	if err := setItem(md, "set_int_max_str_digits", objects.NewBuiltinFunction("set_int_max_str_digits", setIntMaxStrDigits)); err != nil {
		return nil, err
	}
	// sys.get_coroutine_origin_tracking_depth /
	// sys.set_coroutine_origin_tracking_depth track how many traceback
	// frames to capture when a coroutine is created. asyncio uses these
	// to surface where an unawaited coroutine originated.
	//
	// CPython: Python/sysmodule.c:1379 sys_set_coroutine_origin_tracking_depth_impl,
	// Python/sysmodule.c:1402 sys_get_coroutine_origin_tracking_depth_impl
	if err := setItem(md, "get_coroutine_origin_tracking_depth", objects.NewBuiltinFunction("get_coroutine_origin_tracking_depth", getCoroutineOriginTrackingDepth)); err != nil {
		return nil, err
	}
	if err := setItem(md, "set_coroutine_origin_tracking_depth", objects.NewBuiltinFunction("set_coroutine_origin_tracking_depth", setCoroutineOriginTrackingDepth)); err != nil {
		return nil, err
	}
	// sys.get_asyncgen_hooks / sys.set_asyncgen_hooks manage the
	// per-thread firstiter / finalizer hooks the event loop installs so
	// it can drive aclose() on async generators that the collector
	// reclaims. base_events._run_forever_setup swaps them in and out.
	//
	// CPython: Python/sysmodule.c:1443 sys_set_asyncgen_hooks_impl,
	// Python/sysmodule.c:1480 sys_get_asyncgen_hooks_impl
	if err := setItem(md, "get_asyncgen_hooks", objects.NewBuiltinFunction("get_asyncgen_hooks", getAsyncgenHooks)); err != nil {
		return nil, err
	}
	if err := setItem(md, "set_asyncgen_hooks", objects.NewBuiltinFunction("set_asyncgen_hooks", setAsyncgenHooks)); err != nil {
		return nil, err
	}
	// sys._getframe([depth]) returns the frame depth levels up the call
	// stack. depth=0 is the immediate caller's frame.
	//
	// CPython: Python/sysmodule.c:1180 sys__getframe_impl
	if err := setItem(md, "_getframe", objects.NewBuiltinFunction("_getframe", getFrame)); err != nil {
		return nil, err
	}
	// sys._getframemodulename(depth) returns func.__module__ for the
	// function whose frame is depth levels above. doctest's
	// _normalize_module prefers this over _getframe + f_globals so it
	// can route through sys.modules even when the live module was
	// imported under a different key (e.g. __main__).
	//
	// CPython: Python/sysmodule.c:2595 sys__getframemodulename_impl
	if err := setItem(md, "_getframemodulename", objects.NewBuiltinFunction("_getframemodulename", getFrameModuleName)); err != nil {
		return nil, err
	}
	// sys.gettrace / settrace stubs. The vm package overwrites these with the
	// real monitoring-aware implementations via SysTraceBuiltinsHook (set in
	// vm's init). The stubs exist so the attributes are present before vm
	// wires in; callers that run without vm (e.g. pure-parse paths) get the
	// no-op fallback.
	//
	// CPython: Python/sysmodule.c:1145 sys_settrace, 1202 sys_gettrace_impl
	if err := setItem(md, "gettrace", objects.NewBuiltinFunction("gettrace", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return objects.None(), nil
	})); err != nil {
		return nil, err
	}
	if err := setItem(md, "settrace", objects.NewBuiltinFunction("settrace", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return objects.None(), nil
	})); err != nil {
		return nil, err
	}
	// Overwrite stubs with real implementations when the vm hook is wired.
	if SysTraceBuiltinsHook != nil {
		if err := SysTraceBuiltinsHook(md); err != nil {
			return nil, err
		}
	}
	// sys.displayhook prints the value to sys.stdout and stores it in
	// builtins._, skipping None. doctest's runner installs its own
	// displayhook during test execution, but the bare module attribute
	// must exist before that swap.
	//
	// CPython: Python/sysmodule.c:742 sys_displayhook
	if err := setItem(md, "displayhook", objects.NewBuiltinFunction("displayhook", displayHook)); err != nil {
		return nil, err
	}
	if err := setItem(md, "__displayhook__", objects.NewBuiltinFunction("__displayhook__", displayHook)); err != nil {
		return nil, err
	}
	// sys.audit dispatches an audit event to any registered hooks.
	// pickle.Unpickler.find_class fires "pickle.find_class" at every
	// GLOBAL opcode, so importing/loading pickled payloads needs the
	// slot wired even when no hooks are installed.
	//
	// CPython: Python/sysmodule.c:565 sys_audit_impl
	if err := setItem(md, "audit", objects.NewBuiltinFunction("audit", sysAudit)); err != nil {
		return nil, err
	}
	if err := setItem(md, "addaudithook", objects.NewBuiltinFunction("addaudithook", sysAddAuditHook)); err != nil {
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
	if err := setItem(md, "stdout", makeStdoutFile()); err != nil {
		return nil, err
	}
	if err := setItem(md, "stderr", makeStderrFile()); err != nil {
		return nil, err
	}
	if err := setItem(md, "__stdin__", objects.NewFile(os.Stdin, "<stdin>", "r", false, true, false)); err != nil {
		return nil, err
	}
	if err := setItem(md, "__stdout__", makeStdoutFile()); err != nil {
		return nil, err
	}
	if err := setItem(md, "__stderr__", makeStderrFile()); err != nil {
		return nil, err
	}
	// breakpoint() dispatches to sys.breakpointhook; the default
	// (sys.__breakpointhook__) is the builtins-side sys_breakpointhook
	// port, registered via SetBreakpointHook before sys is built.
	//
	// CPython: Python/sysmodule.c:2826 breakpointhook PyMethodDef
	if pendingBreakpointHook != nil {
		if err := setItem(md, "breakpointhook", pendingBreakpointHook); err != nil {
			return nil, err
		}
		if err := setItem(md, "__breakpointhook__", pendingBreakpointHook); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// switchInterval is the current GIL switch interval in seconds.
// gopy uses a GIL-free goroutine model, so reads and writes are
// no-ops that keep the value coherent for Python code that inspects it.
var switchInterval = 0.005 // CPython default: 5 ms

// getSwitchInterval implements sys.getswitchinterval().
//
// CPython: Python/sysmodule.c:1278 sys_getswitchinterval_impl
func getSwitchInterval(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewFloat(switchInterval), nil
}

// setSwitchInterval implements sys.setswitchinterval(interval).
//
// CPython: Python/sysmodule.c:1261 sys_setswitchinterval_impl
func setSwitchInterval(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: setswitchinterval() takes exactly one argument (%d given)", len(args))
	}
	var v float64
	switch a := args[0].(type) {
	case *objects.Float:
		v = a.Float64()
	case *objects.Int:
		n, _ := a.Int64()
		v = float64(n)
	default:
		return nil, fmt.Errorf("TypeError: a float is required")
	}
	if v <= 0 {
		return nil, fmt.Errorf("ValueError: switch interval must be strictly positive")
	}
	switchInterval = v
	return objects.None(), nil
}

// displayHook implements sys.displayhook: skip None, then print repr(o)
// to sys.stdout and store o in builtins._. Used by the REPL and by
// doctest's runner to capture the value of a top-level expression.
//
// CPython: Python/sysmodule.c:742 sys_displayhook
func displayHook(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: displayhook() takes exactly one argument (%d given)", len(args))
	}
	o := args[0]
	if o == objects.None() {
		return objects.None(), nil
	}
	d := liveSysDict()
	var out objects.Object
	if d != nil {
		out, _ = d.GetItem(objects.NewStr("stdout"))
	}
	if out == nil || out == objects.None() {
		return nil, fmt.Errorf("RuntimeError: lost sys.stdout")
	}
	r, err := objects.Repr(o)
	if err != nil {
		return nil, err
	}
	write, err := objects.GetAttr(out, objects.NewStr("write"))
	if err != nil {
		return nil, err
	}
	if _, err := objects.Call(write, objects.NewTuple([]objects.Object{objects.NewStr(r + "\n")}), nil); err != nil {
		return nil, err
	}
	return objects.None(), nil
}

// internShim is the inittab-time form of sys.intern. The thread-aware
// variant in helpers.go drops a PyExc_TypeError on the thread; the
// inittab path has no thread handle, so the shim relies on the Go
// error to carry the same TypeError message back to the VM.
//
// CPython: Python/sysmodule.c:1004 sys_intern_impl
func internShim(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, errInternArity(len(args))
	}
	if args[0].Type() != objects.StrType() {
		return nil, errInternType(args[0].Type().Name)
	}
	return args[0], nil
}

func errInternArity(n int) error {
	return fmt.Errorf("TypeError: intern() takes exactly one argument (%d given)", n)
}

func errInternType(name string) error {
	return fmt.Errorf("TypeError: can't intern %s", name)
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
