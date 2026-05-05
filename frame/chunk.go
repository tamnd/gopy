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
}

// New returns an empty frame stack.
func New() *FrameStack {
	return &FrameStack{}
}

// Push allocates a frame, initializes it for co, and links it as
// the new top of the call chain. Caller's previous top frame is
// passed as prev so the frame chain stays intact.
//
// CPython: Python/frame.c _PyThreadState_PushFrame
func (s *FrameStack) Push(co *objects.Code, globals, builtins, fn objects.Object, prev *Frame) *Frame {
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
	if s.current == nil {
		return
	}
	s.current.top--
	s.depth--
	// Clear the slot so the next Push doesn't see stale refs.
	s.current.frames[s.current.top] = Frame{}
	if s.current.top == 0 {
		s.current = s.current.prev
	}
}

// Depth returns the number of live frames.
func (s *FrameStack) Depth() int { return s.depth }
