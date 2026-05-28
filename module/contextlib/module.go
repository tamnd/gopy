// contextlib module: Go port of Lib/contextlib.py. Provides
// @contextmanager wrapping generator functions into context managers,
// plus the supporting classes ExitStack, suppress, nullcontext,
// closing, redirect_stdout, redirect_stderr, and the abstract base
// classes. Async surface (asynccontextmanager, AsyncExitStack,
// aclosing) ships as a no-op alias of the sync surface for now;
// gopy's async runtime does not yet drive these.
//
// CPython: Lib/contextlib.py:276 contextmanager
// CPython: Lib/contextlib.py:105 _GeneratorContextManagerBase
// CPython: Lib/contextlib.py:129 _GeneratorContextManager
// CPython: Lib/contextlib.py:342 closing
// CPython: Lib/contextlib.py:393 _RedirectStream
// CPython: Lib/contextlib.py:433 suppress
// CPython: Lib/contextlib.py:472 _BaseExitStack
// CPython: Lib/contextlib.py:557 ExitStack
// CPython: Lib/contextlib.py:716 nullcontext

package contextlib

import (
	"errors"
	"fmt"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("contextlib", buildModule)
}

func buildModule() (*objects.Module, error) {
	m := objects.NewModule("contextlib")
	d := m.Dict()
	entries := []struct {
		name string
		val  objects.Object
	}{
		{"contextmanager", objects.NewBuiltinFunction("contextmanager", contextManager)},
		{"asynccontextmanager", objects.NewBuiltinFunction("asynccontextmanager", contextManager)},
		{"suppress", suppressType},
		{"nullcontext", nullcontextType},
		{"ExitStack", exitStackType},
		{"AsyncExitStack", exitStackType},
		{"closing", closingType},
		{"aclosing", closingType},
		{"redirect_stdout", redirectStdoutType},
		{"redirect_stderr", redirectStderrType},
		{"AbstractContextManager", abstractCMType},
		{"AbstractAsyncContextManager", abstractCMType},
		{"ContextDecorator", contextDecoratorType},
		{"AsyncContextDecorator", contextDecoratorType},
		{"_GeneratorContextManager", genCMType},
		{"_GeneratorContextManagerBase", abstractCMType},
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// abstractCMType is the AbstractContextManager / AbstractAsyncContextManager
// shared abstract base. CPython defines two; for gopy we expose one Type
// that any concrete CM can list as a base.
//
// CPython: Lib/contextlib.py:35 AbstractContextManager
var abstractCMType = objects.NewType("AbstractContextManager", []*objects.Type{objects.ObjectType()})

// contextDecoratorType backs ContextDecorator / AsyncContextDecorator.
// The CPython class only adds a __call__ that wraps a function so it
// re-enters the context on every call; unittest does not exercise that
// path. The type is a no-op subclass of object so isinstance checks
// succeed.
//
// CPython: Lib/contextlib.py:73 ContextDecorator
var contextDecoratorType = objects.NewType("ContextDecorator", []*objects.Type{objects.ObjectType()})

// genCMType is _GeneratorContextManager: the class @contextmanager
// builds an instance of every time the decorated function is called.
// Each instance owns a generator suspended at its first yield. __enter__
// drives the generator to that yield and returns the yielded value;
// __exit__ resumes the generator, optionally throwing in the active
// exception, and translates the generator's response back into the
// context-manager protocol.
//
// CPython: Lib/contextlib.py:129 _GeneratorContextManager
var genCMType = newGenCMType()

func newGenCMType() *objects.Type {
	t := objects.NewType("_GeneratorContextManager", []*objects.Type{abstractCMType})
	t.HasDict = true
	t.Getattro = objects.GenericGetAttr
	objects.SetTypeDescr(t, "__init__", objects.NewMethodDescr(t, "__init__", genCMInit))
	objects.SetTypeDescr(t, "__enter__", objects.NewMethodDescr(t, "__enter__", genCMEnter))
	objects.SetTypeDescr(t, "__exit__", objects.NewMethodDescr(t, "__exit__", genCMExit))
	objects.SetTypeDescr(t, "__call__", objects.NewMethodDescr(t, "__call__", genCMCall))
	// Wire t.Call so instances are callable; SetTypeDescr alone doesn't
	// install the slot when the type is constructed outside the normal
	// Python class machinery.
	t.Call = func(self objects.Object, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
		all := append([]objects.Object{self}, args...)
		return genCMCall(all, kwargs)
	}
	return t
}

// genCMInit initializes a _GeneratorContextManager instance created by
// direct instantiation (e.g. MockContextManager(func, args, kwds)).
// Stores func/args/kwds for _recreate_cm and drives func(*args, **kwds)
// to obtain the generator.
//
// CPython: Lib/contextlib.py:105 _GeneratorContextManagerBase.__init__
func genCMInit(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 4 {
		return nil, fmt.Errorf("TypeError: __init__() takes 3 arguments (%d given)", len(args)-1)
	}
	inst, ok := args[0].(*objects.Instance)
	if !ok {
		return nil, fmt.Errorf("TypeError: __init__ self is not Instance")
	}
	fn := args[1]
	posArgs := args[2]
	kwdArg := args[3]

	var callArgs *objects.Tuple
	switch v := posArgs.(type) {
	case *objects.Tuple:
		callArgs = v
	case *objects.List:
		items := make([]objects.Object, v.Len())
		for i := range items {
			items[i] = v.Item(i)
		}
		callArgs = objects.NewTuple(items)
	default:
		callArgs = objects.NewTuple(nil)
	}

	var callKwargs *objects.Dict
	if d, ok := kwdArg.(*objects.Dict); ok {
		callKwargs = d
	}

	genObj, err := objects.Call(fn, callArgs, callKwargs)
	if err != nil {
		return nil, err
	}

	d := inst.EnsureDict()
	_ = d.SetItem(objects.NewStr("gen"), genObj)
	_ = d.SetItem(objects.NewStr("func"), fn)
	_ = d.SetItem(objects.NewStr("args"), posArgs)
	_ = d.SetItem(objects.NewStr("kwds"), kwdArg)

	// Install _recreate_cm so __call__ works on direct subclasses too.
	var recreateHolder [1]objects.Object
	capturedFn := fn
	capturedArgs := callArgs
	capturedKwargs := callKwargs
	recreateHolder[0] = objects.NewBuiltinFunction("_recreate_cm", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		newGen, err := objects.Call(capturedFn, capturedArgs, capturedKwargs)
		if err != nil {
			return nil, err
		}
		newInst := objects.NewInstance(inst.Type())
		nd := newInst.EnsureDict()
		_ = nd.SetItem(objects.NewStr("gen"), newGen)
		_ = nd.SetItem(objects.NewStr("_recreate"), recreateHolder[0])
		return newInst, nil
	})
	_ = d.SetItem(objects.NewStr("_recreate"), recreateHolder[0])
	return objects.None(), nil
}

// contextManager implements @contextmanager. Decorating `func` returns
// a wrapper that, when called with the same arguments, drives `func`
// into a generator and hands it to _GeneratorContextManager.
//
// CPython's helper is a `def helper(*args, **kwds)` Python function so
// the wrapper participates in the descriptor protocol: free-function
// call accepts the args directly, and instance attribute access binds
// self. MethodFunc gives gopy the same surface for a Go-backed body.
//
// CPython: Lib/contextlib.py:276 contextmanager
// CPython: Objects/funcobject.c:411 PyFunction_NewWithQualName
func contextManager(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: contextmanager() takes exactly one argument (%d given)", len(args))
	}
	fn := args[0]
	helper := objects.NewMethodFunc("helper", func(hArgs []objects.Object, hKwargs map[string]objects.Object) (objects.Object, error) {
		genObj, err := objects.Call(fn, objects.NewTuple(hArgs), kwargsToDict(hKwargs))
		if err != nil {
			return nil, err
		}
		inst := objects.NewInstance(genCMType)
		d := inst.EnsureDict()
		if err := d.SetItem(objects.NewStr("gen"), genObj); err != nil {
			return nil, err
		}
		// _recreate: CPython: Lib/contextlib.py:112 _GeneratorContextManagerBase._recreate_cm
		// Captures fn + original call args so __call__ can mint a fresh CM.
		// Use an indirect holder to allow the closure to reference itself.
		capturedArgs := hArgs
		capturedKwargs := hKwargs
		var recreateHolder [1]objects.Object
		recreateHolder[0] = objects.NewBuiltinFunction("_recreate_cm", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			newGen, err := objects.Call(fn, objects.NewTuple(capturedArgs), kwargsToDict(capturedKwargs))
			if err != nil {
				return nil, err
			}
			newInst := objects.NewInstance(genCMType)
			nd := newInst.EnsureDict()
			if err := nd.SetItem(objects.NewStr("gen"), newGen); err != nil {
				return nil, err
			}
			// propagate _recreate to the new instance too
			if err := nd.SetItem(objects.NewStr("_recreate"), recreateHolder[0]); err != nil {
				return nil, err
			}
			return newInst, nil
		})
		if err := d.SetItem(objects.NewStr("_recreate"), recreateHolder[0]); err != nil {
			return nil, err
		}
		return inst, nil
	})
	return helper, nil
}

// genCMEnter sends None into the wrapped generator and returns the
// yielded value. A generator that returns immediately without yielding
// surfaces as RuntimeError per CPython.
//
// CPython: Lib/contextlib.py:136 _GeneratorContextManager.__enter__
func genCMEnter(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: __enter__() missing self")
	}
	inst, ok := args[0].(*objects.Instance)
	if !ok {
		return nil, fmt.Errorf("TypeError: __enter__ self not Instance")
	}
	genObj, err := inst.Dict().GetItem(objects.NewStr("gen"))
	if err != nil || genObj == nil {
		return nil, fmt.Errorf("RuntimeError: _GeneratorContextManager has no generator")
	}
	gen, ok := genObj.(*objects.Generator)
	if !ok {
		return nil, fmt.Errorf("TypeError: @contextmanager function did not return a generator")
	}
	v, err := gen.Send(objects.None())
	if err != nil {
		if errors.Is(err, objects.ErrStopIteration) {
			return nil, fmt.Errorf("RuntimeError: generator didn't yield")
		}
		return nil, err
	}
	return v, nil
}

// genCMExit resumes the wrapped generator. With no active exception we
// expect StopIteration, otherwise the generator must catch or swap the
// exception. The return value drives whether the with-statement
// suppresses the exception.
//
// CPython: Lib/contextlib.py:145 _GeneratorContextManager.__exit__
func genCMExit(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 4 {
		return nil, fmt.Errorf("TypeError: __exit__() takes 3 args (%d given)", len(args)-1)
	}
	inst, ok := args[0].(*objects.Instance)
	if !ok {
		return nil, fmt.Errorf("TypeError: __exit__ self not Instance")
	}
	typArg := args[1]
	valArg := args[2]
	genObj, _ := inst.Dict().GetItem(objects.NewStr("gen"))
	if genObj == nil {
		return objects.NewBool(false), nil
	}
	gen, ok := genObj.(*objects.Generator)
	if !ok {
		return nil, fmt.Errorf("TypeError: stored generator is not a generator")
	}
	if objects.IsNone(typArg) {
		_, err := gen.Send(objects.None())
		if err != nil {
			if errors.Is(err, objects.ErrStopIteration) {
				return objects.NewBool(false), nil
			}
			return nil, err
		}
		_ = gen.Close()
		return nil, fmt.Errorf("RuntimeError: generator didn't stop")
	}
	// Exception path: throw the original Python exception back into the
	// generator as a *RaisedError so execYieldValue can install it on
	// thread state via pyerrors.Raise. Using errorFromException (plain
	// fmt.Errorf) bypasses that path and causes synthesizeException to
	// create a generic Exception instead of preserving the original type.
	if objects.GenThrowHook == nil {
		return nil, fmt.Errorf("RuntimeError: generator.throw not available")
	}
	throwErr, hookErr := objects.GenThrowHook(valArg)
	if hookErr != nil {
		return nil, hookErr
	}
	val, thrErr := gen.Throw(throwErr)
	if thrErr == nil {
		// Generator caught the exception and yielded. Close it and
		// raise RuntimeError per CPython "generator didn't stop after
		// throw()".
		_ = val
		_ = gen.Close()
		return nil, fmt.Errorf("RuntimeError: generator didn't stop after throw()")
	}
	if errors.Is(thrErr, objects.ErrStopIteration) {
		// Generator exited cleanly after catching (or not catching) the
		// exception. CPython: `except StopIteration as exc: return exc is not value`
		// — suppress if the StopIteration is not the thing we threw in.
		// Since we threw a non-StopIteration (valArg), this is True → suppress.
		return objects.NewBool(true), nil
	}
	// CPython: `except BaseException as exc: if exc is not value: raise; return False`
	// Check Python-level identity: if the generator re-raised the SAME
	// exception object we threw in, return False (don't suppress).
	// The generator goroutine wraps the exception in *RaisedError before
	// sending it back, preserving the Python object for this identity check.
	var re *objects.RaisedError
	if errors.As(thrErr, &re) && re.Exc == valArg {
		return objects.NewBool(false), nil
	}
	// Different exception from the generator — re-raise.
	return nil, thrErr
}

// genCMCall makes _GeneratorContextManager instances callable as method
// decorators. When used as @run_with_locale('LC_CTYPE', 'tr_TR') on a
// method, Python calls the returned CM instance with the decorated function.
// Each invocation of the returned wrapper recreates the CM via _recreate so
// the generator is fresh on every call.
//
// CPython: Lib/contextlib.py:116 _GeneratorContextManagerBase.__call__
func genCMCall(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: __call__() missing self and func argument")
	}
	inst, ok := args[0].(*objects.Instance)
	if !ok {
		return nil, fmt.Errorf("TypeError: __call__ self not Instance")
	}
	fn := args[1]
	recreateFn, err := inst.Dict().GetItem(objects.NewStr("_recreate"))
	if err != nil || recreateFn == nil {
		return nil, fmt.Errorf("RuntimeError: _GeneratorContextManager missing _recreate")
	}
	inner := objects.NewMethodFunc("inner", func(iArgs []objects.Object, iKwargs map[string]objects.Object) (objects.Object, error) {
		// _recreate_cm() -> fresh CM instance
		cmObj, err := objects.Call(recreateFn, objects.NewTuple(nil), nil)
		if err != nil {
			return nil, err
		}
		enterFn, err := objects.GetAttr(cmObj, objects.NewStr("__enter__"))
		if err != nil {
			return nil, err
		}
		exitFn, err := objects.GetAttr(cmObj, objects.NewStr("__exit__"))
		if err != nil {
			return nil, err
		}
		if _, err := objects.Call(enterFn, objects.NewTuple(nil), nil); err != nil {
			return nil, err
		}
		result, fnErr := objects.Call(fn, objects.NewTuple(iArgs), kwargsToDict(iKwargs))
		if fnErr != nil {
			_, _ = objects.Call(exitFn, objects.NewTuple([]objects.Object{objects.None(), objects.None(), objects.None()}), nil)
			return nil, fnErr
		}
		if _, err := objects.Call(exitFn, objects.NewTuple([]objects.Object{objects.None(), objects.None(), objects.None()}), nil); err != nil {
			return nil, err
		}
		return result, nil
	})
	return inner, nil
}

// kwargsToDict converts a Go kwargs map into a Python *Dict for Call.
// Returns nil when the map is empty.
func kwargsToDict(kw map[string]objects.Object) *objects.Dict {
	if len(kw) == 0 {
		return nil
	}
	d := objects.NewDict()
	for k, v := range kw {
		_ = d.SetItem(objects.NewStr(k), v)
	}
	return d
}

// suppressType backs contextlib.suppress(*exceptions). Stores the
// allowed exception types and ignores them in __exit__.
//
// CPython: Lib/contextlib.py:433 suppress
var suppressType = newSuppressType()

func newSuppressType() *objects.Type {
	t := objects.NewType("suppress", []*objects.Type{abstractCMType})
	t.HasDict = true
	t.Getattro = objects.GenericGetAttr
	t.TpNew = func(cls *objects.Type, args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		inst := objects.NewInstance(cls)
		if err := inst.EnsureDict().SetItem(objects.NewStr("_exceptions"), objects.NewTuple(args)); err != nil {
			return nil, err
		}
		return inst, nil
	}
	objects.SetTypeDescr(t, "__enter__", objects.NewMethodDescr(t, "__enter__", suppressEnter))
	objects.SetTypeDescr(t, "__exit__", objects.NewMethodDescr(t, "__exit__", suppressExit))
	return t
}

// suppressEnter is the no-op __enter__; suppress yields nothing.
//
// CPython: Lib/contextlib.py:447 suppress.__enter__
func suppressEnter(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.None(), nil
}

// suppressExit returns True when the active exception's type is a
// subclass of any registered exception class, swallowing it. Other
// exception types pass through.
//
// CPython: Lib/contextlib.py:450 suppress.__exit__
func suppressExit(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 4 {
		return objects.NewBool(false), nil
	}
	typArg := args[1]
	if objects.IsNone(typArg) {
		return objects.NewBool(false), nil
	}
	inst, ok := args[0].(*objects.Instance)
	if !ok {
		return objects.NewBool(false), nil
	}
	excTup, _ := inst.Dict().GetItem(objects.NewStr("_exceptions"))
	if excTup == nil {
		return objects.NewBool(false), nil
	}
	tup, ok := excTup.(*objects.Tuple)
	if !ok {
		return objects.NewBool(false), nil
	}
	typ, ok := typArg.(*objects.Type)
	if !ok {
		return objects.NewBool(false), nil
	}
	for i := 0; i < tup.Len(); i++ {
		base, ok := tup.Item(i).(*objects.Type)
		if !ok {
			continue
		}
		if objects.IsSubtype(typ, base) {
			return objects.NewBool(true), nil
		}
	}
	return objects.NewBool(false), nil
}

// nullcontextType backs contextlib.nullcontext(enter_result=None). The
// enter result is stashed on the instance; __exit__ does nothing.
//
// CPython: Lib/contextlib.py:716 nullcontext
var nullcontextType = newNullcontextType()

func newNullcontextType() *objects.Type {
	t := objects.NewType("nullcontext", []*objects.Type{abstractCMType})
	t.HasDict = true
	t.Getattro = objects.GenericGetAttr
	t.TpNew = func(cls *objects.Type, args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		inst := objects.NewInstance(cls)
		enterResult := objects.None()
		if len(args) >= 1 {
			enterResult = args[0]
		}
		if err := inst.EnsureDict().SetItem(objects.NewStr("enter_result"), enterResult); err != nil {
			return nil, err
		}
		return inst, nil
	}
	objects.SetTypeDescr(t, "__enter__", objects.NewMethodDescr(t, "__enter__", nullcontextEnter))
	objects.SetTypeDescr(t, "__exit__", objects.NewMethodDescr(t, "__exit__", nullcontextExit))
	return t
}

func nullcontextEnter(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return objects.None(), nil
	}
	if inst, ok := args[0].(*objects.Instance); ok {
		if v, err := inst.Dict().GetItem(objects.NewStr("enter_result")); err == nil {
			return v, nil
		}
	}
	return objects.None(), nil
}

func nullcontextExit(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewBool(false), nil
}

// closingType backs contextlib.closing(thing). __exit__ calls
// thing.close().
//
// CPython: Lib/contextlib.py:342 closing
var closingType = newClosingType()

func newClosingType() *objects.Type {
	t := objects.NewType("closing", []*objects.Type{abstractCMType})
	t.HasDict = true
	t.Getattro = objects.GenericGetAttr
	t.TpNew = func(cls *objects.Type, args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("TypeError: closing() takes exactly 1 argument (%d given)", len(args))
		}
		inst := objects.NewInstance(cls)
		if err := inst.EnsureDict().SetItem(objects.NewStr("thing"), args[0]); err != nil {
			return nil, err
		}
		return inst, nil
	}
	objects.SetTypeDescr(t, "__enter__", objects.NewMethodDescr(t, "__enter__", closingEnter))
	objects.SetTypeDescr(t, "__exit__", objects.NewMethodDescr(t, "__exit__", closingExit))
	return t
}

func closingEnter(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return objects.None(), nil
	}
	inst, ok := args[0].(*objects.Instance)
	if !ok {
		return objects.None(), nil
	}
	v, _ := inst.Dict().GetItem(objects.NewStr("thing"))
	if v == nil {
		return objects.None(), nil
	}
	return v, nil
}

func closingExit(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return objects.NewBool(false), nil
	}
	inst, ok := args[0].(*objects.Instance)
	if !ok {
		return objects.NewBool(false), nil
	}
	thing, _ := inst.Dict().GetItem(objects.NewStr("thing"))
	if thing == nil {
		return objects.NewBool(false), nil
	}
	closeFn, _ := objects.GetAttr(thing, objects.NewStr("close"))
	if closeFn == nil {
		return objects.NewBool(false), nil
	}
	if _, err := objects.Call(closeFn, objects.NewTuple(nil), nil); err != nil {
		return nil, err
	}
	return objects.NewBool(false), nil
}

// redirectStdoutType / redirectStderrType back redirect_stdout and
// redirect_stderr. __enter__ swaps sys.stdout/stderr, __exit__ restores.
//
// CPython: Lib/contextlib.py:411 redirect_stdout
var (
	redirectStdoutType = newRedirectType("redirect_stdout", "stdout")
	redirectStderrType = newRedirectType("redirect_stderr", "stderr")
)

func newRedirectType(name, stream string) *objects.Type {
	t := objects.NewType(name, []*objects.Type{abstractCMType})
	t.HasDict = true
	t.Getattro = objects.GenericGetAttr
	streamName := stream
	t.TpNew = func(cls *objects.Type, args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("TypeError: %s() takes exactly 1 argument (%d given)", name, len(args))
		}
		inst := objects.NewInstance(cls)
		d := inst.EnsureDict()
		_ = d.SetItem(objects.NewStr("_new_target"), args[0])
		_ = d.SetItem(objects.NewStr("_stream"), objects.NewStr(streamName))
		_ = d.SetItem(objects.NewStr("_old_targets"), objects.NewList(nil))
		return inst, nil
	}
	objects.SetTypeDescr(t, "__enter__", objects.NewMethodDescr(t, "__enter__", redirectEnter))
	objects.SetTypeDescr(t, "__exit__", objects.NewMethodDescr(t, "__exit__", redirectExit))
	return t
}

func redirectEnter(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return objects.None(), nil
	}
	inst, ok := args[0].(*objects.Instance)
	if !ok {
		return objects.None(), nil
	}
	sysMod, ok := imp.GetModule("sys")
	if !ok {
		return objects.None(), nil
	}
	streamName, _ := inst.Dict().GetItem(objects.NewStr("_stream"))
	target, _ := inst.Dict().GetItem(objects.NewStr("_new_target"))
	oldTargets, _ := inst.Dict().GetItem(objects.NewStr("_old_targets"))
	stack, _ := oldTargets.(*objects.List)
	if stack == nil {
		stack = objects.NewList(nil)
	}
	cur, err := objects.GetAttr(sysMod, streamName)
	if err == nil {
		stack.Append(cur)
	}
	_ = inst.Dict().SetItem(objects.NewStr("_old_targets"), stack)
	if sn, ok := streamName.(*objects.Unicode); ok {
		if err := objects.SetAttr(sysMod, objects.NewStr(sn.Value()), target); err != nil {
			return nil, err
		}
	}
	return target, nil
}

func redirectExit(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return objects.NewBool(false), nil
	}
	inst, ok := args[0].(*objects.Instance)
	if !ok {
		return objects.NewBool(false), nil
	}
	sysMod, ok := imp.GetModule("sys")
	if !ok {
		return objects.NewBool(false), nil
	}
	streamName, _ := inst.Dict().GetItem(objects.NewStr("_stream"))
	oldTargets, _ := inst.Dict().GetItem(objects.NewStr("_old_targets"))
	stack, _ := oldTargets.(*objects.List)
	if stack == nil || stack.Len() == 0 {
		return objects.NewBool(false), nil
	}
	old, popErr := stack.Pop(stack.Len() - 1)
	if popErr != nil {
		return nil, popErr
	}
	if sn, ok := streamName.(*objects.Unicode); ok {
		if err := objects.SetAttr(sysMod, objects.NewStr(sn.Value()), old); err != nil {
			return nil, err
		}
	}
	return objects.NewBool(false), nil
}

// exitStackType backs ExitStack. The callback list lives in the
// instance __dict__ as a plain Python list of objects; sync vs async
// is collapsed since gopy treats both the same.
//
// CPython: Lib/contextlib.py:557 ExitStack
var exitStackType = objects.NewType("ExitStack", []*objects.Type{abstractCMType})

func init() {
	t := exitStackType
	t.HasDict = true
	t.Getattro = objects.GenericGetAttr
	t.TpNew = func(cls *objects.Type, _ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		inst := objects.NewInstance(cls)
		_ = inst.EnsureDict().SetItem(objects.NewStr("_exit_callbacks"), objects.NewList(nil))
		return inst, nil
	}
	objects.SetTypeDescr(t, "__enter__", objects.NewMethodDescr(t, "__enter__", exitStackEnter))
	objects.SetTypeDescr(t, "__exit__", objects.NewMethodDescr(t, "__exit__", exitStackExit))
	objects.SetTypeDescr(t, "enter_context", objects.NewMethodDescr(t, "enter_context", exitStackEnterContext))
	objects.SetTypeDescr(t, "push", objects.NewMethodDescr(t, "push", exitStackPush))
	objects.SetTypeDescr(t, "callback", objects.NewMethodDescr(t, "callback", exitStackCallback))
	objects.SetTypeDescr(t, "pop_all", objects.NewMethodDescr(t, "pop_all", exitStackPopAll))
	objects.SetTypeDescr(t, "close", objects.NewMethodDescr(t, "close", exitStackClose))
}

func exitStackEnter(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return objects.None(), nil
	}
	return args[0], nil
}

func exitStackEnterContext(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: enter_context() takes 1 argument")
	}
	self, ok := args[0].(*objects.Instance)
	if !ok {
		return nil, fmt.Errorf("TypeError: enter_context self is not Instance")
	}
	cm := args[1]
	enterFn, err := objects.GetAttr(cm, objects.NewStr("__enter__"))
	if err != nil {
		return nil, fmt.Errorf("TypeError: object does not support the context manager protocol")
	}
	exitFn, err := objects.GetAttr(cm, objects.NewStr("__exit__"))
	if err != nil {
		return nil, fmt.Errorf("TypeError: object does not support the context manager protocol")
	}
	result, err := objects.Call(enterFn, objects.NewTuple(nil), nil)
	if err != nil {
		return nil, err
	}
	cbList, _ := self.Dict().GetItem(objects.NewStr("_exit_callbacks"))
	list, _ := cbList.(*objects.List)
	if list == nil {
		list = objects.NewList(nil)
		_ = self.Dict().SetItem(objects.NewStr("_exit_callbacks"), list)
	}
	list.Append(exitFn)
	return result, nil
}

func exitStackPush(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: push() takes 1 argument")
	}
	self, ok := args[0].(*objects.Instance)
	if !ok {
		return nil, fmt.Errorf("TypeError: push self is not Instance")
	}
	exit := args[1]
	exitFn, err := objects.GetAttr(exit, objects.NewStr("__exit__"))
	cb := exit
	if err == nil {
		cb = exitFn
	}
	cbList, _ := self.Dict().GetItem(objects.NewStr("_exit_callbacks"))
	list, _ := cbList.(*objects.List)
	if list == nil {
		list = objects.NewList(nil)
		_ = self.Dict().SetItem(objects.NewStr("_exit_callbacks"), list)
	}
	list.Append(cb)
	return exit, nil
}

func exitStackCallback(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: callback() takes at least 1 argument")
	}
	self, ok := args[0].(*objects.Instance)
	if !ok {
		return nil, fmt.Errorf("TypeError: callback self is not Instance")
	}
	cb := args[1]
	rest := args[2:]
	// Wrap the (cb, args, kwargs) trio in a builtin that ignores its
	// exc_info arguments so it sits in the same callback list as
	// __exit__ methods.
	captured := rest
	capturedKw := kwargs
	wrapper := objects.NewBuiltinFunction("exit_callback", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return objects.Call(cb, objects.NewTuple(captured), kwargsToDict(capturedKw))
	})
	cbList, _ := self.Dict().GetItem(objects.NewStr("_exit_callbacks"))
	list, _ := cbList.(*objects.List)
	if list == nil {
		list = objects.NewList(nil)
		_ = self.Dict().SetItem(objects.NewStr("_exit_callbacks"), list)
	}
	list.Append(wrapper)
	return cb, nil
}

func exitStackPopAll(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return objects.None(), nil
	}
	self, ok := args[0].(*objects.Instance)
	if !ok {
		return objects.None(), nil
	}
	newStack := objects.NewInstance(exitStackType)
	cur, _ := self.Dict().GetItem(objects.NewStr("_exit_callbacks"))
	_ = newStack.Dict().SetItem(objects.NewStr("_exit_callbacks"), cur)
	_ = self.Dict().SetItem(objects.NewStr("_exit_callbacks"), objects.NewList(nil))
	return newStack, nil
}

func exitStackExit(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return objects.NewBool(false), nil
	}
	self, ok := args[0].(*objects.Instance)
	if !ok {
		return objects.NewBool(false), nil
	}
	cbList, _ := self.Dict().GetItem(objects.NewStr("_exit_callbacks"))
	list, _ := cbList.(*objects.List)
	if list == nil {
		return objects.NewBool(false), nil
	}
	suppress := false
	for list.Len() > 0 {
		cb, err := list.Pop(list.Len() - 1)
		if err != nil {
			return nil, err
		}
		// Exit callbacks are called with (exc_type, exc_val, exc_tb).
		var callArgs []objects.Object
		if len(args) >= 4 {
			callArgs = []objects.Object{args[1], args[2], args[3]}
		} else {
			callArgs = []objects.Object{objects.None(), objects.None(), objects.None()}
		}
		res, err := objects.Call(cb, objects.NewTuple(callArgs), nil)
		if err != nil {
			return nil, err
		}
		if objects.IsTrue(res) {
			suppress = true
		}
	}
	return objects.NewBool(suppress), nil
}

func exitStackClose(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return objects.None(), nil
	}
	return exitStackExit([]objects.Object{args[0], objects.None(), objects.None(), objects.None()}, nil)
}
