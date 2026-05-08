// Package stdlibinit is the gopy equivalent of CPython's
// Modules/config.c.in. The C build generates that file with one
// extern declaration plus one _PyImport_Inittab[] row per built-in
// module; the linker then materializes the inittab at startup.
//
// gopy uses Go init() blocks instead: each module package
// (gc/, contextvar/, ...) calls imp.AppendInittab from its own
// init(). Those init blocks only run when their package is imported
// somewhere in the dependency graph. Without a central registration
// site, cmd/gopy/main.go would have to blank-import every module
// package by hand, and forgetting one means import gc silently
// raises ModuleNotFoundError at runtime.
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
	// Built-in module: gc. Registers itself via gc/module.go init().
	// CPython: Modules/config.c.in:47 {"gc", PyInit_gc}
	_ "github.com/tamnd/gopy/gc"

	// Built-in module: _contextvars. Registers itself via
	// contextvar/module.go init().
	// CPython: Modules/config.c.in:50 {"_contextvars", PyInit__contextvars}
	_ "github.com/tamnd/gopy/contextvar"

	// Built-in module: sys. Registers itself via sys/module.go init().
	// CPython: Modules/config.c.in:42 {"sys", _PyImport_BuiltinSys}
	_ "github.com/tamnd/gopy/sys"
)
