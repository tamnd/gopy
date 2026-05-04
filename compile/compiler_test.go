package compile

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tamnd/gopy/ast"
)

// TestCompileEmptyModule runs the full pipeline on an empty module
// and asserts the resulting Code object carries the LOAD_CONST None /
// RETURN_VALUE epilogue every module needs.
func TestCompileEmptyModule(t *testing.T) {
	co, err := Compile(module(), "<test>", 0)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	want := []byte{byte(LOAD_CONST), 0, byte(RETURN_VALUE), 0}
	if !bytes.Equal(co.Code, want) {
		t.Errorf("co.Code = %v, want %v", co.Code, want)
	}
	if len(co.Consts) != 1 || co.Consts[0] != nil {
		t.Errorf("co.Consts = %v, want [nil]", co.Consts)
	}
	if co.Stacksize < 1 {
		t.Errorf("co.Stacksize = %d, want >= 1", co.Stacksize)
	}
}

// TestCompileSimpleAssign runs `x = 1` end-to-end and asserts the
// disassembly contains the expected sequence.
func TestCompileSimpleAssign(t *testing.T) {
	body := &ast.Assign{Targets: []ast.Expr{nameStore("x")}, Value: cnst(int64(1))}
	co, err := Compile(module(body), "<test>", 0)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	dis := Disassemble(co)
	for _, want := range []string{"LOAD_CONST", "STORE_NAME", "RETURN_VALUE"} {
		if !strings.Contains(dis, want) {
			t.Errorf("disasm missing %s:\n%s", want, dis)
		}
	}
	if len(co.Names) != 1 || co.Names[0] != "x" {
		t.Errorf("co.Names = %v, want [x]", co.Names)
	}
}
