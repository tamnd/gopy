// End-to-end check that open() drives a real round-trip through the
// builtins dict the vm sees: open for write, write, close, reopen,
// read, close.

package vm

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tamnd/gopy/builtins"
	_io "github.com/tamnd/gopy/module/io"
	"github.com/tamnd/gopy/objects"
)

func TestOpenBuiltinRoundTripsThroughVM(t *testing.T) {
	g, err := builtins.Init(nil)
	if err != nil {
		t.Fatalf("builtins.Init: %v", err)
	}
	openFn, err := g.GetItem(objects.NewStr("open"))
	if err != nil {
		t.Fatalf("globals[open]: %v", err)
	}
	open := openFn.(*objects.BuiltinFunction).Fn

	dir := t.TempDir()
	path := filepath.Join(dir, "round-trip.txt")

	out, err := open([]objects.Object{
		objects.NewStr(path),
		objects.NewStr("w"),
	}, nil)
	if err != nil {
		t.Fatalf("open(w): %v", err)
	}
	wfile := out.(*_io.TextIOWrapper)
	if _, err := wfile.Write("vm round-trip\n"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := wfile.Close(); err != nil {
		t.Fatalf("Close(w): %v", err)
	}

	out, err = open([]objects.Object{
		objects.NewStr(path),
	}, nil)
	if err != nil {
		t.Fatalf("open(r): %v", err)
	}
	rfile := out.(*_io.TextIOWrapper)
	defer rfile.Close() //nolint
	got, err := rfile.Read(-1)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got != "vm round-trip\n" {
		t.Fatalf("read back %q", got)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stat: %v", err)
	}
}
