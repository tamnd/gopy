// Location-table parity panel. Builds curated instruction sequences
// with explicit ast.Pos values per instruction, runs them through the
// real compile.AssembleLineTable writer, then walks every byte offset
// in the resulting blob to assert objects.CoPositions returns the
// same (line, endLine, column, endColumn) tuple the writer encoded.
// Drift between writer (compile/assemble_locations.go) and reader
// (objects/code_tables.go) shows up as a tuple mismatch on any
// fixture.
//
// CPython: Objects/codeobject.c:1199 advance_with_locations + the
// writer in Python/assemble.c:285 write_location_info_entry.

package vm

import (
	"testing"

	"github.com/tamnd/gopy/ast"
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

// posFixture describes one parity case. Each instruction's Loc plus
// the Sequence-wide firstLineno fully determines the encoded table.
// want pins the (start, end, line, endLine, column, endColumn) tuple
// for each coalesced run, in source order.
type posFixture struct {
	name        string
	firstLineno int
	instrs      []compile.Instr
	want        []objects.PositionEntry
}

func mkPosInstr(loc ast.Pos) compile.Instr {
	return compile.Instr{Op: compile.NOP, Loc: loc}
}

func posParityFixtures() []posFixture {
	return []posFixture{
		{
			name:        "single_short_form",
			firstLineno: 5,
			instrs: []compile.Instr{
				mkPosInstr(ast.Pos{Lineno: 5, EndLineno: 5, ColOffset: 0, EndColOffset: 4}),
				mkPosInstr(ast.Pos{Lineno: 5, EndLineno: 5, ColOffset: 0, EndColOffset: 4}),
				mkPosInstr(ast.Pos{Lineno: 5, EndLineno: 5, ColOffset: 0, EndColOffset: 4}),
			},
			want: []objects.PositionEntry{
				{Start: 0, End: 6, Line: 5, EndLine: 5, Column: 0, EndColumn: 4},
			},
		},
		{
			name:        "oneline_with_line_delta",
			firstLineno: 1,
			instrs: []compile.Instr{
				mkPosInstr(ast.Pos{Lineno: 1, EndLineno: 1, ColOffset: 0, EndColOffset: 4}),
				mkPosInstr(ast.Pos{Lineno: 2, EndLineno: 2, ColOffset: 90, EndColOffset: 100}),
			},
			want: []objects.PositionEntry{
				{Start: 0, End: 2, Line: 1, EndLine: 1, Column: 0, EndColumn: 4},
				{Start: 2, End: 4, Line: 2, EndLine: 2, Column: 90, EndColumn: 100},
			},
		},
		{
			name:        "long_form_multiline",
			firstLineno: 10,
			instrs: []compile.Instr{
				mkPosInstr(ast.Pos{Lineno: 10, EndLineno: 12, ColOffset: 0, EndColOffset: 8}),
				mkPosInstr(ast.Pos{Lineno: 10, EndLineno: 12, ColOffset: 0, EndColOffset: 8}),
			},
			want: []objects.PositionEntry{
				{Start: 0, End: 4, Line: 10, EndLine: 12, Column: 0, EndColumn: 8},
			},
		},
		{
			name:        "no_column_form",
			firstLineno: 1,
			instrs: []compile.Instr{
				mkPosInstr(ast.Pos{Lineno: 3, EndLineno: 3, ColOffset: -1, EndColOffset: -1}),
			},
			want: []objects.PositionEntry{
				{Start: 0, End: 2, Line: 3, EndLine: 3, Column: -1, EndColumn: -1},
			},
		},
		{
			name:        "none_form",
			firstLineno: 1,
			instrs: []compile.Instr{
				mkPosInstr(ast.Pos{Lineno: -1, EndLineno: -1, ColOffset: -1, EndColOffset: -1}),
			},
			want: []objects.PositionEntry{
				{Start: 0, End: 2, Line: -1, EndLine: -1, Column: -1, EndColumn: -1},
			},
		},
		{
			name:        "split_long_span",
			firstLineno: 1,
			instrs:      longSameLocSequence(20, ast.Pos{Lineno: 1, EndLineno: 1, ColOffset: 0, EndColOffset: 4}),
			want: []objects.PositionEntry{
				{Start: 0, End: 16, Line: 1, EndLine: 1, Column: 0, EndColumn: 4},
				{Start: 16, End: 32, Line: 1, EndLine: 1, Column: 0, EndColumn: 4},
				{Start: 32, End: 40, Line: 1, EndLine: 1, Column: 0, EndColumn: 4},
			},
		},
		{
			name:        "mixed_runs",
			firstLineno: 7,
			instrs: []compile.Instr{
				mkPosInstr(ast.Pos{Lineno: 7, EndLineno: 7, ColOffset: 0, EndColOffset: 4}),
				mkPosInstr(ast.Pos{Lineno: 7, EndLineno: 7, ColOffset: 0, EndColOffset: 4}),
				mkPosInstr(ast.Pos{Lineno: 8, EndLineno: 9, ColOffset: 100, EndColOffset: 200}),
				mkPosInstr(ast.Pos{Lineno: 10, EndLineno: 10, ColOffset: 90, EndColOffset: 100}),
			},
			want: []objects.PositionEntry{
				{Start: 0, End: 4, Line: 7, EndLine: 7, Column: 0, EndColumn: 4},
				{Start: 4, End: 6, Line: 8, EndLine: 9, Column: 100, EndColumn: 200},
				{Start: 6, End: 8, Line: 10, EndLine: 10, Column: 90, EndColumn: 100},
			},
		},
	}
}

func longSameLocSequence(n int, loc ast.Pos) []compile.Instr {
	out := make([]compile.Instr, n)
	for i := range out {
		out[i] = mkPosInstr(loc)
	}
	return out
}

func TestLineTableParityAcrossFixtures(t *testing.T) {
	for _, fix := range posParityFixtures() {
		t.Run(fix.name, func(t *testing.T) {
			seq := &compile.Sequence{Instrs: fix.instrs}
			tab := compile.AssembleLineTable(seq, fix.firstLineno)
			if len(tab) == 0 {
				t.Fatal("AssembleLineTable returned empty bytes; fixture must declare at least one location")
			}
			co := &objects.Code{Linetable: tab, Firstlineno: fix.firstLineno}

			gotEntries := objects.CoPositions(co)
			if len(gotEntries) != len(fix.want) {
				t.Fatalf("walk returned %d entries, want %d (got=%+v)", len(gotEntries), len(fix.want), gotEntries)
			}
			for i, want := range fix.want {
				if gotEntries[i] != want {
					t.Errorf("entry[%d]: got %+v, want %+v", i, gotEntries[i], want)
				}
			}

			// Probe every byte offset across the sequence; each PC
			// inside a declared range must hit the matching tuple via
			// CoAddr2Location.
			totalBytes := len(fix.instrs) * 2
			for pc := 0; pc < totalBytes; pc++ {
				want, ok := findExpectedPos(fix.want, pc)
				if !ok {
					t.Fatalf("fixture coverage gap at pc=%d", pc)
				}
				got, found := objects.CoAddr2Location(co, pc)
				if !found {
					t.Errorf("pc=%d: expected hit on %+v, got miss", pc, want)
					continue
				}
				if got != want {
					t.Errorf("pc=%d: got %+v, want %+v", pc, got, want)
				}
			}
		})
	}
}

func findExpectedPos(want []objects.PositionEntry, pc int) (objects.PositionEntry, bool) {
	for _, w := range want {
		if pc >= w.Start && pc < w.End {
			return w, true
		}
	}
	return objects.PositionEntry{}, false
}
