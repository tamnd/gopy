// Exception unwind. When a dispatch arm returns an error, the loop
// walks the code object's PEP 657 exception table to find a handler.
// On hit, the loop pushes the exception, repoints InstrPtr at the
// handler, and resumes dispatch. On miss, the error escapes to the
// caller (or to the next frame up the chain).
//
// CPython: Python/ceval.c exception_unwind / get_exception_handler

package vm

import (
	"strings"

	pyerrors "github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/gil"
	"github.com/tamnd/gopy/objects"
)

// errorPrefixToType maps the "TypeError: ..." style messages large
// chunks of the runtime still emit as bare Go errors back to the
// matching PyExc_* class. Until every callsite raises through
// errors.SetString the unwind path has to guess from the message;
// otherwise `try: ... except TypeError: ...` in user code never
// catches anything because the exception is a bare Exception.
//
// CPython: Python/errors.c:218 PyErr_GivenExceptionMatches reads
// the typed PyObject; gopy's bridge is this prefix table.
var errorPrefixToType = map[string]*objects.Type{
	"TypeError:":           pyerrors.PyExc_TypeError,
	"ValueError:":          pyerrors.PyExc_ValueError,
	"NameError:":           pyerrors.PyExc_NameError,
	"AttributeError:":      pyerrors.PyExc_AttributeError,
	"KeyError:":            pyerrors.PyExc_KeyError,
	"IndexError:":          pyerrors.PyExc_IndexError,
	"RuntimeError:":        pyerrors.PyExc_RuntimeError,
	"StopIteration:":       pyerrors.PyExc_StopIteration,
	"StopAsyncIteration:":  pyerrors.PyExc_StopAsyncIteration,
	"ArithmeticError:":     pyerrors.PyExc_ArithmeticError,
	"ZeroDivisionError:":   pyerrors.PyExc_ZeroDivisionError,
	"OverflowError:":       pyerrors.PyExc_OverflowError,
	"FloatingPointError:":  pyerrors.PyExc_FloatingPointError,
	"LookupError:":         pyerrors.PyExc_LookupError,
	"AssertionError:":      pyerrors.PyExc_AssertionError,
	"NotImplementedError:": pyerrors.PyExc_NotImplementedError,
	"UnicodeError:":        pyerrors.PyExc_UnicodeError,
	"SystemError:":         pyerrors.PyExc_SystemError,
	"RecursionError:":      pyerrors.PyExc_RecursionError,
	"OSError:":             pyerrors.PyExc_OSError,
	"MemoryError:":         pyerrors.PyExc_MemoryError,
	"ReferenceError:":      pyerrors.PyExc_ReferenceError,
	"BufferError:":         pyerrors.PyExc_BufferError,
	"EOFError:":            pyerrors.PyExc_EOFError,
	"ImportError:":         pyerrors.PyExc_ImportError,
	"ModuleNotFoundError:": pyerrors.PyExc_ModuleNotFoundError,
}

// synthesizeException promotes an unmatched Go error into the closest
// typed Python exception. When the message lacks a recognized prefix
// the result falls back to a plain Exception, matching the previous
// behavior.
func synthesizeException(err error) *pyerrors.Exception {
	msg := err.Error()
	// Drop a leading "vm: " prefix added by some callers.
	if rest, ok := strings.CutPrefix(msg, "vm: "); ok {
		msg = rest
	}
	for prefix, typ := range errorPrefixToType {
		if strings.HasPrefix(msg, prefix) {
			return pyerrors.New(typ, objects.NewTuple([]objects.Object{
				objects.NewStr(strings.TrimSpace(msg[len(prefix):])),
			}))
		}
	}
	return pyerrors.New(pyerrors.PyExc_Exception, objects.NewTuple([]objects.Object{
		objects.NewStr(msg),
	}))
}

// handleException tries to find a handler for err in the current
// frame. Returns (residualValue, true) on hit (caller continues
// dispatch); (nil, false) on miss (caller propagates).
//
// CPython: Python/ceval.c:L1815 get_exception_handler + exception_unwind
func (e *evalState) handleException(err error) bool {
	co := e.f.Code
	if co == nil || len(co.ExceptionTable) == 0 {
		return false
	}
	entry, ok := findExcHandler(co.ExceptionTable, e.f.InstrPtr)
	if !ok {
		return false
	}

	// Pull the live exception off the thread state. raiseValue / the
	// abstract layer installed it there before returning the Go
	// sentinel; the handler block expects it on the value stack.
	exc := pyerrors.Occurred(e.ts)
	if exc == nil {
		exc = synthesizeException(err)
	}
	pyerrors.Clear(e.ts)

	// Unwind the value stack to the saved depth recorded in the
	// exception table entry.
	if entry.depth < e.f.StackTop {
		e.f.StackTop = entry.depth
	}

	// For SETUP_WITH / SETUP_CLEANUP regions, push the bytecode lasti
	// in code-units. The with-statement teardown reads it to resume at
	// the right offset.
	//
	// CPython: Python/ceval.c exception_unwind (`if (lasti)` branch)
	if entry.preserveLasti {
		e.pushObject(objects.NewInt(int64(e.f.InstrPtr / 2)))
	}

	// Push the exception value. The handler's first opcode is
	// PUSH_EXC_INFO, which pops this, pushes the previous exc_info
	// under it, then re-pushes it on top.
	//
	// CPython: Python/ceval.c exception_unwind (`PUSH(exc)`)
	e.pushObject(exc)

	e.f.InstrPtr = entry.target
	e.f.PrevInstr = entry.target
	return true
}

// unwind is invoked when the eval-breaker handler errors. It pops
// the current frame and returns the error so the caller frame can
// see it.
//
// CPython: Python/ceval.c goto exception_unwind
func (e *evalState) unwind(err error) (objects.Object, error) {
	return nil, err
}

// handleEvalBreaker drains pending state visible through the breaker:
// requested GIL drops, queued pending calls, async exceptions, GC
// requests. Returns an error if any handler errored.
//
// CPython: Python/ceval_gil.c handle_signals + _Py_HandlePending
func (e *evalState) handleEvalBreaker() error {
	b := e.breaker
	if b == nil {
		return nil
	}
	if b.IsSet(gil.BreakerCallsPending) {
		b.Clear(gil.BreakerCallsPending)
		if p := PendingFor(e.ts); p != nil {
			if err := p.Drain(); err != nil {
				return err
			}
		}
	}
	return nil
}
