package errors

import "github.com/tamnd/gopy/objects"

// BaseExceptionGroup wraps a sequence of leaf exceptions that all
// surfaced from a single try-block. PEP 654 splits it from
// ExceptionGroup so user code can match the BaseException-only form
// (KeyboardInterrupt, SystemExit) without losing the structural
// grouping. ExceptionGroup is the more common Exception-only form
// the BaseExceptionGroup constructor promotes to when every leaf is
// an Exception.
//
// CPython: Objects/exceptions.c:873 BaseExceptionGroup
// CPython: Objects/exceptions.c:874 ExceptionGroup
var (
	PyExc_BaseExceptionGroup = newExcType("BaseExceptionGroup", []*objects.Type{PyExc_BaseException})
	PyExc_ExceptionGroup     = newExcType("ExceptionGroup", []*objects.Type{PyExc_BaseExceptionGroup, PyExc_Exception})
)

// ExceptionGroupInfo carries the structured payload: the message
// string and the tuple of nested exceptions. Subgroup/Split use this
// to project a subset out of a group while preserving ordering.
//
// CPython: Objects/exceptions.c:886 BaseExceptionGroup_new
type ExceptionGroupInfo struct {
	Message    objects.Object
	Exceptions []*Exception
}
