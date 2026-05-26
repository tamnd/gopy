// Port of cpython/Python/ast_unparse.c. All visitor methods on
// `*unparser` correspond to the matching `append_ast_*` routine in
// the C file (e.g. `(*unparser).binop` ports `append_ast_binop`).
// The `ws/wc/wcond/sep` helpers are Go-only string-builder shims with
// no individual CPython analog.

package ast

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
)

// Unparse renders an expression back to source text. Mirrors the
// limited unparser in cpython/Python/ast_unparse.c which CPython uses
// to stringize PEP 563/649 annotations during compilation.
//
// CPython: Python/ast_unparse.c expr_as_unicode
func Unparse(e Expr) (string, error) {
	var b strings.Builder
	w := unparser{w: &b}
	// CPython: Python/ast_unparse.c _PyAST_ExprAsUnicode uses PR_TEST as the
	// starting level so top-level tuples and generators get parenthesized.
	if err := w.expr(e, prTest); err != nil {
		return "", err
	}
	return b.String(), nil
}

// Operator precedence levels mirror the enum at the head of
// ast_unparse.c. Higher binds tighter.
//
// Note: in C, `PR_BOR = PR_EXPR` is followed by an implicit `PR_EXPR+1`
// for the next entry. Go's iota does not work that way: an implicit
// initializer copies the entire previous expression, so chaining
// iota-relative entries after an alias drops the counter. The block
// below is laid out so the iota progression continues correctly, with
// prBor declared as a separate alias afterwards.
const (
	prTuple = iota
	prTest
	prOr
	prAnd
	prNot
	prCmp
	prExpr
	prBxor
	prBand
	prShift
	prArith
	prTerm
	prFactor
	prPower
	prAwait
	prAtom
)

const prBor = prExpr

type unparser struct {
	w *strings.Builder
}

func (u *unparser) ws(s string) { u.w.WriteString(s) }
func (u *unparser) wc(c byte)   { u.w.WriteByte(c) }
func (u *unparser) wcond(b bool, s string) {
	if b {
		u.w.WriteString(s)
	}
}

// expr is the top-level dispatch over expression node kinds.
//
// CPython: Python/ast_unparse.c append_ast_expr
func (u *unparser) expr(e Expr, level int) error {
	if handled, err := u.exprOps(e, level); handled {
		return err
	}
	if handled, err := u.exprComps(e, level); handled {
		return err
	}
	if handled, err := u.exprAtoms(e, level); handled {
		return err
	}
	if handled, err := u.exprStrings(e); handled {
		return err
	}
	return fmt.Errorf("unparse: unsupported expr %T", e)
}

// exprOps handles operator-shaped expressions. The boolean signals
// whether the dispatch matched so the caller can fall through.
func (u *unparser) exprOps(e Expr, level int) (bool, error) {
	switch n := e.(type) {
	case *BoolOp:
		return true, u.boolop(n, level)
	case *NamedExpr:
		return true, u.namedexpr(n, level)
	case *BinOp:
		return true, u.binop(n, level)
	case *UnaryOp:
		return true, u.unaryop(n, level)
	case *Compare:
		return true, u.compare(n, level)
	case *IfExp:
		return true, u.ifexp(n, level)
	case *Lambda:
		return true, u.lambda(n, level)
	}
	return false, nil
}

// exprComps handles comprehensions, await/yield, and call.
func (u *unparser) exprComps(e Expr, level int) (bool, error) {
	switch n := e.(type) {
	case *Dict:
		return true, u.dict(n)
	case *Set:
		return true, u.set(n)
	case *ListComp:
		return true, u.listcomp(n)
	case *SetComp:
		return true, u.setcomp(n)
	case *DictComp:
		return true, u.dictcomp(n)
	case *GeneratorExp:
		return true, u.genexpr(n)
	case *Await:
		return true, u.await(n, level)
	case *Yield:
		return true, u.yield(n)
	case *YieldFrom:
		return true, u.yieldFrom(n)
	case *Call:
		return true, u.call(n)
	}
	return false, nil
}

// exprAtoms handles primary/atom-shaped expressions.
func (u *unparser) exprAtoms(e Expr, level int) (bool, error) {
	switch n := e.(type) {
	case *Constant:
		return true, u.constant(n)
	case *Attribute:
		return true, u.attr(n)
	case *Subscript:
		return true, u.subscript(n)
	case *Starred:
		return true, u.starred(n)
	case *Name:
		u.ws(n.Id)
		return true, nil
	case *List:
		return true, u.list(n)
	case *Tuple:
		return true, u.tuple(n, level)
	case *Slice:
		return true, u.slice(n)
	}
	return false, nil
}

// exprStrings handles f-string / t-string node families.
func (u *unparser) exprStrings(e Expr) (bool, error) {
	switch n := e.(type) {
	case *JoinedStr:
		return true, u.joinedStr(n, false)
	case *FormattedValue:
		return true, u.formattedValue(n)
	case *Interpolation:
		return true, u.interpolation(n)
	case *TemplateStr:
		return true, u.templateStr(n)
	}
	return false, nil
}

func (u *unparser) boolop(n *BoolOp, level int) error {
	pr := prAnd
	op := " and "
	if n.Op == Or {
		pr = prOr
		op = " or "
	}
	u.wcond(level > pr, "(")
	for i := 0; i < n.Values.Len(); i++ {
		if i > 0 {
			u.ws(op)
		}
		if err := u.expr(n.Values.Get(i), pr+1); err != nil {
			return err
		}
	}
	u.wcond(level > pr, ")")
	return nil
}

func (u *unparser) namedexpr(n *NamedExpr, level int) error {
	u.wcond(level > prTuple, "(")
	if err := u.expr(n.Target, prAtom); err != nil {
		return err
	}
	u.ws(" := ")
	if err := u.expr(n.Value, prAtom); err != nil {
		return err
	}
	u.wcond(level > prTuple, ")")
	return nil
}

// binopInfo returns the precedence level and the spelled operator.
func binopInfo(op Operator) (level int, text string) {
	switch op {
	case Add:
		return prArith, " + "
	case Sub:
		return prArith, " - "
	case Mult:
		return prTerm, " * "
	case MatMult:
		return prTerm, " @ "
	case Div:
		return prTerm, " / "
	case ModOperator:
		return prTerm, " % "
	case Pow:
		return prPower, " ** "
	case LShift:
		return prShift, " << "
	case RShift:
		return prShift, " >> "
	case BitOr:
		return prBor, " | "
	case BitXor:
		return prBxor, " ^ "
	case BitAnd:
		return prBand, " & "
	case FloorDiv:
		return prTerm, " // "
	}
	return prAtom, " ? "
}

func (u *unparser) binop(n *BinOp, level int) error {
	pr, op := binopInfo(n.Op)
	rightAssoc := n.Op == Pow
	u.wcond(level > pr, "(")
	leftLevel := pr
	rightLevel := pr + 1
	if rightAssoc {
		leftLevel = pr + 1
		rightLevel = pr
	}
	if err := u.expr(n.Left, leftLevel); err != nil {
		return err
	}
	u.ws(op)
	if err := u.expr(n.Right, rightLevel); err != nil {
		return err
	}
	u.wcond(level > pr, ")")
	return nil
}

func (u *unparser) unaryop(n *UnaryOp, level int) error {
	switch n.Op {
	case Invert:
		u.wcond(level > prFactor, "(")
		u.ws("~")
		if err := u.expr(n.Operand, prFactor); err != nil {
			return err
		}
		u.wcond(level > prFactor, ")")
	case Not:
		u.wcond(level > prNot, "(")
		u.ws("not ")
		if err := u.expr(n.Operand, prNot); err != nil {
			return err
		}
		u.wcond(level > prNot, ")")
	case UAdd:
		u.wcond(level > prFactor, "(")
		u.ws("+")
		if err := u.expr(n.Operand, prFactor); err != nil {
			return err
		}
		u.wcond(level > prFactor, ")")
	case USub:
		u.wcond(level > prFactor, "(")
		u.ws("-")
		if err := u.expr(n.Operand, prFactor); err != nil {
			return err
		}
		u.wcond(level > prFactor, ")")
	}
	return nil
}

func (u *unparser) lambda(n *Lambda, level int) error {
	u.wcond(level > prTest, "(")
	u.ws("lambda")
	if argsNonEmpty(n.Args) {
		u.wc(' ')
		if err := u.arguments(n.Args); err != nil {
			return err
		}
	}
	u.ws(": ")
	if err := u.expr(n.Body, prTest); err != nil {
		return err
	}
	u.wcond(level > prTest, ")")
	return nil
}

func argsNonEmpty(a *Arguments) bool {
	if a == nil {
		return false
	}
	return a.Posonlyargs.Len() > 0 || a.Args.Len() > 0 ||
		a.Vararg != nil || a.Kwonlyargs.Len() > 0 || a.Kwarg != nil
}

// argEmitter is shared cursor state across the various positional /
// keyword-only / *vararg / **kwarg sections of a function signature.
type argEmitter struct {
	u     *unparser
	first bool
}

func (e *argEmitter) sep() {
	if !e.first {
		e.u.ws(", ")
	}
	e.first = false
}

func (e *argEmitter) arg(arg *Arg, def Expr) error {
	e.sep()
	e.u.ws(arg.Arg)
	if arg.Annotation != nil {
		e.u.ws(": ")
		if err := e.u.expr(arg.Annotation, prTest); err != nil {
			return err
		}
	}
	if def != nil {
		if arg.Annotation != nil {
			e.u.ws(" = ")
		} else {
			e.u.wc('=')
		}
		if err := e.u.expr(def, prTest); err != nil {
			return err
		}
	}
	return nil
}

func (u *unparser) arguments(a *Arguments) error {
	if a == nil {
		return nil
	}
	e := &argEmitter{u: u, first: true}
	if err := u.argsPosonly(e, a); err != nil {
		return err
	}
	if err := u.argsRegular(e, a); err != nil {
		return err
	}
	if err := u.argsStar(e, a); err != nil {
		return err
	}
	if err := u.argsKwonly(e, a); err != nil {
		return err
	}
	return u.argsKwarg(e, a)
}

func (u *unparser) argsPosonly(e *argEmitter, a *Arguments) error {
	posCount := a.Posonlyargs.Len() + a.Args.Len()
	defOff := posCount - a.Defaults.Len()
	for i := 0; i < a.Posonlyargs.Len(); i++ {
		var def Expr
		if i >= defOff {
			def = a.Defaults.Get(i - defOff)
		}
		if err := e.arg(a.Posonlyargs.Get(i), def); err != nil {
			return err
		}
	}
	if a.Posonlyargs.Len() > 0 {
		u.ws(", /")
	}
	return nil
}

func (u *unparser) argsRegular(e *argEmitter, a *Arguments) error {
	posCount := a.Posonlyargs.Len() + a.Args.Len()
	defOff := posCount - a.Defaults.Len()
	for i := 0; i < a.Args.Len(); i++ {
		var def Expr
		j := a.Posonlyargs.Len() + i
		if j >= defOff {
			def = a.Defaults.Get(j - defOff)
		}
		if err := e.arg(a.Args.Get(i), def); err != nil {
			return err
		}
	}
	return nil
}

func (u *unparser) argsStar(e *argEmitter, a *Arguments) error {
	if a.Vararg != nil {
		e.sep()
		u.wc('*')
		u.ws(a.Vararg.Arg)
		if a.Vararg.Annotation != nil {
			u.ws(": ")
			return u.expr(a.Vararg.Annotation, prTest)
		}
		return nil
	}
	if a.Kwonlyargs.Len() > 0 {
		e.sep()
		u.wc('*')
	}
	return nil
}

func (u *unparser) argsKwonly(e *argEmitter, a *Arguments) error {
	for i := 0; i < a.Kwonlyargs.Len(); i++ {
		var def Expr
		if i < a.KwDefaults.Len() {
			def = a.KwDefaults.Get(i)
		}
		if err := e.arg(a.Kwonlyargs.Get(i), def); err != nil {
			return err
		}
	}
	return nil
}

func (u *unparser) argsKwarg(e *argEmitter, a *Arguments) error {
	if a.Kwarg == nil {
		return nil
	}
	e.sep()
	u.ws("**")
	u.ws(a.Kwarg.Arg)
	if a.Kwarg.Annotation != nil {
		u.ws(": ")
		return u.expr(a.Kwarg.Annotation, prTest)
	}
	return nil
}

func (u *unparser) ifexp(n *IfExp, level int) error {
	u.wcond(level > prTest, "(")
	if err := u.expr(n.Body, prTest+1); err != nil {
		return err
	}
	u.ws(" if ")
	if err := u.expr(n.Test, prTest+1); err != nil {
		return err
	}
	u.ws(" else ")
	if err := u.expr(n.Orelse, prTest); err != nil {
		return err
	}
	u.wcond(level > prTest, ")")
	return nil
}

func (u *unparser) dict(n *Dict) error {
	u.wc('{')
	for i := 0; i < n.Values.Len(); i++ {
		if i > 0 {
			u.ws(", ")
		}
		var k Expr
		if i < n.Keys.Len() {
			k = n.Keys.Get(i)
		}
		if k == nil {
			u.ws("**")
			if err := u.expr(n.Values.Get(i), prExpr); err != nil {
				return err
			}
			continue
		}
		if err := u.expr(k, prTest); err != nil {
			return err
		}
		u.ws(": ")
		if err := u.expr(n.Values.Get(i), prTest); err != nil {
			return err
		}
	}
	u.wc('}')
	return nil
}

func (u *unparser) set(n *Set) error {
	if n.Elts.Len() == 0 {
		u.ws("set()")
		return nil
	}
	u.wc('{')
	for i := 0; i < n.Elts.Len(); i++ {
		if i > 0 {
			u.ws(", ")
		}
		if err := u.expr(n.Elts.Get(i), prTest); err != nil {
			return err
		}
	}
	u.wc('}')
	return nil
}

func (u *unparser) comprehensions(gens Seq[*Comprehension]) error {
	for i := 0; i < gens.Len(); i++ {
		c := gens.Get(i)
		if c.IsAsync != 0 {
			u.ws(" async for ")
		} else {
			u.ws(" for ")
		}
		if err := u.expr(c.Target, prTuple); err != nil {
			return err
		}
		u.ws(" in ")
		if err := u.expr(c.Iter, prTest+1); err != nil {
			return err
		}
		for j := 0; j < c.Ifs.Len(); j++ {
			u.ws(" if ")
			if err := u.expr(c.Ifs.Get(j), prTest+1); err != nil {
				return err
			}
		}
	}
	return nil
}

func (u *unparser) listcomp(n *ListComp) error {
	u.wc('[')
	if err := u.expr(n.Elt, prTest); err != nil {
		return err
	}
	if err := u.comprehensions(n.Generators); err != nil {
		return err
	}
	u.wc(']')
	return nil
}

func (u *unparser) setcomp(n *SetComp) error {
	u.wc('{')
	if err := u.expr(n.Elt, prTest); err != nil {
		return err
	}
	if err := u.comprehensions(n.Generators); err != nil {
		return err
	}
	u.wc('}')
	return nil
}

func (u *unparser) dictcomp(n *DictComp) error {
	u.wc('{')
	if err := u.expr(n.Key, prTest); err != nil {
		return err
	}
	u.ws(": ")
	if err := u.expr(n.Value, prTest); err != nil {
		return err
	}
	if err := u.comprehensions(n.Generators); err != nil {
		return err
	}
	u.wc('}')
	return nil
}

func (u *unparser) genexpr(n *GeneratorExp) error {
	u.wc('(')
	if err := u.expr(n.Elt, prTest); err != nil {
		return err
	}
	if err := u.comprehensions(n.Generators); err != nil {
		return err
	}
	u.wc(')')
	return nil
}

func (u *unparser) await(n *Await, level int) error {
	u.wcond(level > prAwait, "(")
	u.ws("await ")
	if err := u.expr(n.Value, prAtom); err != nil {
		return err
	}
	u.wcond(level > prAwait, ")")
	return nil
}

func (u *unparser) yield(n *Yield) error {
	u.wc('(')
	u.ws("yield")
	if n.Value != nil {
		u.wc(' ')
		if err := u.expr(n.Value, prTest); err != nil {
			return err
		}
	}
	u.wc(')')
	return nil
}

func (u *unparser) yieldFrom(n *YieldFrom) error {
	u.ws("(yield from ")
	if err := u.expr(n.Value, prAtom); err != nil {
		return err
	}
	u.wc(')')
	return nil
}

func cmpopText(c Cmpop) string {
	switch c {
	case Eq:
		return " == "
	case NotEq:
		return " != "
	case Lt:
		return " < "
	case LtE:
		return " <= "
	case Gt:
		return " > "
	case GtE:
		return " >= "
	case Is:
		return " is "
	case IsNot:
		return " is not "
	case In:
		return " in "
	case NotIn:
		return " not in "
	}
	return " ? "
}

func (u *unparser) compare(n *Compare, level int) error {
	u.wcond(level > prCmp, "(")
	if err := u.expr(n.Left, prCmp+1); err != nil {
		return err
	}
	for i := 0; i < n.Ops.Len(); i++ {
		u.ws(cmpopText(n.Ops.Get(i)))
		if err := u.expr(n.Comparators.Get(i), prCmp+1); err != nil {
			return err
		}
	}
	u.wcond(level > prCmp, ")")
	return nil
}

func (u *unparser) call(n *Call) error {
	if err := u.expr(n.Func, prAtom); err != nil {
		return err
	}
	u.wc('(')
	first := true
	for i := 0; i < n.Args.Len(); i++ {
		if !first {
			u.ws(", ")
		}
		first = false
		if err := u.expr(n.Args.Get(i), prTest); err != nil {
			return err
		}
	}
	for i := 0; i < n.Keywords.Len(); i++ {
		k := n.Keywords.Get(i)
		if !first {
			u.ws(", ")
		}
		first = false
		if k.Arg == nil {
			u.ws("**")
		} else {
			u.ws(*k.Arg)
			u.wc('=')
		}
		if err := u.expr(k.Value, prTest); err != nil {
			return err
		}
	}
	u.wc(')')
	return nil
}

// constant renders a Constant node. Mirrors append_repr from
// ast_unparse.c: floats and complex with infinite components render
// their inf component as "1e309" so the result evaluates to inf.
//
// CPython: Python/ast_unparse.c:append_repr (kind == "u" prefix)
func (u *unparser) constant(n *Constant) error {
	if n.Kind != nil && *n.Kind == "u" {
		if s, ok := n.Value.(string); ok {
			u.ws("u" + pythonStrRepr(s))
			return nil
		}
	}
	return u.constValue(n.Value)
}

func (u *unparser) constValue(v any) error {
	if u.constSingleton(v) {
		return nil
	}
	if u.constString(v) {
		return nil
	}
	if u.constNumeric(v) {
		return nil
	}
	return u.constCollection(v)
}

func (u *unparser) constSingleton(v any) bool {
	switch x := v.(type) {
	case nil:
		u.ws("None")
	case bool:
		if x {
			u.ws("True")
		} else {
			u.ws("False")
		}
	case EllipsisType:
		u.ws("...")
	default:
		return false
	}
	return true
}

func (u *unparser) constString(v any) bool {
	switch x := v.(type) {
	case string:
		u.ws(pythonStrRepr(x))
	case []byte:
		u.ws("b" + pythonStrRepr(string(x)))
	default:
		return false
	}
	return true
}

// pythonStrRepr renders a string constant using CPython's quote-selection
// logic: prefer single quotes, switch to double when the value contains a
// single quote but no double quote, otherwise single-quote and escape.
//
// CPython: Objects/unicodeobject.c:12956 unicode_repr
func pythonStrRepr(s string) string {
	hasSingle := strings.ContainsRune(s, '\'')
	hasDouble := strings.ContainsRune(s, '"')
	if hasSingle && !hasDouble {
		return strconv.Quote(s)
	}
	// Use single-quote delimiter (Go's strconv.Quote uses double-quote;
	// we rebuild with single-quote by replacing outer quotes and escaping).
	dq := strconv.Quote(s)
	inner := dq[1 : len(dq)-1]
	// The Go double-quoted form may have escaped double-quotes (\") that
	// are not needed inside single-quoted form; unescape them.
	inner = strings.ReplaceAll(inner, `\"`, `"`)
	// Single-quotes inside the value need escaping.
	inner = strings.ReplaceAll(inner, `'`, `\'`)
	return "'" + inner + "'"
}

func (u *unparser) constNumeric(v any) bool {
	switch x := v.(type) {
	case int:
		u.ws(strconv.Itoa(x))
	case int8, int16, int32, int64:
		u.ws(fmt.Sprintf("%d", x))
	case uint, uint8, uint16, uint32, uint64:
		u.ws(fmt.Sprintf("%d", x))
	case *big.Int:
		u.ws(x.String())
	case float32:
		u.floatRepr(float64(x))
	case float64:
		u.floatRepr(x)
	case complex64:
		u.complexRepr(complex128(x))
	case complex128:
		u.complexRepr(x)
	default:
		return false
	}
	return true
}

func (u *unparser) constCollection(v any) error {
	switch x := v.(type) {
	case []any:
		return u.constTuple(x)
	case FrozenSet:
		return u.constFrozenSet(x)
	}
	return fmt.Errorf("unparse: invalid constant %T", v)
}

func (u *unparser) constTuple(items []any) error {
	u.wc('(')
	for i, it := range items {
		if i > 0 {
			u.ws(", ")
		}
		if err := u.constValue(it); err != nil {
			return err
		}
	}
	if len(items) == 1 {
		u.wc(',')
	}
	u.wc(')')
	return nil
}

func (u *unparser) constFrozenSet(items FrozenSet) error {
	u.ws("frozenset({")
	for i, it := range items {
		if i > 0 {
			u.ws(", ")
		}
		if err := u.constValue(it); err != nil {
			return err
		}
	}
	u.ws("})")
	return nil
}

func (u *unparser) floatRepr(f float64) {
	switch {
	case math.IsInf(f, 1):
		u.ws("1e309")
	case math.IsInf(f, -1):
		u.ws("-1e309")
	case math.IsNaN(f):
		u.ws("nan")
	default:
		// strconv.FormatFloat with -1 precision matches CPython's
		// shortest repr in most cases. Trailing ".0" is appended for
		// whole-number floats so the literal stays a float.
		s := strconv.FormatFloat(f, 'g', -1, 64)
		if !strings.ContainsAny(s, ".eE") && !strings.ContainsAny(s, "ni") {
			s += ".0"
		}
		u.ws(s)
	}
}

// imagPartRepr formats the imaginary part of a complex literal.
// CPython: Python/ast_unparse.c - imaginary parts don't get trailing .0 for whole numbers.
func (u *unparser) imagPartRepr(f float64) {
	switch {
	case math.IsInf(f, 1):
		u.ws("1e309")
	case math.IsInf(f, -1):
		u.ws("-1e309")
	case math.IsNaN(f):
		u.ws("nan")
	default:
		s := strconv.FormatFloat(f, 'g', -1, 64)
		u.ws(s)
	}
}

func (u *unparser) complexRepr(c complex128) {
	r, im := real(c), imag(c)
	if r == 0 {
		u.imagPartRepr(im)
		u.wc('j')
		return
	}
	// CPython: Objects/complexobject.c complex_repr uses integer format for
	// whole-number components (no trailing .0).
	u.wc('(')
	u.imagPartRepr(r)
	if math.Signbit(im) {
		u.wc('-')
		u.imagPartRepr(-im)
	} else {
		u.wc('+')
		u.imagPartRepr(im)
	}
	u.wc('j')
	u.wc(')')
}

func (u *unparser) attr(n *Attribute) error {
	if err := u.expr(n.Value, prAtom); err != nil {
		return err
	}
	// Constants like 1.attr need a space so the dot is not parsed as
	// part of the float literal. The parser stores small integers as int64.
	if c, ok := n.Value.(*Constant); ok {
		switch c.Value.(type) {
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, *big.Int:
			u.wc(' ')
		}
	}
	u.wc('.')
	u.ws(n.Attr)
	return nil
}

func (u *unparser) subscript(n *Subscript) error {
	if err := u.expr(n.Value, prAtom); err != nil {
		return err
	}
	u.wc('[')
	if err := u.expr(n.Slice, prTuple); err != nil {
		return err
	}
	u.wc(']')
	return nil
}

func (u *unparser) starred(n *Starred) error {
	u.wc('*')
	return u.expr(n.Value, prExpr)
}

func (u *unparser) list(n *List) error {
	u.wc('[')
	for i := 0; i < n.Elts.Len(); i++ {
		if i > 0 {
			u.ws(", ")
		}
		if err := u.expr(n.Elts.Get(i), prTest); err != nil {
			return err
		}
	}
	u.wc(']')
	return nil
}

func (u *unparser) tuple(n *Tuple, level int) error {
	if n.Elts.Len() == 0 {
		u.ws("()")
		return nil
	}
	u.wcond(level > prTuple, "(")
	for i := 0; i < n.Elts.Len(); i++ {
		if i > 0 {
			u.ws(", ")
		}
		if err := u.expr(n.Elts.Get(i), prTest); err != nil {
			return err
		}
	}
	if n.Elts.Len() == 1 {
		u.wc(',')
	}
	u.wcond(level > prTuple, ")")
	return nil
}

func (u *unparser) slice(n *Slice) error {
	if n.Lower != nil {
		if err := u.expr(n.Lower, prTest); err != nil {
			return err
		}
	}
	u.wc(':')
	if n.Upper != nil {
		if err := u.expr(n.Upper, prTest); err != nil {
			return err
		}
	}
	if n.Step != nil {
		u.wc(':')
		if err := u.expr(n.Step, prTest); err != nil {
			return err
		}
	}
	return nil
}

func (u *unparser) joinedStr(n *JoinedStr, isFormatSpec bool) error {
	if isFormatSpec {
		// Inside a format spec there are no outer quotes; emit raw body.
		for i := 0; i < n.Values.Len(); i++ {
			v := n.Values.Get(i)
			switch x := v.(type) {
			case *Constant:
				s, _ := x.Value.(string)
				u.ws(s)
			case *FormattedValue:
				if err := u.formattedValue(x); err != nil {
					return err
				}
			default:
				if err := u.expr(v, prTest); err != nil {
					return err
				}
			}
		}
		return nil
	}

	// CPython: Python/ast_unparse.c append_joinedstr builds the body then
	// calls append_repr(body) which uses Python repr() quote selection:
	// prefer single quotes unless the body contains a single quote.
	// Literal string segments have braces escaped ({{/}}); expression
	// segments are emitted verbatim as {expr}.
	var body strings.Builder
	bodyU := &unparser{w: &body}
	for i := 0; i < n.Values.Len(); i++ {
		v := n.Values.Get(i)
		switch x := v.(type) {
		case *Constant:
			s, _ := x.Value.(string)
			// Escape braces in literal segments (CPython: escape_braces).
			body.WriteString(escapeFStringBraces(s))
		case *FormattedValue:
			if err := bodyU.formattedValue(x); err != nil {
				return err
			}
		default:
			if err := bodyU.expr(v, prTest); err != nil {
				return err
			}
		}
	}
	bodyStr := body.String()

	// CPython: choose quote char matching Python's repr() preference:
	// single quotes if the body has no single quotes, else double quotes.
	quote := byte('\'')
	if strings.ContainsRune(bodyStr, '\'') {
		quote = '"'
	}

	u.wc('f')
	u.wc(quote)
	u.ws(escapeFStringQuoteAndSlashes(bodyStr, quote))
	u.wc(quote)
	return nil
}

func (u *unparser) formattedValue(n *FormattedValue) error {
	// CPython: Python/ast_unparse.c append_interpolation_str - if the
	// expression starts with '{', insert a space to avoid '{{'.
	var exprBuf strings.Builder
	exprU := &unparser{w: &exprBuf}
	if err := exprU.expr(n.Value, prTest+1); err != nil {
		return err
	}
	exprStr := exprBuf.String()
	if len(exprStr) > 0 && exprStr[0] == '{' {
		u.ws("{ ")
	} else {
		u.wc('{')
	}
	u.ws(exprStr)
	if n.Conversion > 0 {
		u.wc('!')
		u.wc(byte(n.Conversion))
	}
	if n.FormatSpec != nil {
		u.wc(':')
		if js, ok := n.FormatSpec.(*JoinedStr); ok {
			if err := u.joinedStr(js, true); err != nil {
				return err
			}
		} else if err := u.expr(n.FormatSpec, prTest); err != nil {
			return err
		}
	}
	u.wc('}')
	return nil
}

func (u *unparser) interpolation(n *Interpolation) error {
	// CPython: Python/ast_unparse.c append_interpolation uses interp.str
	// (raw source text) to preserve the original whitespace and formatting.
	rawStr, _ := n.Str.(string)
	if rawStr == "" {
		// Fallback: re-render via expression unparser.
		return u.formattedValue(&FormattedValue{
			Value:      n.Value,
			Conversion: n.Conversion,
			FormatSpec: n.FormatSpec,
			Pos:        n.Pos,
		})
	}
	if len(rawStr) > 0 && rawStr[0] == '{' {
		u.ws("{ ")
	} else {
		u.wc('{')
	}
	u.ws(rawStr)
	if n.Conversion > 0 {
		u.wc('!')
		u.wc(byte(n.Conversion))
	}
	if n.FormatSpec != nil {
		u.wc(':')
		if js, ok := n.FormatSpec.(*JoinedStr); ok {
			if err := u.joinedStr(js, true); err != nil {
				return err
			}
		} else if err := u.expr(n.FormatSpec, prTest); err != nil {
			return err
		}
	}
	u.wc('}')
	return nil
}

func (u *unparser) templateStr(n *TemplateStr) error {
	// Same quote selection as joinedStr: prefer single quotes unless body has single quotes.
	var body strings.Builder
	bodyU := &unparser{w: &body}
	for i := 0; i < n.Values.Len(); i++ {
		v := n.Values.Get(i)
		switch x := v.(type) {
		case *Constant:
			s, _ := x.Value.(string)
			body.WriteString(escapeFStringBraces(s))
		case *Interpolation:
			if err := bodyU.interpolation(x); err != nil {
				return err
			}
		default:
			if err := bodyU.expr(v, prTest); err != nil {
				return err
			}
		}
	}
	bodyStr := body.String()
	quote := byte('\'')
	if strings.ContainsRune(bodyStr, '\'') {
		quote = '"'
	}
	u.wc('t')
	u.wc(quote)
	u.ws(escapeFStringQuoteAndSlashes(bodyStr, quote))
	u.wc(quote)
	return nil
}

// escapeFStringBraces escapes literal `{` and `}` in a constant segment of
// an f-string so they are not confused with expression delimiters.
// CPython: Python/ast_unparse.c escape_braces
func escapeFStringBraces(s string) string {
	s = strings.ReplaceAll(s, "{", "{{")
	s = strings.ReplaceAll(s, "}", "}}")
	return s
}

// escapeFStringQuoteAndSlashes escapes the chosen quote character and
// backslash/newline in the f-string body (after braces are already escaped).
// CPython: Python/ast_unparse.c append_repr on the assembled body string.
func escapeFStringQuoteAndSlashes(s string, quote byte) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == rune(quote):
			b.WriteByte('\\')
			b.WriteByte(quote)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
