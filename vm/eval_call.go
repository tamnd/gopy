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
}

// currentInterpreterFrame returns the top of the active thread's frame
// stack, or nil when no Python frame is live on this goroutine. Used by
// objects.superInitNoArgs to read __class__ and self off the caller.
//
// CPython: pycore_frame.h _PyThreadState_GetFrame
func currentInterpreterFrame() objects.InterpreterFrame {
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
	hasVarargs := co.Flags&int(0x04) != 0
	hasVarkw := co.Flags&int(0x08) != 0
	if !hasVarargs && len(args) > npos {
		return nil, fmt.Errorf("TypeError: %s() takes %d positional arguments but %d were given",
			fn.Name, npos, len(args))
	}
	ts := currentThread()
	if ts == nil {
		ts = state.NewThread()
	}
	stack := frameStackFor(ts)
	f := stack.Push(co, fn.Globals, fn.Builtins, fn, nil)
	defer stack.Pop()

	// Positional bind: first npos args go into slots [0..npos).
	bound := len(args)
	if bound > npos {
		bound = npos
	}
	for i := 0; i < bound; i++ {
		f.SetLocal(i, stackref.FromObject(args[i]))
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
	// match. With *args, the varargs slot sits at index npos so the
	// kw-only slots shift to [npos+1 .. npos+1+nkwonly).
	//
	// CPython: Objects/call.c:_PyEval_BindArguments keyword loop
	kwWindow := npos + nkwonly
	if hasVarargs {
		kwWindow++
	}
	for k, v := range kwargs {
		idx := -1
		for i := 0; i < kwWindow; i++ {
			if i == npos && hasVarargs {
				// The *args slot sits inside varnames at index npos but
				// is not eligible for keyword binding.
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
			return nil, fmt.Errorf("TypeError: %s() got an unexpected keyword argument %q", fn.Name, k)
		}
		if !f.LocalAt(idx).IsNull() {
			return nil, fmt.Errorf("TypeError: %s() got multiple values for argument %q", fn.Name, k)
		}
		f.SetLocal(idx, stackref.FromObject(v))
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
	// Verify every positional and kw-only slot is bound.
	for i := 0; i < npos; i++ {
		if f.LocalAt(i).IsNull() {
			return nil, fmt.Errorf("TypeError: %s() missing required argument %q", fn.Name, co.Varnames[i])
		}
	}
	kwOnlyBase := npos
	if hasVarargs {
		kwOnlyBase++
	}
	for i := 0; i < nkwonly; i++ {
		slot := kwOnlyBase + i
		if f.LocalAt(slot).IsNull() {
			return nil, fmt.Errorf("TypeError: %s() missing required keyword-only argument %q", fn.Name, co.Varnames[slot])
		}
	}
	return Eval(ts, f)
}
