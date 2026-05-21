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

	// list.__repr__ slot wrapper. CPython exposes tp_repr via
	// add_operators -> slotdefs so callers can do `list.__repr__(L)`
	// and so pprint's dispatch table can key on it (otherwise the
	// table falls back to object.__repr__, which collides with the
	// dict/deque entries).
	//
	// CPython: Objects/typeobject.c add_operators slot wrapper for tp_repr
	bindConv("__repr__", MethNoArgs, listReprMethod)

	// METH_O rows: append, extend, remove, count. append additionally
	// gets registered into the callable cache so the specializer can
	// emit CALL_LIST_APPEND on identity match.
	appendDescr := bindConv("append", MethO, listAppendMethod)
	RegisterCallableCacheListAppend(appendDescr)
	bindConv("extend", MethO, listExtendMethod)
	bindConv("remove", MethO, listRemoveMethod)
	bindConv("count", MethO, listCountMethod)
	bindConv("__contains__", MethO, listContainsMethod)

	// METH_FASTCALL rows: insert (2 args), index (1-3 args), pop (0-1 args).
	bindConv("insert", MethFastcall, listInsertMethod)
	bindConv("index", MethFastcall, listIndexMethod)
	bindConv("pop", MethFastcall, listPopMethod)

	// METH_FASTCALL|METH_KEYWORDS: sort (kwargs key= / reverse=).
	bindConv("sort", MethFastcall|MethKeywords, listSortMethod)

	// METH_NOARGS rows: clear, reverse, copy, __len__.
	bindConv("clear", MethNoArgs, listClearMethod)
	bindConv("reverse", MethNoArgs, listReverseMethod)
	bindConv("copy", MethNoArgs, listCopyMethod)
	bindConv("__len__", MethNoArgs, listLenMethod)
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
	start := 0
	stop := len(l.items)
	if len(args) >= 3 {
		i, ok := args[2].(*Int)
		if !ok {
			return nil, fmt.Errorf("TypeError: 'start' must be an integer")
		}
		n, _ := i.Int64()
		start = int(n)
	}
	if len(args) >= 4 {
		i, ok := args[3].(*Int)
		if !ok {
			return nil, fmt.Errorf("TypeError: 'stop' must be an integer")
		}
		n, _ := i.Int64()
		stop = int(n)
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
