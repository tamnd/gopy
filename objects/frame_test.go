package objects

import (
	"strings"
	"testing"
)

// fakeInterpFrame is a minimal InterpreterFrame stand-in. It lets
// the objects-side Frame wrapper be tested without pulling in the
// frame package (which would create an import cycle if it imported
// us back from a _test.go).
type fakeInterpFrame struct {
	code     *Code
	globals  Object
	builtins Object
	locals   Object
	back     InterpreterFrame
	lasti    int
	fast     []Object
	cells    []Object
	frees    []Object
}

func (f *fakeInterpFrame) FrameCode() *Code                { return f.code }
func (f *fakeInterpFrame) FrameGlobals() Object            { return f.globals }
func (f *fakeInterpFrame) FrameBuiltins() Object           { return f.builtins }
func (f *fakeInterpFrame) FrameLocals() Object             { return f.locals }
func (f *fakeInterpFrame) FrameBack() InterpreterFrame     { return f.back }
func (f *fakeInterpFrame) FrameSetBack(b InterpreterFrame) { f.back = b }
func (f *fakeInterpFrame) FrameLasti() int                 { return f.lasti }
func (f *fakeInterpFrame) FrameNumLocals() int             { return len(f.fast) }
func (f *fakeInterpFrame) FrameFastLocal(i int) Object {
	return f.fast[i]
}
func (f *fakeInterpFrame) FrameNumCells() int { return len(f.cells) }
func (f *fakeInterpFrame) FrameCellLocal(i int) Object {
	return f.cells[i]
}
func (f *fakeInterpFrame) FrameNumFrees() int { return len(f.frees) }
func (f *fakeInterpFrame) FrameFreeLocal(i int) Object {
	return f.frees[i]
}

func (f *fakeInterpFrame) FrameLocalsPlusItem(i int) Object {
	n := len(f.fast)
	if i < n {
		return f.fast[i]
	}
	if i < n+len(f.cells) {
		return f.cells[i-n]
	}
	if i < n+len(f.cells)+len(f.frees) {
		return f.frees[i-n-len(f.cells)]
	}
	return nil
}

func (f *fakeInterpFrame) FrameSetLocalsPlusItem(i int, v Object) {
	n := len(f.fast)
	switch {
	case i < n:
		f.fast[i] = v
	case i < n+len(f.cells):
		f.cells[i-n] = v
	case i < n+len(f.cells)+len(f.frees):
		f.frees[i-n-len(f.cells)] = v
	}
}
func (f *fakeInterpFrame) FrameFunc() Object { return nil }
func (f *fakeInterpFrame) FrameClearLocals() {
	f.fast = nil
	f.cells = nil
	f.frees = nil
}
func (f *fakeInterpFrame) FrameTakeOwnership()         {}
func (f *fakeInterpFrame) FrameNumStack() int          { return 0 }
func (f *fakeInterpFrame) FrameStackItem(int) Object   { return nil }
func (f *fakeInterpFrame) FrameGenOwner() Object       { return nil }
func (f *fakeInterpFrame) FrameRegisterWrapper(Object) {}

func TestFrameAccessors(t *testing.T) {
	code := &Code{Name: "f", Filename: "t.py", Firstlineno: 10}
	g, b := NewDict(), NewDict()
	interp := &fakeInterpFrame{code: code, globals: g, builtins: b, lasti: 4}
	f := NewFrame(interp)

	if f.Code() != code {
		t.Error("Code")
	}
	if f.Globals() != g {
		t.Error("Globals")
	}
	if f.Builtins() != b {
		t.Error("Builtins")
	}
	if f.Lasti() != 4 {
		t.Error("Lasti")
	}
	if f.Back() != nil {
		t.Error("Back should be nil with no parent")
	}
	if f.Type() != FrameType() {
		t.Error("type singleton mismatch")
	}
}

func TestFrameBackWrapsParent(t *testing.T) {
	parentInterp := &fakeInterpFrame{code: &Code{}}
	childInterp := &fakeInterpFrame{code: &Code{}, back: parentInterp}
	child := NewFrame(childInterp)

	parent := child.Back()
	if parent == nil {
		t.Fatal("Back should wrap non-nil parent")
	}
	if parent.Interp() != parentInterp {
		t.Error("Back should wrap the same InterpreterFrame")
	}
}

func TestFrameTraceDefaults(t *testing.T) {
	f := NewFrame(&fakeInterpFrame{code: &Code{}})
	if f.Trace() != None() {
		t.Error("default trace should be None")
	}
	if !f.TraceLines() {
		t.Error("trace_lines defaults to true")
	}
	if f.TraceOpcodes() {
		t.Error("trace_opcodes defaults to false")
	}
}

func TestFrameSetTrace(t *testing.T) {
	f := NewFrame(&fakeInterpFrame{code: &Code{}})
	fn := NewDict() // any non-None object will do
	f.SetTrace(fn)
	if f.Trace() != fn {
		t.Error("SetTrace should store the callable")
	}
	f.SetTrace(None())
	if f.Trace() != None() {
		t.Error("SetTrace(None) should clear")
	}
	f.SetTrace(fn)
	f.SetTrace(nil)
	if f.Trace() != None() {
		t.Error("SetTrace(nil) should clear")
	}
}

func TestFrameSetTraceLinesOpcodes(t *testing.T) {
	f := NewFrame(&fakeInterpFrame{code: &Code{}})
	f.SetTraceLines(false)
	if f.TraceLines() {
		t.Error("SetTraceLines(false)")
	}
	f.SetTraceOpcodes(true)
	if !f.TraceOpcodes() {
		t.Error("SetTraceOpcodes(true)")
	}
}

func TestFrameLinenoNoLinetable(t *testing.T) {
	code := &Code{Firstlineno: 17}
	f := NewFrame(&fakeInterpFrame{code: code})
	if got := f.Lineno(); got != 0 {
		t.Errorf("Lineno without linetable = %d, want 0", got)
	}
}

func TestFrameRepr(t *testing.T) {
	code := &Code{Name: "g", Filename: "x.py"}
	f := NewFrame(&fakeInterpFrame{code: code})
	s, err := Repr(f)
	if err != nil {
		t.Fatalf("Repr: %v", err)
	}
	if !strings.Contains(s, "<frame at ") || !strings.Contains(s, "x.py") || !strings.Contains(s, "code g") {
		t.Errorf("repr = %q", s)
	}
}
