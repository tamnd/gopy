// Gate tests for the os module accelerator additions.
//
// CPython: Modules/posixmodule.c

package os_test

import (
	"embed"
	"testing"

	"github.com/tamnd/gopy/test/gate"
)

//go:embed gatedata/*.py
var gateScripts embed.FS

func loadScript(t *testing.T, name string) string {
	t.Helper()
	data, err := gateScripts.ReadFile("gatedata/" + name)
	if err != nil {
		t.Fatalf("load gate script %s: %v", name, err)
	}
	return string(data)
}

func TestGateOSExtras(t *testing.T) {
	cpy := gate.FindCPython(t)
	if cpy == "" {
		t.Skip("CPython 3.14 not on PATH")
	}
	gopy := gate.BuildGopy(t)
	gate.Compare(t, cpy, gopy, loadScript(t, "os_extras.py"), t.TempDir())
}
