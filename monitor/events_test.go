package monitor

import "testing"

func TestIsInstrumentedEvent(t *testing.T) {
	for _, ev := range []Event{
		EventPyStart, EventPyResume, EventPyReturn, EventPyYield,
		EventCall, EventLine, EventInstruction, EventJump,
		EventBranchLeft, EventBranchRight, EventStopIteration,
	} {
		if !IsInstrumentedEvent(ev) {
			t.Fatalf("IsInstrumentedEvent(%d) = false, want true", ev)
		}
	}
	for _, ev := range []Event{
		EventRaise, EventExceptionHandled, EventPyUnwind,
		EventPyThrow, EventReraise,
		EventCReturn, EventCRaise, EventBranch,
	} {
		if IsInstrumentedEvent(ev) {
			t.Fatalf("IsInstrumentedEvent(%d) = true, want false", ev)
		}
	}
}

func TestEventSetHasWithWithout(t *testing.T) {
	var s EventSet
	if s.Has(EventLine) {
		t.Fatalf("empty set should not contain Line")
	}
	s = s.With(EventLine).With(EventCall)
	if !s.Has(EventLine) || !s.Has(EventCall) {
		t.Fatalf("set should contain Line and Call after With, got %#x", s)
	}
	if s.Has(EventRaise) {
		t.Fatalf("set should not contain Raise, got %#x", s)
	}
	s = s.Without(EventLine)
	if s.Has(EventLine) {
		t.Fatalf("Without(Line) failed, got %#x", s)
	}
	if !s.Has(EventCall) {
		t.Fatalf("Without(Line) dropped Call too, got %#x", s)
	}
}

func TestEventCounts(t *testing.T) {
	if LocalEvents != 11 {
		t.Fatalf("LocalEvents = %d, want 11", LocalEvents)
	}
	if UngroupedEvents != 16 {
		t.Fatalf("UngroupedEvents = %d, want 16", UngroupedEvents)
	}
	if Events != 19 {
		t.Fatalf("Events = %d, want 19", Events)
	}
}

func TestGlobalMonitorsIsActive(t *testing.T) {
	var g GlobalMonitors
	if g.IsActive(EventCall) {
		t.Fatalf("zero-value GlobalMonitors should not be active for Call")
	}
	g.Tools[EventCall] = 1 << ToolDebugger
	if !g.IsActive(EventCall) {
		t.Fatalf("GlobalMonitors with debugger bit should be active for Call")
	}
	if g.IsActive(EventCReturn) {
		t.Fatalf("Call bit should not leak into CReturn")
	}
	if g.IsActive(Event(UngroupedEvents)) {
		t.Fatalf("out-of-range event should not be reported active")
	}
}

func TestLocalMonitorsIsActive(t *testing.T) {
	var l LocalMonitors
	if l.IsActive(EventLine) {
		t.Fatalf("zero-value LocalMonitors should not be active for Line")
	}
	l.Tools[EventLine] = 1 << ToolCoverage
	if !l.IsActive(EventLine) {
		t.Fatalf("LocalMonitors with coverage bit should be active for Line")
	}
	if l.IsActive(Event(LocalEvents)) {
		t.Fatalf("out-of-range event should not be reported active")
	}
}

func TestToolIDsAreDistinct(t *testing.T) {
	tools := []Tool{
		ToolDebugger, ToolCoverage, ToolProfiler, ToolOptimizer,
		ToolSysProfile, ToolSysTrace,
	}
	for _, tool := range tools {
		if tool >= MaxToolID {
			t.Fatalf("tool %d outside MaxToolID=%d", tool, MaxToolID)
		}
	}
	seen := map[Tool]bool{}
	for _, tool := range tools {
		if seen[tool] {
			t.Fatalf("tool %d duplicated", tool)
		}
		seen[tool] = true
	}
}
