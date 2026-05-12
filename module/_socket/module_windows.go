//go:build windows

// Windows stub: the _socket port uses POSIX fd (int) semantics that do not
// map directly to Windows syscall.Handle (uintptr). The module registers as
// an empty built-in so that `import _socket` resolves without raising
// ModuleNotFoundError; socket.py functionality is not available on Windows.
//
// CPython: Modules/socketmodule.c

package _socket

import (
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("_socket", buildModule)
}

func buildModule() (*objects.Module, error) {
	return objects.NewModule("_socket"), nil
}
