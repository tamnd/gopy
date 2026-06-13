// Built-in methods exposed on array.array instances. The wire-up
// mirrors CPython's array_methods PyMethodDef table plus the two
// getsets for typecode / itemsize. Each method dispatches to the
// shared in-place helpers in array.go / seq.go so the public method
// and the corresponding sequence-protocol slot stay in lockstep.
//
// CPython: Modules/arraymodule.c:2447 array_methods

package array

import (
	"errors"
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// registerArrayMethods installs every method and getset descriptor on
// t. Called from array.go's init() once the type and sequence/mapping
// slots are fully wired.
//
// CPython: Modules/arraymodule.c:3231 array_modexec (PyType_FromModuleAndSpec)
func registerArrayMethods(t *objects.Type) {
	// conv carries the clinic METH flag so methodDescrCheckArity formats
	// the arity TypeError through _PyObject_FunctionStr, yielding
	// "array.byteswap() takes no arguments (N given)" / "array.append()
	// takes exactly one argument (N given)". METH_VARARGS rows (extend,
	// index, insert, pop, __reduce_ex__) keep their hand-rolled messages.
	//
	// CPython: Modules/clinic/arraymodule.c.h array_methods flags
	methods := []struct {
		name string
		conv objects.MethFlag
		fn   func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error)
	}{
		{"append", objects.MethO, arrayMethodAppend},
		{"buffer_info", objects.MethNoArgs, arrayMethodBufferInfo},
		{"byteswap", objects.MethNoArgs, arrayMethodByteswap},
		{"clear", objects.MethNoArgs, arrayMethodClear},
		{"__copy__", objects.MethNoArgs, arrayMethodCopy},
		{"__deepcopy__", objects.MethO, arrayMethodDeepcopy},
		{"count", objects.MethO, arrayMethodCount},
		{"extend", 0, arrayMethodExtend},
		{"frombytes", objects.MethO, arrayMethodFrombytes},
		{"fromlist", objects.MethO, arrayMethodFromlist},
		{"fromunicode", objects.MethO, arrayMethodFromunicode},
		{"index", 0, arrayMethodIndex},
		{"insert", 0, arrayMethodInsert},
		{"pop", 0, arrayMethodPop},
		{"__reduce_ex__", 0, arrayMethodReduceEx},
		{"remove", objects.MethO, arrayMethodRemove},
		{"reverse", objects.MethNoArgs, arrayMethodReverse},
		{"tobytes", objects.MethNoArgs, arrayMethodTobytes},
		{"tolist", objects.MethNoArgs, arrayMethodTolist},
		{"tounicode", objects.MethNoArgs, arrayMethodTounicode},
		{"__sizeof__", objects.MethNoArgs, arrayMethodSizeof},
	}
	for _, m := range methods {
		if m.conv == 0 {
			objects.SetTypeDescr(t, m.name, objects.NewMethodDescr(t, m.name, m.fn))
			continue
		}
		objects.SetTypeDescr(t, m.name, objects.NewMethodDescrConv(t, m.name, m.conv, m.fn))
	}
	objects.SetTypeDescr(t, "typecode", objects.NewGetSetDescr("typecode", arrayGetTypecode, nil))
	objects.SetTypeDescr(t, "itemsize", objects.NewGetSetDescr("itemsize", arrayGetItemsize, nil))
	// PEP 585: array.__class_getitem__ is METH_O|METH_CLASS in CPython, so
	// subscripting the type (array[int]) binds the class as the first
	// argument. Wrap in a classmethod rather than an instance MethodDescr.
	objects.SetTypeDescr(t, "__class_getitem__", objects.NewClassMethod(objects.NewBuiltinFunction("__class_getitem__", arrayClassGetitem)))
}

// --- helpers ----------------------------------------------------------------

func selfArray(args []objects.Object, name string) (*arrayObject, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: descriptor '%s' of 'array.array' object needs an argument", name)
	}
	a, ok := args[0].(*arrayObject)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '%s' for 'array.array' objects doesn't apply to a '%s' object", name, args[0].Type().Name)
	}
	return a, nil
}

func checkArgCount(args []objects.Object, name string, want int) error {
	got := len(args) - 1
	if got != want {
		return fmt.Errorf("TypeError: %s() takes exactly %d argument%s (%d given)", name, want, plural(want), got)
	}
	return nil
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// --- methods ----------------------------------------------------------------

// CPython: Modules/arraymodule.c:1485 array_array_append_impl
func arrayMethodAppend(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	a, err := selfArray(args, "append")
	if err != nil {
		return nil, err
	}
	if err := checkArgCount(args, "append", 1); err != nil {
		return nil, err
	}
	if err := arrayAppendOne(a, args[1]); err != nil {
		return nil, err
	}
	return objects.None(), nil
}

// CPython: Modules/arraymodule.c:1449 array_array_buffer_info_impl
func arrayMethodBufferInfo(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	a, err := selfArray(args, "buffer_info")
	if err != nil {
		return nil, err
	}
	if err := checkArgCount(args, "buffer_info", 0); err != nil {
		return nil, err
	}
	// CPython exposes the raw pointer; gopy has no such C-level address
	// so we return 0, matching what callers that only need the length
	// half of the tuple do anyway.
	return objects.NewTuple([]objects.Object{
		objects.NewInt(0),
		objects.NewInt(int64(a.count)),
	}), nil
}

// CPython: Modules/arraymodule.c:1501 array_array_byteswap_impl
func arrayMethodByteswap(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	a, err := selfArray(args, "byteswap")
	if err != nil {
		return nil, err
	}
	if err := checkArgCount(args, "byteswap", 0); err != nil {
		return nil, err
	}
	switch a.descr.itemsize {
	case 1:
		// no-op
	case 2:
		for i := 0; i < a.count; i++ {
			s := a.itemSlice(i)
			s[0], s[1] = s[1], s[0]
		}
	case 4:
		for i := 0; i < a.count; i++ {
			s := a.itemSlice(i)
			s[0], s[1], s[2], s[3] = s[3], s[2], s[1], s[0]
		}
	case 8:
		for i := 0; i < a.count; i++ {
			s := a.itemSlice(i)
			s[0], s[1], s[2], s[3], s[4], s[5], s[6], s[7] = s[7], s[6], s[5], s[4], s[3], s[2], s[1], s[0]
		}
	default:
		return nil, fmt.Errorf("RuntimeError: don't know how to byteswap this array type")
	}
	return objects.None(), nil
}

// CPython: Modules/arraymodule.c:962 array_array_clear_impl
func arrayMethodClear(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	a, err := selfArray(args, "clear")
	if err != nil {
		return nil, err
	}
	if err := checkArgCount(args, "clear", 0); err != nil {
		return nil, err
	}
	if err := arrayResize(a, 0); err != nil {
		return nil, err
	}
	return objects.None(), nil
}

// CPython: Modules/arraymodule.c:978 array_array___copy___impl
func arrayMethodCopy(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	a, err := selfArray(args, "__copy__")
	if err != nil {
		return nil, err
	}
	return arraySliceCopy(a)
}

// CPython: Modules/arraymodule.c:994 array_array___deepcopy___impl
func arrayMethodDeepcopy(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	a, err := selfArray(args, "__deepcopy__")
	if err != nil {
		return nil, err
	}
	// deepcopy on a typed-buffer array is the same as copy: there are
	// no aliased references inside the buffer.
	return arraySliceCopy(a)
}

// arraySliceCopy returns a fresh array carrying the same typecode and
// contents as a. Mirrors array_slice with start=0, stop=Py_SIZE(self).
//
// CPython: Modules/arraymodule.c:935 array_slice
func arraySliceCopy(a *arrayObject) (objects.Object, error) {
	out, err := newArrayObject(a.Type(), a.count, a.descr)
	if err != nil {
		return nil, err
	}
	copy(out.obItem, a.obItem[:a.nbytes()])
	return out, nil
}

// CPython: Modules/arraymodule.c:1239 array_array_count_impl
func arrayMethodCount(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	a, err := selfArray(args, "count")
	if err != nil {
		return nil, err
	}
	if err := checkArgCount(args, "count", 1); err != nil {
		return nil, err
	}
	v := args[1]
	count := int64(0)
	for i := 0; i < a.count; i++ {
		item, err := getarrayitem(a, i)
		if err != nil {
			return nil, err
		}
		eq, err := objects.RichCmpBool(item, v, objects.CompareEQ)
		if err != nil {
			return nil, err
		}
		if eq {
			count++
		}
	}
	return objects.NewInt(count), nil
}

// CPython: Modules/arraymodule.c:1412 array_array_extend_impl
func arrayMethodExtend(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	a, err := selfArray(args, "extend")
	if err != nil {
		return nil, err
	}
	if err := checkArgCount(args, "extend", 1); err != nil {
		return nil, err
	}
	other := args[1]
	if oa, ok := other.(*arrayObject); ok {
		if oa.descr != a.descr {
			return nil, errors.New("TypeError: can only extend with array of same kind")
		}
		if err := arrayExtendArray(a, oa); err != nil {
			return nil, err
		}
		return objects.None(), nil
	}
	// Generic iterable path.
	it, err := objects.Iter(other)
	if err != nil {
		return nil, err
	}
	for {
		next, nerr := objects.IterNext(it)
		if nerr != nil {
			if errors.Is(nerr, objects.ErrStopIteration) {
				break
			}
			return nil, nerr
		}
		if err := arrayAppendOne(a, next); err != nil {
			return nil, err
		}
	}
	return objects.None(), nil
}

// CPython: Modules/arraymodule.c:1814 array_array_frombytes_impl
func arrayMethodFrombytes(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	a, err := selfArray(args, "frombytes")
	if err != nil {
		return nil, err
	}
	if err := checkArgCount(args, "frombytes", 1); err != nil {
		return nil, err
	}
	var buf []byte
	switch v := args[1].(type) {
	case *objects.Bytes:
		buf = v.Bytes()
	case *objects.ByteArray:
		buf = v.Bytes()
	default:
		return nil, fmt.Errorf("TypeError: a bytes-like object is required, not '%s'", args[1].Type().Name)
	}
	if err := arrayFrombytesRaw(a, buf); err != nil {
		return nil, err
	}
	return objects.None(), nil
}

// CPython: Modules/arraymodule.c:1707 array_array_fromlist_impl
func arrayMethodFromlist(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	a, err := selfArray(args, "fromlist")
	if err != nil {
		return nil, err
	}
	if err := checkArgCount(args, "fromlist", 1); err != nil {
		return nil, err
	}
	list, ok := args[1].(*objects.List)
	if !ok {
		return nil, errors.New("TypeError: arg must be list")
	}
	n := list.Len()
	if n == 0 {
		return objects.None(), nil
	}
	oldCount := a.count
	if err := arrayResize(a, oldCount+n); err != nil {
		return nil, err
	}
	for i := 0; i < n; i++ {
		if err := setarrayitem(a, oldCount+i, list.Item(i)); err != nil {
			_ = arrayResize(a, oldCount)
			return nil, err
		}
	}
	return objects.None(), nil
}

// CPython: Modules/arraymodule.c:1852 array_array_fromunicode_impl
func arrayMethodFromunicode(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	a, err := selfArray(args, "fromunicode")
	if err != nil {
		return nil, err
	}
	if err := checkArgCount(args, "fromunicode", 1); err != nil {
		return nil, err
	}
	s, ok := args[1].(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: fromunicode() argument must be str, not %s", args[1].Type().Name)
	}
	if err := arrayFromunicodeRaw(a, s.Value()); err != nil {
		return nil, err
	}
	return objects.None(), nil
}

// CPython: Modules/arraymodule.c:1277 array_array_index_impl
func arrayMethodIndex(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	a, err := selfArray(args, "index")
	if err != nil {
		return nil, err
	}
	n := len(args) - 1
	if n < 1 || n > 3 {
		return nil, fmt.Errorf("TypeError: index() takes 1 to 3 arguments (%d given)", n)
	}
	v := args[1]
	start := 0
	stop := a.count
	if n >= 2 {
		s, err := indexAsInt(args[2])
		if err != nil {
			return nil, err
		}
		if s < 0 {
			s += a.count
			if s < 0 {
				s = 0
			}
		}
		start = s
	}
	if n == 3 {
		s, err := indexAsInt(args[3])
		if err != nil {
			return nil, err
		}
		if s < 0 {
			s += a.count
		}
		stop = s
	}
	for i := start; i < stop && i < a.count; i++ {
		item, err := getarrayitem(a, i)
		if err != nil {
			return nil, err
		}
		eq, err := objects.RichCmpBool(item, v, objects.CompareEQ)
		if err != nil {
			return nil, err
		}
		if eq {
			return objects.NewInt(int64(i)), nil
		}
	}
	return nil, errors.New("ValueError: array.index(x): x not in array")
}

// CPython: Modules/arraymodule.c:1433 array_array_insert_impl
func arrayMethodInsert(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	a, err := selfArray(args, "insert")
	if err != nil {
		return nil, err
	}
	if err := checkArgCount(args, "insert", 2); err != nil {
		return nil, err
	}
	where, err := indexAsInt(args[1])
	if err != nil {
		return nil, err
	}
	v := args[2]
	// Validate via setitem before resizing, matching ins1's i=-1 call.
	if err := setarrayitem(a, -1, v); err != nil {
		return nil, err
	}
	n := a.count
	if err := arrayResize(a, n+1); err != nil {
		return nil, err
	}
	if where < 0 {
		where += n
		if where < 0 {
			where = 0
		}
	}
	if where > n {
		where = n
	}
	if where != n {
		itemsize := a.descr.itemsize
		copy(a.obItem[(where+1)*itemsize:(n+1)*itemsize], a.obItem[where*itemsize:n*itemsize])
	}
	if err := setarrayitem(a, where, v); err != nil {
		return nil, err
	}
	return objects.None(), nil
}

// CPython: Modules/arraymodule.c:1375 array_array_pop_impl
func arrayMethodPop(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	a, err := selfArray(args, "pop")
	if err != nil {
		return nil, err
	}
	n := len(args) - 1
	if n > 1 {
		return nil, fmt.Errorf("TypeError: pop() takes at most 1 argument (%d given)", n)
	}
	i := -1
	if n == 1 {
		idx, err := indexAsInt(args[1])
		if err != nil {
			return nil, err
		}
		i = idx
	}
	if a.count == 0 {
		return nil, errors.New("IndexError: pop from empty array")
	}
	if i < 0 {
		i += a.count
	}
	if i < 0 || i >= a.count {
		return nil, errors.New("IndexError: pop index out of range")
	}
	v, err := getarrayitem(a, i)
	if err != nil {
		return nil, err
	}
	if err := arrayDeleteAt(a, i); err != nil {
		return nil, err
	}
	return v, nil
}

// CPython: Modules/arraymodule.c:2346 array_array___reduce_ex___impl
// gopy ports only the protocol<3 / list-based path because the
// _array_reconstructor helper (used by protocols 3+) lives in
// Lib/array.py and roundtrips through the C-side mformat code table.
// The list-based form is byte-for-byte compatible with CPython's own
// fallback path.
func arrayMethodReduceEx(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	a, err := selfArray(args, "__reduce_ex__")
	if err != nil {
		return nil, err
	}
	if err := checkArgCount(args, "__reduce_ex__", 1); err != nil {
		return nil, err
	}
	listObj, err := arrayMethodTolist([]objects.Object{a}, nil)
	if err != nil {
		return nil, err
	}
	typecodeStr := objects.NewStr(string([]byte{a.descr.typecode}))
	ctorArgs := objects.NewTuple([]objects.Object{typecodeStr, listObj})
	return objects.NewTuple([]objects.Object{
		a.Type(),
		ctorArgs,
		objects.None(),
	}), nil
}

// CPython: Modules/arraymodule.c:1337 array_array_remove_impl
func arrayMethodRemove(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	a, err := selfArray(args, "remove")
	if err != nil {
		return nil, err
	}
	if err := checkArgCount(args, "remove", 1); err != nil {
		return nil, err
	}
	v := args[1]
	for i := 0; i < a.count; i++ {
		item, err := getarrayitem(a, i)
		if err != nil {
			return nil, err
		}
		eq, err := objects.RichCmpBool(item, v, objects.CompareEQ)
		if err != nil {
			return nil, err
		}
		if eq {
			if err := arrayDeleteAt(a, i); err != nil {
				return nil, err
			}
			return objects.None(), nil
		}
	}
	return nil, errors.New("ValueError: array.remove(x): x not in array")
}

// CPython: Modules/arraymodule.c:1558 array_array_reverse_impl
func arrayMethodReverse(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	a, err := selfArray(args, "reverse")
	if err != nil {
		return nil, err
	}
	if err := checkArgCount(args, "reverse", 0); err != nil {
		return nil, err
	}
	if a.count < 2 {
		return objects.None(), nil
	}
	itemsize := a.descr.itemsize
	tmp := make([]byte, itemsize)
	for i, j := 0, a.count-1; i < j; i, j = i+1, j-1 {
		left := a.itemSlice(i)
		right := a.itemSlice(j)
		copy(tmp, left)
		copy(left, right)
		copy(right, tmp)
	}
	return objects.None(), nil
}

// CPython: Modules/arraymodule.c:1827 array_array_tobytes_impl
func arrayMethodTobytes(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	a, err := selfArray(args, "tobytes")
	if err != nil {
		return nil, err
	}
	if err := checkArgCount(args, "tobytes", 0); err != nil {
		return nil, err
	}
	return objects.NewBytes(append([]byte(nil), a.obItem[:a.nbytes()]...)), nil
}

// CPython: Modules/arraymodule.c:1747 array_array_tolist_impl
func arrayMethodTolist(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	a, err := selfArray(args, "tolist")
	if err != nil {
		return nil, err
	}
	if err := checkArgCount(args, "tolist", 0); err != nil {
		return nil, err
	}
	out := make([]objects.Object, a.count)
	for i := 0; i < a.count; i++ {
		v, err := getarrayitem(a, i)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return objects.NewList(out), nil
}

// CPython: Modules/arraymodule.c:1911 array_array_tounicode_impl
func arrayMethodTounicode(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	a, err := selfArray(args, "tounicode")
	if err != nil {
		return nil, err
	}
	if err := checkArgCount(args, "tounicode", 0); err != nil {
		return nil, err
	}
	s, err := arrayTounicodeRaw(a)
	if err != nil {
		return nil, err
	}
	return objects.NewStr(s), nil
}

// CPython: Modules/arraymodule.c:1937 array_array___sizeof___impl
func arrayMethodSizeof(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	a, err := selfArray(args, "__sizeof__")
	if err != nil {
		return nil, err
	}
	if err := checkArgCount(args, "__sizeof__", 0); err != nil {
		return nil, err
	}
	// Fixed header overhead + per-item allocation. The exact header
	// width is the size of the Go struct; gopy estimates 80 bytes to
	// match the order of magnitude CPython reports.
	const headerBytes = 80
	total := int64(headerBytes + a.allocated*a.descr.itemsize)
	return objects.NewInt(total), nil
}

// arrayClassGetitem implements array.__class_getitem__, the PEP 585
// generic-alias hook. Mirrors Py_GenericAlias used in the C table.
//
// CPython: Modules/arraymodule.c:2471 array_methods {"__class_getitem__", ...}
func arrayClassGetitem(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, errors.New("TypeError: __class_getitem__() missing 1 required positional argument")
	}
	return objects.NewGenericAlias(args[0], args[1]), nil
}

// --- getsets ----------------------------------------------------------------

// CPython: Modules/arraymodule.c:2424 array_get_typecode
func arrayGetTypecode(o objects.Object) (objects.Object, error) {
	a := o.(*arrayObject)
	return objects.NewStr(string([]byte{a.descr.typecode})), nil
}

// CPython: Modules/arraymodule.c:2432 array_get_itemsize
func arrayGetItemsize(o objects.Object) (objects.Object, error) {
	a := o.(*arrayObject)
	return objects.NewInt(int64(a.descr.itemsize)), nil
}
