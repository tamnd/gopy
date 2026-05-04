// Package ast holds the Python AST data model. Mirrors the asdl-driven
// node tree from cpython/Parser/Python.asdl plus the helpers in
// cpython/Python/asdl.c and cpython/Include/internal/pycore_asdl.h.
//
// The asdl source-of-truth is `cpython/Parser/Python.asdl`; the
// generated node structs live in nodes_gen.go (produced by
// tools/asdl_go from that .asdl file). This file ports the runtime
// helpers.
//
// Spec: notes/Spec/1600/1620_gopy_compile_pipeline.md
package ast

// Seq is the asdl_seq* equivalent. CPython stores sequence length and
// elements inline in a heap struct allocated from the per-compile
// arena; in Go a typed slice is the same shape with an O(1) length
// field (cap/len) and arena-friendly bulk allocation through the
// underlying slice header.
//
// CPython: Include/internal/pycore_asdl.h:L30 asdl_seq
type Seq[T any] []T

// Len mirrors asdl_seq_LEN. Returns 0 for a nil sequence.
//
// CPython: Include/internal/pycore_asdl.h:L83 asdl_seq_LEN
func (s Seq[T]) Len() int { return len(s) }

// Get mirrors asdl_seq_GET. The bounds check matches CPython's
// debug-build check; release CPython elides it but a panic on Go
// slice access is the equivalent guarantee.
//
// CPython: Include/internal/pycore_asdl.h:L82 asdl_seq_GET
func (s Seq[T]) Get(i int) T { return s[i] }

// Set mirrors asdl_seq_SET.
//
// CPython: Include/internal/pycore_asdl.h:L86 asdl_seq_SET
func (s Seq[T]) Set(i int, v T) { s[i] = v }

// NewSeq mirrors _Py_asdl_generic_seq_new (and the typed variants
// _Py_asdl_identifier_seq_new, _Py_asdl_int_seq_new). A zero-length
// sequence is allocated with an empty slice, not nil, so that Len()
// is 0 but the sequence itself is non-nil. CPython does the same:
// _Py_asdl_*_seq_new(0, arena) returns a valid asdl_seq* with size=0.
//
// CPython: Python/asdl.c:L4 GENERATE_ASDL_SEQ_CONSTRUCTOR
func NewSeq[T any](size int) Seq[T] {
	if size == 0 {
		return Seq[T]{}
	}
	return make(Seq[T], size)
}
