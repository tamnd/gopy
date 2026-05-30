// Wires objects.FunctionType.Call so a Python-defined function can be
// invoked through objects.Call. Lives in the vm package because the
// call needs to push a new frame and drive the eval loop, which the
// objects package can't reach without an import cycle.
//
// CPython: Objects/funcobject.c function_call
package vm

// DEPRECATED (spec 1714): Spec 1714 phases 5+6: CALL family bodies migrate to typed op<NAME> functions invoked from vm/eval_dispatch_gen.go.
// See website/docs/specs/1700/1714_bytecodes_dsl_codegen.md.

import (
	"fmt"
	"sync"

	pyerrors "github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/frame"
	sys "github.com/tamnd/gopy/module/sys"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/stackref"
	"github.com/tamnd/gopy/state"
)

// activeThreads maps a goroutine ID to the *state.Thread Eval is
// running on that goroutine. Mirrors what CPython gets from
// PyThreadState_GET, just keyed off the Go scheduler instead of an
// OS thread. The map is concurrent-safe so one goroutine entering
// Eval cannot stomp on another goroutine's slot.
//
// Distinct goroutines run independent Eval calls without a lock; the
// map's only contention is between Eval's primer and a nested
// callPyFunction reader on the same goroutine, which never overlap.
//
// CPython: Include/internal/pycore_pystate.h _PyThreadState_GET
var activeThreads sync.Map // map[uint64]*state.Thread

// activeEvalFrames maps a goroutine ID to the *frame.Frame for the
// generator body currently suspended or running on that goroutine.
// Generator goroutines register here on resume and deregister on yield.
//
// CPython: _PyThreadState_GetFrame returns tstate->current_frame,
// which is always current because CPython uses a single C-stack per
// OS thread. gopy uses goroutines, so we maintain this per-goroutine.
var activeEvalFrames sync.Map // map[uint64]*frame.Frame

// genEntryDepths maps a generator goroutine ID to the frame-stack depth
// at the moment the generator body last entered (started or resumed). When
// the depth of frameStackFor(ts) exceeds this value, callPyFunction has
// pushed at least one frame on behalf of the generator body and
// currentInterpreterFrame should return the stack top rather than the
// generator's own saved frame.
//
// CPython: no direct equivalent — CPython's current_frame pointer is always
// the innermost frame regardless of whether it is a generator body or a
// regular call; gopy needs this extra bookkeeping because the generator
// frame is not on the shared FrameStack.
var genEntryDepths sync.Map // map[uint64]int

// genCallerFrames maps a *objects.Generator to the *frame.Frame that
// most recently drove it via SEND. Set by execSend before forwarding,
// read by the sub-generator goroutine on resume to establish f_back.
//
// CPython: Objects/genobject.c gen_send_ex2 sets previous_frame on the
// sub-generator's _PyInterpreterFrame before entering the body.
var genCallerFrames sync.Map // map[*objects.Generator]*frame.Frame

// genOwnFrames maps a *objects.Generator to the *frame.Frame that IS the
// generator's own suspended frame. Set when the generator goroutine starts,
// cleared when the body returns. Used by genThrowForwardHook to temporarily
// install the generator frame as the calling-thread's "current frame" before
// forwarding a throw to a custom iterator, mirroring CPython's
// tstate->current_frame = frame dance in _gen_throw.
//
// CPython: Objects/genobject.c:523 _gen_throw (frame->previous = prev; tstate->current_frame = frame)
var genOwnFrames sync.Map // map[*objects.Generator]*frame.Frame

// setActiveThread primes the current goroutine's Eval slot and
// returns the previous occupant + goid so the caller can restore it
// on the way out without re-resolving goid (runtime.Stack is the
// dominant cost in tight EvalCode loops). Mirrors CPython, where
// PyThreadState_Swap also takes the thread state in/out with one TLS
// read pair, not two.
//
// CPython: Python/pystate.c:2120 _PyThreadState_Swap
func setActiveThread(ts *state.Thread) (prev *state.Thread, g uint64) {
	g = goid()
	if v, ok := activeThreads.Load(g); ok {
		prev = v.(*state.Thread)
	}
	activeThreads.Store(g, ts)
	return prev, g
}

// restoreActiveThread is the defer-side of setActiveThread. Takes the
// cached goid from setActiveThread so we avoid a second goid() call
// per Eval; runtime.Stack is the bench-dominant cost otherwise.
func restoreActiveThread(prev *state.Thread, g uint64) {
	if prev == nil {
		activeThreads.Delete(g)
		return
	}
	activeThreads.Store(g, prev)
}

// currentThread returns the Eval thread on the current goroutine, or
// nil if the goroutine is not currently inside Eval (in which case
// callPyFunction allocates a fresh Thread).
func currentThread() *state.Thread {
	g := goid()
	if v, ok := activeThreads.Load(g); ok {
		return v.(*state.Thread)
	}
	return nil
}

func init() {
	objects.FunctionType.Call = callPyFunction
	objects.FunctionType.Vectorcall = pyFunctionVectorcall
	// add_operators installs __call__ as a slot wrapper for every type
	// with tp_call. FunctionType.Call lands here (not in objects/) to
	// avoid an objects -> vm import cycle, so the slot wrapper has to
	// be installed at the same point.
	//
	// CPython: Objects/typeobject.c wrap_call (slotdefs row for tp_call)
	objects.AddCallSlotWrapper(objects.FunctionType)
	// Expose the current Python frame to objects/ for super() zero-arg.
	// The frame stack lives on the active thread's threadVM; this hook
	// avoids an objects -> vm import edge.
	//
	// CPython: pycore_frame.h _PyThreadState_GetFrame is the same shape.
	objects.CurrentFrameHook = currentInterpreterFrame
	// Expose the same hook to module/sys for sys._getframe().
	//
	// CPython: Python/sysmodule.c:1180 sys__getframe_impl
	sys.CurrentInterpreterFrameHook = currentInterpreterFrame
	// Let seqIterNext clear the thread-state exception when it catches
	// PyExc_IndexError, matching the PyErr_Clear in
	// Objects/iterobject.c:78 iter_iternext.
	objects.ClearCurrentExceptionHook = func() {
		ts := currentThread()
		if ts == nil {
			return
		}
		ts.SetException(nil)
	}
	// Let genFinalize save and restore the active thread's pending
	// exception across Close(). Mirrors CPython's PyErr_GetRaisedException
	// / PyErr_SetRaisedException sandwich around gen_close in
	// _PyGen_Finalize: a finalize that fires during normal evaluation
	// must not let the body's GeneratorExit leak into the caller's slot.
	//
	// CPython: Objects/genobject.c:98 _PyGen_Finalize
	objects.SaveCurrentExceptionHook = func() any {
		ts := currentThread()
		if ts == nil {
			return nil
		}
		return ts.SwapException(nil)
	}
	objects.RestoreCurrentExceptionHook = func(saved any) {
		ts := currentThread()
		if ts == nil {
			return
		}
		exc, _ := saved.(state.Exception)
		ts.SetException(exc)
	}
	// Let typeSetNames attach a __notes__ string to the live exception
	// when __set_name__ raises during class creation, matching
	// _PyErr_FormatNote inside type_new_set_names. When the hook fires
	// before the Go error has been promoted to a typed Exception (e.g.
	// arg-binding TypeError that never reached thread state) the note
	// goes onto a per-goroutine queue that handleException drains.
	//
	// CPython: Python/errors.c:1567 _PyErr_FormatNote
	// CPython: Objects/typeobject.c:11538 type_new_set_names
	objects.FormatNoteHook = func(note string) {
		ts := currentThread()
		if ts == nil {
			return
		}
		exc := pyerrors.Occurred(ts)
		if exc == nil {
			enqueuePendingNote(note)
			return
		}
		if exc.Notes == nil {
			exc.Notes = objects.NewList(nil)
		}
		exc.Notes.Append(objects.NewStr(note))
	}
}

// pendingNotes carries __notes__ strings that FormatNoteHook produced
// before a typed Exception existed on the thread state. handleException
// drains the per-goroutine queue when it synthesizes the Exception so
// the note rides along with the materialized object. Keyed by goid for
// the same reason as activeThreads.
var pendingNotes sync.Map // map[uint64][]string

func enqueuePendingNote(note string) {
	g := goid()
	var list []string
	if v, ok := pendingNotes.Load(g); ok {
		list = v.([]string)
	}
	list = append(list, note)
	pendingNotes.Store(g, list)
}

// drainPendingNotes removes and returns every queued note for the
// current goroutine. handleException calls this once the typed
// Exception is on the thread state so the notes attach in source order.
func drainPendingNotes() []string {
	g := goid()
	v, ok := pendingNotes.LoadAndDelete(g)
	if !ok {
		return nil
	}
	return v.([]string)
}

// currentInterpreterFrame returns the currently executing frame on this
// goroutine. For regular code the frame is on the chunk-based FrameStack.
// For generator bodies the frame is in activeEvalFrames, but when the
// generator body has called a Python function via callPyFunction the
// FrameStack grows beyond genEntryDepths and the stack top is the
// innermost frame.
//
// CPython: pycore_frame.h _PyThreadState_GetFrame
func currentInterpreterFrame() objects.InterpreterFrame {
	g := goid()
	if v, ok := activeEvalFrames.Load(g); ok {
		// Running on a generator goroutine. Check whether callPyFunction
		// pushed frames after the generator's entry point.
		ts := currentThread()
		if ts != nil {
			if d, ok2 := genEntryDepths.Load(g); ok2 {
				if frameStackFor(ts).Depth() > d.(int) {
					// callPyFunction pushed at least one frame; return it.
					return frameStackFor(ts).Top()
				}
			}
		}
		return v.(*frame.Frame)
	}
	ts := currentThread()
	if ts == nil {
		return nil
	}
	f := frameStackFor(ts).Top()
	if f == nil {
		return nil
	}
	return f
}

// callPyFunction pushes a frame for the function's code, binds args
// into fast-locals, and runs the eval loop. Mirrors CPython's
// initialize_locals: positional bind, *args pack, kw-only bind,
// **kwargs collect, defaults, kw-defaults, missing-arg check.
//
// CPython: Objects/call.c _PyEval_Vector
//
//nolint:gocognit,gocyclo // matches CPython's initialize_locals; splitting the steps out hides the linear flow without removing branches.
func callPyFunction(o objects.Object, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	fn := o.(*objects.Function)
	co := fn.Code
	if co == nil {
		return nil, fmt.Errorf("TypeError: function %q has no code", fn.Name)
	}
	npos := co.Argcount
	nkwonly := co.KwonlyArgcount
	nposonly := co.PosonlyArgcount
	hasVarargs := co.Flags&int(0x04) != 0
	hasVarkw := co.Flags&int(0x08) != 0
	qualname := fn.Qualname
	if qualname == "" {
		qualname = fn.Name
	}
	ts := currentThread()
	if ts == nil {
		ts = state.NewThread()
	}
	stack := frameStackFor(ts)
	// CPython: Python/ceval.c:748 _Py_EnterRecursiveCallTstate
	if stack.Depth() >= sys.RecursionLimit() {
		return nil, fmt.Errorf("RecursionError: maximum recursion depth exceeded")
	}
	f := stack.Push(co, fn.Globals, fn.Builtins, fn)
	defer stack.Pop()

	// Positional bind: first npos args go into slots [0..npos).
	bound := len(args)
	if bound > npos {
		bound = npos
	}
	for i := 0; i < bound; i++ {
		f.SetLocal(i, stackref.FromObjectNew(args[i]))
	}
	// *args: pack any extra positionals into a tuple at the varargs slot.
	if hasVarargs {
		extra := args[bound:]
		items := make([]objects.Object, len(extra))
		copy(items, extra)
		f.SetLocal(npos, stackref.FromObject(objects.NewTuple(items)))
	}
	// **kwargs: collect unknown keyword args here. Allocate eagerly so
	// the slot is bound even when no keywords are passed.
	var kwSlot int
	var kwDict *objects.Dict
	if hasVarkw {
		kwSlot = npos + nkwonly
		if hasVarargs {
			kwSlot++
		}
		kwDict = objects.NewDict()
		f.SetLocal(kwSlot, stackref.FromObject(kwDict))
	}
	// Keyword bind: scan the positional + kw-only window for a name
	// match. Positional-only slots [0..nposonly) are not eligible for
	// keyword binding; collisions route to **kwargs when present or
	// surface as positional_only_passed_as_keyword. With *args, the
	// varargs slot sits at index npos so the kw-only slots shift to
	// [npos+1 .. npos+1+nkwonly).
	//
	// CPython: Python/ceval.c:1546 positional_only_passed_as_keyword
	// CPython: Objects/call.c _PyEval_BindArguments keyword loop
	kwWindow := npos + nkwonly
	if hasVarargs {
		kwWindow++
	}
	var posonlyAsKw []string
	for k, v := range kwargs {
		idx := -1
		for i := 0; i < kwWindow; i++ {
			if i < nposonly {
				continue
			}
			if i == npos && hasVarargs {
				continue
			}
			if co.Varnames[i] == k {
				idx = i
				break
			}
		}
		if idx < 0 {
			if hasVarkw {
				if err := kwDict.SetItem(objects.NewStr(k), v); err != nil {
					return nil, err
				}
				continue
			}
			if nposonly > 0 {
				collides := false
				for i := 0; i < nposonly; i++ {
					if co.Varnames[i] == k {
						posonlyAsKw = append(posonlyAsKw, k)
						collides = true
						break
					}
				}
				if collides {
					continue
				}
			}
			return nil, objects.UnexpectedKeywordError(qualname, k)
		}
		if !f.LocalAt(idx).IsNull() {
			return nil, objects.MultipleValuesForArgumentError(qualname, k)
		}
		f.SetLocal(idx, stackref.FromObject(v))
	}
	if len(posonlyAsKw) > 0 {
		ordered := make([]string, 0, len(posonlyAsKw))
		for i := 0; i < nposonly; i++ {
			name := co.Varnames[i]
			for _, k := range posonlyAsKw {
				if k == name {
					ordered = append(ordered, name)
					break
				}
			}
		}
		return nil, objects.PositionalOnlyAsKeywordError(qualname, ordered)
	}
	// Positional defaults fill any unbound tail of the positional slots.
	if fn.Defaults != nil {
		nDefaults := fn.Defaults.Len()
		for i := 0; i < nDefaults; i++ {
			slot := npos - nDefaults + i
			if slot < 0 {
				continue
			}
			if f.LocalAt(slot).IsNull() {
				f.SetLocal(slot, stackref.FromObject(fn.Defaults.Item(i)))
			}
		}
	}
	// Keyword-only defaults fill any unbound kw-only slots.
	if fn.KwDefaults != nil && nkwonly > 0 {
		base := npos
		if hasVarargs {
			base++
		}
		for i := 0; i < nkwonly; i++ {
			slot := base + i
			if !f.LocalAt(slot).IsNull() {
				continue
			}
			name := co.Varnames[slot]
			v, err := fn.KwDefaults.GetItem(objects.NewStr(name))
			if err == nil && v != nil {
				f.SetLocal(slot, stackref.FromObject(v))
			}
		}
	}
	// Verify every positional and kw-only slot is bound; if any
	// are missing, batch them into the CPython-shaped TypeError.
	// CPython: Python/ceval.c:1449 missing_arguments
	var missingPos []string
	for i := 0; i < npos; i++ {
		if f.LocalAt(i).IsNull() {
			missingPos = append(missingPos, co.Varnames[i])
		}
	}
	if len(missingPos) > 0 {
		return nil, objects.MissingArgumentsError(qualname, "positional", missingPos)
	}
	kwOnlyBase := npos
	if hasVarargs {
		kwOnlyBase++
	}
	var missingKw []string
	for i := 0; i < nkwonly; i++ {
		slot := kwOnlyBase + i
		if f.LocalAt(slot).IsNull() {
			missingKw = append(missingKw, co.Varnames[slot])
		}
	}
	if len(missingKw) > 0 {
		return nil, objects.MissingArgumentsError(qualname, "keyword-only", missingKw)
	}
	// Too-many-positional check happens after binding so kwonlyGiven
	// reflects how many keyword-only slots got bound by name (CPython
	// reads localsplus[argcount..argcount+kwonlyargcount] for this).
	//
	// CPython: Python/ceval.c:1487 too_many_positional
	if !hasVarargs && len(args) > npos {
		atMost := npos
		atLeast := npos
		if fn.Defaults != nil {
			atLeast -= fn.Defaults.Len()
		}
		if atLeast < 0 {
			atLeast = 0
		}
		kwonlyGiven := 0
		for i := 0; i < nkwonly; i++ {
			if !f.LocalAt(kwOnlyBase + i).IsNull() {
				name := co.Varnames[kwOnlyBase+i]
				if fn.KwDefaults != nil {
					if v, _ := fn.KwDefaults.GetItem(objects.NewStr(name)); v != nil {
						if _, inKwargs := kwargs[name]; !inKwargs {
							continue
						}
					}
				}
				kwonlyGiven++
			}
		}
		return nil, objects.TooManyPositionalError(qualname, len(args), atLeast, atMost, kwonlyGiven)
	}
	return Eval(ts, f)
}

// pyFunctionVectorcall is the PEP 590 vectorcall entry for Python functions.
// Unlike callPyFunction (which takes map[string]Object after dictToMap strips
// original key objects), this entry receives the raw kwnames tuple so
// non-string-equal keys (e.g. FakeStr that compares equal to a param name
// via __eq__) reach the frame as their original Python objects.
//
// The key differences from callPyFunction:
//   - kwnames matching uses Python RichCmpBool for non-Unicode keys so
//     FakeStr('id') == 'id' maps to the 'id' varname and triggers
//     "multiple values for argument" when the slot is already positionally bound.
//   - Unmatched kwnames whose keys are not plain str go into **kwargs as their
//     original Python objects (not a Str() repr), so _ast.py's __init__ sees
//     the real FakeStr and can emit the correct warning + TypeError.
//
// CPython: Objects/call.c:_PyEval_Vector (vectorcall keyword binding)
//
//nolint:gocognit // mirrors callPyFunction structure; same linear bind flow.
func pyFunctionVectorcall(o objects.Object, args []objects.Object, nargsf uint, kwnames *objects.Tuple) (objects.Object, error) {
	fn := o.(*objects.Function)
	co := fn.Code
	if co == nil {
		return nil, fmt.Errorf("TypeError: function %q has no code", fn.Name)
	}
	nposArgs := objects.VectorcallNargs(nargsf)
	posArgs := args[:nposArgs]
	npos := co.Argcount
	nkwonly := co.KwonlyArgcount
	nposonly := co.PosonlyArgcount
	hasVarargs := co.Flags&int(0x04) != 0
	hasVarkw := co.Flags&int(0x08) != 0
	qualname := fn.Qualname
	if qualname == "" {
		qualname = fn.Name
	}

	// Build a map[string]Object from the kwnames tuple for fast varname
	// lookup, plus an ordered key list so **kwargs insertion preserves
	// the caller's kwarg order.  Non-string-keyed entries go into
	// rawKwEntries; they are stored as original objects in **kwargs.
	var kwargs map[string]objects.Object
	var kwOrder []string          // str keys in kwnames insertion order
	var rawKwEntries []rawKwEntry // original-object keys for **kwargs
	if kwnames != nil && kwnames.Len() > 0 {
		nkw := kwnames.Len()
		kwargs = make(map[string]objects.Object, nkw)
		kwWindow := npos + nkwonly
		if hasVarargs {
			kwWindow++
		}
		for i := 0; i < nkw; i++ {
			kwname := kwnames.Item(i)
			val := args[nposArgs+i]
			// Fast path: plain Python str — use its string value directly.
			if u, ok := kwname.(*objects.Unicode); ok {
				k := u.Value()
				kwargs[k] = val
				kwOrder = append(kwOrder, k)
				continue
			}
			// Slow path: non-str key — scan varnames via Python equality
			// (CPython: _PyEval_BindArguments keyword loop falls back to
			// PyObject_RichCompareBool for non-exact-str keys). Skip
			// positional-only slots; they cannot be passed as keywords.
			//
			// CPython: Objects/call.c:_PyEval_BindArguments (non-unicode key path)
			matched := ""
			for j := 0; j < kwWindow; j++ {
				if j < nposonly {
					continue
				}
				if j == npos && hasVarargs {
					continue
				}
				eq, err := objects.RichCmpBool(kwname, objects.NewStr(co.Varnames[j]), objects.CompareEQ)
				if err != nil {
					return nil, err
				}
				if eq {
					matched = co.Varnames[j]
					break
				}
			}
			if matched != "" {
				kwargs[matched] = val
				kwOrder = append(kwOrder, matched)
			} else {
				// No varname match: route to **kwargs as original object, or
				// surface as unexpected-keyword if there's no **kwargs.
				if !hasVarkw {
					// Best-effort string repr for error message.
					ks, _ := objects.Str(kwname)
					return nil, objects.UnexpectedKeywordError(qualname, ks)
				}
				rawKwEntries = append(rawKwEntries, rawKwEntry{key: kwname, val: val})
			}
		}
	}

	// Always route through callPyFunctionRaw so kwOrder preserves the
	// caller's kwarg insertion order in the **kwargs dict.
	//
	// CPython: Objects/call.c:_PyEval_Vector (binding loop)
	return callPyFunctionRaw(fn, co, posArgs, kwargs, kwOrder, rawKwEntries, qualname)
}

// rawKwEntry carries one original-object kwarg key+value for **kwargs.
type rawKwEntry struct {
	key objects.Object
	val objects.Object
}

// callPyFunctionRaw is the canonical kwargs binding path for Python functions.
// kwOrder, when non-nil, gives the insertion order for kwargs keys; it is used
// to populate the **kwargs dict in caller order so fn(**od) preserves the
// mapping's iteration order (bpo-34320). rawEntries carries non-string kwarg
// keys that must be stored in **kwargs as original Python objects.
//
// CPython: Objects/call.c:_PyEval_Vector (same binding sequence)
//
//nolint:gocognit,gocyclo // same linear bind flow as callPyFunction; splitting loses clarity.
func callPyFunctionRaw(fn *objects.Function, co *objects.Code, posArgs []objects.Object, kwargs map[string]objects.Object, kwOrder []string, rawEntries []rawKwEntry, qualname string) (objects.Object, error) {
	npos := co.Argcount
	nkwonly := co.KwonlyArgcount
	nposonly := co.PosonlyArgcount
	hasVarargs := co.Flags&int(0x04) != 0
	hasVarkw := co.Flags&int(0x08) != 0
	ts := currentThread()
	if ts == nil {
		ts = state.NewThread()
	}
	stack := frameStackFor(ts)
	// CPython: Python/ceval.c:748 _Py_EnterRecursiveCallTstate
	// py_recursion_remaining counts down; gopy counts up via FrameStack.Depth.
	if stack.Depth() >= sys.RecursionLimit() {
		return nil, fmt.Errorf("RecursionError: maximum recursion depth exceeded")
	}
	f := stack.Push(co, fn.Globals, fn.Builtins, fn)
	defer stack.Pop()

	bound := len(posArgs)
	if bound > npos {
		bound = npos
	}
	for i := 0; i < bound; i++ {
		f.SetLocal(i, stackref.FromObjectNew(posArgs[i]))
	}
	if hasVarargs {
		extra := posArgs[bound:]
		items := make([]objects.Object, len(extra))
		copy(items, extra)
		f.SetLocal(npos, stackref.FromObject(objects.NewTuple(items)))
	}
	var kwSlot int
	var kwDict *objects.Dict
	if hasVarkw {
		kwSlot = npos + nkwonly
		if hasVarargs {
			kwSlot++
		}
		kwDict = objects.NewDict()
		f.SetLocal(kwSlot, stackref.FromObject(kwDict))
	}
	kwWindow := npos + nkwonly
	if hasVarargs {
		kwWindow++
	}
	// Iterate kwargs in kwOrder (preserving caller insertion order) when
	// available, otherwise fall back to Go map iteration. kwOrder is
	// always non-nil when called from pyFunctionVectorcall.
	//
	// CPython: Objects/call.c:_PyEval_BindArguments (keyword loop)
	var iterKeys []string
	if kwOrder != nil {
		iterKeys = kwOrder
	} else {
		iterKeys = make([]string, 0, len(kwargs))
		for k := range kwargs {
			iterKeys = append(iterKeys, k)
		}
	}
	var posonlyAsKw []string
	for _, k := range iterKeys {
		v := kwargs[k]
		idx := -1
		for i := 0; i < kwWindow; i++ {
			if i < nposonly {
				continue
			}
			if i == npos && hasVarargs {
				continue
			}
			if co.Varnames[i] == k {
				idx = i
				break
			}
		}
		if idx < 0 {
			if hasVarkw {
				if err := kwDict.SetItem(objects.NewStr(k), v); err != nil {
					return nil, err
				}
				continue
			}
			if nposonly > 0 {
				collides := false
				for i := 0; i < nposonly; i++ {
					if co.Varnames[i] == k {
						posonlyAsKw = append(posonlyAsKw, k)
						collides = true
						break
					}
				}
				if collides {
					continue
				}
			}
			return nil, objects.UnexpectedKeywordError(qualname, k)
		}
		if !f.LocalAt(idx).IsNull() {
			return nil, objects.MultipleValuesForArgumentError(qualname, k)
		}
		f.SetLocal(idx, stackref.FromObject(v))
	}
	// Inject raw-object entries directly into **kwargs dict.
	// These have already been determined to have no varname match.
	//
	// CPython: Objects/call.c:_PyEval_BindArguments (kwdict SetItem path)
	for _, e := range rawEntries {
		if err := kwDict.SetItem(e.key, e.val); err != nil {
			return nil, err
		}
	}
	if len(posonlyAsKw) > 0 {
		ordered := make([]string, 0, len(posonlyAsKw))
		for i := 0; i < nposonly; i++ {
			name := co.Varnames[i]
			for _, k := range posonlyAsKw {
				if k == name {
					ordered = append(ordered, name)
					break
				}
			}
		}
		return nil, objects.PositionalOnlyAsKeywordError(qualname, ordered)
	}
	if fn.Defaults != nil {
		nDefaults := fn.Defaults.Len()
		for i := 0; i < nDefaults; i++ {
			slot := npos - nDefaults + i
			if slot < 0 {
				continue
			}
			if f.LocalAt(slot).IsNull() {
				f.SetLocal(slot, stackref.FromObject(fn.Defaults.Item(i)))
			}
		}
	}
	if fn.KwDefaults != nil && nkwonly > 0 {
		base := npos
		if hasVarargs {
			base++
		}
		for i := 0; i < nkwonly; i++ {
			slot := base + i
			if !f.LocalAt(slot).IsNull() {
				continue
			}
			name := co.Varnames[slot]
			v, err := fn.KwDefaults.GetItem(objects.NewStr(name))
			if err == nil && v != nil {
				f.SetLocal(slot, stackref.FromObject(v))
			}
		}
	}
	var missingPos []string
	for i := 0; i < npos; i++ {
		if f.LocalAt(i).IsNull() {
			missingPos = append(missingPos, co.Varnames[i])
		}
	}
	if len(missingPos) > 0 {
		return nil, objects.MissingArgumentsError(qualname, "positional", missingPos)
	}
	kwOnlyBase := npos
	if hasVarargs {
		kwOnlyBase++
	}
	var missingKw []string
	for i := 0; i < nkwonly; i++ {
		slot := kwOnlyBase + i
		if f.LocalAt(slot).IsNull() {
			missingKw = append(missingKw, co.Varnames[slot])
		}
	}
	if len(missingKw) > 0 {
		return nil, objects.MissingArgumentsError(qualname, "keyword-only", missingKw)
	}
	if !hasVarargs && len(posArgs) > npos {
		atMost := npos
		atLeast := npos
		if fn.Defaults != nil {
			atLeast -= fn.Defaults.Len()
		}
		if atLeast < 0 {
			atLeast = 0
		}
		kwonlyGiven := 0
		kwOnlyBaseInner := npos
		if hasVarargs {
			kwOnlyBaseInner++
		}
		for i := 0; i < nkwonly; i++ {
			if !f.LocalAt(kwOnlyBaseInner + i).IsNull() {
				name := co.Varnames[kwOnlyBaseInner+i]
				if fn.KwDefaults != nil {
					if v, _ := fn.KwDefaults.GetItem(objects.NewStr(name)); v != nil {
						if _, inKwargs := kwargs[name]; !inKwargs {
							continue
						}
					}
				}
				kwonlyGiven++
			}
		}
		return nil, objects.TooManyPositionalError(qualname, len(posArgs), atLeast, atMost, kwonlyGiven)
	}
	return Eval(ts, f)
}
