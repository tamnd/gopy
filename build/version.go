// Package build holds the static version, platform, compiler, and
// copyright strings reported by the gopy runtime. Mirrors CPython's
// getversion.c, getplatform.c, getcompiler.c, and getcopyright.c.
package build

// Version is the gopy release version. Bumped per release tag.
const Version = "0.2.0"

// PythonCompatVersion is the upstream CPython version this port tracks.
const PythonCompatVersion = "3.14.0+"

// VersionString returns the full version banner.
//
//	gopy 0.1.0 (3.14.0+) [go1.26 darwin/arm64]
func VersionString() string {
	return "gopy " + Version + " (" + PythonCompatVersion + ") [" +
		Compiler() + " " + Platform() + "]"
}
