// Sequence and mapping protocol implementations for array.array.
// Each function mirrors the matching slot from CPython arraymodule.c.
//
// CPython: Modules/arraymodule.c array_as_sequence / array_as_mapping

package array

import (
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// arraySqGetItem implements sq_item: integer index access into the
// raw buffer. Negative indices are pre-resolved by the caller.
//
// CPython: Modules/arraymodule.c:752 array_item
func arraySqGetItem(o objects.Object, i int) (objects.Object, error) {
	a := o.(*arrayObject)
	if i < 0 || i >= a.count {
		return nil, fmt.Errorf("IndexError: array index out of range")
	}
	return getarrayitem(a, i)
}

// arraySqSetItem implements sq_ass_item.
//
// CPython: Modules/arraymodule.c:799 array_ass_item
func arraySqSetItem(o objects.Object, i int, v objects.Object) error {
	a := o.(*arrayObject)
	if i < 0 || i >= a.count {
		return fmt.Errorf("IndexError: array assignment index out of range")
	}
	return setarrayitem(a, i, v)
}

// arrayContains implements sq_contains via linear search on rich
// equality.
//
// CPython: Modules/arraymodule.c:1741 array_contains
func arrayContains(o, v objects.Object) (bool, error) {
	a := o.(*arrayObject)
	for i := 0; i < a.count; i++ {
		item, err := getarrayitem(a, i)
		if err != nil {
			return false, err
		}
		eq, err := objects.RichCmpBool(item, v, objects.CompareEQ)
		if err != nil {
			return false, err
		}
		if eq {
			return true, nil
		}
	}
	return false, nil
}

// arrayConcat implements sq_concat: returns a fresh array of the same
// typecode containing self followed by other.
//
// CPython: Modules/arraymodule.c:1162 array_concat
func arrayConcat(a, b objects.Object) (objects.Object, error) {
	aa := a.(*arrayObject)
	bb, ok := b.(*arrayObject)
	if !ok || bb.descr != aa.descr {
		return nil, fmt.Errorf("TypeError: can only append array (not \"%s\") to array", b.Type().Name)
	}
	out, err := newArrayObject(aa.Type(), aa.count+bb.count, aa.descr)
	if err != nil {
		return nil, err
	}
	copy(out.obItem, aa.obItem[:aa.nbytes()])
	copy(out.obItem[aa.nbytes():], bb.obItem[:bb.nbytes()])
	return out, nil
}

// arrayRepeat implements sq_repeat: a*n returns a fresh array of n
// repeated copies of self. Negative n yields an empty result.
//
// CPython: Modules/arraymodule.c:1183 array_repeat
func arrayRepeat(o objects.Object, n int) (objects.Object, error) {
	a := o.(*arrayObject)
	if n < 0 {
		n = 0
	}
	out, err := newArrayObject(a.Type(), a.count*n, a.descr)
	if err != nil {
		return nil, err
	}
	srcBytes := a.nbytes()
	for i := 0; i < n; i++ {
		copy(out.obItem[i*srcBytes:], a.obItem[:srcBytes])
	}
	return out, nil
}

// arrayInPlaceConcat appends other's contents to self.
//
// CPython: Modules/arraymodule.c:1209 array_inplace_concat
func arrayInPlaceConcat(a, b objects.Object) (objects.Object, error) {
	aa := a.(*arrayObject)
	bb, ok := b.(*arrayObject)
	if !ok {
		return nil, fmt.Errorf("TypeError: can only extend array with array (not \"%s\")", b.Type().Name)
	}
	if bb.descr != aa.descr {
		return nil, fmt.Errorf("TypeError: can only extend with array of same kind")
	}
	if err := arrayExtendArray(aa, bb); err != nil {
		return nil, err
	}
	return aa, nil
}

// arrayInPlaceRepeat repeats self in place n times.
//
// CPython: Modules/arraymodule.c:1228 array_inplace_repeat
func arrayInPlaceRepeat(o objects.Object, n int) (objects.Object, error) {
	a := o.(*arrayObject)
	if n <= 0 {
		if err := arrayResize(a, 0); err != nil {
			return nil, err
		}
		return a, nil
	}
	if n == 1 {
		return a, nil
	}
	oldCount := a.count
	if err := arrayResize(a, oldCount*n); err != nil {
		return nil, err
	}
	srcBytes := oldCount * a.descr.itemsize
	for i := 1; i < n; i++ {
		copy(a.obItem[i*srcBytes:(i+1)*srcBytes], a.obItem[:srcBytes])
	}
	return a, nil
}

// arrayExtendArray is the in-place append-array helper shared by
// inplace_concat and the .extend() method.
//
// CPython: Modules/arraymodule.c:1042 array_do_extend
func arrayExtendArray(a, other *arrayObject) error {
	oldCount := a.count
	if err := arrayResize(a, a.count+other.count); err != nil {
		return err
	}
	copy(a.obItem[oldCount*a.descr.itemsize:], other.obItem[:other.nbytes()])
	return nil
}

// arrayMpSubscript dispatches integer and slice subscripts. Mirrors
// array_subscr's branch.
//
// CPython: Modules/arraymodule.c:2545 array_subscr
func arrayMpSubscript(o, key objects.Object) (objects.Object, error) {
	a := o.(*arrayObject)
	if sl, ok := key.(*objects.Slice); ok {
		start, stop, step, sliceLen, err := sl.GetIndices(a.count)
		if err != nil {
			return nil, err
		}
		out, err := newArrayObject(a.Type(), sliceLen, a.descr)
		if err != nil {
			return nil, err
		}
		if step == 1 {
			copy(out.obItem, a.obItem[start*a.descr.itemsize:stop*a.descr.itemsize])
			return out, nil
		}
		for i, j := 0, start; i < sliceLen; i, j = i+1, j+step {
			copy(out.itemSlice(i), a.itemSlice(j))
		}
		return out, nil
	}
	idx, err := indexAsInt(key)
	if err != nil {
		return nil, err
	}
	if idx < 0 {
		idx += a.count
	}
	return arraySqGetItem(o, idx)
}

// arrayMpAssSubscript dispatches integer and slice assignment.
//
// CPython: Modules/arraymodule.c:2592 array_ass_subscr
func arrayMpAssSubscript(o, key, value objects.Object) error {
	a := o.(*arrayObject)
	if sl, ok := key.(*objects.Slice); ok {
		return arraySliceAssign(a, sl, value)
	}
	idx, err := indexAsInt(key)
	if err != nil {
		return err
	}
	if idx < 0 {
		idx += a.count
	}
	return arraySqSetItem(o, idx, value)
}

// arrayMpDelItem handles `del a[i]` and `del a[i:j]`.
//
// CPython: Modules/arraymodule.c:2592 array_ass_subscr (v==NULL branch)
func arrayMpDelItem(o, key objects.Object) error {
	a := o.(*arrayObject)
	if sl, ok := key.(*objects.Slice); ok {
		return arraySliceDelete(a, sl)
	}
	idx, err := indexAsInt(key)
	if err != nil {
		return err
	}
	if idx < 0 {
		idx += a.count
	}
	if idx < 0 || idx >= a.count {
		return fmt.Errorf("IndexError: array assignment index out of range")
	}
	return arrayDeleteAt(a, idx)
}

// arraySliceAssign assigns value (must be an array of matching typecode)
// to the slice. Extended slices require an exact-length source.
//
// CPython: Modules/arraymodule.c:2632 array_ass_subscr slice branch
func arraySliceAssign(a *arrayObject, sl *objects.Slice, value objects.Object) error {
	start, stop, step, sliceLen, err := sl.GetIndices(a.count)
	if err != nil {
		return err
	}
	other, ok := value.(*arrayObject)
	if !ok {
		return fmt.Errorf("TypeError: can only assign array (not \"%s\") to array slice", value.Type().Name)
	}
	if other.descr != a.descr {
		return fmt.Errorf("TypeError: bad argument type for built-in operation")
	}
	if step == 1 {
		// Variable-length replacement: pull the front, swap the middle,
		// push the tail.
		newLen := a.count - sliceLen + other.count
		if newLen > a.count {
			oldCount := a.count
			if err := arrayResize(a, newLen); err != nil {
				return err
			}
			tail := a.obItem[stop*a.descr.itemsize : oldCount*a.descr.itemsize]
			copy(a.obItem[(start+other.count)*a.descr.itemsize:], tail)
		} else if newLen < a.count {
			tail := a.obItem[stop*a.descr.itemsize : a.count*a.descr.itemsize]
			copy(a.obItem[(start+other.count)*a.descr.itemsize:], tail)
			if err := arrayResize(a, newLen); err != nil {
				return err
			}
		}
		copy(a.obItem[start*a.descr.itemsize:], other.obItem[:other.nbytes()])
		return nil
	}
	if other.count != sliceLen {
		return fmt.Errorf("ValueError: attempt to assign array of size %d to extended slice of size %d", other.count, sliceLen)
	}
	for i, j := 0, start; i < sliceLen; i, j = i+1, j+step {
		copy(a.itemSlice(j), other.itemSlice(i))
	}
	return nil
}

// arraySliceDelete removes the slice indices from a.
//
// CPython: Modules/arraymodule.c:2598 array_ass_subscr slice delete branch
func arraySliceDelete(a *arrayObject, sl *objects.Slice) error {
	start, stop, step, sliceLen, err := sl.GetIndices(a.count)
	if err != nil {
		return err
	}
	if sliceLen == 0 {
		return nil
	}
	if step == 1 {
		tail := a.obItem[stop*a.descr.itemsize : a.count*a.descr.itemsize]
		copy(a.obItem[start*a.descr.itemsize:], tail)
		return arrayResize(a, a.count-sliceLen)
	}
	// Walk live indices in order, shifting survivors leftward.
	if step < 0 {
		start = stop + 1
		step = -step
	}
	dropped := make(map[int]bool, sliceLen)
	for i, j := 0, start; i < sliceLen; i, j = i+1, j+step {
		dropped[j] = true
	}
	w := 0
	itemsize := a.descr.itemsize
	for r := 0; r < a.count; r++ {
		if dropped[r] {
			continue
		}
		if r != w {
			copy(a.obItem[w*itemsize:(w+1)*itemsize], a.obItem[r*itemsize:(r+1)*itemsize])
		}
		w++
	}
	return arrayResize(a, w)
}

// arrayDeleteAt removes item at idx (already bounds-checked).
//
// CPython: Modules/arraymodule.c:907 array_del_slice (single-element case)
func arrayDeleteAt(a *arrayObject, idx int) error {
	itemsize := a.descr.itemsize
	copy(a.obItem[idx*itemsize:], a.obItem[(idx+1)*itemsize:a.count*itemsize])
	return arrayResize(a, a.count-1)
}
