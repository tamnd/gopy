// Slice index helpers: PySlice_Unpack, PySlice_AdjustIndices,
// PySlice_GetIndicesEx. Subscripting code (list[a:b], str[a:b:c],
// etc.) routes through these to turn the slice's three Object fields
// into concrete int triples and a length.
//
// CPython: Objects/sliceobject.c

package objects

import (
	"fmt"
	"math"
)

const (
	ssizeMax = math.MaxInt
	ssizeMin = math.MinInt
)

// Unpack pulls (start, stop, step) out of the slice as concrete ints.
// Step defaults to 1 and may not be zero. Start/stop default to the
// step-aware extremes (0/MAX for forward, MAX/MIN for backward).
//
// CPython: Objects/sliceobject.c:218 PySlice_Unpack
func (s *Slice) Unpack() (start, stop, step int, err error) {
	if s.Step == None() {
		step = 1
	} else {
		step, err = sliceIndex(s.Step)
		if err != nil {
			return 0, 0, 0, err
		}
		if step == 0 {
			return 0, 0, 0, fmt.Errorf("ValueError: slice step cannot be zero")
		}
		if step < -ssizeMax {
			step = -ssizeMax
		}
	}

	if s.Start == None() {
		if step < 0 {
			start = ssizeMax
		} else {
			start = 0
		}
	} else {
		start, err = sliceIndex(s.Start)
		if err != nil {
			return 0, 0, 0, err
		}
	}

	if s.Stop == None() {
		if step < 0 {
			stop = ssizeMin
		} else {
			stop = ssizeMax
		}
	} else {
		stop, err = sliceIndex(s.Stop)
		if err != nil {
			return 0, 0, 0, err
		}
	}
	return start, stop, step, nil
}

// AdjustIndices clamps start/stop into [-1, length] (or [0, length])
// depending on step direction and returns the resulting slice length.
//
// CPython: Objects/sliceobject.c:264 PySlice_AdjustIndices
func AdjustIndices(length int, start, stop *int, step int) int {
	if *start < 0 {
		*start += length
		if *start < 0 {
			if step < 0 {
				*start = -1
			} else {
				*start = 0
			}
		}
	} else if *start >= length {
		if step < 0 {
			*start = length - 1
		} else {
			*start = length
		}
	}

	if *stop < 0 {
		*stop += length
		if *stop < 0 {
			if step < 0 {
				*stop = -1
			} else {
				*stop = 0
			}
		}
	} else if *stop >= length {
		if step < 0 {
			*stop = length - 1
		} else {
			*stop = length
		}
	}

	if step < 0 {
		if *stop < *start {
			return (*start-*stop-1)/(-step) + 1
		}
		return 0
	}
	if *start < *stop {
		return (*stop-*start-1)/step + 1
	}
	return 0
}

// GetIndices is the (start, stop, step, slicelen) bundle that
// callers want most of the time.
//
// CPython: Objects/sliceobject.c:308 PySlice_GetIndicesEx
func (s *Slice) GetIndices(length int) (start, stop, step, slicelen int, err error) {
	start, stop, step, err = s.Unpack()
	if err != nil {
		return 0, 0, 0, 0, err
	}
	slicelen = AdjustIndices(length, &start, &stop, step)
	return start, stop, step, slicelen, nil
}

// Indices is the slice.indices(length) Python method: returns the
// triple as a Tuple. Length must be non-negative.
//
// CPython: Objects/sliceobject.c slice_indices_impl
func (s *Slice) Indices(length int) (Object, error) {
	if length < 0 {
		return nil, fmt.Errorf("ValueError: length should not be negative")
	}
	start, stop, step, _, err := s.GetIndices(length)
	if err != nil {
		return nil, err
	}
	return NewTuple([]Object{
		NewInt(int64(start)),
		NewInt(int64(stop)),
		NewInt(int64(step)),
	}), nil
}

// sliceIndicesMethod implements slice.indices(length). args[0] is the slice
// receiver; args[1] is the sequence length. Uses arbitrary-precision
// arithmetic so that lengths larger than sys.maxsize are handled correctly.
//
// CPython: Objects/sliceobject.c:525 slice_indices
func sliceIndicesMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: indices() takes exactly one argument (%d given)", len(args)-1)
	}
	s, ok := asSlice(args[0])
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'indices' for 'slice' objects doesn't apply to a '%s' object", typeNameOf(args[0]))
	}
	// CPython: Objects/sliceobject.c:530 PyNumber_Index(len)
	lenObj, err := NumberIndex(args[1])
	if err != nil {
		return nil, err
	}
	// CPython: Objects/sliceobject.c:536 _PyLong_IsNegative check
	neg, err := RichCmpBool(lenObj, NewInt(0), CompareLT)
	if err != nil {
		return nil, err
	}
	if neg {
		return nil, fmt.Errorf("ValueError: length should not be negative")
	}
	start, stop, step, err := sliceGetLongIndices(s, lenObj)
	if err != nil {
		return nil, err
	}
	return NewTuple([]Object{start, stop, step}), nil
}

// sliceEvalIndex applies __index__ coercion to a slice bound (start/stop/step).
// Returns the integer value; raises TypeError when o has no __index__.
//
// CPython: Objects/sliceobject.c:157 evaluate_slice_index
func sliceEvalIndex(o Object) (Object, error) {
	if _, ok := o.(*Int); ok {
		return o, nil
	}
	if b, ok := o.(*Bool); ok {
		if b == True() {
			return NewInt(1), nil
		}
		return NewInt(0), nil
	}
	if o == None() {
		return NewInt(0), nil
	}
	res, err := NumberIndex(o)
	if err != nil {
		return nil, fmt.Errorf("TypeError: slice indices must be integers or None or have an __index__ method")
	}
	return res, nil
}

// sliceGetLongIndices ports CPython's _PySlice_GetLongIndices: computes
// start, stop, step as Python integers (arbitrary precision) so that
// slice.indices(len) correctly handles len > sys.maxsize.
//
// CPython: Objects/sliceobject.c:394 _PySlice_GetLongIndices
func sliceGetLongIndices(s *Slice, length Object) (start, stop, step Object, err error) {
	zero := NewInt(0)

	// Step: default 1; reject zero.
	if s.Step == None() {
		step = NewInt(1)
	} else {
		step, err = sliceEvalIndex(s.Step)
		if err != nil {
			return
		}
		eq, e2 := RichCmpBool(step, zero, CompareEQ)
		if e2 != nil {
			err = e2
			return
		}
		if eq {
			err = fmt.Errorf("ValueError: slice step cannot be zero")
			return
		}
	}

	// Determine step sign to pick correct bounds.
	stepNeg, e2 := RichCmpBool(step, zero, CompareLT)
	if e2 != nil {
		err = e2
		return
	}

	// Compute lower and upper bounds.
	var lower, upper Object
	if stepNeg {
		lower = NewInt(-1)
		upper, err = NumberAdd(length, lower) // length - 1
		if err != nil {
			return
		}
	} else {
		lower = zero
		upper = length
	}

	// Compute start.
	if s.Start == None() {
		if stepNeg {
			start = upper
		} else {
			start = lower
		}
	} else {
		start, err = sliceEvalIndex(s.Start)
		if err != nil {
			return
		}
		isNeg, e3 := RichCmpBool(start, zero, CompareLT)
		if e3 != nil {
			err = e3
			return
		}
		if isNeg {
			start, err = NumberAdd(start, length)
			if err != nil {
				return
			}
			lt, e3 := RichCmpBool(start, lower, CompareLT)
			if e3 != nil {
				err = e3
				return
			}
			if lt {
				start = lower
			}
		} else {
			gt, e3 := RichCmpBool(start, upper, CompareGT)
			if e3 != nil {
				err = e3
				return
			}
			if gt {
				start = upper
			}
		}
	}

	// Compute stop.
	if s.Stop == None() {
		if stepNeg {
			stop = lower
		} else {
			stop = upper
		}
	} else {
		stop, err = sliceEvalIndex(s.Stop)
		if err != nil {
			return
		}
		isNeg, e3 := RichCmpBool(stop, zero, CompareLT)
		if e3 != nil {
			err = e3
			return
		}
		if isNeg {
			stop, err = NumberAdd(stop, length)
			if err != nil {
				return
			}
			lt, e3 := RichCmpBool(stop, lower, CompareLT)
			if e3 != nil {
				err = e3
				return
			}
			if lt {
				stop = lower
			}
		} else {
			gt, e3 := RichCmpBool(stop, upper, CompareGT)
			if e3 != nil {
				err = e3
				return
			}
			if gt {
				stop = upper
			}
		}
	}

	return start, stop, step, nil
}

// sliceIndex coerces a slice bound to int. CPython routes None and
// integer-like objects through _PyEval_SliceIndex. Objects with __index__
// are coerced; oversized values are clamped to ssizeMin/ssizeMax.
//
// CPython: Python/ceval.c:1786 _PyEval_SliceIndex
func sliceIndex(o Object) (int, error) {
	if o == None() {
		return 0, nil
	}
	if i, ok := o.(*Int); ok {
		v, fits := i.Int64()
		if !fits {
			if i.BigInt().Sign() < 0 {
				return ssizeMin, nil
			}
			return ssizeMax, nil
		}
		return int(v), nil
	}
	if b, ok := o.(*Bool); ok {
		if b == True() {
			return 1, nil
		}
		return 0, nil
	}
	// Try __index__ coercion for user-defined integer types.
	indexed, err := NumberIndex(o)
	if err != nil {
		return 0, fmt.Errorf("TypeError: slice indices must be integers or None or have an __index__ method")
	}
	if i, ok := indexed.(*Int); ok {
		v, fits := i.Int64()
		if !fits {
			if i.BigInt().Sign() < 0 {
				return ssizeMin, nil
			}
			return ssizeMax, nil
		}
		return int(v), nil
	}
	return 0, fmt.Errorf("TypeError: slice indices must be integers or None or have an __index__ method")
}
