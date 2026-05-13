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

// runDictContainsScript boots gopy and runs src. The captured stdout is
// the gate output.
func runDictContainsScript(t *testing.T, src, want string) {
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
	if _, err := pythonrun.RunString(ts, src, "<dict-contains>", parser.ModeFile, g, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.TrimRight(stdout.String(), "\n")
	if got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

// gate F.1a: `key in d` on a dict subclass must return False for missing
// keys, not surface the internal errKeyNotFound sentinel as a KeyError.
// enum.EnumDict.__setitem__ does `if key in self` against the
// dict-subclass namespace; before this fix STRICT = auto() raised
// KeyError because dict.__contains__ propagated the sentinel.
//
// CPython: Objects/dictobject.c:3735 dict_contains
func TestDictContainsOnSubclassMissingKey(t *testing.T) {
	runDictContainsScript(t, `
class MyDict(dict):
    pass

d = MyDict()
print("x" in d)
d["x"] = 1
print("x" in d)
print("y" in d)
`, "False\nTrue\nFalse")
}

// gate F.1b: a class body executed against a dict-subclass namespace
// whose __setitem__ probes the dict with `if key in self` must succeed.
// This is the exact pattern enum.EnumDict relies on.
func TestDictContainsInsideClassNamespaceSetItem(t *testing.T) {
	runDictContainsScript(t, `
class MyDict(dict):
    def __setitem__(self, key, value):
        if key in self:
            value = ("dup", value)
        super().__setitem__(key, value)

class Meta(type):
    @classmethod
    def __prepare__(mcs, name, bases):
        return MyDict()

class C(metaclass=Meta):
    X = 1
    Y = 2

print(C.X, C.Y)
`, "1 2")
}
