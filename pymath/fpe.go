package pymath

// pyfpe.c is essentially empty in modern CPython: floating-point
// exception support was dropped in 3.7 but the symbols stayed for
// stable-ABI compatibility. The Go target preserves the public surface
// so source-shape parity with cpython/Python/pyfpe.c stays one-to-one.
//
// CPython: Python/pyfpe.c overview

// FPECounter is the legacy PyFPE_counter integer. Always zero.
//
// CPython: Python/pyfpe.c:L9 PyFPE_counter
var FPECounter int

// FPEDummy mirrors PyFPE_dummy. Always returns 1.0.
//
// CPython: Python/pyfpe.c:L11 PyFPE_dummy
func FPEDummy() float64 { return 1.0 }
