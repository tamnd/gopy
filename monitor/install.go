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
	if active.Tools[EventInstruction] != 0 {
		ensurePerInstruction(code, data)
		if multipleTools(active.Tools[EventInstruction]) && data.PerInstructionTools == nil {
			data.PerInstructionTools = make([]uint8, instructionCount(code))
		}
	}

	for instr := 0; instr < len(data.Tools); {
		base := GetBaseCodeUnit(code, instr)
		if !OpcodeHasEvent(base) {
			instr += 1 + cacheCount(base, code)
			continue
		}
		ev := EventForOpcode(base)
		// RESUME has no static event: it fires PY_START at function
		// entry (arg 0) and PY_RESUME on a generator / coroutine
		// re-entry (arg > 0), so the event depends on the oparg.
		//
		// CPython: Python/instrumentation.c:1824 force_instrument_lock_held
		// (base_opcode == RESUME ? instr.op.arg > 0 ...)
		if base == compile.RESUME {
			if code.Code[2*instr+1] > 0 {
				ev = EventPyResume
			} else {
				ev = EventPyStart
			}
		}
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

	// GH-103845: line and instruction instrumentation must both be
	// removed before either is added, otherwise the add pass clobbers
	// freshly-installed markers.
	if removed := removedEvents.Tools[EventLine]; removed != 0 && data.Lines != nil {
		for instr := 0; instr < instructionCount(code); instr++ {
			if getOriginalOpcode(data.Lines, instr) != 0 {
				removeLineTools(code, data, instr, removed)
			}
		}
	}
	if removed := removedEvents.Tools[EventInstruction]; removed != 0 && data.PerInstructionOpcodes != nil {
		for instr := firstTraceable(code); instr < instructionCount(code); {
			base := GetBaseCodeUnit(code, instr)
			if base == compile.RESUME || base == compile.END_FOR {
				instr += 1 + cacheCount(base, code)
				continue
			}
			removePerInstructionTools(code, data, instr, removed)
			instr += 1 + cacheCount(base, code)
		}
	}
	if added := newEvents.Tools[EventLine]; added != 0 && data.Lines != nil {
		for instr := 0; instr < instructionCount(code); instr++ {
			if getOriginalOpcode(data.Lines, instr) != 0 {
				addLineTools(code, data, instr, added)
			}
		}
	}
	if added := newEvents.Tools[EventInstruction]; added != 0 && data.PerInstructionOpcodes != nil {
		for instr := firstTraceable(code); instr < instructionCount(code); {
			base := GetBaseCodeUnit(code, instr)
			if base == compile.RESUME || base == compile.END_FOR {
				instr += 1 + cacheCount(base, code)
				continue
			}
			addPerInstructionTools(code, data, instr, added)
			instr += 1 + cacheCount(base, code)
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
	instrument(code, data, offset)
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
	deInstrument(code, data, offset)
}

// opcodeRefKind selects which backing store currently holds the real
// opcode for a codeunit. CPython threads this as a bare uint8* opcode_ptr
// that is redirected into the line table or the per-instruction table as
// each instrumentation layer is unwrapped; Go cannot alias the three
// stores behind one pointer, so the backing store is carried explicitly
// and read/written through get/set.
//
// CPython: Python/instrumentation.c:757 instrument (opcode_ptr)
type opcodeRefKind int

const (
	refLive     opcodeRefKind = iota // code.Code[2*i]
	refLine                          // original opcode in the line table
	refPerInstr                      // per-instruction opcode side table
)

// opcodeRef is the resolved location of the runnable opcode for codeunit
// i, mirroring CPython's opcode_ptr after it has been walked through the
// INSTRUMENTED_LINE and INSTRUMENTED_INSTRUCTION side tables.
type opcodeRef struct {
	code *objects.Code
	data *CoMonitoringData
	i    int
	kind opcodeRefKind
}

func (r opcodeRef) get() compile.Opcode {
	switch r.kind {
	case refLine:
		return compile.Opcode(getOriginalOpcode(r.data.Lines, r.i))
	case refPerInstr:
		return compile.Opcode(r.data.PerInstructionOpcodes[r.i])
	default:
		return compile.Opcode(byteAt(r.code.Code, r.i))
	}
}

func (r opcodeRef) set(op compile.Opcode) {
	switch r.kind {
	case refLine:
		setOriginalOpcode(r.data.Lines, r.i, byte(op))
	case refPerInstr:
		r.data.PerInstructionOpcodes[r.i] = byte(op)
	default:
		r.code.Code[2*r.i] = byte(op)
	}
}

// resolveOpcodeRef walks the live byte through the INSTRUMENTED_LINE and
// INSTRUMENTED_INSTRUCTION side tables, returning the location that holds
// the opcode an instrument / de_instrument should rewrite. This is the
// shared opcode_ptr-walking prologue of CPython's instrument(),
// de_instrument(), and de_instrument_per_instruction().
//
// CPython: Python/instrumentation.c:757 instrument
func resolveOpcodeRef(code *objects.Code, data *CoMonitoringData, i int) (opcodeRef, compile.Opcode) {
	ref := opcodeRef{code: code, data: data, i: i, kind: refLive}
	op := ref.get()
	if op == compile.INSTRUMENTED_LINE {
		ref.kind = refLine
		op = ref.get()
	}
	if op == compile.INSTRUMENTED_INSTRUCTION {
		ref.kind = refPerInstr
		op = ref.get()
	}
	return ref, op
}

// instrument stamps the matching INSTRUMENTED_<X> opcode for codeunit i,
// writing through the resolved opcode_ptr so a site already hidden behind
// INSTRUMENTED_LINE or INSTRUMENTED_INSTRUCTION updates the hidden opcode
// in its side table rather than clobbering the visible marker. A slot
// that is already an instrumented variant is left untouched.
//
// CPython: Python/instrumentation.c:757 instrument
func instrument(code *objects.Code, data *CoMonitoringData, i int) {
	ref, op := resolveOpcodeRef(code, data, i)
	if IsInstrumented(op) {
		return
	}
	deopt := specialize.Deopt(op)
	instrumented := InstrumentedFor(deopt)
	if instrumented == 0 {
		return
	}
	ref.set(instrumented)
	if specialize.CacheCount(deopt) > 0 && code.Quickened {
		specialize.StoreCounter(code.Code, i, specialize.AdaptiveCounterWarmup())
	}
}

// deInstrument restores the original opcode for codeunit i, writing
// through the resolved opcode_ptr so a site hidden behind a line or
// per-instruction marker has its side-table opcode de-instrumented in
// place. A non-instrumented opcode is left alone.
//
// CPython: Python/instrumentation.c:676 de_instrument
func deInstrument(code *objects.Code, data *CoMonitoringData, i int) {
	ref, op := resolveOpcodeRef(code, data, i)
	if int(op) >= len(deinstrument) {
		return
	}
	deinstrumented := deinstrument[op]
	if deinstrumented == 0 {
		return
	}
	ref.set(deinstrumented)
	if specialize.CacheCount(deinstrumented) > 0 && code.Quickened {
		specialize.StoreCounter(code.Code, i, specialize.AdaptiveCounterWarmup())
	}
}
