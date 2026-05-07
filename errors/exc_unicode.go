package errors

import "github.com/tamnd/gopy/objects"

// UnicodeError is the parent for codec-side decode/encode failures.
// Each subclass carries the structured payload codecs.LookupError
// expects: encoding name, the offending object, the byte/codepoint
// range that triggered the failure, and the human reason.
//
// CPython: Objects/exceptions.c:2973 UnicodeError
type UnicodeErrorInfo struct {
	Encoding objects.Object
	Object   objects.Object
	Start    int
	End      int
	Reason   objects.Object
}

// CPython: Objects/exceptions.c:3030 Py_UNICODE_ENCODE_ERROR_NAME panel
var (
	PyExc_UnicodeError          = newExcType("UnicodeError", []*objects.Type{PyExc_ValueError})
	PyExc_UnicodeEncodeError    = newExcType("UnicodeEncodeError", []*objects.Type{PyExc_UnicodeError})
	PyExc_UnicodeDecodeError    = newExcType("UnicodeDecodeError", []*objects.Type{PyExc_UnicodeError})
	PyExc_UnicodeTranslateError = newExcType("UnicodeTranslateError", []*objects.Type{PyExc_UnicodeError})
)
