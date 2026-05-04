package build

import "runtime"

// Compiler returns the Go toolchain identifier that built the running
// binary (e.g. "go1.26"). Mirrors CPython's Py_GetCompiler, which
// returns a string like "[GCC 13.2.0]".
func Compiler() string {
	return runtime.Version()
}
