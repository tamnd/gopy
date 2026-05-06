// gc_list_* helpers from CPython gc.c. CPython packs the prev/next
// links inside the PyGC_Head that sits before every tracked PyObject
// in memory; gopy keeps gcHead as an explicit struct because the Go
// runtime owns object layout. Flags that CPython encodes in the low
// bits of _gc_prev (FINALIZED, COLLECTING) are plain fields here.
//
// CPython: Python/gc.c:202 list functions
// CPython: Include/internal/pycore_gc.h:115 _gc_prev flag bits

package gc

import (
	"github.com/tamnd/gopy/objects"
)

// gcFlag bits track CPython's _PyGC_PREV_MASK_FINALIZED and
// _PyGC_PREV_MASK_COLLECTING. Bits 0 and 1 mirror the C layout so
// the helpers below read like the originals.
//
// CPython: Include/internal/pycore_gc.h:117 _PyGC_PREV_MASK_FINALIZED
type gcFlag uint8

const (
	gcFinalized  gcFlag = 1 << 0
	gcCollecting gcFlag = 1 << 1
)

// gcHead is the doubly-linked list node CPython embeds in every
// tracked object as PyGC_Head. obj points at the gopy object so the
// collector can find its way from a list node back to the live
// object without unsafe pointer arithmetic. The refs scratch slot
// CPython packs into the upper bits of _gc_prev lands with 1613-F
// (gc/refs.go) when update_refs needs it.
//
// CPython: Include/internal/pycore_interp_structs.h PyGC_Head
type gcHead struct {
	prev, next *gcHead
	obj        objects.Object
	flags      gcFlag
}

// newListHead returns an empty list with prev and next pointing at
// itself, matching gc_list_init.
//
// CPython: Python/gc.c:204 gc_list_init
func newListHead() *gcHead {
	h := &gcHead{}
	h.prev = h
	h.next = h
	return h
}

// listIsEmpty mirrors gc_list_is_empty.
//
// CPython: Python/gc.c:213 gc_list_is_empty
func listIsEmpty(list *gcHead) bool {
	return list.next == list
}

// listAppend appends node to list (at the tail).
//
// CPython: Python/gc.c:221 gc_list_append
func listAppend(node, list *gcHead) {
	last := list.prev
	last.next = node
	node.prev = last
	node.next = list
	list.prev = node
}

// listRemove unlinks node from whatever list it currently sits on.
// CPython zeroes _gc_next afterwards as the "not currently tracked"
// sentinel; we set both pointers to nil for the same effect.
//
// CPython: Python/gc.c:236 gc_list_remove
func listRemove(node *gcHead) {
	prev := node.prev
	next := node.next
	prev.next = next
	next.prev = prev
	node.prev = nil
	node.next = nil
}

// listMove unlinks node from its current list and appends it to the
// tail of list.
//
// CPython: Python/gc.c:252 gc_list_move
func listMove(node, list *gcHead) {
	fromPrev := node.prev
	fromNext := node.next
	fromPrev.next = fromNext
	fromNext.prev = fromPrev

	toPrev := list.prev
	toPrev.next = node
	node.prev = toPrev
	node.next = list
	list.prev = node
}

// listMerge moves every node from "from" onto the tail of "to" and
// leaves "from" empty.
//
// CPython: Python/gc.c:271 gc_list_merge
func listMerge(from, to *gcHead) {
	if from == to {
		panic("gc: listMerge: from == to")
	}
	if listIsEmpty(from) {
		return
	}
	toTail := to.prev
	fromHead := from.next
	fromTail := from.prev

	toTail.next = fromHead
	fromHead.prev = toTail

	fromTail.next = to
	to.prev = fromTail

	from.prev = from
	from.next = from
}

// listSize counts the number of nodes on list excluding the head.
//
// CPython: Python/gc.c:291 gc_list_size
func listSize(list *gcHead) int {
	n := 0
	for g := list.next; g != list; g = g.next {
		n++
	}
	return n
}

// listClearCollecting clears the COLLECTING bit on every node in
// collectable.
//
// CPython: Python/gc.c:303 gc_list_clear_collecting
func listClearCollecting(collectable *gcHead) {
	for g := collectable.next; g != collectable; g = g.next {
		g.flags &^= gcCollecting
	}
}
