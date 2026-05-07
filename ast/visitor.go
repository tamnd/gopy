// Visitor and transformer base types ported from cpython/Lib/ast.py:482
// NodeVisitor and cpython/Lib/ast.py:519 NodeTransformer. Python uses
// runtime attribute lookup to dispatch on class name; the gopy port
// uses an explicit handler map registered at construction time.

package ast

import (
	"reflect"
)

// VisitFunc is one node-class handler registered with a NodeVisitor.
// Returning a non-nil node from a NodeTransformer handler replaces
// the node in the parent; returning nil deletes it.
type VisitFunc func(node any) any

// NodeVisitor is the gopy port of cpython/Lib/ast.py:482 NodeVisitor.
// Walks the tree calling Handlers[ClassName](node) for each node, or
// GenericVisit if no handler is registered.
type NodeVisitor struct {
	Handlers map[string]VisitFunc
}

// Visit dispatches node to the registered handler for its class name,
// or to GenericVisit if no handler is registered. Mirrors visit() at
// cpython/Lib/ast.py:502.
func (v *NodeVisitor) Visit(node any) any {
	if h, ok := v.Handlers[nodeClassName(node)]; ok {
		return h(node)
	}
	return v.GenericVisit(node)
}

// GenericVisit calls Visit on every direct child of node and returns
// nil. Mirrors generic_visit() at cpython/Lib/ast.py:508.
func (v *NodeVisitor) GenericVisit(node any) any {
	for _, child := range IterChildNodes(node) {
		v.Visit(child)
	}
	return nil
}

// NodeTransformer is the gopy port of cpython/Lib/ast.py:519
// NodeTransformer. Handlers may return a replacement node, nil to
// delete a list element / clear an optional field, or the original
// node to leave it in place.
type NodeTransformer struct {
	Handlers map[string]VisitFunc
}

// Visit dispatches like NodeVisitor.Visit but uses the transformer's
// GenericVisit (which mutates child slices and fields in place).
func (t *NodeTransformer) Visit(node any) any {
	if h, ok := t.Handlers[nodeClassName(node)]; ok {
		return h(node)
	}
	return t.GenericVisit(node)
}

// GenericVisit walks the fields of node and rewrites child entries
// according to handler return values. Mirrors generic_visit() at
// cpython/Lib/ast.py:555.
func (t *NodeTransformer) GenericVisit(node any) any {
	rv := reflect.ValueOf(node)
	for rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return node
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return node
	}
	st := rv.Type()
	for i := 0; i < st.NumField(); i++ {
		ft := st.Field(i)
		if ft.Name == "Pos" {
			continue
		}
		fv := rv.Field(i)
		if fv.Kind() == reflect.Slice {
			t.transformSlice(fv)
			continue
		}
		if !isASTNode(fv.Interface()) {
			continue
		}
		newChild := t.Visit(fv.Interface())
		if newChild == nil {
			fv.Set(reflect.Zero(fv.Type()))
			continue
		}
		nv := reflect.ValueOf(newChild)
		if nv.Type().AssignableTo(fv.Type()) {
			fv.Set(nv)
		}
	}
	return node
}

// transformSlice walks one list-valued field, replacing each AST entry
// with the visitor's return value. nil deletes; a list-typed return
// is spliced in (CPython's "may return a list of nodes rather than
// just a single node" semantics). All other return values overwrite
// the entry.
func (t *NodeTransformer) transformSlice(slice reflect.Value) {
	if slice.Len() == 0 {
		return
	}
	out := reflect.MakeSlice(slice.Type(), 0, slice.Len())
	for i := 0; i < slice.Len(); i++ {
		item := slice.Index(i).Interface()
		if !isASTNode(item) {
			out = reflect.Append(out, slice.Index(i))
			continue
		}
		newItem := t.Visit(item)
		if newItem == nil {
			continue
		}
		nv := reflect.ValueOf(newItem)
		if nv.Kind() == reflect.Slice && !isASTNode(newItem) {
			for j := 0; j < nv.Len(); j++ {
				out = reflect.Append(out, nv.Index(j))
			}
			continue
		}
		if nv.Type().AssignableTo(slice.Type().Elem()) {
			out = reflect.Append(out, nv)
		} else {
			out = reflect.Append(out, slice.Index(i))
		}
	}
	slice.Set(out)
}
