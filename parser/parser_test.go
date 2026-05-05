package parser

import (
	"strings"
	"testing"

	"github.com/tamnd/gopy/ast"
)

func TestParseStringAssign(t *testing.T) {
	mod, err := ParseString("x = 1\n", "x.py", ModeFile)
	if err != nil {
		t.Fatalf("ParseString: %v", err)
	}
	m, ok := mod.(*ast.Module)
	if !ok {
		t.Fatalf("got %T, want *ast.Module", mod)
	}
	if len(m.Body) != 1 {
		t.Fatalf("Body len = %d, want 1", len(m.Body))
	}
	if _, ok := m.Body[0].(*ast.Assign); !ok {
		t.Errorf("Body[0] = %T, want *ast.Assign", m.Body[0])
	}
}

func TestParseReader(t *testing.T) {
	mod, err := Parse(strings.NewReader("y\n"), "x.py", ModeEval)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if _, ok := mod.(*ast.Expression); !ok {
		t.Errorf("got %T, want *ast.Expression", mod)
	}
}
