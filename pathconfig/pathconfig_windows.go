//go:build windows

package pathconfig

import "github.com/tamnd/gopy/build"

// homeDelim is the PATH-style separator used to split PYTHONHOME and
// PYTHONPATH on Windows. Mirrors getpath.py's DELIM = ';'.
//
// CPython: Modules/getpath.py:188 DELIM (Windows variant)
func homeDelim() rune { return ';' }

// defaultProgramName returns the per-platform default for
// program_name when the caller did not provide one.
//
// CPython: Modules/getpath.py:181 DEFAULT_PROGRAM_NAME (Windows variant)
func defaultProgramName() string {
	return "python" + itoa(build.PythonMajorVersion) + ".exe"
}
