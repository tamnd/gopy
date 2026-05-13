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

// runNameOpsScript runs src under a fresh gopy runtime with __name__
// set, then compares the captured stdout against want. The script is
// the gate: a class body that exercises STORE_NAME / LOAD_NAME /
// DELETE_NAME against a metaclass-prepared dict subclass.
func runNameOpsScript(t *testing.T, src, want string) {
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
	if _, err := pythonrun.RunString(ts, src, "<name-ops>", parser.ModeFile, g, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.TrimRight(stdout.String(), "\n")
	if got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

// gate 8.1: LOAD_NAME inside a class body must go through the mapping
// protocol when the namespace is a dict subclass returned by the
// metaclass __prepare__, so __getitem__ overrides fire.
//
// CPython: Python/bytecodes.c LOAD_NAME (PyMapping_GetOptionalItem)
func TestLoadNameThroughDictSubclassGetItem(t *testing.T) {
	runNameOpsScript(t, `
class TracingDict(dict):
    log = []
    def __getitem__(self, key):
        TracingDict.log.append(("get", key))
        return super().__getitem__(key)

class Meta(type):
    @classmethod
    def __prepare__(mcs, name, bases):
        d = TracingDict()
        d["X"] = 10
        return d

class C(metaclass=Meta):
    Y = X + 1

print(C.Y)
print([entry for entry in TracingDict.log if entry[1] == "X"])
`, "11\n[('get', 'X')]")
}

// gate 8.2: STORE_NAME inside a class body must route through the
// mapping protocol against a dict-subclass namespace, so __setitem__
// overrides see every assignment.
//
// CPython: Python/bytecodes.c STORE_NAME (PyObject_SetItem)
func TestStoreNameThroughDictSubclassSetItem(t *testing.T) {
	runNameOpsScript(t, `
class TracingDict(dict):
    log = []
    def __setitem__(self, key, value):
        TracingDict.log.append(("set", key, value))
        super().__setitem__(key, value)
    def __getitem__(self, key):
        return super().__getitem__(key)

class Meta(type):
    @classmethod
    def __prepare__(mcs, name, bases):
        return TracingDict()

class C(metaclass=Meta):
    A = 1
    B = A + 2

print(C.A, C.B)
print([entry for entry in TracingDict.log if entry[1] in ("A", "B")])
`, "1 3\n[('set', 'A', 1), ('set', 'B', 3)]")
}

// gate 8.3: DELETE_NAME inside a class body must route through the
// mapping protocol against a dict-subclass namespace, so __delitem__
// overrides fire. Before Phase 8 the deleteIn path panicked on any
// non-Dict scope and bypassed __delitem__ on dict subclasses.
//
// CPython: Python/bytecodes.c DELETE_NAME (PyObject_DelItem)
func TestDeleteNameThroughDictSubclassDelItem(t *testing.T) {
	runNameOpsScript(t, `
class TracingDict(dict):
    log = []
    def __setitem__(self, key, value):
        super().__setitem__(key, value)
    def __getitem__(self, key):
        return super().__getitem__(key)
    def __delitem__(self, key):
        TracingDict.log.append(("del", key))
        super().__delitem__(key)

class Meta(type):
    @classmethod
    def __prepare__(mcs, name, bases):
        return TracingDict()

class C(metaclass=Meta):
    X = 1
    del X

print(hasattr(C, "X"))
print(TracingDict.log)
`, "False\n[('del', 'X')]")
}
