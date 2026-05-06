package sys

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

// TestImplementationNameIsGopy pins sys.implementation.name == "gopy"
// per spec 1622: third-party runners branch off the name to detect
// the gopy interpreter, so the value must not drift.
func TestImplementationNameIsGopy(t *testing.T) {
	d, err := Init()
	if err != nil {
		t.Fatal(err)
	}
	v, _ := d.GetItem(objects.NewStr("implementation"))
	tup := v.(*objects.Tuple)
	name, _ := objects.Str(tup.Item(0))
	if name != "gopy" {
		t.Errorf("implementation.name = %q, want \"gopy\"", name)
	}
	if ImplementationName != "gopy" {
		t.Errorf("ImplementationName const = %q, want \"gopy\"", ImplementationName)
	}
}

// TestImplementationCacheTagIsGopy3140 pins sys.implementation.cache_tag
// per spec 1622 and 1600: gopy compiles to .pyc files tagged
// "gopy-3140" so they do not collide with cpython-3140 caches on the
// same filesystem.
func TestImplementationCacheTagIsGopy3140(t *testing.T) {
	d, err := Init()
	if err != nil {
		t.Fatal(err)
	}
	v, _ := d.GetItem(objects.NewStr("implementation"))
	tup := v.(*objects.Tuple)
	cache, _ := objects.Str(tup.Item(3))
	if cache != "gopy-3140" {
		t.Errorf("implementation.cache_tag = %q, want \"gopy-3140\"", cache)
	}
	if CacheTag != "gopy-3140" {
		t.Errorf("CacheTag const = %q, want \"gopy-3140\"", CacheTag)
	}
}
