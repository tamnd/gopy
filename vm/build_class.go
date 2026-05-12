// __build_class__ is the builtin every `class C(...): ...` statement
// invokes through LOAD_BUILD_CLASS. The compiler emits the call as
//
//	__build_class__(<body fn>, "C", *bases, **kwds)
//
// and the implementation here:
//
//  1. allocates a fresh namespace dict (this is the class's tp_dict),
//  2. runs the body fn with that dict as f_locals so STORE_NAME lands
//     in the namespace,
//  3. resolves the metaclass (kwds['metaclass'] if present, else
//     `type`),
//  4. dispatches to metaclass(name, bases_tuple, ns, **other_kwds),
//     which builds the class object via NewUserType.
//
// Lives in vm/ because step 2 needs to push a frame and drive Eval;
// builtins/ cannot do that without an import cycle. The hook back into
// builtins/ is registered in init().
//
// CPython: Python/bltinmodule.c:131 builtin___build_class__
package vm

import (
	"fmt"

	"github.com/tamnd/gopy/builtins"
	"github.com/tamnd/gopy/frame"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/stackref"
	"github.com/tamnd/gopy/state"
)

func init() {
	builtins.SetBuildClass(buildClass)
}

// buildClass implements the body of builtins.__build_class__.
//
// CPython: Python/bltinmodule.c:131 builtin___build_class__
func buildClass(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: __build_class__: not enough arguments")
	}
	fn, ok := args[0].(*objects.Function)
	if !ok {
		return nil, fmt.Errorf("TypeError: __build_class__: func must be a function")
	}
	nameObj, ok := args[1].(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: __build_class__: name is not a string")
	}
	rawBases := args[2:]
	bases := make([]*objects.Type, 0, len(rawBases))
	for _, b := range rawBases {
		t, ok := b.(*objects.Type)
		if !ok {
			return nil, fmt.Errorf("TypeError: __build_class__: base is not a type, got %s", b.Type().Name)
		}
		bases = append(bases, t)
	}

	// kwargs may carry metaclass. Pull it out before forwarding the
	// rest to the metaclass call (CPython removes it from mkw before
	// the meta(name, bases, ns, **mkw) dispatch).
	var meta *objects.Type
	if mc, ok := kwargs["metaclass"]; ok {
		mt, ok := mc.(*objects.Type)
		if !ok {
			return nil, fmt.Errorf("TypeError: metaclass must be a type, not %s", mc.Type().Name)
		}
		meta = mt
		delete(kwargs, "metaclass")
	}
	if meta == nil {
		if len(bases) > 0 {
			meta = bases[0].Type()
		} else {
			meta = objects.TypeType()
		}
	}
	// PEP 3115 metaclass winner: walk bases and pick the most-derived
	// metaclass. CPython: Python/bltinmodule.c:131 builtin___build_class__
	// (_Py_CalculateMetaclass call).
	winner, err := calculateMetaclass(meta, bases)
	if err != nil {
		return nil, err
	}
	meta = winner

	// Call meta.__prepare__(name, bases, **kwds) to get the class
	// namespace. If __prepare__ is not defined, fall back to a plain
	// dict as CPython does.
	//
	// CPython: Python/bltinmodule.c:131 builtin___build_class__
	// (_PyObject_CallMethodIdObjArgs(meta, &PyId___prepare__, ...))
	basesObjs := make([]objects.Object, len(bases))
	for i, b := range bases {
		basesObjs[i] = b
	}
	basesTuple := objects.NewTuple(basesObjs)
	var ns objects.Object
	prep, prepErr := objects.GetAttr(meta, objects.NewStr("__prepare__"))
	if prepErr == nil && prep != nil {
		prepArgsTuple := objects.NewTuple([]objects.Object{nameObj, basesTuple})
		kwargsDict := kwargsToDict(kwargs)
		ns, err = objects.Call(prep, prepArgsTuple, kwargsDict)
		if err != nil {
			return nil, fmt.Errorf("__prepare__: %w", err)
		}
	} else {
		ns = objects.NewDict()
	}

	// The class body opens with LOAD_NAME __name__ → STORE_NAME
	// __module__. That LOAD_NAME must reach the enclosing module's
	// __name__ via globals, so do not pre-stamp __name__ into the
	// class namespace (doing so would clobber __module__ with the
	// class name). __qualname__ is stamped by the body's own
	// STORE_NAME emitted by the compiler.
	//
	// CPython: Python/bltinmodule.c:131 builtin___build_class__

	if err := runClassBody(fn, ns); err != nil {
		return nil, err
	}

	callArgs := []objects.Object{nameObj, basesTuple, ns}
	return objects.Call(meta, objects.NewTuple(callArgs), kwargsToDict(kwargs))
}

// runClassBody invokes the body function with ns as its f_locals, so
// STORE_NAME inside the body populates the class namespace. Mirrors
// PyEval_EvalCodeEx with locals=ns.
//
// CPython: Python/bltinmodule.c builtin___build_class__ (the
// PyObject_Call(func, ()) call after LOCALS = PyObject_Call(prep, ...))
func runClassBody(fn *objects.Function, ns objects.Object) error {
	co := fn.Code
	if co == nil {
		return fmt.Errorf("TypeError: __build_class__: function has no code")
	}
	ts := currentThread()
	if ts == nil {
		ts = state.NewThread()
	}
	stack := frameStackFor(ts)
	f := stack.Push(co, fn.Globals, nil, fn, nil)
	defer stack.Pop()
	f.Locals = ns
	f.Builtins = fn.Builtins

	// The body fn closes over the surrounding lexical environment for
	// __class__ / free variable resolution. Wire its closure cells
	// into the frame's free-var slots so LOAD_DEREF still works inside
	// the body.
	if fn.Closure != nil && fn.Closure.Len() > 0 {
		base := frame.FreesStart(co)
		for i := 0; i < fn.Closure.Len(); i++ {
			f.LocalsPlus[base+i] = stackref.FromObject(fn.Closure.Item(i))
		}
	}

	_, err := Eval(ts, f)
	return err
}

// calculateMetaclass ports _Py_CalculateMetaclass: for each base, if
// the base's metatype is more derived than the current winner, promote
// it; if neither is a subtype of the other, raise TypeError.
//
// CPython: Objects/typeobject.c:3136 _Py_CalculateMetaclass
func calculateMetaclass(winner *objects.Type, bases []*objects.Type) (*objects.Type, error) {
	for _, base := range bases {
		baseMeta := base.Type()
		if objects.IsSubtype(winner, baseMeta) {
			continue
		}
		if objects.IsSubtype(baseMeta, winner) {
			winner = baseMeta
			continue
		}
		return nil, fmt.Errorf("TypeError: metaclass conflict: the metaclass of a derived class must be a (non-strict) subclass of the metaclasses of all its bases")
	}
	return winner, nil
}

func kwargsToDict(kwargs map[string]objects.Object) *objects.Dict {
	if len(kwargs) == 0 {
		return nil
	}
	d := objects.NewDict()
	for k, v := range kwargs {
		_ = d.SetItem(objects.NewStr(k), v)
	}
	return d
}
