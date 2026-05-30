// Per-thread frame arena. Chunks are bumped into; when one fills,
// a new chunk is linked. Pop walks back through the chain. The arena
// keeps Frame allocations bounded and amortized so the eval loop's
// hot path doesn't churn the GC.
//
// CPython: Include/internal/pycore_pystate.h _PyStackChunk
// CPython: Python/frame.c _PyThreadState_AllocateFrame

package frame

import "github.com/tamnd/gopy/objects"

// ChunkSize is the number of frames per chunk. CPython uses
// 4 KB chunks of byte-packed frame data; we use a fixed slot count
// because each Frame is fixed-size.
const ChunkSize = 32

// Chunk is a single arena slab.
type Chunk struct {
	frames [ChunkSize]Frame
	top    int    // next free slot in frames
	prev   *Chunk // previous chunk in the chain (older)
}

// FrameStack is a per-Thread chain of chunks.
//
// CPython: Include/internal/pycore_pystate.h datastack_chunk
type FrameStack struct {
	current *Chunk // newest chunk (top of stack)
	depth   int    // total live frames across all chunks
	// ForcedPrev, when non-nil, overrides the normal s.Top() predecessor
	// for the next Push call and is then cleared. Used by vm's
	// genThrowForwardHook to make a custom-iterator throw's frame inherit
	// f_back from the outer generator's frame rather than the FrameStack
	// top, mirroring CPython's tstate->current_frame = gen_frame in
	// _gen_throw before calling yf.throw().
	//
	// CPython: Objects/genobject.c:523 _gen_throw
	ForcedPrev *Frame
}

// New returns an empty frame stack.
func New() *FrameStack {
	return &FrameStack{}
}

// Push allocates a frame, initializes it for co, and links it as
// the new top of the call chain. The new frame's Previous is wired
// to whatever frame was on top before the push, mirroring
// _PyThreadState_PushFrame which uses tstate->current_frame. If
// ForcedPrev is set it is used instead of s.Top() and then cleared.
//
// CPython: Python/frame.c _PyThreadState_PushFrame
func (s *FrameStack) Push(co *objects.Code, globals, builtins, fn objects.Object) *Frame {
	prev := s.Top()
	if s.ForcedPrev != nil {
		prev = s.ForcedPrev
		s.ForcedPrev = nil
	}
	if s.current == nil || s.current.top == ChunkSize {
		s.current = &Chunk{prev: s.current}
	}
	f := &s.current.frames[s.current.top]
	s.current.top++
	s.depth++
	f.Init(co, globals, builtins, fn, prev)
	return f
}

// Pop releases the top frame. It is an error to Pop more than was
// pushed; Pop on an empty stack is a no-op.
//
// CPython: Python/frame.c _PyThreadState_PopFrame
func (s *FrameStack) Pop() {
	if s.current == nil || s.current.top == 0 {
		return
	}
	s.current.top--
	s.depth--
	f := &s.current.frames[s.current.top]
	if f.Owner == OwnedByGenerator {
		// The frame's been handed off to a Generator object; the
		// generator owns the storage now, including the LocalsPlus
		// slice, so unwire the slot wholesale. The next Push at this
		// slot will allocate fresh LocalsPlus.
		s.current.frames[s.current.top] = Frame{}
	} else {
		// f.Clear() closes every live stackref and nils out Code /
		// Globals / Builtins / Locals / Func / Previous, but leaves
		// f.LocalsPlus alone. Init() on the next Push at this same
		// slot then hits its `cap(LocalsPlus) >= size` fast path and
		// recycles the slice instead of make()ing a new one. This is
		// the gopy analog of CPython's _PyThreadState_PopFrame
		// leaving the activation-record memory in the data stack for
		// the next _PyEvalFramePushAndInit to claim.
		//
		// CPython: Include/internal/pycore_frame.h _PyFrame_Initialize
		// (recycles _PyInterpreterFrame slots in-place)
		f.Clear()
	}
	// Drop a now-empty chunk only when there is an older chunk
	// underneath it. The bottom chunk stays attached so its frame
	// slots survive across a pop-back-to-zero cycle, mirroring
	// CPython's data stack: even when the call depth hits zero
	// the thread keeps its current PyStackChunk for the next
	// _PyEvalFramePushAndInit. Without this, every benchmark that
	// returns to the module-level scope and then re-enters a call
	// would reallocate a fresh LocalsPlus slice.
	//
	// CPython: Include/internal/pycore_pystate.h _PyStackChunk
	// (chunks are only freed at thread destruction or when explicitly
	// shrunk; an idle thread keeps its current chunk).
	if s.current.top == 0 && s.current.prev != nil {
		s.current = s.current.prev
	}
}

// Detach removes the top frame from the chunk arena and returns it.
// Caller takes ownership. Used by Generator on first YIELD_VALUE so
// the suspended state survives past the eval loop's natural unwind.
//
// CPython: Python/ceval.c YIELD_VALUE -> _PyFrame_MakeAndSetFrameObject
func (s *FrameStack) Detach() *Frame {
	if s.current == nil || s.current.top == 0 {
		return nil
	}
	idx := s.current.top - 1
	src := s.current.frames[idx]
	s.current.frames[idx] = Frame{}
	s.current.top--
	s.depth--
	if s.current.top == 0 {
		s.current = s.current.prev
	}
	out := &Frame{}
	*out = src
	out.Owner = OwnedByGenerator
	return out
}

// Depth returns the number of live frames.
func (s *FrameStack) Depth() int { return s.depth }

// Top returns the most recently pushed frame, or nil if the stack is
// empty. Mirrors PyThreadState_GetFrame: the thread's view of its own
// activation chain. Mutating the returned frame mutates the live
// activation record, so callers should treat it as read-only.
//
// CPython: Python/pystate.c:2099 PyThreadState_GetFrame
func (s *FrameStack) Top() *Frame {
	if s.current == nil || s.current.top == 0 {
		return nil
	}
	return &s.current.frames[s.current.top-1]
}
