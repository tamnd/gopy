// CPython: Objects/codeobject.c lineiter_next and friends.
// Decoders for the PEP 626 linetable and the matching positions
// table. The encoded form is a sequence of variable-length entries,
// each describing one (start, end, line, column) span in the
// bytecode. The decoders return one entry per yield.

package objects

// LineEntry is one decoded linetable record. Start and End are
// bytecode byte offsets; Line is the source line (1-based) or -1
// when the span has no associated source line (e.g. compiler
// glue).
//
// CPython: Objects/codeobject.c:1156 line_iter_t
type LineEntry struct {
	Start int
	End   int
	Line  int
}

// PositionEntry adds the column span; for traceback rendering and
// PEP 657 fine-grained error highlighting.
//
// CPython: Objects/codeobject.c:1170 line_iter_t (positions form)
type PositionEntry struct {
	Start     int
	End       int
	Line      int
	EndLine   int
	Column    int
	EndColumn int
}

// CoLines decodes the linetable into one LineEntry per span. The
// implementation here is a simple two-stream reader: each entry
// is a pair (length-byte, line-delta). A length of 0xff signals
// "no source mapping" and returns Line == -1.
//
// CPython: Objects/codeobject.c:1303 _PyCode_NewLineEntry
func CoLines(c *Code) []LineEntry {
	var entries []LineEntry
	if c == nil || len(c.Linetable) == 0 {
		return nil
	}
	pos := 0
	line := c.Firstlineno
	for i := 0; i+1 < len(c.Linetable); i += 2 {
		length := int(c.Linetable[i])
		delta := int(int8(c.Linetable[i+1]))
		next := pos + length
		if length == 0xff {
			entries = append(entries, LineEntry{Start: pos, End: next, Line: -1})
		} else {
			line += delta
			entries = append(entries, LineEntry{Start: pos, End: next, Line: line})
		}
		pos = next
	}
	return entries
}

// CoPositions yields the same spans as CoLines plus column info.
// The placeholder layout pairs each linetable entry with two
// extra bytes of (column, end-column); a real CPython linetable
// uses a denser encoding which the bytecode emitter will switch
// to once the compiler block lands.
//
// CPython: Objects/codeobject.c:1341 _PyCode_GetPositions
func CoPositions(c *Code) []PositionEntry {
	if c == nil {
		return nil
	}
	lines := CoLines(c)
	out := make([]PositionEntry, len(lines))
	for i, l := range lines {
		out[i] = PositionEntry{
			Start:     l.Start,
			End:       l.End,
			Line:      l.Line,
			EndLine:   l.Line,
			Column:    -1,
			EndColumn: -1,
		}
	}
	return out
}
