package compile

import (
	"testing"

	"github.com/tamnd/gopy/ast"
	"github.com/tamnd/gopy/symtable"
)

// helpers --------------------------------------------------------------

func nameLoad(id string) *ast.Name  { return &ast.Name{Id: id, Ctx: ast.Load} }
func nameStore(id string) *ast.Name { return &ast.Name{Id: id, Ctx: ast.Store} }
func cnst(v any) *ast.Constant      { return &ast.Constant{Value: v} }

func module(body ...ast.Stmt) *ast.Module {
	return &ast.Module{Body: body}
}

func compileMod(t *testing.T, m ast.Mod) *Unit {
	t.Helper()
	st := mustBuildSym(t, m)
	c := NewCompiler("<test>", 0, nil, st)
	u, err := c.Codegen(st.Top, m)
	if err != nil {
		t.Fatalf("Codegen: %v", err)
	}
	return u
}

func mustBuildSym(t *testing.T, m ast.Mod) *symtable.Table {
	t.Helper()
	st, err := symtable.Build(m, "<test>", nil)
	if err != nil {
		t.Fatalf("symtable.Build: %v", err)
	}
	return st
}

// opNames lists the opcode names emitted into u.Seq. For module-scope
// units it skips the leading RESUME prologue (emitted by Codegen to
// mirror CPython's codegen_enter_scope) so existing want lists, which
// were authored before the prologue landed, keep matching the body.
// Inner function / class / comprehension Units already had their own
// RESUME emission before the prologue port and their tests bake it in,
// so we leave those alone.
func opNames(u *Unit) []string {
	var instrs []Instr
	// Annotation stash code (prepended by cfgFromSequence at compile time).
	if u.Seq.AnnoCode != nil {
		instrs = append(instrs, u.Seq.AnnoCode.Instrs...)
	}
	body := u.Seq.Instrs
	if u.ScopeType == symtable.ModuleBlock && len(body) > 0 && body[0].Op == RESUME {
		body = body[1:]
	}
	// Codegen plants ANNOTATIONS_PLACEHOLDER right after the module RESUME
	// (cfgFromSequence later splices the __annotate__ stash in at it, or
	// drops it). Skip it too so body-focused want lists keep matching.
	if u.ScopeType == symtable.ModuleBlock && len(body) > 0 && body[0].Op == ANNOTATIONS_PLACEHOLDER {
		body = body[1:]
	}
	instrs = append(instrs, body...)
	out := make([]string, len(instrs))
	for i, in := range instrs {
		out[i] = in.Op.Name()
	}
	return out
}

// tests ---------------------------------------------------------------

func TestEmptyModuleEmitsReturnNone(t *testing.T) {
	u := compileMod(t, module())
	want := []string{"LOAD_CONST", "RETURN_VALUE"}
	got := opNames(u)
	if !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
	if len(u.Consts) != 1 || u.Consts[0] != nil {
		t.Errorf("consts = %v, want [nil]", u.Consts)
	}
}

func TestPassEmitsNopThenReturn(t *testing.T) {
	u := compileMod(t, module(&ast.Pass{}))
	want := []string{"NOP", "LOAD_CONST", "RETURN_VALUE"}
	if got := opNames(u); !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
}

func TestExprStmtPopsValue(t *testing.T) {
	// 1
	u := compileMod(t, module(&ast.ExprStmt{Value: cnst(int64(1))}))
	want := []string{"LOAD_CONST", "POP_TOP", "LOAD_CONST", "RETURN_VALUE"}
	if got := opNames(u); !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
}

func TestModuleAssignEmitsStoreName(t *testing.T) {
	// x = 1
	a := &ast.Assign{Targets: []ast.Expr{nameStore("x")}, Value: cnst(int64(1))}
	u := compileMod(t, module(a))
	want := []string{"LOAD_CONST", "STORE_NAME", "LOAD_CONST", "RETURN_VALUE"}
	if got := opNames(u); !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
	if len(u.Names) != 1 || u.Names[0] != "x" {
		t.Errorf("names = %v, want [x]", u.Names)
	}
}

func TestModuleNameLoadEmitsLoadName(t *testing.T) {
	// x = 1; x
	body := []ast.Stmt{
		&ast.Assign{Targets: []ast.Expr{nameStore("x")}, Value: cnst(int64(1))},
		&ast.ExprStmt{Value: nameLoad("x")},
	}
	u := compileMod(t, module(body...))
	want := []string{
		"LOAD_CONST", "STORE_NAME",
		"LOAD_NAME", "POP_TOP",
		"LOAD_CONST", "RETURN_VALUE",
	}
	if got := opNames(u); !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
}

func TestConstDedup(t *testing.T) {
	// 1; 1; 2  -> only two constants in the pool (1 and 2), plus the
	// trailing None for the implicit return.
	body := []ast.Stmt{
		&ast.ExprStmt{Value: cnst(int64(1))},
		&ast.ExprStmt{Value: cnst(int64(1))},
		&ast.ExprStmt{Value: cnst(int64(2))},
	}
	u := compileMod(t, module(body...))
	if len(u.Consts) != 3 {
		t.Errorf("consts = %v, want 3 entries (1, 2, nil)", u.Consts)
	}
	// First two ExprStmt hit the same const slot.
	if u.Seq.Instrs[0].Oparg != u.Seq.Instrs[2].Oparg {
		t.Errorf("expected first two LOAD_CONSTs to share an index, got %d and %d",
			u.Seq.Instrs[0].Oparg, u.Seq.Instrs[2].Oparg)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
