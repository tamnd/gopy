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
	pyerrors "github.com/tamnd/gopy/errors"
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
	origBasesTuple := objects.NewTuple(append([]objects.Object(nil), rawBases...))
	resolved, err := resolveBases(rawBases)
	if err != nil {
		return nil, err
	}
	// Bases that survive __mro_entries__ resolution must all be types
	// once a real type metaclass is in play, but CPython lets non-type
	// metaclasses run with arbitrary bases. Keep the typed slice for
	// the metaclass-winner calculation and fall back to the resolved
	// slice (basesTuple) for the actual call to meta(...).
	basesTuple := objects.NewTuple(resolved)
	bases := make([]*objects.Type, 0, len(resolved))
	allTypes := true
	for _, b := range resolved {
		t, ok := b.(*objects.Type)
		if !ok {
			allTypes = false
			continue
		}
		bases = append(bases, t)
	}

	// kwargs may carry metaclass. Pull it out before forwarding the
	// rest to the metaclass call (CPython removes it from mkw before
	// the meta(name, bases, ns, **mkw) dispatch). metaclass can be any
	// callable; CPython tracks isclass to decide whether the metaclass
	// winner calculation runs.
	//
	// CPython: Python/bltinmodule.c:147 PyDict_Pop(mkw, &_Py_ID(metaclass), &meta)
	var meta objects.Object
	isclass := false
	if mc, ok := kwargs["metaclass"]; ok {
		meta = mc
		_, isclass = mc.(*objects.Type)
		delete(kwargs, "metaclass")
	}
	if meta == nil {
		if len(bases) > 0 {
			meta = bases[0].Type()
		} else {
			meta = objects.TypeType()
		}
		isclass = true
	}
	// PEP 3115 metaclass winner: walk bases and pick the most-derived
	// metaclass. Skipped when meta is not a class.
	//
	// CPython: Python/bltinmodule.c:172 _PyType_CalculateMetaclass
	if isclass && allTypes {
		winner, err := calculateMetaclass(meta.(*objects.Type), bases)
		if err != nil {
			return nil, err
		}
		meta = winner
	}

	// Call meta.__prepare__(name, bases, **kwds) to get the class
	// namespace. PyObject_GetOptionalAttr suppresses AttributeError
	// only; other errors (e.g. a descriptor __get__ that raises) must
	// propagate so the class statement surfaces the real cause.
	//
	// CPython: Python/bltinmodule.c:183 PyObject_GetOptionalAttr(meta, __prepare__)
	var ns objects.Object
	prep, prepErr := objects.GetAttr(meta, objects.NewStr("__prepare__"))
	if prepErr != nil {
		ts := currentThread()
		if exc := pyerrors.Occurred(ts); exc != nil {
			if !pyerrors.Match(exc, pyerrors.PyExc_AttributeError) {
				return nil, prepErr
			}
			pyerrors.Clear(ts)
		} else if !isAttributeErrorMsg(prepErr) {
			return nil, prepErr
		}
		prep = nil
	}
	if prep != nil {
		prepArgsTuple := objects.NewTuple([]objects.Object{nameObj, basesTuple})
		kwargsDict := kwargsToDict(kwargs)
		ns, err = objects.Call(prep, prepArgsTuple, kwargsDict)
		// CPython: Python/bltinmodule.c:183 Py_XDECREF(prep)
		objects.Decref(prep)
		if err != nil {
			return nil, err
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

	cell, err := runClassBody(fn, ns)
	if err != nil {
		return nil, err
	}

	// When __mro_entries__ rewrote the bases, store the original bases
	// tuple as __orig_bases__ in the class namespace so typing.get_original_bases
	// and similar introspection tools can recover the unevaluated forms.
	//
	// CPython: Python/bltinmodule.c:208 builtin___build_class__
	// (if (bases != orig_bases) PyMapping_SetItemString(ns, "__orig_bases__", orig_bases))
	if len(rawBases) > 0 {
		basesChanged := len(resolved) != len(rawBases)
		if !basesChanged {
			for i, r := range resolved {
				if r != rawBases[i] {
					basesChanged = true
					break
				}
			}
		}
		if basesChanged {
			if d, ok := ns.(*objects.Dict); ok {
				_ = d.SetItem(objects.NewStr("__orig_bases__"), origBasesTuple)
			}
		}
	}

	callArgs := []objects.Object{nameObj, basesTuple, ns}
	result, err := objects.Call(meta, objects.NewTuple(callArgs), kwargsToDict(kwargs))

	// Verify the __class__ cell the body returned was filled with the
	// freshly created class. A metaclass that drops __classcell__ from the
	// namespace, or points it at a different class, is reported here.
	//
	// CPython: Python/bltinmodule.c:215 builtin___build_class__
	if err == nil && result != nil {
		if cellObj, ok := cell.(*objects.Cell); ok {
			if resType, isType := result.(*objects.Type); isType && cellObj.Contents != resType {
				nameRepr, rerr := objects.Repr(nameObj)
				if rerr == nil {
					clsRepr, crerr := objects.Repr(result)
					if crerr == nil {
						if cellObj.Contents == nil {
							err = fmt.Errorf("RuntimeError: __class__ not set defining %s as %s. Was __classcell__ propagated to type.__new__?", nameRepr, clsRepr)
						} else if setRepr, srerr := objects.Repr(cellObj.Contents); srerr == nil {
							err = fmt.Errorf("TypeError: __class__ set to %s defining %s as %s", setRepr, nameRepr, clsRepr)
						}
						result = nil
					}
				}
			}
		}
	}
	// Release the initial NewDict ref. NewTuple copies raw pointers without
	// Incref-ing ns, so the only owner remaining after the metaclass call is
	// this reference. The metaclass copied every namespace entry into the
	// type's descriptor table with its own Incref, so this is the last owner
	// of ns. gopy dicts carry no synchronous tp_dealloc, so dropping the
	// refcount alone leaves the method functions ns holds pinned by a count
	// no live container backs: the class dies, the methods never reclaim, and
	// weakref(A.method) never clears. Mirror dict_dealloc and clear the
	// namespace contents once ns reaches refcount zero (the precise signal
	// that nothing else, e.g. a __prepare__ mapping the caller kept, still
	// holds it).
	//
	// CPython: Python/bltinmodule.c:246 builtin___build_class__ Py_DECREF(ns)
	// CPython: Objects/dictobject.c:2768 dict_dealloc (PyDict_Clear on last decref)
	if d, ok := ns.(*objects.Dict); ok {
		objects.DecrefThrowawayKwargs(d)
	} else {
		objects.Decref(ns)
	}
	return result, err
}

// runClassBody invokes the body function with ns as its f_locals, so
// STORE_NAME inside the body populates the class namespace. Mirrors
// PyEval_EvalCodeEx with locals=ns.
//
// CPython: Python/bltinmodule.c builtin___build_class__ (the
// PyObject_Call(func, ()) call after LOCALS = PyObject_Call(prep, ...))
func runClassBody(fn *objects.Function, ns objects.Object) (objects.Object, error) {
	co := fn.Code
	if co == nil {
		return nil, fmt.Errorf("TypeError: __build_class__: function has no code")
	}
	ts := currentThread()
	if ts == nil {
		ts = state.NewThread()
	}
	stack := frameStackFor(ts)
	f := stack.Push(co, fn.Globals, nil, fn)
	defer stack.Pop()
	f.Locals = ns
	f.Builtins = fn.Builtins

	// The body fn closes over the surrounding lexical environment for
	// __class__ / free variable resolution. Wire its closure cells
	// into the frame's free-var slots so LOAD_DEREF still works inside
	// the body.
	if fn.Closure != nil && fn.Closure.Len() > 0 {
		// CPython: Python/bytecodes.c:1932 COPY_FREE_VARS offset.
		// Free vars sit at co_nlocalsplus - co_nfreevars.
		base := frame.NLocalsPlusOf(co) - fn.Closure.Len()
		for i := 0; i < fn.Closure.Len(); i++ {
			// CPython: Python/bytecodes.c:1925 COPY_FREE_VARS uses
			// PyStackRef_FromPyObjectNew; frame slot owns the reference.
			f.LocalsPlus[base+i] = stackref.FromObjectNew(fn.Closure.Item(i))
		}
	}

	// The body returns its __class__ cell (when the class body references
	// __class__ or uses zero-arg super), otherwise None. __build_class__
	// uses it to verify the cell was filled with the freshly created class.
	//
	// CPython: Python/bltinmodule.c:206 builtin___build_class__ (cell = _PyEval_Vector)
	cell, err := Eval(ts, f)
	return cell, err
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

// resolveBases ports CPython's _PyObject_UpdateBases. For each base that
// is not a type, look up __mro_entries__ and splice the returned tuple
// into the base list in place of the original entry. The original tuple
// (containing the unresolved bases) is passed as the argument so that
// __mro_entries__ implementations can inspect their siblings (e.g.
// typing._GenericAlias.__mro_entries__ returns () when Protocol is in
// the same bases tuple).
//
// CPython: Objects/typeobject.c:3690 _PyObject_UpdateBases
func resolveBases(rawBases []objects.Object) ([]objects.Object, error) {
	origTuple := objects.NewTuple(append([]objects.Object(nil), rawBases...))
	out := make([]objects.Object, 0, len(rawBases))
	mroName := objects.NewStr("__mro_entries__")
	for _, b := range rawBases {
		if _, ok := b.(*objects.Type); ok {
			out = append(out, b)
			continue
		}
		meth, _ := objects.GetAttr(b, mroName)
		if meth == nil {
			out = append(out, b)
			continue
		}
		res, err := objects.Call(meth, objects.NewTuple([]objects.Object{origTuple}), nil)
		if err != nil {
			return nil, err
		}
		entries, ok := res.(*objects.Tuple)
		if !ok {
			return nil, fmt.Errorf("TypeError: __mro_entries__ must return a tuple")
		}
		for i := 0; i < entries.Len(); i++ {
			out = append(out, entries.Item(i))
		}
	}
	return out, nil
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
