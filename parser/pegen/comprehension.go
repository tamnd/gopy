// CPython: Parser/action_helpers.c comprehension section.
// Helpers that take the generator-clause list and produce the four
// comprehension node shapes (list, set, dict, generator).

package pegen

import "github.com/tamnd/gopy/ast"

// CompFor is the (target, iter, ifs, is_async) tuple a `for` clause
// produces inside a comprehension.
//
// CPython: Parser/pegen.h:127 CompFor
type CompFor = ast.Comprehension

// MakeListComp wraps an element expression and the for-clause list
// into a ListComp node.
//
// CPython: Parser/action_helpers.c:925 _PyPegen_make_listcomp
func MakeListComp(elt ast.Expr, gens []*ast.Comprehension) *ast.ListComp {
	return &ast.ListComp{Elt: elt, Generators: gens}
}

// MakeSetComp builds a SetComp node.
//
// CPython: Parser/action_helpers.c:935 _PyPegen_make_setcomp
func MakeSetComp(elt ast.Expr, gens []*ast.Comprehension) *ast.SetComp {
	return &ast.SetComp{Elt: elt, Generators: gens}
}

// MakeDictComp builds a DictComp node from the (key, value) head and
// the generator list.
//
// CPython: Parser/action_helpers.c:945 _PyPegen_make_dictcomp
func MakeDictComp(key, value ast.Expr, gens []*ast.Comprehension) *ast.DictComp {
	return &ast.DictComp{Key: key, Value: value, Generators: gens}
}

// MakeGeneratorExp builds a GeneratorExp node.
//
// CPython: Parser/action_helpers.c:955 _PyPegen_make_generatorexp
func MakeGeneratorExp(elt ast.Expr, gens []*ast.Comprehension) *ast.GeneratorExp {
	return &ast.GeneratorExp{Elt: elt, Generators: gens}
}

// MakeComprehension is the per-clause builder. The for_if_clause
// rule emits one of these and the comprehension rule glues several
// together into the Generators slice.
//
// CPython: Parser/action_helpers.c:885 _PyPegen_comprehension_for_clause
func MakeComprehension(target, iter ast.Expr, ifs []ast.Expr, isAsync bool) *ast.Comprehension {
	async := 0
	if isAsync {
		async = 1
	}
	return &ast.Comprehension{
		Target:  target,
		Iter:    iter,
		Ifs:     ifs,
		IsAsync: async,
	}
}
