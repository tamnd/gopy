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
	"fmt"

	"github.com/tamnd/gopy/ast"
	"github.com/tamnd/gopy/objects"
)

// reverseBridgeRecursionLimit caps the AST conversion depth so that
// circular Python AST objects (e.g. test_recursion_direct) produce a
// RecursionError instead of overflowing the goroutine stack.
//
// CPython: Python/bltinmodule.c:813 builtin_compile_impl PyAST_obj2mod
// uses Py_EnterRecursiveCall which maps to the same limit.
const reverseBridgeRecursionLimit = 500

// astRecursionSentinel is panicked (and recovered) when the reverse
// bridge conversion depth exceeds reverseBridgeRecursionLimit.
type astRecursionSentinel struct{}

// astValidationError is panicked when a required integer field (lineno,
// col_offset) is present on a Python AST node but holds None instead of an
// integer. Mirrors CPython's PyAST_Validate behavior.
//
// CPython: Python/ast.c:validate_expr / validate_stmt LOCATION checks
type astValidationError struct{ msg string }

// astTypeError is panicked when a field holds a value of the wrong type,
// matching CPython's obj2ast_identifier / obj2ast_expr TypeError raises.
//
// CPython: Python/Python-ast.c obj2ast_identifier / obj2ast_expr
type astTypeError struct{ msg string }

// PyASTObjectToMod converts a Python _ast top-level node to a Go ast.Mod.
// Returns (nil, false) when the argument is not recognized as an AST object.
// Returns (nil, false, RecursionError) when the AST is circular.
// Returns (nil, false, ValueError) when a required integer field is None.
//
// CPython: Python/bltinmodule.c:813 builtin_compile_impl PyAST_obj2mod
func PyASTObjectToMod(o objects.Object) (mod ast.Mod, ok bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			if _, isSentinel := r.(astRecursionSentinel); isSentinel {
				err = fmt.Errorf("RecursionError: maximum recursion depth exceeded during compilation")
				return
			}
			if ve, isValidation := r.(astValidationError); isValidation {
				err = fmt.Errorf("ValueError: %s", ve.msg)
				return
			}
			if te, isType := r.(astTypeError); isType {
				err = fmt.Errorf("TypeError: %s", te.msg)
				return
			}
			panic(r)
		}
	}()
	inst, instOK := o.(*objects.Instance)
	if !instOK {
		return nil, false, nil
	}
	r := &reverseASTBridge{depth: reverseBridgeRecursionLimit}
	mod = r.convertMod(inst)
	if mod == nil {
		return nil, false, nil
	}
	return mod, true, nil
}

// reverseASTBridge carries recursion depth for circular-AST detection.
//
// CPython: Python/bltinmodule.c PyAST_obj2mod equivalent (stateless in
// CPython; depth tracking is done via Py_EnterRecursiveCall on the thread).
type reverseASTBridge struct {
	depth int
}

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
	out := make(ast.Seq[ast.Stmt], lst.Len())
	for i := 0; i < lst.Len(); i++ {
		item := lst.Item(i)
		if item == nil || item == objects.None() {
			out[i] = nil
		} else {
			out[i] = r.convertStmt(item)
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
		value := r.convertExpr(r.getAttr(inst, "value"))
		return &ast.Assign{Targets: targets, Value: value, Pos: pos}
	case "Expr":
		value := r.convertExpr(r.getAttr(inst, "value"))
		return &ast.ExprStmt{Value: value, Pos: pos}
	case "AugAssign":
		target := r.convertExpr(r.getAttr(inst, "target"))
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
	case "Import":
		names := r.convertAliases(r.getAttr(inst, "names"))
		return &ast.Import{Names: names, Pos: pos}
	case "ImportFrom":
		module := r.getAttrString(inst, "module")
		names := r.convertAliases(r.getAttr(inst, "names"))
		level := r.convertImportLevel(inst)
		return &ast.ImportFrom{Module: &module, Names: names, Level: level, Pos: pos}
	case "Global":
		names := r.stringList(r.getAttr(inst, "names"))
		return &ast.Global{Names: names, Pos: pos}
	case "Nonlocal":
		names := r.stringList(r.getAttr(inst, "names"))
		return &ast.Nonlocal{Names: names, Pos: pos}
	case "Raise":
		exc := r.convertExprOrNil(r.getAttr(inst, "exc"))
		cause := r.convertExprOrNil(r.getAttr(inst, "cause"))
		return &ast.Raise{Exc: exc, Cause: cause, Pos: pos}
	case "Assert":
		test := r.convertExpr(r.getAttr(inst, "test"))
		msg := r.convertExprOrNil(r.getAttr(inst, "msg"))
		return &ast.Assert{Test: test, Msg: msg, Pos: pos}
	case "Try":
		body := r.stmtList(r.getAttr(inst, "body"))
		handlers := r.convertExceptHandlers(r.getAttr(inst, "handlers"))
		orelse := r.stmtList(r.getAttr(inst, "orelse"))
		finalbody := r.stmtList(r.getAttr(inst, "finalbody"))
		return &ast.Try{Body: body, Handlers: handlers, Orelse: orelse, Finalbody: finalbody, Pos: pos}
	case "TryStar":
		body := r.stmtList(r.getAttr(inst, "body"))
		handlers := r.convertExceptHandlers(r.getAttr(inst, "handlers"))
		orelse := r.stmtList(r.getAttr(inst, "orelse"))
		finalbody := r.stmtList(r.getAttr(inst, "finalbody"))
		return &ast.TryStar{Body: body, Handlers: handlers, Orelse: orelse, Finalbody: finalbody, Pos: pos}
	case "With":
		items := r.convertWithitems(r.getAttr(inst, "items"))
		body := r.stmtList(r.getAttr(inst, "body"))
		return &ast.With{Items: items, Body: body, Pos: pos}
	case "AsyncWith":
		items := r.convertWithitems(r.getAttr(inst, "items"))
		body := r.stmtList(r.getAttr(inst, "body"))
		return &ast.AsyncWith{Items: items, Body: body, Pos: pos}
	case "AsyncFor":
		target := r.convertExpr(r.getAttr(inst, "target"))
		iter := r.convertExpr(r.getAttr(inst, "iter"))
		body := r.stmtList(r.getAttr(inst, "body"))
		orelse := r.stmtList(r.getAttr(inst, "orelse"))
		return &ast.AsyncFor{Target: target, Iter: iter, Body: body, Orelse: orelse, Pos: pos}
	case "Match":
		subject := r.convertExpr(r.getAttr(inst, "subject"))
		cases := r.convertMatchCases(r.getAttr(inst, "cases"))
		return &ast.Match{Subject: subject, Cases: cases, Pos: pos}
	case "TypeAlias":
		name := r.convertExpr(r.getAttr(inst, "name"))
		typeParams := r.convertTypeParams(r.getAttr(inst, "type_params"))
		value := r.convertExpr(r.getAttr(inst, "value"))
		return &ast.TypeAlias{Name: name, TypeParams: typeParams, Value: value, Pos: pos}
	}
	// Unknown statement: emit a Pass so the body remains valid.
	return &ast.Pass{Pos: pos}
}

func (r *reverseASTBridge) convertExceptHandlers(o objects.Object) ast.Seq[ast.Excepthandler] {
	lst, ok := o.(*objects.List)
	if !ok {
		return nil
	}
	out := make(ast.Seq[ast.Excepthandler], 0, lst.Len())
	for i := 0; i < lst.Len(); i++ {
		inst, ok := lst.Item(i).(*objects.Instance)
		if !ok {
			continue
		}
		pos := r.getPos(inst)
		var typ ast.Expr
		if t := r.getAttr(inst, "type"); t != nil && t != objects.None() {
			typ = r.convertExpr(t)
		}
		var name *string
		if n := r.getAttr(inst, "name"); n != nil && n != objects.None() {
			s := r.getAttrString(inst, "name")
			name = &s
		}
		body := r.stmtList(r.getAttr(inst, "body"))
		out = append(out, ast.Excepthandler(&ast.ExceptHandler{Type: typ, Name: name, Body: body, Pos: pos}))
	}
	return out
}

func (r *reverseASTBridge) convertWithitems(o objects.Object) ast.Seq[*ast.Withitem] {
	lst, ok := o.(*objects.List)
	if !ok {
		return nil
	}
	out := make(ast.Seq[*ast.Withitem], 0, lst.Len())
	for i := 0; i < lst.Len(); i++ {
		inst, ok := lst.Item(i).(*objects.Instance)
		if !ok {
			continue
		}
		ctxExpr := r.convertExpr(r.getAttr(inst, "context_expr"))
		var optVars ast.Expr
		if ov := r.getAttr(inst, "optional_vars"); ov != nil && ov != objects.None() {
			optVars = r.convertExpr(ov)
		}
		out = append(out, &ast.Withitem{ContextExpr: ctxExpr, OptionalVars: optVars})
	}
	return out
}

func (r *reverseASTBridge) convertMatchCases(o objects.Object) ast.Seq[*ast.MatchCase] {
	lst, ok := o.(*objects.List)
	if !ok {
		return nil
	}
	out := make(ast.Seq[*ast.MatchCase], 0, lst.Len())
	for i := 0; i < lst.Len(); i++ {
		inst, ok := lst.Item(i).(*objects.Instance)
		if !ok {
			continue
		}
		pattern := r.convertPattern(r.getAttr(inst, "pattern"))
		var guard ast.Expr
		if g := r.getAttr(inst, "guard"); g != nil && g != objects.None() {
			guard = r.convertExpr(g)
		}
		body := r.stmtList(r.getAttr(inst, "body"))
		out = append(out, &ast.MatchCase{Pattern: pattern, Guard: guard, Body: body})
	}
	return out
}

func (r *reverseASTBridge) convertPattern(o objects.Object) ast.Pattern {
	if o == nil || o == objects.None() {
		return nil
	}
	inst, ok := o.(*objects.Instance)
	if !ok {
		return nil
	}
	r.depth--
	if r.depth <= 0 {
		panic(astRecursionSentinel{})
	}
	defer func() { r.depth++ }()
	pos := r.getPos(inst)
	switch inst.Type().Name {
	case "MatchValue":
		value := r.convertExpr(r.getAttr(inst, "value"))
		return &ast.MatchValue{Value: value, Pos: pos}
	case "MatchSingleton":
		val := r.convertConstantValue(r.getAttr(inst, "value"))
		return &ast.MatchSingleton{Value: val, Pos: pos}
	case "MatchSequence":
		patterns := r.convertPatternList(r.getAttr(inst, "patterns"))
		return &ast.MatchSequence{Patterns: patterns, Pos: pos}
	case "MatchMapping":
		keys := r.exprListWithNone(r.getAttr(inst, "keys"))
		patterns := r.convertPatternList(r.getAttr(inst, "patterns"))
		var rest *string
		if rv := r.getAttr(inst, "rest"); rv != nil && rv != objects.None() {
			s := r.getAttrString(inst, "rest")
			rest = &s
		}
		return &ast.MatchMapping{Keys: keys, Patterns: patterns, Rest: rest, Pos: pos}
	case "MatchClass":
		cls := r.convertExpr(r.getAttr(inst, "cls"))
		patterns := r.convertPatternList(r.getAttr(inst, "patterns"))
		kwdAttrs := r.stringList(r.getAttr(inst, "kwd_attrs"))
		kwdPatterns := r.convertPatternList(r.getAttr(inst, "kwd_patterns"))
		return &ast.MatchClass{Cls: cls, Patterns: patterns, KwdAttrs: kwdAttrs, KwdPatterns: kwdPatterns, Pos: pos}
	case "MatchStar":
		var name *string
		if nv := r.getAttr(inst, "name"); nv != nil && nv != objects.None() {
			s := r.getAttrString(inst, "name")
			name = &s
		}
		return &ast.MatchStar{Name: name, Pos: pos}
	case "MatchAs":
		var pattern ast.Pattern
		if pv := r.getAttr(inst, "pattern"); pv != nil && pv != objects.None() {
			pattern = r.convertPattern(pv)
		}
		var name *string
		if nv := r.getAttr(inst, "name"); nv != nil && nv != objects.None() {
			s := r.getAttrString(inst, "name")
			name = &s
		}
		return &ast.MatchAs{Pattern: pattern, Name: name, Pos: pos}
	case "MatchOr":
		patterns := r.convertPatternList(r.getAttr(inst, "patterns"))
		return &ast.MatchOr{Patterns: patterns, Pos: pos}
	}
	return nil
}

func (r *reverseASTBridge) convertPatternList(o objects.Object) ast.Seq[ast.Pattern] {
	lst, ok := o.(*objects.List)
	if !ok {
		return nil
	}
	out := make(ast.Seq[ast.Pattern], 0, lst.Len())
	for i := 0; i < lst.Len(); i++ {
		if p := r.convertPattern(lst.Item(i)); p != nil {
			out = append(out, p)
		}
	}
	return out
}

func (r *reverseASTBridge) convertComprehension(inst *objects.Instance) *ast.Comprehension {
	if inst == nil {
		return nil
	}
	target := r.convertExpr(r.getAttr(inst, "target"))
	iter := r.convertExpr(r.getAttr(inst, "iter"))
	ifs := r.exprList(r.getAttr(inst, "ifs"))
	isAsync := r.getAttrInt(inst, "is_async")
	return &ast.Comprehension{Target: target, Iter: iter, Ifs: ifs, IsAsync: isAsync}
}

func (r *reverseASTBridge) convertComprehensions(o objects.Object) ast.Seq[*ast.Comprehension] {
	lst, ok := o.(*objects.List)
	if !ok {
		return nil
	}
	out := make(ast.Seq[*ast.Comprehension], 0, lst.Len())
	for i := 0; i < lst.Len(); i++ {
		inst, ok := lst.Item(i).(*objects.Instance)
		if !ok {
			continue
		}
		if c := r.convertComprehension(inst); c != nil {
			out = append(out, c)
		}
	}
	return out
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
		TypeParams:    r.convertTypeParams(r.getAttr(inst, "type_params")),
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
		TypeParams:    r.convertTypeParams(r.getAttr(inst, "type_params")),
		Pos:           pos,
	}
}

func (r *reverseASTBridge) convertClassDef(inst *objects.Instance, pos ast.Pos) *ast.ClassDef {
	name := r.getAttrString(inst, "name")
	body := r.stmtList(r.getAttr(inst, "body"))
	decorators := r.exprList(r.getAttr(inst, "decorator_list"))
	bases := r.exprList(r.getAttr(inst, "bases"))
	keywords := r.convertKeywords(r.getAttr(inst, "keywords"))
	return &ast.ClassDef{
		Name:          name,
		Bases:         bases,
		Keywords:      keywords,
		Body:          body,
		DecoratorList: decorators,
		TypeParams:    r.convertTypeParams(r.getAttr(inst, "type_params")),
		Pos:           pos,
	}
}

// convertTypeParams reverses convertTypeParams in the forward bridge:
// it rebuilds the Go PEP 695 type-parameter nodes (TypeVar / TypeVarTuple
// / ParamSpec) from their _ast instances so a compile()-from-AST request
// carrying generic functions, classes, or type aliases reaches codegen
// (and the validator) with its type_params intact.
//
// CPython: Python/Python-ast.c obj2ast_type_param
func (r *reverseASTBridge) convertTypeParams(o objects.Object) ast.Seq[ast.TypeParam] {
	lst, ok := o.(*objects.List)
	if !ok {
		return nil
	}
	out := make(ast.Seq[ast.TypeParam], 0, lst.Len())
	for i := 0; i < lst.Len(); i++ {
		inst, ok := lst.Item(i).(*objects.Instance)
		if !ok {
			continue
		}
		pos := r.getPos(inst)
		name := r.getAttrString(inst, "name")
		switch inst.Type().Name {
		case "TypeVar":
			var bound ast.Expr
			if b := r.getAttr(inst, "bound"); b != nil && b != objects.None() {
				bound = r.convertExpr(b)
			}
			out = append(out, &ast.TypeVar{
				Name:         name,
				Bound:        bound,
				DefaultValue: r.optionalExpr(inst, "default_value"),
				Pos:          pos,
			})
		case "TypeVarTuple":
			out = append(out, &ast.TypeVarTuple{
				Name:         name,
				DefaultValue: r.optionalExpr(inst, "default_value"),
				Pos:          pos,
			})
		case "ParamSpec":
			out = append(out, &ast.ParamSpec{
				Name:         name,
				DefaultValue: r.optionalExpr(inst, "default_value"),
				Pos:          pos,
			})
		}
	}
	return out
}

// optionalExpr converts attr name to a Go expr, returning nil when the
// attribute is missing or None.
func (r *reverseASTBridge) optionalExpr(inst *objects.Instance, name string) ast.Expr {
	if v := r.getAttr(inst, name); v != nil && v != objects.None() {
		return r.convertExpr(v)
	}
	return nil
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
	defaults := r.exprList(r.getAttr(inst, "defaults"))
	kwDefaults := r.exprListWithNone(r.getAttr(inst, "kw_defaults"))
	return &ast.Arguments{
		Posonlyargs: posonlyargs,
		Args:        args,
		Vararg:      vararg,
		Kwonlyargs:  kwonlyargs,
		KwDefaults:  kwDefaults,
		Kwarg:       kwarg,
		Defaults:    defaults,
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
	a := &ast.Arg{
		Arg: r.getAttrString(inst, "arg"),
		Pos: r.getPos(inst),
	}
	if ann := r.getAttr(inst, "annotation"); ann != nil && ann != objects.None() {
		a.Annotation = r.convertExpr(ann)
	}
	return a
}

func (r *reverseASTBridge) exprList(o objects.Object) ast.Seq[ast.Expr] {
	lst, ok := o.(*objects.List)
	if !ok {
		return nil
	}
	out := make(ast.Seq[ast.Expr], lst.Len())
	for i := 0; i < lst.Len(); i++ {
		item := lst.Item(i)
		if item == nil || item == objects.None() {
			out[i] = nil
		} else {
			out[i] = r.convertExpr(item)
		}
	}
	return out
}

// exprListWithNone converts a list that may contain Python None entries,
// preserving them as nil in the Go slice (used for kw_defaults where
// absent defaults are represented as None in the Python AST).
//
// CPython: Python/Python-ast.c arguments_obj2ast kw_defaults field
func (r *reverseASTBridge) exprListWithNone(o objects.Object) ast.Seq[ast.Expr] {
	lst, ok := o.(*objects.List)
	if !ok {
		return nil
	}
	out := make(ast.Seq[ast.Expr], lst.Len())
	for i := 0; i < lst.Len(); i++ {
		item := lst.Item(i)
		if item == nil || item == objects.None() {
			out[i] = nil
		} else {
			out[i] = r.convertExpr(item)
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
	r.depth--
	if r.depth <= 0 {
		panic(astRecursionSentinel{})
	}
	defer func() { r.depth++ }()
	pos := r.getPos(inst)
	switch inst.Type().Name {
	case "Name":
		id := r.getAttrIdentifier(inst, "id")
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
	case "TemplateStr":
		values := r.exprList(r.getAttr(inst, "values"))
		return &ast.TemplateStr{Values: values, Pos: pos}
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
	case "Interpolation":
		value := r.convertExpr(r.getAttr(inst, "value"))
		conv := r.getAttrInt(inst, "conversion")
		if conv == 0 {
			conv = -1
		}
		var fmtSpec ast.Expr
		if fs := r.getAttr(inst, "format_spec"); fs != nil && fs != objects.None() {
			fmtSpec = r.convertExpr(fs)
		}
		var strField any
		if sv := r.getAttr(inst, "str"); sv != nil && sv != objects.None() {
			strField = r.convertConstantValue(sv)
		}
		return &ast.Interpolation{Value: value, Str: strField, Conversion: conv, FormatSpec: fmtSpec, Pos: pos}
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
		keys := r.exprListWithNone(r.getAttr(inst, "keys"))
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
	case "Slice":
		var lower, upper, step ast.Expr
		if v := r.getAttr(inst, "lower"); v != nil && v != objects.None() {
			lower = r.convertExpr(v)
		}
		if v := r.getAttr(inst, "upper"); v != nil && v != objects.None() {
			upper = r.convertExpr(v)
		}
		if v := r.getAttr(inst, "step"); v != nil && v != objects.None() {
			step = r.convertExpr(v)
		}
		return &ast.Slice{Lower: lower, Upper: upper, Step: step, Pos: pos}
	case "ListComp":
		elt := r.convertExpr(r.getAttr(inst, "elt"))
		generators := r.convertComprehensions(r.getAttr(inst, "generators"))
		return &ast.ListComp{Elt: elt, Generators: generators, Pos: pos}
	case "SetComp":
		elt := r.convertExpr(r.getAttr(inst, "elt"))
		generators := r.convertComprehensions(r.getAttr(inst, "generators"))
		return &ast.SetComp{Elt: elt, Generators: generators, Pos: pos}
	case "GeneratorExp":
		elt := r.convertExpr(r.getAttr(inst, "elt"))
		generators := r.convertComprehensions(r.getAttr(inst, "generators"))
		return &ast.GeneratorExp{Elt: elt, Generators: generators, Pos: pos}
	case "DictComp":
		key := r.convertExpr(r.getAttr(inst, "key"))
		value := r.convertExpr(r.getAttr(inst, "value"))
		generators := r.convertComprehensions(r.getAttr(inst, "generators"))
		return &ast.DictComp{Key: key, Value: value, Generators: generators, Pos: pos}
	}
	// CPython: Python/Python-ast.c obj2ast_expr raises TypeError for unknown
	// types (e.g., abstract ast.expr() instances).
	typeName := inst.Type().Name
	panic(astTypeError{fmt.Sprintf("expected some sort of expr, but got %s()", typeName)})
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

// convertExprOrNil converts an expression object that may be None. Returns nil
// (not a Constant(nil)) when obj is None or absent.
//
// CPython: Python/Python-ast.c optional Expr fields use PyObject_IsInstance
func (r *reverseASTBridge) convertExprOrNil(obj objects.Object) ast.Expr {
	if obj == nil || obj == objects.None() {
		return nil
	}
	return r.convertExpr(obj)
}

// convertAliases converts a list of alias objects into ast.Alias nodes.
//
// CPython: Python/Python-ast.c alias constructor
func (r *reverseASTBridge) convertAliases(o objects.Object) ast.Seq[*ast.Alias] {
	lst, ok := o.(*objects.List)
	if !ok {
		return nil
	}
	out := make(ast.Seq[*ast.Alias], 0, lst.Len())
	for i := 0; i < lst.Len(); i++ {
		inst, ok := lst.Item(i).(*objects.Instance)
		if !ok {
			continue
		}
		name := r.getAttrString(inst, "name")
		var asname *string
		asnameVal := r.getAttr(inst, "asname")
		if asnameVal != nil && asnameVal != objects.None() {
			s := r.getAttrString(inst, "asname")
			asname = &s
		}
		out = append(out, &ast.Alias{Name: name, Asname: asname, Pos: r.getPos(inst)})
	}
	return out
}

// convertImportLevel reads the level attribute of an ImportFrom node. None or
// absent is treated as 0 (absolute import), matching CPython's behavior.
//
// CPython: Python/bltinmodule.c level handling in builtin_compile_impl
func (r *reverseASTBridge) convertImportLevel(inst *objects.Instance) *int {
	levelVal := r.getAttr(inst, "level")
	if levelVal == nil || levelVal == objects.None() {
		zero := 0
		return &zero
	}
	level := r.getAttrInt(inst, "level")
	return &level
}

// stringList converts a Python list of str to ast.Seq[string].
func (r *reverseASTBridge) stringList(o objects.Object) ast.Seq[string] {
	lst, ok := o.(*objects.List)
	if !ok {
		return nil
	}
	out := make(ast.Seq[string], 0, lst.Len())
	for i := 0; i < lst.Len(); i++ {
		if u, ok := lst.Item(i).(*objects.Unicode); ok {
			out = append(out, u.Value())
		}
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
	case *objects.Complex:
		return v.Complex128()
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
	case *objects.Set:
		if objects.IsExactFrozenSet(v) {
			elems := v.Items()
			fs := make(ast.FrozenSet, len(elems))
			for i, e := range elems {
				fs[i] = r.convertConstantValue(e)
			}
			return fs
		}
	}
	// CPython: Python/ast.c:195 validate_expr Constant_kind
	panic(astTypeError{fmt.Sprintf("got an invalid type in Constant: %s", o.Type().Name)})
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

// getAttrIdentifier reads a str identifier field and panics with astTypeError
// when it is not a str, mirroring CPython's obj2ast_identifier.
//
// CPython: Python/Python-ast.c:6112 obj2ast_identifier
func (r *reverseASTBridge) getAttrIdentifier(inst *objects.Instance, name string) string {
	v := r.getAttr(inst, name)
	if v == objects.None() {
		return ""
	}
	u, ok := v.(*objects.Unicode)
	if !ok {
		panic(astTypeError{"AST identifier must be of type str"})
	}
	return u.Value()
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

// getAttrPresent reads an attribute and reports whether it was present on the
// instance (vs. absent with an AttributeError). This lets callers distinguish
// "attribute not set" from "attribute set to None".
//
// CPython: Python/ast.c validate_expr uses PyObject_GetAttr + PyErr_Clear
func (r *reverseASTBridge) getAttrPresent(inst *objects.Instance, name string) (objects.Object, bool) {
	v, err := objects.GetAttr(inst, objects.NewStr(name))
	if err != nil || v == nil {
		return objects.None(), false
	}
	return v, true
}

// getPos extracts lineno/col_offset/end_lineno/end_col_offset from an
// instance. Returns ast.NoPos when lineno is absent. Panics with
// astValidationError when lineno is present but holds None. When end_lineno or
// end_col_offset are absent or None, substitutes lineno/col_offset respectively,
// matching CPython's Python/Python-ast.c:11187 obj2ast_stmt.
//
// CPython: Python/ast.c:1043 validate_stmt LOCATION macro
func (r *reverseASTBridge) getPos(inst *objects.Instance) ast.Pos {
	linenoVal, linenoPresent := r.getAttrPresent(inst, "lineno")
	if !linenoPresent {
		return ast.NoPos
	}
	if linenoVal == objects.None() {
		panic(astValidationError{"invalid integer value: None"})
	}
	lineno := r.getAttrInt(inst, "lineno")
	colOffset := r.getAttrInt(inst, "col_offset")

	endLineno := lineno
	if endVal, present := r.getAttrPresent(inst, "end_lineno"); present && endVal != objects.None() {
		endLineno = r.getAttrInt(inst, "end_lineno")
	}

	endColOffset := colOffset
	if endColVal, present := r.getAttrPresent(inst, "end_col_offset"); present && endColVal != objects.None() {
		endColOffset = r.getAttrInt(inst, "end_col_offset")
	}

	return ast.Pos{
		Lineno:       lineno,
		ColOffset:    colOffset,
		EndLineno:    endLineno,
		EndColOffset: endColOffset,
	}
}
