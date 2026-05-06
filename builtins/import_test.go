package builtins

import (
	"errors"
	"strings"
	"testing"

	"github.com/tamnd/gopy/objects"
)

type importCall struct {
	name     string
	pkgname  string
	level    int
	fromlist []string
}

func captureImporter(t *testing.T, mod objects.Object, returnErr error) *importCall {
	t.Helper()
	prev := currentImporter
	t.Cleanup(func() { SetImporter(prev) })

	got := &importCall{}
	SetImporter(func(name, pkgname string, level int, fromlist []string) (objects.Object, error) {
		got.name = name
		got.pkgname = pkgname
		got.level = level
		got.fromlist = fromlist
		return mod, returnErr
	})
	return got
}

func TestImportRoutesPositionalArgs(t *testing.T) {
	mod := objects.NewModule("a")
	got := captureImporter(t, mod, nil)
	out, err := Import([]objects.Object{
		objects.NewStr("a"),
		objects.None(),
		objects.None(),
		objects.NewTuple(nil),
		objects.NewInt(0),
	}, nil)
	if err != nil {
		t.Fatalf("__import__: %v", err)
	}
	if out != mod {
		t.Fatalf("__import__ returned %v, want %v", out, mod)
	}
	if got.name != "a" || got.level != 0 || len(got.fromlist) != 0 {
		t.Fatalf("hook called with %+v, want name=a level=0 fromlist=[]", got)
	}
}

func TestImportFromlistTuple(t *testing.T) {
	got := captureImporter(t, objects.NewModule("pkg"), nil)
	_, err := Import([]objects.Object{
		objects.NewStr("pkg"),
		objects.None(),
		objects.None(),
		objects.NewTuple([]objects.Object{objects.NewStr("sub"), objects.NewStr("other")}),
	}, nil)
	if err != nil {
		t.Fatalf("__import__: %v", err)
	}
	if len(got.fromlist) != 2 || got.fromlist[0] != "sub" || got.fromlist[1] != "other" {
		t.Fatalf("fromlist = %v, want [sub other]", got.fromlist)
	}
}

func TestImportFromlistList(t *testing.T) {
	got := captureImporter(t, objects.NewModule("pkg"), nil)
	_, err := Import([]objects.Object{
		objects.NewStr("pkg"),
		objects.None(),
		objects.None(),
		objects.NewList([]objects.Object{objects.NewStr("x")}),
	}, nil)
	if err != nil {
		t.Fatalf("__import__: %v", err)
	}
	if len(got.fromlist) != 1 || got.fromlist[0] != "x" {
		t.Fatalf("fromlist = %v, want [x]", got.fromlist)
	}
}

func TestImportLevelKwarg(t *testing.T) {
	got := captureImporter(t, objects.NewModule("inner"), nil)
	g := objects.NewDict()
	if err := g.SetItem(objects.NewStr("__name__"), objects.NewStr("pkg.sub")); err != nil {
		t.Fatalf("set __name__: %v", err)
	}
	_, err := Import(
		[]objects.Object{objects.NewStr("x")},
		map[string]objects.Object{
			"globals": g,
			"level":   objects.NewInt(1),
		},
	)
	if err != nil {
		t.Fatalf("__import__: %v", err)
	}
	if got.level != 1 {
		t.Fatalf("level = %d, want 1", got.level)
	}
	if got.pkgname != "pkg" {
		t.Fatalf("pkgname = %q, want %q", got.pkgname, "pkg")
	}
}

func TestImportPackageOverridesName(t *testing.T) {
	got := captureImporter(t, objects.NewModule("inner"), nil)
	g := objects.NewDict()
	if err := g.SetItem(objects.NewStr("__name__"), objects.NewStr("pkg.sub")); err != nil {
		t.Fatalf("set __name__: %v", err)
	}
	if err := g.SetItem(objects.NewStr("__package__"), objects.NewStr("other")); err != nil {
		t.Fatalf("set __package__: %v", err)
	}
	_, err := Import(
		[]objects.Object{objects.NewStr("x")},
		map[string]objects.Object{
			"globals": g,
			"level":   objects.NewInt(1),
		},
	)
	if err != nil {
		t.Fatalf("__import__: %v", err)
	}
	if got.pkgname != "other" {
		t.Fatalf("pkgname = %q, want %q", got.pkgname, "other")
	}
}

func TestImportPackageDirHasPath(t *testing.T) {
	got := captureImporter(t, objects.NewModule("inner"), nil)
	g := objects.NewDict()
	if err := g.SetItem(objects.NewStr("__name__"), objects.NewStr("pkg")); err != nil {
		t.Fatalf("set __name__: %v", err)
	}
	if err := g.SetItem(objects.NewStr("__path__"), objects.NewStr("/some/dir")); err != nil {
		t.Fatalf("set __path__: %v", err)
	}
	_, err := Import(
		[]objects.Object{objects.NewStr("x")},
		map[string]objects.Object{
			"globals": g,
			"level":   objects.NewInt(1),
		},
	)
	if err != nil {
		t.Fatalf("__import__: %v", err)
	}
	if got.pkgname != "pkg" {
		t.Fatalf("pkgname = %q, want %q", got.pkgname, "pkg")
	}
}

func TestImportMissingNameTypeError(t *testing.T) {
	captureImporter(t, nil, nil)
	_, err := Import(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "missing required argument: 'name'") {
		t.Fatalf("__import__(): err=%v, want missing-name TypeError", err)
	}
}

func TestImportTooManyPositionalTypeError(t *testing.T) {
	captureImporter(t, nil, nil)
	args := []objects.Object{
		objects.NewStr("a"), objects.None(), objects.None(),
		objects.NewTuple(nil), objects.NewInt(0), objects.NewInt(1),
	}
	_, err := Import(args, nil)
	if err == nil || !strings.Contains(err.Error(), "at most 5 arguments") {
		t.Fatalf("__import__: err=%v, want too-many-args TypeError", err)
	}
}

func TestImportNameNotStr(t *testing.T) {
	captureImporter(t, nil, nil)
	_, err := Import([]objects.Object{objects.NewInt(1)}, nil)
	if err == nil || !strings.Contains(err.Error(), "name must be str") {
		t.Fatalf("__import__: err=%v, want name-not-str TypeError", err)
	}
}

func TestImportNegativeLevel(t *testing.T) {
	captureImporter(t, nil, nil)
	_, err := Import([]objects.Object{
		objects.NewStr("a"),
		objects.None(),
		objects.None(),
		objects.NewTuple(nil),
		objects.NewInt(-1),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "level must be >= 0") {
		t.Fatalf("__import__: err=%v, want negative-level ValueError", err)
	}
}

func TestImportFromlistRejectsString(t *testing.T) {
	captureImporter(t, nil, nil)
	_, err := Import([]objects.Object{
		objects.NewStr("a"),
		objects.None(),
		objects.None(),
		objects.NewStr("notalist"),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "fromlist must be a tuple or list") {
		t.Fatalf("__import__: err=%v, want fromlist TypeError", err)
	}
}

func TestImportDuplicatePosAndKwarg(t *testing.T) {
	captureImporter(t, nil, nil)
	_, err := Import(
		[]objects.Object{objects.NewStr("a")},
		map[string]objects.Object{"name": objects.NewStr("b")},
	)
	if err == nil || !strings.Contains(err.Error(), "multiple values") {
		t.Fatalf("__import__: err=%v, want multiple-values TypeError", err)
	}
}

func TestImportSurfacesHookError(t *testing.T) {
	want := errors.New("ImportError: boom")
	captureImporter(t, nil, want)
	_, err := Import([]objects.Object{objects.NewStr("a")}, nil)
	if !errors.Is(err, want) {
		t.Fatalf("__import__ err=%v, want %v", err, want)
	}
}

func TestImportNoHookErrors(t *testing.T) {
	prev := currentImporter
	t.Cleanup(func() { SetImporter(prev) })
	SetImporter(nil)
	_, err := Import([]objects.Object{objects.NewStr("a")}, nil)
	if err == nil || !strings.Contains(err.Error(), "__import__ not configured") {
		t.Fatalf("__import__: err=%v, want not-configured ImportError", err)
	}
}

func TestImportRegisteredInDict(t *testing.T) {
	dict, err := Init(nil)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	got, err := dict.GetItem(objects.NewStr("__import__"))
	if err != nil {
		t.Fatalf("dict[__import__]: %v", err)
	}
	if got == nil {
		t.Fatal("dict has no __import__")
	}
}
