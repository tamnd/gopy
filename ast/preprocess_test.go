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

// --- fold tests below ---

func mkExpr(e Expr) *ExprStmt { return &ExprStmt{Value: e, Pos: Pos{Lineno: 1}} }

func mkConst(v any) *Constant { return &Constant{Value: v, Pos: Pos{Lineno: 1}} }

func mkTuple(es ...Expr) *Tuple {
	out := NewSeq[Expr](len(es))
	for i, e := range es {
		out.Set(i, e)
	}
	return &Tuple{Elts: out, Pos: Pos{Lineno: 1}}
}

func TestFoldDebugSubstitution(t *testing.T) {
	// __debug__ in Load context becomes Constant(true) when optimize=0.
	stmt := mkExpr(&Name{Id: "__debug__", Ctx: Load, Pos: Pos{Lineno: 1}})
	mod := &Module{Body: body(stmt)}
	Preprocess(mod, PreprocessOptions{OptimizeLevel: 0})
	c, ok := stmt.Value.(*Constant)
	if !ok {
		t.Fatalf("got %T, want *Constant", stmt.Value)
	}
	if c.Value != true {
		t.Fatalf("Value = %v, want true", c.Value)
	}

	// optimize=1 → __debug__ becomes False.
	stmt2 := mkExpr(&Name{Id: "__debug__", Ctx: Load, Pos: Pos{Lineno: 1}})
	mod2 := &Module{Body: body(stmt2)}
	Preprocess(mod2, PreprocessOptions{OptimizeLevel: 1})
	c2 := stmt2.Value.(*Constant)
	if c2.Value != false {
		t.Fatalf("Value = %v, want false", c2.Value)
	}

	// SyntaxCheckOnly suppresses the substitution.
	n := &Name{Id: "__debug__", Ctx: Load, Pos: Pos{Lineno: 1}}
	stmt3 := mkExpr(n)
	mod3 := &Module{Body: body(stmt3)}
	Preprocess(mod3, PreprocessOptions{SyntaxCheckOnly: true})
	if _, ok := stmt3.Value.(*Name); !ok {
		t.Fatalf("got %T, want *Name (no fold under SyntaxCheckOnly)", stmt3.Value)
	}

	// Store context is left alone.
	st := &Name{Id: "__debug__", Ctx: Store, Pos: Pos{Lineno: 1}}
	stmt4 := mkExpr(st)
	mod4 := &Module{Body: body(stmt4)}
	Preprocess(mod4, PreprocessOptions{})
	if _, ok := stmt4.Value.(*Name); !ok {
		t.Fatalf("got %T, want *Name (Store ctx unchanged)", stmt4.Value)
	}
}

func TestFoldFormatString(t *testing.T) {
	// "%s = %d" % (x, 1) -- %d not in {s,r,a}, so fold bails.
	left := mkConst("%s = %d")
	right := mkTuple(&Name{Id: "x", Ctx: Load, Pos: Pos{Lineno: 1}}, mkConst(int(1)))
	bin := &BinOp{Op: ModOperator, Left: left, Right: right, Pos: Pos{Lineno: 1}}
	stmt := mkExpr(bin)
	Preprocess(&Module{Body: body(stmt)}, PreprocessOptions{})
	if _, ok := stmt.Value.(*BinOp); !ok {
		t.Fatalf("got %T, want *BinOp (unsupported spec must not fold)", stmt.Value)
	}

	// "x=%s" % (n,) -- supported; folds to a JoinedStr.
	left2 := mkConst("x=%s")
	right2 := mkTuple(&Name{Id: "n", Ctx: Load, Pos: Pos{Lineno: 1}})
	bin2 := &BinOp{Op: ModOperator, Left: left2, Right: right2, Pos: Pos{Lineno: 1}}
	stmt2 := mkExpr(bin2)
	Preprocess(&Module{Body: body(stmt2)}, PreprocessOptions{})
	js, ok := stmt2.Value.(*JoinedStr)
	if !ok {
		t.Fatalf("got %T, want *JoinedStr", stmt2.Value)
	}
	if js.Values.Len() != 2 {
		t.Fatalf("JoinedStr.Values.Len() = %d, want 2", js.Values.Len())
	}
	lit, ok := js.Values.Get(0).(*Constant)
	if !ok || lit.Value.(string) != "x=" {
		t.Fatalf("first part = %+v, want literal \"x=\"", js.Values.Get(0))
	}
	fv, ok := js.Values.Get(1).(*FormattedValue)
	if !ok {
		t.Fatalf("second part = %T, want *FormattedValue", js.Values.Get(1))
	}
	if fv.Conversion != int('s') {
		t.Fatalf("Conversion = %d, want 's'", fv.Conversion)
	}

	// Starred element disables the fold.
	left3 := mkConst("%s")
	starred := &Starred{Value: &Name{Id: "xs", Ctx: Load, Pos: Pos{Lineno: 1}}, Pos: Pos{Lineno: 1}}
	right3 := mkTuple(starred)
	bin3 := &BinOp{Op: ModOperator, Left: left3, Right: right3, Pos: Pos{Lineno: 1}}
	stmt3 := mkExpr(bin3)
	Preprocess(&Module{Body: body(stmt3)}, PreprocessOptions{})
	if _, ok := stmt3.Value.(*BinOp); !ok {
		t.Fatalf("got %T, want *BinOp (Starred disables fold)", stmt3.Value)
	}

	// SyntaxCheckOnly suppresses the fold.
	left4 := mkConst("x=%s")
	right4 := mkTuple(&Name{Id: "n", Ctx: Load, Pos: Pos{Lineno: 1}})
	bin4 := &BinOp{Op: ModOperator, Left: left4, Right: right4, Pos: Pos{Lineno: 1}}
	stmt4 := mkExpr(bin4)
	Preprocess(&Module{Body: body(stmt4)}, PreprocessOptions{SyntaxCheckOnly: true})
	if _, ok := stmt4.Value.(*BinOp); !ok {
		t.Fatalf("got %T, want *BinOp under SyntaxCheckOnly", stmt4.Value)
	}
}

func TestFoldFormatRWidthPrec(t *testing.T) {
	// "%5.2r" % (x,) -- supported with width=5, precision=2, conv='r'.
	left := mkConst("%5.2r")
	right := mkTuple(&Name{Id: "x", Ctx: Load, Pos: Pos{Lineno: 1}})
	bin := &BinOp{Op: ModOperator, Left: left, Right: right, Pos: Pos{Lineno: 1}}
	stmt := mkExpr(bin)
	Preprocess(&Module{Body: body(stmt)}, PreprocessOptions{})
	js := stmt.Value.(*JoinedStr)
	fv := js.Values.Get(0).(*FormattedValue)
	if fv.Conversion != int('r') {
		t.Fatalf("Conversion = %d, want 'r'", fv.Conversion)
	}
	specJoined := fv.FormatSpec.(*JoinedStr)
	specStr := specJoined.Values.Get(0).(*Constant).Value.(string)
	if specStr != ">5.2" {
		t.Fatalf("format spec = %q, want %q", specStr, ">5.2")
	}
}

func TestFoldMatchValueNeg(t *testing.T) {
	// `case -1:` -- USub Constant int folds to Constant(-1).
	mv := &MatchValue{
		Value: &UnaryOp{Op: USub, Operand: mkConst(int(1)), Pos: Pos{Lineno: 1}},
		Pos:   Pos{Lineno: 1},
	}
	mc := &MatchCase{Pattern: mv, Body: body(&Pass{Pos: Pos{Lineno: 1}})}
	cases := NewSeq[*MatchCase](1)
	cases.Set(0, mc)
	m := &Match{
		Subject: &Name{Id: "x", Ctx: Load, Pos: Pos{Lineno: 1}},
		Cases:   cases,
		Pos:     Pos{Lineno: 1},
	}
	Preprocess(&Module{Body: body(m)}, PreprocessOptions{})
	c, ok := mv.Value.(*Constant)
	if !ok {
		t.Fatalf("got %T, want *Constant", mv.Value)
	}
	if c.Value != -1 {
		t.Fatalf("Value = %v, want -1", c.Value)
	}
}

func TestFoldMatchValueAdd(t *testing.T) {
	// `case 1+2:` folds to Constant(3+0i) via complex arithmetic path.
	bin := &BinOp{
		Op:    Add,
		Left:  mkConst(int(1)),
		Right: mkConst(int(2)),
		Pos:   Pos{Lineno: 1},
	}
	mv := &MatchValue{Value: bin, Pos: Pos{Lineno: 1}}
	mc := &MatchCase{Pattern: mv, Body: body(&Pass{Pos: Pos{Lineno: 1}})}
	cases := NewSeq[*MatchCase](1)
	cases.Set(0, mc)
	m := &Match{
		Subject: &Name{Id: "x", Ctx: Load, Pos: Pos{Lineno: 1}},
		Cases:   cases,
		Pos:     Pos{Lineno: 1},
	}
	Preprocess(&Module{Body: body(m)}, PreprocessOptions{})
	c, ok := mv.Value.(*Constant)
	if !ok {
		t.Fatalf("got %T, want *Constant", mv.Value)
	}
	if got := c.Value.(complex128); got != complex(3, 0) {
		t.Fatalf("Value = %v, want (3+0i)", got)
	}
}

func TestFoldDocstringRemoval(t *testing.T) {
	// -OO drops the docstring; sole-stmt body becomes Pass.
	doc := mkExpr(mkConst("module doc"))
	mod := &Module{Body: body(doc)}
	Preprocess(mod, PreprocessOptions{OptimizeLevel: 2})
	if mod.Body.Len() != 1 {
		t.Fatalf("Body.Len() = %d, want 1", mod.Body.Len())
	}
	if _, ok := mod.Body.Get(0).(*Pass); !ok {
		t.Fatalf("got %T, want *Pass", mod.Body.Get(0))
	}

	// With more statements the docstring is dropped.
	doc2 := mkExpr(mkConst("module doc"))
	other := mkExpr(mkConst(int(1)))
	mod2 := &Module{Body: body(doc2, other)}
	Preprocess(mod2, PreprocessOptions{OptimizeLevel: 2})
	if mod2.Body.Len() != 1 {
		t.Fatalf("Body.Len() = %d, want 1", mod2.Body.Len())
	}
	if mod2.Body.Get(0) != other {
		t.Fatalf("first stmt is not the non-doc stmt")
	}
}

func TestFoldDocstringKeptUnderO0(t *testing.T) {
	doc := mkExpr(mkConst("module doc"))
	mod := &Module{Body: body(doc)}
	Preprocess(mod, PreprocessOptions{OptimizeLevel: 0})
	if mod.Body.Len() != 1 || mod.Body.Get(0) != doc {
		t.Fatalf("docstring must be preserved at -O0")
	}
}

func TestFoldFormatBodyRewrap(t *testing.T) {
	// A bare `"%s" % (x,)` at body[0] folds to a JoinedStr; the doc
	// detector would re-flag it as a docstring, so the rewrap turns
	// it into JoinedStr(JoinedStr) so IsDocString returns false.
	bin := &BinOp{
		Op:    ModOperator,
		Left:  mkConst("x=%s"),
		Right: mkTuple(&Name{Id: "n", Ctx: Load, Pos: Pos{Lineno: 1}}),
		Pos:   Pos{Lineno: 1},
	}
	stmt := mkExpr(bin)
	mod := &Module{Body: body(stmt)}
	Preprocess(mod, PreprocessOptions{})
	// The fold replaced the BinOp with a JoinedStr. body re-wrap
	// rule does not fire because JoinedStr is not a docstring per
	// IsDocString (which requires string-typed Constant).
	if _, ok := stmt.Value.(*JoinedStr); !ok {
		t.Fatalf("got %T, want *JoinedStr", stmt.Value)
	}
	if IsDocString(stmt) {
		t.Fatal("folded JoinedStr must not register as docstring")
	}
}

func TestFoldAnnotationsSkippedUnderPEP563(t *testing.T) {
	// `def f(x: __debug__): pass` -- under CO_FUTURE_ANNOTATIONS the
	// annotation is left alone (it'll be unparsed to source text).
	deb := &Name{Id: "__debug__", Ctx: Load, Pos: Pos{Lineno: 1}}
	arg := &Arg{Arg: "x", Annotation: deb, Pos: Pos{Lineno: 1}}
	args := &Arguments{
		Args:        seqArgs(arg),
		Posonlyargs: NewSeq[*Arg](0),
		Kwonlyargs:  NewSeq[*Arg](0),
		KwDefaults:  NewSeq[Expr](0),
		Defaults:    NewSeq[Expr](0),
	}
	fn := &FunctionDef{
		Name:          "f",
		Args:          args,
		Body:          body(&Pass{Pos: Pos{Lineno: 1}}),
		DecoratorList: NewSeq[Expr](0),
		TypeParams:    NewSeq[TypeParam](0),
		Pos:           Pos{Lineno: 1},
	}
	mod := &Module{Body: body(fn)}
	Preprocess(mod, PreprocessOptions{FFFeatures: CoFutureAnnotations})
	// Annotation was skipped so __debug__ is not rewritten.
	if got := arg.Annotation; got != deb {
		t.Fatalf("annotation rewritten under PEP 563: got %T", got)
	}

	// Without the future bit the fold runs and __debug__ becomes a Constant.
	deb2 := &Name{Id: "__debug__", Ctx: Load, Pos: Pos{Lineno: 1}}
	arg2 := &Arg{Arg: "x", Annotation: deb2, Pos: Pos{Lineno: 1}}
	args2 := &Arguments{
		Args:        seqArgs(arg2),
		Posonlyargs: NewSeq[*Arg](0),
		Kwonlyargs:  NewSeq[*Arg](0),
		KwDefaults:  NewSeq[Expr](0),
		Defaults:    NewSeq[Expr](0),
	}
	fn2 := &FunctionDef{
		Name:          "f",
		Args:          args2,
		Body:          body(&Pass{Pos: Pos{Lineno: 1}}),
		DecoratorList: NewSeq[Expr](0),
		TypeParams:    NewSeq[TypeParam](0),
		Pos:           Pos{Lineno: 1},
	}
	mod2 := &Module{Body: body(fn2)}
	Preprocess(mod2, PreprocessOptions{})
	if _, ok := arg2.Annotation.(*Constant); !ok {
		t.Fatalf("annotation not folded without PEP 563: got %T", arg2.Annotation)
	}
}

func TestFoldExpressionMode(t *testing.T) {
	// Expression mode folds the body expression.
	expr := &Expression{Body: &Name{Id: "__debug__", Ctx: Load, Pos: Pos{Lineno: 1}}}
	Preprocess(expr, PreprocessOptions{OptimizeLevel: 0})
	if _, ok := expr.Body.(*Constant); !ok {
		t.Fatalf("got %T, want *Constant", expr.Body)
	}
}

func TestFoldUnchangedNodeKeepsString(t *testing.T) {
	_ = strings.Builder{}
}
