package stdlibinit_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tamnd/gopy/builtins"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/parser"
	"github.com/tamnd/gopy/pythonrun"
	"github.com/tamnd/gopy/state"
)

// A class that only defines __getitem__ + __len__ but no __iter__
// must still be iterable via PyObject_GetIter's sequence-protocol
// fallback. This was the blocker for re's SubPattern (an internal
// re/_parser.py class that exposes __getitem__ but no __iter__) and
// would have been masked if FOR_ITER stayed coupled to tp_iter only.
//
// CPython: Objects/abstract.c:2786 PyObject_GetIter (sq_item fallback)
func TestForLoopUsesSequenceProtocolFallback(t *testing.T) {
	var stdout bytes.Buffer
	g, err := builtins.Init(&stdout)
	if err != nil {
		t.Fatalf("builtins.Init: %v", err)
	}
	if err := g.SetItem(objects.NewStr("__name__"), objects.NewStr("__main__")); err != nil {
		t.Fatalf("set __name__: %v", err)
	}
	ts := state.NewThread()
	src := `class SeqOnly:
    def __init__(self, data):
        self._data = data
    def __len__(self):
        return len(self._data)
    def __getitem__(self, i):
        return self._data[i]

s = SeqOnly([10, 20, 30])
for x in s:
    print(x)
`
	if _, err := pythonrun.RunString(ts, src, "<seq-iter>", parser.ModeFile, g, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.TrimRight(stdout.String(), "\n")
	want := "10\n20\n30"
	if got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}
