package ast

import (
	"strings"
	"testing"
)

func body(stmts ...Stmt) Seq[Stmt] {
	out := NewSeq[Stmt](len(stmts))
	for i, s := range stmts {
		out.Set(i, s)
	}
	return out
}

func handlers(hs ...*ExceptHandler) Seq[Excepthandler] {
	out := NewSeq[Excepthandler](len(hs))
	for i, h := range hs {
		out.Set(i, h)
	}
	return out
}

func mkRet() *Return        { return &Return{Pos: Pos{Lineno: 1}} }
func mkBreak() *Break       { return &Break{Pos: Pos{Lineno: 1}} }
func mkContinue() *Continue { return &Continue{Pos: Pos{Lineno: 1}} }

func TestPreprocessReturnInFinally(t *testing.T) {
	mod := &Module{Body: body(&Try{
		Body:      body(),
		Handlers:  handlers(),
		Orelse:    body(),
		Finalbody: body(mkRet()),
		Pos:       Pos{Lineno: 1},
	})}
	ws := Preprocess(mod, PreprocessOptions{Filename: "t.py", EnableWarnings: true})
	if len(ws) != 1 || !strings.Contains(ws[0].Msg, "'return' in a 'finally' block") {
		t.Fatalf("warnings = %+v", ws)
	}
}

func TestPreprocessReturnInFunctionInFinallyOK(t *testing.T) {
	// Nested funcdef shadows: a `return` inside a def inside a finally
	// is fine because the enclosing function boundary is hit first.
	inner := &FunctionDef{
		Name: "inner",
		Body: body(mkRet()),
		Pos:  Pos{Lineno: 1},
	}
	tryStmt := &Try{
		Body:      body(),
		Handlers:  handlers(),
		Orelse:    body(),
		Finalbody: body(inner),
		Pos:       Pos{Lineno: 1},
	}
	mod := &Module{Body: body(&FunctionDef{
		Name: "f", Body: body(tryStmt), Pos: Pos{Lineno: 1},
	})}
	ws := Preprocess(mod, PreprocessOptions{EnableWarnings: true})
	if len(ws) != 0 {
		t.Fatalf("warnings = %+v", ws)
	}
}

func TestPreprocessBreakInFinally(t *testing.T) {
	loop := &While{
		Test: &Constant{Value: true, Pos: Pos{Lineno: 1}},
		Body: body(&Try{
			Body:      body(),
			Handlers:  handlers(),
			Orelse:    body(),
			Finalbody: body(mkBreak()),
			Pos:       Pos{Lineno: 1},
		}),
		Orelse: body(),
		Pos:    Pos{Lineno: 1},
	}
	mod := &Module{Body: body(loop)}
	ws := Preprocess(mod, PreprocessOptions{EnableWarnings: true})
	if len(ws) != 1 || !strings.Contains(ws[0].Msg, "'break' in a 'finally' block") {
		t.Fatalf("warnings = %+v", ws)
	}
}

func TestPreprocessBreakInLoopInFinallyOK(t *testing.T) {
	// A loop nested inside the finally body is its own scope; the
	// break belongs to that loop, not the outer try.
	innerLoop := &While{
		Test:   &Constant{Value: true, Pos: Pos{Lineno: 1}},
		Body:   body(mkBreak()),
		Orelse: body(),
		Pos:    Pos{Lineno: 1},
	}
	tryStmt := &Try{
		Body:      body(),
		Handlers:  handlers(),
		Orelse:    body(),
		Finalbody: body(innerLoop),
		Pos:       Pos{Lineno: 1},
	}
	mod := &Module{Body: body(tryStmt)}
	ws := Preprocess(mod, PreprocessOptions{EnableWarnings: true})
	if len(ws) != 0 {
		t.Fatalf("warnings = %+v", ws)
	}
}

func TestPreprocessContinueInFinally(t *testing.T) {
	loop := &For{
		Target: &Name{Id: "x", Pos: Pos{Lineno: 1}},
		Iter:   &Name{Id: "xs", Pos: Pos{Lineno: 1}},
		Body: body(&Try{
			Body:      body(),
			Handlers:  handlers(),
			Orelse:    body(),
			Finalbody: body(mkContinue()),
			Pos:       Pos{Lineno: 1},
		}),
		Orelse: body(),
		Pos:    Pos{Lineno: 1},
	}
	mod := &Module{Body: body(loop)}
	ws := Preprocess(mod, PreprocessOptions{EnableWarnings: true})
	if len(ws) != 1 || !strings.Contains(ws[0].Msg, "'continue' in a 'finally' block") {
		t.Fatalf("warnings = %+v", ws)
	}
}

func TestPreprocessWarningsDisabled(t *testing.T) {
	mod := &Module{Body: body(&Try{
		Body:      body(),
		Handlers:  handlers(),
		Orelse:    body(),
		Finalbody: body(mkRet()),
		Pos:       Pos{Lineno: 1},
	})}
	ws := Preprocess(mod, PreprocessOptions{EnableWarnings: false})
	if len(ws) != 0 {
		t.Fatalf("warnings = %+v", ws)
	}
}

func TestPreprocessExpressionMode(t *testing.T) {
	expr := &Expression{Body: &Constant{Value: 1, Pos: Pos{Lineno: 1}}}
	if got := Preprocess(expr, PreprocessOptions{EnableWarnings: true}); got != nil {
		t.Fatalf("warnings = %+v", got)
	}
}

func TestWarningError(t *testing.T) {
	w := Warning{Msg: "x", Filename: "f.py", Pos: Pos{Lineno: 7, ColOffset: 4}}
	if got, want := w.Error(), "f.py:7:5: SyntaxWarning: x"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}
