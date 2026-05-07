package monitor

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

func callMethod(t *testing.T, mod *objects.Module, name string, args ...objects.Object) objects.Object {
	t.Helper()
	v, err := mod.Dict().GetItem(objects.NewStr(name))
	if err != nil {
		t.Fatalf("getitem(%q): %v", name, err)
	}
	tp := v.Type()
	if tp.Call == nil {
		t.Fatalf("%q is not callable", name)
	}
	out, err := tp.Call(v, args, nil)
	if err != nil {
		t.Fatalf("%q raised: %v", name, err)
	}
	return out
}

func TestSysMonitoringExposesConstants(t *testing.T) {
	mod := BuildSysMonitoring(NewInterpState())
	for _, name := range []string{
		"DISABLE", "MISSING", "DEBUGGER_ID", "COVERAGE_ID", "PROFILER_ID",
		"OPTIMIZER_ID", "events", "use_tool_id", "free_tool_id", "set_events",
		"register_callback", "_all_events",
	} {
		if _, err := mod.Dict().GetItem(objects.NewStr(name)); err != nil {
			t.Errorf("missing attribute %q: %v", name, err)
		}
	}

	events, _ := mod.Dict().GetItem(objects.NewStr("events"))
	em, ok := events.(*objects.Module)
	if !ok {
		t.Fatalf("events attribute type = %T, want *Module", events)
	}
	for _, name := range []string{"PY_RETURN", "LINE", "CALL", "NO_EVENTS"} {
		if _, err := em.Dict().GetItem(objects.NewStr(name)); err != nil {
			t.Errorf("events.%s missing: %v", name, err)
		}
	}
}

func TestSysMonitoringUseAndFreeToolID(t *testing.T) {
	interp := NewInterpState()
	mod := BuildSysMonitoring(interp)

	callMethod(t, mod, "use_tool_id", objects.NewInt(0), objects.NewStr("debugger"))
	out := callMethod(t, mod, "get_tool", objects.NewInt(0))
	if s, ok := out.(*objects.Unicode); !ok || s.Value() != "debugger" {
		t.Fatalf("get_tool result = %v, want \"debugger\"", out)
	}
	callMethod(t, mod, "free_tool_id", objects.NewInt(0))
	if got := interp.ToolName(0); got != nil {
		t.Fatalf("ToolName after free = %v, want nil", got)
	}
}

func TestSysMonitoringSetEventsSplitsBranch(t *testing.T) {
	interp := NewInterpState()
	mod := BuildSysMonitoring(interp)
	callMethod(t, mod, "use_tool_id", objects.NewInt(0), objects.NewStr("debug"))
	// Setting BRANCH should fan out to BRANCH_LEFT | BRANCH_RIGHT.
	branchBit := objects.NewInt(1 << EventBranch)
	callMethod(t, mod, "set_events", objects.NewInt(0), branchBit)
	out := callMethod(t, mod, "get_events", objects.NewInt(0))
	gotInt, ok := out.(*objects.Int)
	if !ok {
		t.Fatalf("get_events type = %T", out)
	}
	got, _ := gotInt.Int64()
	want := int64(1<<EventBranchLeft | 1<<EventBranchRight)
	if got != want {
		t.Fatalf("get_events = %#x, want %#x", got, want)
	}
}

func TestSysMonitoringRegisterCallback(t *testing.T) {
	interp := NewInterpState()
	mod := BuildSysMonitoring(interp)
	callMethod(t, mod, "use_tool_id", objects.NewInt(0), objects.NewStr("debug"))

	cb := objects.NewBuiltinFunction("noop", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return objects.None(), nil
	})
	prev := callMethod(t, mod, "register_callback",
		objects.NewInt(0),
		objects.NewInt(1<<EventPyReturn),
		cb,
	)
	if prev != objects.None() {
		t.Fatalf("first register prev = %v, want None", prev)
	}
	if got := interp.Callback(0, EventPyReturn); got == nil {
		t.Fatalf("callback not stored")
	}
}

func TestSysMonitoringRejectsBadToolID(t *testing.T) {
	interp := NewInterpState()
	mod := BuildSysMonitoring(interp)
	v, err := mod.Dict().GetItem(objects.NewStr("use_tool_id"))
	if err != nil {
		t.Fatalf("getitem: %v", err)
	}
	tp := v.Type()
	_, err = tp.Call(v, []objects.Object{objects.NewInt(99), objects.NewStr("x")}, nil)
	if err == nil {
		t.Fatalf("use_tool_id(99) succeeded, want error")
	}
}
