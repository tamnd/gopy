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

// gate F.1: `import enum` must succeed end-to-end. The module exercises
// nearly every PEP 487 hook (EnumType metaclass, _proto_member.__set_name__,
// classmethod/staticmethod descriptor detection, type(name, bases, ns,
// boundary=...) with a metaclass override).
func TestImportEnum(t *testing.T) {
	installStdlibFinder(t)
	t.Cleanup(func() { imp.RemoveModule("enum") })

	var stdout bytes.Buffer
	g, err := builtins.Init(&stdout)
	if err != nil {
		t.Fatalf("builtins.Init: %v", err)
	}
	if err := g.SetItem(objects.NewStr("__name__"), objects.NewStr("__main__")); err != nil {
		t.Fatalf("set __name__: %v", err)
	}
	ts := state.NewThread()
	src := "import enum\nprint(enum.FlagBoundary.STRICT.name)\nfor m in enum.FlagBoundary:\n    print(m.name)\n"
	if _, err := pythonrun.RunString(ts, src, "<enum-import>", parser.ModeFile, g, nil); err != nil {
		t.Fatalf("import enum: %v", err)
	}
	got := strings.TrimRight(stdout.String(), "\n")
	want := "STRICT\nSTRICT\nCONFORM\nEJECT\nKEEP"
	if got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

