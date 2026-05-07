package monitor

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

func TestInterpStateUseAndFreeToolID(t *testing.T) {
	s := NewInterpState()
	if err := s.UseToolID(ToolDebugger, objects.NewStr("debug")); err != nil {
		t.Fatalf("UseToolID(Debugger) returned error: %v", err)
	}
	if !s.IsToolInUse(ToolDebugger) {
		t.Fatalf("IsToolInUse(Debugger) = false after UseToolID")
	}
	if name := s.ToolName(ToolDebugger); name == nil {
		t.Fatalf("ToolName(Debugger) = nil after UseToolID")
	}
	if err := s.UseToolID(ToolDebugger, objects.NewStr("again")); err == nil {
		t.Fatalf("UseToolID on busy slot should error")
	}
	if err := s.FreeToolID(ToolDebugger); err != nil {
		t.Fatalf("FreeToolID returned error: %v", err)
	}
	if s.IsToolInUse(ToolDebugger) {
		t.Fatalf("slot still in use after FreeToolID")
	}
}

func TestInterpStateInvalidToolID(t *testing.T) {
	s := NewInterpState()
	if err := s.UseToolID(MaxToolID, objects.NewStr("x")); err == nil {
		t.Fatalf("UseToolID(MaxToolID) should fail")
	}
	if _, err := s.RegisterCallback(MaxToolID, EventCall, nil); err == nil {
		t.Fatalf("RegisterCallback on MaxToolID should fail")
	}
	if err := s.SetEvents(MaxToolID, 0); err == nil {
		t.Fatalf("SetEvents on MaxToolID should fail")
	}
}

func TestInterpStateSetAndGetEvents(t *testing.T) {
	s := NewInterpState()
	if got := s.GetEvents(ToolCoverage); got != 0 {
		t.Fatalf("GetEvents on fresh state = %#x, want 0", got)
	}
	v0 := s.GlobalVersion()
	want := EventSet(0).With(EventLine).With(EventCall)
	if err := s.SetEvents(ToolCoverage, want); err != nil {
		t.Fatalf("SetEvents: %v", err)
	}
	if got := s.GetEvents(ToolCoverage); got != want {
		t.Fatalf("GetEvents = %#x, want %#x", got, want)
	}
	if !s.Monitors.IsActive(EventLine) {
		t.Fatalf("Monitors.IsActive(Line) = false after SetEvents")
	}
	if !s.Monitors.IsActive(EventCall) {
		t.Fatalf("Monitors.IsActive(Call) = false after SetEvents")
	}
	if s.Monitors.IsActive(EventRaise) {
		t.Fatalf("Monitors.IsActive(Raise) = true unexpectedly")
	}
	if v1 := s.GlobalVersion(); v1 == v0 {
		t.Fatalf("GlobalVersion did not bump after SetEvents")
	}

	// Setting the same mask is a no-op and must not bump versions.
	v1 := s.GlobalVersion()
	tv1 := s.ToolVersions[ToolCoverage]
	if err := s.SetEvents(ToolCoverage, want); err != nil {
		t.Fatalf("SetEvents (no-op): %v", err)
	}
	if s.GlobalVersion() != v1 {
		t.Fatalf("GlobalVersion bumped on no-op SetEvents")
	}
	if s.ToolVersions[ToolCoverage] != tv1 {
		t.Fatalf("ToolVersion bumped on no-op SetEvents")
	}
}

func TestInterpStateSetEventsRejectsHighBits(t *testing.T) {
	s := NewInterpState()
	if err := s.SetEvents(ToolCoverage, EventSet(1)<<UngroupedEvents); err == nil {
		t.Fatalf("SetEvents with high bit should fail")
	}
}

func TestInterpStateSetEventsTwoToolsOnSameEvent(t *testing.T) {
	s := NewInterpState()
	if err := s.SetEvents(ToolDebugger, EventSet(0).With(EventCall)); err != nil {
		t.Fatalf("SetEvents Debugger: %v", err)
	}
	if err := s.SetEvents(ToolCoverage, EventSet(0).With(EventCall)); err != nil {
		t.Fatalf("SetEvents Coverage: %v", err)
	}
	want := uint8(1<<ToolDebugger) | uint8(1<<ToolCoverage)
	if got := s.Monitors.Tools[EventCall]; got != want {
		t.Fatalf("Monitors.Tools[Call] = %#b, want %#b", got, want)
	}
	// Clearing one tool must leave the other in place.
	if err := s.SetEvents(ToolDebugger, 0); err != nil {
		t.Fatalf("SetEvents clear Debugger: %v", err)
	}
	if got := s.Monitors.Tools[EventCall]; got != uint8(1<<ToolCoverage) {
		t.Fatalf("Monitors.Tools[Call] = %#b after clearing Debugger, want only Coverage", got)
	}
}

func TestInterpStateRegisterCallback(t *testing.T) {
	s := NewInterpState()
	cb := objects.NewStr("cb")
	prev, err := s.RegisterCallback(ToolDebugger, EventCall, cb)
	if err != nil {
		t.Fatalf("RegisterCallback: %v", err)
	}
	if prev != nil {
		t.Fatalf("first RegisterCallback returned non-nil previous")
	}
	if got := s.Callback(ToolDebugger, EventCall); got != cb {
		t.Fatalf("Callback(Debugger, Call) = %v, want %v", got, cb)
	}
	cb2 := objects.NewStr("cb2")
	prev, err = s.RegisterCallback(ToolDebugger, EventCall, cb2)
	if err != nil {
		t.Fatalf("RegisterCallback (replace): %v", err)
	}
	if prev != cb {
		t.Fatalf("second RegisterCallback returned %v, want %v", prev, cb)
	}
	if got := s.Callback(ToolDebugger, EventCall); got != cb2 {
		t.Fatalf("Callback(Debugger, Call) after replace = %v, want %v", got, cb2)
	}
	prev, err = s.RegisterCallback(ToolDebugger, EventCall, nil)
	if err != nil {
		t.Fatalf("RegisterCallback (clear): %v", err)
	}
	if prev != cb2 {
		t.Fatalf("clear returned %v, want %v", prev, cb2)
	}
	if got := s.Callback(ToolDebugger, EventCall); got != nil {
		t.Fatalf("Callback after clear = %v, want nil", got)
	}
}

func TestInterpStateRegisterCallbackBranchSplits(t *testing.T) {
	s := NewInterpState()
	cb := objects.NewStr("branch")
	prev, err := s.RegisterCallback(ToolDebugger, EventBranch, cb)
	if err != nil {
		t.Fatalf("RegisterCallback Branch: %v", err)
	}
	if prev != nil {
		t.Fatalf("first Branch register returned non-nil previous")
	}
	if got := s.Callback(ToolDebugger, EventBranchLeft); got != cb {
		t.Fatalf("BranchLeft callback = %v, want %v", got, cb)
	}
	if got := s.Callback(ToolDebugger, EventBranchRight); got != cb {
		t.Fatalf("BranchRight callback = %v, want %v", got, cb)
	}
}

func TestInterpStateClearToolIDDropsCallbacksAndEvents(t *testing.T) {
	s := NewInterpState()
	if err := s.UseToolID(ToolDebugger, objects.NewStr("debug")); err != nil {
		t.Fatalf("UseToolID: %v", err)
	}
	if err := s.SetEvents(ToolDebugger, EventSet(0).With(EventLine).With(EventCall)); err != nil {
		t.Fatalf("SetEvents: %v", err)
	}
	if _, err := s.RegisterCallback(ToolDebugger, EventCall, objects.NewStr("cb")); err != nil {
		t.Fatalf("RegisterCallback: %v", err)
	}
	tv0 := s.ToolVersions[ToolDebugger]
	gv0 := s.GlobalVersion()
	if err := s.ClearToolID(ToolDebugger); err != nil {
		t.Fatalf("ClearToolID: %v", err)
	}
	if got := s.GetEvents(ToolDebugger); got != 0 {
		t.Fatalf("events not cleared, got %#x", got)
	}
	if got := s.Callback(ToolDebugger, EventCall); got != nil {
		t.Fatalf("callback not cleared")
	}
	if !s.IsToolInUse(ToolDebugger) {
		t.Fatalf("ClearToolID dropped the tool name; should only clear registrations")
	}
	if s.ToolVersions[ToolDebugger] == tv0 {
		t.Fatalf("ToolVersion did not bump on ClearToolID")
	}
	if s.GlobalVersion() == gv0 {
		t.Fatalf("GlobalVersion did not bump on ClearToolID")
	}
}

func TestInterpStateFreeToolIDClearsRegistrations(t *testing.T) {
	s := NewInterpState()
	if err := s.UseToolID(ToolDebugger, objects.NewStr("debug")); err != nil {
		t.Fatalf("UseToolID: %v", err)
	}
	if err := s.SetEvents(ToolDebugger, EventSet(0).With(EventCall)); err != nil {
		t.Fatalf("SetEvents: %v", err)
	}
	if err := s.FreeToolID(ToolDebugger); err != nil {
		t.Fatalf("FreeToolID: %v", err)
	}
	if s.IsToolInUse(ToolDebugger) {
		t.Fatalf("slot still in use after FreeToolID")
	}
	if got := s.GetEvents(ToolDebugger); got != 0 {
		t.Fatalf("events not cleared after FreeToolID, got %#x", got)
	}
}
