package sys

import (
	"github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

// CurrentThreadHook is the vm-installed callback that returns the
// running thread. vm sets it on init so the sys.exc_info builtin can
// read the per-thread handled-exception slot without sys having to
// import vm. Returns nil when no goroutine-bound thread is active.
var CurrentThreadHook func() *state.Thread

// sysException ports sys.exception(): returns the currently handled exception
// or None. This is the Python 3.11+ replacement for sys.exc_info()[1].
//
// CPython: Python/sysmodule.c:573 sys_exception_impl
func sysException(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if CurrentThreadHook == nil {
		return objects.None(), nil
	}
	ts := CurrentThreadHook()
	if ts == nil {
		return objects.None(), nil
	}
	exc := errors.Handled(ts)
	if exc == nil {
		return objects.None(), nil
	}
	return exc, nil
}

// excInfo ports sys.exc_info(): when an exception is being handled,
// returns the (type, value, traceback) tuple; otherwise returns
// (None, None, None). The handled exception comes from the thread
// state slot the vm refreshes in PUSH_EXC_INFO / POP_EXCEPT.
//
// CPython: Python/sysmodule.c:558 sys_exc_info_impl
func excInfo(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	none := objects.None()
	triple := []objects.Object{none, none, none}
	if CurrentThreadHook == nil {
		return objects.NewTuple(triple), nil
	}
	ts := CurrentThreadHook()
	if ts == nil {
		return objects.NewTuple(triple), nil
	}
	exc := errors.Handled(ts)
	if exc == nil {
		return objects.NewTuple(triple), nil
	}
	if exc.ExcType != nil {
		triple[0] = exc.ExcType
	}
	triple[1] = exc
	if exc.TB != nil {
		triple[2] = exc.TB
	}
	return objects.NewTuple(triple), nil
}
