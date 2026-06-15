// Shadow walk that installs / removes the INSTRUMENTED_<X> opcodes.
// Called when the global monitoring version drifts from a code
// object's last-seen version. Uses the per-tool, per-event diff to
// avoid re-instrumenting bytecode on no-op rebumps.
//
// CPython: Python/instrumentation.c:1923 _Py_Instrument

package monitor

import (
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/specialize"
)

// Instrument walks code's bytecode and installs the INSTRUMENTED_<X>
// opcodes for every (event, offset) where a tool has subscribed.
// Returns nil on success. The walk is idempotent: re-running with the
// same monitor state is a no-op aside from refreshing the version.
//
// CPython: Python/instrumentation.c:1923 _Py_Instrument
func Instrument(code *objects.Code, interp *InterpState) error {
	if code.MonitoringVersion == interp.GlobalVersion() {
		return nil
	}
	return forceInstrument(code, interp)
}

// forceInstrument is the body of _Py_Instrument minus the
// version-up-to-date short-circuit. SetLocalEvents and SetEvents both
// land here directly because they have already pushed a new version
// and need the walk to reflect the change.
//
// CPython: Python/instrumentation.c:1776 force_instrument_lock_held
func forceInstrument(code *objects.Code, interp *InterpState) error {
	data := EnsureCoMonitoringData(code)

	active := localUnion(&interp.Monitors, &data.LocalMonitors)
	var newEvents, removedEvents LocalMonitors
	for ev := 0; ev < LocalEvents; ev++ {
		old := data.ActiveMonitors.Tools[ev]
		now := active.Tools[ev]
		removedEvents.Tools[ev] = old &^ now
		newEvents.Tools[ev] = now &^ old
	}
	data.ActiveMonitors = active

	if monitorsEmpty(&newEvents) && monitorsEmpty(&removedEvents) {
		code.MonitoringVersion = interp.GlobalVersion()
		return nil
	}

	if data.Tools == nil {
		data.Tools = make([]uint8, instructionCount(code))
	}
	if active.Tools[EventLine] != 0 {
		ensureLines(code, data)
		if multipleTools(active.Tools[EventLine]) && data.LineTools == nil {
			data.LineTools = make([]uint8, instructionCount(code))
			initializeLineTools(code, data, &active)
		}
	}

	for instr := 0; instr < len(data.Tools); {
		base := GetBaseCodeUnit(code, instr)
		if !OpcodeHasEvent(base) {
			instr += 1 + cacheCount(base, code)
			continue
		}
		ev := EventForOpcode(base)
		if int(ev) < LocalEvents && ev != EventLine {
			if removed := removedEvents.Tools[ev]; removed != 0 {
				removeTools(code, data, instr, removed)
			}
			if added := newEvents.Tools[ev]; added != 0 {
				addTools(code, data, instr, added)
			}
		}
		instr += 1 + cacheCount(base, code)
	}

	if removed := removedEvents.Tools[EventLine]; removed != 0 && data.Lines != nil {
		for instr := 0; instr < instructionCount(code); instr++ {
			if getOriginalOpcode(data.Lines, instr) != 0 {
				removeLineTools(code, data, instr, removed)
			}
		}
	}
	if added := newEvents.Tools[EventLine]; added != 0 && data.Lines != nil {
		for instr := 0; instr < instructionCount(code); instr++ {
			if getOriginalOpcode(data.Lines, instr) != 0 {
				addLineTools(code, data, instr, added)
			}
		}
	}

	code.MonitoringVersion = interp.GlobalVersion()
	return nil
}

// multipleTools reports whether more than one bit is set in mask.
// CPython allocates per-codeunit line/tool tables only when several
// tools observe the same code; a single tool can be encoded in the
// active_monitors mask alone.
//
// CPython: Python/instrumentation.c:1693 multiple_tools
func multipleTools(mask uint8) bool {
	return mask != 0 && (mask&(mask-1)) != 0
}

// instructionCount returns the number of codeunits in code's
// bytecode. Each codeunit is 2 bytes (opcode + arg).
func instructionCount(code *objects.Code) int {
	return len(code.Code) / 2
}

// byteAt returns the opcode byte at codeunit i.
func byteAt(b []byte, i int) byte {
	return b[2*i]
}

// cacheCount returns the per-opcode cache width when code is
// quickened. Non-quickened code has no cache cells.
func cacheCount(op compile.Opcode, code *objects.Code) int {
	if !code.Quickened {
		return 0
	}
	return specialize.CacheCount(op)
}

// localUnion computes the per-event union of the global monitors and
// a code object's local monitors. Mirrors CPython's local_union.
//
// CPython: Python/instrumentation.c local_union
func localUnion(global *GlobalMonitors, local *LocalMonitors) LocalMonitors {
	var out LocalMonitors
	for ev := 0; ev < LocalEvents; ev++ {
		out.Tools[ev] = global.Tools[ev] | local.Tools[ev]
	}
	return out
}

// monitorsEmpty reports whether m has no active tool bits. Mirrors
// CPython's monitors_are_empty.
//
// CPython: Python/instrumentation.c monitors_are_empty
func monitorsEmpty(m *LocalMonitors) bool {
	for _, t := range m.Tools {
		if t != 0 {
			return false
		}
	}
	return true
}

// addTools registers the bits in tools as observers of (offset,
// event) and stamps the bytecode with the matching INSTRUMENTED_<X>
// opcode if it was not already there.
//
// CPython: Python/instrumentation.c:889 add_tools
func addTools(code *objects.Code, data *CoMonitoringData, offset int, tools uint8) {
	if data.Tools == nil {
		data.Tools = make([]uint8, instructionCount(code))
	}
	data.Tools[offset] |= tools
	op := compile.Opcode(byteAt(code.Code, offset))
	if IsInstrumented(op) {
		return
	}
	base := specialize.Deopt(op)
	instrumented := InstrumentedFor(base)
	if instrumented == 0 {
		return
	}
	code.Code[2*offset] = byte(instrumented)
	if specialize.CacheCount(base) > 0 && code.Quickened {
		specialize.StoreCounter(code.Code, offset, specialize.AdaptiveCounterWarmup())
	}
}

// removeTools clears the bits in tools at (offset, event). When the
// per-codeunit tools mask falls to zero the opcode is restored to its
// adaptive parent so dispatch returns to the fast path.
//
// CPython: Python/instrumentation.c:828 remove_tools
func removeTools(code *objects.Code, data *CoMonitoringData, offset int, tools uint8) {
	if data.Tools == nil {
		return
	}
	data.Tools[offset] &^= tools
	if data.Tools[offset] != 0 {
		return
	}
	op := compile.Opcode(byteAt(code.Code, offset))
	if !IsInstrumented(op) {
		return
	}
	base := DeInstrument(op)
	code.Code[2*offset] = byte(base)
}
