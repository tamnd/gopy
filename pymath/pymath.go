// Package pymath ports cpython/Python/pymath.c plus the math sentinels
// CPython exposes via Include/pymath.h. Most of the original C file is
// x86-87 inline assembly that the Go runtime renders unnecessary; the
// Go target keeps the public sentinels and routes the remaining math
// helpers through the Go standard library.
//
// CPython: Python/pymath.c overview
package pymath

import "math"

// NaN returns a quiet NaN. Mirrors _Py_dg_stdnan / Py_NAN.
//
// CPython: Include/pymath.h:L31 Py_NAN
func NaN() float64 { return math.NaN() }

// Inf returns positive infinity. Mirrors _Py_dg_infinity / Py_HUGE_VAL.
//
// CPython: Include/pymath.h:L25 Py_HUGE_VAL
func Inf() float64 { return math.Inf(1) }

// NegInf returns negative infinity. Mirrors -Py_HUGE_VAL.
//
// CPython: Include/pymath.h:L25 Py_HUGE_VAL
func NegInf() float64 { return math.Inf(-1) }

// CopySign mirrors Py_COPYSIGN. Returns x with the sign bit of y.
//
// CPython: Include/pymath.h:L65 Py_COPYSIGN
func CopySign(x, y float64) float64 { return math.Copysign(x, y) }

// IsNaN mirrors Py_IS_NAN.
//
// CPython: Include/pymath.h:L43 Py_IS_NAN
func IsNaN(x float64) bool { return math.IsNaN(x) }

// IsInf mirrors Py_IS_INFINITY.
//
// CPython: Include/pymath.h:L48 Py_IS_INFINITY
func IsInf(x float64) bool { return math.IsInf(x, 0) }

// IsFinite mirrors Py_IS_FINITE.
//
// CPython: Include/pymath.h:L57 Py_IS_FINITE
func IsFinite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }

// Log1p mirrors Py_LOG1P.
//
// CPython: Include/pymath.h:L80 Py_LOG1P
func Log1p(x float64) float64 { return math.Log1p(x) }

// Hypot mirrors Py_HYPOT.
//
// CPython: Include/pymath.h:L88 Py_HYPOT
func Hypot(x, y float64) float64 { return math.Hypot(x, y) }
