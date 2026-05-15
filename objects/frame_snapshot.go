// FrameSnapshot is a read-only InterpreterFrame copy taken when a
// traceback entry needs to outlive the live activation record.
// gopy reuses interpreter-frame slots through a chunk arena (see
// frame.Frame.Clear), so a *frame.Frame captured during the unwind
// is cleared the moment its function returns, which would leave
// tb.tb_frame.f_code reading None afterwards.
//
// CPython does not need this dance: its frame objects are
// reference-counted PyFrameObjects whose lifetime extends past the
// activation record as soon as something (a traceback, sys._getframe,
// inspect) takes a reference. The snapshot reproduces that effect by
// copying the fields a traceback consumer reads.
//
// CPython: Objects/frameobject.c:1109 _PyFrame_New_NoTrack (the
// PyFrameObject the activation record promotes itself into when it
// has external references)

package objects

// FrameSnapshot captures the fields traceback / inspect consumers
// read off a frame at the moment of capture. Fast locals, cells, and
// free variables are not copied; consumers that need them must walk
// the live interpreter frame.
type FrameSnapshot struct {
	Code     *Code
	Globals  Object
	Builtins Object
	Locals   Object
	Func     Object
	Lasti    int
	Back     InterpreterFrame
}

// SnapshotFrame copies the read-only fields of src into a snapshot
// that satisfies InterpreterFrame. Returns nil when src is nil.
func SnapshotFrame(src InterpreterFrame) *FrameSnapshot {
	if src == nil {
		return nil
	}
	return &FrameSnapshot{
		Code:     src.FrameCode(),
		Globals:  src.FrameGlobals(),
		Builtins: src.FrameBuiltins(),
		Locals:   src.FrameLocals(),
		Func:     src.FrameFunc(),
		Lasti:    src.FrameLasti(),
		Back:     src.FrameBack(),
	}
}

func (s *FrameSnapshot) FrameCode() *Code      { return s.Code }
func (s *FrameSnapshot) FrameGlobals() Object  { return s.Globals }
func (s *FrameSnapshot) FrameBuiltins() Object { return s.Builtins }
func (s *FrameSnapshot) FrameLocals() Object   { return s.Locals }
func (s *FrameSnapshot) FrameFunc() Object     { return s.Func }
func (s *FrameSnapshot) FrameLasti() int       { return s.Lasti }
func (s *FrameSnapshot) FrameBack() InterpreterFrame {
	if s.Back == nil {
		return nil
	}
	return s.Back
}
func (s *FrameSnapshot) FrameNumLocals() int       { return 0 }
func (s *FrameSnapshot) FrameFastLocal(int) Object { return nil }
func (s *FrameSnapshot) FrameNumCells() int        { return 0 }
func (s *FrameSnapshot) FrameCellLocal(int) Object { return nil }
func (s *FrameSnapshot) FrameNumFrees() int        { return 0 }
func (s *FrameSnapshot) FrameFreeLocal(int) Object { return nil }
