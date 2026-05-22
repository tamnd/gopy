package errors

import "github.com/tamnd/gopy/objects"

// PyExc_UnsupportedOperation is the io.UnsupportedOperation class.
// CPython builds it at PyInit__io time as a type with two bases:
// OSError and ValueError. The dual ancestry is load-bearing: stdlib
// code routinely catches OSError (e.g. _colorize.can_colorize) or
// ValueError (e.g. fileio's err_closed callers) and both branches
// must match a UnsupportedOperation instance.
//
// CPython: Modules/_io/_iomodule.c:709 _PyIO_get_module_state (UnsupportedOperation registration)
var PyExc_UnsupportedOperation = newExcType("io.UnsupportedOperation",
	[]*objects.Type{PyExc_OSError, PyExc_ValueError})
