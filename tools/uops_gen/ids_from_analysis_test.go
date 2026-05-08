// Cross-checks the analyzer-driven UopID builder against the pycore_uop_ids.h header that ships with CPython.

package main

import (
	"os"
	"testing"
)

func TestBuildUopIDsFromAnalysisMatchesHeader(t *testing.T) {
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

	got, maxID, err := BuildUopIDsFromAnalysis(analysis, false)
	if err != nil {
		t.Fatalf("BuildUopIDsFromAnalysis: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("BuildUopIDsFromAnalysis returned empty list")
	}

	gotMap := map[string]UopID{}
	for _, e := range got {
		gotMap[e.Name] = e
	}

	// Spot-check the predefined heads and a representative numeric uop.
	if e, ok := gotMap["EXIT_TRACE"]; !ok || e.Value != 300 || e.IsAlias {
		t.Errorf("EXIT_TRACE: got %+v want {Value:300}", e)
	}
	if e, ok := gotMap["SET_IP"]; !ok || e.Value != 301 || e.IsAlias {
		t.Errorf("SET_IP: got %+v want {Value:301}", e)
	}
	for _, name := range []string{"LOAD_FAST", "RESUME_CHECK", "CHECK_VALIDITY", "BINARY_OP_ADD_INT"} {
		if _, ok := gotMap[name]; !ok {
			t.Errorf("missing uop %q", name)
		}
	}

	headerPath := "/Users/apple/cpython-314/Include/internal/pycore_uop_ids.h"
	hdr, err := os.ReadFile(headerPath)
	if err != nil {
		t.Skipf("header unavailable: %v", err)
	}
	want, wantMax, err := ParseUopIDs(string(hdr))
	if err != nil {
		t.Fatalf("ParseUopIDs: %v", err)
	}
	wantMap := map[string]UopID{}
	for _, e := range want {
		wantMap[e.Name] = e
	}

	if maxID != wantMax {
		t.Logf("maxID delta: analysis=%d header=%d", maxID, wantMax)
	}

	missing, extra, mismatch := 0, 0, 0
	for name, w := range wantMap {
		g, ok := gotMap[name]
		if !ok {
			missing++
			continue
		}
		if g.IsAlias != w.IsAlias || g.Value != w.Value || g.Alias != w.Alias {
			mismatch++
			if mismatch <= 5 {
				t.Logf("mismatch %s: analysis=%+v header=%+v", name, g, w)
			}
		}
	}
	for name := range gotMap {
		if _, ok := wantMap[name]; !ok {
			extra++
		}
	}
	t.Logf("ids: analysis=%d header=%d missing=%d extra=%d mismatch=%d",
		len(gotMap), len(wantMap), missing, extra, mismatch)
	if missing > 0 || mismatch > 0 {
		t.Fatalf("analyzer output is not a superset of the header")
	}
}
