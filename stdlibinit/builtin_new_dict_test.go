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

// runBuiltinNewScript boots gopy and runs src. The captured stdout is
// the gate output.
func runBuiltinNewScript(t *testing.T, src, want string) {
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
	if _, err := pythonrun.RunString(ts, src, "<builtin-new>", parser.ModeFile, g, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.TrimRight(stdout.String(), "\n")
	if got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

// gate F.0c: every built-in type whose tp_new is wired through
// bindCtor must also expose __new__ in its own __dict__. enum's
// _find_data_type_ uses `'__new__' in base.__dict__` to decide which
// mixin a class like IntEnum(int, ReprEnum) inherits its storage
// from. Without this, IntEnum hits the "ReprEnum subclasses must be
// mixed with a data type" guard.
//
// CPython: Objects/typeobject.c add_tp_new_wrapper installs the
// __new__ wrapper for every type with a non-NULL tp_new.
func TestBuiltinTypesExposeNewInDict(t *testing.T) {
	runBuiltinNewScript(t, `
checks = [
    ("int", int),
    ("str", str),
    ("float", float),
    ("bool", bool),
    ("list", list),
    ("tuple", tuple),
    ("bytes", bytes),
    ("set", set),
    ("frozenset", frozenset),
]
for name, t in checks:
    if "__new__" not in t.__dict__:
        print(name, "MISSING")
        break
else:
    print("all builtin types expose __new__")
`, "all builtin types expose __new__")
}
