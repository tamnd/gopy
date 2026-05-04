package pysync

import (
	"sync/atomic"
	"unsafe"
)

// OnceFlag mirrors _PyOnceFlag from cpython/Python/lock.c. It runs a
// function exactly once, with concurrent callers parking until the
// runner finishes. If the function returns an error, the flag resets
// and a future caller can retry.
//
// State machine on the byte-flag word:
//
//	0                     unlocked (not yet run)
//	flagLocked            running, or finished successfully
//	flagOnceInitialized   finished successfully (alias of flagLocked)
//	flagLocked|flagHasParked  running with parked waiters
//
// CPython: Include/internal/pycore_lock.h:L18 _Py_ONCE_INITIALIZED
const flagOnceInitialized = flagLocked

// OnceFn is the function invoked by OnceFlag.Do. Returning a non-nil
// error resets the flag so a later caller can retry.
//
// CPython: Include/internal/pycore_lock.h:L127 _Py_once_fn_t
type OnceFn func() error

// OnceFlag is a one-shot initialization flag. The zero value is ready
// to use.
//
// CPython: Include/cpython/modsupport.h:L7 _PyOnceFlag
type OnceFlag struct {
	v atomic.Uint32 // only the low byte is used
}

// Done reports whether the flag has reached the initialized state.
//
// CPython: Include/internal/pycore_lock.h:L140 _PyOnceFlag_CallOnce (adapted from)
func (o *OnceFlag) Done() bool {
	return o.v.Load() == uint32(flagOnceInitialized)
}

// Do invokes fn exactly once. Concurrent callers block until the
// runner finishes. If fn returns an error, the flag is reset and a
// later Do call can retry.
//
// CPython: Python/lock.c:L335 _PyOnceFlag_CallOnceSlow
func (o *OnceFlag) Do(fn OnceFn) error {
	if o.v.Load() == uint32(flagOnceInitialized) {
		return nil
	}
	for {
		v := uint8(o.v.Load())
		if v == 0 {
			if !o.v.CompareAndSwap(uint32(v), uint32(flagLocked)) {
				continue
			}
			err := fn()
			return o.unlock(err)
		}
		if v == flagOnceInitialized {
			return nil
		}
		// running; park until done.
		if v&flagHasParked == 0 {
			newv := v | flagHasParked
			if !o.v.CompareAndSwap(uint32(v), uint32(newv)) {
				continue
			}
			v = newv
		}
		expected := uint32(v)
		_ = Park(unsafe.Pointer(&o.v), func() bool {
			return o.v.Load() == expected
		}, -1, nil, true)
	}
}

// unlock publishes the result of the once-runner and wakes any
// parked callers. nil err marks the flag initialized; non-nil err
// resets it so a future Do can retry.
//
// CPython: Python/lock.c:L311 unlock_once
func (o *OnceFlag) unlock(err error) error {
	var newv uint8
	if err == nil {
		newv = flagOnceInitialized
	}
	old := uint8(o.v.Swap(uint32(newv)))
	if old&flagHasParked != 0 {
		UnparkAll(unsafe.Pointer(&o.v))
	}
	return err
}
