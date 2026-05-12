// Port of io.open via the pure-Python reference in Lib/_pyio.py:75
// def open. CPython's C entry point is Modules/_io/_iomodule.c io_open;
// both share the same shape: validate the mode string, open the file
// descriptor, then layer the buffered / text adapters on top. This file
// delegates to _io.Open which is the canonical implementation.
//
// CPython: Lib/_pyio.py:75 open (reference Python implementation)
// CPython: Modules/_io/_iomodule.c:177 io_open

package builtins

import (
	_io "github.com/tamnd/gopy/module/io"
	"github.com/tamnd/gopy/objects"
)

// Open implements the open() builtin by delegating to _io.Open. This
// keeps a single canonical implementation in the _io module and makes
// builtins.open() behave identically to io.open(), matching CPython
// where both names point at the same C function.
//
// CPython: Lib/_pyio.py:193 open body
// CPython: Modules/_io/_iomodule.c:177 io_open
func Open(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return _io.Open(args, kwargs)
}
