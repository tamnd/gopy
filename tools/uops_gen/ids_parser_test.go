package main

import "testing"

func TestParseUopIDs_NumericAndAlias(t *testing.T) {
	src := `// header preamble
#define _EXIT_TRACE 300
#define _NOP NOP
#define _LOAD_FAST_0 443
#define MAX_UOP_ID 516
`
	ids, maxID, err := ParseUopIDs(src)
	if err != nil {
		t.Fatal(err)
	}
	if maxID != 516 {
		t.Fatalf("maxID = %d, want 516", maxID)
	}
	if len(ids) != 3 {
		t.Fatalf("len = %d, want 3", len(ids))
	}
	if ids[0].Name != "EXIT_TRACE" || ids[0].Value != 300 || ids[0].IsAlias {
		t.Errorf("ids[0] = %+v", ids[0])
	}
	if ids[1].Name != "NOP" || !ids[1].IsAlias || ids[1].Alias != "NOP" {
		t.Errorf("ids[1] = %+v", ids[1])
	}
	if ids[2].Name != "LOAD_FAST_0" || ids[2].Value != 443 {
		t.Errorf("ids[2] = %+v", ids[2])
	}
}

func TestParseUopIDs_MissingMaxIsError(t *testing.T) {
	if _, _, err := ParseUopIDs("#define _NOP 0\n"); err == nil {
		t.Error("want error, got nil")
	}
}
