// The Python-level frame object: f_code / f_back / f_globals /
// f_locals / f_lineno / f_lasti / f_trace. CPython splits the
// activation record (_PyInterpreterFrame, owned by the eval loop)
// from the user-visible PyFrameObject (a thin wrapper). gopy keeps
// the same split: frame.Frame holds the live machine state and lives
// in the frame package; this file holds Frame, the Python-level
// wrapper, and exposes its f_* attribute panel.
//
// objects/ cannot import frame/ (frame already imports objects), so
// access to the activation record goes through InterpreterFrame, an
// interface defined here that the frame package's *frame.Frame
// satisfies via methods.
//
// CPython: Objects/frameobject.c PyFrame_Type and friends

package objects

import "fmt"

// InterpreterFrame is the read-only access objects.Frame needs from
// the interpreter-side activation record. Defining the contract here
// keeps the dependency arrow pointing the right way: frame depends on
// objects, and the live frame implements an interface objects owns.
//
// Method names mirror CPython's PyFrame_Get* / PyFrame_FastLocal*
// helpers (with the Frame prefix kept to avoid collisions with the
// activation-record's own field names).
//
// CPython: Include/cpython/frameobject.h PyFrame_GetCode / GetBack /
// GetGlobals / GetBuiltins / GetLasti
type InterpreterFrame interface {
	FrameCode() *Code
	FrameGlobals() Object
	FrameBuiltins() Object
	FrameLocals() Object
	FrameBack() InterpreterFrame
	FrameLasti() int
	FrameNumLocals() int
	FrameFastLocal(i int) Object
	FrameNumCells() int
	FrameCellLocal(i int) Object
	FrameNumFrees() int
	FrameFreeLocal(i int) Object
}

// Frame is the Python-level frame object. It wraps an interpreter
// frame and adds per-frame trace settings (f_trace, f_trace_lines,
// f_trace_opcodes) that don't belong in the activation record.
//
// CPython: Include/cpython/frameobject.h PyFrameObject
type Frame struct {
	Header
	interp       InterpreterFrame
	trace        Object
	traceLines   bool
	traceOpcodes bool
}

// frameType is the type singleton for `frame`.
//
// CPython: Objects/frameobject.c:1238 PyFrame_Type
var frameType = NewType("frame", []*Type{objectType})

func init() {
	frameType.Repr = frameRepr
}

// NewFrame wraps interp in a Python-level frame object. interp is
// the live activation record; the wrapper holds it as an interface so
// objects/ does not import frame/.
//
// CPython: Objects/frameobject.c:1109 _PyFrame_New_NoTrack
func NewFrame(interp InterpreterFrame) *Frame {
	f := &Frame{
		interp:     interp,
		traceLines: true,
	}
	f.init(frameType)
	return f
}

// FrameType returns the type singleton for `frame`.
//
// CPython: Objects/frameobject.c:1238 PyFrame_Type
func FrameType() *Type { return frameType }

// Interp returns the underlying interpreter frame.
func (f *Frame) Interp() InterpreterFrame { return f.interp }

// Code returns f.f_code: the running Code object.
//
// CPython: Objects/frameobject.c:794 frame_getcode / 1336 PyFrame_GetCode
func (f *Frame) Code() *Code { return f.interp.FrameCode() }

// Globals returns f.f_globals.
//
// CPython: Objects/frameobject.c:879 frame_getglobals / 1357 PyFrame_GetGlobals
func (f *Frame) Globals() Object { return f.interp.FrameGlobals() }

// Builtins returns f.f_builtins.
//
// CPython: Objects/frameobject.c:888 frame_getbuiltins / 1346 PyFrame_GetBuiltins
func (f *Frame) Builtins() Object { return f.interp.FrameBuiltins() }

// Lasti returns f.f_lasti, the offset (in code units) of the last
// dispatched instruction.
//
// CPython: Objects/frameobject.c:802 frame_getlasti / 1380 PyFrame_GetLasti
func (f *Frame) Lasti() int { return f.interp.FrameLasti() }

// Back returns the caller's Python-level frame. nil when this frame
// has no caller.
//
// CPython: Objects/frameobject.c:1314 PyFrame_GetBack
func (f *Frame) Back() *Frame {
	parent := f.interp.FrameBack()
	if parent == nil {
		return nil
	}
	return NewFrame(parent)
}

// Lineno returns f.f_lineno: the source line of the running
// instruction. Zero when the line table is empty or f_lasti falls
// outside any covered range.
//
// CPython: Objects/frameobject.c:843 frame_getlineno / 1242 PyFrame_GetLineNumber
func (f *Frame) Lineno() int {
	code := f.interp.FrameCode()
	if code == nil {
		return 0
	}
	pos, ok := CoAddr2Location(code, f.interp.FrameLasti())
	if !ok {
		return 0
	}
	return pos.Line
}

// Trace returns f.f_trace, or None if no trace function is set.
//
// CPython: Objects/frameobject.c:863 frame_gettrace
func (f *Frame) Trace() Object {
	if f.trace == nil {
		return None()
	}
	return f.trace
}

// SetTrace sets f.f_trace. Pass nil or None to clear.
//
// CPython: Objects/frameobject.c:870 frame_settrace
func (f *Frame) SetTrace(fn Object) {
	if fn == nil || fn == None() {
		f.trace = nil
		return
	}
	f.trace = fn
}

// TraceLines reports f.f_trace_lines.
func (f *Frame) TraceLines() bool { return f.traceLines }

// SetTraceLines stores f.f_trace_lines.
func (f *Frame) SetTraceLines(v bool) { f.traceLines = v }

// TraceOpcodes reports f.f_trace_opcodes.
func (f *Frame) TraceOpcodes() bool { return f.traceOpcodes }

// SetTraceOpcodes stores f.f_trace_opcodes.
func (f *Frame) SetTraceOpcodes(v bool) { f.traceOpcodes = v }

// frameRepr renders frame_repr.
//
// CPython: Objects/frameobject.c:809 frame_repr
func frameRepr(o Object) (string, error) {
	f := o.(*Frame)
	name, file := "?", "?"
	if code := f.interp.FrameCode(); code != nil {
		name = code.Name
		file = code.Filename
	}
	return fmt.Sprintf("<frame at %p, file '%s', line %d, code %s>",
		f, file, f.Lineno(), name), nil
}
