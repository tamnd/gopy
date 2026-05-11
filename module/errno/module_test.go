// Gate panel for the errno built-in module registration. Mirrors the
// CPython test_errno surface: check the inittab entry, build the
// module, assert the platform-relevant ENOENT/EAGAIN/EINTR constants
// resolve to ints, and round-trip errorcode[ENOENT] back to the name.

package errno

import (
	"testing"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func TestInittabRegistration(t *testing.T) {
	if imp.FindInitFunc("errno") == nil {
		t.Fatal("errno not registered in inittab")
	}
	found := false
	for _, e := range imp.InittabSnapshot() {
		if e.Name == "errno" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("errno missing from InittabSnapshot()")
	}
}

func TestModuleNameAttr(t *testing.T) {
	mod, err := buildModule()
	if err != nil {
		t.Fatalf("buildModule: %v", err)
	}
	v, err := mod.Dict().GetItem(objects.NewStr("__name__"))
	if err != nil {
		t.Fatalf("__name__: %v", err)
	}
	got, err := objects.Str(v)
	if err != nil {
		t.Fatalf("Str(__name__): %v", err)
	}
	if got != "errno" {
		t.Fatalf("__name__ = %q, want %q", got, "errno")
	}
}

func TestCommonConstantsExist(t *testing.T) {
	mod, err := buildModule()
	if err != nil {
		t.Fatalf("buildModule: %v", err)
	}
	d := mod.Dict()
	for _, name := range []string{"ENOENT", "EAGAIN", "EINTR"} {
		v, err := d.GetItem(objects.NewStr(name))
		if err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
		if _, ok := v.(*objects.Int); !ok {
			t.Fatalf("%s: got %T, want *objects.Int", name, v)
		}
	}
}

func TestErrorcodeReverseMap(t *testing.T) {
	mod, err := buildModule()
	if err != nil {
		t.Fatalf("buildModule: %v", err)
	}
	d := mod.Dict()
	enoent, err := d.GetItem(objects.NewStr("ENOENT"))
	if err != nil {
		t.Fatalf("ENOENT: %v", err)
	}
	ec, err := d.GetItem(objects.NewStr("errorcode"))
	if err != nil {
		t.Fatalf("errorcode: %v", err)
	}
	errorcode, ok := ec.(*objects.Dict)
	if !ok {
		t.Fatalf("errorcode is %T, want *objects.Dict", ec)
	}
	name, err := errorcode.GetItem(enoent)
	if err != nil {
		t.Fatalf("errorcode[ENOENT]: %v", err)
	}
	got, err := objects.Str(name)
	if err != nil {
		t.Fatalf("Str(name): %v", err)
	}
	if got != "ENOENT" {
		t.Fatalf("errorcode[ENOENT] = %q, want %q", got, "ENOENT")
	}
}
