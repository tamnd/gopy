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

// runStrSubclassScript boots gopy and runs src. The captured stdout is
// the gate output.
func runStrSubclassScript(t *testing.T, src, want string) {
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
	if _, err := pythonrun.RunString(ts, src, "<str-subclass>", parser.ModeFile, g, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.TrimRight(stdout.String(), "\n")
	if got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

// gate F.1c: a plain `class MyStr(str): pass` followed by `MyStr("hi")`
// must yield an instance of MyStr, not of str. enum.StrEnum relies on
// `member = str.__new__(cls, value)` returning a cls-tagged object.
//
// CPython: Objects/unicodeobject.c:13993 unicode_subtype_new
func TestStrSubclassConstructorReturnsSubclass(t *testing.T) {
	runStrSubclassScript(t, `
class MyStr(str):
    pass

s = MyStr("hi")
print(type(s).__name__)
print(isinstance(s, MyStr))
print(isinstance(s, str))
print(s == "hi")
print(s)
`, "MyStr\nTrue\nTrue\nTrue\nhi")
}

// gate F.1d: a str subclass instance must accept instance attributes.
// enum's _proto_member.__set_name__ does `enum_member._value_ = ...`
// against the freshly built member. Without per-instance storage this
// raises "AttributeError: 'str' object has no attribute '_value_'".
//
// CPython: Objects/typeobject.c type_new_descriptors picks up __dict__
// from object for str subclasses.
func TestStrSubclassInstanceAttribute(t *testing.T) {
	runStrSubclassScript(t, `
class MyStr(str):
    pass

s = MyStr("hi")
s.tag = "marker"
print(s.tag)
del s.tag
try:
    s.tag
except AttributeError:
    print("ok")
`, "marker\nok")
}

// gate F.1e: str subclass instances inherit str.__repr__, str.__str__,
// str.__hash__, and str's rich comparisons. Before the slot-wrapper
// fix, MyStr("hi") printed as `<MyStr object at 0x...>`, hashed by id,
// and compared unequal to "hi".
//
// CPython: Objects/typeobject.c slotdefs (TPSLOT entries for __repr__,
// __str__, __hash__, __eq__) install slot wrappers in str's __dict__,
// which subclasses then find via MRO walks.
func TestStrSubclassInheritsSlotWrappers(t *testing.T) {
	runStrSubclassScript(t, `
class MyStr(str):
    pass

a = MyStr("hi")
b = MyStr("hi")
print(repr(a))
print(str(a))
print(hash(a) == hash("hi"))
print(a == b)
print(a == "hi")
print("hi" == a)
`, "'hi'\nhi\nTrue\nTrue\nTrue\nTrue")
}
