// _PyObjectStack port. CPython implements the cycle collector's
// scratch work-queue as a linked list of fixed-size chunks so the
// per-collection allocator overhead stays bounded; the chunks come
// from a freelist on the C side. gopy keeps the same chunk-list
// shape but lets Go's GC own the freelist.
//
// CPython: Python/object_stack.c
// CPython: Include/internal/pycore_object_stack.h:14 _PyObjectStack

package gc

import (
	"github.com/tamnd/gopy/objects"
)

// objectStackChunkSize matches CPython's _Py_OBJECT_STACK_CHUNK_SIZE.
//
// CPython: Include/internal/pycore_object_stack.h:16 _Py_OBJECT_STACK_CHUNK_SIZE
const objectStackChunkSize = 254

// objectStackChunk is one segment of the linked-list stack.
//
// CPython: Include/internal/pycore_object_stack.h:18 _PyObjectStackChunk
type objectStackChunk struct {
	prev *objectStackChunk
	n    int
	objs [objectStackChunkSize]objects.Object
}

// objectStack is the head pointer wrapping the chunk chain.
//
// CPython: Include/internal/pycore_object_stack.h:24 _PyObjectStack
type objectStack struct {
	head *objectStackChunk
}

// push appends obj. Allocates a fresh chunk on the head when the
// current one is full or absent.
//
// CPython: Include/internal/pycore_object_stack.h:37 _PyObjectStack_Push
func (s *objectStack) push(obj objects.Object) {
	buf := s.head
	if buf == nil || buf.n == objectStackChunkSize {
		buf = &objectStackChunk{prev: s.head}
		s.head = buf
	}
	buf.objs[buf.n] = obj
	buf.n++
}

// pop returns the top item or nil when the stack is empty. CPython
// frees the chunk back to its freelist when it drops to empty; gopy
// just unlinks it and lets the runtime reclaim memory.
//
// CPython: Include/internal/pycore_object_stack.h:58 _PyObjectStack_Pop
func (s *objectStack) pop() objects.Object {
	buf := s.head
	if buf == nil {
		return nil
	}
	buf.n--
	obj := buf.objs[buf.n]
	buf.objs[buf.n] = nil
	if buf.n == 0 {
		s.head = buf.prev
	}
	return obj
}

// size walks the chunk chain summing per-chunk counts.
//
// CPython: Include/internal/pycore_object_stack.h:75 _PyObjectStack_Size
func (s *objectStack) size() int {
	n := 0
	for buf := s.head; buf != nil; buf = buf.prev {
		n += buf.n
	}
	return n
}

// clear pops every chunk so the receiver is empty.
//
// CPython: Python/object_stack.c:39 _PyObjectStack_Clear
func (s *objectStack) clear() {
	for s.head != nil {
		s.head.n = 0
		s.head = s.head.prev
	}
}

// merge appends every chunk from src onto dst, leaving src empty.
// CPython splices src's bottom-most prev pointer to dst's old head;
// we follow the same step.
//
// CPython: Python/object_stack.c:48 _PyObjectStack_Merge
func (s *objectStack) merge(src *objectStack) {
	if src.head == nil {
		return
	}
	if s.head != nil {
		last := src.head
		for last.prev != nil {
			last = last.prev
		}
		last.prev = s.head
	}
	s.head = src.head
	src.head = nil
}
