package pymath_test

import (
	"math"
	"testing"

	"github.com/tamnd/gopy/pymath"
)

func TestSentinels(t *testing.T) {
	if !math.IsNaN(pymath.NaN()) {
		t.Error("NaN() did not produce NaN")
	}
	if !math.IsInf(pymath.Inf(), 1) {
		t.Error("Inf() did not produce +Inf")
	}
	if !math.IsInf(pymath.NegInf(), -1) {
		t.Error("NegInf() did not produce -Inf")
	}
}

func TestPredicates(t *testing.T) {
	if !pymath.IsNaN(pymath.NaN()) {
		t.Error("IsNaN(NaN) = false")
	}
	if pymath.IsNaN(1.0) {
		t.Error("IsNaN(1) = true")
	}
	if !pymath.IsInf(pymath.Inf()) {
		t.Error("IsInf(+Inf) = false")
	}
	if pymath.IsFinite(pymath.NaN()) || pymath.IsFinite(pymath.Inf()) {
		t.Error("IsFinite returned true for non-finite")
	}
	if !pymath.IsFinite(0.0) || !pymath.IsFinite(-1.5) {
		t.Error("IsFinite returned false for finite")
	}
}

func TestCopySign(t *testing.T) {
	if pymath.CopySign(1, -2) != -1 {
		t.Error("CopySign(1,-2) != -1")
	}
	if pymath.CopySign(-3, 4) != 3 {
		t.Error("CopySign(-3,4) != 3")
	}
}

func TestLog1pHypot(t *testing.T) {
	if got := pymath.Log1p(0); got != 0 {
		t.Errorf("Log1p(0) = %v", got)
	}
	if got := pymath.Hypot(3, 4); got != 5 {
		t.Errorf("Hypot(3,4) = %v", got)
	}
}

func TestFPESentinels(t *testing.T) {
	if pymath.FPECounter != 0 {
		t.Errorf("FPECounter = %d", pymath.FPECounter)
	}
	if pymath.FPEDummy() != 1.0 {
		t.Errorf("FPEDummy() = %v", pymath.FPEDummy())
	}
}
