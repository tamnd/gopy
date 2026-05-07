// Structural AST comparison ported from cpython/Lib/ast.py:400 compare.
// Used by tooling that needs to assert "these two parse trees are the
// same" without comparing source positions.

package ast

import (
	"reflect"
)

// Equal ports cpython/Lib/ast.py:400 compare. Returns true iff a and
// b are the same AST type and every field is recursively equal. When
// compareAttributes is true, the source-position quadruple (lineno,
// col_offset, end_lineno, end_col_offset) must also match.
//
// CPython names this function `ast.compare`; gopy uses `Equal`
// because `Compare` is taken by the asdl Compare expr-node (`a < b`).
func Equal(a, b any, compareAttributes bool) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if reflect.TypeOf(a) != reflect.TypeOf(b) {
		return false
	}
	av := reflect.ValueOf(a)
	bv := reflect.ValueOf(b)
	for av.Kind() == reflect.Pointer || av.Kind() == reflect.Interface {
		if av.IsNil() && bv.IsNil() {
			return true
		}
		if av.IsNil() != bv.IsNil() {
			return false
		}
		av = av.Elem()
		bv = bv.Elem()
	}
	if av.Kind() != reflect.Struct {
		return reflect.DeepEqual(a, b)
	}
	t := av.Type()
	for i := 0; i < t.NumField(); i++ {
		if t.Field(i).Name == "Pos" && !compareAttributes {
			continue
		}
		if !compareValues(av.Field(i).Interface(), bv.Field(i).Interface(), compareAttributes) {
			return false
		}
	}
	return true
}

// compareValues handles the recursive case for non-struct field
// values: AST nodes recurse through Equal, slices are compared
// elementwise, everything else falls through to reflect.DeepEqual.
func compareValues(a, b any, attrs bool) bool {
	if isASTNode(a) {
		return Equal(a, b, attrs)
	}
	av := reflect.ValueOf(a)
	bv := reflect.ValueOf(b)
	if av.Kind() == reflect.Slice && bv.Kind() == reflect.Slice {
		if av.Len() != bv.Len() {
			return false
		}
		for i := 0; i < av.Len(); i++ {
			if !compareValues(av.Index(i).Interface(), bv.Index(i).Interface(), attrs) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(a, b)
}
