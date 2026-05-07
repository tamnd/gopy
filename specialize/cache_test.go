package specialize

import (
	"testing"
	"unsafe"
)

// Pin every inline cache width against the CPython numbers in
// Include/internal/pycore_code.h. Drift here means a generated
// dispatch arm walks past its cache cells into the next opcode,
// which corrupts the bytecode stream. The check is cheap; run it.
func TestInlineCacheWidths(t *testing.T) {
	cases := []struct {
		name string
		got  int
		want int
	}{
		{"LOAD_GLOBAL", InlineCacheEntriesLoadGlobal, 4},
		{"BINARY_OP", InlineCacheEntriesBinaryOp, 5},
		{"UNPACK_SEQUENCE", InlineCacheEntriesUnpackSequence, 1},
		{"COMPARE_OP", InlineCacheEntriesCompareOp, 1},
		{"LOAD_SUPER_ATTR", InlineCacheEntriesLoadSuperAttr, 1},
		{"LOAD_ATTR", InlineCacheEntriesLoadAttr, 9},
		{"STORE_ATTR", InlineCacheEntriesStoreAttr, 4},
		{"CALL", InlineCacheEntriesCall, 3},
		{"CALL_KW", InlineCacheEntriesCallKw, 3},
		{"STORE_SUBSCR", InlineCacheEntriesStoreSubscr, 1},
		{"FOR_ITER", InlineCacheEntriesForIter, 1},
		{"SEND", InlineCacheEntriesSend, 1},
		{"TO_BOOL", InlineCacheEntriesToBool, 3},
		{"CONTAINS_OP", InlineCacheEntriesContainsOp, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.got != c.want {
				t.Fatalf("INLINE_CACHE_ENTRIES_%s: got %d codeunits, want %d",
					c.name, c.got, c.want)
			}
		})
	}
}

func TestBackoffCounterFitsInOneCodeUnit(t *testing.T) {
	const want = 2
	got := unsafe.Sizeof(BackoffCounter{})
	if int(got) != want {
		t.Fatalf("BackoffCounter width: got %d want %d", got, want)
	}
}
