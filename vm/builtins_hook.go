// vm-side wiring for builtins.globals() and builtins.locals(). The
// builtin needs to read the running frame, but builtins doesn't import
// vm or frame. This file installs a hook from the vm side so the
// dependency arrow points the right way: builtins -> objects, vm ->
// builtins.

package vm

import (
	"errors"
	"fmt"
	"os"

	"github.com/tamnd/gopy/builtins"
	pyerrors "github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/frame"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
	"github.com/tamnd/gopy/traceback"
)

func init() {
	builtins.SetCurrentScope(currentScope)
	builtins.SetImporter(currentImporter)
	builtins.SetEvaluator(currentEvaluator)
	objects.GenThrowHook = genThrowHook
	objects.GenThrowTripleHook = genThrowTripleHook
	objects.GenThrowForwardHook = genThrowForwardHook
	objects.GenAttachFrameTBHook = genAttachFrameTBHook
	objects.WriteUnraisableHook = writeUnraisable
}

// writeUnraisable routes an exception that cannot be propagated through
// sys.unraisablehook. The hook is invoked from objects/ at sites that
// cannot let the exception escape (slot_tp_finalize, gen_close inside
// gen_finalize, weakref callbacks). It builds an UnraisableHookArgs
// SimpleNamespace, calls sys.unraisablehook(args), and clears any
// exception the hook itself raised so the caller can press on.
//
// CPython: Python/errors.c:1380 _PyErr_WriteUnraisable
// CPython: Python/sysmodule.c:893 sys_unraisablehook
func writeUnraisable(obj objects.Object, errMsg string, errFromCaller error) {
	ts := currentThread()
	if ts == nil {
		ts = state.NewThread()
	}
	// Prefer the thread state's live exception. The eval-loop unwind
	// attaches a TB to the exception sitting on tstate via
	// PyTraceBack_Here; the bare Go error the caller hands us is just
	// a fmt.Errorf sentinel that doesn't carry that TB. Use the live
	// exception when its identity matches the error so exc_traceback
	// reaches the hook.
	//
	// CPython: Python/errors.c:1380 _PyErr_WriteUnraisable reads the
	// active exception via _PyErr_GetRaisedException before formatting.
	var exc *pyerrors.Exception
	if live := pyerrors.Occurred(ts); live != nil {
		if liveMatchesErr(live, errFromCaller) {
			exc = live
		}
	}
	if exc == nil {
		exc = synthesizeException(errFromCaller)
	}
	if exc == nil {
		return
	}
	args := objects.NewNamespace()
	d := args.Dict()
	excType := objects.None()
	if exc.ExcType != nil {
		excType = exc.ExcType
	}
	_ = d.SetItem(objects.NewStr("exc_type"), excType)
	_ = d.SetItem(objects.NewStr("exc_value"), exc)
	// Fall back to a current-frame traceback when the exception has none,
	// matching _PyErr_WriteUnraisableMsg's PyThreadState_GetFrame branch.
	// Tests like test_generators coroutine doctest check that
	// cm.unraisable.exc_traceback is not None on a gen_close-driven
	// "generator ignored GeneratorExit" raise, which is set without going
	// through the eval loop's PyTraceBack_Here.
	//
	// CPython: Python/errors.c:1380 _PyErr_WriteUnraisableMsg
	// (the exc_tb == Py_None branch that calls _PyTraceBack_FromFrame)
	if exc.TB == nil {
		if tb := tracebackFromCurrentFrame(); tb != nil {
			exc.TB = tb
		}
	}
	tbObj := objects.None()
	if exc.TB != nil {
		tbObj = exc.TB
	}
	_ = d.SetItem(objects.NewStr("exc_traceback"), tbObj)
	_ = d.SetItem(objects.NewStr("err_msg"), objects.NewStr(errMsg))
	objAttr := objects.None()
	if obj != nil {
		objAttr = obj
	}
	_ = d.SetItem(objects.NewStr("object"), objAttr)

	sysMod, ok := imp.GetModule("sys")
	if !ok {
		return
	}
	hook, err := sysMod.Dict().GetItem(objects.NewStr("unraisablehook"))
	if err != nil || hook == nil || hook == objects.None() {
		return
	}
	// Save the caller's pending exception so the hook's own raise does
	// not leak into the surrounding tstate slot.
	saved := ts.SwapException(nil)
	_, hookErr := objects.Call(hook, objects.NewTuple([]objects.Object{args}), nil)
	if hookErr != nil {
		// A hook that itself errors is unrecoverable. CPython falls back
		// to writing "Exception ignored in sys.unraisablehook" to stderr;
		// gopy mirrors that by writing a short notice and swallowing.
		fmt.Fprintln(os.Stderr, "Exception ignored in sys.unraisablehook:")
		fmt.Fprintln(os.Stderr, hookErr.Error())
	}
	ts.SetException(saved)
}

// liveMatchesErr reports whether the exception sitting on the thread
// state corresponds to err. excSentinel returns a bare fmt.Errorf with
// "TypeName: msg" (or "TypeName" when msg is empty); a RaisedError
// carries the exception directly. We accept either shape so the live
// exception (with its TB chain) is preferred over the fmt.Errorf
// shadow.
//
// CPython: Python/errors.c equivalent of _PyErr_GetRaisedException
// (the active exception always corresponds to the propagating error;
// the C code carries it on the tstate slot rather than the return code).
func liveMatchesErr(live *pyerrors.Exception, err error) bool {
	if live == nil || err == nil {
		return false
	}
	var re *objects.RaisedError
	if errors.As(err, &re) {
		return re.Exc == live
	}
	msg := err.Error()
	if rest, ok := stringsCutPrefix(msg, "vm: "); ok {
		msg = rest
	}
	tn := live.TypeName()
	lm := live.Message()
	if lm == "" {
		return msg == tn
	}
	return msg == tn+": "+lm
}

// tracebackFromCurrentFrame walks the active interpreter frame chain and
// builds a traceback whose head is the innermost frame. Used by
// writeUnraisable when a synthesized exception lacks a TB (e.g.,
// gen_close raises RuntimeError directly via PyErr_SetString without the
// eval loop attaching one). Returns nil when no frame is active.
//
// CPython: Python/traceback.c:985 _PyTraceBack_FromFrame
func tracebackFromCurrentFrame() *traceback.Traceback {
	ip := currentInterpreterFrame()
	if ip == nil {
		return nil
	}
	co := ip.FrameCode()
	if co == nil {
		return nil
	}
	line := co.Firstlineno
	if line <= 0 {
		line = 1
	}
	if pos, ok := objects.CoAddr2Location(co, ip.FrameLasti()); ok && pos.Line > 0 {
		line = pos.Line
	}
	name := co.Name
	if co.Qualname != "" {
		name = co.Qualname
	}
	entry := traceback.Entry{File: co.Filename, Line: line, Name: name}
	tb := &traceback.Traceback{
		Entry:   entry,
		TbFrame: objects.NewFrame(ip),
	}
	tb.Init(traceback.Type)
	return tb
}

// stringsCutPrefix matches strings.CutPrefix. Imported on demand to
// avoid threading the import through this file's existing imports.
func stringsCutPrefix(s, prefix string) (string, bool) {
	if len(s) < len(prefix) || s[:len(prefix)] != prefix {
		return s, false
	}
	return s[len(prefix):], true
}

// genAttachFrameTBHook prepends a traceback entry for the generator
// body's code object to the exception carried by err. Throw on an
// unstarted generator skips body execution (the goroutine has not yet
// reached its first SendCh receive in a state that can absorb a throw),
// so the body frame would never appear in the tb. CPython runs the
// body which raises at the body entry point and adds the body frame
// via PyTraceBack_Here. We mirror the user-visible result by adding
// the entry directly.
//
// CPython: Objects/genobject.c:466 _gen_throw (gen_send_ex which calls
// PyEval_EvalFrame; body propagates with PyTraceBack_Here entries)
func genAttachFrameTBHook(g *objects.Generator, err error) error {
	if err == nil {
		return nil
	}
	var re *objects.RaisedError
	var exc *pyerrors.Exception
	if errors.As(err, &re) && re.Exc != nil {
		if e, ok := re.Exc.(*pyerrors.Exception); ok {
			exc = e
		}
	}
	if exc == nil {
		return err
	}
	co, ok := g.Code.(*objects.Code)
	if !ok || co == nil {
		return err
	}
	line := co.Firstlineno
	if line <= 0 {
		line = 1
	}
	name := co.Name
	if co.Qualname != "" {
		name = co.Qualname
	}
	// Snapshot the body frame so the tb does not pin the live interp
	// frame: when the goroutine exits and the frame is recycled by the
	// chunk arena, any tb that still referenced the live frame would
	// observe state from whatever code object next claims the slot.
	//
	// CPython: PyFrameObject is refcounted, so traceback entries can
	// hold real frame references. gopy recycles interp frames, so we
	// copy out the bits traceback.py actually reads (code, globals,
	// lasti).
	_ = frame.OwnedByGenerator
	var tbFrame objects.Object
	if fr, ok := g.GiFrame.(*objects.Frame); ok {
		if ip := fr.Interp(); ip != nil {
			if rawFrame, ok2 := ip.(*frame.Frame); ok2 {
				snap := objects.SnapshotFrame(rawFrame)
				tbFrame = objects.NewFrame(snap)
			}
		}
	}
	if tbFrame == nil {
		// Fall back to the live frame wrapper. The body has not been
		// recycled yet because Throw runs synchronously with the
		// caller; the wrapper is safe to attach as long as the caller
		// reads the tb before the generator object is collected.
		tbFrame = g.GiFrame
	}
	entry := traceback.Entry{File: co.Filename, Line: line, Name: name}
	tb := &traceback.Traceback{Entry: entry, Next: exc.TB, TbFrame: tbFrame}
	tb.Init(traceback.Type)
	exc.TB = tb
	return err
}

// genThrowHook validates the argument passed to generator.throw(exc),
// instantiates an exception class with no args, and wraps the resulting
// Python exception object in objects.RaisedError so the generator's
// YIELD_VALUE handler can install it on the thread state. The single-arg
// signature mirrors CPython 3.14's preferred form.
//
// CPython: Objects/genobject.c:599 gen_throw / :466 _gen_throw (throw_here
// branch handling the PyExceptionClass_Check and PyExceptionInstance_Check
// cases)
func genThrowHook(arg objects.Object) (error, error) {
	switch v := arg.(type) {
	case *pyerrors.Exception:
		msg := v.TypeName()
		if m := v.Message(); m != "" {
			msg = msg + ": " + m
		}
		return objects.NewRaisedError(v, msg), nil
	case *objects.Type:
		if !pyerrors.IsSubtype(v, pyerrors.PyExc_BaseException) {
			return nil, fmt.Errorf(
				"TypeError: exceptions must be classes or instances deriving from BaseException, not %s",
				arg.Type().Name)
		}
		// Instantiate via the Python constructor so __new__/__init__ run.
		// CPython: Objects/genobject.c:557 throw_here (PyErr_NormalizeException path
		// that calls __new__ and checks for a BaseException instance). When __new__
		// returns a non-instance, PyErr_NormalizeException sets a TypeError and
		// gen_send_ex still throws that TypeError into the generator (which closes it).
		// We mirror that: convert any instantiation failure to a throwable RaisedError
		// so g.Throw runs and the generator is marked closed.
		inst, callErr := objects.Call(v, objects.NewTuple(nil), nil)
		if callErr != nil {
			// Call failed outright: propagate as a prep error (generator not entered).
			return nil, callErr
		}
		exc, ok := inst.(*pyerrors.Exception)
		if !ok {
			// __new__ returned a non-instance. CPython raises TypeError and still
			// throws it into the generator body so the generator closes.
			msg := fmt.Sprintf("TypeError: calling %s should have returned an instance of BaseException, not %s",
				v.Name, inst.Type().Name)
			typeErr := pyerrors.New(pyerrors.PyExc_TypeError,
				objects.NewTuple([]objects.Object{objects.NewStr(
					fmt.Sprintf("calling %s should have returned an instance of BaseException, not %s",
						v.Name, inst.Type().Name))}))
			return objects.NewRaisedError(typeErr, msg), nil
		}
		msg := exc.TypeName()
		if m := exc.Message(); m != "" {
			msg = msg + ": " + m
		}
		return objects.NewRaisedError(exc, msg), nil
	default:
		return nil, fmt.Errorf(
			"TypeError: exceptions must be classes or instances deriving from BaseException, not %s",
			arg.Type().Name)
	}
}

// genThrowTripleHook ports the (typ, val, tb) normalization that
// CPython's _gen_throw runs on the deprecated 3-arg form. It validates
// the traceback argument, then funnels typ/val through
// PyErr_NormalizeException so the generator receives a fully
// instantiated exception.
//
// CPython: Objects/genobject.c:541 _gen_throw (throw_here block)
// CPython: Python/errors.c:389 _PyErr_NormalizeException
// CPython: Python/errors.c:33 _PyErr_CreateException
func genThrowTripleHook(typ, val, tb objects.Object) (error, error) {
	// CPython: Objects/genobject.c:544 (tb == Py_None -> NULL; else
	// PyTraceBack_Check)
	if tb != nil && tb != objects.None() {
		if _, ok := tb.(*traceback.Traceback); !ok {
			return nil, fmt.Errorf("TypeError: throw() third argument must be a traceback object")
		}
	}

	if val == objects.None() {
		val = nil
	}

	switch v := typ.(type) {
	case *objects.Type:
		// CPython: Objects/genobject.c:557 (PyExceptionClass_Check ->
		// PyErr_NormalizeException). The class must derive from
		// BaseException; otherwise PyExceptionClass_Check is false and
		// we fall through to the "exceptions must be classes" branch.
		if !pyerrors.IsSubtype(v, pyerrors.PyExc_BaseException) {
			return nil, fmt.Errorf(
				"TypeError: exceptions must be classes or instances deriving from BaseException, not %s",
				typ.Type().Name)
		}
		return normalizeExceptionTriple(v, val)
	case *pyerrors.Exception:
		// CPython: Objects/genobject.c:560 (PyExceptionInstance_Check
		// branch). val must be absent / None, else TypeError.
		if val != nil {
			return nil, fmt.Errorf("TypeError: instance exception may not have a separate value")
		}
		msg := v.TypeName()
		if m := v.Message(); m != "" {
			msg = msg + ": " + m
		}
		return objects.NewRaisedError(v, msg), nil
	default:
		// CPython: Objects/genobject.c:577 (else branch -> PyErr_Format)
		return nil, fmt.Errorf(
			"TypeError: exceptions must be classes or instances deriving from BaseException, not %s",
			typ.Type().Name)
	}
}

// normalizeExceptionTriple mirrors PyErr_NormalizeException when the
// first slot is an exception class. If val is already an instance of
// (a subclass of) cls it is used as-is; otherwise _PyErr_CreateException
// instantiates cls with val.
//
// CPython: Python/errors.c:415 (PyExceptionClass_Check branch)
// CPython: Python/errors.c:33 _PyErr_CreateException
func normalizeExceptionTriple(cls *objects.Type, val objects.Object) (error, error) {
	if val != nil {
		if exc, ok := val.(*pyerrors.Exception); ok {
			if pyerrors.IsSubtype(exc.ExcType, cls) {
				msg := exc.TypeName()
				if m := exc.Message(); m != "" {
					msg = msg + ": " + m
				}
				return objects.NewRaisedError(exc, msg), nil
			}
		}
	}
	// _PyErr_CreateException: val nil/None -> cls(), tuple -> cls(*val), else -> cls(val).
	// CPython: Python/errors.c:33-45
	var callArgs *objects.Tuple
	switch v := val.(type) {
	case nil:
		callArgs = objects.NewTuple(nil)
	case *objects.Tuple:
		callArgs = v
	default:
		callArgs = objects.NewTuple([]objects.Object{val})
	}
	inst, callErr := objects.Call(cls, callArgs, nil)
	if callErr != nil {
		return nil, callErr
	}
	exc, ok := inst.(*pyerrors.Exception)
	if !ok {
		// _PyErr_CreateException sets TypeError and returns NULL. CPython
		// then throws that TypeError into the generator.
		// CPython: Python/errors.c:47
		msg := fmt.Sprintf("calling %s should have returned an instance of BaseException, not %s",
			cls.Name, inst.Type().Name)
		typeErr := pyerrors.New(pyerrors.PyExc_TypeError,
			objects.NewTuple([]objects.Object{objects.NewStr(msg)}))
		return objects.NewRaisedError(typeErr, "TypeError: "+msg), nil
	}
	msg := exc.TypeName()
	if m := exc.Message(); m != "" {
		msg = msg + ": " + m
	}
	return objects.NewRaisedError(exc, msg), nil
}

// genThrowForwardHook forwards a throw to a custom (non-Generator) yield-from
// sub-iterator by calling yf.throw(exc). Before the call it temporarily
// installs gen's own frame as the "current frame" on this goroutine so
// that sys._getframe() inside yf.throw() sees gen's frame as f_back.
// This mirrors CPython's frame->previous = prev; tstate->current_frame = frame
// in _gen_throw for the custom-iterator path.
// Returns (val, nil) when yf.throw() yields a value, (nil, err) when it raises.
//
// CPython: Objects/genobject.c:504 _gen_throw (custom iterator throw path,
// the PyObject_LookupAttr("throw") + call branch)
// CPython: Objects/genobject.c:523 _gen_throw (tstate->current_frame = frame)
func genThrowForwardHook(genObj objects.Object, yf objects.Object, err error) (objects.Object, error) {
	// Extract the Python exception object from the Go error.
	var excObj objects.Object
	var re *objects.RaisedError
	switch {
	case errors.As(err, &re) && re.Exc != nil:
		excObj = re.Exc
	case errors.Is(err, objects.ErrGeneratorExit):
		excObj = pyerrors.New(pyerrors.PyExc_GeneratorExit, objects.NewTuple(nil))
	default:
		excObj = pyerrors.New(pyerrors.PyExc_RuntimeError,
			objects.NewTuple([]objects.Object{objects.NewStr(err.Error())}))
	}

	// Look up yf.throw; if absent, skip forwarding.
	throwAttr, lookErr := objects.GetAttr(yf, objects.NewStr("throw"))
	if lookErr != nil {
		return nil, err
	}

	// Make gen's frame the "previous frame" for the next callPyFunction
	// Push so that throw's frame gets f_back = gen's frame. Also set
	// gen's frame's Previous to the current FrameStack top so the chain
	// is: caller_frame -> gen's frame -> throw's frame.
	// Mirrors CPython: frame->previous = prev; tstate->current_frame = frame
	// in _gen_throw before calling yf.throw().
	//
	// CPython: Objects/genobject.c:523 _gen_throw
	if gen, ok := genObj.(*objects.Generator); ok {
		if fv, ok2 := genOwnFrames.Load(gen); ok2 {
			genFrame := fv.(*frame.Frame)
			ts := currentThread()
			if ts != nil {
				stack := frameStackFor(ts)
				prevTop := stack.Top()
				genFrame.Previous = prevTop
				stack.ForcedPrev = genFrame
				defer func() {
					// CPython: tstate->current_frame = prev; frame->previous = NULL
					genFrame.Previous = nil
					stack.ForcedPrev = nil
				}()
			}
		}
	}

	result, callErr := objects.Call(throwAttr, objects.NewTuple([]objects.Object{excObj}), nil)
	if callErr != nil {
		return nil, callErr
	}
	return result, nil
}

// currentScope yields the running frame's (globals, locals). The
// locals branch matches PyEval_GetFrameLocals: if the frame has an
// explicit f_locals dict (set by EvalCode for module / class bodies)
// it is returned as-is; otherwise the fast locals are materialized
// into a fresh dict via FrameFastToLocals.
//
// CPython: Python/bltinmodule.c:1267 builtin_globals_impl
// CPython: Python/bltinmodule.c:1933 builtin_locals_impl
func currentScope() (objects.Object, objects.Object) {
	ip := currentInterpreterFrame()
	if ip == nil {
		return nil, nil
	}
	f, ok := ip.(*frame.Frame)
	if !ok {
		return ip.FrameGlobals(), ip.FrameLocals()
	}
	if f.Locals != nil {
		return f.Globals, f.Locals
	}
	d, err := objects.FrameFastToLocals(objects.NewFrame(f))
	if err != nil {
		return f.Globals, nil
	}
	return f.Globals, d
}

// currentEvaluator is the hook builtins.eval and builtins.exec
// dispatch through. It pulls the active state.Thread off the goroutine
// (or allocates a fresh one when called outside any frame, e.g. from a
// stand-alone program that imports gopy as a library) and calls
// EvalCode against the supplied globals/locals.
//
// CPython: equivalent of PyEval_EvalCode (Python/ceval.c)
func currentEvaluator(code *objects.Code, globals, locals objects.Object) (objects.Object, error) {
	ts := currentThread()
	if ts == nil {
		ts = state.NewThread()
	}
	// builtin_exec_impl auto-injects __builtins__ into a user-supplied
	// globals dict so name lookups still find UnboundLocalError /
	// AttributeError / Exception even when the caller passed a fresh
	// dict like exec(src, {"fail": fail}).
	//
	// CPython: Python/bltinmodule.c:1081 builtin_exec_impl
	// (PyDict_GetItemWithError + PyDict_SetItem(__builtins__))
	if d, ok := globals.(*objects.Dict); ok {
		key := objects.NewStr("__builtins__")
		if v, _ := d.GetItem(key); v == nil {
			if f := frameStackFor(ts).Top(); f != nil && f.Builtins != nil {
				_ = d.SetItem(key, f.Builtins)
			}
		}
	}
	return EvalCode(ts, code, globals, locals)
}

// currentImporter is the hook builtins.__import__ delegates to. It
// reuses vmExecutor so the import can run frozen / built-in module
// init code, then forwards to imp.ImportModuleLevel. fromlist is
// accepted for signature parity; the existing IMPORT_NAME arm
// likewise drops it pending fromlist-driven submodule discovery.
//
// CPython: Python/import.c:1561 PyImport_ImportModuleLevelObject
func currentImporter(name, pkgname string, level int, _ []string) (objects.Object, error) {
	ts := currentThread()
	if ts == nil {
		ts = state.NewThread()
	}
	// Inherit builtins from the running frame so imported modules see
	// __build_class__ without a separate propagation step.
	//
	// CPython: Python/import.c:1759 import_name reads interp->builtins_module.
	var b objects.Object
	if f := frameStackFor(ts).Top(); f != nil {
		b = callerBuiltins(f)
	}
	exec := &vmExecutor{ts: ts, builtins: b}
	mod, err := imp.ImportModuleLevel(exec, name, pkgname, level)
	if err != nil {
		return nil, err
	}
	return mod, nil
}
