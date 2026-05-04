package build

import "runtime"

// Platform returns the platform string in the form "GOOS/GOARCH"
// (e.g. "darwin/arm64", "linux/amd64"). Mirrors CPython's
// Py_GetPlatform.
func Platform() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}
