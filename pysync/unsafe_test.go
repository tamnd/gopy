package pysync

import "unsafe"

// unsafePtr is a tiny helper to keep the *_test files free of unsafe
// import noise.
func unsafePtr[T any](p *T) unsafe.Pointer { return unsafe.Pointer(p) }
