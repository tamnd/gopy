// FrameSnapshot is a read-only InterpreterFrame copy taken when a
// traceback entry or generator frame needs to outlive the live
// activation record. gopy reuses interpreter-frame slots through a
// chunk arena (see frame.Frame.Clear), so a *frame.Frame captured
// during the unwind is cleared the moment its function returns,
// which would leave tb.tb_frame.f_code reading None afterwards.
//
// CPython does not need this dance: its frame objects are
// reference-counted PyFrameObjects whose lifetime extends past the
// activation record as soon as something (a traceback, sys._getframe,
// inspect, or a closed generator's gi_frame consumer) takes a
// reference. _PyFrame_MakeAndSetFrameObject + take_ownership on the
// generator path copy the iframe fields into the PyFrameObject so
// reads continue to work. SnapshotFrame and SnapshotFrameWithLocals
// reproduce that effect by copying the fields the relevant consumers
// read.
//
// CPython: Objects/frameobject.c:1109 _PyFrame_New_NoTrack (the
// PyFrameObject the activation record promotes itself into when it
// has external references)
// CPython: Objects/frameobject.c:1138 take_ownership (the iframe copy
// used when a generator is dealloc'd while gi_frame is externally
// referenced)

package objects

// FrameSnapshot captures the fields traceback / inspect consumers
// read off a frame at the moment of capture. When LocalsPlus is
// populated (the take_ownership case) FrameLocalsProxy reads continue
// to work after the live activation record is cleared.
type FrameSnapshot struct {
	Code     *Code
	Globals  Object
	Builtins Object
	Locals   Object
	Func     Object
	Lasti    int
	Back     InterpreterFrame
	// LocalsPlus is a copy of the activation record's LocalsPlus slice
	// at snapshot time. Populated only by SnapshotFrameWithLocals so
	// the cheap traceback-only path stays allocation-light.
	LocalsPlus []Object
	// NumLocals/NumCells/NumFrees mirror the activation record's
	// slot-count layout at snapshot time. Needed by FrameLocalsProxy
	// so cell unwrap and getval still find the right kind bucket.
	NumLocals int
	NumCells  int
	NumFrees  int
	// NumStack is the count of live operand-stack entries copied into
	// LocalsPlus past the locals/cells/frees range. CPython's
	// frame_traverse walks this range, and gc/test_refleak relies on
	// it so the snapshot stays reachable through the operand stack.
	NumStack int
	// GenOwner mirrors the live frame's GenOwner so frameClear can
	// raise "cannot clear a suspended frame" on a tb_frame wrapper
	// minted from a snapshot of a generator activation record.
	//
	// CPython: Objects/frameobject.c:1997 frame_clear_impl
	// (FRAME_OWNED_BY_GENERATOR branch resolves the same gen)
	GenOwner Object
	// SrcFrame is the live activation record this snapshot was minted
	// from. Held as an opaque InterpreterFrame so objects/ stays free of
	// the frame package import. frameClear walks the current thread's
	// FrameStack and treats the snapshot as "still executing" when this
	// pointer matches a live frame. The chunk arena reuses storage on
	// Pop, so callers also compare Code identity to ward off stale
	// matches.
	//
	// CPython: Objects/frameobject.c:2007 frame_clear_impl
	// (FRAME_OWNED_BY_THREAD goto running)
	SrcFrame InterpreterFrame
}

// SnapshotFrame copies the read-only fields of src into a snapshot
// that satisfies InterpreterFrame. Returns nil when src is nil.
// LocalsPlus is left empty.
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
		GenOwner: src.FrameGenOwner(),
		SrcFrame: src,
	}
}

// SnapshotFrameWithLocals is the take_ownership-equivalent: it copies
// the activation record's LocalsPlus (fast locals, cells, frees, and
// live stack) into the snapshot so FrameLocalsProxy reads continue to
// work after the live activation record is cleared.
//
// CPython: Objects/frameobject.c:1138 take_ownership
func SnapshotFrameWithLocals(src InterpreterFrame) *FrameSnapshot {
	if src == nil {
		return nil
	}
	s := SnapshotFrame(src)
	nLocals := src.FrameNumLocals()
	nCells := src.FrameNumCells()
	nFrees := src.FrameNumFrees()
	nStack := src.FrameNumStack()
	s.NumLocals = nLocals
	s.NumCells = nCells
	s.NumFrees = nFrees
	s.NumStack = nStack
	total := nLocals + nCells + nFrees + nStack
	if total == 0 {
		return s
	}
	s.LocalsPlus = make([]Object, total)
	for i := 0; i < nLocals; i++ {
		s.LocalsPlus[i] = src.FrameFastLocal(i)
	}
	for i := 0; i < nCells; i++ {
		s.LocalsPlus[nLocals+i] = src.FrameCellLocal(i)
	}
	for i := 0; i < nFrees; i++ {
		s.LocalsPlus[nLocals+nCells+i] = src.FrameFreeLocal(i)
	}
	for i := 0; i < nStack; i++ {
		s.LocalsPlus[nLocals+nCells+nFrees+i] = src.FrameStackItem(i)
	}
	return s
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
func (s *FrameSnapshot) FrameNumLocals() int { return s.NumLocals }
func (s *FrameSnapshot) FrameFastLocal(i int) Object {
	if i < 0 || i >= s.NumLocals || i >= len(s.LocalsPlus) {
		return nil
	}
	return s.LocalsPlus[i]
}
func (s *FrameSnapshot) FrameNumCells() int { return s.NumCells }
func (s *FrameSnapshot) FrameCellLocal(i int) Object {
	if i < 0 || i >= s.NumCells {
		return nil
	}
	idx := s.NumLocals + i
	if idx >= len(s.LocalsPlus) {
		return nil
	}
	return s.LocalsPlus[idx]
}
func (s *FrameSnapshot) FrameNumFrees() int { return s.NumFrees }
func (s *FrameSnapshot) FrameFreeLocal(i int) Object {
	if i < 0 || i >= s.NumFrees {
		return nil
	}
	idx := s.NumLocals + s.NumCells + i
	if idx >= len(s.LocalsPlus) {
		return nil
	}
	return s.LocalsPlus[idx]
}

func (s *FrameSnapshot) FrameLocalsPlusItem(i int) Object {
	if i < 0 || i >= len(s.LocalsPlus) {
		return nil
	}
	return s.LocalsPlus[i]
}

func (s *FrameSnapshot) FrameSetLocalsPlusItem(i int, v Object) {
	if i < 0 || i >= len(s.LocalsPlus) {
		return
	}
	s.LocalsPlus[i] = v
}
func (s *FrameSnapshot) FrameNumStack() int { return s.NumStack }
func (s *FrameSnapshot) FrameStackItem(i int) Object {
	if i < 0 || i >= s.NumStack {
		return nil
	}
	idx := s.NumLocals + s.NumCells + s.NumFrees + i
	if idx >= len(s.LocalsPlus) {
		return nil
	}
	return s.LocalsPlus[idx]
}

func (s *FrameSnapshot) FrameClearLocals() {
	for i := range s.LocalsPlus {
		s.LocalsPlus[i] = nil
	}
}

// FrameTakeOwnership is a no-op on a snapshot: the snapshot already
// owns its LocalsPlus copy.
func (s *FrameSnapshot) FrameTakeOwnership() {}

// FrameGenOwner returns the generator that owned the original
// activation record at snapshot time, or nil.
//
// CPython: Objects/genobject.c:107 _PyGen_GetGeneratorFromFrame
func (s *FrameSnapshot) FrameGenOwner() Object { return s.GenOwner }
