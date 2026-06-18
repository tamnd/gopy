// Per-code event registration. CPython's set_local_events /
// get_local_events let a tool subscribe to instrumented events on a
// specific code object only; the union of LocalMonitors and the
// per-interpreter GlobalMonitors lands in CoMonitoringData.ActiveMonitors
// for the shadow walk to read.
//
// CPython: Python/instrumentation.c:1976 set_local_events

package monitor

import (
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// EnsureCoMonitoringData allocates code.MonitoringData on first use
// and returns the concrete pointer. Subsequent calls reuse the
// existing instance. Mirrors CPython's allocate_instrumentation_data.
//
// CPython: Python/instrumentation.c:1828 allocate_instrumentation_data
func EnsureCoMonitoringData(code *objects.Code) *CoMonitoringData {
	if data, ok := code.MonitoringData.(*CoMonitoringData); ok && data != nil {
		return data
	}
	data := &CoMonitoringData{}
	code.MonitoringData = data
	return data
}

// CoMonitoring returns the existing CoMonitoringData for code or nil
// if no instrument pass has run yet.
func CoMonitoring(code *objects.Code) *CoMonitoringData {
	if data, ok := code.MonitoringData.(*CoMonitoringData); ok {
		return data
	}
	return nil
}

// SetLocalEvents installs the local event mask for tool on code.
// Errors if the tool is not in use. Returns nil on success and bumps
// the per-tool version so the shadow walk re-instruments code on next
// run.
//
// CPython: Python/instrumentation.c:2033 _PyMonitoring_SetLocalEvents
func (s *InterpState) SetLocalEvents(code *objects.Code, tool Tool, events EventSet) error {
	if err := CheckToolID(tool); err != nil {
		return err
	}
	if events>>LocalEvents != 0 {
		return fmt.Errorf("event set %#x has bits outside the %d local events", events, LocalEvents)
	}
	if err := s.checkTool(tool); err != nil {
		return err
	}
	data := EnsureCoMonitoringData(code)
	data.ToolVersions[tool] = s.ToolVersions[tool]
	if existing := getLocalEvents(&data.LocalMonitors, tool); existing == events {
		return nil
	}
	setLocalEvents(&data.LocalMonitors, tool, events)
	s.ToolVersions[tool]++
	s.bumpVersion()
	return nil
}

// GetLocalEvents returns the local event mask for tool on code, or 0
// if no instrument data has been allocated yet.
//
// CPython: Python/instrumentation.c:2065 _PyMonitoring_GetLocalEvents
func (s *InterpState) GetLocalEvents(code *objects.Code, tool Tool) (EventSet, error) {
	if err := CheckToolID(tool); err != nil {
		return 0, err
	}
	if err := s.checkTool(tool); err != nil {
		return 0, err
	}
	data := CoMonitoring(code)
	if data == nil {
		return 0, nil
	}
	return getLocalEvents(&data.LocalMonitors, tool), nil
}

// getLocalEvents reads the bit-per-event union for tool out of the
// per-event tool tables.
//
// CPython: Python/instrumentation.c:266 get_local_events
func getLocalEvents(local *LocalMonitors, tool Tool) EventSet {
	var result EventSet
	for ev := 0; ev < LocalEvents; ev++ {
		if (local.Tools[ev]>>tool)&1 != 0 {
			result |= 1 << ev
		}
	}
	return result
}

// setLocalEvents stamps the per-event tool tables with the bits in
// events.
//
// CPython: Python/instrumentation.c:1976 set_local_events
func setLocalEvents(local *LocalMonitors, tool Tool, events EventSet) {
	bit := uint8(1) << tool
	for ev := 0; ev < LocalEvents; ev++ {
		active := uint8((events >> ev) & 1)
		local.Tools[ev] &^= bit
		local.Tools[ev] |= active << tool
	}
}
