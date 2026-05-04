// Package traceback ports cpython/Python/traceback.c. v0.3 ships the
// data shape, the format/print entry points, and a minimal frameless
// builder used by the errors package to attach traceback rows from Go
// call sites. Real frames join in v0.6 once the VM lands.
package traceback

import (
	"fmt"
	"strings"
)

// Entry is one line of a traceback. Mirrors the data carried by
// PyTracebackObject (filename, line number, function name).
//
// CPython: Objects/frameobject.h (analog) and Python/traceback.c
type Entry struct {
	File string
	Line int
	Name string
}

// Traceback is the linked list of entries that backs Python's
// `__traceback__`. The next pointer chains older frames; CPython
// stores the chain in reverse, with the newest frame at the head.
//
// CPython: Include/cpython/traceback.h:L9 PyTracebackObject
type Traceback struct {
	Entry Entry
	Next  *Traceback
}

// New builds a single-entry traceback. The errors package uses this
// when v0.3 runtime code wants to attach a position from Go.
//
// CPython: Python/traceback.c:L154 PyTraceBack_Here
func New(entry Entry) *Traceback {
	return &Traceback{Entry: entry}
}

// Push prepends a new entry. The result becomes the new head.
//
// CPython: Python/traceback.c:L154 PyTraceBack_Here
func Push(tb *Traceback, entry Entry) *Traceback {
	return &Traceback{Entry: entry, Next: tb}
}

// Format returns the multi-line traceback string, oldest entry first.
// Mirrors traceback.format_tb output.
//
// CPython: Python/traceback.c:L985 _PyTraceBack_FromFrame
func Format(tb *Traceback) string {
	if tb == nil {
		return ""
	}
	var entries []Entry
	for cur := tb; cur != nil; cur = cur.Next {
		entries = append(entries, cur.Entry)
	}
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	var b strings.Builder
	b.WriteString("Traceback (most recent call last):\n")
	for _, e := range entries {
		fmt.Fprintf(&b, "  File %q, line %d, in %s\n", e.File, e.Line, e.Name)
	}
	return b.String()
}

// FormatException prepends typeName: message to a Format(tb) output,
// matching `traceback.format_exception` for a single (no-chain)
// exception. The errors package walks the cause/context chain itself
// and calls FormatException per node.
//
// CPython: Python/traceback.c:L1129 _PyTraceBack_Print
func FormatException(tb *Traceback, typeName, message string) string {
	body := Format(tb)
	if message == "" {
		return body + typeName + "\n"
	}
	return body + typeName + ": " + message + "\n"
}
