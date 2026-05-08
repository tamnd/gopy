package monitor

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

// makeLineCode builds a minimal Code with a synthetic linetable: each
// codeunit lives on a fresh line firstlineno+i. Width-1 short-form
// records keep CoLines round-tripping cleanly. The linetable byte
// pattern follows objects.CoPositions's expectations: for a short
// record the first byte is 0b1_dddd_kkk where dddd is the location
// code (here 0 for one-codeunit short-form col0..7) and kkk is
// length-1; the second byte packs end columns. The codepath we test
// only needs the line numbers, so we cheat with a "noCols" record
// that pins line = firstlineno + i.
func makeLineCode(opcodes []byte) *objects.Code {
	c := &objects.Code{Code: append([]byte{}, opcodes...), Firstlineno: 1}
	// Build a linetable via the noCols form (code locNoCols=13) so we
	// only emit a signed varint per codeunit. The decoder demands
	// length-1 in the bottom three bits (0 for one codeunit).
	const start = 1 << 7
	const noCols = 13 << 3
	var tab []byte
	for i := 0; i < len(opcodes)/2; i++ {
		// First byte: start | noCols | length=0 (one codeunit).
		// Second byte: signed varint for delta=1: (1<<1)|0 = 2.
		tab = append(tab, byte(start|noCols), 2)
	}
	c.Linetable = tab
	return c
}

func TestComputeLineDeltaAndBack(t *testing.T) {
	c := &objects.Code{Firstlineno: 10}
	if got := computeLineDelta(c, 12); got != 2 {
		t.Fatalf("computeLineDelta(12) = %d, want 2", got)
	}
	if got := computeLineDelta(c, -1); got != NoLine {
		t.Fatalf("computeLineDelta(-1) = %d, want NoLine", got)
	}
	if got := computeLine(c, 2); got != 12 {
		t.Fatalf("computeLine(2) = %d, want 12", got)
	}
	if got := computeLine(c, NoLine); got != -1 {
		t.Fatalf("computeLine(NoLine) = %d, want -1", got)
	}
}

func TestSetGetLineDeltaRoundTrip(t *testing.T) {
	for _, bpe := range []int{2, 3, 4, 5} {
		ld := &LineInstrumentationData{BytesPerEntry: uint8(bpe), Data: make([]byte, 4*bpe)}
		setOriginalOpcode(ld, 1, 42)
		setLineDelta(ld, 1, 17)
		if got := getLineDelta(ld, 1); got != 17 {
			t.Errorf("bpe=%d: getLineDelta = %d, want 17", bpe, got)
		}
		if got := getOriginalOpcode(ld, 1); got != 42 {
			t.Errorf("bpe=%d: getOriginalOpcode = %d, want 42", bpe, got)
		}
		setLineDelta(ld, 2, NoLine)
		if got := getLineDelta(ld, 2); got != NoLine {
			t.Errorf("bpe=%d: NoLine round-trip = %d", bpe, got)
		}
	}
}

func TestInstrumentInstallsLineMarker(t *testing.T) {
	code := makeLineCode([]byte{
		byte(compile.LOAD_CONST), 0,
		byte(compile.RETURN_VALUE), 0,
	})
	s := NewInterpState()
	if err := s.UseToolID(ToolDebugger, objects.NewStr("debug")); err != nil {
		t.Fatalf("UseToolID: %v", err)
	}
	if err := s.SetEvents(ToolDebugger, EventSet(0).With(EventLine)); err != nil {
		t.Fatalf("SetEvents: %v", err)
	}
	if err := Instrument(code, s); err != nil {
		t.Fatalf("Instrument: %v", err)
	}
	// First codeunit (LOAD_CONST) starts the first line, so it should
	// be hidden behind INSTRUMENTED_LINE.
	if got := compile.Opcode(code.Code[0]); got != compile.INSTRUMENTED_LINE {
		t.Fatalf("first codeunit not rewritten, got %v", got)
	}
	data := CoMonitoring(code)
	if data == nil || data.Lines == nil {
		t.Fatalf("Lines table not allocated")
	}
	if got := getOriginalOpcode(data.Lines, 0); compile.Opcode(got) != compile.LOAD_CONST {
		t.Fatalf("original opcode for codeunit 0 = %v, want LOAD_CONST", compile.Opcode(got))
	}
}

func TestInstrumentClearLineRestoresOpcode(t *testing.T) {
	code := makeLineCode([]byte{
		byte(compile.LOAD_CONST), 0,
		byte(compile.RETURN_VALUE), 0,
	})
	s := NewInterpState()
	if err := s.UseToolID(ToolDebugger, objects.NewStr("debug")); err != nil {
		t.Fatalf("UseToolID: %v", err)
	}
	if err := s.SetEvents(ToolDebugger, EventSet(0).With(EventLine)); err != nil {
		t.Fatalf("SetEvents: %v", err)
	}
	if err := Instrument(code, s); err != nil {
		t.Fatalf("Instrument: %v", err)
	}
	if err := s.SetEvents(ToolDebugger, 0); err != nil {
		t.Fatalf("SetEvents (clear): %v", err)
	}
	if err := Instrument(code, s); err != nil {
		t.Fatalf("Instrument (clear): %v", err)
	}
	if got := compile.Opcode(code.Code[0]); got != compile.LOAD_CONST {
		t.Fatalf("LOAD_CONST not restored, got %v", got)
	}
}

func TestCallInstrumentationLineFires(t *testing.T) {
	code := makeLineCode([]byte{
		byte(compile.LOAD_CONST), 0,
		byte(compile.RETURN_VALUE), 0,
	})
	s := NewInterpState()
	if err := s.UseToolID(ToolDebugger, objects.NewStr("debug")); err != nil {
		t.Fatalf("UseToolID: %v", err)
	}
	var seen int
	cb := objects.NewBuiltinFunction("rec", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		seen++
		if len(args) != 2 {
			t.Errorf("LINE callback got %d args, want 2", len(args))
		}
		return objects.None(), nil
	})
	if _, err := s.RegisterCallback(ToolDebugger, EventLine, cb); err != nil {
		t.Fatalf("RegisterCallback: %v", err)
	}
	if err := s.SetEvents(ToolDebugger, EventSet(0).With(EventLine)); err != nil {
		t.Fatalf("SetEvents: %v", err)
	}
	if err := Instrument(code, s); err != nil {
		t.Fatalf("Instrument: %v", err)
	}
	disp, err := CallInstrumentationLine(s, code, 0, -1)
	if err != nil {
		t.Fatalf("CallInstrumentationLine: %v", err)
	}
	if disp.OriginalOpcode != compile.LOAD_CONST {
		t.Fatalf("OriginalOpcode = %v, want LOAD_CONST", disp.OriginalOpcode)
	}
	if seen != 1 {
		t.Fatalf("line callback fired %d times, want 1", seen)
	}
}
