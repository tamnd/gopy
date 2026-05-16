package main

import "testing"

func TestParseUopMetadata_FlagsRepPopped(t *testing.T) {
	src := `const uint16_t _PyUop_Flags[MAX_UOP_ID+1] = {
    [_NOP] = HAS_PURE_FLAG,
    [_LOAD_FAST] = HAS_ARG_FLAG | HAS_LOCAL_FLAG | HAS_PURE_FLAG,
    [_POP_TOP] = 0,
};

const uint8_t _PyUop_Replication[MAX_UOP_ID+1] = {
    [_LOAD_FAST] = 8,
};

int _PyUop_num_popped(int opcode, int oparg)
{
    switch(opcode) {
        case _NOP:
            return 0;
        case _LOAD_FAST:
            return 0;
        case _POP_TOP:
            return 1;
        case _BUILD_TUPLE:
            return oparg;
    }
}
`
	meta, err := ParseUopMetadata(src)
	if err != nil {
		t.Fatal(err)
	}

	pop := meta["POP_TOP"]
	if pop == nil || pop.Popped != "1" || !pop.HasPopped {
		t.Errorf("POP_TOP = %+v", pop)
	}
	if got := meta["LOAD_FAST"]; got == nil || got.Replication != 8 {
		t.Errorf("LOAD_FAST replication = %+v", got)
	}
	loadFastFlags := meta["LOAD_FAST"].Flags
	want := []string{"ARG", "LOCAL", "PURE"}
	if len(loadFastFlags) != len(want) {
		t.Fatalf("LOAD_FAST flags = %v, want %v", loadFastFlags, want)
	}
	for i, w := range want {
		if loadFastFlags[i] != w {
			t.Errorf("LOAD_FAST flags[%d] = %s, want %s", i, loadFastFlags[i], w)
		}
	}

	// oparg-dependent return is preserved verbatim so the emitter can
	// fall back to -1.
	bt := meta["BUILD_TUPLE"]
	if bt == nil || bt.Popped != "oparg" {
		t.Errorf("BUILD_TUPLE = %+v", bt)
	}
}
