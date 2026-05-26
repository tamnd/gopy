// Python _ast object → Go ast.Mod reverse bridge. The complement of
// ast_bridge.go: accepts the _ast.Module / _ast.Expression etc. objects
// that compile(PyCF_ONLY_AST) returned and reconstructs the Go ast.Mod
// the compiler needs.
//
// The bridge covers the node subset exercised by test_fstring.test_ast
// and test_fstring.test_ast_compile_time_concat.
//
// CPython: Python/Python-ast.c PyAST_obj2mod (reverse direction)

package builtins

import (
	"github.com/tamnd/gopy/ast"
	"github.com/tamnd/gopy/objects"
)

// PyASTObjectToMod converts a Python _ast top-level node to a Go ast.Mod.
// Returns (nil, false) when the argument is not recognised as an AST object.
//
// CPython: Python/bltinmodule.c:813 builtin_compile_impl PyAST_obj2mod
func PyASTObjectToMod(o objects.Object) (ast.Mod, bool) {
	inst, ok := o.(*objects.Instance)
	if !ok {
		return nil, false
	}
	r := &reverseASTBridge{}
	mod := r.convertMod(inst)
	if mod == nil {
		return nil, false
	}
	return mod, true
}

// reverseASTBridge holds no state; methods are defined on it for
// readability, mirroring the forward astBridge.
type reverseASTBridge struct{}

func (r *reverseASTBridge) convertMod(inst *objects.Instance) ast.Mod {
	switch inst.Type().Name {
	case "Module":
		body := r.stmtList(r.getAttr(inst, "body"))
		return &ast.Module{Body: body}
	case "Expression":
		expr := r.convertExpr(r.getAttr(inst, "body"))
		return &ast.Expression{Body: expr}
	case "Interactive":
		body := r.stmtList(r.getAttr(inst, "body"))
		return &ast.Interactive{Body: body}
	}
	return nil
}

func (r *reverseASTBridge) stmtList(o objects.Object) ast.Seq[ast.Stmt] {
	lst, ok := o.(*objects.List)
	if !ok {
		return nil
	}
	out := make(ast.Seq[ast.Stmt], 0, lst.Len())
	for i := 0; i < lst.Len(); i++ {
		if s := r.convertStmt(lst.Item(i)); s != nil {
			out = append(out, s)
		}
	}
	return out
}

func (r *reverseASTBridge) convertStmt(o objects.Object) ast.Stmt {
	inst, ok := o.(*objects.Instance)
	if !ok {
		return nil
	}
	pos := r.getPos(inst)
	switch inst.Type().Name {
	case "Assign":
		targets := r.exprList(r.getAttr(inst, "targets"))
		// Mark assignment targets as Store so the compiler emits STORE_*.
		for _, t := range targets {
			r.setStoreCtx(t)
		}
		value := r.convertExpr(r.getAttr(inst, "value"))
		return &ast.Assign{Targets: targets, Value: value, Pos: pos}
	case "Expr":
		value := r.convertExpr(r.getAttr(inst, "value"))
		return &ast.ExprStmt{Value: value, Pos: pos}
	case "AugAssign":
		target := r.convertExpr(r.getAttr(inst, "target"))
		r.setStoreCtx(target)
		op := r.convertOperator(r.getAttr(inst, "op"))
		value := r.convertExpr(r.getAttr(inst, "value"))
		return &ast.AugAssign{Target: target, Op: op, Value: value, Pos: pos}
	case "Return":
		val := r.getAttr(inst, "value")
		var retVal ast.Expr
		if val != objects.None() {
			retVal = r.convertExpr(val)
		}
		return &ast.Return{Value: retVal, Pos: pos}
	case "If":
		test := r.convertExpr(r.getAttr(inst, "test"))
		body := r.stmtList(r.getAttr(inst, "body"))
		orelse := r.stmtList(r.getAttr(inst, "orelse"))
		return &ast.If{Test: test, Body: body, Orelse: orelse, Pos: pos}
	case "For":
		target := r.convertExpr(r.getAttr(inst, "target"))
		r.setStoreCtx(target)
		iter := r.convertExpr(r.getAttr(inst, "iter"))
		body := r.stmtList(r.getAttr(inst, "body"))
		orelse := r.stmtList(r.getAttr(inst, "orelse"))
		return &ast.For{Target: target, Iter: iter, Body: body, Orelse: orelse, Pos: pos}
	case "While":
		test := r.convertExpr(r.getAttr(inst, "test"))
		body := r.stmtList(r.getAttr(inst, "body"))
		orelse := r.stmtList(r.getAttr(inst, "orelse"))
		return &ast.While{Test: test, Body: body, Orelse: orelse, Pos: pos}
	case "FunctionDef":
		return r.convertFunctionDef(inst, pos)
	case "AsyncFunctionDef":
		return r.convertAsyncFunctionDef(inst, pos)
	case "ClassDef":
		return r.convertClassDef(inst, pos)
	case "Pass":
		return &ast.Pass{Pos: pos}
	case "Break":
		return &ast.Break{Pos: pos}
	case "Continue":
		return &ast.Continue{Pos: pos}
	case "Delete":
		targets := r.exprList(r.getAttr(inst, "targets"))
		return &ast.Delete{Targets: targets, Pos: pos}
	}
	// Unknown statement: emit a Pass so the body remains valid.
	return &ast.Pass{Pos: pos}
}

func (r *reverseASTBridge) convertFunctionDef(inst *objects.Instance, pos ast.Pos) *ast.FunctionDef {
	name := r.getAttrString(inst, "name")
	args := r.convertArguments(r.getAttr(inst, "args"))
	body := r.stmtList(r.getAttr(inst, "body"))
	decorators := r.exprList(r.getAttr(inst, "decorator_list"))
	var returns ast.Expr
	ret := r.getAttr(inst, "returns")
	if ret != nil && ret != objects.None() {
		returns = r.convertExpr(ret)
	}
	return &ast.FunctionDef{
		Name:          name,
		Args:          args,
		Body:          body,
		DecoratorList: decorators,
		Returns:       returns,
		Pos:           pos,
	}
}

func (r *reverseASTBridge) convertAsyncFunctionDef(inst *objects.Instance, pos ast.Pos) *ast.AsyncFunctionDef {
	name := r.getAttrString(inst, "name")
	args := r.convertArguments(r.getAttr(inst, "args"))
	body := r.stmtList(r.getAttr(inst, "body"))
	decorators := r.exprList(r.getAttr(inst, "decorator_list"))
	var returns ast.Expr
	ret := r.getAttr(inst, "returns")
	if ret != nil && ret != objects.None() {
		returns = r.convertExpr(ret)
	}
	return &ast.AsyncFunctionDef{
		Name:          name,
		Args:          args,
		Body:          body,
		DecoratorList: decorators,
		Returns:       returns,
		Pos:           pos,
	}
}

func (r *reverseASTBridge) convertClassDef(inst *objects.Instance, pos ast.Pos) *ast.ClassDef {
	name := r.getAttrString(inst, "name")
	body := r.stmtList(r.getAttr(inst, "body"))
	decorators := r.exprList(r.getAttr(inst, "decorator_list"))
	bases := r.exprList(r.getAttr(inst, "bases"))
	return &ast.ClassDef{
		Name:          name,
		Bases:         bases,
		Body:          body,
		DecoratorList: decorators,
		Pos:           pos,
	}
}

func (r *reverseASTBridge) convertArguments(o objects.Object) *ast.Arguments {
	inst, ok := o.(*objects.Instance)
	if !ok {
		return &ast.Arguments{}
	}
	posonlyargs := r.convertArgList(r.getAttr(inst, "posonlyargs"))
	args := r.convertArgList(r.getAttr(inst, "args"))
	kwonlyargs := r.convertArgList(r.getAttr(inst, "kwonlyargs"))
	var vararg *ast.Arg
	if v := r.getAttr(inst, "vararg"); v != nil && v != objects.None() {
		vararg = r.convertArg(v)
	}
	var kwarg *ast.Arg
	if v := r.getAttr(inst, "kwarg"); v != nil && v != objects.None() {
		kwarg = r.convertArg(v)
	}
	return &ast.Arguments{
		Posonlyargs: posonlyargs,
		Args:        args,
		Vararg:      vararg,
		Kwonlyargs:  kwonlyargs,
		Kwarg:       kwarg,
	}
}

func (r *reverseASTBridge) convertArgList(o objects.Object) ast.Seq[*ast.Arg] {
	lst, ok := o.(*objects.List)
	if !ok {
		return nil
	}
	out := make(ast.Seq[*ast.Arg], 0, lst.Len())
	for i := 0; i < lst.Len(); i++ {
		if a := r.convertArg(lst.Item(i)); a != nil {
			out = append(out, a)
		}
	}
	return out
}

func (r *reverseASTBridge) convertArg(o objects.Object) *ast.Arg {
	inst, ok := o.(*objects.Instance)
	if !ok {
		return nil
	}
	return &ast.Arg{
		Arg: r.getAttrString(inst, "arg"),
		Pos: r.getPos(inst),
	}
}

func (r *reverseASTBridge) exprList(o objects.Object) ast.Seq[ast.Expr] {
	lst, ok := o.(*objects.List)
	if !ok {
		return nil
	}
	out := make(ast.Seq[ast.Expr], 0, lst.Len())
	for i := 0; i < lst.Len(); i++ {
		if e := r.convertExpr(lst.Item(i)); e != nil {
			out = append(out, e)
		}
	}
	return out
}

func (r *reverseASTBridge) convertExpr(o objects.Object) ast.Expr {
	if o == nil || o == objects.None() {
		return nil
	}
	inst, ok := o.(*objects.Instance)
	if !ok {
		return nil
	}
	pos := r.getPos(inst)
	switch inst.Type().Name {
	case "Name":
		id := r.getAttrString(inst, "id")
		ctx := r.readCtx(inst)
		return &ast.Name{Id: id, Ctx: ctx, Pos: pos}
	case "Constant":
		val := r.convertConstantValue(r.getAttr(inst, "value"))
		return &ast.Constant{Value: val, Pos: pos}
	case "BinOp":
		left := r.convertExpr(r.getAttr(inst, "left"))
		op := r.convertOperator(r.getAttr(inst, "op"))
		right := r.convertExpr(r.getAttr(inst, "right"))
		return &ast.BinOp{Left: left, Op: op, Right: right, Pos: pos}
	case "UnaryOp":
		uop := r.convertUnaryOperator(r.getAttr(inst, "op"))
		operand := r.convertExpr(r.getAttr(inst, "operand"))
		return &ast.UnaryOp{Op: uop, Operand: operand, Pos: pos}
	case "Call":
		fn := r.convertExpr(r.getAttr(inst, "func"))
		args := r.exprList(r.getAttr(inst, "args"))
		kws := r.convertKeywords(r.getAttr(inst, "keywords"))
		return &ast.Call{Func: fn, Args: args, Keywords: kws, Pos: pos}
	case "Subscript":
		value := r.convertExpr(r.getAttr(inst, "value"))
		slice := r.convertExpr(r.getAttr(inst, "slice"))
		ctx := r.readCtx(inst)
		return &ast.Subscript{Value: value, Slice: slice, Ctx: ctx, Pos: pos}
	case "Attribute":
		value := r.convertExpr(r.getAttr(inst, "value"))
		attr := r.getAttrString(inst, "attr")
		ctx := r.readCtx(inst)
		return &ast.Attribute{Value: value, Attr: attr, Ctx: ctx, Pos: pos}
	case "JoinedStr":
		values := r.exprList(r.getAttr(inst, "values"))
		return &ast.JoinedStr{Values: values, Pos: pos}
	case "FormattedValue":
		value := r.convertExpr(r.getAttr(inst, "value"))
		conv := r.getAttrInt(inst, "conversion")
		if conv == 0 {
			conv = -1
		}
		var fmtSpec ast.Expr
		fs := r.getAttr(inst, "format_spec")
		if fs != nil && fs != objects.None() {
			fmtSpec = r.convertExpr(fs)
		}
		return &ast.FormattedValue{Value: value, Conversion: conv, FormatSpec: fmtSpec, Pos: pos}
	case "Tuple":
		elts := r.exprList(r.getAttr(inst, "elts"))
		ctx := r.readCtx(inst)
		return &ast.Tuple{Elts: elts, Ctx: ctx, Pos: pos}
	case "List":
		elts := r.exprList(r.getAttr(inst, "elts"))
		ctx := r.readCtx(inst)
		return &ast.List{Elts: elts, Ctx: ctx, Pos: pos}
	case "Starred":
		value := r.convertExpr(r.getAttr(inst, "value"))
		ctx := r.readCtx(inst)
		return &ast.Starred{Value: value, Ctx: ctx, Pos: pos}
	case "IfExp":
		test := r.convertExpr(r.getAttr(inst, "test"))
		body := r.convertExpr(r.getAttr(inst, "body"))
		orelse := r.convertExpr(r.getAttr(inst, "orelse"))
		return &ast.IfExp{Test: test, Body: body, Orelse: orelse, Pos: pos}
	case "Lambda":
		args := r.convertArguments(r.getAttr(inst, "args"))
		body := r.convertExpr(r.getAttr(inst, "body"))
		return &ast.Lambda{Args: args, Body: body, Pos: pos}
	case "NamedExpr":
		target := r.convertExpr(r.getAttr(inst, "target"))
		r.setStoreCtx(target)
		value := r.convertExpr(r.getAttr(inst, "value"))
		return &ast.NamedExpr{Target: target, Value: value, Pos: pos}
	case "Yield":
		val := r.getAttr(inst, "value")
		var yieldVal ast.Expr
		if val != nil && val != objects.None() {
			yieldVal = r.convertExpr(val)
		}
		return &ast.Yield{Value: yieldVal, Pos: pos}
	case "YieldFrom":
		value := r.convertExpr(r.getAttr(inst, "value"))
		return &ast.YieldFrom{Value: value, Pos: pos}
	case "Await":
		value := r.convertExpr(r.getAttr(inst, "value"))
		return &ast.Await{Value: value, Pos: pos}
	case "Dict":
		keys := r.exprList(r.getAttr(inst, "keys"))
		values := r.exprList(r.getAttr(inst, "values"))
		return &ast.Dict{Keys: keys, Values: values, Pos: pos}
	case "Set":
		elts := r.exprList(r.getAttr(inst, "elts"))
		return &ast.Set{Elts: elts, Pos: pos}
	case "Compare":
		left := r.convertExpr(r.getAttr(inst, "left"))
		cmpOps := r.convertCmpOps(r.getAttr(inst, "ops"))
		comparators := r.exprList(r.getAttr(inst, "comparators"))
		return &ast.Compare{Left: left, Ops: cmpOps, Comparators: comparators, Pos: pos}
	case "BoolOp":
		op := r.convertBoolOperator(r.getAttr(inst, "op"))
		values := r.exprList(r.getAttr(inst, "values"))
		return &ast.BoolOp{Op: op, Values: values, Pos: pos}
	}
	// Unknown expression: return a None constant to keep the tree valid.
	return &ast.Constant{Value: nil, Pos: pos}
}

func (r *reverseASTBridge) convertKeywords(o objects.Object) ast.Seq[*ast.Keyword] {
	lst, ok := o.(*objects.List)
	if !ok {
		return nil
	}
	out := make(ast.Seq[*ast.Keyword], 0, lst.Len())
	for i := 0; i < lst.Len(); i++ {
		inst, ok := lst.Item(i).(*objects.Instance)
		if !ok {
			continue
		}
		var arg *string
		argVal := r.getAttr(inst, "arg")
		if argVal != nil && argVal != objects.None() {
			s := r.getAttrString(inst, "arg")
			arg = &s
		}
		val := r.convertExpr(r.getAttr(inst, "value"))
		out = append(out, &ast.Keyword{Arg: arg, Value: val, Pos: r.getPos(inst)})
	}
	return out
}

// readCtx reads the ctx attribute from an instance and converts it to
// ast.ExprContext. Defaults to Load when the attribute is missing or unknown.
func (r *reverseASTBridge) readCtx(inst *objects.Instance) ast.ExprContext {
	ctx := r.getAttr(inst, "ctx")
	if ctx == nil || ctx == objects.None() {
		return ast.Load
	}
	ctxInst, ok := ctx.(*objects.Instance)
	if !ok {
		return ast.Load
	}
	switch ctxInst.Type().Name {
	case "Store":
		return ast.Store
	case "Del":
		return ast.Del
	default:
		return ast.Load
	}
}

// setStoreCtx recursively stamps Store on Name/Attribute/Subscript
// leaves when a target has no explicit ctx set. This covers the case
// where the forward bridge omitted ctx (old objects produced before
// the ctx fix landed).
func (r *reverseASTBridge) setStoreCtx(e ast.Expr) {
	switch v := e.(type) {
	case *ast.Name:
		v.Ctx = ast.Store
	case *ast.Attribute:
		v.Ctx = ast.Store
	case *ast.Subscript:
		v.Ctx = ast.Store
	case *ast.Starred:
		r.setStoreCtx(v.Value)
	case *ast.Tuple:
		for _, elt := range v.Elts {
			r.setStoreCtx(elt)
		}
		v.Ctx = ast.Store
	case *ast.List:
		for _, elt := range v.Elts {
			r.setStoreCtx(elt)
		}
		v.Ctx = ast.Store
	}
}

func (r *reverseASTBridge) convertOperator(o objects.Object) ast.Operator {
	inst, ok := o.(*objects.Instance)
	if !ok {
		return ast.Add
	}
	switch inst.Type().Name {
	case "Add":
		return ast.Add
	case "Sub":
		return ast.Sub
	case "Mult":
		return ast.Mult
	case "Div":
		return ast.Div
	case "Mod":
		return ast.ModOperator
	case "Pow":
		return ast.Pow
	case "BitOr":
		return ast.BitOr
	case "BitAnd":
		return ast.BitAnd
	case "BitXor":
		return ast.BitXor
	case "FloorDiv":
		return ast.FloorDiv
	case "LShift":
		return ast.LShift
	case "RShift":
		return ast.RShift
	case "MatMult":
		return ast.MatMult
	}
	return ast.Add
}

func (r *reverseASTBridge) convertUnaryOperator(o objects.Object) ast.Unaryop {
	inst, ok := o.(*objects.Instance)
	if !ok {
		return ast.USub
	}
	switch inst.Type().Name {
	case "Invert":
		return ast.Invert
	case "Not":
		return ast.Not
	case "UAdd":
		return ast.UAdd
	case "USub":
		return ast.USub
	}
	return ast.USub
}

func (r *reverseASTBridge) convertBoolOperator(o objects.Object) ast.Boolop {
	inst, ok := o.(*objects.Instance)
	if !ok {
		return ast.And
	}
	switch inst.Type().Name {
	case "Or":
		return ast.Or
	default:
		return ast.And
	}
}

func (r *reverseASTBridge) convertCmpOps(o objects.Object) []ast.Cmpop {
	lst, ok := o.(*objects.List)
	if !ok {
		return nil
	}
	out := make([]ast.Cmpop, 0, lst.Len())
	for i := 0; i < lst.Len(); i++ {
		out = append(out, r.convertCmpOp(lst.Item(i)))
	}
	return out
}

func (r *reverseASTBridge) convertCmpOp(o objects.Object) ast.Cmpop {
	inst, ok := o.(*objects.Instance)
	if !ok {
		return ast.Eq
	}
	switch inst.Type().Name {
	case "Eq":
		return ast.Eq
	case "NotEq":
		return ast.NotEq
	case "Lt":
		return ast.Lt
	case "LtE":
		return ast.LtE
	case "Gt":
		return ast.Gt
	case "GtE":
		return ast.GtE
	case "Is":
		return ast.Is
	case "IsNot":
		return ast.IsNot
	case "In":
		return ast.In
	case "NotIn":
		return ast.NotIn
	}
	return ast.Eq
}

func (r *reverseASTBridge) convertConstantValue(o objects.Object) any {
	if o == nil || o == objects.None() {
		return nil
	}
	if objects.IsEllipsis(o) {
		return ast.EllipsisType{}
	}
	switch v := o.(type) {
	case *objects.Unicode:
		return v.Value()
	case *objects.Int:
		i64, _ := v.Int64()
		return i64
	case *objects.Float:
		return v.Float64()
	case *objects.Bool:
		return o == objects.True()
	case *objects.Bytes:
		return v.Bytes()
	case *objects.Tuple:
		items := make([]any, v.Len())
		for i := range items {
			items[i] = r.convertConstantValue(v.Item(i))
		}
		return items
	}
	return nil
}

// getAttr reads an attribute from an instance. Returns None() on error.
func (r *reverseASTBridge) getAttr(inst *objects.Instance, name string) objects.Object {
	v, err := objects.GetAttr(inst, objects.NewStr(name))
	if err != nil || v == nil {
		return objects.None()
	}
	return v
}

// getAttrString reads a string attribute. Returns "" on error.
func (r *reverseASTBridge) getAttrString(inst *objects.Instance, name string) string {
	v := r.getAttr(inst, name)
	if u, ok := v.(*objects.Unicode); ok {
		return u.Value()
	}
	return ""
}

// getAttrInt reads an int attribute. Returns 0 on error.
func (r *reverseASTBridge) getAttrInt(inst *objects.Instance, name string) int {
	v := r.getAttr(inst, name)
	if i, ok := v.(*objects.Int); ok {
		i64, _ := i.Int64()
		return int(i64)
	}
	return 0
}

// getPos extracts lineno/col_offset/end_lineno/end_col_offset from an
// instance, mirroring the forward bridge's withPos. Returns ast.NoPos
// if the attributes are missing.
func (r *reverseASTBridge) getPos(inst *objects.Instance) ast.Pos {
	lineno := r.getAttrInt(inst, "lineno")
	if lineno <= 0 {
		return ast.NoPos
	}
	return ast.Pos{
		Lineno:       lineno,
		ColOffset:    r.getAttrInt(inst, "col_offset"),
		EndLineno:    r.getAttrInt(inst, "end_lineno"),
		EndColOffset: r.getAttrInt(inst, "end_col_offset"),
	}
}
