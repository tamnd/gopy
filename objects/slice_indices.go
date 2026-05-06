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

// sliceIndex coerces a slice bound to int. CPython routes None and
// integer-like objects through _PyEval_SliceIndex. We mirror that.
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
	return 0, fmt.Errorf("TypeError: slice indices must be integers or None or have an __index__ method")
}
