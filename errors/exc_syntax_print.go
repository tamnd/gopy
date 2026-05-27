package errors

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/tamnd/gopy/objects"
)

// formatSyntaxError mirrors TracebackException._format_syntax_error
// from Lib/traceback.py. SyntaxError prints differently from every
// other exception: the type name lands at the END of a multi-line
// block that names the file, dumps the offending source line, and
// drops a caret under the bad column.
//
// CPython: Lib/traceback.py:1376 TracebackException._format_syntax_error
func formatSyntaxError(exc *Exception) string {
	state := exc.SyntaxErr
	stype := exc.TypeName()
	if state == nil {
		return stype + ": " + excDisplayMessage(exc) + "\n"
	}
	var b strings.Builder
	filenameSuffix := ""
	if lineno, ok := syntaxInt(state.Lineno); ok {
		filename := "<string>"
		if s, ok := syntaxStrField(state.Filename); ok && s != "" {
			filename = s
		}
		fmt.Fprintf(&b, "  File \"%s\", line %d\n", filename, lineno)
	} else if s, ok := syntaxStrField(state.Filename); ok && s != "" {
		filenameSuffix = " (" + s + ")"
	}
	if text, ok := syntaxStrField(state.Text); ok {
		writeSyntaxBody(&b, text, state)
	}
	msg := "<no detail available>"
	if state.Msg != nil && !objects.IsNone(state.Msg) {
		s, err := objects.Str(state.Msg)
		if err == nil {
			msg = s
		}
	}
	fmt.Fprintf(&b, "%s: %s%s\n", stype, msg, filenameSuffix)
	return b.String()
}

// writeSyntaxBody emits the source line plus the caret line under it.
// Mirrors the middle block of _format_syntax_error that lays out the
// `    <ltext>\n    <caretspace>^^^\n` rows; lifted out so the parent
// function's cognitive complexity stays in budget.
//
// CPython: Lib/traceback.py:1396 source line + caret emission
func writeSyntaxBody(b *strings.Builder, text string, state *SyntaxErrorState) {
	rtext := strings.TrimRight(text, "\n")
	ltext := strings.TrimLeft(rtext, " \n\f")
	spaces := len(rtext) - len(ltext)
	offset, hasOffset := syntaxInt(state.Offset)
	if !hasOffset {
		fmt.Fprintf(b, "    %s\n", ltext)
		return
	}
	offset, endOffset := clampSyntaxOffsets(offset, text, rtext, state)
	colno := offset - 1 - spaces
	endColno := endOffset - 1 - spaces
	if colno < 0 {
		fmt.Fprintf(b, "    %s\n", ltext)
		return
	}
	caretSpace := caretIndent(ltext, colno)
	fmt.Fprintf(b, "    %s\n", ltext)
	fmt.Fprintf(b, "    %s%s\n", caretSpace, strings.Repeat("^", endColno-colno))
}

// clampSyntaxOffsets resolves the (offset, end_offset) pair into a
// well-formed range. Mirrors the C-side clamping CPython does before
// it computes colno / end_colno: cross-line spans collapse to the
// trailing newline column, missing end offsets default to a 1-column
// caret, and bogus negatives are normalized.
//
// CPython: Lib/traceback.py:1402 _format_syntax_error offset clamps
func clampSyntaxOffsets(offset int, text, rtext string, state *SyntaxErrorState) (int, int) {
	endOffset, hasEndOff := syntaxInt(state.EndOffset)
	lineno, _ := syntaxInt(state.Lineno)
	endLineno, _ := syntaxInt(state.EndLineno)
	if lineno == endLineno {
		if !hasEndOff || endOffset == 0 {
			endOffset = offset
		}
	} else {
		endOffset = len(rtext) + 1
	}
	if offset > len(text) {
		offset = len(rtext) + 1
	}
	if endOffset > len(text) {
		endOffset = len(rtext) + 1
	}
	if offset >= endOffset || endOffset < 0 {
		endOffset = offset + 1
	}
	return offset, endOffset
}

// caretIndent builds the leading whitespace for the caret line.
// CPython preserves non-space whitespace (tabs) so the caret aligns
// with the source line under proportional / tab-stop rendering.
// colno counts Unicode code points (SyntaxError.offset is 1-indexed
// codepoint offset), so slicing must walk runes, not bytes.
//
// CPython: Lib/traceback.py:1437 caretspace generator
func caretIndent(ltext string, colno int) string {
	var b strings.Builder
	count := 0
	for _, r := range ltext {
		if count >= colno {
			break
		}
		count++
		if unicode.IsSpace(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
	}
	return b.String()
}

// syntaxInt unwraps a SyntaxError member that may be None or *Int.
// Returns (n, true) when the field holds a positive int.
func syntaxInt(v objects.Object) (int, bool) {
	if v == nil || objects.IsNone(v) {
		return 0, false
	}
	i, ok := v.(*objects.Int)
	if !ok {
		return 0, false
	}
	n, _ := i.Int64()
	return int(n), true
}

// syntaxStrField unwraps a SyntaxError member that may be None or *Unicode.
func syntaxStrField(v objects.Object) (string, bool) {
	if v == nil || objects.IsNone(v) {
		return "", false
	}
	u, ok := v.(*objects.Unicode)
	if !ok {
		return "", false
	}
	return u.Value(), true
}
