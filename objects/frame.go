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
	// FrameLocalsPlusItem returns LocalsPlus[i] for the absolute
	// localsplus index i. Callers walking LocalsplusKinds need this
	// to fetch the post-fix_cell_offsets value at a kind-tagged slot
	// regardless of whether the slot is fast/cell/free.
	//
	// CPython: Objects/frameobject.c:2199 frame_get_var (the
	// frame->localsplus[i] read).
	FrameLocalsPlusItem(i int) Object
	// FrameFunc returns the Function that produced the call, or nil
	// when the frame was not created from a function (e.g. module
	// init, exec). The Tier-2 globals folder needs the function for
	// _PyFunction_GetVersionForCurrentState; everything else can use
	// FrameGlobals/FrameBuiltins.
	//
	// CPython: Python/optimizer_analysis.c:156 _PyFrame_GetFunction
	FrameFunc() Object
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
	frameType.TpTraverse = frameTraverse
	frameType.Getattro = frameGetAttr
}

// frameGetAttr exposes the standard frame attributes f_locals, f_globals,
// f_code, f_back, f_lineno, f_lasti, f_trace.
//
// CPython: Objects/frameobject.c:790 frame_getattro / per-attribute getsets
func frameGetAttr(o Object, name Object) (Object, error) {
	f, ok := o.(*Frame)
	if !ok {
		return GenericGetAttr(o, name)
	}
	n, ok2 := name.(*Unicode)
	if !ok2 {
		return nil, fmt.Errorf("TypeError: attribute name must be string, not '%s'", name.Type().Name)
	}
	switch n.v {
	case "f_locals":
		d, err := FrameFastToLocals(f)
		if err != nil {
			return nil, err
		}
		return d, nil
	case "f_globals":
		if g := f.Globals(); g != nil {
			return g, nil
		}
		return NewDict(), nil
	case "f_code":
		if f.interp != nil {
			if c := f.interp.FrameCode(); c != nil {
				return c, nil
			}
		}
		return None(), nil
	case "f_back":
		b := f.Back()
		if b == nil {
			return None(), nil
		}
		return b, nil
	case "f_lineno":
		return NewInt(int64(f.Lineno())), nil
	case "f_lasti":
		return NewInt(int64(f.Lasti())), nil
	case "f_trace":
		if f.trace != nil {
			return f.trace, nil
		}
		return None(), nil
	case "f_builtins":
		if b := f.Builtins(); b != nil {
			return b, nil
		}
		return NewDict(), nil
	}
	return GenericGetAttr(o, name)
}

// frameTraverse walks every Object reachable from the frame: f_trace
// plus the activation record's globals, builtins, locals, fast/cell/
// free locals, and the back-frame chain. Mirrors frame_traverse on
// the live PyFrameObject.
//
// CPython: Objects/frameobject.c:1163 frame_traverse
func frameTraverse(o Object, visit Visitor) error {
	f := o.(*Frame)
	if f.trace != nil {
		if err := visit(f.trace); err != nil {
			return err
		}
	}
	if f.interp == nil {
		return nil
	}
	if err := visitInterp(f.interp, visit); err != nil {
		return err
	}
	for back := f.interp.FrameBack(); back != nil; back = back.FrameBack() {
		if err := visitInterp(back, visit); err != nil {
			return err
		}
	}
	return nil
}

// visitInterp walks the references on a single activation record. It
// is split out from frameTraverse so the back-chain loop can reuse it
// without recursing through the wrapper-allocation path.
func visitInterp(ip InterpreterFrame, visit Visitor) error {
	if g := ip.FrameGlobals(); g != nil {
		if err := visit(g); err != nil {
			return err
		}
	}
	if b := ip.FrameBuiltins(); b != nil {
		if err := visit(b); err != nil {
			return err
		}
	}
	if l := ip.FrameLocals(); l != nil {
		if err := visit(l); err != nil {
			return err
		}
	}
	for i, n := 0, ip.FrameNumLocals(); i < n; i++ {
		v := ip.FrameFastLocal(i)
		if v == nil {
			continue
		}
		if err := visit(v); err != nil {
			return err
		}
	}
	for i, n := 0, ip.FrameNumCells(); i < n; i++ {
		v := ip.FrameCellLocal(i)
		if v == nil {
			continue
		}
		if err := visit(v); err != nil {
			return err
		}
	}
	for i, n := 0, ip.FrameNumFrees(); i < n; i++ {
		v := ip.FrameFreeLocal(i)
		if v == nil {
			continue
		}
		if err := visit(v); err != nil {
			return err
		}
	}
	return nil
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
