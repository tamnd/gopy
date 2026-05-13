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

// TestImportArgparse is the spec 1702 argparse gate: `import argparse` must
// resolve end-to-end via Lib/argparse.py served by PathFinder (not the stub
// inittab entry). Verifies that ArgumentParser.add_argument / parse_args
// works for the basic unittest-style usage.
//
// CPython: Lib/argparse.py
func TestImportArgparse(t *testing.T) {
	installArgparseFinder(t)

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
import argparse
p = argparse.ArgumentParser()
p.add_argument('--name')
p.add_argument('-v', action='count', default=0)
ns = p.parse_args(['--name', 'x', '-vv'])
print(ns.name)
print(ns.v)
`
	if _, err := pythonrun.RunString(ts, src, "<argparse-import>", parser.ModeFile, g, nil); err != nil {
		t.Fatalf("import argparse: %v", err)
	}
	got := strings.TrimRight(stdout.String(), "\n")
	want := "x\n2"
	if got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

// installArgparseFinder sets up PathFinder for argparse and its transitive
// deps (gettext, operator, os, re, enum, keyword, types, abc, _weakrefset,
// _collections_abc).
func installArgparseFinder(t *testing.T) {
	t.Helper()
	prev := imp.GetPathFinder()
	imp.SetPathFinder(&imp.PathFinder{
		Paths:    []string{stdlibPath(t)},
		Compiler: abcCompiler,
	})
	t.Cleanup(func() {
		imp.SetPathFinder(prev)
		for _, name := range []string{
			"argparse", "gettext", "operator",
			"enum", "keyword", "types",
			"abc", "_py_abc", "_weakrefset", "_collections_abc",
		} {
			imp.RemoveModule(name)
		}
	})
}
