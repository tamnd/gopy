package sys

import (
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// CurrentInterpreterFrameHook is installed by vm so that sys._getframe
// can walk the call stack without a circular import.
//
// CPython: Python/sysmodule.c:1180 sys__getframe_impl
var CurrentInterpreterFrameHook func() objects.InterpreterFrame

// getFrame ports sys._getframe([depth]).
// Returns the frame object depth levels up the call stack.
// depth=0 (default) returns the caller's frame.
//
// CPython: Python/sysmodule.c:1180 sys__getframe_impl
func getFrame(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	depth := 0
	if len(args) >= 1 {
		iv, ok := args[0].(*objects.Int)
		if !ok {
			return nil, fmt.Errorf("TypeError: an integer is required")
		}
		v, fits := iv.Int64()
		if !fits || v < 0 {
			return nil, fmt.Errorf("ValueError: frame depth must be a non-negative integer")
		}
		depth = int(v)
	}

	if CurrentInterpreterFrameHook == nil {
		return objects.None(), nil
	}
	f := CurrentInterpreterFrameHook()
	for i := 0; i < depth; i++ {
		if f == nil {
			return nil, fmt.Errorf("ValueError: call stack is not deep enough")
		}
		f = f.FrameBack()
	}
	if f == nil {
		return nil, fmt.Errorf("ValueError: call stack is not deep enough")
	}
	return objects.NewFrame(f), nil
}

// getFrameModuleName ports sys._getframemodulename([depth]). Returns
// the __module__ attribute of the function whose frame sits `depth`
// levels above the caller (i.e. the C builtin's caller frame is the
// base, depth=0). Falls back to None when no function backs the frame
// or the stack is too shallow, mirroring CPython's behavior.
//
// CPython: Python/sysmodule.c:2595 sys__getframemodulename_impl
func getFrameModuleName(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	depth := 0
	if len(args) >= 1 {
		iv, ok := args[0].(*objects.Int)
		if !ok {
			return nil, fmt.Errorf("TypeError: an integer is required")
		}
		v, fits := iv.Int64()
		if !fits || v < 0 {
			return nil, fmt.Errorf("ValueError: frame depth must be a non-negative integer")
		}
		depth = int(v)
	}

	if CurrentInterpreterFrameHook == nil {
		return objects.None(), nil
	}
	f := CurrentInterpreterFrameHook()
	for i := 0; i < depth; i++ {
		if f == nil {
			return objects.None(), nil
		}
		f = f.FrameBack()
	}
	if f == nil {
		return objects.None(), nil
	}
	fn := f.FrameFunc()
	if fn == nil {
		return objects.None(), nil
	}
	if fobj, ok := fn.(*objects.Function); ok {
		if fobj.Module != nil {
			return fobj.Module, nil
		}
	}
	return objects.None(), nil
}
