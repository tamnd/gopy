// Source-location helpers ported from cpython/Lib/ast.py. Operate on
// the per-node Pos field (Stmt/Expr/Pattern/TypeParam/Excepthandler);
// node types without a Pos field (Module/Interactive/Expression/
// FunctionType) silently no-op, matching CPython's behavior on nodes
// whose `_attributes` tuple is empty.

package ast

import (
	"reflect"
	"regexp"
	"strings"
)

// CopyLocation ports cpython/Lib/ast.py:193 copy_location. Copies the
// source-position quadruple from oldNode to newNode and returns
// newNode. If either node lacks a Pos field, returns newNode
// unchanged.
func CopyLocation(newNode, oldNode any) any {
	nv := posValue(newNode)
	ov := posValue(oldNode)
	if !nv.IsValid() || !ov.IsValid() {
		return newNode
	}
	nv.Set(ov)
	return newNode
}

// FixMissingLocations ports cpython/Lib/ast.py:210 fix_missing_locations.
// Walks the tree and fills any zero-valued position field on a node
// from the nearest ancestor that has a non-zero value. Returns node.
func FixMissingLocations(node any) any {
	var fix func(n any, parent Pos)
	fix = func(n any, parent Pos) {
		f := posValue(n)
		if f.IsValid() {
			p := f.Interface().(Pos)
			if p.Lineno == 0 {
				p.Lineno = parent.Lineno
			} else {
				parent.Lineno = p.Lineno
			}
			if p.ColOffset == 0 {
				p.ColOffset = parent.ColOffset
			} else {
				parent.ColOffset = p.ColOffset
			}
			if p.EndLineno == 0 {
				p.EndLineno = parent.EndLineno
			} else {
				parent.EndLineno = p.EndLineno
			}
			if p.EndColOffset == 0 {
				p.EndColOffset = parent.EndColOffset
			} else {
				parent.EndColOffset = p.EndColOffset
			}
			f.Set(reflect.ValueOf(p))
		}
		for _, c := range IterChildNodes(n) {
			fix(c, parent)
		}
	}
	fix(node, Pos{Lineno: 1, ColOffset: 0, EndLineno: 1, EndColOffset: 0})
	return node
}

// IncrementLineno ports cpython/Lib/ast.py:245 increment_lineno. Adds
// n to lineno and end_lineno on every node in the subtree, leaving
// columns alone. Returns node.
func IncrementLineno(node any, n int) any {
	for _, child := range Walk(node) {
		f := posValue(child)
		if !f.IsValid() {
			continue
		}
		p := f.Interface().(Pos)
		p.Lineno += n
		if p.EndLineno != 0 {
			p.EndLineno += n
		}
		f.Set(reflect.ValueOf(p))
	}
	return node
}

// posValue returns the Pos field of v as a settable reflect.Value, or
// the zero Value if v is not a pointer-to-struct AST node with a Pos
// field.
func posValue(v any) reflect.Value {
	if v == nil {
		return reflect.Value{}
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() || rv.Elem().Kind() != reflect.Struct {
		return reflect.Value{}
	}
	f := rv.Elem().FieldByName("Pos")
	if !f.IsValid() || !f.CanSet() {
		return reflect.Value{}
	}
	return f
}

// linePattern matches one line including its terminator, or the
// trailing partial line at end of input. Mirrors the regex compiled
// in cpython/Lib/ast.py:328 _splitlines_no_ff.
var linePattern = regexp.MustCompile(`(?s).*?(?:\r\n|\n|\r|$)`)

// splitlinesNoFF ports cpython/Lib/ast.py:319 _splitlines_no_ff. Splits
// source on \n, \r, and \r\n only - form-feed and other Unicode line
// separators stay inline, matching how the Python parser counts lines.
func splitlinesNoFF(source string, maxlines int) []string {
	matches := linePattern.FindAllString(source, -1)
	var out []string
	for i, m := range matches {
		if maxlines > 0 && i+1 > maxlines {
			break
		}
		if m == "" && i == len(matches)-1 {
			break
		}
		out = append(out, m)
	}
	return out
}

// padWhitespace ports cpython/Lib/ast.py:338 _pad_whitespace. Replaces
// every char in source that is not \f or \t with a space.
func padWhitespace(source string) string {
	out := make([]byte, len(source))
	for i := 0; i < len(source); i++ {
		c := source[i]
		if c == '\f' || c == '\t' {
			out[i] = c
		} else {
			out[i] = ' '
		}
	}
	return string(out)
}

// GetSourceSegment ports cpython/Lib/ast.py:349 get_source_segment.
// Returns the source code segment that produced node, or ok=false if
// any required position attribute (lineno, end_lineno, col_offset,
// end_col_offset) is missing. With padded=true, the first line of a
// multi-line statement is padded with spaces to its original column.
func GetSourceSegment(source string, node any, padded bool) (string, bool) {
	f := posValue(node)
	if !f.IsValid() {
		return "", false
	}
	p := f.Interface().(Pos)
	if p.EndLineno == 0 || p.Lineno == 0 {
		return "", false
	}
	lineno := p.Lineno - 1
	endLineno := p.EndLineno - 1
	colOffset := p.ColOffset
	endColOffset := p.EndColOffset

	lines := splitlinesNoFF(source, endLineno+1)
	if endLineno >= len(lines) || lineno >= len(lines) || lineno < 0 {
		return "", false
	}
	if endLineno == lineno {
		bs := []byte(lines[lineno])
		if colOffset > len(bs) || endColOffset > len(bs) || colOffset > endColOffset {
			return "", false
		}
		return string(bs[colOffset:endColOffset]), true
	}

	firstBytes := []byte(lines[lineno])
	lastBytes := []byte(lines[endLineno])
	if colOffset > len(firstBytes) || endColOffset > len(lastBytes) {
		return "", false
	}
	pad := ""
	if padded {
		pad = padWhitespace(string(firstBytes[:colOffset]))
	}
	first := pad + string(firstBytes[colOffset:])
	last := string(lastBytes[:endColOffset])
	mid := lines[lineno+1 : endLineno]
	parts := make([]string, 0, len(mid)+2)
	parts = append(parts, first)
	parts = append(parts, mid...)
	parts = append(parts, last)
	return strings.Join(parts, ""), true
}
