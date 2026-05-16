// CWriter is a token-aware writer for C source. The cases_generator
// pipeline funnels every emitted byte through CWriter so the output
// keeps relative whitespace alignment with the DSL source while still
// tracking nesting indentation for inserted blocks. Mirrors
// Tools/cases_generator/cwriter.py CWriter.
package main

import (
	"fmt"
	"io"
	"strings"
)

// CWriter buffers C output with indent tracking, optional #line
// directives, and a small two-token spill / reload state machine the
// generators use to insert _PyFrame_{Set,Get}StackPointer pairs.
//
// CPython: Tools/cases_generator/cwriter.py:6-19 CWriter
type CWriter struct {
	out            io.Writer
	baseColumn     int
	indents        []int
	lineDirectives bool
	lastToken      *Token
	newline        bool
	pendingSpill   bool
	pendingReload  bool
}

// NewCWriter returns a CWriter rooted at indent (each indent step is 4
// columns) writing to out. lineDirectives=true emits #line markers
// when the source line jumps.
//
// CPython: Tools/cases_generator/cwriter.py:11-19 CWriter.__init__
func NewCWriter(out io.Writer, indent int, lineDirectives bool) *CWriter {
	indents := make([]int, indent+1)
	for i := 0; i <= indent; i++ {
		indents[i] = i * 4
	}
	return &CWriter{
		out:            out,
		baseColumn:     indent * 4,
		indents:        indents,
		lineDirectives: lineDirectives,
		newline:        true,
	}
}

// NullCWriter returns a CWriter that drops every byte. Used by the
// abstract-interpreter generator paths that re-run productions just
// to compute spill placement.
//
// CPython: Tools/cases_generator/cwriter.py:21-23 CWriter.null
func NullCWriter() *CWriter { return NewCWriter(io.Discard, 0, false) }

func (c *CWriter) write(s string) {
	if _, err := io.WriteString(c.out, s); err != nil {
		panic(err)
	}
}

// SetPosition advances the cursor to align with tkn: emits a newline
// when the source line jumps, indents to match, and writes a column
// gap for tokens on the same line.
//
// CPython: Tools/cases_generator/cwriter.py:25-39 set_position
func (c *CWriter) SetPosition(tkn Token) {
	if c.lastToken != nil {
		if c.lastToken.End.Line < tkn.Begin.Line {
			c.write("\n")
		}
		if c.lastToken.Begin.Line < tkn.Begin.Line {
			if c.lineDirectives {
				c.write(fmt.Sprintf("#line %d %q\n", tkn.Begin.Line, tkn.Filename))
			}
			c.write(strings.Repeat(" ", c.indents[len(c.indents)-1]))
		} else {
			gap := tkn.Begin.Col - c.lastToken.End.Col
			if gap < 0 {
				gap = 0
			}
			c.write(strings.Repeat(" ", gap))
		}
	} else if c.newline {
		c.write(strings.Repeat(" ", c.indents[len(c.indents)-1]))
	}
	t := tkn
	c.lastToken = &t
	c.newline = false
}

// EmitAt writes txt at tkn's position (after flushing any pending
// spill / reload).
//
// CPython: Tools/cases_generator/cwriter.py:41-44 emit_at
func (c *CWriter) EmitAt(txt string, where Token) {
	c.MaybeWriteSpill()
	c.SetPosition(where)
	c.write(txt)
}

// MaybeDedent pops indent levels for closing parens / braces or for
// label-shaped fragments.
//
// CPython: Tools/cases_generator/cwriter.py:46-52 maybe_dedent
func (c *CWriter) MaybeDedent(txt string) {
	parens := strings.Count(txt, "(") - strings.Count(txt, ")")
	if parens < 0 && len(c.indents) > 1 {
		c.indents = c.indents[:len(c.indents)-1]
	}
	braces := strings.Count(txt, "{") - strings.Count(txt, "}")
	if (braces < 0 || isLabel(txt)) && len(c.indents) > 1 {
		c.indents = c.indents[:len(c.indents)-1]
	}
}

// MaybeIndent pushes a new indent level for an opening paren / brace
// or for a label.
//
// CPython: Tools/cases_generator/cwriter.py:54-73 maybe_indent
func (c *CWriter) MaybeIndent(txt string) {
	parens := strings.Count(txt, "(") - strings.Count(txt, ")")
	if parens > 0 {
		offset := c.indents[len(c.indents)-1] + 4
		if c.lastToken != nil {
			lastOffset := c.lastToken.End.Col - 1
			if lastOffset > c.indents[len(c.indents)-1] && lastOffset <= 40 {
				offset = lastOffset
			}
		}
		c.indents = append(c.indents, offset)
	}
	if isLabel(txt) {
		c.indents = append(c.indents, c.indents[len(c.indents)-1]+4)
		return
	}
	braces := strings.Count(txt, "{") - strings.Count(txt, "}")
	if braces > 0 {
		if strings.Contains(txt, `extern "C"`) {
			c.indents = append(c.indents, c.indents[len(c.indents)-1])
		} else {
			c.indents = append(c.indents, c.indents[len(c.indents)-1]+4)
		}
	}
}

// EmitText writes raw text without indent or position tracking.
//
// CPython: Tools/cases_generator/cwriter.py:75-76 emit_text
func (c *CWriter) EmitText(txt string) { c.write(txt) }

// EmitMultilineComment re-indents a block comment so each line lines
// up with the current indent level (extra column for `* ...` lines).
//
// CPython: Tools/cases_generator/cwriter.py:78-94 emit_multiline_comment
func (c *CWriter) EmitMultilineComment(tkn Token) {
	c.SetPosition(tkn)
	lines := splitLinesKeepEnds(tkn.Text)
	first := true
	for _, line := range lines {
		text := strings.TrimLeft(line, " \t")
		var spaces int
		if first {
			spaces = 0
		} else {
			spaces = c.indents[len(c.indents)-1]
			if strings.HasPrefix(text, "*") {
				spaces++
			} else {
				spaces += 3
			}
		}
		first = false
		c.write(strings.Repeat(" ", spaces))
		c.write(text)
	}
}

// EmitToken writes one Token, dispatching multi-line comments through
// EmitMultilineComment and toggling newline mode for #-macros.
//
// CPython: Tools/cases_generator/cwriter.py:96-104 emit_token
func (c *CWriter) EmitToken(tkn Token) {
	if tkn.Kind == TokComment && strings.Contains(tkn.Text, "\n") {
		c.EmitMultilineComment(tkn)
		return
	}
	c.MaybeDedent(tkn.Text)
	c.SetPosition(tkn)
	c.EmitText(tkn.Text)
	if strings.HasPrefix(tkn.Kind, "CMACRO") {
		c.newline = true
	}
	c.MaybeIndent(tkn.Text)
}

// EmitStr writes a free-form string (no Token context). Re-indents
// when at the start of a line, and tracks indent based on braces.
//
// CPython: Tools/cases_generator/cwriter.py:106-116 emit_str
func (c *CWriter) EmitStr(txt string) {
	c.MaybeDedent(txt)
	if c.newline && txt != "" && txt[0] != '\n' {
		c.write(strings.Repeat(" ", c.indents[len(c.indents)-1]))
		c.newline = false
	}
	c.EmitText(txt)
	if strings.HasSuffix(txt, "\n") {
		c.newline = true
	}
	c.MaybeIndent(txt)
	c.lastToken = nil
}

// EmitToken or EmitStr wrapper, flushing pending spill state first.
//
// CPython: Tools/cases_generator/cwriter.py:118-125 emit
func (c *CWriter) Emit(v any) {
	c.MaybeWriteSpill()
	switch x := v.(type) {
	case Token:
		c.EmitToken(x)
	case *Token:
		c.EmitToken(*x)
	case string:
		c.EmitStr(x)
	default:
		panic(fmt.Sprintf("CWriter.Emit: unsupported type %T", v))
	}
}

// StartLine forces a newline before the next emit if we are not
// already at column zero.
//
// CPython: Tools/cases_generator/cwriter.py:127-131 start_line
func (c *CWriter) StartLine() {
	if !c.newline {
		c.write("\n")
	}
	c.newline = true
	c.lastToken = nil
}

// EmitSpill arms the pending spill edge (or cancels a pending reload).
//
// CPython: Tools/cases_generator/cwriter.py:133-138 emit_spill
func (c *CWriter) EmitSpill() {
	if c.pendingReload {
		c.pendingReload = false
		return
	}
	if c.pendingSpill {
		panic("CWriter: EmitSpill called twice")
	}
	c.pendingSpill = true
}

// MaybeWriteSpill flushes a pending spill or reload edge by emitting
// the matching _PyFrame_{Set,Get}StackPointer call.
//
// CPython: Tools/cases_generator/cwriter.py:140-146 maybe_write_spill
func (c *CWriter) MaybeWriteSpill() {
	switch {
	case c.pendingSpill:
		c.pendingSpill = false
		c.EmitStr("_PyFrame_SetStackPointer(frame, stack_pointer);\n")
	case c.pendingReload:
		c.pendingReload = false
		c.EmitStr("stack_pointer = _PyFrame_GetStackPointer(frame);\n")
	}
}

// EmitReload arms the pending reload edge (or cancels a pending spill).
//
// CPython: Tools/cases_generator/cwriter.py:148-153 emit_reload
func (c *CWriter) EmitReload() {
	if c.pendingSpill {
		c.pendingSpill = false
		return
	}
	if c.pendingReload {
		panic("CWriter: EmitReload called twice")
	}
	c.pendingReload = true
}

// HeaderGuard wraps body in #ifndef/#define/extern "C" boilerplate.
// Returns the closing string callers append once body is written.
//
// CPython: Tools/cases_generator/cwriter.py:155-175 header_guard
func (c *CWriter) HeaderGuardOpen(name string) {
	c.write(fmt.Sprintf(`
#ifndef %s
#define %s
#ifdef __cplusplus
extern "C" {
#endif

`, name, name))
}

// HeaderGuardClose emits the matching #endif/#endif tail of the guard
// HeaderGuardOpen began.
func (c *CWriter) HeaderGuardClose(name string) {
	c.write(fmt.Sprintf(`
#ifdef __cplusplus
}
#endif
#endif /* !%s */
`, name))
}

// isLabel reports whether txt is a C label (`identifier:`) but not a
// line comment.
//
// CPython: Tools/cases_generator/cwriter.py:178-179 is_label
func isLabel(txt string) bool {
	return !strings.HasPrefix(txt, "//") && strings.HasSuffix(txt, ":")
}

// splitLinesKeepEnds is Python's str.splitlines(keepends=True): split
// on \n but retain the terminator on each chunk.
func splitLinesKeepEnds(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i+1])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}
