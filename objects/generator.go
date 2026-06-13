// Generator object. Ports PyGenObject from Objects/genobject.c.
// The yield/send protocol uses Go channels so each generator runs on
// its own goroutine and blocks at YIELD_VALUE; the caller unblocks it
// via Send. This mirrors CPython's frame-suspend approach but uses
// goroutines instead of C stack switching.
//
// CPython: Include/cpython/genobject.h PyGenObject
// CPython: Objects/genobject.c

package objects

import (
	"errors"
	"fmt"
	"os"
	"runtime/debug"
	"sync/atomic"
)

var (
	debugGenFinalize      = os.Getenv("GOPY_DEBUG_GENFINALIZE") != ""
	debugGenFinalizeStack = os.Getenv("GOPY_DEBUG_GENFINALIZE_STACK") != ""
)

// CloseReturnValue is the sentinel error returned by closeWith when the
// generator body caught GeneratorExit and returned a non-None value. The vm
// package (genCloseMethod) unwraps it and returns Val to the Python caller.
//
// CPython: Objects/genobject.c:449 gen_close (_PyGen_FetchStopIterationValue)
type CloseReturnValue struct {
	Val Object
}

func (c *CloseReturnValue) Error() string { return "CloseReturnValue" }

// ErrGeneratorExit is the Go-level sentinel for PyExc_GeneratorExit.
// close() throws this into the body; if the body yields a value
// instead of swallowing it, the runtime raises RuntimeError per
// CPython's "generator ignored GeneratorExit" check.
//
// CPython: Objects/exceptions.c PyExc_GeneratorExit
var ErrGeneratorExit = errors.New("GeneratorExit")

// ErrStopAsyncIteration mirrors PyExc_StopAsyncIteration. An async
// generator returning normally surfaces this to its consumer.
//
// CPython: Objects/exceptions.c PyExc_StopAsyncIteration
var ErrStopAsyncIteration = errors.New("StopAsyncIteration")

// GenMsg is the channel message type for the generator yield/send protocol.
// Exported so the vm package can use it without importing this struct
// from a helper package.
type GenMsg struct {
	Val Object // yielded / sent value; nil when Err is set
	Err error  // ErrStopIteration at normal end; other errors on throw()
	// CallerFrame is the caller's currently-executing frame at the time of
	// Send/Throw/Close. The generator goroutine assigns it to its own
	// frame.Previous so gi_frame.f_back reflects the caller while the body
	// is running. Mirrors CPython's gen_send_ex2 which sets
	// frame->previous = tstate->current_frame on every resume.
	//
	// CPython: Objects/genobject.c:248 gen_send_ex2
	CallerFrame InterpreterFrame
}

// RaisedError is the Go-level wrapper for a Python exception object
// crossing the generator yield/send protocol or any other channel that
// only carries Go errors. The vm side recognizes this wrapper and
// installs Exc on the thread state via pyerrors.Raise before unwinding,
// so a `raise` re-raised inside the generator preserves the original
// PyObject identity (`exc is value` checks in contextlib's __exit__).
//
// CPython: equivalent of PyErr_Restore passing the exception PyObject
// directly across the generator boundary.
type RaisedError struct {
	Exc Object // The Python exception instance
	Msg string // Formatted message for Error()
}

// Error implements the error interface. The text mirrors excSentinel
// so any caller that pins err.Error() keeps working.
func (r *RaisedError) Error() string {
	if r == nil {
		return "Exception"
	}
	return r.Msg
}

// Generator is PyGenObject: a suspended frame that produces values
// one at a time via __next__ or send(). The goroutine that runs the
// generator body communicates through YieldCh and SendCh.
//
// CPython: Objects/genobject.c:L49 PyGenObject
type Generator struct {
	Header
	Name     string
	Qualname string

	// YieldCh carries values from the generator to the caller.
	YieldCh chan GenMsg
	// SendCh carries values from the caller into the generator.
	SendCh chan GenMsg

	started bool
	closed  bool

	// Running is 1 while the generator goroutine is actively executing
	// the body (between reading from SendCh and writing to YieldCh).
	// Mirrors CPython's gi_frame_state == FRAME_EXECUTING check that
	// prevents re-entrant calls from deadlocking.
	//
	// CPython: Objects/genobject.c:275 gen_send_ex2 FRAME_EXECUTING check
	Running atomic.Int32

	// gi_exc_state emulation: CPython keeps a per-generator _PyErr_StackItem
	// (gi_exc_state.exc_value) separate from the caller's exc_info chain.
	// gopy replaces the linked-list model with three fields:
	//   ExcHandled: the ts.HandledException value saved at the last yield,
	//               only valid when ExcDepth > 0 (generator is inside ≥1
	//               of its own except blocks at yield time).
	//   CallerExc:  the caller's ts.HandledException at the most recent
	//               send/throw boundary; restored when the generator yields.
	//   ExcDepth:   nesting count of active PUSH_EXC_INFO opcodes executed
	//               inside this generator (incremented by PUSH_EXC_INFO,
	//               decremented by POP_EXCEPT).
	//
	// CPython: Include/cpython/genobject.h:L23 gi_exc_state (_PyErr_StackItem)
	// CPython: Objects/genobject.c:248 gen_send_ex2 (exc_info push/pop)
	// CPython: Python/errors.c:116 _PyErr_GetTopmostException (chain walk)
	ExcHandled Object
	CallerExc  Object
	ExcDepth   int

	// Code is the code object for the generator function.
	// Set at creation time from the frame's code object so gi_code can
	// return it even after the frame is released.
	//
	// CPython: Include/cpython/genobject.h:L17 gi_code (PyCodeObject*)
	Code Object

	// GiFrame holds the Python-visible frame object for the suspended
	// generator. Set by the vm package (execReturnGenerator) and cleared
	// to None when the generator is exhausted. Only non-nil between
	// RETURN_GENERATOR and the final yield/return.
	//
	// CPython: Include/cpython/genobject.h:L15 gi_iframe
	GiFrame Object

	// YieldFromTarget is the sub-iterator currently being delegated by
	// yield from. Set by execSend before forwarding a value and cleared
	// when the sub-iterator raises StopIteration. Mirrors the
	// FRAME_SUSPENDED_YIELD_FROM state check in _gen_throw.
	//
	// CPython: Objects/genobject.c:469 _gen_throw (_PyGen_yf)
	YieldFromTarget Object
}

// GenExcState is implemented by generators, coroutines, and async
// generators: the three suspendable types CPython drives through the
// shared gen_send_ex2. Each carries its own _PyErr_StackItem
// (gi_/cr_/ag_exc_state) that must be swapped out for the caller's
// exc_info on yield and swapped back on resume, so a handled exception
// inside a suspended body never leaks into the caller's chain.
//
// CPython: Objects/genobject.c:248 gen_send_ex2 (exc_info push/pop)
type GenExcState interface {
	GetExcHandled() Object
	SetExcHandled(Object)
	GetCallerExc() Object
	SetCallerExc(Object)
	ExcDepthVal() int
	IncExcDepth()
	DecExcDepth()
}

func (g *Generator) GetExcHandled() Object  { return g.ExcHandled }
func (g *Generator) SetExcHandled(o Object) { g.ExcHandled = o }
func (g *Generator) GetCallerExc() Object   { return g.CallerExc }
func (g *Generator) SetCallerExc(o Object)  { g.CallerExc = o }
func (g *Generator) ExcDepthVal() int       { return g.ExcDepth }
func (g *Generator) IncExcDepth()           { g.ExcDepth++ }
func (g *Generator) DecExcDepth() {
	if g.ExcDepth > 0 {
		g.ExcDepth--
	}
}

// GeneratorType is the type singleton for generator.
//
// CPython: Objects/genobject.c:L898 PyGen_Type
var GeneratorType *Type

func init() {
	GeneratorType = NewType("generator", []*Type{objectType})
	// CPython: Objects/genobject.c gen_methods ("__class_getitem__")
	bindClassGetitem(GeneratorType)
	GeneratorType.Repr = genRepr
	GeneratorType.Str = genRepr
	GeneratorType.Iter = func(o Object) (Object, error) { Incref(o); return o, nil }
	GeneratorType.IterNext = genIterNext
	GeneratorType.Getattro = GenericGetAttr
	GeneratorType.Setattro = GenericSetAttr
	// Generators are hashable by identity, inheriting object's
	// _Py_HashPointer through inherit_slots.
	//
	// CPython: Objects/genobject.c PyGen_Type (tp_hash inherited from object)
	GeneratorType.Hash = IdentityHash
	// Tp_finalize: invoked by the cycle collector when the generator
	// becomes unreachable while still suspended. Calls close() so the
	// body's finally clauses run before memory is reclaimed.
	//
	// CPython: Objects/genobject.c:87 _PyGen_Finalize
	GeneratorType.Finalize = genFinalize
	// Tp_traverse: lets the cycle collector walk references the
	// generator holds (its frame, exception state). Without this, an
	// orphaned generator looks reachable to the collector and never
	// becomes a finalize candidate.
	//
	// CPython: Objects/genobject.c:69 gen_traverse
	GeneratorType.TpTraverse = genTraverse
	for name, fn := range map[string]func([]Object, map[string]Object) (Object, error){
		"send":          genSendMethod,
		"throw":         genThrowMethod,
		"close":         genCloseMethod,
		"__reduce__":    genReduceReject,
		"__reduce_ex__": genReduceReject,
	} {
		SetTypeDescr(GeneratorType, name, NewMethodDescr(GeneratorType, name, fn))
	}
	// gi_running: 1 when the generator body is executing, 0 otherwise.
	//
	// CPython: Objects/genobject.c gi_running member (PyMemberDef)
	SetTypeDescr(GeneratorType, "gi_running", NewGetSetDescr("gi_running",
		func(o Object) (Object, error) {
			g := o.(*Generator)
			return NewInt(int64(g.Running.Load())), nil
		}, nil))
	// gi_frame: the frame object of the suspended generator. Returns None
	// when the generator is exhausted or closed.
	//
	// CPython: Objects/genobject.c gi_frame member
	SetTypeDescr(GeneratorType, "gi_frame", NewGetSetDescr("gi_frame",
		func(o Object) (Object, error) {
			g := o.(*Generator)
			if !g.closed && g.GiFrame != nil {
				// Handing the frame object to user code is exactly
				// CPython's _PyFrame_GetFrameObject materializing
				// frame->frame_obj; take_ownership keys off that when the
				// generator is later finalized.
				//
				// CPython: Objects/frameobject.c:1138 take_ownership
				if fr, ok := g.GiFrame.(*Frame); ok {
					fr.MarkExposed()
				}
				return g.GiFrame, nil
			}
			return None(), nil
		}, nil))
	// gi_suspended: True when the generator is suspended (yielded), False when
	// running or closed.
	//
	// CPython: Objects/genobject.c gi_suspended member (PyMemberDef)
	SetTypeDescr(GeneratorType, "gi_suspended", NewGetSetDescr("gi_suspended",
		func(o Object) (Object, error) {
			g := o.(*Generator)
			if g.started && !g.closed && g.Running.Load() == 0 {
				return True(), nil
			}
			return False(), nil
		}, nil))
	// gi_yieldfrom: the object currently being iterated by yield from,
	// or None. CPython only exposes the yield-from target while the
	// frame is in FRAME_SUSPENDED_YIELD_FROM, that is, suspended on a
	// `yield from`. While the generator is executing (re-entered into
	// the body) the attribute reads as None even if the body originally
	// suspended on a yield-from, because the inner subgenerator is the
	// one running, not us.
	//
	// CPython: Objects/genobject.c:374 _PyGen_yf
	// CPython: Objects/genobject.c:750 gen_getyieldfrom
	SetTypeDescr(GeneratorType, "gi_yieldfrom", NewGetSetDescr("gi_yieldfrom",
		func(o Object) (Object, error) {
			g := o.(*Generator)
			if g.YieldFromTarget != nil && g.started && !g.closed && g.Running.Load() == 0 {
				Incref(g.YieldFromTarget)
				return g.YieldFromTarget, nil
			}
			return None(), nil
		}, nil))
	// gi_code: the code object for the generator function.
	//
	// CPython: Objects/genobject.c gi_code member (PyMemberDef)
	SetTypeDescr(GeneratorType, "gi_code", NewGetSetDescr("gi_code",
		func(o Object) (Object, error) {
			g := o.(*Generator)
			if g.Code != nil {
				return g.Code, nil
			}
			return None(), nil
		}, nil))
	// __name__: writable string name of the generator.
	//
	// CPython: Objects/genobject.c gen_name getter/setter (PyGetSetDef)
	SetTypeDescr(GeneratorType, "__name__", NewGetSetDescr("__name__",
		func(o Object) (Object, error) {
			return NewStr(o.(*Generator).Name), nil
		},
		func(o Object, v Object) error {
			if v == nil {
				return fmt.Errorf("TypeError: __name__ attribute cannot be deleted")
			}
			s, ok := v.(*Unicode)
			if !ok {
				return fmt.Errorf("TypeError: __name__ must be a string, not %s", v.Type().Name)
			}
			o.(*Generator).Name = s.Value()
			return nil
		}))
	// __qualname__: writable qualified name of the generator.
	//
	// CPython: Objects/genobject.c gen_qualname getter/setter (PyGetSetDef)
	SetTypeDescr(GeneratorType, "__qualname__", NewGetSetDescr("__qualname__",
		func(o Object) (Object, error) {
			g := o.(*Generator)
			if g.Qualname != "" {
				return NewStr(g.Qualname), nil
			}
			return NewStr(g.Name), nil
		},
		func(o Object, v Object) error {
			if v == nil {
				return fmt.Errorf("TypeError: __qualname__ attribute cannot be deleted")
			}
			s, ok := v.(*Unicode)
			if !ok {
				return fmt.Errorf("TypeError: __qualname__ must be a string, not %s", v.Type().Name)
			}
			o.(*Generator).Qualname = s.Value()
			return nil
		}))
	AddIterSlotWrappers(GeneratorType)
}

// genReduceReject implements __reduce__ / __reduce_ex__ for generator,
// coroutine, and async-generator objects by raising TypeError. CPython
// does not register a pickle reducer for these types, so pickle.dumps
// falls through to object.__reduce_ex__ which raises the same error.
// The doctest gates rely on this rejection rather than producing a
// dangling pickle that would fail to load.
//
// CPython: Objects/typeobject.c:5827 reduce_newobj (rejects types
// without a usable __new__ chain)
//
//nolint:unparam // descriptor signature requires (Object, error); always rejects.
func genReduceReject(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: __reduce__ missing self")
	}
	tname := "object"
	if t := args[0].Type(); t != nil {
		tname = t.Name
	}
	return nil, fmt.Errorf("TypeError: cannot pickle %q object", tname)
}

func genSendMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: send() takes exactly one argument")
	}
	g, ok := args[0].(*Generator)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'send' requires a 'generator' object")
	}
	return g.Send(args[1])
}

// GenThrowHook converts a Python exception object to a Go error for
// generator.throw(). Installed by the vm package to break the import cycle.
// The returned error is what gets sent into the generator goroutine;
// callers can pass it to Generator.Throw directly.
var GenThrowHook func(Object) (error, error)

// GenThrowTripleHook implements the legacy 3-arg throw normalization
// (PyErr_NormalizeException + traceback validation). vm installs it
// so the heavy lifting (exception class instantiation, MRO check)
// lives next to the rest of the exception plumbing.
//
// CPython: Objects/genobject.c:541 _gen_throw (throw_here block)
// CPython: Python/errors.c:389 _PyErr_NormalizeException
var GenThrowTripleHook func(typ, val, tb Object) (error, error)

// GenThrowForwardHook forwards a throw into a custom (non-Generator)
// yield-from sub-iterator by calling yf.throw(exc). The first argument
// is the outer Generator whose frame must be installed as the "current
// frame" before the call, mirroring CPython's frame->previous/
// tstate->current_frame dance in _gen_throw. Installed by vm.
// Returns (val, nil) when yf.throw() yields a value, (nil, err) otherwise.
//
// CPython: Objects/genobject.c:523 _gen_throw (tstate->current_frame = frame
// for both generator and custom-iterator paths)
var GenThrowForwardHook func(gen Object, yf Object, err error) (Object, error)

// GenAttachFrameTBHook prepends a traceback entry for the generator
// body's frame onto err's underlying exception. Used by Throw on an
// unstarted generator, where CPython would have started the body to
// raise the throw at the body entry point (adding the body frame to
// tb) but gopy's goroutine-driven body has no mechanism to inject an
// exception at the first send. Installed by vm.
//
// CPython: Objects/genobject.c:466 _gen_throw (gen_send_ex runs the
// body which propagates with PyTraceBack_Here entries)
var GenAttachFrameTBHook func(g *Generator, err error) error

// DebugGenFinalize, when set, is called at the entry to genFinalize for
// every generator the cycle collector picks up. Used for diagnostics
// only; production builds leave it nil.
var DebugGenFinalize func(*Generator)

// NewRaisedError wraps a Python exception object as a Go error. The
// caller is responsible for ensuring exc is a real exception instance
// (not a class). msg should be the formatted "Type: message" string.
func NewRaisedError(exc Object, msg string) *RaisedError {
	return &RaisedError{Exc: exc, Msg: msg}
}

func genThrowMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: throw() requires an exception")
	}
	g, ok := args[0].(*Generator)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'throw' requires a 'generator' object")
	}
	if GenThrowHook == nil {
		return nil, fmt.Errorf("RuntimeError: generator.throw not available")
	}
	// CPython: Objects/genobject.c:599 gen_throw. throw(typ, val, tb) is
	// the deprecated 3-arg form; emit DeprecationWarning, then normalize
	// the (typ, val, tb) triple via PyErr_NormalizeException-equivalent.
	if len(args) > 2 {
		if DeprecWarnHook != nil {
			if werr := DeprecWarnHook("the (type, exc, tb) signature of throw() is deprecated, use the single-arg signature instead."); werr != nil {
				return nil, werr
			}
		}
		if GenThrowTripleHook == nil {
			return nil, fmt.Errorf("RuntimeError: generator.throw 3-arg form not available")
		}
		var val, tb Object
		if len(args) > 2 {
			val = args[2]
		}
		if len(args) > 3 {
			tb = args[3]
		}
		exc, err := GenThrowTripleHook(args[1], val, tb)
		if err != nil {
			return nil, err
		}
		return g.Throw(exc)
	}
	exc, err := GenThrowHook(args[1])
	if err != nil {
		return nil, err
	}
	return g.Throw(exc)
}

func genCloseMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: close() missing self argument")
	}
	g, ok := args[0].(*Generator)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'close' requires a 'generator' object")
	}
	err := g.Close()
	if err == nil {
		return None(), nil
	}
	// When the generator caught GeneratorExit and returned a value, closeWith
	// surfaces it via CloseReturnValue rather than propagating it as an error.
	//
	// CPython: Objects/genobject.c:449 gen_close (_PyGen_FetchStopIterationValue)
	var crv *CloseReturnValue
	if errors.As(err, &crv) {
		return crv.Val, nil
	}
	return nil, err
}

// NewGenerator creates a generator with the given name and qualname. The caller
// (RETURN_GENERATOR in the vm package) is responsible for starting the
// goroutine that drives the body and communicates via YieldCh/SendCh.
//
// CPython: Objects/genobject.c:L867 gen_new_with_qualname
func NewGenerator(name, qualname string) *Generator {
	g := &Generator{
		Name:     name,
		Qualname: qualname,
		YieldCh:  make(chan GenMsg, 1),
		SendCh:   make(chan GenMsg, 1),
	}
	g.init(GeneratorType)
	// CPython tracks every generator at tp_alloc so the cycle collector
	// can pick up the frame.locals self-cycle that forms on `g.send(g)`,
	// or any other reachability path that would otherwise let a suspended
	// generator outlive every Python-visible reference.
	//
	// CPython: Objects/genobject.c:867 gen_new_with_qualname
	//   (PyObject_GC_Track at the end of make_gen)
	if h := GCTrackHook; h != nil {
		h(g)
	}
	return g
}

// genFinalize is the tp_finalize slot for generator objects. The cycle
// collector calls it on a suspended (un-finished) generator that has
// become unreachable. The generator is closed so its finally clauses
// run and any resources held by the suspended frame are released.
//
// CPython: Objects/genobject.c:87 _PyGen_Finalize
func genFinalize(o Object) {
	g, ok := o.(*Generator)
	if !ok {
		return
	}
	// CPython: Objects/genobject.c:91 FRAME_STATE_FINISHED early return.
	if g.closed {
		return
	}
	if debugGenFinalize {
		fmt.Fprintf(os.Stderr, "[genFinalize] g=%p qualname=%v running=%d closed=%v yf=%v refcnt=%d\n",
			g, g.Qualname, g.Running.Load(), g.closed, g.YieldFromTarget, g.Hdr().Refcnt())
		if debugGenFinalizeStack {
			fmt.Fprintf(os.Stderr, "%s\n", string(debug.Stack()))
		}
	}
	// A running generator cannot be finalized. In CPython the GIL plus
	// the frame's external refcount make this unreachable: a generator
	// whose body is mid-execution always has its frame on a call stack,
	// so its refcount never drops to zero. In gopy the body executes on
	// its own goroutine, and the goroutine-stack reference to the
	// Generator is invisible to the Python cycle collector. Without this
	// guard the collector picks up the running generator as unreachable
	// and closeWith deadlocks: closeWith would send GeneratorExit on
	// SendCh then block on YieldCh waiting for a yield that the running
	// goroutine is in no position to produce (it is either this very
	// goroutine, recursively, or another one whose next yield is what
	// triggered the collection).
	//
	// CPython: Objects/genobject.c:91 _PyGen_Finalize
	//   (FRAME_STATE_EXECUTING implicit via GIL)
	if g.Running.Load() == 1 {
		return
	}
	// Save the active thread's pending exception across Close: the
	// generator body runs on its own goroutine but mutates the caller's
	// thread-state slot (savedTS in vm/eval_gen). Without this, a
	// finalize that fires while the caller already had a different
	// exception pending (or no exception at all) would leak the body's
	// GeneratorExit into the caller's slot.
	//
	// CPython: Objects/genobject.c:98 _PyGen_Finalize
	//   (PyErr_GetRaisedException + PyErr_SetRaisedException pair)
	var saved any
	if h := SaveCurrentExceptionHook; h != nil {
		saved = h()
	}
	// Build the err_msg prefix before Close clears any state so the
	// generator's repr reflects the suspended frame, matching CPython's
	// gen_close which formats the message before the body's GeneratorExit
	// can change visible attributes.
	//
	// CPython: Objects/genobject.c:131 gen_close + PyErr_FormatUnraisable
	gRepr, _ := Repr(g)
	// Take ownership of the iframe so external references to gi_frame
	// (the test_frame_outlives_generator path) keep reading fast-local
	// data after Close clears the underlying activation record. CPython
	// only performs take_ownership when a PyFrameObject was materialized
	// and outlives the activation record (frame->frame_obj != NULL); a
	// frame object nobody requested is simply cleared by gen_close. gopy
	// mints the gi_frame wrapper eagerly, so it tracks exposure with an
	// explicit flag set by the gi_frame getter and sys._getframe instead.
	// Snapshotting unconditionally would pin every suspended fast local
	// (for example an iterator's owner whose __del__ the caller is waiting
	// on) past the close, so the locals never reach the collector and
	// free_after_iterating never fires.
	//
	// CPython: Objects/frameobject.c:1138 take_ownership (frame->frame_obj != NULL)
	if g.GiFrame != nil {
		if fr, ok := g.GiFrame.(*Frame); ok && fr.Exposed() {
			fr.TakeOwnership()
		}
	}
	closeErr := g.Close()
	if closeErr != nil {
		var crv *CloseReturnValue
		if !errors.As(closeErr, &crv) {
			if h := WriteUnraisableHook; h != nil {
				h(g, "Exception ignored while closing generator "+gRepr, closeErr)
			}
		}
	}
	if h := RestoreCurrentExceptionHook; h != nil {
		h(saved)
	}
}

// genTraverse implements tp_traverse for generators. Walks the
// references the generator owns so the cycle collector can decide
// whether a generator participates in an unreachable cycle.
//
// YieldFromTarget is intentionally NOT visited here: CPython has no
// separate gi_yieldfrom field, it recovers the yield-from receiver
// from the frame's value stack on demand (_PyGen_yf). The receiver
// always lives at the SEND peek slot on the suspended frame, so the
// GiFrame walk below already visits it. Visiting YieldFromTarget on
// top of that double-decrements the receiver's refs in subtract_refs
// and pushes long yield-from chains (Conjoin/Queens) underwater.
//
// CPython: Objects/genobject.c:60 gen_traverse
func genTraverse(o Object, visit Visitor) error {
	g, ok := o.(*Generator)
	if !ok {
		return nil
	}
	if g.GiFrame != nil {
		if err := visit(g.GiFrame); err != nil {
			return err
		}
	}
	if g.ExcHandled != nil {
		if err := visit(g.ExcHandled); err != nil {
			return err
		}
	}
	if g.Code != nil {
		if err := visit(g.Code); err != nil {
			return err
		}
	}
	return nil
}

// GCRoot pins a running generator as a cycle-collector root. The body
// executes on its own goroutine, whose stack holds the live reference
// to this generator and to every sub-generator it is currently
// iterating. That reference is invisible to the refcount collector, so
// without pinning, subtract_refs would drive the whole active spine to
// zero and reclaim the suspended children mid-iteration.
//
// CPython: Python/gc.c:1208 gc_collect_main (executing frame stays rooted)
func (g *Generator) GCRoot() bool { return g.Running.Load() == 1 }

// MarkFinished records that the generator body has run to completion
// (either by returning, raising, or having no more yields). After this
// call genFinalize / Close are no-ops so we never try to send on
// SendCh after the body goroutine has already exited.
//
// CPython: Objects/genobject.c:225 gen_send_ex2 (gi_frame_state = FRAME_COMPLETED)
func (g *Generator) MarkFinished() {
	g.closed = true
}

// Send delivers v into the generator and returns the next yielded value.
// Sending None to an unstarted generator is equivalent to __next__.
// Sending a non-None to an unstarted generator raises TypeError.
//
// CPython: Objects/genobject.c:L260 gen_send_ex2
func (g *Generator) Send(v Object) (Object, error) {
	if g.closed {
		return nil, ErrStopIteration
	}
	if !g.started && v != None() {
		return nil, errors.New("TypeError: can't send non-None value to a just-started generator")
	}
	// Detect re-entrant calls: if the generator body is currently executing
	// (e.g., the body calls next() on itself), raise ValueError immediately
	// rather than deadlocking on the channel. Mirrors CPython's
	// gi_frame_state == FRAME_EXECUTING guard.
	//
	// CPython: Objects/genobject.c:275 gen_send_ex2
	if g.Running.Load() == 1 {
		return nil, fmt.Errorf("ValueError: generator already executing")
	}
	g.started = true
	g.SendCh <- GenMsg{Val: v, CallerFrame: callerFrame()}
	msg := <-g.YieldCh
	if msg.Err != nil {
		g.closed = true
		return nil, msg.Err
	}
	return msg.Val, nil
}

// callerFrame returns the current Python frame on this goroutine, used by
// Send / Throw / Close to stamp gi_frame.f_back on the generator body for
// the duration of the resume. CPython does the same via
// frame->previous = tstate->current_frame in gen_send_ex2.
//
// CPython: Objects/genobject.c:248 gen_send_ex2
func callerFrame() InterpreterFrame {
	if CurrentFrameHook == nil {
		return nil
	}
	return CurrentFrameHook()
}

// ownFrame returns the generator body's own interpreter frame, used as
// the f_back stamped onto a sub-iterator when a throw is forwarded
// through yield from. gen.throw() unwinds the delegation chain on the
// throwing goroutine via nested Go calls, so callerFrame() there is the
// throwing frame, not the delegating generator. Passing this frame down
// keeps the resumed sub-iterator's f_back identical to what the send()
// path builds, so traceback.extract_stack() reports the same depth from
// throw() as from send().
//
// CPython: Objects/genobject.c:489 _gen_throw (tstate->current_frame = frame)
func (g *Generator) ownFrame() InterpreterFrame {
	if f, ok := g.GiFrame.(*Frame); ok && f != nil {
		return f.Interp()
	}
	return nil
}

// Throw raises err inside the generator at its current YIELD_VALUE
// suspension point. If the generator is suspended inside yield from,
// the throw is first forwarded to the sub-iterator. If the generator
// catches it and yields, that value is returned; if it propagates,
// Throw returns the error.
//
// CPython: Objects/genobject.c:L466 _gen_throw
func (g *Generator) Throw(err error) (Object, error) {
	return g.throwWithCaller(err, callerFrame())
}

// throwWithCaller is the body of Throw with the resume frame threaded
// explicitly. Public Throw passes the throwing goroutine's current
// frame; a forwarded throw from a delegating generator/coroutine passes
// the delegator's own frame so the f_back chain matches the send() path.
//
// CPython: Objects/genobject.c:466 _gen_throw
func (g *Generator) throwWithCaller(err error, caller InterpreterFrame) (Object, error) {
	if err == nil {
		return nil, errors.New("TypeError: throw() requires an exception")
	}
	if g.closed {
		return nil, err
	}
	if !g.started {
		// Throwing into an unstarted generator: synthesize the body
		// frame entry via GenAttachFrameTBHook so the body's frame
		// appears in tb. We do not run the body because the goroutine
		// is parked at <-sendCh and gopy has no mechanism to inject a
		// pending exception at the body entry point without dispatching
		// the body's first op.
		//
		// CPython: Objects/genobject.c:466 _gen_throw -> gen_send_ex
		g.closed = true
		if h := GenAttachFrameTBHook; h != nil {
			err = h(g, err)
		}
		return nil, err
	}
	// Forward to the yield-from sub-iterator if present.
	// CPython: Objects/genobject.c:469 _gen_throw (_PyGen_yf branch)
	if yf := g.YieldFromTarget; yf != nil {
		// gen.throw() enters with close_on_genexit=1 (public throw
		// always sets it). When the thrown exception is GeneratorExit
		// the sub-iter is closed (its finally runs, raising "ignored
		// GeneratorExit" if it swallows + yields), then the exit is
		// re-raised into the outer body via throw_here below.
		//
		// CPython: Objects/genobject.c:475 _gen_throw (close_on_genexit branch)
		if errors.Is(err, ErrGeneratorExit) || isGeneratorExitException(err) {
			g.Running.Store(1)
			e := GenCloseIter(yf)
			g.Running.Store(0)
			if e != nil && !isCleanCloseError(e) {
				g.YieldFromTarget = nil
				g.closed = true
				return nil, e
			}
			g.YieldFromTarget = nil
			// Fall through to throw_here: send the GeneratorExit into
			// the outer body's yield from line via the channel.
		} else {
			// Mark outer as FRAME_EXECUTING while the throw is forwarded
			// to yf. gi_running visible to the sub-iter's throw handler
			// matches CPython, and a re-entrant send/throw on outer
			// raises ValueError "generator already executing".
			//
			// CPython: Objects/genobject.c:466 _gen_throw (FRAME_EXECUTING)
			g.Running.Store(1)
			forwarded := true
			var fval Object
			var ferr error
			// Link this generator's frame into the running call chain for
			// the forwarded throw, then unlink, so the resumed leaf reports
			// the full yield-from chain in its f_back.
			//
			// CPython: Objects/genobject.c:493 _gen_throw (frame linking)
			my := g.ownFrame()
			if my != nil {
				my.FrameSetBack(caller)
			}
			switch v := yf.(type) {
			case *Generator:
				fval, ferr = v.throwWithCaller(err, my)
			case *Coroutine:
				fval, ferr = v.throwWithCaller(err, my)
			default:
				if GenThrowForwardHook != nil {
					fval, ferr = GenThrowForwardHook(g, yf, err)
				} else {
					forwarded = false
				}
			}
			if my != nil {
				my.FrameSetBack(nil)
			}
			g.Running.Store(0)
			if forwarded {
				return g.forwardThrowResult(fval, ferr, caller)
			}
		}
	}
	g.SendCh <- GenMsg{Err: err, CallerFrame: caller}
	msg := <-g.YieldCh
	if msg.Err != nil {
		g.closed = true
		return nil, msg.Err
	}
	return msg.Val, nil
}

// forwardThrowResult handles the result of forwarding a throw to a
// yield-from sub-iterator. Mirrors the retval/StopIteration/other-exc
// branches in _gen_throw after the yf.throw() call.
//
// CPython: Objects/genobject.c:511 _gen_throw (retval / StopIteration branches)
func (g *Generator) forwardThrowResult(fval Object, ferr error, caller InterpreterFrame) (Object, error) {
	if ferr == nil {
		// Sub-iterator yielded fval. Return it directly as the outer
		// generator's yielded value without entering the body.
		//
		// CPython: Objects/genobject.c:511 _gen_throw (retval != NULL)
		return fval, nil
	}
	// Sub-iterator raised something (StopIteration or otherwise): throw
	// it into the outer body. CPython's _gen_throw at L536 always calls
	// gen_send_ex(gen, Py_None, 1, 0) when the sub-iter returned NULL,
	// regardless of the exception class. The yield-from exception table
	// entry routes a thrown StopIteration to CLEANUP_THROW, which
	// extracts .value and jumps past END_SEND. Sending the bare value
	// via Val instead would re-enter the SEND loop with a now-closed
	// sub-iterator and lose the carried value.
	//
	// CPython: Objects/genobject.c:536 _gen_throw (gen_send_ex exc=1)
	g.YieldFromTarget = nil
	g.SendCh <- GenMsg{Err: ferr, CallerFrame: caller}
	msg := <-g.YieldCh
	if msg.Err != nil {
		g.closed = true
		if errors.Is(msg.Err, ErrStopIteration) {
			return nil, ErrStopIteration
		}
		return nil, msg.Err
	}
	return msg.Val, nil
}

// Close throws GeneratorExit into the generator. A body that yields
// instead of swallowing the exit raises RuntimeError; StopIteration
// and GeneratorExit are both treated as a clean exit.
//
// CPython: Objects/genobject.c:L388 gen_close
func (g *Generator) Close() error {
	// Save and restore the active thread's pending exception across the
	// body's close. The body's eval installs the synthesized GeneratorExit
	// on the shared thread state via pyerrors.Raise as it unwinds. The
	// goroutine returns the exception via channel but does not clear it,
	// so without this sandwich the suppressed GeneratorExit leaks into the
	// caller's slot and reappears at the next op that checks
	// _PyErr_Occurred. CPython does not see this because gen_close runs on
	// the same call stack (no shared mutable tstate slot).
	//
	// CPython: Objects/genobject.c:449 gen_close (PyErr_Get/SetRaisedException pair)
	var saved any
	if h := SaveCurrentExceptionHook; h != nil {
		saved = h()
	}
	err := g.closeWith("generator ignored GeneratorExit")
	if h := RestoreCurrentExceptionHook; h != nil {
		h(saved)
	}
	return err
}

// closeWith bundles every shutdown path: a closed gen (no-op), an unstarted
// gen (frame release without throw), a stuck-in-yieldfrom gen (throw forwarded
// to sub-iterator), and the normal "throw GeneratorExit, swallow exit" walk.
// Splitting them out duplicates the frame-release dance; keep cohesion.
//
//nolint:gocognit,gocyclo // unified shutdown state machine, mirrors gen_close in CPython
func (g *Generator) closeWith(ignoredMsg string) error {
	if g.closed {
		return nil
	}
	if !g.started {
		g.closed = true
		// Release the bound args held in the suspended frame's
		// LocalsPlus. The goroutine never woke, so its deferred Pop
		// would otherwise leak those refs until the Generator object
		// is collected.
		//
		// CPython: Objects/genobject.c:155 gen_dealloc (calls
		// _PyFrame_ClearExceptCode before the generator object goes away)
		if g.GiFrame != nil {
			if fr, ok := g.GiFrame.(*Frame); ok && fr.interp != nil {
				fr.interp.FrameClearLocals()
			}
		}
		return nil
	}
	// Close the sub-iterator first so its finally clauses fire before
	// the GeneratorExit lands on the outer body. Without this, a
	// yield-from / await chain leaves the inner finally unrun: CPython
	// solves it via _PyGen_yf inside gen_close.
	//
	// CPython: Objects/genobject.c:405 gen_close (gen_close_iter call)
	//
	// If the inner generator's close raises a non-GeneratorExit /
	// non-StopIteration exception, that exception becomes the pending
	// throw for the outer body, mirroring CPython's "if (err == 0)
	// PyErr_SetNone(PyExc_GeneratorExit)" gate at L424: when err is
	// non-zero the pending exception from the sub-iter is forwarded as
	// the gen_send_ex throw.
	//
	// CPython: Objects/genobject.c:424 gen_close (err==0 gate)
	throwErr := ErrGeneratorExit
	if yf := g.YieldFromTarget; yf != nil {
		// Mark the outer generator as running while gen_close_iter
		// drives the sub-iterator. This matches CPython's
		// gen->gi_frame_state = FRAME_EXECUTING around gen_close_iter
		// in gen_close, so a user-defined yf.close() that touches
		// outer.gi_running sees True (and a re-entrant next/send/throw
		// on outer raises ValueError "generator already executing").
		//
		// CPython: Objects/genobject.c:421 gen_close (FRAME_EXECUTING)
		g.Running.Store(1)
		e := GenCloseIter(yf)
		g.Running.Store(0)
		if e != nil && !isCleanCloseError(e) {
			throwErr = e
		}
		g.YieldFromTarget = nil
	}
	g.SendCh <- GenMsg{Err: throwErr, CallerFrame: callerFrame()}
	msg := <-g.YieldCh
	g.closed = true
	if msg.Err == nil {
		// Body yielded a value rather than letting GeneratorExit
		// propagate. CPython calls this an error.
		return fmt.Errorf("RuntimeError: %s", ignoredMsg)
	}
	if errors.Is(msg.Err, ErrGeneratorExit) ||
		errors.Is(msg.Err, ErrStopIteration) {
		return nil
	}
	// RaisedError wraps a Python exception that propagated out of the
	// generator body. GeneratorExit is a clean exit (return nil). StopIteration
	// carries the generator's return value when the body caught GeneratorExit
	// and returned normally; surface it via CloseReturnValue so genCloseMethod
	// can return it to the caller.
	//
	// CPython: Objects/genobject.c:449 gen_close (_PyGen_FetchStopIterationValue)
	var re *RaisedError
	if errors.As(msg.Err, &re) && re.Exc != nil {
		name := re.Exc.Type().Name
		if name == "GeneratorExit" {
			return nil
		}
		if name == "StopIteration" {
			// Extract the return value (StopIteration.args[0]).
			if exc, ok := re.Exc.(ExceptionInstance); ok {
				args := exc.ExceptionArgs()
				if args != nil && args.Len() > 0 {
					v := args.Item(0)
					if v != nil && v != None() {
						return &CloseReturnValue{Val: v}
					}
				}
			}
			return nil
		}
	}
	return msg.Err
}

// isGeneratorExitException reports whether err carries a raised
// GeneratorExit instance, as opposed to the bare ErrGeneratorExit
// sentinel. _gen_throw's close_on_genexit branch keys on the type
// matching PyExc_GeneratorExit regardless of which form raised it.
//
// CPython: Objects/genobject.c:475 _gen_throw (PyErr_GivenExceptionMatches)
func isGeneratorExitException(err error) bool {
	var re *RaisedError
	if errors.As(err, &re) && re.Exc != nil {
		t := re.Exc.Type()
		for cur := t; cur != nil; cur = parentBase(cur) {
			if cur.Name == "GeneratorExit" {
				return true
			}
		}
	}
	return false
}

// parentBase returns the primary base of t (Bases[0]) or nil. Used to
// walk the MRO when matching exception class names without importing
// the errors package.
func parentBase(t *Type) *Type {
	if t == nil || len(t.Bases) == 0 {
		return nil
	}
	return t.Bases[0]
}

// isCleanCloseError returns true when an error returned by
// GenCloseIter represents a clean shutdown (GeneratorExit or
// StopIteration). Anything else is a "real" exception that gen_close
// must forward as the throw exception for the outer body.
//
// CPython: Objects/genobject.c:442 gen_close (PyErr_ExceptionMatches
// GeneratorExit / StopIteration filter).
func isCleanCloseError(e error) bool {
	if errors.Is(e, ErrGeneratorExit) || errors.Is(e, ErrStopIteration) {
		return true
	}
	// CloseReturnValue wraps a StopIteration return-value from the
	// sub-iterator's close. CPython's gen_close_iter discards the
	// returned object and returns 0 (clean) at Objects/genobject.c:344.
	var crv *CloseReturnValue
	if errors.As(e, &crv) {
		return true
	}
	var re *RaisedError
	if errors.As(e, &re) && re.Exc != nil {
		name := re.Exc.Type().Name
		if name == "GeneratorExit" || name == "StopIteration" {
			return true
		}
	}
	return false
}

// GenCloseIter mirrors CPython's gen_close_iter: a yield-from target
// that is a generator or coroutine has its Close() driven directly;
// any other awaitable has its close attribute looked up and called.
// Errors are reported back to the caller so gen_close can choose
// whether to surface them or swallow.
//
// CPython: Objects/genobject.c:412 gen_close_iter
func GenCloseIter(yf Object) error {
	switch v := yf.(type) {
	case *Generator:
		return v.Close()
	case *Coroutine:
		return v.Close()
	case *AsyncGenerator:
		return v.Close()
	}
	// PyObject_GetOptionalAttr semantics: a missing close attribute is
	// not an error, so the sub-iterator simply has nothing to close. Only
	// a lookup that raises something other than AttributeError is reported
	// as unraisable; a plain AttributeError means the attribute is absent.
	//
	// CPython: Objects/genobject.c:412 gen_close_iter (PyObject_GetOptionalAttr)
	closeFn, err := GetAttr(yf, NewStr("close"))
	if err != nil {
		if !isAttributeError(err) && WriteUnraisableHook != nil {
			WriteUnraisableHook(yf, "Exception ignored while closing iterator", err)
		}
		return nil
	}
	_, callErr := Call(closeFn, NewTuple(nil), nil)
	return callErr
}

func genIterNext(o Object) (Object, error) {
	return o.(*Generator).Send(None())
}

func genRepr(o Object) (string, error) {
	g := o.(*Generator)
	return fmt.Sprintf("<generator object %s at %p>", g.Name, g), nil
}
