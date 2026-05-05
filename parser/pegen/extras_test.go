package pegen

import (
	"testing"

	"github.com/tamnd/gopy/ast"
)

func TestKeyValueExtractors(t *testing.T) {
	pairs := []KeyValuePair{
		{Key: &ast.Name{Id: "a"}, Value: &ast.Constant{Value: 1}},
		{Key: &ast.Name{Id: "b"}, Value: &ast.Constant{Value: 2}},
	}
	keys := GetKeys(pairs)
	values := GetValues(pairs)
	if len(keys) != 2 || keys[0].(*ast.Name).Id != "a" {
		t.Errorf("GetKeys mismatch: %v", keys)
	}
	if values[1].(*ast.Constant).Value.(int) != 2 {
		t.Errorf("GetValues mismatch: %v", values)
	}
}

func TestEnsureRealImaginary(t *testing.T) {
	p := &Parser{}
	if got := p.EnsureReal(&ast.Constant{Value: 1.5}); got == nil {
		t.Errorf("real ok rejected")
	}
	if p.errorIndicator {
		t.Errorf("error indicator should not flip on real ok")
	}
	if got := p.EnsureImaginary(&ast.Constant{Value: complex(0, 2)}); got == nil {
		t.Errorf("imaginary ok rejected")
	}
	p2 := &Parser{}
	if got := p2.EnsureReal(&ast.Constant{Value: complex(1, 1)}); got != nil {
		t.Errorf("complex passed as real")
	}
	if !p2.errorIndicator {
		t.Errorf("error indicator should flip")
	}
}

func TestConcatenateStringsPlain(t *testing.T) {
	parts := []ast.Expr{
		&ast.Constant{Value: "ab"},
		&ast.Constant{Value: "cd"},
	}
	got := ConcatenateStrings(parts).(*ast.Constant)
	if got.Value.(string) != "abcd" {
		t.Errorf("concat = %q", got.Value)
	}
}

func TestConcatenateStringsMixedFallsBack(t *testing.T) {
	parts := []ast.Expr{
		&ast.Constant{Value: "ab"},
		&ast.JoinedStr{},
	}
	got := ConcatenateStrings(parts)
	if _, ok := got.(*ast.JoinedStr); !ok {
		t.Errorf("expected JoinedStr fallback, got %T", got)
	}
}

func TestFunctionDefDecorators(t *testing.T) {
	fn := &ast.FunctionDef{Name: "f"}
	dec := []ast.Expr{&ast.Name{Id: "d"}}
	out := FunctionDefDecorators(dec, fn).(*ast.FunctionDef)
	if len(out.DecoratorList) != 1 {
		t.Errorf("decorators not stamped: %v", out.DecoratorList)
	}
}
