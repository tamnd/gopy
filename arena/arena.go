// Package arena is a bump allocator with a linked list of fixed-size
// blocks. It mirrors cpython/Python/pyarena.c and serves the compiler
// pipeline (parser, AST, symtable, codegen) where many small nodes
// share a single lifetime.
//
// The arena is not safe for concurrent use. Callers serialize
// externally, the same as CPython.
package arena

const (
	defaultBlockSize = 8192
	alignment        = 8
)

// block is one fixed-size chunk in the arena's linked list.
//
// CPython: Python/pyarena.c:L14 _block
type block struct {
	mem  []byte
	off  int
	next *block
}

// newBlock allocates a block of the given size, growing it to
// defaultBlockSize when smaller.
//
// CPython: Python/pyarena.c:L74 block_new
func newBlock(size int) *block {
	if size < defaultBlockSize {
		size = defaultBlockSize
	}
	return &block{mem: make([]byte, size)}
}

// Arena is a bump allocator. The zero value is not usable; call New.
//
// CPython: Python/pyarena.c:L43 _arena
type Arena struct {
	head    *block
	cur     *block
	objects []any
}

// New returns an arena with one preallocated block.
//
// CPython: Python/pyarena.c:L125 _PyArena_New
func New() *Arena {
	b := newBlock(defaultBlockSize)
	return &Arena{head: b, cur: b}
}

// Malloc returns a byte slice of length n carved out of the current
// block, allocating a new block if needed. The slice is capped so
// callers cannot append into the next allocation. n is rounded up
// to the alignment boundary; the returned slice has the rounded
// length, matching what _PyArena_Malloc hands out in C.
//
// CPython: Python/pyarena.c:L177 _PyArena_Malloc
func (a *Arena) Malloc(n int) []byte {
	if n < 0 {
		panic("arena: negative size")
	}
	n = roundUp(n, alignment)
	if n == 0 {
		return nil
	}
	if a.cur.off+n > len(a.cur.mem) {
		nb := newBlock(n)
		a.cur.next = nb
		a.cur = nb
	}
	off := a.cur.off
	a.cur.off += n
	return a.cur.mem[off : off+n : off+n]
}

// AddObject registers obj so it is retained until Free is called.
// The error return mirrors the C call shape; in practice the value
// is always nil.
//
// CPython: Python/pyarena.c:L200 _PyArena_AddPyObject
func (a *Arena) AddObject(obj any) error {
	a.objects = append(a.objects, obj)
	return nil
}

// Free drops every block and tracked object, so the GC can reclaim
// them. The arena must not be used after Free.
//
// CPython: Python/pyarena.c:L154 _PyArena_Free
func (a *Arena) Free() {
	a.head = nil
	a.cur = nil
	a.objects = nil
}

// roundUp returns n rounded up to a multiple of align. Mirrors the
// inline arithmetic inside block_alloc.
//
// CPython: Python/pyarena.c:L99 block_alloc (adapted from)
func roundUp(n, align int) int {
	return (n + align - 1) &^ (align - 1)
}
