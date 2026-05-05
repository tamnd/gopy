// Exception-table parity panel. Builds a curated set of instruction
// sequences with explicit handler ranges, runs them through the real
// compile.AssembleExceptionTable writer, then walks every byte offset
// in the resulting blob to assert findExcHandler returns the same
// (start, end, target, depth, lasti) tuple the writer encoded. Drift
// between writer (compile/assemble_exceptions.go) and reader
// (vm/exctable.go) shows up as a tuple mismatch on any fixture.
//
// CPython: Objects/exception_handling_notes.txt format spec, plus the
// reader at Python/ceval.c get_exception_handler.

package vm

import (
	"testing"

	"github.com/tamnd/gopy/compile"
)

// excFixture describes one parity case. Instrs is the post-flowgraph
// instruction list; Handler.Label fields point at indices into Instrs.
// Want pins the (start, end, target, depth, lasti) tuple the writer
// must produce for each declared handler range — written as an
// excEntry list ordered by start offset.
type excFixture struct {
	name   string
	instrs []compile.Instr
	want   []excEntry
}

func mkInstr(op compile.Opcode, h compile.ExceptHandlerInfo) compile.Instr {
	return compile.Instr{Op: op, Handler: h}
}

func excParitySequences() []excFixture {
	noHandler := compile.ExceptHandlerInfo{Label: -1}
	// Each instruction occupies two bytes (one code unit, no inline
	// caches on the picked opcodes), so byte offset = 2 * index.
	return []excFixture{
		{
			name: "single_handler_run",
			// idx: 0 NOP, 1 NOP, 2 NOP (protected), 3 NOP (protected),
			//      4 NOP (handler target).
			instrs: []compile.Instr{
				mkInstr(compile.NOP, noHandler),
				mkInstr(compile.NOP, noHandler),
				mkInstr(compile.NOP, compile.ExceptHandlerInfo{Label: 4, StartDepth: 1, PreserveLasti: 0}),
				mkInstr(compile.NOP, compile.ExceptHandlerInfo{Label: 4, StartDepth: 1, PreserveLasti: 0}),
				mkInstr(compile.NOP, noHandler),
			},
			want: []excEntry{
				{start: 4, end: 8, target: 8, depth: 0, preserveLasti: false},
			},
		},
		{
			name: "two_disjoint_handlers",
			instrs: []compile.Instr{
				mkInstr(compile.NOP, compile.ExceptHandlerInfo{Label: 3, StartDepth: 2}),
				mkInstr(compile.NOP, noHandler),
				mkInstr(compile.NOP, compile.ExceptHandlerInfo{Label: 5, StartDepth: 2}),
				mkInstr(compile.NOP, noHandler),
				mkInstr(compile.NOP, noHandler),
				mkInstr(compile.NOP, noHandler),
			},
			want: []excEntry{
				{start: 0, end: 2, target: 6, depth: 1, preserveLasti: false},
				{start: 4, end: 6, target: 10, depth: 1, preserveLasti: false},
			},
		},
		{
			name: "preserve_lasti_bit",
			instrs: []compile.Instr{
				mkInstr(compile.NOP, compile.ExceptHandlerInfo{Label: 2, StartDepth: 3, PreserveLasti: 1}),
				mkInstr(compile.NOP, compile.ExceptHandlerInfo{Label: 2, StartDepth: 3, PreserveLasti: 1}),
				mkInstr(compile.NOP, noHandler),
			},
			// depth = StartDepth - 1 - PreserveLasti = 3 - 1 - 1 = 1.
			want: []excEntry{
				{start: 0, end: 4, target: 4, depth: 1, preserveLasti: true},
			},
		},
		{
			name: "large_offsets",
			instrs: largeOffsetSequence(),
			want: []excEntry{
				{start: 200, end: 400, target: 400, depth: 0, preserveLasti: false},
			},
		},
	}
}

// largeOffsetSequence builds 250 NOPs with the middle 100 protected by
// a handler at index 200, forcing multi-byte varints in the writer.
func largeOffsetSequence() []compile.Instr {
	noHandler := compile.ExceptHandlerInfo{Label: -1}
	in := make([]compile.Instr, 250)
	for i := range in {
		in[i] = mkInstr(compile.NOP, noHandler)
	}
	for i := 100; i < 200; i++ {
		in[i].Handler = compile.ExceptHandlerInfo{Label: 200, StartDepth: 1}
	}
	return in
}

func TestExceptionTableParityAcrossFixtures(t *testing.T) {
	for _, fix := range excParitySequences() {
		fix := fix
		t.Run(fix.name, func(t *testing.T) {
			seq := &compile.Sequence{Instrs: fix.instrs}
			tab := compile.AssembleExceptionTable(seq)
			if len(tab) == 0 {
				t.Fatal("AssembleExceptionTable returned empty bytes; fixture must declare at least one handler")
			}
			// 1. Every PC inside a declared range must hit the matching tuple.
			covered := map[int]excEntry{}
			for _, want := range fix.want {
				for pc := want.start; pc < want.end; pc++ {
					got, ok := findExcHandler(tab, pc)
					if !ok {
						t.Errorf("pc=%d: expected hit on %+v, got miss", pc, want)
						continue
					}
					if got != want {
						t.Errorf("pc=%d: got %+v, want %+v", pc, got, want)
					}
					covered[pc] = want
				}
			}
			// 2. Probe every byte offset across the sequence; PCs not
			// in any declared range must miss.
			totalBytes := len(fix.instrs) * 2
			for pc := 0; pc < totalBytes+4; pc++ {
				_, hit := findExcHandler(tab, pc)
				_, expected := covered[pc]
				if hit != expected {
					t.Errorf("pc=%d: hit=%v, expected=%v", pc, hit, expected)
				}
			}
			// 3. Walking the table directly with the same varint
			// primitive must return the declared list in source order.
			gotEntries := walkExcEntries(tab)
			if len(gotEntries) != len(fix.want) {
				t.Fatalf("walk returned %d entries, want %d", len(gotEntries), len(fix.want))
			}
			for i, want := range fix.want {
				if gotEntries[i] != want {
					t.Errorf("entry[%d]: got %+v, want %+v", i, gotEntries[i], want)
				}
			}
		})
	}
}

// walkExcEntries yields every record in tab in source order, using the
// same varint primitive findExcHandler runs against. The parity test
// uses this to enumerate the writer's output and probe each range.
func walkExcEntries(tab []byte) []excEntry {
	var out []excEntry
	pos := 0
	for pos < len(tab) {
		if tab[pos]&excEntryStartBit == 0 {
			pos++
			continue
		}
		start, n, ok := readExcVarint(tab, pos)
		if !ok {
			return out
		}
		pos += n
		size, n, ok := readExcVarint(tab, pos)
		if !ok {
			return out
		}
		pos += n
		target, n, ok := readExcVarint(tab, pos)
		if !ok {
			return out
		}
		pos += n
		depthLasti, n, ok := readExcVarint(tab, pos)
		if !ok {
			return out
		}
		pos += n
		out = append(out, excEntry{
			start:         start,
			end:           start + size,
			target:        target,
			depth:         depthLasti >> 1,
			preserveLasti: depthLasti&1 != 0,
		})
	}
	return out
}
