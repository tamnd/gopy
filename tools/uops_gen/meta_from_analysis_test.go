// Cross-checks the analyzer-driven UopMeta builder against the pycore_uop_metadata.h header that ships with CPython.

package main

import (
	"os"
	"sort"
	"strings"
	"testing"
)

func TestBuildUopMetaFromAnalysisMatchesHeader(t *testing.T) {
	src, err := readBytecodesSection("/Users/apple/cpython-314/Python/bytecodes.c")
	if err != nil {
		t.Skipf("bytecodes.c unavailable: %v", err)
	}
	p, err := NewParser(src, "bytecodes.c")
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	var forest []Node
	for {
		n, err := p.Definition()
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if n == nil {
			break
		}
		forest = append(forest, n)
	}
	analysis, err := AnalyzeForest(forest)
	if err != nil {
		t.Fatalf("AnalyzeForest: %v", err)
	}
	got, err := BuildUopMetaFromAnalysis(analysis)
	if err != nil {
		t.Fatalf("BuildUopMetaFromAnalysis: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("BuildUopMetaFromAnalysis returned no entries")
	}

	// Spot-check representative uops.
	for _, name := range []string{"LOAD_FAST", "RESUME_CHECK", "CHECK_VALIDITY", "BINARY_OP_ADD_INT"} {
		m, ok := got[name]
		if !ok {
			t.Errorf("missing meta for %q", name)
			continue
		}
		if !m.HasPopped {
			t.Errorf("%q: HasPopped=false, expected viable uop with popped count", name)
		}
	}

	headerPath := "/Users/apple/cpython-314/Include/internal/pycore_uop_metadata.h"
	hdr, err := os.ReadFile(headerPath)
	if err != nil {
		t.Skipf("header unavailable: %v", err)
	}
	want, err := ParseUopMetadata(string(hdr))
	if err != nil {
		t.Fatalf("ParseUopMetadata: %v", err)
	}

	flagSetEq := func(a, b []string) bool {
		if len(a) != len(b) {
			return false
		}
		ax := append([]string(nil), a...)
		bx := append([]string(nil), b...)
		sort.Strings(ax)
		sort.Strings(bx)
		for i := range ax {
			if ax[i] != bx[i] {
				return false
			}
		}
		return true
	}

	missing, mismatch := 0, 0
	deltas := []string{}
	for name, w := range want {
		g, ok := got[name]
		if !ok {
			missing++
			continue
		}
		if !flagSetEq(g.Flags, w.Flags) {
			mismatch++
			if len(deltas) < 5 {
				deltas = append(deltas,
					name+": flags analysis="+strings.Join(g.Flags, "|")+" header="+strings.Join(w.Flags, "|"))
			}
		}
		if g.Replication != w.Replication {
			mismatch++
			if len(deltas) < 5 {
				deltas = append(deltas, name+": replication mismatch")
			}
		}
		if g.HasPopped && w.HasPopped {
			gp := strings.ReplaceAll(g.Popped, " ", "")
			wp := strings.ReplaceAll(w.Popped, " ", "")
			if gp != wp {
				mismatch++
				if len(deltas) < 5 {
					deltas = append(deltas, name+": popped analysis="+g.Popped+" header="+w.Popped)
				}
			}
		}
	}
	for _, d := range deltas {
		t.Logf("delta: %s", d)
	}
	t.Logf("meta: analysis=%d header=%d missing=%d mismatch=%d",
		len(got), len(want), missing, mismatch)
	if missing > 0 || mismatch > 0 {
		t.Fatalf("analyzer meta is not a superset of header meta")
	}
}
