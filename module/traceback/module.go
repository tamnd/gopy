// traceback module: thin Go-backed surface that satisfies the names
// unittest pulls in at import time. The full Lib/traceback.py is a
// 1700-line beast that pulls collections.abc, linecache, textwrap,
// codeop, tokenize, _colorize and contextlib through the import
// graph; vendoring the chain is many sessions of work and the
// runtime needs every one of those to import cleanly. This module
// covers the three names unittest reaches for (format_exc,
// clear_frames, TracebackException) so the package's `import
// traceback` line resolves and the full traceback panel can land
// later via a real port.
//
// CPython: Lib/traceback.py the public surface; this stub mirrors
// the four-arg TracebackException constructor and the two helpers.

package traceback

import (
	"fmt"
	"strings"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
	gtraceback "github.com/tamnd/gopy/traceback"
)

func init() {
	_ = imp.AppendInittab("traceback", buildModule)
}

// buildModule populates the traceback module dict.
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("traceback")
	d := m.Dict()

	entries := []struct {
		name string
		val  objects.Object
	}{
		{"format_exc", objects.NewBuiltinFunction("format_exc", formatExc)},
		{"clear_frames", objects.NewBuiltinFunction("clear_frames", clearFrames)},
		{"TracebackException", tracebackExceptionType},
		{"format_exception", objects.NewBuiltinFunction("format_exception", formatException)},
		{"format_exception_only", objects.NewBuiltinFunction("format_exception_only", formatExceptionOnly)},
		{"format_tb", objects.NewBuiltinFunction("format_tb", formatTb)},
		{"print_exc", objects.NewBuiltinFunction("print_exc", printExc)},
		{"print_exception", objects.NewBuiltinFunction("print_exception", printExc)},
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// formatExc ports traceback.format_exc(): formats the currently-handled
// exception. gopy's per-thread exception state is plumbed through
// state.Thread, but builtin functions don't receive a thread argument
// today; until the call ABI grows that, return a placeholder that
// keeps callers (unittest.loader.format_exc on a test failure) from
// crashing.
//
// CPython: Lib/traceback.py:1041 format_exc
func formatExc(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewStr("Traceback (most recent call last):\n  (formatting unavailable in this build)\n"), nil
}

// clearFrames ports traceback.clear_frames(tb): walks the traceback
// chain calling frame.clear() on each frame's local namespace. Frame
// locals lifecycle isn't fully fleshed out yet, so the no-op keeps
// unittest.case's tearDown call working.
//
// CPython: Lib/traceback.py:1062 clear_frames
func clearFrames(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.None(), nil
}

// formatException ports traceback.format_exception. unittest doesn't
// call it directly today; the wrapper exists so user code that does
// can keep moving.
//
// CPython: Lib/traceback.py:163 format_exception
func formatException(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return formatFromExc(args)
}

// formatExceptionOnly ports traceback.format_exception_only.
//
// CPython: Lib/traceback.py:189 format_exception_only
func formatExceptionOnly(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return formatFromExc(args)
}

// formatTb ports traceback.format_tb.
//
// CPython: Lib/traceback.py:148 format_tb
func formatTb(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewList([]objects.Object{}), nil
}

// printExc ports traceback.print_exc and print_exception.
//
// CPython: Lib/traceback.py:130 print_exc / Lib/traceback.py:106 print_exception
func printExc(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.None(), nil
}

func formatFromExc(args []objects.Object) (objects.Object, error) {
	msg := "Traceback (most recent call last):\n"
	if len(args) >= 1 {
		typ := args[0]
		var name string
		if t, ok := typ.(*objects.Type); ok {
			name = t.Name
		} else {
			name = typ.Type().Name
		}
		body := name
		if len(args) >= 2 {
			vs, err := objects.Str(args[1])
			if err == nil && vs != "" {
				body = name + ": " + vs
			}
		}
		msg += body + "\n"
	}
	return objects.NewList([]objects.Object{objects.NewStr(msg)}), nil
}

// tracebackExceptionType is the TracebackException class. Its public
// surface is __init__(exc_type, exc_value, exc_tb, **kwargs) and a
// format(colorize=False) method that yields strings.
//
// CPython: Lib/traceback.py:836 TracebackException
var tracebackExceptionType = newTracebackExceptionType()

func newTracebackExceptionType() *objects.Type {
	t := objects.NewType("TracebackException", []*objects.Type{objects.ObjectType()})
	t.HasDict = true
	t.TpNew = tracebackExceptionNew
	objects.SetTypeDescr(t, "format", objects.NewMethodDescr(t, "format", tracebackExceptionFormat))
	objects.SetTypeDescr(t, "format_exception_only", objects.NewMethodDescr(t, "format_exception_only", tracebackExceptionFormatOnly))
	t.Getattro = objects.GenericGetAttr
	return t
}

// tracebackExceptionNew constructs a TracebackException by stashing
// the (type, value, tb) triple on an Instance so format() can
// re-render it. The full CPython class captures source frames,
// suggestion strings, and exception groups; this lean version keeps
// only enough state to answer format() with a non-empty list.
//
// CPython: Lib/traceback.py:836 TracebackException.__init__
func tracebackExceptionNew(cls *objects.Type, args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 3 {
		return nil, fmt.Errorf("TypeError: TracebackException requires (exc_type, exc_value, exc_tb)")
	}
	inst := objects.NewInstance(cls)
	d := inst.Dict()
	if err := d.SetItem(objects.NewStr("exc_type"), args[0]); err != nil {
		return nil, err
	}
	if err := d.SetItem(objects.NewStr("exc_value"), args[1]); err != nil {
		return nil, err
	}
	if err := d.SetItem(objects.NewStr("exc_traceback"), args[2]); err != nil {
		return nil, err
	}
	return inst, nil
}

func tracebackExceptionFormat(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("TypeError: format() missing self")
	}
	inst, ok := args[0].(*objects.Instance)
	if !ok {
		return nil, fmt.Errorf("TypeError: format() called on non-TracebackException")
	}
	d := inst.Dict()
	typ, _ := d.GetItem(objects.NewStr("exc_type"))
	val, _ := d.GetItem(objects.NewStr("exc_value"))
	tb, _ := d.GetItem(objects.NewStr("exc_traceback"))

	var b strings.Builder
	if t, ok := tb.(*gtraceback.Traceback); ok && t != nil {
		b.WriteString(gtraceback.Format(t))
	} else {
		b.WriteString("Traceback (most recent call last):\n")
	}
	name := "<unknown>"
	if t, ok := typ.(*objects.Type); ok {
		name = t.Name
	} else if typ != nil {
		name = typ.Type().Name
	}
	body := name
	if val != nil && !objects.IsNone(val) {
		if vs, err := objects.Str(val); err == nil && vs != "" {
			body = name + ": " + vs
		}
	}
	b.WriteString(body + "\n")
	return objects.NewList([]objects.Object{objects.NewStr(b.String())}), nil
}

func tracebackExceptionFormatOnly(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return tracebackExceptionFormat(args, nil)
}
