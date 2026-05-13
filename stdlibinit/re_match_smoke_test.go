package stdlibinit_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tamnd/gopy/builtins"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/parser"
	"github.com/tamnd/gopy/pythonrun"
	"github.com/tamnd/gopy/state"
)

// TestImportReMatchGroupsSmoke pins F.3 of spec 1704: re.match runs
// end-to-end against the vendored stdlib re package and the bytes /
// bytearray method panel that re._compiler depends on.
func TestImportReMatchGroupsSmoke(t *testing.T) {
	installStdlibFinder(t)
	t.Cleanup(func() {
		imp.RemoveModule("re")
		imp.RemoveModule("enum")
	})

	var stdout bytes.Buffer
	g, err := builtins.Init(&stdout)
	if err != nil {
		t.Fatalf("builtins.Init: %v", err)
	}
	if err := g.SetItem(objects.NewStr("__name__"), objects.NewStr("__main__")); err != nil {
		t.Fatalf("set __name__: %v", err)
	}
	ts := state.NewThread()
	src := "import re\n" +
		"m = re.match(r\"(\\d+)-(\\d+)\", \"12-34\")\n" +
		"print(m.groups())\n"
	if _, err := pythonrun.RunString(ts, src, "<re-match>", parser.ModeFile, g, nil); err != nil {
		t.Fatalf("re.match smoke: %v", err)
	}
	got := strings.TrimRight(stdout.String(), "\n")
	want := "('12', '34')"
	if got != want {
		t.Fatalf("re.match groups: got %q, want %q", got, want)
	}
}
