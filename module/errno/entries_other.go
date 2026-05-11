// Fallback errno table for GOOSes that gopy has not enumerated by hand
// yet (everything except darwin and linux: freebsd, openbsd, netbsd,
// windows, plan9, js, wasip1). Returns an empty list so the module
// still loads and `import errno; errno.errorcode` works; only the E*
// names are missing on the unsupported platform. The CPython source
// gates each constant individually behind #ifdef, so an empty fallback
// is the structural equivalent: no host symbols defined, no Python
// attributes published.
//
// CPython: Modules/errnomodule.c:121 add_errcode block (all #ifdef arms false)

//go:build !darwin && !linux && !windows

package errno

// errnoEntries returns the (name, code) pairs the errno module exposes
// on platforms gopy has not curated. Currently empty so the build is
// safe across all Go GOOS targets.
//
// CPython: Modules/errnomodule.c:88 errno_exec (no-platform slice)
func errnoEntries() []errnoEntry {
	return nil
}
