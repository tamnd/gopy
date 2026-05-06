//go:build darwin || linux

package pathconfig

import "github.com/tamnd/gopy/build"

// homeDelim is the PATH-style separator used to split PYTHONHOME and
// PYTHONPATH on POSIX. Mirrors getpath.py's DELIM = ':'.
//
// CPython: Modules/getpath.py:188 DELIM
func homeDelim() rune { return ':' }

// defaultProgramName returns the per-platform default for
// program_name when the caller did not provide one.
//
// CPython: Modules/getpath.py:181 DEFAULT_PROGRAM_NAME
func defaultProgramName() string {
	return "python" + itoa(build.PythonMajorVersion)
}
