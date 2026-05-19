// Float singleton cache. CPython manages allocation pressure on hot
// float code through a per-thread free list (Objects/floatobject.c:126
// `_Py_FREELIST_POP(PyFloatObject, floats)`). gopy can't replicate the
// "consume inputs" idiom directly because we can't push *Float headers
// back to a pool without explicit lifetime hooks the VM doesn't
// surface yet, but we can match the most impactful slice of the win:
// pre-allocate the values that show up over and over in numeric loops
// (0.0, -0.0, +/- 1.0, NaN, +/- Inf) so the common cases on the float
// fast path are allocation-free.
//
// CPython: Objects/floatobject.c:126 PyFloat_FromDouble freelist branch

package objects

import "math"

// singleton bit patterns. Using math.Float64bits keeps the comparison
// to a single uint64 equality that the compiler can fold to a couple
// of compares.
var (
	bitsZero    = math.Float64bits(0)
	bitsNegZero = math.Float64bits(math.Copysign(0, -1))
	bitsOne     = math.Float64bits(1)
	bitsNegOne  = math.Float64bits(-1)
	bitsPosInf  = math.Float64bits(math.Inf(1))
	bitsNegInf  = math.Float64bits(math.Inf(-1))
	// bitsNaN uses the canonical NaN payload Go's math.NaN produces so
	// every NewFloat(math.NaN()) collapses to the same singleton; user
	// code that constructs a different NaN payload (e.g. from struct
	// decoding) falls through to the alloc path because comparing those
	// to the canonical NaN bit-pattern fails.
	bitsNaN = math.Float64bits(math.NaN())
)

// singleton *Float pointers. Initialized lazily in init() once
// FloatType has its slots wired so floatHash / floatRepr don't trip
// over the unfilled type singleton during package load.
var (
	floatZero    *Float
	floatNegZero *Float
	floatOne     *Float
	floatNegOne  *Float
	floatPosInf  *Float
	floatNegInf  *Float
	floatNaN     *Float
)

func init() {
	floatZero = newFloatRaw(0)
	floatNegZero = newFloatRaw(math.Copysign(0, -1))
	floatOne = newFloatRaw(1)
	floatNegOne = newFloatRaw(-1)
	floatPosInf = newFloatRaw(math.Inf(1))
	floatNegInf = newFloatRaw(math.Inf(-1))
	floatNaN = newFloatRaw(math.NaN())
}

// newFloatRaw constructs a *Float without consulting the singleton
// cache. Used by init() and by the fast path's fall-through.
func newFloatRaw(x float64) *Float {
	o := &Float{v: x}
	o.init(FloatType)
	return o
}

// cachedFloat returns the singleton *Float for the well-known
// constants, or nil if x is not one of them. Bit-pattern compare so
// signed zero, NaN, and infinity all match exactly.
//
// CPython: Objects/floatobject.c:126 PyFloat_FromDouble (free-list
// branch returns a recycled PyFloatObject; we instead recycle a
// permanent singleton for the values the freelist hits the most).
func cachedFloat(x float64) *Float {
	b := math.Float64bits(x)
	switch b {
	case bitsZero:
		return floatZero
	case bitsNegZero:
		return floatNegZero
	case bitsOne:
		return floatOne
	case bitsNegOne:
		return floatNegOne
	case bitsPosInf:
		return floatPosInf
	case bitsNegInf:
		return floatNegInf
	case bitsNaN:
		return floatNaN
	}
	return nil
}
