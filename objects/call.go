// Port of Objects/call.c. The vectorcall path (PEP 590) is the
// preferred entry point; objects whose type does not advertise a
// Vectorcall slot fall back through MakeTpCall, which builds the
// tuple/dict the legacy tp_call slot expects. Variadic helpers like
// PyObject_CallFunctionObjArgs that depend on C va_list machinery are
// not yet ported.
//
// CPython: Objects/call.c

package objects

import (
	"fmt"
)

// VectorcallArgumentsOffset is the high bit of nargsf signaling that
// the caller reserved one slot at args[-1] for a self pointer; the
// callee may overwrite that slot. Mirrors PY_VECTORCALL_ARGUMENTS_OFFSET,
// the high bit of size_t.
//
// CPython: Include/cpython/abstract.h:24 PY_VECTORCALL_ARGUMENTS_OFFSET
const VectorcallArgumentsOffset uint = 1 << 63

// VectorcallNargs strips the VectorcallArgumentsOffset flag from
// nargsf and returns just the positional count.
//
// CPython: Objects/call.c:1127 PyVectorcall_NARGS
func VectorcallNargs(nargsf uint) int {
	return int(nargsf & ^VectorcallArgumentsOffset)
}

// vectorcallFunction returns the type's Vectorcall slot, or nil if
// the type does not opt in. Mirrors _PyVectorcall_FunctionInline plus
// the public PyVectorcall_Function wrapper.
//
// CPython: Objects/call.c:257 PyVectorcall_Function /
//
//	Include/internal/pycore_call.h:124 _PyVectorcall_FunctionInline
func vectorcallFunction(callable Object) func(Object, []Object, uint, *Tuple) (Object, error) {
	if callable == nil {
		return nil
	}
	return callable.Type().Vectorcall
}

// MakeTpCall is the slow path used when a callable does not advertise
// vectorcall. It builds the (args_tuple, kwargs_dict) shape the
// tp_call slot expects and dispatches through it.
//
// CPython: Objects/call.c:200 _PyObject_MakeTpCall
func MakeTpCall(callable Object, args []Object, nargs int, keywords Object) (Object, error) {
	if nargs < 0 {
		return nil, fmt.Errorf("MakeTpCall: nargs %d < 0", nargs)
	}
	t := callable.Type()
	if t.Call == nil {
		return nil, objectIsNotCallable(callable)
	}

	posArgs := make([]Object, nargs)
	if nargs > 0 {
		copy(posArgs, args[:nargs])
	}

	var kwargs map[string]Object
	switch kw := keywords.(type) {
	case nil:
		// no kwargs
	case *Tuple:
		// kwnames tuple: kwarg values follow positionals in args.
		nkw := kw.Len()
		if nkw > 0 {
			if len(args) < nargs+nkw {
				return nil, fmt.Errorf("MakeTpCall: kwnames tuple of size %d but args has only %d trailing slots", nkw, len(args)-nargs)
			}
			kwargs = make(map[string]Object, nkw)
			for i := range nkw {
				name, err := Str(kw.Item(i))
				if err != nil {
					return nil, err
				}
				kwargs[name] = args[nargs+i]
			}
		}
	case *Dict:
		if kw.Len() > 0 {
			kwargs = dictToMap(kw)
		}
	default:
		return nil, fmt.Errorf("MakeTpCall: keywords must be tuple or dict, not %s", keywords.Type().Name)
	}

	return t.Call(callable, posArgs, kwargs)
}

// vectorcallCall is the inner helper that converts a tuple/dict pair
// into a vectorcall and dispatches through func. Mirrors the static
// _PyVectorcall_Call helper in call.c.
//
// CPython: Objects/call.c:263 _PyVectorcall_Call
func vectorcallCall(fn func(Object, []Object, uint, *Tuple) (Object, error), callable Object, tuple *Tuple, kwargs *Dict) (Object, error) {
	nargs := tuple.Len()
	nkw := 0
	if kwargs != nil {
		nkw = kwargs.Len()
	}
	args := make([]Object, nargs+nkw)
	for i := range nargs {
		args[i] = tuple.Item(i)
	}
	if nkw == 0 {
		return fn(callable, args[:nargs], uint(nargs), nil)
	}
	keys := kwargs.Keys()
	kwnamesItems := make([]Object, 0, len(keys))
	for i, k := range keys {
		v, err := kwargs.GetItem(k)
		if err != nil {
			return nil, err
		}
		args[nargs+i] = v
		kwnamesItems = append(kwnamesItems, k)
	}
	kwnames := NewTuple(kwnamesItems)
	return fn(callable, args, uint(nargs)|VectorcallArgumentsOffset, kwnames)
}

// Vectorcall is the PEP 590 entry point used by the eval loop's CALL
// family. nargsf is the positional count plus the optional
// VectorcallArgumentsOffset flag; kwnames carries keyword names whose
// values live at args[nargs..nargs+nkwnames].
//
// CPython: Objects/call.c:323 PyObject_Vectorcall (forwards to
// _PyObject_VectorcallTstate, Include/internal/pycore_call.h:162)
func Vectorcall(callable Object, args []Object, nargsf uint, kwnames *Tuple) (Object, error) {
	fn := vectorcallFunction(callable)
	if fn == nil {
		var keywords Object
		if kwnames != nil {
			keywords = kwnames
		}
		return MakeTpCall(callable, args, VectorcallNargs(nargsf), keywords)
	}
	return fn(callable, args, nargsf, kwnames)
}

// Call is PyObject_Call: the legacy entry that hands a positional
// tuple and keyword dict to whatever path the callable advertises.
// vectorcall-capable types take a fast path; everything else lands in
// tp_call via MakeTpCall.
//
// CPython: Objects/call.c:370 PyObject_Call (forwards to _PyObject_Call)
func Call(callable Object, args *Tuple, kwargs *Dict) (Object, error) {
	if args == nil {
		args = NewTuple(nil)
	}
	fn := vectorcallFunction(callable)
	if fn != nil {
		return vectorcallCall(fn, callable, args, kwargs)
	}
	t := callable.Type()
	if t.Call == nil {
		return nil, objectIsNotCallable(callable)
	}
	posArgs := make([]Object, args.Len())
	for i := 0; i < args.Len(); i++ {
		posArgs[i] = args.Item(i)
	}
	var kwmap map[string]Object
	if kwargs != nil && kwargs.Len() > 0 {
		kwmap = dictToMap(kwargs)
	}
	return t.Call(callable, posArgs, kwmap)
}

// CallNoArgs is PyObject_CallNoArgs; a zero-arg vectorcall.
//
// CPython: Objects/call.c:101 PyObject_CallNoArgs
func CallNoArgs(callable Object) (Object, error) {
	return Vectorcall(callable, nil, 0, nil)
}

// CallOneArg is PyObject_CallOneArg; a single-positional vectorcall
// that reserves the args[-1] slot via VectorcallArgumentsOffset.
//
// CPython: Objects/call.c:386 PyObject_CallOneArg
func CallOneArg(callable Object, arg Object) (Object, error) {
	if arg == nil {
		return nil, fmt.Errorf("CallOneArg: arg must not be nil")
	}
	stack := []Object{nil, arg}
	return Vectorcall(callable, stack[1:], 1|VectorcallArgumentsOffset, nil)
}

// CallObject is PyObject_CallObject. args may be nil, in which case
// it behaves like CallNoArgs; otherwise it is treated as a positional
// tuple.
//
// CPython: Objects/call.c:460 PyObject_CallObject
func CallObject(callable Object, args *Tuple) (Object, error) {
	if args == nil {
		return CallNoArgs(callable)
	}
	return Call(callable, args, nil)
}

// VectorcallDict is the PyObject_VectorcallDict entry: same shape as
// Vectorcall but the keyword side is a *Dict instead of (kwnames,
// trailing args). Empty/nil kwargs short-circuit to a plain
// vectorcall; otherwise the dict is unpacked into kwnames + values
// appended to a fresh stack so the underlying slot sees the standard
// vectorcall layout.
//
// CPython: Objects/call.c:154 PyObject_VectorcallDict (forwards to
// Objects/call.c:110 _PyObject_VectorcallDictTstate)
func VectorcallDict(callable Object, args []Object, nargsf uint, kwargs *Dict) (Object, error) {
	nargs := VectorcallNargs(nargsf)
	if nargs < 0 {
		return nil, fmt.Errorf("VectorcallDict: nargs %d < 0", nargs)
	}
	fn := vectorcallFunction(callable)
	if fn == nil {
		var keywords Object
		if kwargs != nil && kwargs.Len() > 0 {
			keywords = kwargs
		}
		return MakeTpCall(callable, args, nargs, keywords)
	}
	if kwargs == nil || kwargs.Len() == 0 {
		return fn(callable, args[:nargs], nargsf, nil)
	}
	keys := kwargs.Keys()
	stack := make([]Object, nargs+len(keys))
	if nargs > 0 {
		copy(stack, args[:nargs])
	}
	kwnamesItems := make([]Object, 0, len(keys))
	for i, k := range keys {
		v, err := kwargs.GetItem(k)
		if err != nil {
			return nil, err
		}
		stack[nargs+i] = v
		kwnamesItems = append(kwnamesItems, k)
	}
	kwnames := NewTuple(kwnamesItems)
	return fn(callable, stack, uint(nargs)|VectorcallArgumentsOffset, kwnames)
}

// VectorcallPrepend prepends arg to the positional portion of args
// and dispatches the result. CPython exploits the
// VectorcallArgumentsOffset hint to overwrite args[-1] in place; Go
// slices can't address the slot before a sub-slice's start, so gopy
// always allocates a fresh stack regardless of the OFFSET bit. The
// behavioral contract matches; only the alloc-saving fast path is
// missing.
//
// CPython: Objects/call.c:829 _PyObject_VectorcallPrepend
func VectorcallPrepend(callable Object, arg Object, args []Object, nargsf uint, kwnames *Tuple) (Object, error) {
	nargs := VectorcallNargs(nargsf)
	if nargs < 0 {
		return nil, fmt.Errorf("VectorcallPrepend: nargs %d < 0", nargs)
	}
	nkw := kwnamesLen(kwnames)
	stack := make([]Object, 1+nargs+nkw)
	stack[0] = arg
	if nargs+nkw > 0 {
		copy(stack[1:], args[:nargs+nkw])
	}
	return Vectorcall(callable, stack, uint(nargs+1), kwnames)
}

// CallPrepend mirrors _PyObject_Call_Prepend: builds a fresh args
// stack with obj at slot 0 followed by the positional tuple, then
// dispatches via VectorcallDict so the keyword dict is honored
// without first materializing it as kwnames.
//
// CPython: Objects/call.c:478 _PyObject_Call_Prepend
func CallPrepend(callable Object, obj Object, args *Tuple, kwargs *Dict) (Object, error) {
	if args == nil {
		args = NewTuple(nil)
	}
	argcount := args.Len()
	stack := make([]Object, argcount+1)
	stack[0] = obj
	for i := 0; i < argcount; i++ {
		stack[i+1] = args.Item(i)
	}
	return VectorcallDict(callable, stack, uint(argcount+1), kwargs)
}

// objectIsNotCallable formats the canonical TypeError raised when a
// callable lacks both vectorcall and tp_call.
//
// CPython: Objects/call.c:162 object_is_not_callable
func objectIsNotCallable(callable Object) error {
	return fmt.Errorf("TypeError: '%s' object is not callable", callable.Type().Name)
}

// dictToMap copies a *Dict's entries into the map[string]Object shape
// gopy's tp_call slot expects. Used when MakeTpCall has to bridge a
// dict-flavored kwargs onto the slot.
func dictToMap(d *Dict) map[string]Object {
	out := make(map[string]Object, d.Len())
	for _, k := range d.Keys() {
		v, err := d.GetItem(k)
		if err != nil {
			continue
		}
		ks, err := Str(k)
		if err != nil {
			continue
		}
		out[ks] = v
	}
	return out
}
