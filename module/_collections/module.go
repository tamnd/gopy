// Package _collections is the gopy port of CPython's _collectionsmodule.c.
// It provides the deque, defaultdict, _tuplegetter, _deque_iterator,
// _deque_reverse_iterator types plus the _count_elements helper that back
// Lib/collections/__init__.py.
//
// CPython: Modules/_collectionsmodule.c

package _collections

import (
	"errors"
	"fmt"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("_collections", buildModule)
}

// buildModule materializes the _collections module dict. Mirrors
// collections_exec which registers all types and _count_elements.
//
// CPython: Modules/_collectionsmodule.c:2854 collections_exec
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_collections")
	d := m.Dict()
	entries := []struct {
		name string
		val  objects.Object
	}{
		{"deque", DequeType},
		{"defaultdict", DefaultDictType},
		{"_deque_iterator", DequeIterType},
		{"_deque_reverse_iterator", DequeRevIterType},
		{"_tuplegetter", TupleGetterType},
		{"_count_elements", objects.NewBuiltinFunction("_count_elements", countElements)},
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// deque type.
// ---------------------------------------------------------------------------

// dequeObject is the runtime shape of a collections.deque. The C
// implementation uses a doubly-linked list of fixed-size blocks; here we use
// a slice of items plus head/tail indices giving O(1) amortised append/pop on
// both sides. The state counter is incremented on every structural mutation so
// active iterators can detect concurrent modification.
//
// CPython: Modules/_collectionsmodule.c:135 dequeobject
type dequeObject struct {
	objects.Header
	items  []objects.Object
	head   int
	tail   int // tail is exclusive: items in use are items[head:tail]
	maxlen int64
	state  uint64
}

// DequeType is the type singleton for collections.deque.
//
// CPython: Modules/_collectionsmodule.c:1893 deque_spec
var DequeType = newDequeType()

func newDequeType() *objects.Type {
	t := objects.NewType("deque", []*objects.Type{objects.ObjectType()})
	t.HasDict = false
	t.Repr = dequeRepr
	t.Str = dequeRepr
	t.RichCmp = dequeRichCmp
	t.Iter = dequeIter
	t.Getattro = dequeGetattr
	t.TpNew = dequeNew
	t.Call = nil
	t.Sequence = &objects.SequenceMethods{
		Length:  dequeSeqLen,
		GetItem: dequeSeqGetItem,
		SetItem: dequeSeqSetItem,
		Contains: func(o, v objects.Object) (bool, error) {
			d := o.(*dequeObject)
			return dequeContains(d, v)
		},
		Concat:        dequeSeqConcat,
		Repeat:        dequeSeqRepeat,
		InPlaceConcat: dequeSeqInPlaceConcat,
		InPlaceRepeat: dequeSeqInPlaceRepeat,
	}
	objects.SetTypeDescr(t, "maxlen", &maxlenDescr{})
	objects.SetTypeDescr(t, "append", objects.NewMethodDescr(t, "append", dequeAppendMethod))
	objects.SetTypeDescr(t, "appendleft", objects.NewMethodDescr(t, "appendleft", dequeAppendLeftMethod))
	objects.SetTypeDescr(t, "pop", objects.NewMethodDescr(t, "pop", dequePopMethod))
	objects.SetTypeDescr(t, "popleft", objects.NewMethodDescr(t, "popleft", dequePopLeftMethod))
	objects.SetTypeDescr(t, "extend", objects.NewMethodDescr(t, "extend", dequeExtendMethod))
	objects.SetTypeDescr(t, "extendleft", objects.NewMethodDescr(t, "extendleft", dequeExtendLeftMethod))
	objects.SetTypeDescr(t, "rotate", objects.NewMethodDescr(t, "rotate", dequeRotateMethod))
	objects.SetTypeDescr(t, "reverse", objects.NewMethodDescr(t, "reverse", dequeReverseMethod))
	objects.SetTypeDescr(t, "count", objects.NewMethodDescr(t, "count", dequeCountMethod))
	objects.SetTypeDescr(t, "remove", objects.NewMethodDescr(t, "remove", dequeRemoveMethod))
	objects.SetTypeDescr(t, "index", objects.NewMethodDescrConv(t, "index", objects.MethFastcall, dequeIndexMethod))
	objects.SetTypeDescr(t, "insert", objects.NewMethodDescr(t, "insert", dequeInsertMethod))
	objects.SetTypeDescr(t, "clear", objects.NewMethodDescr(t, "clear", dequeClearMethod))
	objects.SetTypeDescr(t, "copy", objects.NewMethodDescr(t, "copy", dequeCopyMethod))
	objects.SetTypeDescr(t, "__copy__", objects.NewMethodDescrConv(t, "__copy__", objects.MethNoArgs, dequeCopyMethod))
	// __repr__ slot wrapper. Binding distinct descriptors per type
	// keeps pprint's dispatch table from collapsing every C type onto
	// object.__repr__.
	//
	// CPython: Objects/typeobject.c add_operators slot wrapper for tp_repr
	objects.SetTypeDescr(t, "__repr__", objects.NewMethodDescr(t, "__repr__", dequeReprMethod))
	objects.SetTypeDescr(t, "__reversed__", objects.NewMethodDescrConv(t, "__reversed__", objects.MethNoArgs, dequeReversedMethod))
	objects.SetTypeDescr(t, "__init__", objects.NewMethodDescr(t, "__init__", dequeInitMethod))
	objects.SetTypeDescr(t, "__reduce__", objects.NewMethodDescrConv(t, "__reduce__", objects.MethNoArgs, dequeReduceMethod))
	objects.SetTypeDescr(t, "__sizeof__", objects.NewMethodDescrConv(t, "__sizeof__", objects.MethNoArgs, dequeSizeofMethod))
	return t
}

// maxlenDescr is a data descriptor that exposes deque.maxlen as read-only.
// Mirrors deque_get_maxlen.
//
// CPython: Modules/_collectionsmodule.c:1799 deque_get_maxlen
type maxlenDescr struct {
	objects.Header
}

var maxlenDescrType = func() *objects.Type {
	t := objects.NewType("getset_descriptor", []*objects.Type{objects.ObjectType()})
	t.DescrGet = func(descr objects.Object, owner objects.Object, _ *objects.Type) (objects.Object, error) {
		if owner == nil || objects.IsNone(owner) {
			return descr, nil
		}
		d, ok := owner.(*dequeObject)
		if !ok {
			return nil, fmt.Errorf("TypeError: descriptor 'maxlen' for 'deque' objects doesn't apply to '%s' object", owner.Type().Name)
		}
		if d.maxlen < 0 {
			return objects.None(), nil
		}
		return objects.NewInt(d.maxlen), nil
	}
	return t
}()

func init() {
	// Wire maxlenDescr to its type.
	ml := &maxlenDescr{}
	ml.Init(maxlenDescrType)
	// Replace the placeholder in DequeType.
	objects.SetTypeDescr(DequeType, "maxlen", ml)
}

func dequeLen(d *dequeObject) int {
	return d.tail - d.head
}

// dequeNew allocates a fresh empty deque. Mirrors deque_new.
//
// CPython: Modules/_collectionsmodule.c:203 deque_new
func dequeNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	d := &dequeObject{
		items:  make([]objects.Object, 0, 64),
		head:   0,
		tail:   0,
		maxlen: -1,
	}
	d.Init(cls)
	if len(args) > 0 || len(kwargs) > 0 {
		if err := dequeInit(d, args, kwargs); err != nil {
			return nil, err
		}
	}
	return d, nil
}

// dequeInit seeds the deque from an optional iterable and sets maxlen.
// Mirrors deque_init_impl.
//
// CPython: Modules/_collectionsmodule.c:1750 deque_init_impl
func dequeInit(d *dequeObject, args []objects.Object, kwargs map[string]objects.Object) error {
	maxlen := int64(-1)
	if ml, ok := kwargs["maxlen"]; ok {
		if objects.IsNone(ml) {
			maxlen = -1
		} else {
			n, ok2 := ml.(*objects.Int)
			if !ok2 {
				return fmt.Errorf("TypeError: an integer is required")
			}
			v, fits := n.Int64()
			if !fits || v < 0 {
				if fits && v < 0 {
					return fmt.Errorf("ValueError: maxlen must be non-negative")
				}
				return fmt.Errorf("OverflowError: maxlen too large")
			}
			maxlen = v
		}
	} else if len(args) >= 2 {
		ml := args[1]
		if objects.IsNone(ml) {
			maxlen = -1
		} else {
			n, ok2 := ml.(*objects.Int)
			if !ok2 {
				return fmt.Errorf("TypeError: an integer is required")
			}
			v, fits := n.Int64()
			if !fits || v < 0 {
				if fits && v < 0 {
					return fmt.Errorf("ValueError: maxlen must be non-negative")
				}
				return fmt.Errorf("OverflowError: maxlen too large")
			}
			maxlen = v
		}
	}
	d.maxlen = maxlen

	// Clear existing items.
	d.items = d.items[:0]
	d.head = 0
	d.tail = 0
	d.state++

	if len(args) >= 1 {
		it, err := objects.Iter(args[0])
		if err != nil {
			return fmt.Errorf("TypeError: 'deque' object is not iterable")
		}
		for {
			v, ierr := objects.IterNext(it)
			if ierr != nil {
				if errors.Is(ierr, objects.ErrStopIteration) {
					break
				}
				return ierr
			}
			if v == nil {
				break
			}
			dequeAppendRight(d, v)
		}
	}
	return nil
}

func dequeInitMethod(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: __init__ requires at least self")
	}
	d, ok := args[0].(*dequeObject)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__init__' requires a 'deque' object")
	}
	if err := dequeInit(d, args[1:], kwargs); err != nil {
		return nil, err
	}
	return objects.None(), nil
}

// dequeAppendRight appends item to the right, trimming left when maxlen is set.
// Mirrors deque_append_lock_held.
//
// CPython: Modules/_collectionsmodule.c:341 deque_append_lock_held
func dequeAppendRight(d *dequeObject, item objects.Object) {
	if d.head > 0 && d.head > cap(d.items)/2 {
		// Slide items left to reclaim space.
		copy(d.items, d.items[d.head:d.tail])
		n := dequeLen(d)
		d.tail = n
		d.head = 0
	}
	if d.tail >= len(d.items) {
		d.items = append(d.items, item)
	} else {
		d.items[d.tail] = item
	}
	d.tail++
	if d.maxlen >= 0 && int64(dequeLen(d)) > d.maxlen {
		d.head++
	} else {
		d.state++
	}
}

// dequeAppendLeft appends item to the left, trimming right when maxlen is set.
// Mirrors deque_appendleft_lock_held.
//
// CPython: Modules/_collectionsmodule.c:387 deque_appendleft_lock_held
func dequeAppendLeft(d *dequeObject, item objects.Object) {
	if d.head > 0 {
		d.head--
		d.items[d.head] = item
	} else {
		newItems := make([]objects.Object, dequeLen(d)+1+64)
		copy(newItems[1:], d.items[d.head:d.tail])
		n := dequeLen(d)
		d.items = newItems
		d.head = 0
		d.tail = n + 1
		d.items[0] = item
	}
	if d.maxlen >= 0 && int64(dequeLen(d)) > d.maxlen {
		d.tail--
	} else {
		d.state++
	}
}

// dequeAppendMethod implements deque.append.
//
// CPython: Modules/_collectionsmodule.c:377 deque_append_impl
func dequeAppendMethod(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: append() takes exactly one argument")
	}
	d := args[0].(*dequeObject)
	dequeAppendRight(d, args[1])
	return objects.None(), nil
}

// dequeAppendLeftMethod implements deque.appendleft.
//
// CPython: Modules/_collectionsmodule.c:424 deque_appendleft_impl
func dequeAppendLeftMethod(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: appendleft() takes exactly one argument")
	}
	d := args[0].(*dequeObject)
	dequeAppendLeft(d, args[1])
	return objects.None(), nil
}

// dequePopMethod implements deque.pop.
//
// CPython: Modules/_collectionsmodule.c:244 deque_pop_impl
func dequePopMethod(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	d := args[0].(*dequeObject)
	if dequeLen(d) == 0 {
		return nil, fmt.Errorf("IndexError: pop from an empty deque")
	}
	d.tail--
	item := d.items[d.tail]
	d.items[d.tail] = nil
	d.state++
	return item, nil
}

// dequePopLeftMethod implements deque.popleft.
//
// CPython: Modules/_collectionsmodule.c:289 deque_popleft_impl
func dequePopLeftMethod(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	d := args[0].(*dequeObject)
	if dequeLen(d) == 0 {
		return nil, fmt.Errorf("IndexError: pop from an empty deque")
	}
	item := d.items[d.head]
	d.items[d.head] = nil
	d.head++
	d.state++
	return item, nil
}

// dequeExtendMethod implements deque.extend.
//
// CPython: Modules/_collectionsmodule.c:474 deque_extend_impl
func dequeExtendMethod(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: extend() takes exactly one argument")
	}
	d := args[0].(*dequeObject)
	iterable := args[1]
	it, err := objects.Iter(iterable)
	if err != nil {
		return nil, fmt.Errorf("TypeError: '%s' object is not iterable", iterable.Type().Name)
	}
	for {
		v, ierr := objects.IterNext(it)
		if ierr != nil {
			if errors.Is(ierr, objects.ErrStopIteration) {
				break
			}
			return nil, ierr
		}
		if v == nil {
			break
		}
		dequeAppendRight(d, v)
	}
	return objects.None(), nil
}

// dequeExtendLeftMethod implements deque.extendleft.
//
// CPython: Modules/_collectionsmodule.c:530 deque_extendleft_impl
func dequeExtendLeftMethod(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: extendleft() takes exactly one argument")
	}
	d := args[0].(*dequeObject)
	iterable := args[1]
	it, err := objects.Iter(iterable)
	if err != nil {
		return nil, fmt.Errorf("TypeError: '%s' object is not iterable", iterable.Type().Name)
	}
	for {
		v, ierr := objects.IterNext(it)
		if ierr != nil {
			if errors.Is(ierr, objects.ErrStopIteration) {
				break
			}
			return nil, ierr
		}
		if v == nil {
			break
		}
		dequeAppendLeft(d, v)
	}
	return objects.None(), nil
}

// dequeRotate rotates the deque n steps to the right. Negative n rotates
// left. Mirrors _deque_rotate.
//
// CPython: Modules/_collectionsmodule.c:952 _deque_rotate
func dequeRotate(d *dequeObject, n int) {
	length := dequeLen(d)
	if length <= 1 || n == 0 {
		return
	}
	n = n % length
	if n < 0 {
		n += length
	}
	// Rotate right by n: move the last n items to the front.
	items := d.items[d.head:d.tail]
	rotated := make([]objects.Object, length)
	copy(rotated, items[length-n:])
	copy(rotated[n:], items[:length-n])
	copy(d.items[d.head:d.tail], rotated)
	d.state++
}

// dequeRotateMethod implements deque.rotate.
//
// CPython: Modules/_collectionsmodule.c:1087 deque_rotate_impl
func dequeRotateMethod(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	d := args[0].(*dequeObject)
	n := int64(1)
	if len(args) >= 2 {
		v, ok := args[1].(*objects.Int)
		if !ok {
			return nil, fmt.Errorf("TypeError: an integer is required")
		}
		var fits bool
		n, fits = v.Int64()
		if !fits {
			return nil, fmt.Errorf("OverflowError: Python int too large to convert to Go int")
		}
	}
	dequeRotate(d, int(n))
	return objects.None(), nil
}

// dequeReverseMethod implements deque.reverse.
//
// CPython: Modules/_collectionsmodule.c:1105 deque_reverse_impl
func dequeReverseMethod(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	d := args[0].(*dequeObject)
	items := d.items[d.head:d.tail]
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	d.state++
	return objects.None(), nil
}

// dequeCountMethod implements deque.count.
//
// CPython: Modules/_collectionsmodule.c:1155 deque_count_impl
func dequeCountMethod(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: count() takes exactly one argument")
	}
	d := args[0].(*dequeObject)
	v := args[1]
	startState := d.state
	count := int64(0)
	items := d.items[d.head:d.tail]
	for _, item := range items {
		eq, err := objects.RichCmpBool(item, v, objects.CompareEQ)
		if err != nil {
			return nil, err
		}
		if eq {
			count++
		}
		if d.state != startState {
			return nil, fmt.Errorf("RuntimeError: deque mutated during iteration")
		}
	}
	return objects.NewInt(count), nil
}

// dequeContains is the sq_contains slot.
//
// CPython: Modules/_collectionsmodule.c:1192 deque_contains_lock_held
func dequeContains(d *dequeObject, v objects.Object) (bool, error) {
	startState := d.state
	items := d.items[d.head:d.tail]
	for _, item := range items {
		eq, err := objects.RichCmpBool(item, v, objects.CompareEQ)
		if err != nil {
			return false, err
		}
		if eq {
			return true, nil
		}
		if d.state != startState {
			return false, fmt.Errorf("RuntimeError: deque mutated during iteration")
		}
	}
	return false, nil
}

// dequeRemoveMethod implements deque.remove.
//
// CPython: Modules/_collectionsmodule.c:1456 deque_remove_impl
func dequeRemoveMethod(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: remove() takes exactly one argument")
	}
	d := args[0].(*dequeObject)
	value := args[1]
	startState := d.state
	items := d.items[d.head:d.tail]
	for i, item := range items {
		eq, err := objects.RichCmpBool(item, value, objects.CompareEQ)
		if err != nil {
			return nil, err
		}
		if d.state != startState {
			return nil, fmt.Errorf("IndexError: deque mutated during iteration")
		}
		if eq {
			// Remove items[i] by rotating, popleft, rotate back.
			dequeRotate(d, -i)
			d.head++
			d.items[d.head-1] = nil
			d.state++
			dequeRotate(d, i)
			return objects.None(), nil
		}
	}
	return nil, fmt.Errorf("ValueError: deque.remove(x): x not in deque")
}

// dequeIndexMethod implements deque.index.
//
// CPython: Modules/_collectionsmodule.c:1258 deque_index_impl
func dequeIndexMethod(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: index() requires at least 1 argument")
	}
	d := args[0].(*dequeObject)
	v := args[1]
	n := dequeLen(d)
	start := 0
	stop := n
	if len(args) >= 3 {
		sv, ok := args[2].(*objects.Int)
		if !ok {
			return nil, fmt.Errorf("TypeError: an integer is required")
		}
		s64, _ := sv.Int64()
		start = int(s64)
		if start < 0 {
			start += n
			if start < 0 {
				start = 0
			}
		}
	}
	if len(args) >= 4 {
		sv, ok := args[3].(*objects.Int)
		if !ok {
			return nil, fmt.Errorf("TypeError: an integer is required")
		}
		s64, _ := sv.Int64()
		stop = int(s64)
		if stop < 0 {
			stop += n
			if stop < 0 {
				stop = 0
			}
		}
		if stop > n {
			stop = n
		}
	}
	if start > stop {
		start = stop
	}
	startState := d.state
	items := d.items[d.head : d.head+stop]
	for i := start; i < stop; i++ {
		item := items[i]
		eq, err := objects.RichCmpBool(item, v, objects.CompareEQ)
		if err != nil {
			return nil, err
		}
		if d.state != startState {
			return nil, fmt.Errorf("RuntimeError: deque mutated during iteration")
		}
		if eq {
			return objects.NewInt(int64(i)), nil
		}
	}
	return nil, fmt.Errorf("ValueError: deque.index(x): x not in deque")
}

// dequeInsertMethod implements deque.insert.
//
// CPython: Modules/_collectionsmodule.c:1342 deque_insert_impl
func dequeInsertMethod(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 3 {
		return nil, fmt.Errorf("TypeError: insert() requires index and value")
	}
	d := args[0].(*dequeObject)
	idxObj, ok := args[1].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: an integer is required")
	}
	idx64, _ := idxObj.Int64()
	index := int(idx64)
	value := args[2]
	n := dequeLen(d)
	if d.maxlen >= 0 && int64(n) == d.maxlen {
		return nil, fmt.Errorf("IndexError: deque already at its maximum size")
	}
	if index >= n {
		dequeAppendRight(d, value)
		return objects.None(), nil
	}
	if index <= -n || index == 0 {
		dequeAppendLeft(d, value)
		return objects.None(), nil
	}
	dequeRotate(d, -index)
	if index < 0 {
		dequeAppendRight(d, value)
	} else {
		dequeAppendLeft(d, value)
	}
	dequeRotate(d, index)
	return objects.None(), nil
}

// dequeClearMethod implements deque.clear.
//
// CPython: Modules/_collectionsmodule.c:812 deque_clearmethod_impl
func dequeClearMethod(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	d := args[0].(*dequeObject)
	for i := d.head; i < d.tail; i++ {
		d.items[i] = nil
	}
	d.head = 0
	d.tail = 0
	d.state++
	return objects.None(), nil
}

// dequeCopyMethod implements deque.copy and deque.__copy__.
//
// CPython: Modules/_collectionsmodule.c:599 deque_copy_impl
func dequeCopyMethod(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	d := args[0].(*dequeObject)
	src := d.items[d.head:d.tail]
	newItems := make([]objects.Object, len(src))
	copy(newItems, src)
	nd := &dequeObject{
		items:  newItems,
		head:   0,
		tail:   len(newItems),
		maxlen: d.maxlen,
	}
	nd.Init(d.Type())
	return nd, nil
}

// dequeReversedMethod implements deque.__reversed__.
//
// CPython: Modules/_collectionsmodule.c:1817 deque___reversed___impl
func dequeReversedMethod(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	d := args[0].(*dequeObject)
	return newDequeRevIter(d), nil
}

// dequeReduceMethod implements deque.__reduce__.
//
// CPython: Modules/_collectionsmodule.c:1600 deque___reduce___impl
func dequeReduceMethod(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	d := args[0].(*dequeObject)
	src := d.items[d.head:d.tail]
	listItems := make([]objects.Object, len(src))
	copy(listItems, src)
	it, err := objects.Iter(objects.NewList(listItems))
	if err != nil {
		return nil, err
	}
	emptyArgs := objects.NewTuple(nil)
	state := objects.None()
	if d.maxlen < 0 {
		return objects.NewTuple([]objects.Object{
			d.Type(),
			emptyArgs,
			state,
			it,
		}), nil
	}
	argsTup := objects.NewTuple([]objects.Object{
		objects.NewTuple(nil),
		objects.NewInt(d.maxlen),
	})
	return objects.NewTuple([]objects.Object{
		d.Type(),
		argsTup,
		state,
		it,
	}), nil
}

// dequeSizeofMethod implements deque.__sizeof__. The CPython value reflects
// block layout; here we return a plausible total based on capacity, matching
// the behavioral contract (a positive int proportional to length).
//
// CPython: Modules/_collectionsmodule.c:1785 deque___sizeof___impl
func dequeSizeofMethod(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	d := args[0].(*dequeObject)
	const headerSize = 64
	return objects.NewInt(int64(headerSize + cap(d.items)*8)), nil
}

// dequeSeqConcat implements deque + other (sq_concat).
//
// CPython: Modules/_collectionsmodule.c:675 deque_concat_lock_held
func dequeSeqConcat(a, b objects.Object) (objects.Object, error) {
	d := a.(*dequeObject)
	if _, ok := b.(*dequeObject); !ok {
		return nil, fmt.Errorf("TypeError: can only concatenate deque (not \"%s\") to deque", b.Type().Name)
	}
	cp, err := dequeCopyMethod([]objects.Object{d}, nil)
	if err != nil {
		return nil, err
	}
	if _, err := dequeSeqInPlaceConcat(cp, b); err != nil {
		return nil, err
	}
	return cp, nil
}

// dequeSeqRepeat implements deque * n (sq_repeat).
//
// CPython: Modules/_collectionsmodule.c:908 deque_repeat
func dequeSeqRepeat(o objects.Object, n int) (objects.Object, error) {
	d := o.(*dequeObject)
	cp, err := dequeCopyMethod([]objects.Object{d}, nil)
	if err != nil {
		return nil, err
	}
	if _, err := dequeSeqInPlaceRepeat(cp, n); err != nil {
		return nil, err
	}
	return cp, nil
}

// dequeSeqInPlaceRepeat implements deque *= n (sq_inplace_repeat).
//
// CPython: Modules/_collectionsmodule.c:820 deque_inplace_repeat_lock_held
func dequeSeqInPlaceRepeat(o objects.Object, n int) (objects.Object, error) {
	d := o.(*dequeObject)
	size := dequeLen(d)
	if size == 0 || n == 1 {
		return d, nil
	}
	if n <= 0 {
		for i := d.head; i < d.tail; i++ {
			d.items[i] = nil
		}
		d.head = 0
		d.tail = 0
		d.state++
		return d, nil
	}
	src := make([]objects.Object, size)
	copy(src, d.items[d.head:d.tail])
	for i := 0; i < n-1; i++ {
		for _, v := range src {
			dequeAppendRight(d, v)
		}
	}
	return d, nil
}

// dequeSeqLen is the sq_length slot.
//
// CPython: Modules/_collectionsmodule.c:1236 deque_len
func dequeSeqLen(o objects.Object) (int, error) {
	return dequeLen(o.(*dequeObject)), nil
}

// dequeSeqGetItem is the sq_item slot.
//
// CPython: Modules/_collectionsmodule.c:1379 deque_item_lock_held
func dequeSeqGetItem(o objects.Object, i int) (objects.Object, error) {
	d := o.(*dequeObject)
	n := dequeLen(d)
	if i < 0 {
		i += n
	}
	if i < 0 || i >= n {
		return nil, fmt.Errorf("IndexError: deque index out of range")
	}
	return d.items[d.head+i], nil
}

// dequeSeqSetItem is the sq_ass_item slot (v==nil means delete).
//
// CPython: Modules/_collectionsmodule.c:1530 deque_ass_item_lock_held
func dequeSeqSetItem(o objects.Object, i int, v objects.Object) error {
	d := o.(*dequeObject)
	n := dequeLen(d)
	if i < 0 {
		i += n
	}
	if i < 0 || i >= n {
		return fmt.Errorf("IndexError: deque index out of range")
	}
	if v == nil {
		// Delete item at position i.
		dequeRotate(d, -i)
		d.head++
		d.items[d.head-1] = nil
		d.state++
		dequeRotate(d, i)
		return nil
	}
	d.items[d.head+i] = v
	return nil
}

// dequeSeqInPlaceConcat is the sq_inplace_concat slot.
//
// CPython: Modules/_collectionsmodule.c:575 deque_inplace_concat
func dequeSeqInPlaceConcat(a, b objects.Object) (objects.Object, error) {
	d := a.(*dequeObject)
	it, err := objects.Iter(b)
	if err != nil {
		return nil, err
	}
	for {
		v, ierr := objects.IterNext(it)
		if ierr != nil {
			if errors.Is(ierr, objects.ErrStopIteration) {
				break
			}
			return nil, ierr
		}
		if v == nil {
			break
		}
		dequeAppendRight(d, v)
	}
	return a, nil
}

// dequeGetattr exposes deque attributes.
//
// CPython: Modules/_collectionsmodule.c:1866 deque deque_getset/deque_methods
func dequeGetattr(o objects.Object, name objects.Object) (objects.Object, error) {
	return objects.GenericGetAttr(o, name)
}

// dequeReprMethod is the slot wrapper for tp_repr. Binding it as a
// descriptor keeps deque.__repr__ distinct from object.__repr__ so
// pprint's _dispatch table maps deque to _pprint_deque instead of
// collapsing onto the object.__repr__ entry shared with list/dict.
//
// CPython: Objects/typeobject.c add_operators slot wrapper for tp_repr
func dequeReprMethod(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __repr__() takes no arguments (%d given)", len(args)-1)
	}
	d, ok := args[0].(*dequeObject)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__repr__' requires a 'deque' object")
	}
	s, err := dequeRepr(d)
	if err != nil {
		return nil, err
	}
	return objects.NewStr(s), nil
}

// dequeRepr renders deque([...]) or deque([...], maxlen=N).
//
// CPython: Modules/_collectionsmodule.c:1629 deque_repr
func dequeRepr(o objects.Object) (string, error) {
	d := o.(*dequeObject)
	items := d.items[d.head:d.tail]
	parts := make([]string, 0, len(items))
	for _, item := range items {
		r, err := objects.Repr(item)
		if err != nil {
			return "", err
		}
		parts = append(parts, r)
	}
	list := "["
	for i, p := range parts {
		if i > 0 {
			list += ", "
		}
		list += p
	}
	list += "]"
	if d.maxlen >= 0 {
		return fmt.Sprintf("deque(%s, maxlen=%d)", list, d.maxlen), nil
	}
	return fmt.Sprintf("deque(%s)", list), nil
}

// dequeRichCmp implements ==, !=, <, <=, >, >=.
//
// CPython: Modules/_collectionsmodule.c:1659 deque_richcompare
func dequeRichCmp(a, b objects.Object, op objects.CompareOp) (objects.Object, error) {
	da, ok1 := a.(*dequeObject)
	db, ok2 := b.(*dequeObject)
	if !ok1 || !ok2 {
		return objects.NotImplemented(), nil
	}
	la := dequeLen(da)
	lb := dequeLen(db)
	switch op {
	case objects.CompareEQ:
		if a == b {
			return objects.True(), nil
		}
		if la != lb {
			return objects.False(), nil
		}
	case objects.CompareNE:
		if a == b {
			return objects.False(), nil
		}
		if la != lb {
			return objects.True(), nil
		}
	}
	ia := da.items[da.head:da.tail]
	ib := db.items[db.head:db.tail]
	minLen := la
	if lb < minLen {
		minLen = lb
	}
	for i := 0; i < minLen; i++ {
		eq, err := objects.RichCmpBool(ia[i], ib[i], objects.CompareEQ)
		if err != nil {
			return nil, err
		}
		if !eq {
			return objects.RichCmp(ia[i], ib[i], op)
		}
	}
	// All compared equal: compare lengths.
	switch op {
	case objects.CompareLT:
		if la < lb {
			return objects.True(), nil
		}
		return objects.False(), nil
	case objects.CompareLE:
		if la <= lb {
			return objects.True(), nil
		}
		return objects.False(), nil
	case objects.CompareEQ:
		if la == lb {
			return objects.True(), nil
		}
		return objects.False(), nil
	case objects.CompareNE:
		if la != lb {
			return objects.True(), nil
		}
		return objects.False(), nil
	case objects.CompareGT:
		if la > lb {
			return objects.True(), nil
		}
		return objects.False(), nil
	case objects.CompareGE:
		if la >= lb {
			return objects.True(), nil
		}
		return objects.False(), nil
	}
	return objects.NotImplemented(), nil
}

// dequeIter is the tp_iter slot.
//
// CPython: Modules/_collectionsmodule.c:1916 deque_iter
func dequeIter(o objects.Object) (objects.Object, error) {
	d := o.(*dequeObject)
	return newDequeIter(d), nil
}

// ---------------------------------------------------------------------------
// _deque_iterator type.
// ---------------------------------------------------------------------------

// dequeIterObject is the forward iterator over a deque. counter tracks how
// many items remain; state is the deque's state at creation time.
//
// CPython: Modules/_collectionsmodule.c:1904 dequeiterobject
type dequeIterObject struct {
	objects.Header
	deque   *dequeObject
	index   int
	counter int
	state   uint64
}

// DequeIterType is the type singleton for _deque_iterator.
//
// CPython: Modules/_collectionsmodule.c:2085 dequeiter_spec
var DequeIterType = newDequeIterType()

func newDequeIterType() *objects.Type {
	t := objects.NewType("_deque_iterator", []*objects.Type{objects.ObjectType()})
	t.Iter = func(o objects.Object) (objects.Object, error) { return o, nil }
	t.IterNext = dequeIterNext
	objects.AddIterSlotWrappers(t)
	// CPython: Modules/_collectionsmodule.c:2027 dequeiter_len
	objects.SetTypeDescr(t, "__length_hint__", objects.NewMethodDescrConv(t, "__length_hint__", objects.MethNoArgs, dequeIterLenHint))
	return t
}

// dequeIterLenHint reports the remaining item count. Mirrors CPython's
// dequeiter_len, which returns counter (forced to 0 by dequeiter_next
// on a deque mutation, so callers see 0 instead of a stale count).
//
// CPython: Modules/_collectionsmodule.c:2027 dequeiter_len
func dequeIterLenHint(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __length_hint__ takes no arguments")
	}
	it := args[0].(*dequeIterObject)
	return objects.NewInt(int64(it.counter)), nil
}

func newDequeIter(d *dequeObject) *dequeIterObject {
	it := &dequeIterObject{
		deque:   d,
		index:   d.head,
		counter: dequeLen(d),
		state:   d.state,
	}
	it.Init(DequeIterType)
	return it
}

// dequeIterNext is the tp_iternext slot.
//
// CPython: Modules/_collectionsmodule.c:1992 dequeiter_next
func dequeIterNext(o objects.Object) (objects.Object, error) {
	it := o.(*dequeIterObject)
	if it.deque.state != it.state {
		it.counter = 0
		return nil, fmt.Errorf("RuntimeError: deque mutated during iteration")
	}
	if it.counter == 0 {
		return nil, objects.ErrStopIteration
	}
	item := it.deque.items[it.index]
	it.index++
	it.counter--
	return item, nil
}

// ---------------------------------------------------------------------------
// _deque_reverse_iterator type.
// ---------------------------------------------------------------------------

// DequeRevIterType is the type singleton for _deque_reverse_iterator.
//
// CPython: Modules/_collectionsmodule.c:2203 dequereviter_spec
var DequeRevIterType = newDequeRevIterType()

func newDequeRevIterType() *objects.Type {
	t := objects.NewType("_deque_reverse_iterator", []*objects.Type{objects.ObjectType()})
	t.Iter = func(o objects.Object) (objects.Object, error) { return o, nil }
	t.IterNext = dequeRevIterNext
	objects.AddIterSlotWrappers(t)
	// CPython: Modules/_collectionsmodule.c:2027 dequeiter_len (shared with forward)
	objects.SetTypeDescr(t, "__length_hint__", objects.NewMethodDescrConv(t, "__length_hint__", objects.MethNoArgs, dequeIterLenHint))
	return t
}

func newDequeRevIter(d *dequeObject) *dequeIterObject {
	it := &dequeIterObject{
		deque:   d,
		index:   d.tail - 1,
		counter: dequeLen(d),
		state:   d.state,
	}
	it.Init(DequeRevIterType)
	return it
}

// dequeRevIterNext is the tp_iternext slot for the reverse iterator.
//
// CPython: Modules/_collectionsmodule.c:2143 dequereviter_next
func dequeRevIterNext(o objects.Object) (objects.Object, error) {
	it := o.(*dequeIterObject)
	if it.counter == 0 {
		return nil, objects.ErrStopIteration
	}
	if it.deque.state != it.state {
		it.counter = 0
		return nil, fmt.Errorf("RuntimeError: deque mutated during iteration")
	}
	item := it.deque.items[it.index]
	it.index--
	it.counter--
	return item, nil
}

// ---------------------------------------------------------------------------
// defaultdict type.
// ---------------------------------------------------------------------------

// DefaultDictObject is the runtime shape of collections.defaultdict. It
// embeds a Dict and adds default_factory.
//
// CPython: Modules/_collectionsmodule.c:2213 defdictobject
type DefaultDictObject struct {
	objects.Header
	Dict           *objects.Dict
	DefaultFactory objects.Object
}

// DefaultDictType is the type singleton for defaultdict.
//
// CPython: Modules/_collectionsmodule.c:2514 defdict_spec
var DefaultDictType = newDefaultDictType()

// AsDictBacking exposes the inner *Dict storage so dict's tp_richcompare
// (and any other slot that introspects subclass instances) operates on
// the dict_data the subclass inherits from PyDict_Type.
//
// CPython: Modules/_collectionsmodule.c:2213 defdictobject (PyDictObject substructure)
func (dd *DefaultDictObject) AsDictBacking() *objects.Dict { return dd.Dict }

func newDefaultDictType() *objects.Type {
	t := objects.NewType("defaultdict", []*objects.Type{objects.DictType})
	t.HasDict = false
	t.Getattro = defaultDictGetattr
	t.Setattro = defaultDictSetattr
	t.Repr = defaultDictRepr
	t.TpNew = defaultDictNew
	t.Mapping = &objects.MappingMethods{
		Length:  func(o objects.Object) (int, error) { return o.(*DefaultDictObject).Dict.Len(), nil },
		GetItem: defaultDictGetItem,
		SetItem: defaultDictSetItem,
		DelItem: defaultDictDelItem,
	}
	t.Iter = func(o objects.Object) (objects.Object, error) {
		return objects.Iter(o.(*DefaultDictObject).Dict)
	}
	objects.SetTypeDescr(t, "default_factory", &defaultFactoryDescr{})
	// __repr__ slot wrapper. Binding a distinct method descriptor on
	// defaultdict keeps it from inheriting dict.__repr__ — pprint's
	// _dispatch table is keyed on `type.__repr__`, so without a
	// per-type entry `defaultdict.__repr__` collapses onto
	// `dict.__repr__` and the dispatcher routes plain dicts through
	// _pprint_default_dict.
	//
	// CPython: Objects/typeobject.c add_operators slot wrapper for tp_repr
	objects.SetTypeDescr(t, "__repr__", objects.NewMethodDescr(t, "__repr__", defaultDictReprMethod))
	objects.SetTypeDescr(t, "__missing__", objects.NewMethodDescr(t, "__missing__", defaultDictMissing))
	objects.SetTypeDescr(t, "copy", objects.NewMethodDescr(t, "copy", defaultDictCopy))
	objects.SetTypeDescr(t, "__copy__", objects.NewMethodDescrConv(t, "__copy__", objects.MethNoArgs, defaultDictCopy))
	objects.SetTypeDescr(t, "keys", objects.NewMethodDescr(t, "keys", defaultDictKeys))
	objects.SetTypeDescr(t, "values", objects.NewMethodDescr(t, "values", defaultDictValues))
	objects.SetTypeDescr(t, "items", objects.NewMethodDescr(t, "items", defaultDictItems))
	objects.SetTypeDescr(t, "get", objects.NewMethodDescr(t, "get", defaultDictGet))
	objects.SetTypeDescr(t, "__init__", objects.NewMethodDescr(t, "__init__", defaultDictInitMethod))
	objects.SetTypeDescr(t, "__reduce__", objects.NewMethodDescrConv(t, "__reduce__", objects.MethNoArgs, defaultDictReduceMethod))
	objects.SetTypeDescr(t, "__or__", objects.NewMethodDescr(t, "__or__", defaultDictOrMethod))
	objects.SetTypeDescr(t, "__ror__", objects.NewMethodDescr(t, "__ror__", defaultDictRorMethod))
	objects.SetTypeDescr(t, "__ior__", objects.NewMethodDescr(t, "__ior__", defaultDictIorMethod))
	t.Number = &objects.NumberMethods{
		Or:        defaultDictOr,
		InPlaceOr: defaultDictIor,
	}
	return t
}

// defaultFactoryDescr is the data descriptor for defaultdict.default_factory.
//
// CPython: Modules/_collectionsmodule.c:2344 defdict_members
type defaultFactoryDescr struct {
	objects.Header
}

var defaultFactoryDescrType = func() *objects.Type {
	t := objects.NewType("member_descriptor", []*objects.Type{objects.ObjectType()})
	t.DescrGet = func(descr objects.Object, owner objects.Object, _ *objects.Type) (objects.Object, error) {
		if owner == nil || objects.IsNone(owner) {
			return descr, nil
		}
		dd, ok := owner.(*DefaultDictObject)
		if !ok {
			return nil, fmt.Errorf("TypeError: descriptor 'default_factory' for 'defaultdict' objects doesn't apply to '%s' object", owner.Type().Name)
		}
		if dd.DefaultFactory == nil {
			return objects.None(), nil
		}
		return dd.DefaultFactory, nil
	}
	t.DescrSet = func(descr objects.Object, owner objects.Object, value objects.Object) error {
		dd, ok := owner.(*DefaultDictObject)
		if !ok {
			return fmt.Errorf("TypeError: descriptor 'default_factory' for 'defaultdict' objects doesn't apply to '%s' object", owner.Type().Name)
		}
		if objects.IsNone(value) || value == nil {
			dd.DefaultFactory = nil
		} else {
			dd.DefaultFactory = value
		}
		return nil
	}
	return t
}()

func init() {
	df := &defaultFactoryDescr{}
	df.Init(defaultFactoryDescrType)
	objects.SetTypeDescr(DefaultDictType, "default_factory", df)
}

// defaultDictNew is tp_new for defaultdict. Mirrors defdict_init.
//
// CPython: Modules/_collectionsmodule.c:2453 defdict_init
func defaultDictNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	dd := &DefaultDictObject{
		Dict: objects.NewDict(),
	}
	dd.Init(cls)
	return defaultDictSetup(dd, args, kwargs)
}

func defaultDictInitMethod(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: __init__ requires at least self")
	}
	dd, ok := args[0].(*DefaultDictObject)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__init__' requires a 'defaultdict' object")
	}
	if _, err := defaultDictSetup(dd, args[1:], kwargs); err != nil {
		return nil, err
	}
	return objects.None(), nil
}

func defaultDictSetup(dd *DefaultDictObject, args []objects.Object, kwargs map[string]objects.Object) (*DefaultDictObject, error) {
	var factory objects.Object
	if len(args) > 0 {
		factory = args[0]
		args = args[1:]
	}
	if factory != nil && !objects.IsNone(factory) {
		// Validate callable.
		tp := factory.Type()
		if tp.Call == nil && tp.TpNew == nil && tp.Vectorcall == nil {
			if _, isType := factory.(*objects.Type); !isType {
				return nil, fmt.Errorf("TypeError: first argument must be callable or None")
			}
		}
		dd.DefaultFactory = factory
	} else {
		dd.DefaultFactory = nil
	}

	// Populate from remaining positional args (treated as dict init).
	for _, kv := range args {
		if err := mergeIntoDict(dd.Dict, kv); err == nil {
			continue
		}
		it, err := objects.Iter(kv)
		if err != nil {
			return nil, err
		}
		for {
			pair, ierr := objects.IterNext(it)
			if ierr != nil {
				if errors.Is(ierr, objects.ErrStopIteration) {
					break
				}
				return nil, ierr
			}
			if pair == nil {
				break
			}
			tup, ok := pair.(*objects.Tuple)
			if !ok || tup.Len() != 2 {
				return nil, fmt.Errorf("ValueError: dictionary update sequence element is not a length-2 tuple")
			}
			if err := dd.Dict.SetItem(tup.Item(0), tup.Item(1)); err != nil {
				return nil, err
			}
		}
	}
	for k, v := range kwargs {
		if err := dd.Dict.SetItem(objects.NewStr(k), v); err != nil {
			return nil, err
		}
	}
	return dd, nil
}

// defaultDictGetItem dispatches through __missing__ on key absence.
// Mirrors defdict's mp_subscript behavior.
//
// CPython: Modules/_collectionsmodule.c:2229 defdict_missing (called from getitem)
func defaultDictGetItem(o, key objects.Object) (objects.Object, error) {
	dd := o.(*DefaultDictObject)
	v, err := dd.Dict.GetItem(key)
	if err == nil {
		return v, nil
	}
	// Key absent: call __missing__.
	res, merr := defaultDictMissing([]objects.Object{o, key}, nil)
	if merr != nil {
		return nil, merr
	}
	return res, nil
}

// defaultDictMissing implements __missing__: call default_factory() and store
// the result. Mirrors defdict_missing.
//
// CPython: Modules/_collectionsmodule.c:2229 defdict_missing
func defaultDictMissing(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: __missing__ requires self and key")
	}
	dd := args[0].(*DefaultDictObject)
	key := args[1]
	if dd.DefaultFactory == nil || objects.IsNone(dd.DefaultFactory) {
		repr, _ := objects.Repr(key)
		return nil, fmt.Errorf("KeyError: %s", repr)
	}
	value, err := objects.Call(dd.DefaultFactory, objects.NewTuple(nil), nil)
	if err != nil {
		return nil, err
	}
	if err := dd.Dict.SetItem(key, value); err != nil {
		return nil, err
	}
	return value, nil
}

func defaultDictSetItem(o, key, value objects.Object) error {
	return o.(*DefaultDictObject).Dict.SetItem(key, value)
}

func defaultDictDelItem(o, key objects.Object) error {
	return o.(*DefaultDictObject).Dict.DelItem(key)
}

func defaultDictKeys(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	dd := args[0].(*DefaultDictObject)
	return objects.NewList(dd.Dict.Keys()), nil
}

func defaultDictValues(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	dd := args[0].(*DefaultDictObject)
	keys := dd.Dict.Keys()
	vals := make([]objects.Object, 0, len(keys))
	for _, k := range keys {
		v, err := dd.Dict.GetItem(k)
		if err != nil {
			return nil, err
		}
		vals = append(vals, v)
	}
	return objects.NewList(vals), nil
}

func defaultDictItems(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	dd := args[0].(*DefaultDictObject)
	keys := dd.Dict.Keys()
	items := make([]objects.Object, 0, len(keys))
	for _, k := range keys {
		v, err := dd.Dict.GetItem(k)
		if err != nil {
			return nil, err
		}
		items = append(items, objects.NewTuple([]objects.Object{k, v}))
	}
	return objects.NewList(items), nil
}

func defaultDictGet(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: get() requires key argument")
	}
	dd := args[0].(*DefaultDictObject)
	key := args[1]
	var dflt objects.Object = objects.None()
	if len(args) >= 3 {
		dflt = args[2]
	}
	v, err := dd.Dict.GetItem(key)
	if err != nil {
		return dflt, nil
	}
	return v, nil
}

// defaultDictCopy implements defaultdict.copy. Mirrors defdict_copy.
//
// CPython: Modules/_collectionsmodule.c:2264 defdict_copy
func defaultDictCopy(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	dd := args[0].(*DefaultDictObject)
	nd := &DefaultDictObject{
		Dict:           objects.NewDict(),
		DefaultFactory: dd.DefaultFactory,
	}
	nd.Init(dd.Type())
	for _, k := range dd.Dict.Keys() {
		v, err := dd.Dict.GetItem(k)
		if err != nil {
			return nil, err
		}
		if err := nd.Dict.SetItem(k, v); err != nil {
			return nil, err
		}
	}
	return nd, nil
}

// defaultDictReduceMethod implements defaultdict.__reduce__.
//
// CPython: Modules/_collectionsmodule.c:2274 defdict_reduce
func defaultDictReduceMethod(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	dd := args[0].(*DefaultDictObject)
	var factoryArgs *objects.Tuple
	if dd.DefaultFactory == nil || objects.IsNone(dd.DefaultFactory) {
		factoryArgs = objects.NewTuple(nil)
	} else {
		factoryArgs = objects.NewTuple([]objects.Object{dd.DefaultFactory})
	}
	itemsRes, err := defaultDictItems([]objects.Object{dd}, nil)
	if err != nil {
		return nil, err
	}
	it, err := objects.Iter(itemsRes)
	if err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{
		dd.Type(),
		factoryArgs,
		objects.None(),
		objects.None(),
		it,
	}), nil
}

// defaultDictOr implements defaultdict | other via nb_or.
//
// CPython: Modules/_collectionsmodule.c:2402 defdict_or
func defaultDictOr(a, b objects.Object) (objects.Object, error) {
	left, lok := a.(*DefaultDictObject)
	right, rok := b.(*DefaultDictObject)
	var self *DefaultDictObject
	switch {
	case lok:
		self = left
	case rok:
		self = right
	default:
		return objects.NotImplemented(), nil
	}
	if _, isDict := b.(*objects.Dict); !isDict {
		if _, isDD := b.(*DefaultDictObject); !isDD {
			if _, isDict := a.(*objects.Dict); !isDict {
				if _, isDD := a.(*DefaultDictObject); !isDD {
					return objects.NotImplemented(), nil
				}
			}
		}
	}
	// Build a new defaultdict seeded from `a`.
	newDD := &DefaultDictObject{
		Dict:           objects.NewDict(),
		DefaultFactory: self.DefaultFactory,
	}
	newDD.Init(self.Type())
	if err := mergeIntoDict(newDD.Dict, a); err != nil {
		return nil, err
	}
	if err := mergeIntoDict(newDD.Dict, b); err != nil {
		return nil, err
	}
	return newDD, nil
}

// defaultDictIor implements defaultdict |= other via nb_inplace_or.
//
// CPython: Modules/_collectionsmodule.c uses dict's tp_as_number->nb_inplace_or
// by inheritance; defaultdict reuses dict's update semantics in-place.
func defaultDictIor(a, b objects.Object) (objects.Object, error) {
	dd, ok := a.(*DefaultDictObject)
	if !ok {
		return objects.NotImplemented(), nil
	}
	if err := mergeIntoDict(dd.Dict, b); err != nil {
		return nil, err
	}
	return dd, nil
}

func defaultDictOrMethod(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: __or__ requires self and other")
	}
	return defaultDictOr(args[0], args[1])
}

func defaultDictRorMethod(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: __ror__ requires self and other")
	}
	return defaultDictOr(args[1], args[0])
}

func defaultDictIorMethod(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: __ior__ requires self and other")
	}
	return defaultDictIor(args[0], args[1])
}

// mergeIntoDict copies key/value pairs from src into dst. src may be a dict
// or defaultdict.
func mergeIntoDict(dst *objects.Dict, src objects.Object) error {
	switch s := src.(type) {
	case *objects.Dict:
		for _, k := range s.Keys() {
			v, err := s.GetItem(k)
			if err != nil {
				return err
			}
			if err := dst.SetItem(k, v); err != nil {
				return err
			}
		}
	case *DefaultDictObject:
		for _, k := range s.Dict.Keys() {
			v, err := s.Dict.GetItem(k)
			if err != nil {
				return err
			}
			if err := dst.SetItem(k, v); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("TypeError: unsupported operand for | with '%s'", src.Type().Name)
	}
	return nil
}

// defaultDictRepr renders defaultdict(factory, {...}).
//
// CPython: Modules/_collectionsmodule.c:2363 defdict_repr
// defaultDictReprMethod is the method-descriptor wrapper for tp_repr.
// Distinct from defaultDictRepr so pprint's dispatch table keys the
// defaultdict entry separately from dict.__repr__.
//
// CPython: Modules/_collectionsmodule.c:2364 defdict_repr
func defaultDictReprMethod(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __repr__() takes no arguments (%d given)", len(args)-1)
	}
	dd, ok := args[0].(*DefaultDictObject)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__repr__' requires a 'defaultdict' object")
	}
	s, err := defaultDictRepr(dd)
	if err != nil {
		return nil, err
	}
	return objects.NewStr(s), nil
}

func defaultDictRepr(o objects.Object) (string, error) {
	dd := o.(*DefaultDictObject)
	var factoryRepr string
	if dd.DefaultFactory == nil {
		factoryRepr = "None"
	} else {
		r, err := objects.Repr(dd.DefaultFactory)
		if err != nil {
			return "", err
		}
		factoryRepr = r
	}
	dictRepr, err := objects.Repr(dd.Dict)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("defaultdict(%s, %s)", factoryRepr, dictRepr), nil
}

// defaultDictGetattr exposes default_factory and generic attributes.
//
// CPython: Modules/_collectionsmodule.c:2344 defdict_members
func defaultDictGetattr(o objects.Object, name objects.Object) (objects.Object, error) {
	n, err := objects.Str(name)
	if err != nil {
		return nil, err
	}
	if n == "default_factory" {
		dd := o.(*DefaultDictObject)
		if dd.DefaultFactory == nil {
			return objects.None(), nil
		}
		return dd.DefaultFactory, nil
	}
	// Forward dict methods.
	dd := o.(*DefaultDictObject)
	v, err2 := objects.GenericGetAttr(o, name)
	if err2 == nil {
		return v, nil
	}
	// Try dict.
	v, err2 = dd.Dict.Type().Getattro(dd.Dict, name)
	if err2 == nil {
		return objects.NewBoundMethod(v, o), nil
	}
	return nil, fmt.Errorf("AttributeError: 'defaultdict' object has no attribute '%s'", n)
}

// defaultDictSetattr sets default_factory and generic attributes.
//
// CPython: Modules/_collectionsmodule.c:2344 defdict_members
func defaultDictSetattr(o objects.Object, name objects.Object, value objects.Object) error {
	n, err := objects.Str(name)
	if err != nil {
		return err
	}
	if n == "default_factory" {
		dd := o.(*DefaultDictObject)
		if value == nil || objects.IsNone(value) {
			dd.DefaultFactory = nil
		} else {
			dd.DefaultFactory = value
		}
		return nil
	}
	_, err2 := objects.GenericGetAttr(o, name)
	return err2
}

// ---------------------------------------------------------------------------
// _tuplegetter type.
// ---------------------------------------------------------------------------

// TupleGetterObject is the descriptor used by namedtuple for named fields.
// Each instance holds an index into the tuple and an optional doc string.
//
// CPython: Modules/_collectionsmodule.c:2651 _tuplegetterobject
type TupleGetterObject struct {
	objects.Header
	Index int
	Doc   objects.Object
}

// TupleGetterType is the type singleton for _tuplegetter.
//
// CPython: Modules/_collectionsmodule.c:2791 tuplegetter_spec
var TupleGetterType = newTupleGetterType()

func newTupleGetterType() *objects.Type {
	t := objects.NewType("_tuplegetter", []*objects.Type{objects.ObjectType()})
	t.TpNew = tupleGetterNew
	t.Repr = tupleGetterRepr
	t.DescrGet = tupleGetterDescrGet
	t.DescrSet = tupleGetterDescrSet
	t.Getattro = tupleGetterGetattr
	t.Setattro = tupleGetterSetattr
	return t
}

// tupleGetterNew is _tuplegetter.__new__.
//
// CPython: Modules/_collectionsmodule.c:2668 tuplegetter_new_impl
func tupleGetterNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: _tuplegetter() requires index and doc arguments")
	}
	idxObj, ok := args[0].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: an integer is required for index")
	}
	idx64, _ := idxObj.Int64()
	tg := &TupleGetterObject{
		Index: int(idx64),
		Doc:   args[1],
	}
	tg.Init(cls)
	return tg, nil
}

// tupleGetterDescrGet is the tp_descr_get slot. Returns the tuple element
// at Index when called on an instance.
//
// CPython: Modules/_collectionsmodule.c:2682 tuplegetter_descr_get
func tupleGetterDescrGet(descr objects.Object, owner objects.Object, _ *objects.Type) (objects.Object, error) {
	tg := descr.(*TupleGetterObject)
	if owner == nil || objects.IsNone(owner) {
		return descr, nil
	}
	tup, ok := owner.(*objects.Tuple)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor for index %d for tuple subclasses doesn't apply to '%s' object", tg.Index, owner.Type().Name)
	}
	if tg.Index < 0 || tg.Index >= tup.Len() {
		return nil, fmt.Errorf("IndexError: tuple index out of range")
	}
	return tup.Item(tg.Index), nil
}

// tupleGetterDescrSet rejects writes and deletes.
//
// CPython: Modules/_collectionsmodule.c:2713 tuplegetter_descr_set
func tupleGetterDescrSet(descr objects.Object, owner objects.Object, value objects.Object) error {
	if value == nil {
		return fmt.Errorf("AttributeError: can't delete attribute")
	}
	return fmt.Errorf("AttributeError: can't set attribute")
}

// tupleGetterRepr renders _tuplegetter(index, doc).
//
// CPython: Modules/_collectionsmodule.c:2759 tuplegetter_repr
func tupleGetterRepr(o objects.Object) (string, error) {
	tg := o.(*TupleGetterObject)
	docRepr, err := objects.Repr(tg.Doc)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("_tuplegetter(%d, %s)", tg.Index, docRepr), nil
}

// tupleGetterSetattr supports __doc__ assignment. CPython's
// tuplegetter_members marks __doc__ as a writable member, so the
// namedtuple __init__ machinery and downstream code (e.g. dis.py
// rebinding Instruction field docstrings) can replace it post-hoc.
//
// CPython: Modules/_collectionsmodule.c:2768 tuplegetter_members
func tupleGetterSetattr(o objects.Object, name objects.Object, value objects.Object) error {
	tg := o.(*TupleGetterObject)
	n, err := objects.Str(name)
	if err != nil {
		return err
	}
	if n == "__doc__" {
		if value == nil {
			tg.Doc = objects.None()
		} else {
			tg.Doc = value
		}
		return nil
	}
	return objects.GenericSetAttr(o, name, value)
}

// tupleGetterGetattr exposes __doc__ and other attrs.
//
// CPython: Modules/_collectionsmodule.c:2768 tuplegetter_members
func tupleGetterGetattr(o objects.Object, name objects.Object) (objects.Object, error) {
	tg := o.(*TupleGetterObject)
	n, err := objects.Str(name)
	if err != nil {
		return nil, err
	}
	if n == "__doc__" {
		if tg.Doc == nil {
			return objects.None(), nil
		}
		return tg.Doc, nil
	}
	return objects.GenericGetAttr(o, name)
}

// ---------------------------------------------------------------------------
// _count_elements helper.
// ---------------------------------------------------------------------------

// countElements counts elements in the iterable, updating the mapping.
// Mirrors _collections__count_elements_impl.
//
// Fast path: when neither get() nor __setitem__() is overridden on the
// mapping type (i.e. it is a plain dict), go through Go slots directly.
// Slow path: call mapping.get(key, 0) and mapping[key] = val through the
// Python protocol so Counter subclasses that override those methods are
// respected.
//
// CPython: Modules/_collectionsmodule.c:2534 _collections__count_elements_impl
func countElements(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: _count_elements() requires mapping and iterable")
	}
	mapping := args[0]
	iterable := args[1]

	it, err := objects.Iter(iterable)
	if err != nil {
		return nil, fmt.Errorf("TypeError: '%s' object is not iterable", iterable.Type().Name)
	}
	one := objects.NewInt(1)
	zero := objects.NewInt(0)
	mappingTP := mapping.Type()

	// Check if get() or __setitem__() is overridden relative to dict.
	// CPython: Modules/_collectionsmodule.c:2563 override detection
	mappingGet, _ := objects.LookupDescriptor(mappingTP, "get")
	dictGet, _ := objects.LookupDescriptor(objects.DictType, "get")
	mappingSetitem, _ := objects.LookupDescriptor(mappingTP, "__setitem__")
	dictSetitem, _ := objects.LookupDescriptor(objects.DictType, "__setitem__")
	fastPath := mappingGet == dictGet && mappingSetitem == dictSetitem

	if fastPath {
		// Fast path: use Go-level slots directly.
		// CPython: Modules/_collectionsmodule.c:2575 fast path
		for {
			key, ierr := objects.IterNext(it)
			if ierr != nil {
				if errors.Is(ierr, objects.ErrStopIteration) {
					break
				}
				return nil, ierr
			}
			if key == nil {
				break
			}
			var oldval objects.Object
			if mappingTP.Mapping != nil && mappingTP.Mapping.GetItem != nil {
				oldval, _ = mappingTP.Mapping.GetItem(mapping, key)
			}
			var newval objects.Object
			if oldval == nil {
				newval = one
			} else {
				newval, err = objects.NumberAdd(oldval, one)
				if err != nil {
					return nil, err
				}
			}
			if mappingTP.Mapping != nil && mappingTP.Mapping.SetItem != nil {
				if err := mappingTP.Mapping.SetItem(mapping, key, newval); err != nil {
					return nil, err
				}
			}
		}
	} else {
		// Slow path: call mapping.get(key, 0) and mapping[key] = val through
		// Python protocol so subclass overrides are respected.
		// CPython: Modules/_collectionsmodule.c:2612 slow path
		boundGet, err := objects.GetAttr(mapping, objects.NewStr("get"))
		if err != nil {
			return nil, err
		}
		for {
			key, ierr := objects.IterNext(it)
			if ierr != nil {
				if errors.Is(ierr, objects.ErrStopIteration) {
					break
				}
				return nil, ierr
			}
			if key == nil {
				break
			}
			oldval, err := objects.Call(boundGet, objects.NewTuple([]objects.Object{key, zero}), nil)
			if err != nil {
				return nil, err
			}
			var newval objects.Object
			if objects.Is(oldval, zero) {
				newval = one
			} else {
				newval, err = objects.NumberAdd(oldval, one)
				if err != nil {
					return nil, err
				}
			}
			if err := objects.SetItem(mapping, key, newval); err != nil {
				return nil, err
			}
		}
	}
	return objects.None(), nil
}
