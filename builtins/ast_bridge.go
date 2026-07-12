// Go → Python _ast bridge. Translates a Go ast.Mod tree into the
// Python _ast object hierarchy that compile() returns under PyCF_ONLY_AST.
// The bridge covers all statement and expression nodes defined in
// Python.asdl so test_unparse / test_ast / annotationlib work end-to-end.
//
// CPython: Python/Python-ast.c (auto-generated AST constructors)
// CPython: Python/bltinmodule.c:813 builtin_compile_impl PyAST_obj2mod

package builtins

import (
	"math/big"

	"github.com/tamnd/gopy/ast"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

// astModToObject converts a Go ast.Mod to a Python _ast object.
// Returns objects.None() if the _ast module is not yet loaded or the
// node type has no bridge.
//
// CPython: Python/bltinmodule.c:813 builtin_compile_impl
func astModToObject(mod ast.Mod) objects.Object {
	b := newASTBridge()
	if b == nil {
		return objects.None()
	}
	return b.convertMod(mod)
}

// astBridge wraps the _ast module so individual converters can look up
// class types once per call.
type astBridge struct {
	mod *objects.Module
}

func newASTBridge() *astBridge {
	m, ok := imp.GetModule("_ast")
	if !ok {
		return nil
	}
	return &astBridge{mod: m}
}

// cls returns the _ast class named name as a *objects.Type.
func (b *astBridge) cls(name string) *objects.Type {
	v, err := objects.GetAttr(b.mod, objects.NewStr(name))
	if err != nil {
		return nil
	}
	t, ok := v.(*objects.Type)
	if !ok {
		return nil
	}
	return t
}

// newInst allocates a new instance of the named _ast class and sets the
// given attributes on it. Returns objects.None() if the class is not found.
func (b *astBridge) newInst(name string, attrs map[string]objects.Object) objects.Object {
	t := b.cls(name)
	if t == nil {
		return objects.None()
	}
	inst := objects.NewInstance(t)
	for k, v := range attrs {
		_ = objects.SetAttr(inst, objects.NewStr(k), v)
	}
	return inst
}

func (b *astBridge) convertMod(m ast.Mod) objects.Object {
	switch n := m.(type) {
	case *ast.Module:
		return b.convertModule(n)
	case *ast.FunctionType:
		return b.convertFunctionType(n)
	case *ast.Expression:
		return b.newInst("Expression", map[string]objects.Object{
			"body": b.convertExpr(n.Body),
		})
	case *ast.Interactive:
		return b.newInst("Interactive", map[string]objects.Object{
			"body": b.convertStmtList(n.Body),
		})
	}
	return objects.None()
}

func (b *astBridge) convertModule(n *ast.Module) objects.Object {
	body := b.convertStmtList(n.Body)
	ignores := b.convertTypeIgnores(n.TypeIgnores)
	return b.newInst("Module", map[string]objects.Object{
		"body":         body,
		"type_ignores": ignores,
	})
}

func (b *astBridge) convertFunctionType(n *ast.FunctionType) objects.Object {
	argtypes := b.convertExprList(n.Argtypes)
	returns := b.convertExpr(n.Returns)
	return b.newInst("FunctionType", map[string]objects.Object{
		"argtypes": argtypes,
		"returns":  returns,
	})
}

func (b *astBridge) convertTypeIgnores(seq ast.Seq[ast.TypeIgnore]) objects.Object {
	items := make([]objects.Object, 0, len(seq))
	for _, ti := range seq {
		if n, ok := ti.(*ast.TypeIgnoreNode); ok {
			obj := b.newInst("TypeIgnore", map[string]objects.Object{
				"lineno": objects.NewInt(int64(n.Lineno)),
				"tag":    objects.NewStr(n.Tag),
			})
			items = append(items, obj)
		}
	}
	return objects.NewList(items)
}

func (b *astBridge) convertStmtList(seq ast.Seq[ast.Stmt]) objects.Object {
	items := make([]objects.Object, 0, len(seq))
	for _, s := range seq {
		items = append(items, b.convertStmt(s))
	}
	return objects.NewList(items)
}

// convertStmt converts any Go AST statement to the matching Python _ast node.
//
// CPython: Python/Python-ast.c (all stmt constructors)
func (b *astBridge) convertStmt(s ast.Stmt) objects.Object {
	if s == nil {
		return objects.None()
	}
	switch n := s.(type) {
	case *ast.FunctionDef:
		return b.convertFunctionDef(n)
	case *ast.AsyncFunctionDef:
		return b.convertAsyncFunctionDef(n)
	case *ast.ClassDef:
		return b.convertClassDef(n)
	case *ast.Return:
		return b.withPos("Return", map[string]objects.Object{
			"value": b.convertExprOrNone(n.Value),
		}, n.Pos)
	case *ast.Delete:
		return b.withPos("Delete", map[string]objects.Object{
			"targets": b.convertExprList(n.Targets),
		}, n.Pos)
	case *ast.Assign:
		return b.withPos("Assign", map[string]objects.Object{
			"targets":      b.convertExprList(n.Targets),
			"value":        b.convertExpr(n.Value),
			"type_comment": typeCommentObj(n.TypeComment),
		}, n.Pos)
	case *ast.TypeAlias:
		return b.withPos("TypeAlias", map[string]objects.Object{
			"name":        b.convertExpr(n.Name),
			"type_params": b.convertTypeParams(n.TypeParams),
			"value":       b.convertExpr(n.Value),
		}, n.Pos)
	case *ast.AugAssign:
		return b.withPos("AugAssign", map[string]objects.Object{
			"target": b.convertExpr(n.Target),
			"op":     b.convertOperator(n.Op),
			"value":  b.convertExpr(n.Value),
		}, n.Pos)
	case *ast.AnnAssign:
		return b.withPos("AnnAssign", map[string]objects.Object{
			"target":     b.convertExpr(n.Target),
			"annotation": b.convertExpr(n.Annotation),
			"value":      b.convertExprOrNone(n.Value),
			"simple":     objects.NewInt(int64(n.Simple)),
		}, n.Pos)
	case *ast.For:
		return b.withPos("For", map[string]objects.Object{
			"target":       b.convertExpr(n.Target),
			"iter":         b.convertExpr(n.Iter),
			"body":         b.convertStmtList(n.Body),
			"orelse":       b.convertStmtList(n.Orelse),
			"type_comment": typeCommentObj(n.TypeComment),
		}, n.Pos)
	case *ast.AsyncFor:
		return b.withPos("AsyncFor", map[string]objects.Object{
			"target":       b.convertExpr(n.Target),
			"iter":         b.convertExpr(n.Iter),
			"body":         b.convertStmtList(n.Body),
			"orelse":       b.convertStmtList(n.Orelse),
			"type_comment": typeCommentObj(n.TypeComment),
		}, n.Pos)
	case *ast.While:
		return b.withPos("While", map[string]objects.Object{
			"test":   b.convertExpr(n.Test),
			"body":   b.convertStmtList(n.Body),
			"orelse": b.convertStmtList(n.Orelse),
		}, n.Pos)
	case *ast.If:
		return b.withPos("If", map[string]objects.Object{
			"test":   b.convertExpr(n.Test),
			"body":   b.convertStmtList(n.Body),
			"orelse": b.convertStmtList(n.Orelse),
		}, n.Pos)
	case *ast.With:
		return b.withPos("With", map[string]objects.Object{
			"items":        b.convertWithItems(n.Items),
			"body":         b.convertStmtList(n.Body),
			"type_comment": typeCommentObj(n.TypeComment),
		}, n.Pos)
	case *ast.AsyncWith:
		return b.withPos("AsyncWith", map[string]objects.Object{
			"items":        b.convertWithItems(n.Items),
			"body":         b.convertStmtList(n.Body),
			"type_comment": typeCommentObj(n.TypeComment),
		}, n.Pos)
	case *ast.Match:
		return b.withPos("Match", map[string]objects.Object{
			"subject": b.convertExpr(n.Subject),
			"cases":   b.convertMatchCases(n.Cases),
		}, n.Pos)
	case *ast.Raise:
		return b.withPos("Raise", map[string]objects.Object{
			"exc":   b.convertExprOrNone(n.Exc),
			"cause": b.convertExprOrNone(n.Cause),
		}, n.Pos)
	case *ast.Try:
		return b.withPos("Try", map[string]objects.Object{
			"body":      b.convertStmtList(n.Body),
			"handlers":  b.convertHandlers(n.Handlers),
			"orelse":    b.convertStmtList(n.Orelse),
			"finalbody": b.convertStmtList(n.Finalbody),
		}, n.Pos)
	case *ast.TryStar:
		return b.withPos("TryStar", map[string]objects.Object{
			"body":      b.convertStmtList(n.Body),
			"handlers":  b.convertHandlers(n.Handlers),
			"orelse":    b.convertStmtList(n.Orelse),
			"finalbody": b.convertStmtList(n.Finalbody),
		}, n.Pos)
	case *ast.Assert:
		return b.withPos("Assert", map[string]objects.Object{
			"test": b.convertExpr(n.Test),
			"msg":  b.convertExprOrNone(n.Msg),
		}, n.Pos)
	case *ast.Import:
		return b.withPos("Import", map[string]objects.Object{
			"names": b.convertAliases(n.Names),
		}, n.Pos)
	case *ast.ImportFrom:
		var modObj objects.Object = objects.None()
		if n.Module != nil {
			modObj = objects.NewStr(*n.Module)
		}
		var levelObj objects.Object = objects.None()
		if n.Level != nil {
			levelObj = objects.NewInt(int64(*n.Level))
		}
		return b.withPos("ImportFrom", map[string]objects.Object{
			"module": modObj,
			"names":  b.convertAliases(n.Names),
			"level":  levelObj,
		}, n.Pos)
	case *ast.Global:
		return b.withPos("Global", map[string]objects.Object{
			"names": b.convertStrNames(n.Names),
		}, n.Pos)
	case *ast.Nonlocal:
		return b.withPos("Nonlocal", map[string]objects.Object{
			"names": b.convertStrNames(n.Names),
		}, n.Pos)
	case *ast.ExprStmt:
		return b.withPos("Expr", map[string]objects.Object{
			"value": b.convertExpr(n.Value),
		}, n.Pos)
	case *ast.Pass:
		return b.withPos("Pass", map[string]objects.Object{}, n.Pos)
	case *ast.Break:
		return b.withPos("Break", map[string]objects.Object{}, n.Pos)
	case *ast.Continue:
		return b.withPos("Continue", map[string]objects.Object{}, n.Pos)
	}
	pos := ast.Pos{}
	if positioner, ok := s.(interface{ Position() ast.Pos }); ok {
		pos = positioner.Position()
	}
	return b.withPos("Pass", map[string]objects.Object{}, pos)
}

func (b *astBridge) convertFunctionDef(n *ast.FunctionDef) objects.Object {
	return b.withPos("FunctionDef", map[string]objects.Object{
		"name":           objects.NewStr(n.Name),
		"args":           b.convertArguments(n.Args),
		"body":           b.convertStmtList(n.Body),
		"decorator_list": b.convertExprList(n.DecoratorList),
		"returns":        b.convertExprOrNone(n.Returns),
		"type_comment":   typeCommentObj(n.TypeComment),
		"type_params":    b.convertTypeParams(n.TypeParams),
	}, n.Pos)
}

func (b *astBridge) convertAsyncFunctionDef(n *ast.AsyncFunctionDef) objects.Object {
	return b.withPos("AsyncFunctionDef", map[string]objects.Object{
		"name":           objects.NewStr(n.Name),
		"args":           b.convertArguments(n.Args),
		"body":           b.convertStmtList(n.Body),
		"decorator_list": b.convertExprList(n.DecoratorList),
		"returns":        b.convertExprOrNone(n.Returns),
		"type_comment":   typeCommentObj(n.TypeComment),
		"type_params":    b.convertTypeParams(n.TypeParams),
	}, n.Pos)
}

func (b *astBridge) convertClassDef(n *ast.ClassDef) objects.Object {
	return b.withPos("ClassDef", map[string]objects.Object{
		"name":           objects.NewStr(n.Name),
		"bases":          b.convertExprList(n.Bases),
		"keywords":       b.convertKeywords(n.Keywords),
		"body":           b.convertStmtList(n.Body),
		"decorator_list": b.convertExprList(n.DecoratorList),
		"type_params":    b.convertTypeParams(n.TypeParams),
	}, n.Pos)
}

func (b *astBridge) convertArguments(a *ast.Arguments) objects.Object {
	if a == nil {
		return b.newInst("arguments", map[string]objects.Object{
			"posonlyargs": objects.NewList(nil),
			"args":        objects.NewList(nil),
			"vararg":      objects.None(),
			"kwonlyargs":  objects.NewList(nil),
			"kw_defaults": objects.NewList(nil),
			"kwarg":       objects.None(),
			"defaults":    objects.NewList(nil),
		})
	}
	var vararg objects.Object = objects.None()
	if a.Vararg != nil {
		vararg = b.convertArg(a.Vararg)
	}
	var kwarg objects.Object = objects.None()
	if a.Kwarg != nil {
		kwarg = b.convertArg(a.Kwarg)
	}
	return b.newInst("arguments", map[string]objects.Object{
		"posonlyargs": b.convertArgList(a.Posonlyargs),
		"args":        b.convertArgList(a.Args),
		"vararg":      vararg,
		"kwonlyargs":  b.convertArgList(a.Kwonlyargs),
		"kw_defaults": b.convertExprListWithNone(a.KwDefaults),
		"kwarg":       kwarg,
		"defaults":    b.convertExprList(a.Defaults),
	})
}

func (b *astBridge) convertArgList(seq ast.Seq[*ast.Arg]) objects.Object {
	items := make([]objects.Object, 0, len(seq))
	for _, a := range seq {
		items = append(items, b.convertArg(a))
	}
	return objects.NewList(items)
}

func (b *astBridge) convertArg(a *ast.Arg) objects.Object {
	if a == nil {
		return objects.None()
	}
	return b.withPos("arg", map[string]objects.Object{
		"arg":          objects.NewStr(a.Arg),
		"annotation":   b.convertExprOrNone(a.Annotation),
		"type_comment": typeCommentObj(a.TypeComment),
	}, a.Pos)
}

func (b *astBridge) convertExprList(seq ast.Seq[ast.Expr]) objects.Object {
	items := make([]objects.Object, 0, len(seq))
	for _, e := range seq {
		items = append(items, b.convertExpr(e))
	}
	return objects.NewList(items)
}

// convertExprListWithNone converts a sequence of optional exprs, emitting
// None for nil entries (used for kw_defaults where absent defaults are None).
func (b *astBridge) convertExprListWithNone(seq ast.Seq[ast.Expr]) objects.Object {
	items := make([]objects.Object, 0, len(seq))
	for _, e := range seq {
		items = append(items, b.convertExprOrNone(e))
	}
	return objects.NewList(items)
}

func (b *astBridge) convertExprOrNone(e ast.Expr) objects.Object {
	if e == nil {
		return objects.None()
	}
	return b.convertExpr(e)
}

func (b *astBridge) convertKeywords(seq ast.Seq[*ast.Keyword]) objects.Object {
	items := make([]objects.Object, 0, len(seq))
	for _, kw := range seq {
		if kw == nil {
			continue
		}
		var argObj objects.Object = objects.None()
		if kw.Arg != nil {
			argObj = objects.NewStr(*kw.Arg)
		}
		items = append(items, b.withPos("keyword", map[string]objects.Object{
			"arg":   argObj,
			"value": b.convertExpr(kw.Value),
		}, kw.Pos))
	}
	return objects.NewList(items)
}

func (b *astBridge) convertAliases(seq ast.Seq[*ast.Alias]) objects.Object {
	items := make([]objects.Object, 0, len(seq))
	for _, a := range seq {
		if a == nil {
			continue
		}
		var asname objects.Object = objects.None()
		if a.Asname != nil {
			asname = objects.NewStr(*a.Asname)
		}
		items = append(items, b.withPos("alias", map[string]objects.Object{
			"name":   objects.NewStr(a.Name),
			"asname": asname,
		}, a.Pos))
	}
	return objects.NewList(items)
}

func (b *astBridge) convertStrNames(seq ast.Seq[string]) objects.Object {
	items := make([]objects.Object, 0, len(seq))
	for _, s := range seq {
		items = append(items, objects.NewStr(s))
	}
	return objects.NewList(items)
}

func (b *astBridge) convertWithItems(seq ast.Seq[*ast.Withitem]) objects.Object {
	items := make([]objects.Object, 0, len(seq))
	for _, wi := range seq {
		if wi == nil {
			continue
		}
		items = append(items, b.newInst("withitem", map[string]objects.Object{
			"context_expr":  b.convertExpr(wi.ContextExpr),
			"optional_vars": b.convertExprOrNone(wi.OptionalVars),
		}))
	}
	return objects.NewList(items)
}

func (b *astBridge) convertHandlers(seq ast.Seq[ast.Excepthandler]) objects.Object {
	items := make([]objects.Object, 0, len(seq))
	for _, h := range seq {
		if eh, ok := h.(*ast.ExceptHandler); ok {
			var nameObj objects.Object = objects.None()
			if eh.Name != nil {
				nameObj = objects.NewStr(*eh.Name)
			}
			items = append(items, b.withPos("ExceptHandler", map[string]objects.Object{
				"type": b.convertExprOrNone(eh.Type),
				"name": nameObj,
				"body": b.convertStmtList(eh.Body),
			}, eh.Pos))
		}
	}
	return objects.NewList(items)
}

func (b *astBridge) convertMatchCases(seq ast.Seq[*ast.MatchCase]) objects.Object {
	items := make([]objects.Object, 0, len(seq))
	for _, mc := range seq {
		if mc == nil {
			continue
		}
		items = append(items, b.newInst("match_case", map[string]objects.Object{
			"pattern": b.convertPattern(mc.Pattern),
			"guard":   b.convertExprOrNone(mc.Guard),
			"body":    b.convertStmtList(mc.Body),
		}))
	}
	return objects.NewList(items)
}

func (b *astBridge) convertPattern(p ast.Pattern) objects.Object {
	if p == nil {
		return objects.None()
	}
	switch n := p.(type) {
	case *ast.MatchValue:
		return b.withPos("MatchValue", map[string]objects.Object{
			"value": b.convertExpr(n.Value),
		}, n.Pos)
	case *ast.MatchSingleton:
		return b.withPos("MatchSingleton", map[string]objects.Object{
			"value": b.convertConstantValue(n.Value),
		}, n.Pos)
	case *ast.MatchSequence:
		return b.withPos("MatchSequence", map[string]objects.Object{
			"patterns": b.convertPatternList(n.Patterns),
		}, n.Pos)
	case *ast.MatchMapping:
		return b.withPos("MatchMapping", map[string]objects.Object{
			"keys":     b.convertExprList(n.Keys),
			"patterns": b.convertPatternList(n.Patterns),
			"rest":     strOrNone(n.Rest),
		}, n.Pos)
	case *ast.MatchClass:
		return b.withPos("MatchClass", map[string]objects.Object{
			"cls":          b.convertExpr(n.Cls),
			"patterns":     b.convertPatternList(n.Patterns),
			"kwd_attrs":    b.convertStrNames(n.KwdAttrs),
			"kwd_patterns": b.convertPatternList(n.KwdPatterns),
		}, n.Pos)
	case *ast.MatchStar:
		return b.withPos("MatchStar", map[string]objects.Object{
			"name": strOrNone(n.Name),
		}, n.Pos)
	case *ast.MatchAs:
		return b.withPos("MatchAs", map[string]objects.Object{
			"pattern": b.convertPattern(n.Pattern),
			"name":    strOrNone(n.Name),
		}, n.Pos)
	case *ast.MatchOr:
		return b.withPos("MatchOr", map[string]objects.Object{
			"patterns": b.convertPatternList(n.Patterns),
		}, n.Pos)
	}
	return objects.None()
}

func (b *astBridge) convertPatternList(seq ast.Seq[ast.Pattern]) objects.Object {
	items := make([]objects.Object, 0, len(seq))
	for _, p := range seq {
		items = append(items, b.convertPattern(p))
	}
	return objects.NewList(items)
}

func (b *astBridge) convertTypeParams(seq ast.Seq[ast.TypeParam]) objects.Object {
	items := make([]objects.Object, 0, len(seq))
	for _, tp := range seq {
		switch n := tp.(type) {
		case *ast.TypeVar:
			items = append(items, b.withPos("TypeVar", map[string]objects.Object{
				"name":          objects.NewStr(n.Name),
				"bound":         b.convertExprOrNone(n.Bound),
				"default_value": b.convertExprOrNone(n.DefaultValue),
			}, n.Pos))
		case *ast.TypeVarTuple:
			items = append(items, b.withPos("TypeVarTuple", map[string]objects.Object{
				"name":          objects.NewStr(n.Name),
				"default_value": b.convertExprOrNone(n.DefaultValue),
			}, n.Pos))
		case *ast.ParamSpec:
			items = append(items, b.withPos("ParamSpec", map[string]objects.Object{
				"name":          objects.NewStr(n.Name),
				"default_value": b.convertExprOrNone(n.DefaultValue),
			}, n.Pos))
		}
	}
	return objects.NewList(items)
}

// convertExpr converts any Go AST expression to the matching Python _ast node.
//
// CPython: Python/Python-ast.c (all expr constructors)
func (b *astBridge) convertExpr(e ast.Expr) objects.Object {
	if e == nil {
		return objects.None()
	}
	switch n := e.(type) {
	case *ast.BoolOp:
		return b.withPos("BoolOp", map[string]objects.Object{
			"op":     b.convertBoolop(n.Op),
			"values": b.convertExprList(n.Values),
		}, n.Pos)
	case *ast.NamedExpr:
		return b.withPos("NamedExpr", map[string]objects.Object{
			"target": b.convertExpr(n.Target),
			"value":  b.convertExpr(n.Value),
		}, n.Pos)
	case *ast.BinOp:
		return b.withPos("BinOp", map[string]objects.Object{
			"left":  b.convertExpr(n.Left),
			"op":    b.convertOperator(n.Op),
			"right": b.convertExpr(n.Right),
		}, n.Pos)
	case *ast.UnaryOp:
		return b.withPos("UnaryOp", map[string]objects.Object{
			"op":      b.convertUnaryop(n.Op),
			"operand": b.convertExpr(n.Operand),
		}, n.Pos)
	case *ast.Lambda:
		return b.withPos("Lambda", map[string]objects.Object{
			"args": b.convertArguments(n.Args),
			"body": b.convertExpr(n.Body),
		}, n.Pos)
	case *ast.IfExp:
		return b.withPos("IfExp", map[string]objects.Object{
			"test":   b.convertExpr(n.Test),
			"body":   b.convertExpr(n.Body),
			"orelse": b.convertExpr(n.Orelse),
		}, n.Pos)
	case *ast.Dict:
		return b.withPos("Dict", map[string]objects.Object{
			"keys":   b.convertExprListWithNone(n.Keys),
			"values": b.convertExprList(n.Values),
		}, n.Pos)
	case *ast.Set:
		return b.withPos("Set", map[string]objects.Object{
			"elts": b.convertExprList(n.Elts),
		}, n.Pos)
	case *ast.ListComp:
		return b.withPos("ListComp", map[string]objects.Object{
			"elt":        b.convertExpr(n.Elt),
			"generators": b.convertComprehensions(n.Generators),
		}, n.Pos)
	case *ast.SetComp:
		return b.withPos("SetComp", map[string]objects.Object{
			"elt":        b.convertExpr(n.Elt),
			"generators": b.convertComprehensions(n.Generators),
		}, n.Pos)
	case *ast.DictComp:
		return b.withPos("DictComp", map[string]objects.Object{
			"key":        b.convertExpr(n.Key),
			"value":      b.convertExpr(n.Value),
			"generators": b.convertComprehensions(n.Generators),
		}, n.Pos)
	case *ast.GeneratorExp:
		return b.withPos("GeneratorExp", map[string]objects.Object{
			"elt":        b.convertExpr(n.Elt),
			"generators": b.convertComprehensions(n.Generators),
		}, n.Pos)
	case *ast.Await:
		return b.withPos("Await", map[string]objects.Object{
			"value": b.convertExpr(n.Value),
		}, n.Pos)
	case *ast.Yield:
		return b.withPos("Yield", map[string]objects.Object{
			"value": b.convertExprOrNone(n.Value),
		}, n.Pos)
	case *ast.YieldFrom:
		return b.withPos("YieldFrom", map[string]objects.Object{
			"value": b.convertExpr(n.Value),
		}, n.Pos)
	case *ast.Compare:
		return b.withPos("Compare", map[string]objects.Object{
			"left":        b.convertExpr(n.Left),
			"ops":         b.convertCmpops(n.Ops),
			"comparators": b.convertExprList(n.Comparators),
		}, n.Pos)
	case *ast.Call:
		return b.withPos("Call", map[string]objects.Object{
			"func":     b.convertExpr(n.Func),
			"args":     b.convertExprList(n.Args),
			"keywords": b.convertKeywords(n.Keywords),
		}, n.Pos)
	case *ast.FormattedValue:
		var fmtSpec objects.Object = objects.None()
		if n.FormatSpec != nil {
			fmtSpec = b.convertExpr(n.FormatSpec)
		}
		return b.withPos("FormattedValue", map[string]objects.Object{
			"value":       b.convertExpr(n.Value),
			"conversion":  objects.NewInt(int64(n.Conversion)),
			"format_spec": fmtSpec,
		}, n.Pos)
	case *ast.Interpolation:
		var fmtSpec objects.Object = objects.None()
		if n.FormatSpec != nil {
			fmtSpec = b.convertExpr(n.FormatSpec)
		}
		var strObj objects.Object = objects.None()
		if n.Str != nil {
			strObj = b.convertConstantValue(n.Str)
		}
		return b.withPos("Interpolation", map[string]objects.Object{
			"value":       b.convertExpr(n.Value),
			"str":         strObj,
			"conversion":  objects.NewInt(int64(n.Conversion)),
			"format_spec": fmtSpec,
		}, n.Pos)
	case *ast.JoinedStr:
		return b.withPos("JoinedStr", map[string]objects.Object{
			"values": b.convertExprList(n.Values),
		}, n.Pos)
	case *ast.TemplateStr:
		return b.withPos("TemplateStr", map[string]objects.Object{
			"values": b.convertExprList(n.Values),
		}, n.Pos)
	case *ast.Constant:
		var kind objects.Object = objects.None()
		if n.Kind != nil {
			kind = objects.NewStr(*n.Kind)
		}
		return b.withPos("Constant", map[string]objects.Object{
			"value": b.convertConstantValue(n.Value),
			"kind":  kind,
		}, n.Pos)
	case *ast.Attribute:
		return b.withPos("Attribute", map[string]objects.Object{
			"value": b.convertExpr(n.Value),
			"attr":  objects.NewStr(n.Attr),
			"ctx":   b.exprCtx(n.Ctx),
		}, n.Pos)
	case *ast.Subscript:
		return b.withPos("Subscript", map[string]objects.Object{
			"value": b.convertExpr(n.Value),
			"slice": b.convertExpr(n.Slice),
			"ctx":   b.exprCtx(n.Ctx),
		}, n.Pos)
	case *ast.Starred:
		return b.withPos("Starred", map[string]objects.Object{
			"value": b.convertExpr(n.Value),
			"ctx":   b.exprCtx(n.Ctx),
		}, n.Pos)
	case *ast.Name:
		return b.withPos("Name", map[string]objects.Object{
			"id":  objects.NewStr(n.Id),
			"ctx": b.exprCtx(n.Ctx),
		}, n.Pos)
	case *ast.List:
		return b.withPos("List", map[string]objects.Object{
			"elts": b.convertExprList(n.Elts),
			"ctx":  b.exprCtx(n.Ctx),
		}, n.Pos)
	case *ast.Tuple:
		return b.withPos("Tuple", map[string]objects.Object{
			"elts": b.convertExprList(n.Elts),
			"ctx":  b.exprCtx(n.Ctx),
		}, n.Pos)
	case *ast.Slice:
		return b.withPos("Slice", map[string]objects.Object{
			"lower": b.convertExprOrNone(n.Lower),
			"upper": b.convertExprOrNone(n.Upper),
			"step":  b.convertExprOrNone(n.Step),
		}, n.Pos)
	}
	// Unknown expr type: return a None constant as a safe fallback.
	pos := ast.Pos{}
	if positioner, ok := e.(interface{ Position() ast.Pos }); ok {
		pos = positioner.Position()
	}
	return b.withPos("Constant", map[string]objects.Object{
		"value": objects.None(),
		"kind":  objects.None(),
	}, pos)
}

func (b *astBridge) convertComprehensions(seq ast.Seq[*ast.Comprehension]) objects.Object {
	items := make([]objects.Object, 0, len(seq))
	for _, c := range seq {
		if c == nil {
			continue
		}
		items = append(items, b.newInst("comprehension", map[string]objects.Object{
			"target":   b.convertExpr(c.Target),
			"iter":     b.convertExpr(c.Iter),
			"ifs":      b.convertExprList(c.Ifs),
			"is_async": objects.NewInt(int64(c.IsAsync)),
		}))
	}
	return objects.NewList(items)
}

// withPos allocates a new _ast instance and stamps lineno/col_offset/
// end_lineno/end_col_offset from the Go source position.
//
// CPython: Python/Python-ast.c (all AST constructors set these fields)
func (b *astBridge) withPos(name string, attrs map[string]objects.Object, pos ast.Pos) objects.Object {
	if pos.Lineno > 0 {
		attrs["lineno"] = objects.NewInt(int64(pos.Lineno))
		attrs["col_offset"] = objects.NewInt(int64(pos.ColOffset))
		attrs["end_lineno"] = objects.NewInt(int64(pos.EndLineno))
		attrs["end_col_offset"] = objects.NewInt(int64(pos.EndColOffset))
	}
	return b.newInst(name, attrs)
}

// convertConstantValue converts a Go constant value to a Python object.
func (b *astBridge) convertConstantValue(v any) objects.Object {
	switch x := v.(type) {
	case int64:
		return objects.NewInt(x)
	case *big.Int:
		return objects.NewIntFromBig(x)
	case float64:
		return objects.NewFloat(x)
	case string:
		return objects.NewStr(x)
	case bool:
		return objects.NewBool(x)
	case []byte:
		return objects.NewBytes(x)
	case complex128:
		return objects.NewComplex(real(x), imag(x))
	case complex64:
		return objects.NewComplex(float64(real(x)), float64(imag(x)))
	case ast.EllipsisType:
		_ = x
		return objects.Ellipsis()
	case []any:
		items := make([]objects.Object, len(x))
		for i, elem := range x {
			items[i] = b.convertConstantValue(elem)
		}
		return objects.NewTuple(items)
	case ast.FrozenSet:
		items := make([]objects.Object, len(x))
		for i, elem := range x {
			items[i] = b.convertConstantValue(elem)
		}
		fs, err := objects.NewFrozenset(items)
		if err != nil {
			return objects.None()
		}
		return fs
	}
	return objects.None()
}

// convertOperator converts a Go AST operator to a Python _ast operator instance.
func (b *astBridge) convertOperator(op ast.Operator) objects.Object {
	switch op {
	case ast.Add:
		return b.newInst("Add", nil)
	case ast.Sub:
		return b.newInst("Sub", nil)
	case ast.Mult:
		return b.newInst("Mult", nil)
	case ast.Div:
		return b.newInst("Div", nil)
	case ast.ModOperator:
		return b.newInst("Mod", nil)
	case ast.Pow:
		return b.newInst("Pow", nil)
	case ast.BitOr:
		return b.newInst("BitOr", nil)
	case ast.BitAnd:
		return b.newInst("BitAnd", nil)
	case ast.BitXor:
		return b.newInst("BitXor", nil)
	case ast.FloorDiv:
		return b.newInst("FloorDiv", nil)
	case ast.LShift:
		return b.newInst("LShift", nil)
	case ast.RShift:
		return b.newInst("RShift", nil)
	case ast.MatMult:
		return b.newInst("MatMult", nil)
	}
	return b.newInst("Add", nil)
}

// convertBoolop converts a boolop to the matching _ast instance.
func (b *astBridge) convertBoolop(op ast.Boolop) objects.Object {
	switch op {
	case ast.And:
		return b.newInst("And", nil)
	case ast.Or:
		return b.newInst("Or", nil)
	}
	return b.newInst("And", nil)
}

// convertUnaryop converts a unaryop to the matching _ast instance.
func (b *astBridge) convertUnaryop(op ast.Unaryop) objects.Object {
	switch op {
	case ast.Invert:
		return b.newInst("Invert", nil)
	case ast.Not:
		return b.newInst("Not", nil)
	case ast.UAdd:
		return b.newInst("UAdd", nil)
	case ast.USub:
		return b.newInst("USub", nil)
	}
	return b.newInst("Not", nil)
}

// convertCmpops converts a sequence of cmpops to a list of _ast instances.
func (b *astBridge) convertCmpops(ops ast.Seq[ast.Cmpop]) objects.Object {
	items := make([]objects.Object, 0, len(ops))
	for _, op := range ops {
		switch op {
		case ast.Eq:
			items = append(items, b.newInst("Eq", nil))
		case ast.NotEq:
			items = append(items, b.newInst("NotEq", nil))
		case ast.Lt:
			items = append(items, b.newInst("Lt", nil))
		case ast.LtE:
			items = append(items, b.newInst("LtE", nil))
		case ast.Gt:
			items = append(items, b.newInst("Gt", nil))
		case ast.GtE:
			items = append(items, b.newInst("GtE", nil))
		case ast.Is:
			items = append(items, b.newInst("Is", nil))
		case ast.IsNot:
			items = append(items, b.newInst("IsNot", nil))
		case ast.In:
			items = append(items, b.newInst("In", nil))
		case ast.NotIn:
			items = append(items, b.newInst("NotIn", nil))
		default:
			items = append(items, b.newInst("Eq", nil))
		}
	}
	return objects.NewList(items)
}

// exprCtx converts a Go ExprContext to the matching _ast ctx instance.
//
// CPython: Python/Python-ast.c (context fields on Name, Attribute, etc.)
func (b *astBridge) exprCtx(ctx ast.ExprContext) objects.Object {
	switch ctx {
	case ast.Store:
		return b.newInst("Store", nil)
	case ast.Del:
		return b.newInst("Del", nil)
	default:
		return b.newInst("Load", nil)
	}
}

// typeCommentObj converts a *string type_comment to a Python str or None.
func typeCommentObj(tc *string) objects.Object {
	if tc == nil {
		return objects.None()
	}
	return objects.NewStr(*tc)
}

// strOrNone converts a *string to str or None.
func strOrNone(s *string) objects.Object {
	if s == nil {
		return objects.None()
	}
	return objects.NewStr(*s)
}
