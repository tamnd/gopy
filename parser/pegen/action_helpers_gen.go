// Hand-written action helpers that the generated parser calls into.
// The generator emits panic-stubs for every actionAst*/actionPgen*
// reference it scans in the bodies; the names defined here are
// excluded from the stub set so the real implementations win.
//
// This file is the seam where the typed AST surface meets the
// untyped any returned by per-rule parsers. Each helper does the
// type assertions and surfaces a real ast.* node.
//
// CPython: Parser/action_helpers.c, Parser/pegen.c

package pegen

import (
	"github.com/tamnd/gopy/ast"
)

// actionPgenMakeModule corresponds to _PyPegen_make_module from
// pegen.c: build a Module from the optional statements list.
// Variadic shape mirrors the generated stub signature; the call
// site passes (p, p, a) where the inner p is the C-action's
// explicit p arg and a is the statements slice (or nil).
//
// CPython: Parser/pegen.c:1203 _PyPegen_make_module
func actionPgenMakeModule(p *Parser, args ...any) any {
	_ = p
	var body ast.Seq[ast.Stmt]
	if len(args) >= 2 {
		body = stmtSeqOf(args[1])
	}
	return &ast.Module{Body: body}
}

// stmtSeqOf coerces an any (the result of parseRule_statements or
// similar) into a Seq[Stmt]. The generated parser stores rule
// results as []any; until the action translator is fully typed we
// pull them apart at the boundary.
func stmtSeqOf(v any) ast.Seq[ast.Stmt] {
	switch x := v.(type) {
	case nil:
		return nil
	case ast.Seq[ast.Stmt]:
		return x
	case []ast.Stmt:
		return ast.Seq[ast.Stmt](x)
	case []any:
		out := make(ast.Seq[ast.Stmt], 0, len(x))
		for _, e := range x {
			if s, ok := e.(ast.Stmt); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
