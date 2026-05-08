// Package frame is the interpreter-side activation record. One
// Frame per Python call. Frames live on a per-thread chunk arena
// (see Chunk) so push/pop is amortized O(1) and matches CPython's
// _PyThreadState_PushFrame / PopFrame call shape.
//
// The user-visible `frame` Python object (sys._getframe()) is a
// thin wrapper over Frame and lives in the objects package
// (spec 1687).
//
// CPython: Include/internal/pycore_frame.h _PyInterpreterFrame
// CPython: Python/frame.c

package frame

import (
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/stackref"
)

// OwnerKind tracks which subsystem holds the canonical pointer to
// a frame. Mirrors _PyFrameOwner from pycore_frame.h.
//
// CPython: Include/internal/pycore_frame.h _PyFrameOwner
type OwnerKind uint8

const (
	OwnedByEval      OwnerKind = iota // top-of-stack, owned by the eval loop
	OwnedByGenerator                  // copied to a generator object on suspend
	OwnedByThread                     // root frame for a thread
	OwnedByFrameObj                   // a Python frame object holds it
	OwnedByCstack                     // C-stack-allocated, do not free
)

// Frame is the activation record. Field order roughly mirrors
// _PyInterpreterFrame so the layout reads the same as CPython's;
// alignment is left to Go's compiler.
//
// CPython: Include/internal/pycore_frame.h _PyInterpreterFrame
type Frame struct {
	// Code is the running bytecode. nil means a placeholder /
	// torn-down frame.
	Code *objects.Code

	// Globals, Builtins, Locals are the scope dicts. Locals is
	// nil for fast-locals frames (the common case).
	Globals  objects.Object
	Builtins objects.Object
	Locals   objects.Object

	// Func is the function object that produced this call. Held
	// as objects.Object until 1685 lands the typed Function.
	Func objects.Object

	// Previous is the caller frame. nil for thread-root frames.
	Previous *Frame

	// InstrPtr is the index into Code.Code of the next instruction.
	// PrevInstr is the index of the most recently executed
	// instruction (for RESUME after yield, and for traceback).
	InstrPtr  int
	PrevInstr int

	// StackTop is the index into LocalsPlus where the live value
	// stack ends. The stack starts at NLocalsPlus.
	StackTop int

	// LocalsPlus packs fast locals, cell vars, free vars, and the
	// value stack into one slice. Layout:
	//
	//   [ 0          .. nlocals                              )  fast locals
	//   [ nlocals    .. nlocals + ncells                     )  cells
	//   [ ...        .. nlocals + ncells + nfree (= nlocals+) )  frees
	//   [ nlocalsplus .. nlocalsplus + stacktop              )  stack
	LocalsPlus []stackref.Ref

	// Owner discriminates teardown / suspend behavior.
	Owner OwnerKind

	// ReturnOffset is set on a callee frame when the caller wants
	// the eval loop to resume at a non-default offset on return
	// (inline calls reuse this; v0.6 always uses 0).
	ReturnOffset int

	// YieldOffset is set when a generator suspends at YIELD_VALUE
	// so RESUME picks up at the right instruction.
	YieldOffset int

	// TraceLines mirrors PyFrameObject.f_trace_lines. The legacy
	// sys.settrace machinery suppresses the LINE callback when this
	// flag is false; user code sets it via frame.f_trace_lines.
	// Defaults to true to match CPython.
	//
	// CPython: Include/cpython/frameobject.h PyFrameObject.f_trace_lines
	TraceLines bool
	// TraceOpcodes mirrors PyFrameObject.f_trace_opcodes. The legacy
	// trace bridge installs INSTRUMENTED_INSTRUCTION on the running
	// frame's code only when this is true.
	//
	// CPython: Include/cpython/frameobject.h PyFrameObject.f_trace_opcodes
	TraceOpcodes bool
	// Lineno is the line number the legacy trace bridge passes to
	// the user's tracefunc. The bridge stamps it before each call
	// and clears it back to zero on the way out, mirroring
	// CPython's frame->f_lineno protocol.
	//
	// CPython: Include/cpython/frameobject.h PyFrameObject.f_lineno
	Lineno int
}

// NLocalsOf returns the count of fast-local slots a code object owns.
//
// CPython: Include/internal/pycore_code.h _PyCode_NLOCALS
func NLocalsOf(co *objects.Code) int {
	return len(co.Varnames)
}

// NCellsOf returns the count of cell slots a code object owns.
//
// CPython: Include/internal/pycore_code.h _PyCode_NCELLS
func NCellsOf(co *objects.Code) int {
	return len(co.Cellvars)
}

// NFreeOf returns the count of free-var slots a code object owns.
//
// CPython: Include/internal/pycore_code.h _PyCode_NFREE
func NFreeOf(co *objects.Code) int {
	return len(co.Freevars)
}

// NLocalsPlusOf returns the total locals+cells+frees slot count.
//
// CPython: Include/internal/pycore_code.h _PyCode_NLOCALSPLUS
func NLocalsPlusOf(co *objects.Code) int {
	return NLocalsOf(co) + NCellsOf(co) + NFreeOf(co)
}

// SizeFor computes the LocalsPlus length needed to run code.
// Includes both the locals/cells/frees and the value-stack reservation.
//
// CPython: Include/internal/pycore_frame.h _PyFrame_NumSlotsForCodeObject
func SizeFor(co *objects.Code) int {
	return NLocalsPlusOf(co) + co.Stacksize
}

// CellsStart returns the index in LocalsPlus where cell vars begin.
func CellsStart(co *objects.Code) int { return NLocalsOf(co) }

// FreesStart returns the index where free vars begin.
func FreesStart(co *objects.Code) int { return NLocalsOf(co) + NCellsOf(co) }

// StackStart returns the index where the value stack begins.
func StackStart(co *objects.Code) int { return NLocalsPlusOf(co) }

// Init wires a freshly allocated frame to a code object. Resets the
// instruction pointer and stack top, and sizes LocalsPlus.
//
// CPython: Python/frame.c _PyFrame_Initialize
func (f *Frame) Init(co *objects.Code, globals, builtins objects.Object, fn objects.Object, prev *Frame) {
	f.Code = co
	f.Globals = globals
	f.Builtins = builtins
	f.Locals = nil
	f.Func = fn
	f.Previous = prev
	f.InstrPtr = 0
	f.PrevInstr = -1
	f.StackTop = 0
	f.Owner = OwnedByEval
	f.ReturnOffset = 0
	f.YieldOffset = 0
	f.TraceLines = true
	f.TraceOpcodes = false
	f.Lineno = 0
	size := SizeFor(co)
	if cap(f.LocalsPlus) >= size {
		f.LocalsPlus = f.LocalsPlus[:size]
		for i := range f.LocalsPlus {
			f.LocalsPlus[i] = stackref.Null
		}
	} else {
		f.LocalsPlus = make([]stackref.Ref, size)
	}
}

// PushStack adds r to the top of the value stack.
//
// CPython: Python/ceval.c PUSH macro
func (f *Frame) PushStack(r stackref.Ref) {
	base := NLocalsPlusOf(f.Code)
	f.LocalsPlus[base+f.StackTop] = r
	f.StackTop++
}

// PopStack removes and returns the top stack value.
//
// CPython: Python/ceval.c POP macro
func (f *Frame) PopStack() stackref.Ref {
	f.StackTop--
	base := NLocalsPlusOf(f.Code)
	r := f.LocalsPlus[base+f.StackTop]
	f.LocalsPlus[base+f.StackTop] = stackref.Null
	return r
}

// PeekStack returns the value at depth from the top (0 = top).
//
// CPython: Python/ceval.c PEEK macro
func (f *Frame) PeekStack(depth int) stackref.Ref {
	base := NLocalsPlusOf(f.Code)
	return f.LocalsPlus[base+f.StackTop-1-depth]
}

// Clear closes every live stackref (locals, cells, frees, stack)
// and resets Code/Globals/Builtins/Locals to nil. Used both on normal
// frame teardown (eval loop unwinding the call) and on the path that
// detaches a frame to a Generator object before resume.
//
// CPython: Python/frame.c _PyFrame_ClearExceptCode + _PyFrame_Clear
func (f *Frame) Clear() {
	for i := range f.LocalsPlus {
		f.LocalsPlus[i].Close()
		f.LocalsPlus[i] = stackref.Null
	}
	f.StackTop = 0
	f.Code = nil
	f.Globals = nil
	f.Builtins = nil
	f.Locals = nil
	f.Func = nil
	f.Previous = nil
}

// Suspend prepares the frame for ownership transfer to a Generator
// object on YIELD_VALUE. Marks the owner so a subsequent FrameStack.Pop
// knows not to zero the slot, and records the offset where Resume
// should pick up. The caller is responsible for splicing the frame
// out of the chunk arena and into the generator.
//
// CPython: Python/ceval.c YIELD_VALUE handler
func (f *Frame) Suspend(yieldOffset int) {
	f.Owner = OwnedByGenerator
	f.YieldOffset = yieldOffset
}

// Resume re-marks a generator-owned frame as eval-owned and restores
// the instruction pointer to YieldOffset so the next dispatch tick
// picks up after the suspending YIELD_VALUE.
//
// CPython: Python/ceval.c SEND / SEND_GEN handler
func (f *Frame) Resume() {
	f.Owner = OwnedByEval
	f.InstrPtr = f.YieldOffset
}

// LocalAt returns the fast local at index i.
func (f *Frame) LocalAt(i int) stackref.Ref { return f.LocalsPlus[i] }

// SetLocal stores r at fast-local slot i.
func (f *Frame) SetLocal(i int, r stackref.Ref) { f.LocalsPlus[i] = r }

// The methods below satisfy objects.InterpreterFrame so the
// Python-level frame wrapper in objects/ can read this activation
// record without importing the frame package. The Frame-prefixed
// names avoid colliding with the existing field names (Code,
// Globals, etc.) on this same type.
//
// CPython: Include/cpython/frameobject.h PyFrame_Get* / FastLocal*

// FrameCode returns the running Code object.
func (f *Frame) FrameCode() *objects.Code { return f.Code }

// FrameGlobals returns f_globals.
func (f *Frame) FrameGlobals() objects.Object { return f.Globals }

// FrameBuiltins returns f_builtins.
func (f *Frame) FrameBuiltins() objects.Object { return f.Builtins }

// FrameFunc returns the Function that produced this call, or nil for
// frames that were not created from a function (module init, exec).
//
// CPython: Python/optimizer_analysis.c:156 _PyFrame_GetFunction
func (f *Frame) FrameFunc() objects.Object { return f.Func }

// FrameLocals returns f_locals (nil for fast-locals frames).
func (f *Frame) FrameLocals() objects.Object { return f.Locals }

// FrameBack returns the caller's activation record, or nil at the
// thread root. The explicit nil return avoids handing back a typed
// nil wrapped in a non-nil interface.
func (f *Frame) FrameBack() objects.InterpreterFrame {
	if f.Previous == nil {
		return nil
	}
	return f.Previous
}

// FrameLasti returns the offset of the next instruction.
func (f *Frame) FrameLasti() int { return f.InstrPtr }

// FrameNumLocals returns the count of fast-local slots.
func (f *Frame) FrameNumLocals() int { return NLocalsOf(f.Code) }

// FrameFastLocal returns the fast local at index i, or nil if the
// slot is unbound.
func (f *Frame) FrameFastLocal(i int) objects.Object {
	return f.LocalsPlus[i].AsObject()
}

// FrameNumCells returns the count of cell slots.
func (f *Frame) FrameNumCells() int { return NCellsOf(f.Code) }

// FrameCellLocal returns the cell var at index i, or nil if unbound.
func (f *Frame) FrameCellLocal(i int) objects.Object {
	return f.LocalsPlus[CellsStart(f.Code)+i].AsObject()
}

// FrameNumFrees returns the count of free-var slots.
func (f *Frame) FrameNumFrees() int { return NFreeOf(f.Code) }

// FrameFreeLocal returns the free var at index i, or nil if unbound.
func (f *Frame) FrameFreeLocal(i int) objects.Object {
	return f.LocalsPlus[FreesStart(f.Code)+i].AsObject()
}
