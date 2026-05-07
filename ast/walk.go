// Tree walking primitives ported from cpython/Lib/ast.py. Together
// with dump.go these are the "ast module" surface a tool would use
// to traverse, inspect, and transform AST trees.

package ast

import (
	"reflect"
)

// Field is a (name, value) pair as yielded by IterFields. The name
// matches the asdl identifier for the field (e.g. "context_expr",
// "type_comment"), not the Go struct field name.
type Field struct {
	Name  string
	Value any
}

// IterFields ports cpython/Lib/ast.py:268 iter_fields. Yields one
// entry per declared field of node in source order, skipping the
// internal `Pos` source-position field (which CPython tracks under
// _attributes, not _fields).
func IterFields(node any) []Field {
	rv := reflect.ValueOf(node)
	for rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil
	}
	var out []Field
	t := rv.Type()
	for i := 0; i < t.NumField(); i++ {
		ft := t.Field(i)
		if ft.Name == "Pos" {
			continue
		}
		out = append(out, Field{
			Name:  asdlFieldName(t.Name(), ft.Name),
			Value: rv.Field(i).Interface(),
		})
	}
	return out
}

// IterChildNodes ports cpython/Lib/ast.py:280 iter_child_nodes. Yields
// every direct child of node that is itself an AST node, plus every
// item of any list-valued field that is an AST node.
func IterChildNodes(node any) []any {
	var out []any
	for _, f := range IterFields(node) {
		if isASTNode(f.Value) {
			out = append(out, f.Value)
			continue
		}
		rv := reflect.ValueOf(f.Value)
		if rv.Kind() == reflect.Slice {
			for i := 0; i < rv.Len(); i++ {
				v := rv.Index(i).Interface()
				if isASTNode(v) {
					out = append(out, v)
				}
			}
		}
	}
	return out
}

// Walk ports cpython/Lib/ast.py:386 walk. Returns every descendant of
// node (including node itself) in BFS order with no defined sibling
// ordering beyond what IterChildNodes returns.
func Walk(node any) []any {
	out := []any{node}
	for i := 0; i < len(out); i++ {
		out = append(out, IterChildNodes(out[i])...)
	}
	return out
}

// isASTNode reports whether v is one of the gopy AST types. Mirrors
// Python's `isinstance(v, AST)` predicate. The asdl product types
// (Arguments, Arg, Keyword, Alias, Withitem, MatchCase, Comprehension)
// are not on a sum-type marker interface, so we list them by hand.
func isASTNode(v any) bool {
	if v == nil {
		return false
	}
	switch v.(type) {
	case Mod, Stmt, Expr, Excepthandler, Pattern, TypeIgnore, TypeParam:
		return true
	case *Arguments, *Arg, *Keyword, *Alias, *Withitem, *MatchCase, *Comprehension:
		return true
	}
	return false
}

// nodeClassName returns the asdl class name for an AST node value,
// matching what CPython prints as `node.__class__.__name__`. Reuses
// the dump.go class-name table so tooling and dumps stay in sync.
func nodeClassName(node any) string {
	if node == nil {
		return ""
	}
	rv := reflect.ValueOf(node)
	for rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return ""
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return ""
	}
	return dumpClassName(rv.Type())
}
