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
	// Mapping protocol entries for list mirror CPython's
	// list_subscript / list_ass_subscript, which is what handles
	// list[a:b] and del list[a:b] alongside the integer cases.
	//
	// CPython: Objects/listobject.c:3216 list_as_mapping
	ListType.Mapping = &MappingMethods{
		Length:  listLen,
		GetItem: listMappingGet,
		SetItem: listMappingSet,
		DelItem: listMappingDel,
	}
	ListType.TpTraverse = listTraverse
	// TpNew allocates a bare *List bound to the requested class so
	// `class S(list): pass; S()` returns an S instance rather than a
	// plain list. Population happens in __init__ (wired in
	// builtins/ctor.go via bindListCtor), matching CPython's split of
	// list_new (allocate) and list___init___impl (populate).
	//
	// CPython: Objects/listobject.c:2855 list_new
	ListType.TpNew = func(cls *Type, args []Object, kwargs map[string]Object) (Object, error) {
		l := &List{}
		l.init(cls)
		return l, nil
	}
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

// SetItem stores v at index i. The caller is responsible for bounds
// checking; out-of-range indices panic.
//
// CPython: Objects/listobject.c:271 PyList_SET_ITEM
func (l *List) SetItem(i int, v Object) { l.items[i] = v }

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

// listMappingGet ports list_subscript: integer keys go through
// listGetItem, slice keys return a fresh list slice.
//
// CPython: Objects/listobject.c:3162 list_subscript
func listMappingGet(o, key Object) (Object, error) {
	if s, ok := key.(*Slice); ok {
		return listGetSlice(o.(*List), s)
	}
	idx, err := indexValueAsInt(key, "list")
	if err != nil {
		return nil, err
	}
	return listGetItem(o, idx)
}

// listMappingSet ports list_ass_subscript: integer assignment goes
// through listSetItem; slice assignment replaces the slice region in
// place. value==nil means delete (the path used by mp_ass_subscript
// for both `del list[i]` and `del list[a:b]`).
//
// CPython: Objects/listobject.c:3198 list_ass_subscript
func listMappingSet(o, key, v Object) error {
	l := o.(*List)
	if s, ok := key.(*Slice); ok {
		if v == nil {
			return listDelSlice(l, s)
		}
		return listSetSlice(l, s, v)
	}
	idx, err := indexValueAsInt(key, "list")
	if err != nil {
		return err
	}
	if v == nil {
		return listDelIndex(l, idx)
	}
	return listSetItem(o, idx, v)
}

// listMappingDel is the standalone delitem entry. CPython routes
// `del obj[key]` through mp_ass_subscript with NULL value; gopy's vm
// calls mp.DelItem instead, so dispatch here delegates to the same
// shared helper.
//
// CPython: Objects/listobject.c:3198 list_ass_subscript (NULL v arm)
func listMappingDel(o, key Object) error {
	return listMappingSet(o, key, nil)
}

// listDelIndex removes l.items[i]. Mirrors list_ass_item with v=NULL.
//
// CPython: Objects/listobject.c:3041 list_ass_item
func listDelIndex(l *List, i int) error {
	if i < 0 {
		i += len(l.items)
	}
	if i < 0 || i >= len(l.items) {
		return errIndexOutOfRange
	}
	l.items = append(l.items[:i], l.items[i+1:]...)
	l.size = int64(len(l.items))
	return nil
}

// listGetSlice returns a fresh list containing the slice region.
//
// CPython: Objects/listobject.c:482 list_slice
func listGetSlice(l *List, s *Slice) (Object, error) {
	start, _, step, slicelen, err := s.GetIndices(len(l.items))
	if err != nil {
		return nil, err
	}
	out := make([]Object, slicelen)
	for i, idx := 0, start; i < slicelen; i, idx = i+1, idx+step {
		out[i] = l.items[idx]
	}
	return NewList(out), nil
}

// listSetSlice replaces l[start:stop:step] with the values produced by
// iterating v. Step==1 supports differing lengths (grow / shrink);
// extended steps require matching lengths.
//
// CPython: Objects/listobject.c:806 list_ass_slice + extended-slice arm
// of list_ass_subscript
func listSetSlice(l *List, s *Slice, v Object) error {
	start, stop, step, slicelen, err := s.GetIndices(len(l.items))
	if err != nil {
		return err
	}
	src, err := drainIterableForSlice(v)
	if err != nil {
		return err
	}
	if step == 1 {
		newLen := start + len(src) + (len(l.items) - stop)
		out := make([]Object, 0, newLen)
		out = append(out, l.items[:start]...)
		out = append(out, src...)
		out = append(out, l.items[stop:]...)
		l.items = out
		l.size = int64(len(l.items))
		return nil
	}
	if len(src) != slicelen {
		return fmt.Errorf("ValueError: attempt to assign sequence of size %d to extended slice of size %d", len(src), slicelen)
	}
	for i, idx := 0, start; i < slicelen; i, idx = i+1, idx+step {
		l.items[idx] = src[i]
	}
	return nil
}

// listDelSlice removes l[start:stop:step] in place.
//
// CPython: Objects/listobject.c:806 list_ass_slice (NULL v) +
// extended-slice arm of list_ass_subscript
func listDelSlice(l *List, s *Slice) error {
	start, stop, step, slicelen, err := s.GetIndices(len(l.items))
	if err != nil {
		return err
	}
	if slicelen == 0 {
		return nil
	}
	if step == 1 {
		l.items = append(l.items[:start], l.items[stop:]...)
		l.size = int64(len(l.items))
		return nil
	}
	drop := make(map[int]bool, slicelen)
	for i, idx := 0, start; i < slicelen; i, idx = i+1, idx+step {
		drop[idx] = true
	}
	out := l.items[:0]
	for i, v := range l.items {
		if drop[i] {
			continue
		}
		out = append(out, v)
	}
	l.items = out
	l.size = int64(len(l.items))
	return nil
}

// indexValueAsInt coerces an integer-shaped object to a Go int for
// container subscript dispatch. Mirrors PyNumber_AsSsize_t through the
// __index__ slot, with the type name used in error messages.
//
// CPython: Objects/abstract.c PyNumber_AsSsize_t
func indexValueAsInt(key Object, typeName string) (int, error) {
	if i, ok := key.(*Int); ok {
		n, _ := i.Int64()
		return int(n), nil
	}
	if b, ok := key.(*Bool); ok {
		if b == True() {
			return 1, nil
		}
		return 0, nil
	}
	return 0, fmt.Errorf("TypeError: %s indices must be integers or slices, not %s", typeName, key.Type().Name)
}

// drainIterableForSlice materializes an iterable into a slice for the
// slice-assignment path. Mirrors PySequence_Fast: route through the
// general Iter() (which falls back to the __getitem__-driven SeqIter
// when an object has no __iter__) so anything CPython treats as an
// iterable here works the same way.
//
// CPython: Objects/abstract.c:1846 PySequence_Fast
func drainIterableForSlice(o Object) ([]Object, error) {
	if l, ok := o.(*List); ok {
		out := make([]Object, len(l.items))
		copy(out, l.items)
		return out, nil
	}
	if t, ok := o.(*Tuple); ok {
		out := make([]Object, len(t.items))
		copy(out, t.items)
		return out, nil
	}
	it, err := Iter(o)
	if err != nil {
		return nil, fmt.Errorf("TypeError: can only assign an iterable")
	}
	itT := it.Type()
	if itT.IterNext == nil {
		return nil, fmt.Errorf("TypeError: iter() returned non-iterator of type '%s'", itT.Name)
	}
	var out []Object
	for {
		v, err := itT.IterNext(it)
		if errors.Is(err, ErrStopIteration) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
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
