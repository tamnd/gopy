// Gate panel for the gc built-in module registration. Mirrors the
// test_gc boundary: import gc; check the eight exposed names; pin
// the simple toggles (enable/disable/isenabled) and the threshold/
// count tuples.

package gc

import (
	"strings"
	"testing"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func TestInittabRegistration(t *testing.T) {
	if imp.FindInitFunc("gc") == nil {
		t.Fatal("gc not registered in inittab")
	}
}

func TestBuildModuleExports(t *testing.T) {
	mod, err := buildModule()
	if err != nil {
		t.Fatalf("buildModule: %v", err)
	}
	want := []string{
		"collect", "enable", "disable", "isenabled",
		"get_threshold", "set_threshold", "get_count", "is_tracked",
	}
	for _, name := range want {
		v, err := mod.Dict().GetItem(objects.NewStr(name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if v == nil {
			t.Fatalf("%s: nil entry", name)
		}
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
	if got != "gc" {
		t.Fatalf("__name__ = %q, want %q", got, "gc")
	}
}

func TestEnableDisableRoundTrip(t *testing.T) {
	prev := IsEnabled()
	defer func() {
		if prev {
			Enable()
		} else {
			Disable()
		}
	}()

	if _, err := gcDisable(nil, nil); err != nil {
		t.Fatalf("disable(): %v", err)
	}
	if IsEnabled() {
		t.Fatal("after disable(): IsEnabled true, want false")
	}
	out, err := gcIsEnabled(nil, nil)
	if err != nil {
		t.Fatalf("isenabled(): %v", err)
	}
	if out != objects.NewBool(false) {
		t.Fatalf("isenabled() = %v, want False", out)
	}
	if _, err := gcEnable(nil, nil); err != nil {
		t.Fatalf("enable(): %v", err)
	}
	if !IsEnabled() {
		t.Fatal("after enable(): IsEnabled false, want true")
	}
}

func TestThresholdRoundTrip(t *testing.T) {
	prev0, prev1, prev2 := GetThreshold()
	defer SetThreshold(prev0, prev1, prev2)

	in := []objects.Object{
		objects.NewInt(123),
		objects.NewInt(45),
		objects.NewInt(6),
	}
	if _, err := gcSetThreshold(in, nil); err != nil {
		t.Fatalf("set_threshold: %v", err)
	}
	tup, err := gcGetThreshold(nil, nil)
	if err != nil {
		t.Fatalf("get_threshold: %v", err)
	}
	got, ok := tup.(*objects.Tuple)
	if !ok {
		t.Fatalf("get_threshold returned %T, want *Tuple", tup)
	}
	want := []int64{123, 45, 6}
	if got.Len() != 3 {
		t.Fatalf("len = %d, want 3", got.Len())
	}
	for i, w := range want {
		x := got.Item(i).(*objects.Int)
		v, fits := x.Int64()
		if !fits || v != w {
			t.Fatalf("item %d = %v, want %d", i, x, w)
		}
	}
}

func TestSetThresholdRequiresInt(t *testing.T) {
	_, err := gcSetThreshold([]objects.Object{objects.NewStr("oops")}, nil)
	if err == nil {
		t.Fatal("set_threshold('oops'): want TypeError, got nil")
	}
	if !strings.Contains(err.Error(), "TypeError") {
		t.Fatalf("err=%v, want TypeError", err)
	}
}

func TestSetThresholdRejectsKwargs(t *testing.T) {
	_, err := gcSetThreshold(
		[]objects.Object{objects.NewInt(1)},
		map[string]objects.Object{"foo": objects.NewInt(2)},
	)
	if err == nil {
		t.Fatal("set_threshold(1, foo=2): want TypeError, got nil")
	}
}

func TestSetThresholdArgRange(t *testing.T) {
	if _, err := gcSetThreshold(nil, nil); err == nil {
		t.Fatal("set_threshold(): want TypeError, got nil")
	}
	too := []objects.Object{
		objects.NewInt(1), objects.NewInt(2),
		objects.NewInt(3), objects.NewInt(4),
	}
	if _, err := gcSetThreshold(too, nil); err == nil {
		t.Fatal("set_threshold(1,2,3,4): want TypeError, got nil")
	}
}

func TestCollectReturnsZero(t *testing.T) {
	out, err := gcCollect(nil, nil)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	x, ok := out.(*objects.Int)
	if !ok {
		t.Fatalf("collect returned %T, want *Int", out)
	}
	v, _ := x.Int64()
	if v != 0 {
		t.Fatalf("collect() = %d, want 0", v)
	}
}

func TestIsTrackedFollowsTrack(t *testing.T) {
	o := objects.NewStr("watch")
	defer Untrack(o)
	Track(o)
	out, err := gcIsTracked([]objects.Object{o}, nil)
	if err != nil {
		t.Fatalf("is_tracked: %v", err)
	}
	if out != objects.NewBool(true) {
		t.Fatalf("is_tracked = %v, want True", out)
	}
}

func TestGetCountReturnsTriple(t *testing.T) {
	out, err := gcGetCount(nil, nil)
	if err != nil {
		t.Fatalf("get_count: %v", err)
	}
	tup, ok := out.(*objects.Tuple)
	if !ok {
		t.Fatalf("get_count returned %T, want *Tuple", out)
	}
	if tup.Len() != 3 {
		t.Fatalf("len = %d, want 3", tup.Len())
	}
}
