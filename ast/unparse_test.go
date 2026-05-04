package ast

import (
	"math"
	"math/big"
	"testing"
)

func name(s string) *Name { return &Name{Id: s, Pos: Pos{Lineno: 1}} }

func cint(v int) *Constant { return &Constant{Value: v, Pos: Pos{Lineno: 1}} }

func cstr(v string) *Constant { return &Constant{Value: v, Pos: Pos{Lineno: 1}} }

func mustUnparse(t *testing.T, e Expr) string {
	t.Helper()
	got, err := Unparse(e)
	if err != nil {
		t.Fatalf("Unparse: %v", err)
	}
	return got
}

func seqExpr(es ...Expr) Seq[Expr] {
	out := NewSeq[Expr](len(es))
	for i, e := range es {
		out.Set(i, e)
	}
	return out
}

func TestUnparseSimple(t *testing.T) {
	cases := []struct {
		name string
		expr Expr
		want string
	}{
		{"name", name("x"), "x"},
		{"int", cint(42), "42"},
		{"none", &Constant{Value: nil}, "None"},
		{"true", &Constant{Value: true}, "True"},
		{"false", &Constant{Value: false}, "False"},
		{"ellipsis", &Constant{Value: Ellipsis}, "..."},
		{"str", cstr("hi"), `"hi"`},
		{"bytes", &Constant{Value: []byte("hi")}, `b"hi"`},
		{"bigint", &Constant{Value: big.NewInt(99)}, "99"},
		{"float", &Constant{Value: 1.5}, "1.5"},
		{"float_whole", &Constant{Value: 2.0}, "2.0"},
		{"float_inf", &Constant{Value: math.Inf(1)}, "1e309"},
		{"float_neg_inf", &Constant{Value: math.Inf(-1)}, "-1e309"},
		{"complex_pure", &Constant{Value: complex(0, 1)}, "1.0j"},
		{"complex_full", &Constant{Value: complex(1, 2)}, "(1.0+2.0j)"},
		{"complex_neg_im", &Constant{Value: complex(1, -2)}, "(1.0-2.0j)"},
		{"empty_tuple_const", &Constant{Value: []any{}}, "()"},
		{"singleton_tuple_const", &Constant{Value: []any{int(1)}}, "(1,)"},
		{"frozenset_const", &Constant{Value: FrozenSet{int(1), int(2)}}, "frozenset({1, 2})"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mustUnparse(t, tc.expr); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestUnparseBoolOp(t *testing.T) {
	e := &BoolOp{Op: And, Values: seqExpr(name("a"), name("b"), name("c"))}
	if got, want := mustUnparse(t, e), "a and b and c"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	e2 := &BoolOp{Op: Or, Values: seqExpr(name("a"), name("b"))}
	if got, want := mustUnparse(t, e2), "a or b"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestUnparseBinOpPrecedence(t *testing.T) {
	// (1 + 2) * 3 needs parens around left.
	add := &BinOp{Op: Add, Left: cint(1), Right: cint(2)}
	mul := &BinOp{Op: Mult, Left: add, Right: cint(3)}
	if got, want := mustUnparse(t, mul), "(1 + 2) * 3"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	// 1 + 2 * 3 -- no parens needed
	mul2 := &BinOp{Op: Mult, Left: cint(2), Right: cint(3)}
	add2 := &BinOp{Op: Add, Left: cint(1), Right: mul2}
	if got, want := mustUnparse(t, add2), "1 + 2 * 3"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestUnparsePowRightAssoc(t *testing.T) {
	// 2 ** 3 ** 4  is 2 ** (3 ** 4) -- right side does not need parens.
	inner := &BinOp{Op: Pow, Left: cint(3), Right: cint(4)}
	outer := &BinOp{Op: Pow, Left: cint(2), Right: inner}
	if got, want := mustUnparse(t, outer), "2 ** 3 ** 4"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	// (2 ** 3) ** 4  -- left side needs parens.
	inner2 := &BinOp{Op: Pow, Left: cint(2), Right: cint(3)}
	outer2 := &BinOp{Op: Pow, Left: inner2, Right: cint(4)}
	if got, want := mustUnparse(t, outer2), "(2 ** 3) ** 4"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestUnparseUnaryOps(t *testing.T) {
	cases := []struct {
		op   Unaryop
		want string
	}{
		{Invert, "~x"},
		{Not, "not x"},
		{UAdd, "+x"},
		{USub, "-x"},
	}
	for _, tc := range cases {
		e := &UnaryOp{Op: tc.op, Operand: name("x")}
		if got := mustUnparse(t, e); got != tc.want {
			t.Fatalf("op=%v got %q, want %q", tc.op, got, tc.want)
		}
	}
}

func TestUnparseLambdaAndCall(t *testing.T) {
	args := &Arguments{
		Args:       seqArgs(&Arg{Arg: "a"}, &Arg{Arg: "b"}),
		Defaults:   seqExpr(cint(2)),
		Kwonlyargs: NewSeq[*Arg](0),
		KwDefaults: NewSeq[Expr](0),
	}
	body := &BinOp{Op: Add, Left: name("a"), Right: name("b")}
	lam := &Lambda{Args: args, Body: body}
	if got, want := mustUnparse(t, lam), "lambda a, b=2: a + b"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	keyword := "k"
	c := &Call{
		Func:     name("f"),
		Args:     seqExpr(name("x"), &Starred{Value: name("rest")}),
		Keywords: seqKeywords(&Keyword{Arg: &keyword, Value: cint(1)}, &Keyword{Value: name("kw")}),
	}
	if got, want := mustUnparse(t, c), "f(x, *rest, k=1, **kw)"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func seqArgs(as ...*Arg) Seq[*Arg] {
	out := NewSeq[*Arg](len(as))
	for i, a := range as {
		out.Set(i, a)
	}
	return out
}

func seqKeywords(ks ...*Keyword) Seq[*Keyword] {
	out := NewSeq[*Keyword](len(ks))
	for i, k := range ks {
		out.Set(i, k)
	}
	return out
}

func TestUnparseArgumentsFull(t *testing.T) {
	// def f(a, b, /, c, d=1, *args, e, f=2, **kw)
	args := &Arguments{
		Posonlyargs: seqArgs(&Arg{Arg: "a"}, &Arg{Arg: "b"}),
		Args:        seqArgs(&Arg{Arg: "c"}, &Arg{Arg: "d"}),
		Vararg:      &Arg{Arg: "args"},
		Kwonlyargs:  seqArgs(&Arg{Arg: "e"}, &Arg{Arg: "f"}),
		KwDefaults:  seqExpr(nil, cint(2)),
		Kwarg:       &Arg{Arg: "kw"},
		Defaults:    seqExpr(cint(1)),
	}
	lam := &Lambda{Args: args, Body: cint(0)}
	got := mustUnparse(t, lam)
	want := "lambda a, b, /, c, d=1, *args, e, f=2, **kw: 0"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestUnparseIfExp(t *testing.T) {
	e := &IfExp{Test: name("c"), Body: name("a"), Orelse: name("b")}
	if got, want := mustUnparse(t, e), "a if c else b"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestUnparseDictAndSet(t *testing.T) {
	d := &Dict{
		Keys:   seqExpr(cstr("a"), nil),
		Values: seqExpr(cint(1), name("rest")),
	}
	if got, want := mustUnparse(t, d), `{"a": 1, **rest}`; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	s := &Set{Elts: seqExpr(cint(1), cint(2))}
	if got, want := mustUnparse(t, s), "{1, 2}"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	empty := &Set{Elts: NewSeq[Expr](0)}
	if got, want := mustUnparse(t, empty), "set()"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func seqComp(cs ...*Comprehension) Seq[*Comprehension] {
	out := NewSeq[*Comprehension](len(cs))
	for i, c := range cs {
		out.Set(i, c)
	}
	return out
}

func TestUnparseComprehensions(t *testing.T) {
	gen := &Comprehension{
		Target:  name("x"),
		Iter:    name("xs"),
		Ifs:     seqExpr(name("p")),
		IsAsync: 0,
	}
	lc := &ListComp{Elt: name("x"), Generators: seqComp(gen)}
	if got, want := mustUnparse(t, lc), "[x for x in xs if p]"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	sc := &SetComp{Elt: name("x"), Generators: seqComp(gen)}
	if got, want := mustUnparse(t, sc), "{x for x in xs if p}"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	dc := &DictComp{Key: name("k"), Value: name("v"), Generators: seqComp(gen)}
	if got, want := mustUnparse(t, dc), "{k: v for x in xs if p}"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	ge := &GeneratorExp{Elt: name("x"), Generators: seqComp(gen)}
	if got, want := mustUnparse(t, ge), "(x for x in xs if p)"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	asyncGen := &Comprehension{Target: name("x"), Iter: name("xs"), Ifs: NewSeq[Expr](0), IsAsync: 1}
	lc2 := &ListComp{Elt: name("x"), Generators: seqComp(asyncGen)}
	if got, want := mustUnparse(t, lc2), "[x async for x in xs]"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func seqCmpop(ops ...Cmpop) Seq[Cmpop] {
	out := NewSeq[Cmpop](len(ops))
	for i, o := range ops {
		out.Set(i, o)
	}
	return out
}

func TestUnparseCompareChain(t *testing.T) {
	c := &Compare{
		Left:        name("a"),
		Ops:         seqCmpop(Lt, LtE),
		Comparators: seqExpr(name("b"), name("c")),
	}
	if got, want := mustUnparse(t, c), "a < b <= c"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	cases := []struct {
		op   Cmpop
		want string
	}{
		{Eq, "a == b"},
		{NotEq, "a != b"},
		{Is, "a is b"},
		{IsNot, "a is not b"},
		{In, "a in b"},
		{NotIn, "a not in b"},
	}
	for _, tc := range cases {
		c := &Compare{Left: name("a"), Ops: seqCmpop(tc.op), Comparators: seqExpr(name("b"))}
		if got := mustUnparse(t, c); got != tc.want {
			t.Fatalf("op=%v got %q, want %q", tc.op, got, tc.want)
		}
	}
}

func TestUnparseAwaitYield(t *testing.T) {
	if got, want := mustUnparse(t, &Await{Value: name("x")}), "await x"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got, want := mustUnparse(t, &Yield{}), "(yield)"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got, want := mustUnparse(t, &Yield{Value: name("x")}), "(yield x)"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got, want := mustUnparse(t, &YieldFrom{Value: name("xs")}), "(yield from xs)"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestUnparseAttributeIntSpace(t *testing.T) {
	// 1 .real -- need space so float parser does not eat the dot.
	a := &Attribute{Value: cint(1), Attr: "real"}
	if got, want := mustUnparse(t, a), "1 .real"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	a2 := &Attribute{Value: name("x"), Attr: "y"}
	if got, want := mustUnparse(t, a2), "x.y"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestUnparseSubscriptAndSlice(t *testing.T) {
	sub := &Subscript{Value: name("a"), Slice: cint(0)}
	if got, want := mustUnparse(t, sub), "a[0]"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	sl := &Slice{Lower: cint(1), Upper: cint(5), Step: cint(2)}
	sub2 := &Subscript{Value: name("a"), Slice: sl}
	if got, want := mustUnparse(t, sub2), "a[1:5:2]"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	sl2 := &Slice{}
	sub3 := &Subscript{Value: name("a"), Slice: sl2}
	if got, want := mustUnparse(t, sub3), "a[:]"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestUnparseListAndTuple(t *testing.T) {
	if got, want := mustUnparse(t, &List{Elts: seqExpr(cint(1), cint(2))}), "[1, 2]"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got, want := mustUnparse(t, &Tuple{Elts: NewSeq[Expr](0)}), "()"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got, want := mustUnparse(t, &Tuple{Elts: seqExpr(cint(1))}), "1,"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got, want := mustUnparse(t, &Tuple{Elts: seqExpr(cint(1), cint(2))}), "1, 2"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestUnparseStarred(t *testing.T) {
	e := &Starred{Value: name("xs")}
	if got, want := mustUnparse(t, e), "*xs"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestUnparseNamedExpr(t *testing.T) {
	// Top-level mirrors ast_unparse.c: no parens at PR_TUPLE.
	e := &NamedExpr{Target: name("x"), Value: cint(1)}
	if got, want := mustUnparse(t, e), "x := 1"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	// Inside a call argument the level rises to PR_TEST, forcing parens.
	c := &Call{Func: name("f"), Args: seqExpr(e), Keywords: NewSeq[*Keyword](0)}
	if got, want := mustUnparse(t, c), "f((x := 1))"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestUnparseFString(t *testing.T) {
	js := &JoinedStr{Values: seqExpr(
		cstr("hi "),
		&FormattedValue{Value: name("x")},
	)}
	if got, want := mustUnparse(t, js), `f"hi {x}"`; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	conv := 'r'
	js2 := &JoinedStr{Values: seqExpr(
		&FormattedValue{Value: name("x"), Conversion: int(conv)},
	)}
	if got, want := mustUnparse(t, js2), `f"{x!r}"`; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	js3 := &JoinedStr{Values: seqExpr(
		&FormattedValue{
			Value:      name("x"),
			FormatSpec: &JoinedStr{Values: seqExpr(cstr(".2f"))},
		},
	)}
	if got, want := mustUnparse(t, js3), `f"{x:.2f}"`; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestUnparseTemplateStr(t *testing.T) {
	ts := &TemplateStr{Values: seqExpr(
		cstr("hi "),
		&Interpolation{Value: name("x")},
	)}
	if got, want := mustUnparse(t, ts), `t"hi {x}"`; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestUnparseFStringEscapesBraces(t *testing.T) {
	// Literal braces in the string segment must be doubled so the
	// reparser still treats them as literals.
	js := &JoinedStr{Values: seqExpr(cstr("{n}"))}
	if got, want := mustUnparse(t, js), `f"{{n}}"`; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestUnparseUnsupported(t *testing.T) {
	if _, err := Unparse(nil); err == nil {
		t.Fatal("expected error for nil Expr")
	}
}
