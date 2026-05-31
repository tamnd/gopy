// The Python-level frame object: f_code / f_back / f_globals /
// f_locals / f_lineno / f_lasti / f_trace. CPython splits the
// activation record (_PyInterpreterFrame, owned by the eval loop)
// from the user-visible PyFrameObject (a thin wrapper). gopy keeps
// the same split: frame.Frame holds the live machine state and lives
// in the frame package; this file holds Frame, the Python-level
// wrapper, and exposes its f_* attribute panel.
//
// objects/ cannot import frame/ (frame already imports objects), so
// access to the activation record goes through InterpreterFrame, an
// interface defined here that the frame package's *frame.Frame
// satisfies via methods.
//
// CPython: Objects/frameobject.c PyFrame_Type and friends

package objects

import "fmt"

// InterpreterFrame is the read-only access objects.Frame needs from
// the interpreter-side activation record. Defining the contract here
// keeps the dependency arrow pointing the right way: frame depends on
// objects, and the live frame implements an interface objects owns.
//
// Method names mirror CPython's PyFrame_Get* / PyFrame_FastLocal*
// helpers (with the Frame prefix kept to avoid collisions with the
// activation-record's own field names).
//
// CPython: Include/cpython/frameobject.h PyFrame_GetCode / GetBack /
// GetGlobals / GetBuiltins / GetLasti
type InterpreterFrame interface {
	FrameCode() *Code
	FrameGlobals() Object
	FrameBuiltins() Object
	FrameLocals() Object
	FrameBack() InterpreterFrame
	// FrameSetBack rewires f_back to back (nil to unlink). A forwarded
	// gen/coro throw uses this to relink the delegating frames into the
	// running call chain while the leaf executes, then unlinks them,
	// matching _gen_throw's frame->previous = prev / = NULL bracket.
	//
	// CPython: Objects/genobject.c:496 _gen_throw (frame->previous)
	FrameSetBack(back InterpreterFrame)
	FrameLasti() int
	FrameNumLocals() int
	FrameFastLocal(i int) Object
	FrameNumCells() int
	FrameCellLocal(i int) Object
	FrameNumFrees() int
	FrameFreeLocal(i int) Object
	// FrameNumStack returns the count of live operand-stack entries.
	// frame_traverse visits these so the cycle collector marks values
	// held on the running stack as reachable. Without this walk a
	// generator passed as an argument to list() (still on the operand
	// stack, not yet bound to a local) gets misclassified as
	// unreachable.
	//
	// CPython: Objects/frameobject.c:1163 frame_traverse
	FrameNumStack() int
	FrameStackItem(i int) Object
	// FrameLocalsPlusItem returns LocalsPlus[i] for the absolute
	// localsplus index i. Callers walking LocalsplusKinds need this
	// to fetch the post-fix_cell_offsets value at a kind-tagged slot
	// regardless of whether the slot is fast/cell/free.
	//
	// CPython: Objects/frameobject.c:2199 frame_get_var (the
	// frame->localsplus[i] read).
	FrameLocalsPlusItem(i int) Object
	// FrameSetLocalsPlusItem writes v into LocalsPlus[i] using
	// stackref.FromObject. Callers writing through FrameLocalsProxy
	// need this to push values back to the fast slots when user code
	// does f_locals[name] = v.
	//
	// CPython: Objects/frameobject.c:246 framelocalsproxy_setitem (the
	// fast[i] = PyStackRef_FromPyObjectNew(value) store)
	FrameSetLocalsPlusItem(i int, v Object)
	// FrameFunc returns the Function that produced the call, or nil
	// when the frame was not created from a function (e.g. module
	// init, exec). The Tier-2 globals folder needs the function for
	// _PyFunction_GetVersionForCurrentState; everything else can use
	// FrameGlobals/FrameBuiltins.
	//
	// CPython: Python/optimizer_analysis.c:156 _PyFrame_GetFunction
	FrameFunc() Object
	// FrameClearLocals releases every live stackref in LocalsPlus
	// (fast locals, cells, frees, and the value stack), leaving the
	// code object and scope dicts intact so gi_code / f_code stay
	// readable on a closed generator. Used by Generator.Close /
	// gi_frame.clear() to break the references the body holds onto
	// without losing the metadata callers still query.
	//
	// CPython: Python/frame.c:108 _PyFrame_ClearExceptCode
	FrameClearLocals()
	// FrameTakeOwnership snapshots LocalsPlus into an internal
	// backing store on the activation record so reads through
	// FrameLocalsProxy and the f_locals view survive a subsequent
	// FrameClearLocals. Idempotent. Used by genFinalize so a
	// generator that is being dealloc'd while gi_frame or
	// sys._getframe-returned wrappers are still externally
	// referenced keeps fast-local data alive after the body unwinds.
	//
	// CPython: Objects/frameobject.c:1138 take_ownership
	FrameTakeOwnership()
	// FrameGenOwner returns the generator / coroutine / async-generator
	// that owns this activation record, or nil. The owner is stored on
	// the live frame so every *Frame wrapper that points at it
	// (gi_frame plus any tb_frame minted on demand) resolves to the
	// same suspended/executing state.
	//
	// CPython: Objects/genobject.c:107 _PyGen_GetGeneratorFromFrame
	FrameGenOwner() Object
	// FrameRegisterWrapper records a Python-level frame wrapper for the
	// activation record. The chunk arena uses this list to rebind every
	// wrapper to a snapshot before its storage is recycled on Pop, so
	// reads through sys._getframe / inspect / tb_frame survive the
	// natural return of the call.
	//
	// CPython: Objects/frameobject.c:1109 _PyFrame_New_NoTrack (the
	// PyFrameObject linkage that gopy emulates via wrapper registration)
	FrameRegisterWrapper(w Object)
}

// Frame is the Python-level frame object. It wraps an interpreter
// frame and adds per-frame trace settings (f_trace, f_trace_lines,
// f_trace_opcodes) that don't belong in the activation record.
//
// CPython: Include/cpython/frameobject.h PyFrameObject
type Frame struct {
	Header
	interp       InterpreterFrame
	trace        Object
	traceLines   bool
	traceOpcodes bool
	// extraLocals is the dict that f_locals stores keys that are not in
	// LocalsplusNames. PEP 667 spills writes to unknown names here so
	// repeated reads see the same dict.
	//
	// CPython: Objects/frameobject.c:419 framelocalsproxy_new (the
	// f_extra_locals slot on PyFrameObject)
	extraLocals *Dict
	// owner is the generator / coroutine / async-generator that owns this
	// frame, or nil when the frame is owned by the thread. Set by the vm
	// in execReturnGenerator. frameClear consults it to mirror CPython's
	// FRAME_OWNED_BY_GENERATOR liveness checks.
	//
	// CPython: Include/internal/pycore_frame.h _PyFrame_owner +
	// Objects/frameobject.c:1997 frame_clear_impl FRAME_OWNED_BY_GENERATOR
	owner Object
}

// SetOwner stamps the owning generator / coroutine / async-generator on
// the frame. The vm calls this in execReturnGenerator right after
// allocating the wrapper so frameClear and frame_is_suspended can
// resolve the owner without walking the FrameStack.
//
// CPython: Python/frame.c:81 _PyFrame_InitializeSpecials sets the
// owner field on _PyInterpreterFrame; gopy stores the equivalent
// back-pointer on the Python-visible Frame.
func (f *Frame) SetOwner(o Object) {
	if f == nil {
		return
	}
	f.owner = o
}

// Owner returns the generator / coroutine / async-generator that owns
// the frame, or nil for thread-owned frames.
//
// CPython: Include/internal/pycore_frame.h FRAME_OWNED_BY_GENERATOR
func (f *Frame) Owner() Object {
	if f == nil {
		return nil
	}
	return f.owner
}

// frameType is the type singleton for `frame`.
//
// CPython: Objects/frameobject.c:1238 PyFrame_Type
var frameType = NewType("frame", []*Type{objectType})

func init() {
	frameType.Repr = frameRepr
	frameType.TpTraverse = frameTraverse
	frameType.Getattro = frameGetAttr
	frameType.Setattro = frameSetAttr
	SetTypeDescr(frameType, "clear", NewMethodDescr(frameType, "clear", frameClear))
}

// frameClear releases the frame's locals/cells/frees/stack. Used by
// traceback.clear_frames(), Generator.close()'s post-hoc cleanup, and
// user code that wants to break reference cycles. Mirrors CPython's
// FRAME_OWNED_BY_GENERATOR / FRAME_OWNED_BY_THREAD branching: a frame
// that belongs to a still-running or suspended generator raises
// RuntimeError rather than tearing locals out from under it.
//
// CPython: Objects/frameobject.c:1994 frame_clear_impl
//
//nolint:gocognit,gocyclo // mirrors frame_clear_impl: generator/coroutine/async-gen owner branching plus thread-stack guard
func frameClear(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: clear() missing self argument")
	}
	f, ok := args[0].(*Frame)
	if !ok {
		return nil, fmt.Errorf(
			"TypeError: descriptor 'clear' requires a 'frame' object")
	}
	var gowner Object
	if f.interp != nil {
		gowner = f.interp.FrameGenOwner()
	}
	if gowner == nil {
		// Some wrappers (e.g. the gi_frame allocated by execReturnGenerator
		// before SetOwner ran) carry the back-pointer only on the
		// wrapper itself. Fall back to that.
		gowner = f.owner
	}
	if gowner != nil {
		// FRAME_OWNED_BY_GENERATOR. Running == 1 mirrors FRAME_EXECUTING;
		// started && !closed && !Running mirrors FRAME_STATE_SUSPENDED.
		// Anything else (FRAME_CREATED or FRAME_COMPLETED) falls into
		// _PyGen_Finalize, which warns about a never-awaited coroutine
		// and runs close() on suspended-but-not-resumed generators.
		//
		// CPython: Objects/frameobject.c:1997 frame_clear_impl
		// (FRAME_OWNED_BY_GENERATOR branch)
		switch g := gowner.(type) {
		case *Generator:
			if g.Running.Load() == 1 {
				return nil, fmt.Errorf("RuntimeError: cannot clear an executing frame")
			}
			if g.started && !g.closed {
				return nil, fmt.Errorf("RuntimeError: cannot clear a suspended frame")
			}
		case *Coroutine:
			if g.Running.Load() == 1 {
				return nil, fmt.Errorf("RuntimeError: cannot clear an executing frame")
			}
			if g.started && !g.closed {
				return nil, fmt.Errorf("RuntimeError: cannot clear a suspended frame")
			}
		case *AsyncGenerator:
			if g.Running.Load() == 1 {
				return nil, fmt.Errorf("RuntimeError: cannot clear an executing frame")
			}
			if g.started && !g.closed {
				return nil, fmt.Errorf("RuntimeError: cannot clear a suspended frame")
			}
		}
		// Drop into _PyGen_Finalize: warns about a never-awaited
		// coroutine, runs close() on a still-suspended async generator,
		// and is a no-op for a generator that already ran to completion.
		//
		// CPython: Objects/frameobject.c:2005 frame_clear_impl
		// (calls _PyGen_Finalize before tearing locals down)
		if t := gowner.Type(); t != nil && t.Finalize != nil {
			t.Finalize(gowner)
		}
	} else if frameIsOnCurrentStack(f) {
		// FRAME_OWNED_BY_THREAD: the frame is currently live on this
		// thread's call stack, so clearing would yank state out from
		// under the eval loop. CPython raises here.
		//
		// CPython: Objects/frameobject.c:2007 frame_clear_impl
		// (FRAME_OWNED_BY_THREAD goto running)
		return nil, fmt.Errorf("RuntimeError: cannot clear an executing frame")
	}
	if f.interp != nil {
		f.interp.FrameClearLocals()
	}
	return None(), nil
}

// CurrentStackProbe is the hook the vm installs so objects/ can ask
// whether a given *Frame's activation record is still on the running
// thread's FrameStack. Returns true when the frame is live (the eval
// loop is mid-execution on it or one of its callees). Nil when the
// runtime has not booted; in that case the frame cannot be live.
//
// CPython does this with a pointer chase from the thread state's
// current frame down through f_back; gopy keeps the FrameStack inside
// the vm package, so we route the probe through a function pointer.
//
// CPython: Include/internal/pycore_pystate.h tstate->current_frame
var CurrentStackProbe func(*Frame) bool

func frameIsOnCurrentStack(f *Frame) bool {
	if CurrentStackProbe == nil {
		return false
	}
	return CurrentStackProbe(f)
}

// frameGetAttr exposes the standard frame attributes f_locals, f_globals,
// f_code, f_back, f_lineno, f_lasti, f_trace.
//
// CPython: Objects/frameobject.c:790 frame_getattro / per-attribute getsets
//
//nolint:gocognit,gocyclo // mirrors frame_getattro: one branch per f_* getset attribute
func frameGetAttr(o Object, name Object) (Object, error) {
	f, ok := o.(*Frame)
	if !ok {
		return GenericGetAttr(o, name)
	}
	n, ok2 := name.(*Unicode)
	if !ok2 {
		return nil, fmt.Errorf("TypeError: attribute name must be string, not '%s'", name.Type().Name)
	}
	switch n.v {
	case "f_locals":
		// Class bodies, module bodies, and exec frames carry an explicit
		// locals dict; CPython returns that dict directly so writes via
		// f_locals[name] = v flow to the namespace without going through
		// FrameLocalsProxy's fast-locals slot machinery. Fast-local
		// frames have Locals == nil; for those, wrap in a proxy.
		//
		// CPython: Objects/frameobject.c:1297 PyFrame_GetLocals
		if f.interp != nil {
			if loc := f.interp.FrameLocals(); loc != nil {
				return loc, nil
			}
		}
		return NewFrameLocalsProxy(f), nil
	case "f_globals":
		if g := f.Globals(); g != nil {
			return g, nil
		}
		return NewDict(), nil
	case "f_code":
		if f.interp != nil {
			if c := f.interp.FrameCode(); c != nil {
				return c, nil
			}
		}
		return None(), nil
	case "f_back":
		b := f.Back()
		if b == nil {
			return None(), nil
		}
		return b, nil
	case "f_lineno":
		return NewInt(int64(f.Lineno())), nil
	case "f_lasti":
		return NewInt(int64(f.Lasti())), nil
	case "f_trace":
		if f.trace != nil {
			return f.trace, nil
		}
		return None(), nil
	case "f_trace_lines":
		if f.traceLines {
			return True(), nil
		}
		return False(), nil
	case "f_trace_opcodes":
		if f.traceOpcodes {
			return True(), nil
		}
		return False(), nil
	case "f_builtins":
		if b := f.Builtins(); b != nil {
			return b, nil
		}
		return NewDict(), nil
	case "f_generator":
		// CPython: Objects/frameobject.c:1887 frame_generator_get_impl
		var owner Object
		if f.interp != nil {
			owner = f.interp.FrameGenOwner()
		}
		if owner == nil {
			owner = f.owner
		}
		if owner == nil {
			return None(), nil
		}
		return owner, nil
	}
	return GenericGetAttr(o, name)
}

// CPython: Objects/frameobject.c:1898 frame_getsetlist
func frameSetAttr(o Object, name Object, v Object) error {
	f, ok := o.(*Frame)
	if !ok {
		return fmt.Errorf("TypeError: descriptor requires a 'frame' object")
	}
	n, ok2 := name.(*Unicode)
	if !ok2 {
		return fmt.Errorf("TypeError: attribute name must be string, not '%s'", name.Type().Name)
	}
	switch n.v {
	case "f_trace":
		f.SetTrace(v)
		return nil
	case "f_trace_lines":
		f.SetTraceLines(v == True())
		return nil
	case "f_trace_opcodes":
		f.SetTraceOpcodes(v == True())
		return nil
	}
	return fmt.Errorf("AttributeError: 'frame' object attribute %q is read-only", n.v)
}

// frameTraverse walks every Object reachable from the frame: f_trace
// plus the activation record's globals, builtins, locals, fast/cell/
// free locals, the value stack, and every back frame's same panel.
// CPython visits f_back as a single edge (Py_VISIT(f->f_back)) and
// relies on each PyFrameObject being independently tracked so the gc
// reaches the chain. gopy's Frame.Back() allocates a fresh wrapper on
// every call, so the back wrappers are not tracked. Walking the
// activation-record chain inline reproduces what CPython gets from
// per-frame tracking.
//
// CPython: Objects/frameobject.c:1949 frame_traverse
func frameTraverse(o Object, visit Visitor) error {
	f := o.(*Frame)
	if f.trace != nil {
		if err := visit(f.trace); err != nil {
			return err
		}
	}
	if f.interp == nil {
		return nil
	}
	if err := visitInterp(f.interp, visit); err != nil {
		return err
	}
	for back := f.interp.FrameBack(); back != nil; back = back.FrameBack() {
		if err := visitInterp(back, visit); err != nil {
			return err
		}
	}
	return nil
}

// visitInterp walks the references on a single activation record. It
// is split out from frameTraverse so the back-chain loop can reuse it
// without recursing through the wrapper-allocation path.
//
// Mirrors CPython's _PyFrame_Traverse: visits f_locals, f_funcobj,
// f_executable, then the localsplus span (fast / cells / frees /
// stack). f_globals and f_builtins are intentionally NOT visited here.
// CPython reaches them transitively via f_funcobj (the function's
// __globals__ traverse), and visiting them directly here over-counts
// the same shared dict on every frame in a recursive chain, pushing
// subtract_refs underwater (Conjoin / Queens / queens(8) regress to
// half the expected solution count).
//
// CPython: Python/frame.c:11 _PyFrame_Traverse
func visitInterp(ip InterpreterFrame, visit Visitor) error {
	if l := ip.FrameLocals(); l != nil {
		if err := visit(l); err != nil {
			return err
		}
	}
	// Visit f_funcobj. CPython's _PyFrame_Traverse visits the function
	// via _Py_VISIT_STACKREF so the function's tp_traverse can pull in
	// __closure__, __defaults__, __globals__, __builtins__. Without
	// this, a closure cell shared across a yield-from chain
	// (Conjoin/Queens) misses its refcount contribution from the iframe
	// and the cycle GC misclassifies a still-suspended chain member as
	// unreachable.
	//
	// CPython: Python/frame.c:15 _PyFrame_Traverse (f_funcobj)
	if fn := ip.FrameFunc(); fn != nil {
		if err := visit(fn); err != nil {
			return err
		}
	}
	for i, n := 0, ip.FrameNumLocals(); i < n; i++ {
		v := ip.FrameFastLocal(i)
		if v == nil {
			continue
		}
		if err := visit(v); err != nil {
			return err
		}
	}
	for i, n := 0, ip.FrameNumCells(); i < n; i++ {
		v := ip.FrameCellLocal(i)
		if v == nil {
			continue
		}
		if err := visit(v); err != nil {
			return err
		}
	}
	for i, n := 0, ip.FrameNumFrees(); i < n; i++ {
		v := ip.FrameFreeLocal(i)
		if v == nil {
			continue
		}
		if err := visit(v); err != nil {
			return err
		}
	}
	// Walk live operand-stack entries. Without this, a generator object
	// passed as an argument to list() but not yet bound to a local
	// reads as unreachable mid-call.
	//
	// CPython: Objects/frameobject.c:1163 frame_traverse (visits the
	// localsplus span between locals_start and stack_pointer).
	for i, n := 0, ip.FrameNumStack(); i < n; i++ {
		v := ip.FrameStackItem(i)
		if v == nil {
			continue
		}
		if err := visit(v); err != nil {
			return err
		}
	}
	return nil
}

// NewFrame wraps interp in a Python-level frame object. interp is
// the live activation record; the wrapper holds it as an interface so
// objects/ does not import frame/. CPython's PyFrameObject is unique
// per activation record (lazily attached), so when a wrapper has
// already been registered for this iframe we return it instead of
// allocating a parallel one. This keeps extraLocals shared across
// sys._getframe() / locals() / f_locals readers within the same call.
//
// CPython: Objects/frameobject.c:1109 _PyFrame_New_NoTrack
func NewFrame(interp InterpreterFrame) *Frame {
	if interp != nil {
		if existing, ok := interp.(interface {
			FrameWrapper() Object
		}); ok {
			if w := existing.FrameWrapper(); w != nil {
				if fw, ok2 := w.(*Frame); ok2 {
					return fw
				}
			}
		}
	}
	f := &Frame{
		interp:     interp,
		traceLines: true,
	}
	f.init(frameType)
	// CPython: Objects/frameobject.c:1109 _PyFrame_New_NoTrack
	// followed by PyObject_GC_Track at the call site.
	if h := GCTrackHook; h != nil {
		h(f)
	}
	// Register the wrapper on the live activation record so the chunk
	// arena can rebind us to a snapshot before its storage gets recycled.
	//
	// CPython: Objects/frameobject.c:1109 _PyFrame_New_NoTrack (the
	// PyFrameObject linkage CPython gets for free from refcounting).
	if interp != nil {
		interp.FrameRegisterWrapper(f)
	}
	return f
}

// SwapInterp replaces the wrapper's underlying activation record. The
// chunk arena calls this on Pop when the live iframe is about to be
// recycled, pointing the wrapper at a FrameSnapshot so f_code,
// f_globals, and f_locals reads still work after the function returns.
//
// CPython: Objects/frameobject.c:1138 take_ownership (the iframe copy
// PyFrameObject keeps after the activation record goes away).
func (f *Frame) SwapInterp(i InterpreterFrame) {
	if f == nil {
		return
	}
	f.interp = i
}

// FrameType returns the type singleton for `frame`.
//
// CPython: Objects/frameobject.c:1238 PyFrame_Type
func FrameType() *Type { return frameType }

// Interp returns the underlying interpreter frame.
func (f *Frame) Interp() InterpreterFrame { return f.interp }

// TakeOwnership installs a take_ownership-style snapshot on the
// underlying activation record so reads through this wrapper, and
// through any sibling *Frame wrapping the same iframe (e.g., one
// returned by sys._getframe() inside the gen's body), survive a
// subsequent FrameClearLocals. Idempotent and safe to call when
// interp is nil.
//
// CPython: Objects/frameobject.c:1138 take_ownership
func (f *Frame) TakeOwnership() {
	if f == nil || f.interp == nil {
		return
	}
	f.interp.FrameTakeOwnership()
}

// Code returns f.f_code: the running Code object.
//
// CPython: Objects/frameobject.c:794 frame_getcode / 1336 PyFrame_GetCode
func (f *Frame) Code() *Code { return f.interp.FrameCode() }

// Globals returns f.f_globals.
//
// CPython: Objects/frameobject.c:879 frame_getglobals / 1357 PyFrame_GetGlobals
func (f *Frame) Globals() Object { return f.interp.FrameGlobals() }

// Builtins returns f.f_builtins.
//
// CPython: Objects/frameobject.c:888 frame_getbuiltins / 1346 PyFrame_GetBuiltins
func (f *Frame) Builtins() Object { return f.interp.FrameBuiltins() }

// Lasti returns f.f_lasti, the offset (in code units) of the last
// dispatched instruction.
//
// CPython: Objects/frameobject.c:802 frame_getlasti / 1380 PyFrame_GetLasti
func (f *Frame) Lasti() int { return f.interp.FrameLasti() }

// Back returns the caller's Python-level frame. nil when this frame
// has no caller.
//
// CPython: Objects/frameobject.c:1314 PyFrame_GetBack
func (f *Frame) Back() *Frame {
	parent := f.interp.FrameBack()
	if parent == nil {
		return nil
	}
	return NewFrame(parent)
}

// Lineno returns f.f_lineno: the source line of the running
// instruction. Zero when the line table is empty or f_lasti falls
// outside any covered range.
//
// CPython: Objects/frameobject.c:843 frame_getlineno / 1242 PyFrame_GetLineNumber
func (f *Frame) Lineno() int {
	code := f.interp.FrameCode()
	if code == nil {
		return 0
	}
	pos, ok := CoAddr2Location(code, f.interp.FrameLasti())
	if !ok {
		return 0
	}
	return pos.Line
}

// Trace returns f.f_trace, or None if no trace function is set.
//
// CPython: Objects/frameobject.c:863 frame_gettrace
func (f *Frame) Trace() Object {
	if f.trace == nil {
		return None()
	}
	return f.trace
}

// SetTrace sets f.f_trace. Pass nil or None to clear.
//
// CPython: Objects/frameobject.c:870 frame_settrace
func (f *Frame) SetTrace(fn Object) {
	if fn == nil || fn == None() {
		f.trace = nil
		return
	}
	f.trace = fn
}

// TraceLines reports f.f_trace_lines.
func (f *Frame) TraceLines() bool { return f.traceLines }

// SetTraceLines stores f.f_trace_lines.
func (f *Frame) SetTraceLines(v bool) { f.traceLines = v }

// TraceOpcodes reports f.f_trace_opcodes.
func (f *Frame) TraceOpcodes() bool { return f.traceOpcodes }

// SetTraceOpcodes stores f.f_trace_opcodes.
func (f *Frame) SetTraceOpcodes(v bool) { f.traceOpcodes = v }

// frameRepr renders frame_repr.
//
// CPython: Objects/frameobject.c:809 frame_repr
func frameRepr(o Object) (string, error) {
	f := o.(*Frame)
	name, file := "?", "?"
	if code := f.interp.FrameCode(); code != nil {
		name = code.Name
		file = code.Filename
	}
	return fmt.Sprintf("<frame at %p, file '%s', line %d, code %s>",
		f, file, f.Lineno(), name), nil
}
