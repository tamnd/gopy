// Package build holds the static version, platform, compiler, and
// copyright strings reported by the gopy runtime. Mirrors CPython's
// getversion.c, getplatform.c, getcompiler.c, and getcopyright.c.
package build

// Version is the gopy release version. Bumped per release tag.
const Version = "0.12.1"

// PythonCompatVersion is the upstream CPython version this port tracks.
const PythonCompatVersion = "3.14.0+"

// PythonMajorVersion is the major component of the CPython version
// gopy tracks. Mirrors Include/patchlevel.h:20 PY_MAJOR_VERSION.
const PythonMajorVersion = 3

// PythonMinorVersion is the minor component of the CPython version
// gopy tracks. Mirrors Include/patchlevel.h:21 PY_MINOR_VERSION.
const PythonMinorVersion = 14

// VersionString returns the full version banner.
//
//	gopy 0.1.0 (3.14.0+) [go1.26 darwin/arm64]
//
// CPython: Python/getversion.c:27 Py_GetVersion
func VersionString() string {
	return "gopy " + Version + " (" + PythonCompatVersion + ") [" +
		Compiler() + " " + Platform() + "]"
}
