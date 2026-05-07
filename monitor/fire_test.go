package monitor

import (
	"errors"
	"testing"

	"github.com/tamnd/gopy/objects"
)

// callbackRecorder is a builtin function that captures every call
// and lets tests assert on the arguments.
type callbackRecorder struct {
	calls   [][]objects.Object
	respond func(args []objects.Object) (objects.Object, error)
}

func (r *callbackRecorder) build() objects.Object {
	return objects.NewBuiltinFunction("recorder", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		captured := make([]objects.Object, len(args))
		copy(captured, args)
		r.calls = append(r.calls, captured)
		if r.respond != nil {
			return r.respond(args)
		}
		return objects.None(), nil
	})
}

func newRecorder() *callbackRecorder { return &callbackRecorder{} }

func TestMostSignificantBit(t *testing.T) {
	cases := []struct {
		bits uint8
		want int
	}{
		{0b00000001, 0},
		{0b00000010, 1},
		{0b00000011, 1},
		{0b10000000, 7},
		{0b11111111, 7},
		{0b00010000, 4},
	}
	for _, c := range cases {
		if got := mostSignificantBit(c.bits); got != c.want {
			t.Errorf("mostSignificantBit(%#08b) = %d, want %d", c.bits, got, c.want)
		}
	}
}

func TestEnterScopeRefreshesActive(t *testing.T) {
	s := NewInterpState()
	if err := s.SetEvents(ToolDebugger, EventSet(0).With(EventCall).With(EventLine)); err != nil {
		t.Fatalf("SetEvents: %v", err)
	}
	states := make([]MonState, 2)
	var version uint32
	events := []Event{EventCall, EventLine}
	EnterScope(s, states, &version, events)
	if states[0].Active != 1<<ToolDebugger {
		t.Errorf("states[0].Active = %#b, want %#b", states[0].Active, 1<<ToolDebugger)
	}
	if states[1].Active != 1<<ToolDebugger {
		t.Errorf("states[1].Active = %#b, want %#b", states[1].Active, 1<<ToolDebugger)
	}
	if version != s.GlobalVersion() {
		t.Errorf("version not synced after EnterScope")
	}

	// Second call must be a no-op when version matches.
	states[0].Active = 0
	states[1].Active = 0
	EnterScope(s, states, &version, events)
	if states[0].Active != 0 || states[1].Active != 0 {
		t.Errorf("EnterScope refreshed despite matching version")
	}

	// Bumping the version forces a refresh on the next call.
	if err := s.SetEvents(ToolCoverage, EventSet(0).With(EventCall)); err != nil {
		t.Fatalf("SetEvents: %v", err)
	}
	EnterScope(s, states, &version, events)
	wantCall := uint8(1<<ToolDebugger | 1<<ToolCoverage)
	if states[0].Active != wantCall {
		t.Errorf("states[0].Active = %#b, want %#b", states[0].Active, wantCall)
	}
}

func TestFirePyStartCallsRegisteredCallback(t *testing.T) {
	s := NewInterpState()
	rec := newRecorder()
	if _, err := s.RegisterCallback(ToolDebugger, EventPyStart, rec.build()); err != nil {
		t.Fatalf("RegisterCallback: %v", err)
	}
	state := MonState{Active: 1 << ToolDebugger}
	code := objects.NewStr("code")
	if err := FirePyStart(s, &state, code, 42); err != nil {
		t.Fatalf("FirePyStart: %v", err)
	}
	if len(rec.calls) != 1 {
		t.Fatalf("recorder called %d times, want 1", len(rec.calls))
	}
	args := rec.calls[0]
	if len(args) != 2 {
		t.Fatalf("callback got %d args, want 2", len(args))
	}
	if args[0] != code {
		t.Errorf("arg0 = %v, want %v", args[0], code)
	}
	if i, ok := args[1].(*objects.Int); !ok {
		t.Errorf("arg1 = %v, want Int", args[1])
	} else if v, ok2 := i.Int64(); !ok2 || v != 42 {
		t.Errorf("arg1 = %v, want Int(42)", args[1])
	}
}

func TestFireCallPassesCallableAndArg0(t *testing.T) {
	s := NewInterpState()
	rec := newRecorder()
	if _, err := s.RegisterCallback(ToolProfiler, EventCall, rec.build()); err != nil {
		t.Fatalf("RegisterCallback: %v", err)
	}
	state := MonState{Active: 1 << ToolProfiler}
	code := objects.NewStr("code")
	callable := objects.NewStr("the_callable")
	arg0 := objects.NewStr("the_arg0")
	if err := FireCall(s, &state, code, 7, callable, arg0); err != nil {
		t.Fatalf("FireCall: %v", err)
	}
	if len(rec.calls) != 1 {
		t.Fatalf("recorder calls = %d", len(rec.calls))
	}
	args := rec.calls[0]
	if len(args) != 4 {
		t.Fatalf("callback got %d args, want 4", len(args))
	}
	if args[2] != callable || args[3] != arg0 {
		t.Errorf("call args = %v, want callable=%v arg0=%v", args, callable, arg0)
	}
}

func TestFireLineSubstitutesLineNumberForOffset(t *testing.T) {
	s := NewInterpState()
	rec := newRecorder()
	if _, err := s.RegisterCallback(ToolCoverage, EventLine, rec.build()); err != nil {
		t.Fatalf("RegisterCallback: %v", err)
	}
	state := MonState{Active: 1 << ToolCoverage}
	code := objects.NewStr("code")
	if err := FireLine(s, &state, code, 999, 12); err != nil {
		t.Fatalf("FireLine: %v", err)
	}
	args := rec.calls[0]
	if len(args) != 2 {
		t.Fatalf("line callback args = %d, want 2", len(args))
	}
	if i, ok := args[1].(*objects.Int); !ok {
		t.Errorf("line arg = %v, want Int", args[1])
	} else if v, ok2 := i.Int64(); !ok2 || v != 12 {
		t.Errorf("line arg = %v, want Int(12)", args[1])
	}
}

func TestFireSkipsTowerWhenSlotEmpty(t *testing.T) {
	s := NewInterpState()
	state := MonState{Active: 1 << ToolDebugger}
	if err := FirePyStart(s, &state, objects.NewStr("code"), 0); err != nil {
		t.Fatalf("FirePyStart with empty slot returned error: %v", err)
	}
}

func TestFirePropagatesCallbackError(t *testing.T) {
	s := NewInterpState()
	rec := newRecorder()
	rec.respond = func(_ []objects.Object) (objects.Object, error) {
		return nil, errors.New("boom")
	}
	if _, err := s.RegisterCallback(ToolDebugger, EventPyStart, rec.build()); err != nil {
		t.Fatalf("RegisterCallback: %v", err)
	}
	state := MonState{Active: 1 << ToolDebugger}
	if err := FirePyStart(s, &state, objects.NewStr("code"), 0); err == nil {
		t.Fatalf("expected error from FirePyStart, got nil")
	}
}

func TestFireDisableOnInstrumentedClearsActiveBit(t *testing.T) {
	s := NewInterpState()
	rec := newRecorder()
	rec.respond = func(_ []objects.Object) (objects.Object, error) { return Disable, nil }
	if _, err := s.RegisterCallback(ToolDebugger, EventLine, rec.build()); err != nil {
		t.Fatalf("RegisterCallback: %v", err)
	}
	state := MonState{Active: 1 << ToolDebugger}
	if err := FireLine(s, &state, objects.NewStr("code"), 0, 1); err != nil {
		t.Fatalf("FireLine: %v", err)
	}
	if state.Active != 0 {
		t.Fatalf("state.Active = %#b, want 0 after Disable", state.Active)
	}
	if got := s.Callback(ToolDebugger, EventLine); got == nil {
		t.Fatalf("Disable on instrumented event must keep the slot")
	}
}

func TestFireDisableOnNonInstrumentedClearsSlotAndErrors(t *testing.T) {
	s := NewInterpState()
	rec := newRecorder()
	rec.respond = func(_ []objects.Object) (objects.Object, error) { return Disable, nil }
	if _, err := s.RegisterCallback(ToolDebugger, EventRaise, rec.build()); err != nil {
		t.Fatalf("RegisterCallback: %v", err)
	}
	state := MonState{Active: 1 << ToolDebugger}
	err := FireRaise(s, &state, objects.NewStr("code"), 0, objects.NewStr("exc"))
	if err == nil {
		t.Fatalf("expected error when Disable returned for non-instrumented event")
	}
	if got := s.Callback(ToolDebugger, EventRaise); got != nil {
		t.Fatalf("Disable on non-instrumented event must clear the slot")
	}
}

func TestFireFanoutAcrossMultipleTools(t *testing.T) {
	s := NewInterpState()
	rec1 := newRecorder()
	rec2 := newRecorder()
	if _, err := s.RegisterCallback(ToolDebugger, EventPyStart, rec1.build()); err != nil {
		t.Fatalf("RegisterCallback Debugger: %v", err)
	}
	if _, err := s.RegisterCallback(ToolProfiler, EventPyStart, rec2.build()); err != nil {
		t.Fatalf("RegisterCallback Profiler: %v", err)
	}
	state := MonState{Active: (1 << ToolDebugger) | (1 << ToolProfiler)}
	if err := FirePyStart(s, &state, objects.NewStr("code"), 1); err != nil {
		t.Fatalf("FirePyStart: %v", err)
	}
	if len(rec1.calls) != 1 || len(rec2.calls) != 1 {
		t.Fatalf("expected both tools called once: rec1=%d rec2=%d", len(rec1.calls), len(rec2.calls))
	}
}
