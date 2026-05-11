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

	bind := func(name string, fn func(args []Object, kwargs map[string]Object) (Object, error)) {
		SetTypeDescr(ListType, name, NewMethodDescr(ListType, name, fn))
	}

	bind("append", listAppendMethod)
	bind("extend", listExtendMethod)
	bind("insert", listInsertMethod)
	bind("remove", listRemoveMethod)
	bind("pop", listPopMethod)
	bind("clear", listClearMethod)
	bind("reverse", listReverseMethod)
	bind("copy", listCopyMethod)
	bind("index", listIndexMethod)
	bind("count", listCountMethod)
	bind("sort", listSortMethod)
	bind("__len__", listLenMethod)
	bind("__contains__", listContainsMethod)
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
			if b, ok := v.(*Bool); ok {
				reverse = b == True()
			} else if i, ok := v.(*Int); ok {
				n, _ := i.Int64()
				reverse = n != 0
			} else {
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
