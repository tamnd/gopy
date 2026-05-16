// Gate tests for the specializer. Each .py file under gatedata/ runs
// under both CPython 3.14 and the gopy binary; stdout must match byte
// for byte. This is the parity check spec 1712 P1.4b cites as the
// smoke test for LOAD_ATTR fast paths.

package specialize_test

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

func TestGateSpecPropertyAndMethod(t *testing.T) {
	cpy := gate.FindCPython(t)
	if cpy == "" {
		t.Skip("CPython 3.14 not on PATH")
	}
	gopy := gate.BuildGopy(t)
	gate.Compare(t, cpy, gopy, loadScript(t, "spec_property.py"))
}

func TestGateSpecBinaryOp(t *testing.T) {
	cpy := gate.FindCPython(t)
	if cpy == "" {
		t.Skip("CPython 3.14 not on PATH")
	}
	gopy := gate.BuildGopy(t)
	gate.Compare(t, cpy, gopy, loadScript(t, "spec_binary_op.py"))
}

func TestGateSpecLoadGlobal(t *testing.T) {
	cpy := gate.FindCPython(t)
	if cpy == "" {
		t.Skip("CPython 3.14 not on PATH")
	}
	gopy := gate.BuildGopy(t)
	gate.Compare(t, cpy, gopy, loadScript(t, "spec_load_global.py"))
}
