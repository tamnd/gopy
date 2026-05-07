package errors

import "github.com/tamnd/gopy/objects"

// Warning is the root of the runtime-warning class tree. All warnings
// raised by the warnings module bottom out here. Subclasses partition
// the categories the default filter list distinguishes (deprecation
// vs. user vs. resource and so on).
//
// CPython: Objects/exceptions.c:2865 PyExc_Warning
var (
	PyExc_Warning                   = newExcType("Warning", []*objects.Type{PyExc_Exception})
	PyExc_UserWarning               = newExcType("UserWarning", []*objects.Type{PyExc_Warning})
	PyExc_DeprecationWarning        = newExcType("DeprecationWarning", []*objects.Type{PyExc_Warning})
	PyExc_PendingDeprecationWarning = newExcType("PendingDeprecationWarning", []*objects.Type{PyExc_Warning})
	PyExc_SyntaxWarning             = newExcType("SyntaxWarning", []*objects.Type{PyExc_Warning})
	PyExc_RuntimeWarning            = newExcType("RuntimeWarning", []*objects.Type{PyExc_Warning})
	PyExc_FutureWarning             = newExcType("FutureWarning", []*objects.Type{PyExc_Warning})
	PyExc_ImportWarning             = newExcType("ImportWarning", []*objects.Type{PyExc_Warning})
	PyExc_UnicodeWarning            = newExcType("UnicodeWarning", []*objects.Type{PyExc_Warning})
	PyExc_BytesWarning              = newExcType("BytesWarning", []*objects.Type{PyExc_Warning})
	PyExc_ResourceWarning           = newExcType("ResourceWarning", []*objects.Type{PyExc_Warning})
	PyExc_EncodingWarning           = newExcType("EncodingWarning", []*objects.Type{PyExc_Warning})
)
