// sys.monitoring builtin module construction. CPython exposes the
// PEP 669 surface as the "sys.monitoring" module, with all method
// entries closing over the running interpreter. gopy builds the same
// shape per-interpreter from the InterpState pointer the lifecycle
// already owns.
//
// CPython: Python/instrumentation.c:2529 _Py_CreateMonitoringObject

package monitor

import (
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// BuildSysMonitoring returns a *Module populated with the PEP 669
// API methods, the DISABLE / MISSING sentinels, and the events
// namespace. The methods close over interp so the caller does not
// have to thread a thread-state argument through every Python-level
// invocation.
//
// CPython: Python/instrumentation.c:2529 _Py_CreateMonitoringObject
func BuildSysMonitoring(interp *InterpState) *objects.Module {
	mod := objects.NewModule("sys.monitoring")
	d := mod.Dict()

	_ = d.SetItem(objects.NewStr("DISABLE"), Disable)
	_ = d.SetItem(objects.NewStr("MISSING"), Missing)

	_ = d.SetItem(objects.NewStr("DEBUGGER_ID"), objects.NewInt(int64(ToolDebugger)))
	_ = d.SetItem(objects.NewStr("COVERAGE_ID"), objects.NewInt(int64(ToolCoverage)))
	_ = d.SetItem(objects.NewStr("PROFILER_ID"), objects.NewInt(int64(ToolProfiler)))
	_ = d.SetItem(objects.NewStr("OPTIMIZER_ID"), objects.NewInt(int64(ToolOptimizer)))

	events := objects.NewModule("sys.monitoring.events")
	for i, name := range eventNames {
		_ = events.Dict().SetItem(objects.NewStr(name), objects.NewInt(int64(1)<<i))
	}
	_ = events.Dict().SetItem(objects.NewStr("NO_EVENTS"), objects.NewInt(0))
	_ = d.SetItem(objects.NewStr("events"), events)

	bind := func(name string, fn func([]objects.Object, map[string]objects.Object) (objects.Object, error)) {
		_ = d.SetItem(objects.NewStr(name), objects.NewBuiltinFunction(name, fn))
	}

	bind("use_tool_id", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		tool, name, err := parseToolName(args)
		if err != nil {
			return nil, err
		}
		if err := interp.UseToolID(tool, name); err != nil {
			return nil, err
		}
		return objects.None(), nil
	})
	bind("free_tool_id", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		tool, err := parseTool(args)
		if err != nil {
			return nil, err
		}
		if err := interp.FreeToolID(tool); err != nil {
			return nil, err
		}
		return objects.None(), nil
	})
	bind("clear_tool_id", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		tool, err := parseTool(args)
		if err != nil {
			return nil, err
		}
		if err := interp.ClearToolID(tool); err != nil {
			return nil, err
		}
		return objects.None(), nil
	})
	bind("get_tool", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		tool, err := parseTool(args)
		if err != nil {
			return nil, err
		}
		name := interp.ToolName(tool)
		if name == nil {
			return objects.None(), nil
		}
		return name, nil
	})
	bind("register_callback", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		if len(args) != 3 {
			return nil, fmt.Errorf("register_callback() takes exactly 3 arguments (got %d)", len(args))
		}
		tool, err := toolFromObject(args[0])
		if err != nil {
			return nil, err
		}
		eventBit, err := intFromObject(args[1])
		if err != nil {
			return nil, err
		}
		ev, err := eventFromBit(eventBit)
		if err != nil {
			return nil, err
		}
		var cb objects.Object = args[2]
		if cb == objects.None() {
			cb = nil
		}
		prev, err := interp.RegisterCallback(tool, ev, cb)
		if err != nil {
			return nil, err
		}
		if prev == nil {
			return objects.None(), nil
		}
		return prev, nil
	})
	bind("get_events", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		tool, err := parseTool(args)
		if err != nil {
			return nil, err
		}
		return objects.NewInt(int64(interp.GetEvents(tool))), nil
	})
	bind("set_events", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("set_events() takes exactly 2 arguments (got %d)", len(args))
		}
		tool, err := toolFromObject(args[0])
		if err != nil {
			return nil, err
		}
		raw, err := intFromObject(args[1])
		if err != nil {
			return nil, err
		}
		set, err := normalizeEventSet(int64(raw), Events)
		if err != nil {
			return nil, err
		}
		if err := interp.SetEvents(tool, set); err != nil {
			return nil, err
		}
		return objects.None(), nil
	})
	bind("get_local_events", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		tool, code, err := parseToolCode(args)
		if err != nil {
			return nil, err
		}
		set, err := interp.GetLocalEvents(code, tool)
		if err != nil {
			return nil, err
		}
		return objects.NewInt(int64(set)), nil
	})
	bind("set_local_events", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		if len(args) != 3 {
			return nil, fmt.Errorf("set_local_events() takes exactly 3 arguments (got %d)", len(args))
		}
		tool, err := toolFromObject(args[0])
		if err != nil {
			return nil, err
		}
		code, ok := args[1].(*objects.Code)
		if !ok {
			return nil, fmt.Errorf("set_local_events() expected a code object, got %T", args[1])
		}
		raw, err := intFromObject(args[2])
		if err != nil {
			return nil, err
		}
		set, err := normalizeEventSet(int64(raw), LocalEvents)
		if err != nil {
			return nil, err
		}
		if err := interp.SetLocalEvents(code, tool, set); err != nil {
			return nil, err
		}
		return objects.None(), nil
	})
	bind("restart_events", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		interp.RestartEvents()
		return objects.None(), nil
	})
	bind("_all_events", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		out := objects.NewDict()
		for ev := 0; ev < UngroupedEvents; ev++ {
			tools := interp.Monitors.Tools[ev]
			if tools == 0 {
				continue
			}
			_ = out.SetItem(objects.NewStr(eventNames[ev]), objects.NewInt(int64(tools)))
		}
		return out, nil
	})

	return mod
}

// parseTool unpacks a single (tool_id) positional argument.
func parseTool(args []objects.Object) (Tool, error) {
	if len(args) != 1 {
		return 0, fmt.Errorf("expected 1 argument, got %d", len(args))
	}
	return toolFromObject(args[0])
}

// parseToolName unpacks a (tool_id, name) positional argument pair.
func parseToolName(args []objects.Object) (Tool, objects.Object, error) {
	if len(args) != 2 {
		return 0, nil, fmt.Errorf("expected 2 arguments, got %d", len(args))
	}
	tool, err := toolFromObject(args[0])
	if err != nil {
		return 0, nil, err
	}
	if _, ok := args[1].(*objects.Unicode); !ok {
		return 0, nil, fmt.Errorf("tool name must be a str")
	}
	return tool, args[1], nil
}

// parseToolCode unpacks a (tool_id, code) positional argument pair.
func parseToolCode(args []objects.Object) (Tool, *objects.Code, error) {
	if len(args) != 2 {
		return 0, nil, fmt.Errorf("expected 2 arguments, got %d", len(args))
	}
	tool, err := toolFromObject(args[0])
	if err != nil {
		return 0, nil, err
	}
	code, ok := args[1].(*objects.Code)
	if !ok {
		return 0, nil, fmt.Errorf("expected a code object, got %T", args[1])
	}
	return tool, code, nil
}

// toolFromObject reads a tool ID out of a Python int and validates it.
func toolFromObject(o objects.Object) (Tool, error) {
	v, err := intFromObject(o)
	if err != nil {
		return 0, err
	}
	if v < 0 || v >= int(MaxToolID) {
		return 0, fmt.Errorf("invalid tool %d (must be between 0 and %d)", v, MaxToolID-1)
	}
	return Tool(v), nil
}

// intFromObject narrows an Object to a small int value.
func intFromObject(o objects.Object) (int, error) {
	i, ok := o.(*objects.Int)
	if !ok {
		return 0, fmt.Errorf("expected int, got %T", o)
	}
	v, ok := i.Int64()
	if !ok {
		return 0, fmt.Errorf("int does not fit in machine word")
	}
	return int(v), nil
}

// eventFromBit converts a single-bit event constant (1 << event_id)
// to the Event ID. Errors if the value has zero or more than one bit
// set or is out of range.
func eventFromBit(bit int) (Event, error) {
	if bit <= 0 || bit&(bit-1) != 0 {
		return 0, fmt.Errorf("the callback can only be set for one event at a time")
	}
	id := 0
	for v := bit; v > 1; v >>= 1 {
		id++
	}
	if id >= Events {
		return 0, fmt.Errorf("invalid event %d", bit)
	}
	return Event(id), nil
}

// normalizeEventSet validates an event-set bitmask and folds the
// grouped BRANCH bit into BRANCH_LEFT / BRANCH_RIGHT, mirroring
// CPython's monitoring_set_events_impl.
//
// CPython: Python/instrumentation.c:2316 monitoring_set_events_impl
func normalizeEventSet(raw int64, max int) (EventSet, error) {
	if raw < 0 || raw >= (1<<max) {
		return 0, fmt.Errorf("invalid event set 0x%x", raw)
	}
	set := EventSet(raw)
	if set>>EventBranch&1 != 0 {
		set &^= 1 << EventBranch
		set |= 1 << EventBranchRight
		set |= 1 << EventBranchLeft
	}
	return set, nil
}

// eventNames carries the canonical PEP 669 event names, indexed by
// Event. Order matches the constants in events.go.
//
// CPython: Python/instrumentation.c:60 event_names
var eventNames = [...]string{
	"PY_START",
	"PY_RESUME",
	"PY_RETURN",
	"PY_YIELD",
	"CALL",
	"LINE",
	"INSTRUCTION",
	"JUMP",
	"BRANCH",
	"STOP_ITERATION",
	"BRANCH_RIGHT",
	"BRANCH_LEFT",
	"C_RETURN",
	"PY_THROW",
	"RAISE",
	"EXCEPTION_HANDLED",
	"C_RAISE",
	"PY_UNWIND",
	"RERAISE",
}
