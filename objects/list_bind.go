// Bind list method descriptors. The Go-side helpers already implement
// every list method; this file wires each one onto ListType so Python
// code can call `lst.append(x)` etc. via attribute lookup.
//
// CPython: Objects/listobject.c:3308 list_methods

package objects

import (
	"errors"
	"fmt"
)

func init() {
	ListType.Getattro = GenericGetAttr

	// bindConv tags the descriptor with the matching METH_* flag so
	// specialize_method_descriptor picks the right CALL_METHOD_DESCRIPTOR_*
	// arm. The wrapper closure still receives args as a slice (self
	// followed by user args), so only the specializer hint differs.
	//
	// CPython: Objects/clinic/listobject.c.h list_methods table.
	bindConv := func(name string, conv MethFlag, fn func(args []Object, kwargs map[string]Object) (Object, error)) *MethodDescr {
		d := NewMethodDescrConv(ListType, name, conv, fn)
		SetTypeDescr(ListType, name, d)
		return d
	}

	// bindWrap installs a slot wrapper (wrapper_descriptor) rather than a
	// method_descriptor. add_operators produces one for every tp_*/sq_*/mp_*
	// slot list fills in, so type(list.__add__) is wrapper_descriptor and
	// l.__add__ binds to a method-wrapper. The closure rebuilds the
	// (self, *args) stack the existing METH-style helper expects.
	//
	// CPython: Objects/typeobject.c add_operators (slotdefs slot wrappers)
	bindWrap := func(name, sig, doc string, fn func(args []Object, kwargs map[string]Object) (Object, error)) {
		setWrapperIfAbsent(ListType, name, sig, doc, func(self Object, args []Object) (Object, error) {
			full := make([]Object, 0, len(args)+1)
			full = append(full, self)
			full = append(full, args...)
			return fn(full, nil)
		})
	}

	// list.__repr__ slot wrapper (tp_repr).
	//
	// CPython: Objects/typeobject.c add_operators slot wrapper for tp_repr
	bindWrap("__repr__", "($self, /)", "Return repr(self).", listReprMethod)

	// METH_O rows: append, extend, remove, count. append additionally
	// gets registered into the callable cache so the specializer can
	// emit CALL_LIST_APPEND on identity match.
	appendDescr := bindConv("append", MethO, listAppendMethod)
	RegisterCallableCacheListAppend(appendDescr)
	bindConv("extend", MethO, listExtendMethod)
	bindConv("remove", MethO, listRemoveMethod)
	bindConv("count", MethO, listCountMethod)
	bindWrap("__contains__", "($self, key, /)", "Return bool(key in self).", listContainsMethod)

	// METH_FASTCALL rows: insert (2 args), index (1-3 args), pop (0-1 args).
	bindConv("insert", MethFastcall, listInsertMethod)
	bindConv("index", MethFastcall, listIndexMethod)
	bindConv("pop", MethFastcall, listPopMethod)

	// METH_FASTCALL|METH_KEYWORDS: sort (kwargs key= / reverse=).
	bindConv("sort", MethFastcall|MethKeywords, listSortMethod)

	// METH_NOARGS rows: clear, reverse, copy.
	bindConv("clear", MethNoArgs, listClearMethod)
	bindConv("reverse", MethNoArgs, listReverseMethod)
	bindConv("copy", MethNoArgs, listCopyMethod)
	bindWrap("__len__", "($self, /)", "Return len(self).", listLenMethod)

	// Slot wrappers for the sequence/mapping/richcompare dunders. CPython
	// generates these via add_operators -> slotdefs. __getitem__ and
	// __reversed__ stay method_descriptors because list defines them as real
	// METH_O/METH_NOARGS rows, not as bare slots.
	//
	// CPython: Objects/typeobject.c add_operators (sq_concat, sq_repeat,
	// sq_inplace_concat, sq_inplace_repeat, mp_ass_subscript, tp_iter,
	// tp_richcompare)
	bindConv("__getitem__", MethO, listGetItemMethod)
	bindConv("__reversed__", MethNoArgs, listReversedMethod)
	bindWrap("__setitem__", "($self, key, value, /)", "Set self[key] to value.", listSetItemMethod)
	bindWrap("__delitem__", "($self, key, /)", "Delete self[key].", listDelItemMethod)
	bindWrap("__add__", "($self, value, /)", "Return self+value.", listAddMethod)
	bindWrap("__mul__", "($self, value, /)", "Return self*value.", listMulMethod)
	bindWrap("__rmul__", "($self, value, /)", "Return value*self.", listMulMethod)
	bindWrap("__iadd__", "($self, value, /)", "Implement self+=value.", listIAddMethod)
	bindWrap("__imul__", "($self, value, /)", "Implement self*=value.", listIMulMethod)
	bindWrap("__iter__", "($self, /)", "Implement iter(self).", listIterMethod)
	bindWrap("__eq__", "($self, value, /)", "Return self==value.", listEqMethod)
	bindWrap("__ne__", "($self, value, /)", "Return self!=value.", listNeMethod)
	bindWrap("__lt__", "($self, value, /)", "Return self<value.", listLtMethod)
	bindWrap("__le__", "($self, value, /)", "Return self<=value.", listLeMethod)
	bindWrap("__gt__", "($self, value, /)", "Return self>value.", listGtMethod)
	bindWrap("__ge__", "($self, value, /)", "Return self>=value.", listGeMethod)
}

// listGetItemMethod backs list.__getitem__. Routes through
// listMappingGet which handles both int (sq_item) and slice
// (mp_subscript) keys.
//
// CPython: Objects/listobject.c:3216 list_as_mapping (mp_subscript)
func listGetItemMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __getitem__() takes exactly one argument (%d given)", len(args)-1)
	}
	return listMappingGet(args[0], args[1])
}

// listSetItemMethod backs list.__setitem__(key, value).
//
// CPython: Objects/listobject.c list_ass_subscript
func listSetItemMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 3 {
		return nil, fmt.Errorf("TypeError: __setitem__() takes exactly 2 arguments (%d given)", len(args)-1)
	}
	if err := listMappingSet(args[0], args[1], args[2]); err != nil {
		return nil, err
	}
	return None(), nil
}

// listDelItemMethod backs list.__delitem__(key).
//
// CPython: Objects/listobject.c list_ass_subscript (delete branch)
func listDelItemMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __delitem__() takes exactly one argument (%d given)", len(args)-1)
	}
	if err := listMappingDel(args[0], args[1]); err != nil {
		return nil, err
	}
	return None(), nil
}

// listAddMethod backs list.__add__(other). Returns NotImplemented when
// other is not a list, matching list_concat's PyList_Check guard.
//
// CPython: Objects/listobject.c:520 list_concat
func listAddMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __add__() takes exactly one argument (%d given)", len(args)-1)
	}
	if _, ok := args[1].(*List); !ok {
		return NotImplemented(), nil
	}
	return listConcat(args[0], args[1])
}

// listMulMethod backs list.__mul__ / list.__rmul__. n is coerced via
// PyNumber_Index, mirroring list_repeat's PyIndex_Check guard.
//
// CPython: Objects/listobject.c:551 list_repeat
func listMulMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __mul__() takes exactly one argument (%d given)", len(args)-1)
	}
	idx, err := NumberIndex(args[1])
	if err != nil {
		return NotImplemented(), nil //nolint:nilerr // mirrors Py_NotImplemented return when other can't be coerced to an index
	}
	n, ok := idx.(*Int)
	if !ok {
		return NotImplemented(), nil
	}
	v, ok := n.Int64()
	if !ok {
		return nil, errors.New("OverflowError: cannot fit 'int' into an index-sized integer")
	}
	return listRepeat(args[0], int(v))
}

// listIAddMethod backs list.__iadd__(other).
//
// CPython: Objects/listobject.c list_inplace_concat
func listIAddMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __iadd__() takes exactly one argument (%d given)", len(args)-1)
	}
	return listInPlaceConcat(args[0], args[1])
}

// listIMulMethod backs list.__imul__(n). Coerces via __index__.
//
// CPython: Objects/listobject.c list_inplace_repeat
func listIMulMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __imul__() takes exactly one argument (%d given)", len(args)-1)
	}
	idx, err := NumberIndex(args[1])
	if err != nil {
		return nil, err
	}
	n, ok := idx.(*Int)
	if !ok {
		return nil, errors.New("TypeError: __index__ did not return int")
	}
	v, ok := n.Int64()
	if !ok {
		return nil, errors.New("OverflowError: cannot fit 'int' into an index-sized integer")
	}
	return listInPlaceRepeat(args[0], int(v))
}

// listIterMethod backs list.__iter__.
func listIterMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __iter__() takes no arguments (%d given)", len(args)-1)
	}
	return listIter(args[0])
}

// listReversedMethod backs list.__reversed__ by allocating a real
// list_reverseiterator over the source list. The iterator keeps a
// reference to the source so pickle round-trips share identity and
// list mutations after pickle.loads are visible to the loaded
// iterator (test_reversed_pickle).
//
// CPython: Objects/listobject.c:4140 list___reversed___impl
func listReversedMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __reversed__() takes no arguments (%d given)", len(args)-1)
	}
	l, ok := args[0].(*List)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__reversed__' requires a 'list' object")
	}
	return listRevIter(l), nil
}

func listRichCmpMethod(name string, op CompareOp) func(args []Object, _ map[string]Object) (Object, error) {
	return func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("TypeError: %s() takes exactly one argument (%d given)", name, len(args)-1)
		}
		return listRichCmp(args[0], args[1], op)
	}
}

func listEqMethod(args []Object, kw map[string]Object) (Object, error) {
	return listRichCmpMethod("__eq__", CompareEQ)(args, kw)
}

func listNeMethod(args []Object, kw map[string]Object) (Object, error) {
	return listRichCmpMethod("__ne__", CompareNE)(args, kw)
}

func listLtMethod(args []Object, kw map[string]Object) (Object, error) {
	return listRichCmpMethod("__lt__", CompareLT)(args, kw)
}

func listLeMethod(args []Object, kw map[string]Object) (Object, error) {
	return listRichCmpMethod("__le__", CompareLE)(args, kw)
}

func listGtMethod(args []Object, kw map[string]Object) (Object, error) {
	return listRichCmpMethod("__gt__", CompareGT)(args, kw)
}

func listGeMethod(args []Object, kw map[string]Object) (Object, error) {
	return listRichCmpMethod("__ge__", CompareGE)(args, kw)
}

func selfList(args []Object, name string) (*List, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: descriptor '%s' of 'list' needs an argument", name)
	}
	l, ok := args[0].(*List)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '%s' requires a 'list' object", name)
	}
	return l, nil
}

func listAppendMethod(args []Object, _ map[string]Object) (Object, error) {
	l, err := selfList(args, "append")
	if err != nil {
		return nil, err
	}
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: append() takes exactly one argument (%d given)", len(args)-1)
	}
	l.Append(args[1])
	return None(), nil
}

func listExtendMethod(args []Object, _ map[string]Object) (Object, error) {
	l, err := selfList(args, "extend")
	if err != nil {
		return nil, err
	}
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: extend() takes exactly one argument (%d given)", len(args)-1)
	}
	if _, err := listInPlaceConcat(l, args[1]); err != nil {
		return nil, err
	}
	return None(), nil
}

func listInsertMethod(args []Object, _ map[string]Object) (Object, error) {
	l, err := selfList(args, "insert")
	if err != nil {
		return nil, err
	}
	if len(args) != 3 {
		return nil, fmt.Errorf("TypeError: insert() takes exactly 2 arguments (%d given)", len(args)-1)
	}
	idx, ierr := args[1].(*Int)
	if !ierr {
		return nil, fmt.Errorf("TypeError: 'index' must be an integer")
	}
	n, _ := idx.Int64()
	l.Insert(int(n), args[2])
	return None(), nil
}

func listRemoveMethod(args []Object, _ map[string]Object) (Object, error) {
	l, err := selfList(args, "remove")
	if err != nil {
		return nil, err
	}
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: remove() takes exactly one argument (%d given)", len(args)-1)
	}
	if err := l.Remove(args[1]); err != nil {
		return nil, err
	}
	return None(), nil
}

func listPopMethod(args []Object, _ map[string]Object) (Object, error) {
	l, err := selfList(args, "pop")
	if err != nil {
		return nil, err
	}
	// CPython: Objects/clinic/listobject.c.h list_pop accepts at most one
	// positional argument; _PyArg_CheckPositional reports the count
	// before the body runs (test_pop expects TypeError, not IndexError).
	if len(args) > 2 {
		return nil, fmt.Errorf("TypeError: list.pop() takes at most 1 argument (%d given)", len(args)-1)
	}
	i := -1
	if len(args) >= 2 {
		idx, ok := args[1].(*Int)
		if !ok {
			return nil, fmt.Errorf("TypeError: 'index' must be an integer")
		}
		n, _ := idx.Int64()
		i = int(n)
	}
	return l.Pop(i)
}

func listClearMethod(args []Object, _ map[string]Object) (Object, error) {
	l, err := selfList(args, "clear")
	if err != nil {
		return nil, err
	}
	l.Clear()
	return None(), nil
}

func listReverseMethod(args []Object, _ map[string]Object) (Object, error) {
	l, err := selfList(args, "reverse")
	if err != nil {
		return nil, err
	}
	l.Reverse()
	return None(), nil
}

func listCopyMethod(args []Object, _ map[string]Object) (Object, error) {
	l, err := selfList(args, "copy")
	if err != nil {
		return nil, err
	}
	return l.Copy(), nil
}

func listIndexMethod(args []Object, _ map[string]Object) (Object, error) {
	l, err := selfList(args, "index")
	if err != nil {
		return nil, err
	}
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: index() takes at least 1 argument")
	}
	// CPython: Objects/listobject.c:1430 list_index_impl uses the
	// _PyEval_SliceIndex converter so start / stop accept any
	// integer-like value, including ones larger than PY_SSIZE_T_MAX
	// (clamped to the ssize_t range).
	start := 0
	stop := len(l.items)
	if len(args) >= 3 {
		v, ierr := sliceIndex(args[2])
		if ierr != nil {
			return nil, ierr
		}
		start = v
	}
	if len(args) >= 4 {
		v, ierr := sliceIndex(args[3])
		if ierr != nil {
			return nil, ierr
		}
		stop = v
	}
	idx, ierr := l.Index(args[1], start, stop)
	if ierr != nil {
		return nil, ierr
	}
	return NewInt(int64(idx)), nil
}

func listCountMethod(args []Object, _ map[string]Object) (Object, error) {
	l, err := selfList(args, "count")
	if err != nil {
		return nil, err
	}
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: count() takes exactly one argument (%d given)", len(args)-1)
	}
	n, cerr := l.Count(args[1])
	if cerr != nil {
		return nil, cerr
	}
	return NewInt(int64(n)), nil
}

func listSortMethod(args []Object, kwargs map[string]Object) (Object, error) {
	l, err := selfList(args, "sort")
	if err != nil {
		return nil, err
	}
	if len(args) > 1 {
		return nil, errors.New("TypeError: sort() takes no positional arguments")
	}
	var keyfunc Object
	reverse := false
	for k, v := range kwargs {
		switch k {
		case "key":
			keyfunc = v
		case "reverse":
			switch x := v.(type) {
			case *Bool:
				reverse = x == True()
			case *Int:
				n, _ := x.Int64()
				reverse = n != 0
			default:
				reverse = v != nil && !IsNone(v)
			}
		default:
			return nil, fmt.Errorf("TypeError: sort() got an unexpected keyword argument '%s'", k)
		}
	}
	if serr := l.Sort(keyfunc, reverse); serr != nil {
		return nil, serr
	}
	return None(), nil
}

func listLenMethod(args []Object, _ map[string]Object) (Object, error) {
	l, err := selfList(args, "__len__")
	if err != nil {
		return nil, err
	}
	return NewInt(int64(l.Len())), nil
}

func listReprMethod(args []Object, _ map[string]Object) (Object, error) {
	l, err := selfList(args, "__repr__")
	if err != nil {
		return nil, err
	}
	s, rerr := listRepr(l)
	if rerr != nil {
		return nil, rerr
	}
	return NewStr(s), nil
}

func listContainsMethod(args []Object, _ map[string]Object) (Object, error) {
	l, err := selfList(args, "__contains__")
	if err != nil {
		return nil, err
	}
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __contains__() takes exactly one argument (%d given)", len(args)-1)
	}
	for _, it := range l.items {
		eq, cerr := RichCmpBool(it, args[1], CompareEQ)
		if cerr != nil {
			return nil, cerr
		}
		if eq {
			return True(), nil
		}
	}
	return False(), nil
}
