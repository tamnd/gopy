package pegen

import (
	"testing"

	"github.com/tamnd/gopy/ast"
)

func TestJoinedStrFoldsAdjacentStrings(t *testing.T) {
	js := JoinedStrFromValues([]ast.Expr{
		&ast.Constant{Value: "a"},
		&ast.Constant{Value: "b"},
		&ast.FormattedValue{Value: &ast.Name{Id: "x"}, Conversion: ConvNone},
		&ast.Constant{Value: "c"},
	})
	if len(js.Values) != 3 {
		t.Fatalf("Values = %d, want 3", len(js.Values))
	}
	first, ok := js.Values[0].(*ast.Constant)
	if !ok || first.Value.(string) != "ab" {
		t.Errorf("first = %+v", js.Values[0])
	}
}

func TestFormattedValueFor(t *testing.T) {
	fv := FormattedValueFor(&ast.Name{Id: "x"}, ConvRepr, &ast.Constant{Value: ".2f"})
	if fv.Conversion != int('r') {
		t.Errorf("conversion = %d", fv.Conversion)
	}
	if fv.FormatSpec == nil {
		t.Errorf("format spec dropped")
	}
}

func TestDebugLiteral(t *testing.T) {
	if DebugLiteral("x + 1").Value.(string) != "x + 1=" {
		t.Errorf("debug literal = %v", DebugLiteral("x + 1").Value)
	}
}

func TestTemplateStrFromValues(t *testing.T) {
	ts := TemplateStrFromValues([]ast.Expr{&ast.Constant{Value: "a"}, &ast.Constant{Value: "b"}})
	if len(ts.Values) != 1 {
		t.Errorf("expected fold to one Constant, got %d", len(ts.Values))
	}
}
