package objects

import (
	"strings"
	"testing"
)

// TestTypeAddWatcher_AllocatesUserSlot covers the user-driven
// PyType_AddWatcher: the first allocated slot must skip past the
// reserved index 0 that CPython carves out for the optimizer.
//
// CPython: Objects/typeobject.c:1022 ("start at 1, 0 is reserved
// for cpython optimizer")
func TestTypeAddWatcher_AllocatesUserSlot(t *testing.T) {
	resetTypeWatchers(t)

	id, err := TypeAddWatcher(func(*Type) int { return 0 })
	if err != nil {
		t.Fatalf("AddWatcher: %v", err)
	}
	if id < typeReservedWatchers {
		t.Errorf("expected slot >= %d, got %d", typeReservedWatchers, id)
	}
}

// TestTypeAddWatcher_ExhaustsTable confirms RuntimeError once every
// user slot is occupied. CPython raises PyExc_RuntimeError with the
// same message text.
//
// CPython: Objects/typeobject.c:1029
func TestTypeAddWatcher_ExhaustsTable(t *testing.T) {
	resetTypeWatchers(t)

	for i := typeReservedWatchers; i < TypeMaxWatchers; i++ {
		if _, err := TypeAddWatcher(func(*Type) int { return 0 }); err != nil {
			t.Fatalf("AddWatcher slot %d: %v", i, err)
		}
	}
	_, err := TypeAddWatcher(func(*Type) int { return 0 })
	if err == nil || !strings.Contains(err.Error(), "RuntimeError") {
		t.Errorf("expected RuntimeError once the table is full, got %v", err)
	}
}

// TestTypeWatch_FiresOnInvalidate confirms a watched type's callback
// runs when InvalidateVersionTag is called.
func TestTypeWatch_FiresOnInvalidate(t *testing.T) {
	resetTypeWatchers(t)

	var fires int
	id, err := TypeAddWatcher(func(*Type) int {
		fires++
		return 0
	})
	if err != nil {
		t.Fatalf("AddWatcher: %v", err)
	}

	cls := NewType("watched", []*Type{ObjectType()})
	if err := TypeWatch(id, cls); err != nil {
		t.Fatalf("Watch: %v", err)
	}
	cls.VersionTag() // force allocation so Invalidate has work to do
	cls.InvalidateVersionTag()

	if fires != 1 {
		t.Errorf("expected exactly one fire, got %d", fires)
	}
}

// TestTypeUnwatch_StopsEvents drops a watched type and confirms no
// further events arrive on its callback.
func TestTypeUnwatch_StopsEvents(t *testing.T) {
	resetTypeWatchers(t)

	var fires int
	id, err := TypeAddWatcher(func(*Type) int {
		fires++
		return 0
	})
	if err != nil {
		t.Fatalf("AddWatcher: %v", err)
	}

	cls := NewType("watched", []*Type{ObjectType()})
	_ = TypeWatch(id, cls)
	cls.VersionTag()
	cls.InvalidateVersionTag()
	if fires == 0 {
		t.Fatalf("expected an event before Unwatch")
	}
	prev := fires

	_ = TypeUnwatch(id, cls)
	cls.VersionTag()
	cls.InvalidateVersionTag()
	if fires != prev {
		t.Errorf("unwatch must stop further events, fires=%d prev=%d", fires, prev)
	}
}

// TestTypeClearWatcher_ReleasesSlot verifies the slot is freed for
// AddWatcher to reuse.
func TestTypeClearWatcher_ReleasesSlot(t *testing.T) {
	resetTypeWatchers(t)

	id, err := TypeAddWatcher(func(*Type) int { return 0 })
	if err != nil {
		t.Fatalf("AddWatcher: %v", err)
	}
	if err := TypeClearWatcher(id); err != nil {
		t.Fatalf("ClearWatcher: %v", err)
	}
	again, err := TypeAddWatcher(func(*Type) int { return 0 })
	if err != nil {
		t.Fatalf("AddWatcher after clear: %v", err)
	}
	if again != id {
		t.Errorf("expected reused slot %d, got %d", id, again)
	}
}

// TestTypeWatch_RejectsInvalidID covers the validate_watcher_id
// guards: out-of-range, and slot pointing at no callback.
func TestTypeWatch_RejectsInvalidID(t *testing.T) {
	resetTypeWatchers(t)
	cls := NewType("watched", []*Type{ObjectType()})

	if err := TypeWatch(-1, cls); err == nil || !strings.Contains(err.Error(), "Invalid") {
		t.Errorf("negative id must yield ValueError: %v", err)
	}
	if err := TypeWatch(TypeMaxWatchers, cls); err == nil || !strings.Contains(err.Error(), "Invalid") {
		t.Errorf("out-of-range id must yield ValueError: %v", err)
	}
	if err := TypeWatch(typeReservedWatchers, cls); err == nil || !strings.Contains(err.Error(), "No type watcher") {
		t.Errorf("unbound slot must yield 'No type watcher': %v", err)
	}
}

// TestTypeSetReservedWatcher_HitsReservedSlot confirms the back-door
// helper that installs into a reserved slot (used by the optimizer
// to claim slot 0 without going through AddWatcher).
func TestTypeSetReservedWatcher_HitsReservedSlot(t *testing.T) {
	resetTypeWatchers(t)

	var fires int
	if err := TypeSetReservedWatcher(0, func(*Type) int {
		fires++
		return 0
	}); err != nil {
		t.Fatalf("SetReservedWatcher: %v", err)
	}

	cls := NewType("watched", []*Type{ObjectType()})
	if err := TypeWatch(0, cls); err != nil {
		t.Fatalf("Watch reserved id: %v", err)
	}
	cls.VersionTag()
	cls.InvalidateVersionTag()
	if fires != 1 {
		t.Errorf("reserved watcher must fire once, got %d", fires)
	}

	if err := TypeSetReservedWatcher(typeReservedWatchers, func(*Type) int { return 0 }); err == nil {
		t.Errorf("SetReservedWatcher must refuse non-reserved IDs")
	}
}

// TestInvalidateVersionTag_RecursesSubclasses confirms the gopy
// port of CPython's type_modified_unlocked subclass walk fires
// every subclass's watcher and zeros every subclass's version tag
// before this type's tag clears, matching the post-order recursion
// inside typeobject.c:1130.
func TestInvalidateVersionTag_RecursesSubclasses(t *testing.T) {
	resetTypeWatchers(t)

	var order []string
	id, err := TypeAddWatcher(func(t *Type) int {
		order = append(order, t.Name)
		return 0
	})
	if err != nil {
		t.Fatalf("AddWatcher: %v", err)
	}

	base := NewType("Base", []*Type{ObjectType()})
	sub := NewType("Sub", []*Type{base})
	grandsub := NewType("GrandSub", []*Type{sub})

	for _, ty := range []*Type{base, sub, grandsub} {
		if err := TypeWatch(id, ty); err != nil {
			t.Fatalf("Watch %s: %v", ty.Name, err)
		}
	}

	base.InvalidateVersionTag()

	// Post-order: deepest subclass fires first, base last.
	want := []string{"GrandSub", "Sub", "Base"}
	if len(order) != len(want) {
		t.Fatalf("watch order: want %v, got %v", want, order)
	}
	for i, w := range want {
		if order[i] != w {
			t.Errorf("order[%d]: want %s, got %s", i, w, order[i])
		}
	}

	// Every tag must be cleared.
	for _, ty := range []*Type{base, sub, grandsub} {
		if ty.versionTag != 0 {
			t.Errorf("%s version tag not cleared after walk", ty.Name)
		}
	}
}

// resetTypeWatchers clears the package-level watcher table. Tests
// share the global so each test must start from a clean slate.
func resetTypeWatchers(t *testing.T) {
	t.Helper()
	for i := range TypeMaxWatchers {
		typeWatchers[i] = nil
	}
	t.Cleanup(func() {
		for i := range TypeMaxWatchers {
			typeWatchers[i] = nil
		}
	})
}
