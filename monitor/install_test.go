package monitor

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

// makeCode builds a tiny bytecode blob from a list of (op, arg) pairs.
// The Quickened flag is left false so cacheCount returns 0; the shadow
// walk treats every codeunit as a single instruction with no cache.
func makeCode(pairs ...byte) *objects.Code {
	if len(pairs)%2 != 0 {
		panic("makeCode: pairs must be even-length")
	}
	c := &objects.Code{}
	c.Code = append([]byte{}, pairs...)
	return c
}

func TestInstrumentNoEventsLeavesBytecodeAlone(t *testing.T) {
	s := NewInterpState()
	if err := s.UseToolID(ToolDebugger, objects.NewStr("debug")); err != nil {
		t.Fatalf("UseToolID: %v", err)
	}
	code := makeCode(byte(compile.RETURN_VALUE), 0)
	if err := Instrument(code, s); err != nil {
		t.Fatalf("Instrument: %v", err)
	}
	if got := compile.Opcode(code.Code[0]); got != compile.RETURN_VALUE {
		t.Fatalf("Instrument with no subscribers rewrote opcode to %v", got)
	}
}

func TestInstrumentRewritesReturnValueWhenToolSubscribes(t *testing.T) {
	s := NewInterpState()
	if err := s.UseToolID(ToolDebugger, objects.NewStr("debug")); err != nil {
		t.Fatalf("UseToolID: %v", err)
	}
	if err := s.SetEvents(ToolDebugger, EventSet(0).With(EventPyReturn)); err != nil {
		t.Fatalf("SetEvents: %v", err)
	}
	code := makeCode(byte(compile.RETURN_VALUE), 0)
	if err := Instrument(code, s); err != nil {
		t.Fatalf("Instrument: %v", err)
	}
	if got := compile.Opcode(code.Code[0]); got != compile.INSTRUMENTED_RETURN_VALUE {
		t.Fatalf("Instrument did not rewrite to INSTRUMENTED_RETURN_VALUE, got %v", got)
	}
	data := CoMonitoring(code)
	if data == nil {
		t.Fatalf("Instrument did not allocate CoMonitoringData")
	}
	if got := data.Tools[0]; got != 1<<ToolDebugger {
		t.Fatalf("data.Tools[0] = %#b, want %#b", got, 1<<ToolDebugger)
	}
	if data.ActiveMonitors.Tools[EventPyReturn] != 1<<ToolDebugger {
		t.Fatalf("ActiveMonitors not refreshed")
	}
	if code.MonitoringVersion != s.GlobalVersion() {
		t.Fatalf("MonitoringVersion not synced after Instrument")
	}
}

func TestInstrumentTwiceIsNoOp(t *testing.T) {
	s := NewInterpState()
	if err := s.UseToolID(ToolDebugger, objects.NewStr("debug")); err != nil {
		t.Fatalf("UseToolID: %v", err)
	}
	if err := s.SetEvents(ToolDebugger, EventSet(0).With(EventPyReturn)); err != nil {
		t.Fatalf("SetEvents: %v", err)
	}
	code := makeCode(byte(compile.RETURN_VALUE), 0)
	if err := Instrument(code, s); err != nil {
		t.Fatalf("Instrument: %v", err)
	}
	first := compile.Opcode(code.Code[0])
	if err := Instrument(code, s); err != nil {
		t.Fatalf("Instrument (second): %v", err)
	}
	if got := compile.Opcode(code.Code[0]); got != first {
		t.Fatalf("second Instrument rewrote opcode")
	}
}

func TestInstrumentRemovesToolWhenEventCleared(t *testing.T) {
	s := NewInterpState()
	if err := s.UseToolID(ToolDebugger, objects.NewStr("debug")); err != nil {
		t.Fatalf("UseToolID: %v", err)
	}
	if err := s.SetEvents(ToolDebugger, EventSet(0).With(EventPyReturn)); err != nil {
		t.Fatalf("SetEvents: %v", err)
	}
	code := makeCode(byte(compile.RETURN_VALUE), 0)
	if err := Instrument(code, s); err != nil {
		t.Fatalf("Instrument: %v", err)
	}
	if compile.Opcode(code.Code[0]) != compile.INSTRUMENTED_RETURN_VALUE {
		t.Fatalf("setup: opcode not instrumented")
	}
	if err := s.SetEvents(ToolDebugger, 0); err != nil {
		t.Fatalf("SetEvents (clear): %v", err)
	}
	if err := Instrument(code, s); err != nil {
		t.Fatalf("Instrument (clear): %v", err)
	}
	if got := compile.Opcode(code.Code[0]); got != compile.RETURN_VALUE {
		t.Fatalf("Instrument did not restore base opcode, got %v", got)
	}
	if got := CoMonitoring(code).Tools[0]; got != 0 {
		t.Fatalf("data.Tools[0] = %#b, want 0", got)
	}
}

func TestInstrumentRespectsLocalEvents(t *testing.T) {
	s := NewInterpState()
	if err := s.UseToolID(ToolDebugger, objects.NewStr("debug")); err != nil {
		t.Fatalf("UseToolID: %v", err)
	}
	code := makeCode(byte(compile.CALL), 0)
	if err := s.SetLocalEvents(code, ToolDebugger, EventSet(0).With(EventCall)); err != nil {
		t.Fatalf("SetLocalEvents: %v", err)
	}
	if err := Instrument(code, s); err != nil {
		t.Fatalf("Instrument: %v", err)
	}
	if got := compile.Opcode(code.Code[0]); got != compile.INSTRUMENTED_CALL {
		t.Fatalf("Instrument did not rewrite CALL, got %v", got)
	}
}

func TestInstrumentSkipsOpcodesWithoutEvents(t *testing.T) {
	s := NewInterpState()
	if err := s.UseToolID(ToolDebugger, objects.NewStr("debug")); err != nil {
		t.Fatalf("UseToolID: %v", err)
	}
	if err := s.SetEvents(ToolDebugger, EventSet(0).With(EventPyReturn).With(EventCall)); err != nil {
		t.Fatalf("SetEvents: %v", err)
	}
	// LOAD_CONST has no monitoring event; the walk must leave it
	// alone even when other tools are subscribed.
	code := makeCode(byte(compile.LOAD_CONST), 0, byte(compile.RETURN_VALUE), 0)
	if err := Instrument(code, s); err != nil {
		t.Fatalf("Instrument: %v", err)
	}
	if got := compile.Opcode(code.Code[0]); got != compile.LOAD_CONST {
		t.Fatalf("LOAD_CONST rewritten unexpectedly to %v", got)
	}
	if got := compile.Opcode(code.Code[2]); got != compile.INSTRUMENTED_RETURN_VALUE {
		t.Fatalf("RETURN_VALUE not rewritten, got %v", got)
	}
}
