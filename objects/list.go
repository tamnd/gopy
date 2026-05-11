package objects

import (
	"errors"
	"fmt"
	"strings"
)

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
	ListType.TpFlags = TpFlagSequence
	ListType.Repr = listRepr
	ListType.Str = listRepr
	ListType.RichCmp = listRichCmp
	ListType.Iter = listIter
	ListType.Sequence = &SequenceMethods{
		Length:        listLen,
		Concat:        listConcat,
		Repeat:        listRepeat,
		GetItem:       listGetItem,
		SetItem:       listSetItem,
		InPlaceConcat: listInPlaceConcat,
		InPlaceRepeat: listInPlaceRepeat,
	}
	ListType.TpTraverse = listTraverse
}

// listTraverse visits every item. Mirrors list_traverse.
//
// CPython: Objects/listobject.c:2829 list_traverse
func listTraverse(o Object, visit Visitor) error {
	l := o.(*List)
	for _, it := range l.items {
		if it == nil {
			continue
		}
		if err := visit(it); err != nil {
			return err
		}
	}
	return nil
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

// listConcat ports list_concat: build a fresh list with a's items
// followed by b's. b must be a list; mismatched types raise TypeError
// like CPython does (not NotImplemented; the list slot itself rejects
// non-lists).
//
// CPython: Objects/listobject.c:541 list_concat
func listConcat(a, b Object) (Object, error) {
	la := a.(*List)
	lb, ok := b.(*List)
	if !ok {
		return nil, fmt.Errorf("TypeError: can only concatenate list (not \"%s\") to list", b.Type().Name)
	}
	out := make([]Object, 0, len(la.items)+len(lb.items))
	out = append(out, la.items...)
	out = append(out, lb.items...)
	return NewList(out), nil
}

// listRepeat ports list_repeat: produce a fresh list containing n
// copies of o's items. Negative or zero n produces an empty list,
// matching the CPython behavior.
//
// CPython: Objects/listobject.c:577 list_repeat
func listRepeat(o Object, n int) (Object, error) {
	l := o.(*List)
	if n <= 0 {
		return NewList(nil), nil
	}
	out := make([]Object, 0, len(l.items)*n)
	for i := 0; i < n; i++ {
		out = append(out, l.items...)
	}
	return NewList(out), nil
}

// listInPlaceConcat extends a with b's items. b must be iterable; the
// CPython slot delegates to list_extend, which accepts any iterable.
//
// CPython: Objects/listobject.c:838 list_inplace_concat
func listInPlaceConcat(a, b Object) (Object, error) {
	la := a.(*List)
	tp := b.Type()
	if tp.Iter == nil {
		return nil, fmt.Errorf("TypeError: '%s' object is not iterable", tp.Name)
	}
	it, err := tp.Iter(b)
	if err != nil {
		return nil, err
	}
	itType := it.Type()
	if itType.IterNext == nil {
		return nil, fmt.Errorf("TypeError: iter() returned non-iterator of type '%s'", itType.Name)
	}
	for {
		v, err := itType.IterNext(it)
		if errors.Is(err, ErrStopIteration) {
			break
		}
		if err != nil {
			return nil, err
		}
		la.Append(v)
	}
	return la, nil
}

// listInPlaceRepeat repeats a's contents n times in place. n<=0 wipes
// the list, matching list_inplace_repeat.
//
// CPython: Objects/listobject.c:626 list_inplace_repeat
func listInPlaceRepeat(o Object, n int) (Object, error) {
	l := o.(*List)
	if n <= 0 {
		l.items = l.items[:0]
		l.size = 0
		return l, nil
	}
	if n == 1 {
		return l, nil
	}
	base := append([]Object(nil), l.items...)
	for i := 1; i < n; i++ {
		l.items = append(l.items, base...)
	}
	l.size = int64(len(l.items))
	return l, nil
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
