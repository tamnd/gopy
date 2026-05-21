package builtins

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/tamnd/gopy/objects"
)

// fileFromTemp returns an *objects.File wrapping a fresh temp file in
// write mode. The caller is responsible for closing + reading the
// captured bytes.
func fileFromTemp(t *testing.T) (*objects.File, *os.File) {
	t.Helper()
	tmp, err := os.CreateTemp(t.TempDir(), "print")
	if err != nil {
		t.Fatalf("temp file: %v", err)
	}
	pf := objects.NewFile(tmp, tmp.Name(), "w", false, false, true)
	return pf, tmp
}

func readBack(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	return string(b)
}

func TestPrintPositional(t *testing.T) {
	pf, tmp := fileFromTemp(t)
	defer tmp.Close()
	out, err := Print([]objects.Object{
		objects.NewInt(1),
		objects.NewInt(2),
		objects.NewInt(3),
	}, map[string]objects.Object{"file": pf})
	if err != nil {
		t.Fatalf("Print: %v", err)
	}
	if !objects.IsNone(out) {
		t.Errorf("Print returned %v, want None", out)
	}
	_ = pf.Flush()
	_ = tmp.Sync()
	if got := readBack(t, tmp.Name()); got != "1 2 3\n" {
		t.Errorf("output = %q, want %q", got, "1 2 3\n")
	}
}

func TestPrintSepEnd(t *testing.T) {
	pf, tmp := fileFromTemp(t)
	defer tmp.Close()
	_, err := Print(
		[]objects.Object{objects.NewInt(1), objects.NewInt(2)},
		map[string]objects.Object{
			"file": pf,
			"sep":  objects.NewStr("-"),
			"end":  objects.NewStr("!"),
		},
	)
	if err != nil {
		t.Fatalf("Print: %v", err)
	}
	_ = pf.Flush()
	_ = tmp.Sync()
	if got := readBack(t, tmp.Name()); got != "1-2!" {
		t.Errorf("output = %q, want %q", got, "1-2!")
	}
}

func TestPrintNoneSepRetainsDefault(t *testing.T) {
	pf, tmp := fileFromTemp(t)
	defer tmp.Close()
	_, err := Print(
		[]objects.Object{objects.NewInt(1), objects.NewInt(2)},
		map[string]objects.Object{"file": pf, "sep": objects.None()},
	)
	if err != nil {
		t.Fatalf("Print: %v", err)
	}
	_ = pf.Flush()
	_ = tmp.Sync()
	if got := readBack(t, tmp.Name()); got != "1 2\n" {
		t.Errorf("output = %q, want %q (None sep means default space)", got, "1 2\n")
	}
}

func TestPrintRejectsNonStringSep(t *testing.T) {
	pf, tmp := fileFromTemp(t)
	defer tmp.Close()
	_, err := Print(
		[]objects.Object{objects.NewInt(1)},
		map[string]objects.Object{"file": pf, "sep": objects.NewInt(0)},
	)
	if err == nil {
		t.Fatalf("Print accepted non-string sep")
	}
	if !strings.Contains(err.Error(), "sep must be None or a string") {
		t.Errorf("err = %v, want TypeError about sep", err)
	}
}

func TestInitPopulatesBuiltins(t *testing.T) {
	var buf bytes.Buffer
	dict, err := Init(&buf)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	for _, name := range []string{"None", "True", "False", "NotImplemented", "print"} {
		v, err := dict.GetItem(objects.NewStr(name))
		if err != nil || v == nil {
			t.Errorf("dict missing %q: %v", name, err)
		}
	}
	pr, _ := dict.GetItem(objects.NewStr("print"))
	if _, ok := pr.(*objects.BuiltinFunction); !ok {
		t.Errorf("print = %T, want *objects.BuiltinFunction", pr)
	}
}
