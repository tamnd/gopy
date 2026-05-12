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

	_ "github.com/tamnd/gopy/stdlibinit"
)

// runInheritScript wires the gopy runtime, sets __name__ so class
// bodies resolve, and runs src. The captured output is the test's
// inheritance assertion: the gate compares it against want.
func runInheritScript(t *testing.T, src, want string) {
	t.Helper()
	var stdout bytes.Buffer
	g, err := builtins.Init(&stdout)
	if err != nil {
		t.Fatalf("builtins.Init: %v", err)
	}
	if err := g.SetItem(objects.NewStr("__name__"), objects.NewStr("__main__")); err != nil {
		t.Fatalf("set __name__: %v", err)
	}
	ts := state.NewThread()
	if _, err := pythonrun.RunString(ts, src, "<inherit-slots>", parser.ModeFile, g, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.TrimRight(stdout.String(), "\n")
	if got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

// gate 7.1: list subclass inherits TpNew + Sequence slots from list,
// so the subclass behaves like a list for len(), indexing, and reversed().
//
// CPython: Objects/typeobject.c:7521 inherit_slots
func TestInheritSlotsListSubclass(t *testing.T) {
	runInheritScript(t, `
class L(list):
    pass
l = L([1, 2, 3])
print(len(l), l[1], list(reversed(l)))
`, "3 2 [3, 2, 1]")
}

// gate 7.2: dict subclass keeps the Mapping slot table so subscript
// and len() route through dict.
func TestInheritSlotsDictSubclass(t *testing.T) {
	runInheritScript(t, `
class D(dict):
    pass
d = D({"a": 1})
print(d["a"], len(d), "a" in d)
`, "1 1 True")
}

// gate 7.3: int subclass inherits the Number slot table so + and *
// keep working on instances of the subclass.
func TestInheritSlotsIntSubclass(t *testing.T) {
	runInheritScript(t, `
class I(int):
    pass
print(I(3) + I(4), I(5) * I(2))
`, "7 10")
}
