// Docstring extraction ported from cpython/Lib/ast.py:294 get_docstring
// plus the inspect.cleandoc helper it delegates to.

package ast

import (
	"strings"
)

// GetDocstring ports cpython/Lib/ast.py:294 get_docstring. Returns the
// docstring of node (Module/FunctionDef/AsyncFunctionDef/ClassDef) and
// ok=true; ok=false signals "no docstring" (empty body, first stmt is
// not a string-literal Expr, or node is not a docstring-bearing type).
// When clean is true, the result is post-processed by cleandoc to
// strip uniform leading whitespace.
func GetDocstring(node any, clean bool) (string, bool) {
	var body Seq[Stmt]
	switch n := node.(type) {
	case *Module:
		body = n.Body
	case *FunctionDef:
		body = n.Body
	case *AsyncFunctionDef:
		body = n.Body
	case *ClassDef:
		body = n.Body
	default:
		return "", false
	}
	if len(body) == 0 {
		return "", false
	}
	es, ok := body[0].(*ExprStmt)
	if !ok {
		return "", false
	}
	c, ok := es.Value.(*Constant)
	if !ok {
		return "", false
	}
	s, ok := c.Value.(string)
	if !ok {
		return "", false
	}
	if clean {
		s = cleandoc(s)
	}
	return s, true
}

// cleandoc ports cpython/Lib/inspect.py inspect.cleandoc. Expands tabs,
// strips uniform leading whitespace from every line after the first,
// and trims leading and trailing blank lines.
func cleandoc(doc string) string {
	doc = expandTabs(doc, 8)
	lines := strings.Split(doc, "\n")
	margin := -1
	for _, line := range lines[min(1, len(lines)):] {
		stripped := strings.TrimLeft(line, " ")
		if stripped == "" {
			continue
		}
		indent := len(line) - len(stripped)
		if margin == -1 || indent < margin {
			margin = indent
		}
	}
	if len(lines) > 0 {
		lines[0] = strings.TrimLeft(lines[0], " ")
	}
	if margin >= 0 {
		for i := 1; i < len(lines); i++ {
			if len(lines[i]) >= margin {
				lines[i] = lines[i][margin:]
			}
		}
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	return strings.Join(lines, "\n")
}

// expandTabs mirrors Python str.expandtabs(tabsize). Replaces each tab
// with the number of spaces needed to reach the next multiple of
// tabsize, counting from the most recent line break.
func expandTabs(s string, tabsize int) string {
	var b strings.Builder
	col := 0
	for _, r := range s {
		switch r {
		case '\t':
			n := tabsize - col%tabsize
			for i := 0; i < n; i++ {
				b.WriteByte(' ')
			}
			col += n
		case '\n', '\r':
			b.WriteRune(r)
			col = 0
		default:
			b.WriteRune(r)
			col++
		}
	}
	return b.String()
}
