// Line-event instrumentation. Most of the structure mirrors regular
// per-(event, offset) instrumentation, but line events get their own
// auxiliary table (LineInstrumentationData) because the bytecode at a
// monitored line is overwritten by INSTRUMENTED_LINE: the original
// opcode and the delta-from-firstlineno have to live somewhere else
// so the dispatcher can recover both when the marker fires.
//
// CPython: Python/instrumentation.c:1287 _Py_Instrumentation_GetLine

package monitor

import (
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/specialize"
)

// NoLine is the sentinel stored in the line table when a codeunit
// has no source line. CPython: Python/instrumentation.c:294 NO_LINE.
const NoLine = -2

// computeLineDelta returns the delta from code's firstlineno that
// encodes line, or NoLine when line is negative (no line info).
//
// CPython: Python/instrumentation.c:302 compute_line_delta
func computeLineDelta(code *objects.Code, line int) int {
	if line < 0 {
		return NoLine
	}
	return line - code.Firstlineno
}

// computeLine inverts computeLineDelta. Returns -1 for NoLine.
//
// CPython: Python/instrumentation.c:314 compute_line
func computeLine(code *objects.Code, delta int) int {
	if delta == NoLine {
		return -1
	}
	return code.Firstlineno + delta
}

// pickBytesPerEntry chooses the smallest entry width that can hold
// every line delta in code. CPython picks 2..5 inclusive: one byte
// for the original opcode plus 1..4 bytes for the line-delta value.
//
// CPython: Python/instrumentation.c:1715 update_instrumentation_data
func pickBytesPerEntry(code *objects.Code) int {
	maxDelta := 1
	for _, le := range objects.CoLines(code) {
		if le.Line < 0 {
			continue
		}
		d := le.Line - code.Firstlineno
		if d > maxDelta {
			maxDelta = d
		}
	}
	switch {
	case maxDelta < 256+NoLine:
		return 2
	case maxDelta < (1<<16)+NoLine:
		return 3
	case maxDelta < (1<<24)+NoLine:
		return 4
	default:
		return 5
	}
}

// getOriginalOpcode reads the underlying opcode that INSTRUMENTED_LINE
// is hiding at codeunit i.
//
// CPython: Python/instrumentation.c:333 get_original_opcode
func getOriginalOpcode(line *LineInstrumentationData, i int) byte {
	return line.Data[i*int(line.BytesPerEntry)]
}

// setOriginalOpcode stores the original opcode at codeunit i so the
// dispatcher can recover it after firing the line event.
//
// CPython: Python/instrumentation.c:345 set_original_opcode
func setOriginalOpcode(line *LineInstrumentationData, i int, op byte) {
	line.Data[i*int(line.BytesPerEntry)] = op
}

// getLineDelta reads the variable-width delta-from-firstlineno stored
// after the original opcode. The stored value is delta-NoLine so 0
// means NoLine.
//
// CPython: Python/instrumentation.c:351 get_line_delta
func getLineDelta(line *LineInstrumentationData, i int) int {
	bpe := int(line.BytesPerEntry)
	base := i*bpe + 1
	value := uint32(line.Data[base])
	for idx := 2; idx < bpe; idx++ {
		value |= uint32(line.Data[base+idx-1]) << uint((idx-1)*8)
	}
	return int(value) + NoLine
}

// setLineDelta stores delta in the entry for codeunit i.
//
// CPython: Python/instrumentation.c:367 set_line_delta
func setLineDelta(line *LineInstrumentationData, i int, delta int) {
	bpe := int(line.BytesPerEntry)
	base := i*bpe + 1
	adjusted := uint32(delta - NoLine)
	line.Data[base] = byte(adjusted & 0xff)
	for idx := 2; idx < bpe; idx++ {
		adjusted >>= 8
		line.Data[base+idx-1] = byte(adjusted & 0xff)
	}
}

// LineForOffset returns the source line number that codeunit i in
// code maps to via PEP 626 location data, or -1 when the entry has no
// line information.
func LineForOffset(code *objects.Code, i int) int {
	byteOffset := i * 2
	for _, le := range objects.CoLines(code) {
		if byteOffset >= le.Start && byteOffset < le.End {
			return le.Line
		}
	}
	return -1
}

// initializeLines builds a LineInstrumentationData for code. Every
// codeunit that begins a new source line gets its base opcode stamped
// in; everything else gets 0 so the dispatcher knows it must not fire
// a line event for that codeunit.
//
// CPython: Python/instrumentation.c:1509 initialize_lines
func initializeLines(code *objects.Code, line *LineInstrumentationData) {
	codeLen := instructionCount(code)
	currentLine := -1
	for i := 0; i < codeLen; {
		base := GetBaseCodeUnit(code, i)
		line2 := LineForOffset(code, i)
		setLineDelta(line, i, computeLineDelta(code, line2))
		length := 1 + cacheCount(base, code)
		switch base {
		case compile.END_ASYNC_FOR, compile.END_FOR, compile.END_SEND,
			compile.RESUME, compile.POP_ITER:
			setOriginalOpcode(line, i, 0)
		default:
			if line2 != currentLine && line2 >= 0 {
				setOriginalOpcode(line, i, byte(base))
			} else {
				setOriginalOpcode(line, i, 0)
			}
			currentLine = line2
		}
		for j := 1; j < length; j++ {
			setOriginalOpcode(line, i+j, 0)
			setLineDelta(line, i+j, NoLine)
		}
		i += length
	}
}

// ensureLines lazily allocates the line-instrumentation table on data
// when a tool first registers for EventLine on code.
//
// CPython: Python/instrumentation.c:1701 update_instrumentation_data
func ensureLines(code *objects.Code, data *CoMonitoringData) {
	if data.Lines != nil {
		return
	}
	bpe := pickBytesPerEntry(code)
	codeLen := instructionCount(code)
	data.Lines = &LineInstrumentationData{
		BytesPerEntry: uint8(bpe),
		Data:          make([]byte, codeLen*bpe),
	}
	initializeLines(code, data.Lines)
}

// instrumentLine rewrites the opcode at codeunit i to INSTRUMENTED_LINE
// and remembers the original (deopted) opcode in the line table.
// Skips codeunits that already carry the marker.
//
// CPython: Python/instrumentation.c:786 instrument_line
func instrumentLine(code *objects.Code, data *CoMonitoringData, i int) {
	op := compile.Opcode(byteAt(code.Code, i))
	if op == compile.INSTRUMENTED_LINE {
		return
	}
	setOriginalOpcode(data.Lines, i, byte(specialize.Deopt(op)))
	code.Code[2*i] = byte(compile.INSTRUMENTED_LINE)
}

// deInstrumentLine restores the opcode that instrumentLine hid behind
// INSTRUMENTED_LINE at codeunit i. A no-op when the marker is gone.
//
// CPython: Python/instrumentation.c:707 de_instrument_line
func deInstrumentLine(code *objects.Code, data *CoMonitoringData, i int) {
	op := compile.Opcode(byteAt(code.Code, i))
	if op != compile.INSTRUMENTED_LINE {
		return
	}
	original := getOriginalOpcode(data.Lines, i)
	code.Code[2*i] = original
}

// addLineTools sets the line-tool bits at codeunit offset and stamps
// the bytecode with INSTRUMENTED_LINE. Lazily allocates LineTools
// when more than one tool subscribes.
//
// CPython: Python/instrumentation.c:909 add_line_tools
func addLineTools(code *objects.Code, data *CoMonitoringData, offset int, tools uint8) {
	if data.LineTools != nil {
		data.LineTools[offset] |= tools
	}
	instrumentLine(code, data, offset)
}

// removeLineTools clears the line-tool bits at codeunit offset and
// restores the original opcode when no tool remains.
//
// CPython: Python/instrumentation.c:863 remove_line_tools
func removeLineTools(code *objects.Code, data *CoMonitoringData, offset int, tools uint8) {
	shouldDeinstrument := false
	if data.LineTools != nil {
		data.LineTools[offset] &^= tools
		shouldDeinstrument = data.LineTools[offset] == 0
	} else {
		single := data.ActiveMonitors.Tools[EventLine]
		shouldDeinstrument = (single & tools) == single
	}
	if shouldDeinstrument {
		deInstrumentLine(code, data, offset)
	}
}

// initializeLineTools fills LineTools so every codeunit starts with
// the tool bitmask CPython would compute from the local-union.
//
// CPython: Python/instrumentation.c:1635 initialize_line_tools
func initializeLineTools(code *objects.Code, data *CoMonitoringData, all *LocalMonitors) {
	codeLen := instructionCount(code)
	tools := all.Tools[EventLine]
	for i := 0; i < codeLen; i++ {
		data.LineTools[i] = tools
	}
}

// LineDispatch carries the result of a fired line event back to the
// dispatcher. OriginalOpcode is the opcode the dispatcher should run
// in place of INSTRUMENTED_LINE.
type LineDispatch struct {
	OriginalOpcode compile.Opcode
	Line           int
}

// GetOriginalOpcode returns the original opcode stored at instr in
// data.Lines, or 0 if no data is present. Used by the dispatcher when
// it needs the opcode without firing any events (e.g. re-entrance guard).
//
// CPython: Python/instrumentation.c getOriginalOpcode (static inline)
func GetOriginalOpcode(data *CoMonitoringData, instr int) compile.Opcode {
	if data == nil || data.Lines == nil {
		return 0
	}
	return compile.Opcode(getOriginalOpcode(data.Lines, instr))
}

// CallInstrumentationLine fires LINE for the (code, instr) pair when
// the instr offset starts a new line and at least one tool subscribes,
// then returns the original opcode the dispatcher should execute in
// INSTRUMENTED_LINE's place.
//
// CPython: Python/instrumentation.c:1297 _Py_call_instrumentation_line
func CallInstrumentationLine(interp *InterpState, code *objects.Code, instr int, prevInstr int) (LineDispatch, error) {
	out := LineDispatch{OriginalOpcode: compile.NOP}
	data := CoMonitoring(code)
	if data == nil || data.Lines == nil {
		return out, nil
	}
	line := computeLine(code, getLineDelta(data.Lines, instr))
	out.Line = line

	if prevInstr >= 0 && prevInstr != instr {
		prevLine := computeLine(code, getLineDelta(data.Lines, prevInstr))
		if prevLine == line {
			prevOp := compile.Opcode(byteAt(code.Code, prevInstr))
			if prevOp != compile.RESUME && prevOp != compile.INSTRUMENTED_RESUME {
				out.OriginalOpcode = compile.Opcode(getOriginalOpcode(data.Lines, instr))
				return out, nil
			}
		}
	}

	tools := uint8(0)
	if data.LineTools != nil {
		tools = data.LineTools[instr]
	} else {
		tools = data.ActiveMonitors.Tools[EventLine]
	}
	if tools != 0 && interp != nil {
		state := MonState{Active: tools}
		if err := FireLine(interp, &state, code, int32(instr), line); err != nil {
			return out, err
		}
		// Reflect Disable returns back into per-codeunit tools.
		if data.LineTools != nil {
			data.LineTools[instr] = state.Active
		}
	}
	out.OriginalOpcode = compile.Opcode(getOriginalOpcode(data.Lines, instr))
	return out, nil
}
