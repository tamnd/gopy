package monitor

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

func TestSetLocalEventsRequiresToolInUse(t *testing.T) {
	s := NewInterpState()
	code := &objects.Code{}
	if err := s.SetLocalEvents(code, ToolDebugger, EventSet(0).With(EventLine)); err == nil {
		t.Fatalf("SetLocalEvents on free tool slot should fail")
	}
}

func TestSetAndGetLocalEvents(t *testing.T) {
	s := NewInterpState()
	if err := s.UseToolID(ToolDebugger, objects.NewStr("debug")); err != nil {
		t.Fatalf("UseToolID: %v", err)
	}
	code := &objects.Code{}

	got, err := s.GetLocalEvents(code, ToolDebugger)
	if err != nil {
		t.Fatalf("GetLocalEvents on uninstrumented code: %v", err)
	}
	if got != 0 {
		t.Errorf("GetLocalEvents on uninstrumented code = %#x, want 0", got)
	}

	want := EventSet(0).With(EventLine).With(EventCall)
	if err := s.SetLocalEvents(code, ToolDebugger, want); err != nil {
		t.Fatalf("SetLocalEvents: %v", err)
	}
	got, err = s.GetLocalEvents(code, ToolDebugger)
	if err != nil {
		t.Fatalf("GetLocalEvents: %v", err)
	}
	if got != want {
		t.Errorf("GetLocalEvents = %#x, want %#x", got, want)
	}
	data := CoMonitoring(code)
	if data == nil {
		t.Fatalf("CoMonitoring returned nil after SetLocalEvents")
	}
	if !data.LocalMonitors.IsActive(EventLine) {
		t.Errorf("LocalMonitors.IsActive(Line) = false")
	}
}

func TestSetLocalEventsRejectsHighBits(t *testing.T) {
	s := NewInterpState()
	if err := s.UseToolID(ToolDebugger, objects.NewStr("debug")); err != nil {
		t.Fatalf("UseToolID: %v", err)
	}
	code := &objects.Code{}
	if err := s.SetLocalEvents(code, ToolDebugger, EventSet(1)<<LocalEvents); err == nil {
		t.Fatalf("SetLocalEvents with high bit should fail")
	}
}

func TestSetLocalEventsBumpsToolVersion(t *testing.T) {
	s := NewInterpState()
	if err := s.UseToolID(ToolDebugger, objects.NewStr("debug")); err != nil {
		t.Fatalf("UseToolID: %v", err)
	}
	code := &objects.Code{}
	tv0 := s.ToolVersions[ToolDebugger]
	gv0 := s.GlobalVersion()
	if err := s.SetLocalEvents(code, ToolDebugger, EventSet(0).With(EventLine)); err != nil {
		t.Fatalf("SetLocalEvents: %v", err)
	}
	if s.ToolVersions[ToolDebugger] == tv0 {
		t.Errorf("ToolVersion did not bump after SetLocalEvents")
	}
	if s.GlobalVersion() == gv0 {
		t.Errorf("GlobalVersion did not bump after SetLocalEvents")
	}
}

func TestSetLocalEventsNoOpDoesNotBump(t *testing.T) {
	s := NewInterpState()
	if err := s.UseToolID(ToolDebugger, objects.NewStr("debug")); err != nil {
		t.Fatalf("UseToolID: %v", err)
	}
	code := &objects.Code{}
	want := EventSet(0).With(EventLine)
	if err := s.SetLocalEvents(code, ToolDebugger, want); err != nil {
		t.Fatalf("SetLocalEvents (first): %v", err)
	}
	tv := s.ToolVersions[ToolDebugger]
	gv := s.GlobalVersion()
	if err := s.SetLocalEvents(code, ToolDebugger, want); err != nil {
		t.Fatalf("SetLocalEvents (no-op): %v", err)
	}
	if s.ToolVersions[ToolDebugger] != tv {
		t.Errorf("ToolVersion bumped on no-op SetLocalEvents")
	}
	if s.GlobalVersion() != gv {
		t.Errorf("GlobalVersion bumped on no-op SetLocalEvents")
	}
}

func TestEnsureCoMonitoringDataIsIdempotent(t *testing.T) {
	code := &objects.Code{}
	d1 := EnsureCoMonitoringData(code)
	d2 := EnsureCoMonitoringData(code)
	if d1 != d2 {
		t.Fatalf("EnsureCoMonitoringData allocated a fresh instance on the second call")
	}
}
