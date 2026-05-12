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

// runSlotLookupScript boots gopy, sets __name__ so class bodies run,
// and runs src. The gate is the captured output: it must match want.
func runSlotLookupScript(t *testing.T, src, want string) {
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
	if _, err := pythonrun.RunString(ts, src, "<slot-lookup>", parser.ModeFile, g, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.TrimRight(stdout.String(), "\n")
	if got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

// gate F.0a: hash(C) on a user metaclass must terminate. Before this
// fix, slot_tp_hash routed __hash__ through GetAttr, which returned
// the descriptor unbound when the receiver was a class. The bound
// method then called PyObject_Hash again, recursing forever.
//
// CPython: Objects/typeobject.c:8266 slot_tp_hash + Objects/typeobject.c:2255 lookup_maybe_method
func TestHashOnClassWithMetaclassTerminates(t *testing.T) {
	runSlotLookupScript(t, `
class Meta(type):
    pass

class C(metaclass=Meta):
    pass

print(isinstance(hash(C), int))
`, "True")
}

// gate F.0b: hash(C) returns a stable identity hash across calls.
// The slot wrapper backing object.__hash__ used to call Hash()
// recursively when reached via a user metaclass; now it computes the
// identity hash directly so hash(C) is well-defined and stable.
//
// CPython: Objects/typeobject.c:6986 object___hash__
func TestHashOnClassIsStable(t *testing.T) {
	runSlotLookupScript(t, `
class Meta(type):
    pass

class C(metaclass=Meta):
    pass

print(hash(C) == hash(C))
`, "True")
}
