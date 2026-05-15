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

// TestImportSocket pins the spec 1702 socket gate: `import socket` must
// resolve via Lib/socket.py served by PathFinder, on top of the built-in
// _socket module. The IntEnum._convert_ wiring promotes AF_* / SOCK_*
// integers from _socket into AddressFamily / SocketKind enums on the
// public socket module, so we check both shapes.
//
// CPython: Lib/socket.py
func TestImportSocket(t *testing.T) {
	installSocketFinder(t)

	var stdout bytes.Buffer
	g, err := builtins.Init(&stdout)
	if err != nil {
		t.Fatalf("builtins.Init: %v", err)
	}
	if err := g.SetItem(objects.NewStr("__name__"), objects.NewStr("__main__")); err != nil {
		t.Fatalf("set __name__: %v", err)
	}
	ts := state.NewThread()
	src := `
import socket
print(socket.AF_INET == 2 or socket.AF_INET.value == 2)
print(socket.SocketKind.SOCK_STREAM.name)
print(type(socket.AddressFamily.AF_INET).__name__)
`
	if _, err := pythonrun.RunString(ts, src, "<socket-import>", parser.ModeFile, g, nil); err != nil {
		t.Fatalf("import socket: %v", err)
	}
	got := strings.TrimRight(stdout.String(), "\n")
	want := "True\nSOCK_STREAM\nAddressFamily"
	if got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func installSocketFinder(t *testing.T) {
	t.Helper()
	prev := imp.GetPathFinder()
	imp.SetPathFinder(&imp.PathFinder{
		Paths:    []string{stdlibPath(t)},
		Compiler: abcCompiler,
	})
	t.Cleanup(func() {
		imp.SetPathFinder(prev)
		for _, name := range []string{
			"socket", "enum", "io", "os", "errno",
			"_collections_abc", "types",
		} {
			imp.RemoveModule(name)
		}
	})
}
