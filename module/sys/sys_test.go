package sys

import (
	"strings"
	"testing"

	"github.com/tamnd/gopy/objects"
)

// TestInitStaticAttributesPresent pins the keys the v0.7 sys-A
// surface advertises. The test is structural; the values are
// covered case by case below.
func TestInitStaticAttributesPresent(t *testing.T) {
	d, err := Init()
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	want := []string{
		"version", "platform", "byteorder", "copyright",
		"_framework", "abiflags", "float_repr_style",
		"hexversion", "api_version", "maxsize", "maxunicode",
		"version_info", "implementation", "builtin_module_names",
		"stdlib_module_names", "hash_info",
	}
	for _, name := range want {
		has, _ := d.Contains(objects.NewStr(name))
		if !has {
			t.Errorf("sys dict missing %q", name)
		}
	}
}

// TestInitVersionInfoShape pins the five-tuple shape: (major,
// minor, micro, releaselevel, serial). Keep CPython's order so the
// 1651-sys-C named-tuple wrap-up is a pure rename.
func TestInitVersionInfoShape(t *testing.T) {
	d, err := Init()
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	v, err := d.GetItem(objects.NewStr("version_info"))
	if err != nil {
		t.Fatalf("GetItem(version_info): %v", err)
	}
	tup, ok := v.(*objects.Tuple)
	if !ok {
		t.Fatalf("version_info is %T, want *Tuple", v)
	}
	if tup.Len() != 5 {
		t.Fatalf("version_info has %d items, want 5", tup.Len())
	}
	level, lerr := objects.Str(tup.Item(3))
	if lerr != nil || level != "final" {
		t.Errorf("releaselevel = %q (err=%v), want \"final\"", level, lerr)
	}
}

// TestInitImplementationName pins sys.implementation.name == "gopy".
func TestInitImplementationName(t *testing.T) {
	d, err := Init()
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	v, err := d.GetItem(objects.NewStr("implementation"))
	if err != nil {
		t.Fatalf("GetItem(implementation): %v", err)
	}
	tup, ok := v.(*objects.Tuple)
	if !ok {
		t.Fatalf("implementation is %T, want *Tuple", v)
	}
	name, nerr := objects.Str(tup.Item(0))
	if nerr != nil || name != "gopy" {
		t.Errorf("implementation[0] = %q (err=%v), want \"gopy\"", name, nerr)
	}
}

// TestInitByteorderMatchesHost pins sys.byteorder against the host
// platform. Since gopy targets darwin/arm64 and linux/amd64 first
// (both little-endian), the expected default is "little"; we read
// it out of the dict and check the value is one of the two known
// strings to keep the test platform-portable.
func TestInitByteorderValid(t *testing.T) {
	d, err := Init()
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	v, err := d.GetItem(objects.NewStr("byteorder"))
	if err != nil {
		t.Fatalf("GetItem(byteorder): %v", err)
	}
	s, serr := objects.Str(v)
	if serr != nil {
		t.Fatalf("Str(byteorder): %v", serr)
	}
	if s != "little" && s != "big" {
		t.Errorf("byteorder = %q, want little or big", s)
	}
}

// TestInitVersionMentionsGopy pins that sys.version starts with
// the gopy banner so consumers (REPL banner, scripts that sniff
// the runtime) can identify the implementation.
func TestInitVersionMentionsGopy(t *testing.T) {
	d, err := Init()
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	v, _ := d.GetItem(objects.NewStr("version"))
	s, _ := objects.Str(v)
	if !strings.HasPrefix(s, "gopy ") {
		t.Errorf("version = %q, must start with \"gopy \"", s)
	}
}

// TestInitMaxsizePositive pins sys.maxsize > 0 (CPython guarantees
// this on every supported platform).
func TestInitMaxsizePositive(t *testing.T) {
	d, err := Init()
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	v, _ := d.GetItem(objects.NewStr("maxsize"))
	i, ok := v.(*objects.Int)
	if !ok {
		t.Fatalf("maxsize is %T, want *Int", v)
	}
	got, _ := i.Int64()
	if got <= 0 {
		t.Errorf("maxsize = %d, want > 0", got)
	}
}

// TestInitBuiltinModuleNamesIncludesSys pins the v0.7 module list:
// builtins and sys are static-init, so they are advertised here.
// Once 1623 lands the import system, this list grows.
func TestInitBuiltinModuleNamesIncludesSys(t *testing.T) {
	d, err := Init()
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	v, _ := d.GetItem(objects.NewStr("builtin_module_names"))
	tup, ok := v.(*objects.Tuple)
	if !ok {
		t.Fatalf("builtin_module_names is %T, want *Tuple", v)
	}
	want := map[string]bool{"builtins": false, "sys": false}
	for i := 0; i < tup.Len(); i++ {
		s, err := objects.Str(tup.Item(i))
		if err != nil {
			continue
		}
		if _, tracked := want[s]; tracked {
			want[s] = true
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("builtin_module_names missing %q", name)
		}
	}
}
