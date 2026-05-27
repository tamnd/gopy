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

// NewUnicodeEncodeError returns a UnicodeEncodeError instance with the
// standard .encoding, .object, .start, .end, .reason attributes stored
// in the per-instance dict so Python-level error handlers can read them.
//
// CPython: Objects/exceptions.c:3040 UnicodeError_init
func NewUnicodeEncodeError(encoding string, obj objects.Object, start, end int, reason string) *Exception {
	args := objects.NewTuple([]objects.Object{
		objects.NewStr(encoding), obj,
		objects.NewInt(int64(start)), objects.NewInt(int64(end)),
		objects.NewStr(reason),
	})
	exc := New(PyExc_UnicodeEncodeError, args)
	d := exc.EnsureAttrDict()
	_ = d.SetItem(objects.NewStr("encoding"), objects.NewStr(encoding))
	_ = d.SetItem(objects.NewStr("object"), obj)
	_ = d.SetItem(objects.NewStr("start"), objects.NewInt(int64(start)))
	_ = d.SetItem(objects.NewStr("end"), objects.NewInt(int64(end)))
	_ = d.SetItem(objects.NewStr("reason"), objects.NewStr(reason))
	return exc
}

// CPython: Objects/exceptions.c:3030 Py_UNICODE_ENCODE_ERROR_NAME panel
var (
	PyExc_UnicodeError          = newExcType("UnicodeError", []*objects.Type{PyExc_ValueError})
	PyExc_UnicodeEncodeError    = newExcType("UnicodeEncodeError", []*objects.Type{PyExc_UnicodeError})
	PyExc_UnicodeDecodeError    = newExcType("UnicodeDecodeError", []*objects.Type{PyExc_UnicodeError})
	PyExc_UnicodeTranslateError = newExcType("UnicodeTranslateError", []*objects.Type{PyExc_UnicodeError})
)
