// PEP 657 exception table writer. Each entry encodes start, size,
// target, and depth_lasti as 6-bit varints with a continuation bit
// (0x40); the very first byte of an entry sets bit 7 so a reader can
// resync on entry boundaries.
//
// CPython: Python/assemble.c:L89 exception-table panel

package compile

const (
	continuationBit byte = 0x40
	entryStartBit   byte = 0x80
)

// writeExcItem writes one varint-encoded value into the exception
// table. The msb argument is OR'd into the first emitted byte (set
// only on the very first byte of each table entry).
//
// CPython: Python/assemble.c:L105 assemble_emit_exception_table_item
func writeExcItem(buf []byte, value int, msb byte) []byte {
	v := uint32(value)
	if v >= 1<<24 {
		buf = append(buf, byte(v>>24)|continuationBit|msb)
		msb = 0
	}
	if v >= 1<<18 {
		buf = append(buf, byte((v>>18)&0x3f)|continuationBit|msb)
		msb = 0
	}
	if v >= 1<<12 {
		buf = append(buf, byte((v>>12)&0x3f)|continuationBit|msb)
		msb = 0
	}
	if v >= 1<<6 {
		buf = append(buf, byte((v>>6)&0x3f)|continuationBit|msb)
		msb = 0
	}
	return append(buf, byte(v&0x3f)|msb)
}

// writeExcEntry emits one (start, end, target, depth_lasti) record.
// CPython packs depth into bits 1..n with the preserve-lasti flag in
// bit 0.
//
// CPython: Python/assemble.c:L132 assemble_emit_exception_table_entry
func writeExcEntry(buf []byte, start, end, target int, h *ExceptHandlerInfo) []byte {
	depth := h.StartDepth - 1
	if h.PreserveLasti > 0 {
		depth--
	}
	if depth < 0 {
		depth = 0
	}
	depthLasti := (depth << 1) | h.PreserveLasti
	buf = writeExcItem(buf, start, entryStartBit)
	buf = writeExcItem(buf, end-start, 0)
	buf = writeExcItem(buf, target, 0)
	buf = writeExcItem(buf, depthLasti, 0)
	return buf
}

// AssembleExceptionTable is the exported wrapper for assembleExceptionTable.
// External tests use it to build exception-table bytes from a curated
// Sequence and round-trip them through the vm reader without going
// through the full compile pipeline.
func AssembleExceptionTable(seq *Sequence) []byte { return assembleExceptionTable(seq) }

// assembleExceptionTable walks the post-flowgraph instruction stream,
// groups runs that share a handler, and emits one varint record per
// run. Offsets are in code units (one codeunit per opcode plus its
// EXTENDED_ARG prefixes and inline caches), matching CPython's
// exception-table format: dis.py and the unwinder both read these
// values as instruction offsets and scale by the codeunit size, so
// the serialized table must NOT pre-multiply to bytes.
//
// CPython: Python/assemble.c:L157 assemble_exception_table
func assembleExceptionTable(seq *Sequence) []byte {
	if seq == nil || len(seq.Instrs) == 0 {
		return nil
	}
	offsets := make([]int, len(seq.Instrs))
	off := 0
	for i := range seq.Instrs {
		offsets[i] = off
		off += instrSize(&seq.Instrs[i])
	}
	totalBytes := off

	var buf []byte
	handler := ExceptHandlerInfo{Label: -1, StartDepth: -1, PreserveLasti: -1}
	start := -1
	for i := range seq.Instrs {
		ins := &seq.Instrs[i]
		if ins.Handler.Label != handler.Label {
			if handler.Label >= 0 && handler.Label < len(offsets) {
				buf = writeExcEntry(buf, start, offsets[i], offsets[handler.Label], &handler)
			}
			start = offsets[i]
			handler = ins.Handler
		}
	}
	if handler.Label >= 0 && handler.Label < len(offsets) {
		buf = writeExcEntry(buf, start, totalBytes, offsets[handler.Label], &handler)
	}
	return buf
}
