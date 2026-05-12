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

	// Built-in module: _warnings. Registers itself via
	// module/_warnings/module.go init(). 1:1 port of CPython's
	// Python/_warnings.c, backing the vendored Lib/warnings.py and
	// the C-level PyErr_Warn family.
	// CPython: Modules/config.c.in:51 {"_warnings", _PyWarnings_Init}
	_ "github.com/tamnd/gopy/module/_warnings"

	// Built-in module: time. Registers itself via
	// module/_time/module.go init(). The Go directory name carries an
	// underscore prefix to avoid colliding with Go's stdlib `time`
	// package; the registered Python name is the bare `time`.
	// CPython: Modules/config.c.in:43 {"time", PyInit_time}
	_ "github.com/tamnd/gopy/module/_time"

	// Built-in module: itertools. Registers itself via
	// module/_itertools/module.go init(). The Go directory carries the
	// underscore prefix; the registered Python module name is the bare
	// `itertools`. 1:1 port of Modules/itertoolsmodule.c.
	// CPython: Modules/config.c.in:46 {"itertools", PyInit_itertools}
	_ "github.com/tamnd/gopy/module/_itertools"

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

	// Built-in module: _collections. Registers itself via
	// module/_collections/module.go init(). Ports
	// Modules/_collectionsmodule.c: deque, defaultdict, _tuplegetter,
	// _deque_iterator, _deque_reverse_iterator, _count_elements.
	// CPython: Modules/config.c.in:46 {"_collections", PyInit__collections}
	_ "github.com/tamnd/gopy/module/_collections"

	// Built-in module: _thread. Registers itself via module/_thread/module.go
	// init(). Exposes goroutine identity, lock allocation, and new-thread
	// creation backing Lib/threading.py and reprlib.py.
	// CPython: Modules/_threadmodule.c:1 (module init)
	_ "github.com/tamnd/gopy/module/_thread"

	// Go-backed Python modules: not in CPython's config.c.in (those
	// are pure-Python in Lib/), but until the corresponding Lib/*.py
	// vendoring lands the import system needs something to satisfy
	// `import traceback` and friends pulled in by unittest.
	// Built-in module: _sre. Registers itself via module/_sre/module.go
	// init(). Backs Lib/re/ with the compiled pattern engine. Go's
	// regexp/RE2 backend.
	// CPython: Modules/_sre/sre.c:1 (module init)
	_ "github.com/tamnd/gopy/module/_sre"

	_ "github.com/tamnd/gopy/module/argparse"
	_ "github.com/tamnd/gopy/module/contextlib"
	_ "github.com/tamnd/gopy/module/dataclasses"
	_ "github.com/tamnd/gopy/module/fnmatch"
	_ "github.com/tamnd/gopy/module/functools"
	_ "github.com/tamnd/gopy/module/os"
	_ "github.com/tamnd/gopy/module/signal"
	_ "github.com/tamnd/gopy/module/traceback"
	_ "github.com/tamnd/gopy/module/weakref"
)
