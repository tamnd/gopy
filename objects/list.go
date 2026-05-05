package objects

import "strings"

// List is the Python list, a mutable ordered sequence.
//
// CPython: Include/cpython/listobject.h:L7 PyListObject
type List struct {
	VarHeader
	items []Object
}

// ListType is the type singleton for list. Mirrors PyList_Type.
//
// CPython: Objects/listobject.c:L3380 PyList_Type
var ListType = NewType("list", []*Type{objectType})

func init() {
	ListType.Repr = listRepr
	ListType.Str = listRepr
	ListType.RichCmp = listRichCmp
	ListType.Iter = listIter
	ListType.Sequence = &SequenceMethods{
		Length:  listLen,
		GetItem: listGetItem,
		SetItem: listSetItem,
	}
}

// NewList builds a list from items. The slice is copied.
//
// CPython: Objects/listobject.c:L156 PyList_New
func NewList(items []Object) *List {
	l := &List{items: append([]Object(nil), items...)}
	l.init(ListType)
	l.size = int64(len(items))
	return l
}

// Len returns the number of items.
//
// CPython: Objects/listobject.c:L286 PyList_Size
func (l *List) Len() int { return len(l.items) }

// Append adds v to the end. Mirrors PyList_Append.
//
// CPython: Objects/listobject.c:L351 PyList_Append
func (l *List) Append(v Object) {
	l.items = append(l.items, v)
	l.size = int64(len(l.items))
}

// Item returns the item at index i without bounds check.
//
// CPython: Objects/listobject.c:L308 PyList_GetItem
func (l *List) Item(i int) Object { return l.items[i] }

// SetSlice replaces items[start:stop] with values. CPython implements
// this through PyList_SetSlice; the gopy port keeps the contract
// (start and stop already clamped/normalized by the caller).
//
// CPython: Objects/listobject.c PyList_SetSlice
func (l *List) SetSlice(start, stop int, values []Object) {
	tail := append([]Object(nil), l.items[stop:]...)
	l.items = append(append(l.items[:start], values...), tail...)
	l.size = int64(len(l.items))
}

func listLen(o Object) (int, error) {
	return o.(*List).Len(), nil
}

func listGetItem(o Object, i int) (Object, error) {
	l := o.(*List)
	if i < 0 {
		i += len(l.items)
	}
	if i < 0 || i >= len(l.items) {
		return nil, errIndexOutOfRange
	}
	return l.items[i], nil
}

func listSetItem(o Object, i int, v Object) error {
	l := o.(*List)
	if i < 0 {
		i += len(l.items)
	}
	if i < 0 || i >= len(l.items) {
		return errIndexOutOfRange
	}
	l.items[i] = v
	return nil
}

func listRepr(o Object) (string, error) {
	l := o.(*List)
	var b strings.Builder
	b.WriteByte('[')
	for i, it := range l.items {
		if i > 0 {
			b.WriteString(", ")
		}
		s, err := Repr(it)
		if err != nil {
			return "", err
		}
		b.WriteString(s)
	}
	b.WriteByte(']')
	return b.String(), nil
}

func listRichCmp(a, b Object, op CompareOp) (Object, error) {
	al, ok := a.(*List)
	if !ok {
		return notImplemented(), nil
	}
	bl, ok := b.(*List)
	if !ok {
		return notImplemented(), nil
	}
	switch op {
	case CompareEQ, CompareNE:
		eq := len(al.items) == len(bl.items)
		if eq {
			for i := range al.items {
				ok, err := RichCmpBool(al.items[i], bl.items[i], CompareEQ)
				if err != nil {
					return nil, err
				}
				if !ok {
					eq = false
					break
				}
			}
		}
		if op == CompareNE {
			eq = !eq
		}
		return NewBool(eq), nil
	}
	return notImplemented(), nil
}

// listIterator is the iterator returned by iter(list).
//
// CPython: Objects/listobject.c:L3539 PyListIter_Type
type listIterator struct {
	Header
	src *List
	pos int
}

var listIterType = NewType("list_iterator", []*Type{objectType})

func init() {
	listIterType.Iter = func(o Object) (Object, error) { return o, nil }
	listIterType.IterNext = func(o Object) (Object, error) {
		it := o.(*listIterator)
		if it.pos >= len(it.src.items) {
			return nil, ErrStopIteration
		}
		v := it.src.items[it.pos]
		it.pos++
		return v, nil
	}
}

func listIter(o Object) (Object, error) {
	it := &listIterator{src: o.(*List)}
	it.init(listIterType)
	return it, nil
}
