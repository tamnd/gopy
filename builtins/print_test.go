package builtins

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tamnd/gopy/objects"
)

func TestPrintPositional(t *testing.T) {
	var buf bytes.Buffer
	fn := Print(&buf)
	out, err := fn([]objects.Object{
		objects.NewInt(1),
		objects.NewInt(2),
		objects.NewInt(3),
	}, nil)
	if err != nil {
		t.Fatalf("Print: %v", err)
	}
	if !objects.IsNone(out) {
		t.Errorf("Print returned %v, want None", out)
	}
	if got := buf.String(); got != "1 2 3\n" {
		t.Errorf("output = %q, want %q", got, "1 2 3\n")
	}
}

func TestPrintSepEnd(t *testing.T) {
	var buf bytes.Buffer
	fn := Print(&buf)
	_, err := fn(
		[]objects.Object{objects.NewInt(1), objects.NewInt(2)},
		map[string]objects.Object{
			"sep": objects.NewStr("-"),
			"end": objects.NewStr("!"),
		},
	)
	if err != nil {
		t.Fatalf("Print: %v", err)
	}
	if got := buf.String(); got != "1-2!" {
		t.Errorf("output = %q, want %q", got, "1-2!")
	}
}

func TestPrintNoneSepRetainsDefault(t *testing.T) {
	var buf bytes.Buffer
	fn := Print(&buf)
	_, err := fn(
		[]objects.Object{objects.NewInt(1), objects.NewInt(2)},
		map[string]objects.Object{"sep": objects.None()},
	)
	if err != nil {
		t.Fatalf("Print: %v", err)
	}
	if got := buf.String(); got != "1 2\n" {
		t.Errorf("output = %q, want %q (None sep means default space)", got, "1 2\n")
	}
}

func TestPrintRejectsNonStringSep(t *testing.T) {
	var buf bytes.Buffer
	fn := Print(&buf)
	_, err := fn(
		[]objects.Object{objects.NewInt(1)},
		map[string]objects.Object{"sep": objects.NewInt(0)},
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
