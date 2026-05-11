// Package stdlibinit is the gopy equivalent of CPython's
// Modules/config.c.in. The C build generates that file with one
// extern declaration plus one _PyImport_Inittab[] row per built-in
// module; the linker then materializes the inittab at startup.
//
// gopy uses Go init() blocks instead: each module package
// (module/gc/, module/contextvars/, ...) calls imp.AppendInittab
// from its own init(). Those init blocks only run when their
// package is imported somewhere in the dependency graph. Without
// a central registration site, cmd/gopy/main.go would have to
// blank-import every module package by hand, and forgetting one
// means import gc silently raises ModuleNotFoundError at runtime.
//
// This package centralizes that registration: blank-importing
// stdlibinit pulls in every gopy module package, which forces every
// init() to run, which populates imp.Inittab. cmd/gopy/main.go
// blank-imports stdlibinit and nothing else has to change.
//
// CPython: Modules/config.c.in:26 _PyImport_Inittab[]
// CPython: Python/import.c:2403 _PyImport_FindBuiltin
package stdlibinit

import (
	// Built-in module: gc. Registers itself via module/gc/module.go
	// init().
	// CPython: Modules/config.c.in:47 {"gc", PyInit_gc}
	_ "github.com/tamnd/gopy/module/gc"

	// Built-in module: _contextvars. Registers itself via
	// module/contextvars/module.go init().
	// CPython: Modules/config.c.in:50 {"_contextvars", PyInit__contextvars}
	_ "github.com/tamnd/gopy/module/contextvars"

	// Built-in module: sys. Registers itself via module/sys/module.go
	// init().
	// CPython: Modules/config.c.in:42 {"sys", _PyImport_BuiltinSys}
	_ "github.com/tamnd/gopy/module/sys"

	// Built-in module: _io. Registers itself via module/io/module.go
	// init(). The current port covers StringIO; the rest of the
	// type family is stubbed so io.py's `from _io import (...)`
	// resolves at name-lookup time.
	// CPython: Modules/config.c.in:48 {"_io", PyInit__io}
	_ "github.com/tamnd/gopy/module/io"

	// Built-in module: _types. Re-exports the canonical type
	// singletons (FunctionType, MappingProxyType, SimpleNamespace,
	// ...) consumed by the vendored Lib/types.py.
	// CPython: Modules/Setup.bootstrap.in:28 _types _typesmodule.c
	_ "github.com/tamnd/gopy/module/types"

	// Built-in module: errno. Registers itself via
	// module/errno/module.go init(). Exposes the host platform's
	// POSIX errno constants plus the reverse `errorcode` dict.
	// CPython: Modules/config.c.in:46 {"errno", PyInit_errno}
	_ "github.com/tamnd/gopy/module/errno"

	// Built-in module: _functools. Registers itself via
	// module/_functools/module.go init(). Backs Lib/functools.py with
	// the partial type, the cmp_to_key key class, reduce, and the
	// _lru_cache_wrapper used by @lru_cache.
	// CPython: Modules/config.c.in:45 {"_functools", PyInit__functools}
	_ "github.com/tamnd/gopy/module/_functools"

	// Built-in module: _operator. Registers itself via
	// module/_operator/module.go init(). Backs Lib/operator.py with
	// the fast paths for arithmetic, comparisons, item access, plus
	// the itemgetter/attrgetter/methodcaller callable types.
	// CPython: Modules/config.c.in:44 {"_operator", PyInit__operator}
	_ "github.com/tamnd/gopy/module/_operator"

	// Go-backed Python modules: not in CPython's config.c.in (those
	// are pure-Python in Lib/), but until the corresponding Lib/*.py
	// vendoring lands the import system needs something to satisfy
	// `import traceback` and friends pulled in by unittest.
	_ "github.com/tamnd/gopy/module/argparse"
	_ "github.com/tamnd/gopy/module/collections"
	_ "github.com/tamnd/gopy/module/colorize"
	_ "github.com/tamnd/gopy/module/contextlib"
	_ "github.com/tamnd/gopy/module/dataclasses"
	_ "github.com/tamnd/gopy/module/difflib"
	_ "github.com/tamnd/gopy/module/fnmatch"
	_ "github.com/tamnd/gopy/module/functools"
	_ "github.com/tamnd/gopy/module/os"
	_ "github.com/tamnd/gopy/module/pprint"
	_ "github.com/tamnd/gopy/module/re"
	_ "github.com/tamnd/gopy/module/signal"
	_ "github.com/tamnd/gopy/module/time"
	_ "github.com/tamnd/gopy/module/traceback"
	_ "github.com/tamnd/gopy/module/warnings"
	_ "github.com/tamnd/gopy/module/weakref"

	// Built-in module: _weakref. Registers itself via
	// module/_weakref/module.go init(). Ports Modules/_weakref.c,
	// publishing ref / ProxyType / CallableProxyType plus the four
	// module functions getweakrefcount, _remove_dead_weakref,
	// getweakrefs, proxy.
	// CPython: Modules/config.c.in:49 {"_weakref", PyInit__weakref}
	_ "github.com/tamnd/gopy/module/_weakref"

	// Built-in module: _abc. Registers itself via module/_abc/module.go
	// init(). Ports Modules/_abc.c: ABC machinery backing Lib/abc.py.
	// CPython: Modules/config.c.in:44 {"_abc", PyInit__abc}
	_ "github.com/tamnd/gopy/module/_abc"
)
