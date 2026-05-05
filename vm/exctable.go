// PEP 657 exception-table reader. The writer in compile/assemble_exceptions.go
// emits one entry per handler region as four varints: start, size,
// target, depth_lasti. The first byte of each entry has bit 7
// (entryStartBit, 0x80) set; varints chain via the continuation bit
// (0x40). Both fields are byte offsets into the bytecode blob.
//
// CPython: Python/ceval.c:L1815 get_exception_handler
// CPython: Objects/exception_handling_notes.txt format spec

package vm

const (
	excContinuationBit byte = 0x40
	excEntryStartBit   byte = 0x80
)

// excEntry is one decoded record. depth and preserveLasti come from
// the encoded depthLasti = (depth << 1) | preserveLasti.
type excEntry struct {
	start, end, target int
	depth              int
	preserveLasti      bool
}

// readExcVarint decodes one varint starting at b[pos]. The first byte
// may carry the entry-start bit; we ignore it here (the caller has
// already consumed it for boundary detection). Returns the value and
// the number of bytes consumed.
//
// CPython: Python/ceval.c:L1798 parse_varint
func readExcVarint(b []byte, pos int) (val, n int, ok bool) {
	for {
		if pos+n >= len(b) {
			return 0, 0, false
		}
		x := b[pos+n]
		val = (val << 6) | int(x&0x3f)
		n++
		if x&excContinuationBit == 0 {
			return val, n, true
		}
	}
}

// findExcHandler walks tab looking for the first entry whose
// [start, start+size) range contains pc. Returns the matching entry
// and ok=true on hit; ok=false if no entry covers pc.
//
// pc is a byte offset into the bytecode blob.
//
// CPython: Python/ceval.c:L1815 get_exception_handler
func findExcHandler(tab []byte, pc int) (excEntry, bool) {
	pos := 0
	for pos < len(tab) {
		// Each entry starts at a byte with bit 7 set.
		if tab[pos]&excEntryStartBit == 0 {
			// Resync: skip a stray byte rather than panic. Should
			// not happen on well-formed tables.
			pos++
			continue
		}
		start, n, ok := readExcVarint(tab, pos)
		if !ok {
			return excEntry{}, false
		}
		pos += n

		size, n, ok := readExcVarint(tab, pos)
		if !ok {
			return excEntry{}, false
		}
		pos += n

		target, n, ok := readExcVarint(tab, pos)
		if !ok {
			return excEntry{}, false
		}
		pos += n

		depthLasti, n, ok := readExcVarint(tab, pos)
		if !ok {
			return excEntry{}, false
		}
		pos += n

		end := start + size
		if pc >= start && pc < end {
			return excEntry{
				start:         start,
				end:           end,
				target:        target,
				depth:         depthLasti >> 1,
				preserveLasti: depthLasti&1 != 0,
			}, true
		}
	}
	return excEntry{}, false
}
