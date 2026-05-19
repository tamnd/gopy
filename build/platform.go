package build

import "runtime"

// Platform returns the value CPython exposes through sys.platform.
// CPython maps the C-level PLATFORM define (set by configure) to a
// short OS name without arch suffix. Windows is "win32", macOS is
// "darwin", Linux is "linux", the BSDs include their major version,
// and unknown systems fall back to the Go GOOS string.
//
// CPython: Python/getplatform.c:9 Py_GetPlatform
// CPython: configure.ac PLATFORM definition (per-target case)
func Platform() string {
	switch runtime.GOOS {
	case "windows":
		return "win32"
	case "wasip1":
		return "wasi"
	}
	return runtime.GOOS
}
