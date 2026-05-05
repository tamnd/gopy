package sys

import (
	"testing"

	"github.com/tamnd/gopy/initconfig"
	"github.com/tamnd/gopy/objects"
)

// TestUpdateConfigStampsArgv pins the argv pass-through: the
// PyConfig.Argv slice lands on sys.argv as a tuple of str.
func TestUpdateConfigStampsArgv(t *testing.T) {
	d, err := Init()
	if err != nil {
		t.Fatal(err)
	}
	cfg := &initconfig.PyConfig{Argv: []string{"gopy", "-c", "pass"}}
	if uerr := UpdateConfig(d, cfg); uerr != nil {
		t.Fatalf("UpdateConfig: %v", uerr)
	}
	v, _ := d.GetItem(objects.NewStr("argv"))
	tup, ok := v.(*objects.Tuple)
	if !ok || tup.Len() != 3 {
		t.Fatalf("argv = %v, want a 3-tuple", v)
	}
	got := []string{}
	for i := 0; i < tup.Len(); i++ {
		s, _ := objects.Str(tup.Item(i))
		got = append(got, s)
	}
	for i, want := range []string{"gopy", "-c", "pass"} {
		if got[i] != want {
			t.Errorf("argv[%d] = %q, want %q", i, got[i], want)
		}
	}
}

// TestUpdateConfigPathOnlyWhenSet pins the search-path semantics:
// when ModuleSearchPathsSet is 0, sys.path is not overwritten by
// stale data from PyConfig (the path-config phase fills it in
// later). The slot still gets the empty default.
func TestUpdateConfigPathOnlyWhenSet(t *testing.T) {
	d, err := Init()
	if err != nil {
		t.Fatal(err)
	}
	cfg := &initconfig.PyConfig{}
	if uerr := UpdateConfig(d, cfg); uerr != nil {
		t.Fatalf("UpdateConfig: %v", uerr)
	}
	v, _ := d.GetItem(objects.NewStr("path"))
	tup, ok := v.(*objects.Tuple)
	if !ok {
		t.Fatalf("path is %T, want *Tuple", v)
	}
	if tup.Len() != 0 {
		t.Errorf("path should be empty when not set, got %d items", tup.Len())
	}
}

// TestUpdateConfigPathHonorsSet pins the set branch.
func TestUpdateConfigPathHonorsSet(t *testing.T) {
	d, err := Init()
	if err != nil {
		t.Fatal(err)
	}
	cfg := &initconfig.PyConfig{
		ModuleSearchPathsSet: 1,
		ModuleSearchPaths:    []string{"/lib/python", "/site"},
	}
	if uerr := UpdateConfig(d, cfg); uerr != nil {
		t.Fatalf("UpdateConfig: %v", uerr)
	}
	v, _ := d.GetItem(objects.NewStr("path"))
	tup := v.(*objects.Tuple)
	if tup.Len() != 2 {
		t.Fatalf("path has %d items, want 2", tup.Len())
	}
	first, _ := objects.Str(tup.Item(0))
	if first != "/lib/python" {
		t.Errorf("path[0] = %q, want \"/lib/python\"", first)
	}
}

// TestUpdateConfigXOptionsKVSplit pins the -X foo=bar parse: bare
// keys map to True, key=value to the value string.
func TestUpdateConfigXOptionsKVSplit(t *testing.T) {
	d, err := Init()
	if err != nil {
		t.Fatal(err)
	}
	cfg := &initconfig.PyConfig{XOptions: []string{"dev", "tracemalloc=5"}}
	if uerr := UpdateConfig(d, cfg); uerr != nil {
		t.Fatalf("UpdateConfig: %v", uerr)
	}
	v, _ := d.GetItem(objects.NewStr("_xoptions"))
	xd, ok := v.(*objects.Dict)
	if !ok {
		t.Fatalf("_xoptions is %T, want *Dict", v)
	}
	dev, err := xd.GetItem(objects.NewStr("dev"))
	if err != nil {
		t.Fatalf("_xoptions['dev']: %v", err)
	}
	if dev != objects.True() {
		t.Errorf("_xoptions['dev'] = %v, want True", dev)
	}
	tm, err := xd.GetItem(objects.NewStr("tracemalloc"))
	if err != nil {
		t.Fatalf("_xoptions['tracemalloc']: %v", err)
	}
	got, _ := objects.Str(tm)
	if got != "5" {
		t.Errorf("_xoptions['tracemalloc'] = %q, want \"5\"", got)
	}
}

// TestUpdateConfigPycachePrefixNoneByDefault pins the empty-string
// branch: when PyConfig.PycachePrefix is "", sys.pycache_prefix is
// None (matching the wide-string NULL check in CPython).
func TestUpdateConfigPycachePrefixNoneByDefault(t *testing.T) {
	d, err := Init()
	if err != nil {
		t.Fatal(err)
	}
	if uerr := UpdateConfig(d, &initconfig.PyConfig{}); uerr != nil {
		t.Fatalf("UpdateConfig: %v", uerr)
	}
	v, _ := d.GetItem(objects.NewStr("pycache_prefix"))
	if v != objects.None() {
		t.Errorf("pycache_prefix = %v, want None", v)
	}
}
