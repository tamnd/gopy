package imp

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

// TestAddGetModule pins the AddModule / GetModule round-trip.
func TestAddGetModule(t *testing.T) {
	mod := objects.NewModule("testmod_sysmod")
	AddModule("testmod_sysmod", mod)

	got, ok := GetModule("testmod_sysmod")
	if !ok {
		t.Fatal("GetModule: not found after AddModule")
	}
	if got != mod {
		t.Error("GetModule returned different pointer")
	}
}

// TestRemoveModule pins that RemoveModule removes the entry.
func TestRemoveModule(t *testing.T) {
	mod := objects.NewModule("testmod_remove")
	AddModule("testmod_remove", mod)
	RemoveModule("testmod_remove")

	_, ok := GetModule("testmod_remove")
	if ok {
		t.Error("GetModule returned entry after RemoveModule")
	}
}

// TestRemoveModuleNoop pins that removing a non-existent key is safe.
func TestRemoveModuleNoop(t *testing.T) {
	RemoveModule("no_such_module_xyz_remove") // must not panic
}

// TestSysModulesSnapshot pins that the snapshot contains added entries.
func TestSysModulesSnapshot(t *testing.T) {
	mod := objects.NewModule("testmod_snap")
	AddModule("testmod_snap", mod)

	snap := SysModulesSnapshot()
	if snap["testmod_snap"] != mod {
		t.Error("snapshot missing added module")
	}
}
