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

	// LinenoJumped is set when the f_lineno setter (the debugger line
	// jump) relocates InstrPtr during a trace callback. The dispatch
	// loop consults it after the INSTRUMENTED_LINE event to resume at
	// the new target instead of running the hidden opcode, then clears
	// it. A bare InstrPtr-before/after comparison would also trip on the
	// EXTENDED_ARG-prefix advance fetchExtended performs, so the jump
	// needs its own explicit signal.
	//
	// CPython: Objects/frameobject.c:1640 frame_lineno_set_impl sets
	// frame->instr_ptr, which Python/bytecodes.c INSTRUMENTED_LINE then
	// detects via instr_ptr != this_instr.
	LinenoJumped bool

	// StackTop is the index into LocalsPlus where the live value
	// stack ends. The stack starts at StackBase.
	StackTop int

	// StackBase caches NLocalsPlusOf(Code) so PushStack / PopStack /
	// PeekStack / DropStack avoid the three slice-header reads
	// (Varnames + Cellvars + Freevars) on every value-stack op.
	// Populated by Init and recomputed by Clear.
	//
	// CPython: Include/internal/pycore_code.h _PyCode_NLOCALSPLUS
	// is precomputed at code creation time; the frame just adds it
	// to f->localsplus to find the stack base.
	StackBase int

	// LocalsPlus packs fast locals, cell vars, free vars, and the
	// value stack into one slice. Layout:
	//
	//   [ 0          .. nlocals                              )  fast locals
	//   [ nlocals    .. nlocals + ncells                     )  cells
	//   [ ...        .. nlocals + ncells + nfree (= nlocals+) )  frees
	//   [ nlocalsplus .. nlocalsplus + stacktop              )  stack
	LocalsPlus []stackref.Ref

	// snapshot is a take_ownership-style mirror of LocalsPlus. Set
	// once by TakeOwnership before a suspended generator's body is
	// closed so that frame.f_locals and gi_frame consumers continue
	// to see fast-local / cell / free data after the body unwinds and
	// FrameClearLocals zeroes LocalsPlus. Each Ref carries an
	// independent strong reference (Dup at take time) so the
	// underlying objects survive the body's clear.
	//
	// CPython: Objects/frameobject.c:1138 take_ownership
	snapshot []stackref.Ref

	// Owner discriminates teardown / suspend behavior.
	Owner OwnerKind

	// GenOwner is the generator / coroutine / async-generator that
	// holds this activation record when Owner == OwnedByGenerator.
	// nil for thread-owned and eval-owned frames. Used by frameClear
	// to mirror CPython's FRAME_OWNED_BY_GENERATOR liveness check: any
	// *Frame wrapper that references this activation record resolves
	// to the same generator state via this back-pointer, including
	// wrappers minted on demand by tb_frame.
	//
	// CPython: Include/internal/pycore_frame.h _PyInterpreterFrame f_executable
	// + Objects/genobject.c:107 _PyGen_GetGeneratorFromFrame (back-derive
	// via PyGenObject layout; gopy stores the pointer explicitly).
	GenOwner objects.Object

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

	// exposed records whether a Python-level frame object for this
	// activation record has actually been handed to user code: returned
	// by gi_frame / cr_frame / ag_frame, by sys._getframe(), or wrapped
	// into a traceback. gopy mints the *Frame wrapper eagerly when a
	// generator frame is created, so the wrapper's existence is not a
	// reliable signal; this flag is the faithful stand-in for CPython's
	// frame->frame_obj != NULL test, which take_ownership keys off when a
	// suspended generator is finalized while a frame object outlives it.
	//
	// CPython: Objects/frameobject.c:1138 take_ownership (the
	// frame->frame_obj != NULL guard before the iframe is copied out).
	exposed bool

	// Wrappers is the list of Python-visible frame objects that point
	// at this activation record (sys._getframe, traceback walks, etc).
	// FrameStack.Pop reads this list to rebind each wrapper to a
	// FrameSnapshot before the chunk slot is recycled, so f_code / f_globals
	// / f_locals reads continue to work after the call returns. Held as
	// opaque objects.Object so the *Frame layout does not pull in the
	// objects package's wrapper type for every slot.
	//
	// CPython: Objects/frameobject.c:1109 _PyFrame_New_NoTrack (the
	// PyFrameObject linkage CPython gets for free from refcounting).
	Wrappers []objects.Object
}

// NLocalsOf returns the count of fast-local slots a code object owns.
// Reads the precomputed Nlocals field that mirrors CPython's
// init_code-stored co_nlocals; falls back to len(Varnames) for test
// fixtures that build Code by struct literal without calling
// SyncLocalsplusCounts (the v0.5 frame_test.go mkCode helper, etc).
//
// CPython: Include/cpython/code.h:87 co_nlocals
func NLocalsOf(co *objects.Code) int {
	if co == nil {
		return 0
	}
	if co.Nlocalsplus != 0 {
		return co.Nlocals
	}
	return len(co.Varnames)
}

// NCellsOf returns the count of cell slots a code object owns.
//
// CPython: Include/cpython/code.h:88 co_ncellvars
func NCellsOf(co *objects.Code) int {
	if co == nil {
		return 0
	}
	if co.Nlocalsplus != 0 {
		return co.Ncellvars
	}
	return len(co.Cellvars)
}

// NFreeOf returns the count of free-var slots a code object owns.
//
// CPython: Include/cpython/code.h:89 co_nfreevars
func NFreeOf(co *objects.Code) int {
	if co == nil {
		return 0
	}
	if co.Nlocalsplus != 0 {
		return co.Nfreevars
	}
	return len(co.Freevars)
}

// NLocalsPlusOf returns the compacted localsplus slot count, matching
// the size init_code stamps onto co_nlocalsplus from
// len(co_localsplusnames). NOT len(Varnames)+len(Cellvars)+len(Freevars):
// when a cellvar's name overlaps with a varname (arg cells),
// fix_cell_offsets merges the two slots and the compacted nlocalsplus
// drops below the naive sum. Reading this from the cached field keeps
// the frame layout aligned with the bytecode that fix_cell_offsets
// rewrote against the compacted offsets.
//
// CPython: Include/cpython/code.h:84 co_nlocalsplus
func NLocalsPlusOf(co *objects.Code) int {
	if co.Nlocalsplus != 0 {
		return co.Nlocalsplus
	}
	return len(co.Varnames) + len(co.Cellvars) + len(co.Freevars)
}

// SizeFor computes the LocalsPlus length needed to run code.
// Includes both the locals/cells/frees and the value-stack reservation.
//
// CPython: Include/internal/pycore_frame.h _PyFrame_NumSlotsForCodeObject
func SizeFor(co *objects.Code) int {
	return NLocalsPlusOf(co) + co.Stacksize
}

// CellsStart returns the index in LocalsPlus where post-locals cell
// vars begin. After fix_cell_offsets, arg-cells live in the merged
// local slot (kind = CO_FAST_LOCAL|CO_FAST_CELL); only non-arg cells
// occupy slots [Nlocals, Nlocalsplus - Nfreevars).
func CellsStart(co *objects.Code) int { return NLocalsOf(co) }

// FreesStart returns the index where free vars begin, matching the
// COPY_FREE_VARS handler at Python/bytecodes.c:1925 which computes
// offset = co_nlocalsplus - co_nfreevars. Falls back to the legacy
// nlocals+ncells layout when the precomputed counts are unset (test
// fixtures only).
//
// CPython: Python/bytecodes.c:1932 COPY_FREE_VARS offset
func FreesStart(co *objects.Code) int {
	if co.Nlocalsplus != 0 {
		return co.Nlocalsplus - co.Nfreevars
	}
	return len(co.Varnames) + len(co.Cellvars)
}

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
	// The frame owns a counted reference on its function object for the
	// duration of the call; _PyEvalFramePushAndInit transfers/holds
	// f_funcobj and clear_thread_frame drops it. Without this the CALL
	// that consumed the callable's only stack reference would let the
	// function reach refcount zero (and run func_dealloc, clearing its
	// closure) while its own frame is still executing.
	//
	// CPython: Python/ceval.c:1860 _PyEval_BuildFrame sets f_funcobj
	if fn != nil {
		objects.Incref(fn)
	}
	f.Func = fn
	f.Previous = prev
	f.InstrPtr = 0
	f.PrevInstr = -1
	f.StackTop = 0
	f.StackBase = NLocalsPlusOf(co)
	f.Owner = OwnedByEval
	f.ReturnOffset = 0
	f.YieldOffset = 0
	f.TraceLines = true
	f.TraceOpcodes = false
	f.Lineno = 0
	f.exposed = false
	f.Wrappers = nil
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
	f.LocalsPlus[f.StackBase+f.StackTop] = r
	f.StackTop++
}

// PopStack removes and returns the top stack value.
//
// CPython: Python/ceval.c POP macro
func (f *Frame) PopStack() stackref.Ref {
	f.StackTop--
	i := f.StackBase + f.StackTop
	r := f.LocalsPlus[i]
	f.LocalsPlus[i] = stackref.Null
	return r
}

// PeekStack returns the value at depth from the top (0 = top).
//
// CPython: Python/ceval.c PEEK macro
func (f *Frame) PeekStack(depth int) stackref.Ref {
	return f.LocalsPlus[f.StackBase+f.StackTop-1-depth]
}

// SetPeekStack writes r into the slot at depth from the top, closing
// the slot's prior occupant first so its refcount is released.
// Mirrors the POKE pattern in CPython where the named output binding
// replaces a named input that was just CLOSE-d in the same arm.
//
// CPython: Python/ceval_macros.h POKE macro (stack_pointer[-(depth)+1] = ref).
func (f *Frame) SetPeekStack(depth int, r stackref.Ref) {
	i := f.StackBase + f.StackTop - 1 - depth
	f.LocalsPlus[i].Close()
	f.LocalsPlus[i] = r
}

// PokeStack writes r into the slot at depth from the top without
// touching the prior occupant's refcount. This is the faithful POKE:
// it relocates a live reference rather than replacing a consumed one.
// Passthrough writebacks (SWAP, COPY) use it because the value being
// written is a live input being moved, and the slot's prior occupant
// is itself a live input relocated elsewhere in the same instruction;
// closing it here would double-free.
//
// CPython: Python/ceval_macros.h POKE macro (stack_pointer[-(depth)] = ref).
func (f *Frame) PokeStack(depth int, r stackref.Ref) {
	f.LocalsPlus[f.StackBase+f.StackTop-1-depth] = r
}

// DropStack removes the top n stack entries, closing each slot's
// stackref before nulling it. Closing matches CPython's pattern of
// DECREF_INPUTS followed by STACK_SHRINK: the stack pointer adjusts
// only after the owned references are released. Slots that already
// hold Null (because the producer used PopStack to hand off
// ownership) just no-op through Close.
//
// CPython: Python/ceval_macros.h STACK_SHRINK.
func (f *Frame) DropStack(n int) {
	base := f.StackBase
	for range n {
		f.StackTop--
		i := base + f.StackTop
		f.LocalsPlus[i].Close()
		f.LocalsPlus[i] = stackref.Null
	}
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
	f.StackBase = 0
	f.Code = nil
	f.Globals = nil
	f.Builtins = nil
	f.Locals = nil
	// Release the frame's reference on f_funcobj acquired in Init.
	//
	// CPython: Python/frame.c clear_thread_frame Py_DECREF(frame->f_funcobj)
	if f.Func != nil {
		objects.Decref(f.Func)
	}
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

// FrameSetBack rewires f_back. A nil argument (or an interface holding a
// nil *Frame) unlinks the frame, matching _gen_throw's
// frame->previous = NULL restore after a forwarded throw returns.
//
// CPython: Objects/genobject.c:496 _gen_throw (frame->previous)
func (f *Frame) FrameSetBack(back objects.InterpreterFrame) {
	if back == nil {
		f.Previous = nil
		return
	}
	if p, ok := back.(*Frame); ok {
		f.Previous = p
		return
	}
	f.Previous = nil
}

// FrameLasti returns the offset of the next instruction.
func (f *Frame) FrameLasti() int { return f.InstrPtr }

// FrameGenOwner returns the generator / coroutine / async-generator
// that owns this activation record, or nil for thread-owned frames.
// objects.frameClear uses this to mirror CPython's
// FRAME_OWNED_BY_GENERATOR branch in frame_clear_impl.
//
// CPython: Objects/genobject.c:107 _PyGen_GetGeneratorFromFrame
func (f *Frame) FrameGenOwner() objects.Object { return f.GenOwner }

// FrameNumLocals returns the count of fast-local slots.
func (f *Frame) FrameNumLocals() int { return NLocalsOf(f.Code) }

// FrameFastLocal returns the fast local at index i, or nil if the
// slot is unbound. Reads the take_ownership snapshot when set so
// reads survive a generator's body-close.
func (f *Frame) FrameFastLocal(i int) objects.Object {
	if f.snapshot != nil && i < len(f.snapshot) {
		return f.snapshot[i].AsObject()
	}
	return f.LocalsPlus[i].AsObject()
}

// FrameNumCells returns the count of cell slots.
func (f *Frame) FrameNumCells() int { return NCellsOf(f.Code) }

// FrameCellLocal returns the cell var at index i, or nil if unbound.
// Reads the take_ownership snapshot when set.
func (f *Frame) FrameCellLocal(i int) objects.Object {
	idx := CellsStart(f.Code) + i
	if f.snapshot != nil && idx < len(f.snapshot) {
		return f.snapshot[idx].AsObject()
	}
	return f.LocalsPlus[idx].AsObject()
}

// FrameNumFrees returns the count of free-var slots.
func (f *Frame) FrameNumFrees() int { return NFreeOf(f.Code) }

// FrameFreeLocal returns the free var at index i, or nil if unbound.
// Reads the take_ownership snapshot when set.
func (f *Frame) FrameFreeLocal(i int) objects.Object {
	idx := FreesStart(f.Code) + i
	if f.snapshot != nil && idx < len(f.snapshot) {
		return f.snapshot[idx].AsObject()
	}
	return f.LocalsPlus[idx].AsObject()
}

// FrameClearLocals releases every live stackref in LocalsPlus (fast
// locals, cells, frees, and the value stack) without touching Code /
// Globals / Builtins / Locals / Func / Previous. Matches CPython's
// _PyFrame_ClearExceptCode: a generator that finished or was closed
// still exposes gi_code, so the code pointer must survive even though
// the per-call data is gone.
//
// CPython: Python/frame.c:108 _PyFrame_ClearExceptCode
func (f *Frame) FrameClearLocals() {
	// Capture the **kwargs parameter dict before its slot is closed. It is
	// freshly built by argument binding and owned exclusively by this
	// frame, so when Close drops it to refcount zero its stored values
	// must be released synchronously: gopy wires no global dict tp_dealloc
	// (a namespace dict reachable only through an un-counted Go field
	// routinely sits at refcount zero while live, so a blanket dealloc
	// would corrupt it), and with the cycle collector disabled nothing
	// else reclaims the captured values. Targeting only the varkeywords
	// slot keeps a local that merely aliases a namespace dict
	// (g = globals()) untouched.
	//
	// CPython: Python/frame.c:108 _PyFrame_ClearExceptCode (per-local
	// Py_DECREF, where the kwargs dict's own tp_dealloc clears it)
	kwDict := f.VarkeywordsDict()
	for i := range f.LocalsPlus {
		f.LocalsPlus[i].Close()
		f.LocalsPlus[i] = stackref.Null
	}
	if kwDict != nil {
		objects.ReleaseDeadDictContents(kwDict)
	}
	f.StackTop = 0
}

// VarkeywordsDict returns the borrowed **kwargs parameter dict bound in
// LocalsPlus, or nil when the code has no CO_VARKEYWORDS parameter or the
// slot does not currently hold a plain dict. The slot index mirrors the
// layout argument binding builds in callPyFunction: positional and
// keyword-only args, then the optional *args tuple, then **kwargs.
// frame.clear() reads this before clearing so it can release the dict's
// captured values once both the frame slot and the take_ownership
// snapshot have dropped their references.
//
// CPython: Objects/codeobject.c co_argcount / CO_VARKEYWORDS layout
func (f *Frame) VarkeywordsDict() *objects.Dict {
	co := f.Code
	if co == nil || co.Flags&0x08 == 0 {
		return nil
	}
	slot := co.Argcount + co.KwonlyArgcount
	if co.Flags&0x04 != 0 {
		slot++
	}
	if slot < 0 || slot >= len(f.LocalsPlus) {
		return nil
	}
	d, _ := f.LocalsPlus[slot].AsObject().(*objects.Dict)
	return d
}

// FrameDropSnapshot releases the take_ownership snapshot. It is the
// explicit counterpart to FrameClearLocals: the natural generator
// unwind keeps the snapshot so an externally-held gi_frame can still
// read the locals after the body is gone (test_frame_outlives_generator),
// but frame.clear() asks for the locals to truly disappear, so the
// duplicated strong references the snapshot holds must drop too.
// Without this, an object bound only to a generator argument lives
// forever once gi_frame.clear() has snapshotted it (gh-142766).
//
// CPython: Python/frame.c:108 _PyFrame_ClearExceptCode (the single
// owned copy is the one cleared; gopy keeps the snapshot separate).
func (f *Frame) FrameDropSnapshot() {
	for i := range f.snapshot {
		f.snapshot[i].Close()
		f.snapshot[i] = stackref.Null
	}
	f.snapshot = nil
}

// FrameLocalsPlusItem returns LocalsPlus[i] at the absolute slot
// (post-fix_cell_offsets). Used by kinds-driven walks like
// FrameFastToLocals where the slot semantics live in
// LocalsplusKinds rather than the legacy varnames/cellvars/freevars
// split. Reads the take_ownership snapshot when set.
//
// CPython: Objects/frameobject.c:2199 frame_get_var (the
// frame->localsplus[i] read).
func (f *Frame) FrameLocalsPlusItem(i int) objects.Object {
	if f.snapshot != nil {
		if i < 0 || i >= len(f.snapshot) {
			return nil
		}
		return f.snapshot[i].AsObject()
	}
	if i < 0 || i >= len(f.LocalsPlus) {
		return nil
	}
	return f.LocalsPlus[i].AsObject()
}

// FrameSetLocalsPlusItem stores v in LocalsPlus[i] via
// stackref.FromObject. Used by the FrameLocalsProxy write-through
// path (f_locals[name] = v).
//
// CPython: Objects/frameobject.c:246 framelocalsproxy_setitem (the
// fast[i] = PyStackRef_FromPyObjectNew(value) store)
func (f *Frame) FrameSetLocalsPlusItem(i int, v objects.Object) {
	if i < 0 || i >= len(f.LocalsPlus) {
		return
	}
	f.LocalsPlus[i].Close()
	f.LocalsPlus[i] = stackref.FromObject(v)
}

// FrameNumStack returns the count of live operand-stack entries.
// frame_traverse walks LocalsPlus[StackBase:StackBase+StackTop] so the
// cycle collector sees values held on the running stack (e.g., a
// generator object passed as an argument to list()).
//
// CPython: Objects/frameobject.c:1163 frame_traverse (PyTrace_VISITOR
// over PyFrame.localsplus[0:StackTop])
func (f *Frame) FrameNumStack() int { return f.StackTop }

// FrameStackItem returns the operand-stack entry at depth i from the
// stack base (0 == bottom of the live region).
//
// CPython: Objects/frameobject.c:1163 frame_traverse
func (f *Frame) FrameStackItem(i int) objects.Object {
	if i < 0 || i >= f.StackTop {
		return nil
	}
	return f.LocalsPlus[f.StackBase+i].AsObject()
}

// FrameRegisterWrapper records a Python-level wrapper for this
// activation record. FrameStack.Pop walks the list and calls SwapInterp
// on each wrapper with a FrameSnapshot so reads through the wrapper
// survive the chunk slot's recycle.
//
// CPython: Objects/frameobject.c:1109 _PyFrame_New_NoTrack
func (f *Frame) FrameRegisterWrapper(w objects.Object) {
	if w == nil {
		return
	}
	f.Wrappers = append(f.Wrappers, w)
}

// FrameMarkExposed records that a Python-level frame object for this
// activation record has been handed to user code. genFinalize keys
// take_ownership off this so a suspended generator only pays the
// snapshot cost when a frame object genuinely outlives it.
//
// CPython: Objects/frameobject.c:1138 take_ownership (frame->frame_obj
// is set when _PyFrame_GetFrameObject materializes the PyFrameObject).
func (f *Frame) FrameMarkExposed() { f.exposed = true }

// FrameExposed reports whether FrameMarkExposed has been called for
// this activation record.
//
// CPython: Objects/frameobject.c:1138 take_ownership (frame->frame_obj != NULL)
func (f *Frame) FrameExposed() bool { return f.exposed }

// FrameWrapper returns the previously-registered Python-level wrapper
// for this activation record, or nil if no wrapper has been minted yet.
// objects.NewFrame reads this to avoid creating a duplicate PyFrameObject
// equivalent so f_locals readers share the same extraLocals dict.
//
// CPython: Objects/frameobject.c:1109 _PyFrame_New_NoTrack (the
// PyFrameObject pointer cached on the activation record).
func (f *Frame) FrameWrapper() objects.Object {
	if len(f.Wrappers) == 0 {
		return nil
	}
	return f.Wrappers[0]
}

// FrameTakeOwnership snapshots the activation record's LocalsPlus into
// the frame's own backing store so a subsequent FrameClearLocals (the
// body's natural unwind on GeneratorExit) does not break reads through
// the Python-level frame object. Each snapshot ref is a Dup of the
// corresponding LocalsPlus ref, so the snapshot owns an independent
// strong reference. Idempotent: a second call after the snapshot is
// installed is a no-op.
//
// CPython: Objects/frameobject.c:1138 take_ownership
func (f *Frame) FrameTakeOwnership() {
	if f.snapshot != nil {
		return
	}
	if len(f.LocalsPlus) == 0 {
		return
	}
	snap := make([]stackref.Ref, len(f.LocalsPlus))
	for i := range f.LocalsPlus {
		snap[i] = f.LocalsPlus[i].Dup()
	}
	f.snapshot = snap
}
