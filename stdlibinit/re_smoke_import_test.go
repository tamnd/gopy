package stdlibinit_test

import (
	"bytes"
	"testing"

	"github.com/tamnd/gopy/builtins"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/parser"
	"github.com/tamnd/gopy/pythonrun"
	"github.com/tamnd/gopy/state"
)

func TestImportReBareSmoke(t *testing.T) {
	installStdlibFinder(t)
	t.Cleanup(func() { imp.RemoveModule("re") })

	var stdout bytes.Buffer
	g, err := builtins.Init(&stdout)
	if err != nil {
		t.Fatalf("builtins.Init: %v", err)
	}
	if err := g.SetItem(objects.NewStr("__name__"), objects.NewStr("__main__")); err != nil {
		t.Fatalf("set __name__: %v", err)
	}
	ts := state.NewThread()
	src := "import re\n"
	if _, err := pythonrun.RunString(ts, src, "<re-import>", parser.ModeFile, g, nil); err != nil {
		t.Fatalf("import re: %v", err)
	}
}
