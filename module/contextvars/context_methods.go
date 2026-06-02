// Context method bindings: run, copy, get, keys, values, items, plus
// the mapping (len, subscript), sequence (contains), iteration, and
// richcompare protocol. Ports PyContext_methods, PyContext_as_mapping,
// PyContext_as_sequence, context_tp_iter and context_tp_richcompare
// from cpython/Python/context.c so Python code can call ctx.run(fn),
// ctx.copy(), ctx[var], var in ctx, iter(ctx), and len(ctx).
//
// CPython: Python/context.c:734 PyContext_methods
// CPython: Python/context.c:744 PyContext_as_sequence
// CPython: Python/context.c:748 PyContext_as_mapping

package contextvars

import (
	"fmt"

	"github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/objects"
)

func init() {
	ContextType.Iter = contextIter
	ContextType.RichCmp = contextRichCompare
	ContextType.Mapping = &objects.MappingMethods{
		Length:  contextLen,
		GetItem: contextSubscript,
	}
	ContextType.Sequence = &objects.SequenceMethods{
		Contains: contextContains,
	}
	// Context is unhashable: CPython wires tp_hash to
	// PyObject_HashNotImplemented.
	//
	// CPython: Python/context.c:769 .tp_hash = PyObject_HashNotImplemented
	ContextType.Hash = func(o objects.Object) (int64, error) {
		return 0, fmt.Errorf("TypeError: unhashable type: 'Context'")
	}
	// Context omits Py_TPFLAGS_BASETYPE, so `class C(Context)` raises
	// "not an acceptable base type".
	//
	// CPython: Python/context.c:768 .tp_flags (no Py_TPFLAGS_BASETYPE)
	ContextType.TpFlags &^= objects.TpFlagBasetype

	// Register the tp_methods table as method descriptors and let the
	// inherited PyObject_GenericGetAttr slot resolve them, so __class__,
	// __eq__ and friends fall through to object rather than a custom
	// getattro that masks them.
	//
	// CPython: Python/context.c:734 PyContext_methods
	// CPython: Python/context.c:766 .tp_getattro = PyObject_GenericGetAttr
	objects.SetTypeDescr(ContextType, "run", objects.NewMethodDescr(ContextType, "run", contextRunDescr))
	objects.SetTypeDescr(ContextType, "copy", objects.NewMethodDescr(ContextType, "copy", contextCopyDescr))
	objects.SetTypeDescr(ContextType, "get", objects.NewMethodDescr(ContextType, "get", contextGetDescr))
	objects.SetTypeDescr(ContextType, "keys", objects.NewMethodDescr(ContextType, "keys", contextKeysDescr))
	objects.SetTypeDescr(ContextType, "values", objects.NewMethodDescr(ContextType, "values", contextValuesDescr))
	objects.SetTypeDescr(ContextType, "items", objects.NewMethodDescr(ContextType, "items", contextItemsMethod))
	// The mapping / sequence / iter slots also surface as the dunder
	// methods CPython's slot-wrapper generation would create, so
	// collections.abc.Mapping's method probe finds them.
	//
	// CPython: Objects/typeobject.c:11220 add_operators
	objects.SetTypeDescr(ContextType, "__getitem__", objects.NewMethodDescr(ContextType, "__getitem__", contextGetItemDescr))
	objects.SetTypeDescr(ContextType, "__len__", objects.NewMethodDescr(ContextType, "__len__", contextLenDescr))
	objects.SetTypeDescr(ContextType, "__contains__", objects.NewMethodDescr(ContextType, "__contains__", contextContainsDescr))
	objects.SetTypeDescr(ContextType, "__iter__", objects.NewMethodDescr(ContextType, "__iter__", contextIterDescr))
}

// contextSelf pulls the Context receiver out of args[0] for a method
// descriptor, which binds self as the first positional argument.
func contextSelf(name string, args []objects.Object) (*Context, []objects.Object, error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("TypeError: descriptor '%s' of 'Context' object needs an argument", name)
	}
	c, ok := args[0].(*Context)
	if !ok {
		return nil, nil, fmt.Errorf("TypeError: descriptor '%s' requires a 'Context' object", name)
	}
	return c, args[1:], nil
}

func contextRunDescr(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	c, rest, err := contextSelf("run", args)
	if err != nil {
		return nil, err
	}
	return contextRun(c, rest, kwargs)
}

func contextCopyDescr(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	c, rest, err := contextSelf("copy", args)
	if err != nil {
		return nil, err
	}
	if len(rest) != 0 || len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: copy() takes no arguments")
	}
	return c.Copy(), nil
}

func contextGetDescr(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	c, rest, err := contextSelf("get", args)
	if err != nil {
		return nil, err
	}
	return contextGetMethod(c, rest, kwargs)
}

func contextKeysDescr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	c, _, err := contextSelf("keys", args)
	if err != nil {
		return nil, err
	}
	ks, _, _, err := contextItems(c)
	if err != nil {
		return nil, err
	}
	return objects.Iter(objects.NewList(ks))
}

func contextValuesDescr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	c, _, err := contextSelf("values", args)
	if err != nil {
		return nil, err
	}
	_, vs, _, err := contextItems(c)
	if err != nil {
		return nil, err
	}
	return objects.Iter(objects.NewList(vs))
}

func contextItemsMethod(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	c, _, err := contextSelf("items", args)
	if err != nil {
		return nil, err
	}
	_, _, pairs, err := contextItems(c)
	if err != nil {
		return nil, err
	}
	return objects.Iter(objects.NewList(pairs))
}

func contextGetItemDescr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	c, rest, err := contextSelf("__getitem__", args)
	if err != nil {
		return nil, err
	}
	if len(rest) != 1 {
		return nil, fmt.Errorf("TypeError: __getitem__() takes exactly one argument (%d given)", len(rest))
	}
	return contextSubscript(c, rest[0])
}

func contextLenDescr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	c, _, err := contextSelf("__len__", args)
	if err != nil {
		return nil, err
	}
	n, err := contextLen(c)
	if err != nil {
		return nil, err
	}
	return objects.NewInt(int64(n)), nil
}

func contextContainsDescr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	c, rest, err := contextSelf("__contains__", args)
	if err != nil {
		return nil, err
	}
	if len(rest) != 1 {
		return nil, fmt.Errorf("TypeError: __contains__() takes exactly one argument (%d given)", len(rest))
	}
	found, err := contextContains(c, rest[0])
	if err != nil {
		return nil, err
	}
	return objects.NewBool(found), nil
}

func contextIterDescr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	c, _, err := contextSelf("__iter__", args)
	if err != nil {
		return nil, err
	}
	return contextIter(c)
}

// contextRun ports Context.run(callable, *args, **kwargs). It enters the
// context, vector-calls the callable with the remaining arguments, and
// exits the context whether or not the call raised.
//
// CPython: Python/context.c:706 context_run
func contextRun(c *Context, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	ts, err := currentTS()
	if err != nil {
		return nil, err
	}
	if len(args) < 1 {
		errors.SetString(ts, errors.PyExc_TypeError,
			"run() missing 1 required positional argument")
		return nil, fmt.Errorf("TypeError: run() missing 1 required positional argument")
	}
	callable := args[0]
	callArgs := objects.NewTuple(args[1:])
	var kwargsDict *objects.Dict
	if len(kwargs) > 0 {
		kwargsDict = objects.NewDict()
		for k, v := range kwargs {
			if serr := kwargsDict.SetItem(objects.NewStr(k), v); serr != nil {
				return nil, serr
			}
		}
	}
	return c.Run(ts, func() (objects.Object, error) {
		return objects.Call(callable, callArgs, kwargsDict)
	})
}

// contextGet ports Context.get(key, default=None). A missing key returns
// the default rather than raising, unlike subscription.
//
// CPython: Python/context.c:627 _contextvars_Context_get_impl
func contextGetMethod(c *Context, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: get() takes no keyword arguments")
	}
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("TypeError: get() takes from 1 to 2 positional arguments")
	}
	cv, err := contextCheckKeyType(args[0])
	if err != nil {
		return nil, err
	}
	def := objects.None()
	if len(args) == 2 {
		def = args[1]
	}
	val, found, err := c.Get(cv)
	if err != nil {
		return nil, err
	}
	if !found {
		return def, nil
	}
	return val, nil
}

// contextLen ports context_tp_len (mp_length).
//
// CPython: Python/context.c:577 context_tp_len
func contextLen(o objects.Object) (int, error) {
	c, ok := o.(*Context)
	if !ok {
		return 0, fmt.Errorf("TypeError: expected Context")
	}
	return c.Len(), nil
}

// contextSubscript ports context_tp_subscript (mp_subscript): ctx[var].
// A missing key raises KeyError; a non-ContextVar key raises TypeError.
//
// CPython: Python/context.c:585 context_tp_subscript
func contextSubscript(o, key objects.Object) (objects.Object, error) {
	c, ok := o.(*Context)
	if !ok {
		return nil, fmt.Errorf("TypeError: expected Context")
	}
	cv, err := contextCheckKeyType(key)
	if err != nil {
		return nil, err
	}
	val, found, err := c.Get(cv)
	if err != nil {
		return nil, err
	}
	if !found {
		if ts, terr := currentTS(); terr == nil {
			errors.Set(ts, errors.PyExc_KeyError, objects.NewTuple([]objects.Object{key}))
		}
		return nil, fmt.Errorf("KeyError")
	}
	return val, nil
}

// contextContains ports context_tp_contains (sq_contains): var in ctx.
//
// CPython: Python/context.c:603 context_tp_contains
func contextContains(o, key objects.Object) (bool, error) {
	c, ok := o.(*Context)
	if !ok {
		return false, fmt.Errorf("TypeError: expected Context")
	}
	cv, err := contextCheckKeyType(key)
	if err != nil {
		return false, err
	}
	_, found, err := c.Get(cv)
	if err != nil {
		return false, err
	}
	return found, nil
}

// contextIter ports context_tp_iter: iterating a Context yields its
// keys, matching dict iteration semantics.
//
// CPython: Python/context.c:544 context_tp_iter
func contextIter(o objects.Object) (objects.Object, error) {
	c, ok := o.(*Context)
	if !ok {
		return nil, fmt.Errorf("TypeError: expected Context")
	}
	ks, _, _, err := contextItems(c)
	if err != nil {
		return nil, err
	}
	return objects.Iter(objects.NewList(ks))
}

// contextRichCompare ports context_tp_richcompare: two Contexts compare
// equal when their HAMTs hold the same bindings. Only EQ/NE are defined;
// any other comparison or operand type returns NotImplemented.
//
// CPython: Python/context.c:550 context_tp_richcompare
func contextRichCompare(a, b objects.Object, op objects.CompareOp) (objects.Object, error) {
	ac, aok := a.(*Context)
	bc, bok := b.(*Context)
	if !aok || !bok || (op != objects.CompareEQ && op != objects.CompareNE) {
		return objects.NotImplemented(), nil
	}
	eq, err := ac.Eq(bc)
	if err != nil {
		return nil, err
	}
	if op == objects.CompareNE {
		eq = !eq
	}
	return objects.NewBool(eq), nil
}

// contextItems materializes the context's bindings into parallel key,
// value, and (key, value) tuple slices in a single HAMT walk. keys(),
// values(), items() and iteration all share this so they observe the
// same snapshot.
//
// CPython: Python/context.c:657 _contextvars_Context_items_impl
func contextItems(c *Context) (keys, vals, pairs []objects.Object, err error) {
	it := c.vars.Iter()
	for {
		k, v, ok, ierr := it.Next()
		if ierr != nil {
			return nil, nil, nil, ierr
		}
		if !ok {
			break
		}
		keys = append(keys, k)
		vals = append(vals, v)
		pairs = append(pairs, objects.NewTuple([]objects.Object{k, v}))
	}
	return keys, vals, pairs, nil
}

// contextCheckKeyType enforces that subscription / membership keys are
// ContextVar instances, matching context_check_key_type.
//
// CPython: Python/context.c:497 context_check_key_type
func contextCheckKeyType(key objects.Object) (*ContextVar, error) {
	cv, ok := key.(*ContextVar)
	if !ok {
		r, _ := objects.Repr(key)
		return nil, fmt.Errorf("TypeError: ContextVar key was expected, got %s", r)
	}
	return cv, nil
}
