// Package traceback ports cpython/Python/traceback.c. v0.3 ships the
// data shape, the format/print entry points, and a minimal frameless
// builder used by the errors package to attach traceback rows from Go
// call sites. Real frames join in v0.6 once the VM lands.
package traceback

import (
	"fmt"
	"strings"

	"github.com/tamnd/gopy/objects"
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
// In CPython this is the PyTracebackObject struct exposed as the
// builtin `traceback` type. The Entry field carries the position
// data the original v0.3 port used (file/line/name); TbFrame and
// TbLasti are the slots that match CPython's tb_frame / tb_lasti
// once a real frame is available. Either the Entry path or the
// TbFrame path is enough to drive Format.
//
// CPython: Include/cpython/traceback.h:L9 PyTracebackObject
type Traceback struct {
	objects.Header
	Entry   Entry
	Next    *Traceback
	TbFrame objects.Object
	TbLasti int
}

// Type is the type singleton for the `traceback` builtin type.
//
// CPython: Python/traceback.c:269 PyTraceBack_Type
var Type *objects.Type

func init() {
	Type = objects.NewType("traceback", []*objects.Type{objects.ObjectType()})
	Type.Getattro = tracebackGetattr
	Type.Setattro = tracebackSetattr
	Type.Repr = tracebackRepr
	Type.Str = tracebackRepr
}

// New builds a single-entry traceback. The errors package uses this
// when v0.3 runtime code wants to attach a position from Go.
//
// CPython: Python/traceback.c:L154 PyTraceBack_Here
func New(entry Entry) *Traceback {
	tb := &Traceback{Entry: entry}
	tb.Init(Type)
	return tb
}

// Push prepends a new entry. The result becomes the new head.
//
// CPython: Python/traceback.c:L154 PyTraceBack_Here
func Push(tb *Traceback, entry Entry) *Traceback {
	out := &Traceback{Entry: entry, Next: tb}
	out.Init(Type)
	return out
}

// tracebackGetattr serves the type's tb_frame / tb_lasti / tb_lineno
// / tb_next attributes. Mirrors the union of tb_memberlist and
// tb_getsetters in CPython.
//
// CPython: Python/traceback.c:229 tb_memberlist
// CPython: Python/traceback.c:235 tb_getsetters
func tracebackGetattr(o objects.Object, name objects.Object) (objects.Object, error) {
	tb := o.(*Traceback)
	n, ok := name.(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: attribute name must be string, not '%s'", name.Type().Name)
	}
	switch n.Value() {
	case "tb_frame":
		if tb.TbFrame == nil {
			return objects.None(), nil
		}
		return tb.TbFrame, nil
	case "tb_lasti":
		return objects.NewInt(int64(tb.TbLasti)), nil
	case "tb_lineno":
		return objects.NewInt(int64(tb.Entry.Line)), nil
	case "tb_next":
		if tb.Next == nil {
			return objects.None(), nil
		}
		return tb.Next, nil
	}
	return nil, fmt.Errorf("AttributeError: 'traceback' object has no attribute %q", n.Value())
}

// tracebackSetattr handles tb_next assignment, which is the only
// settable attribute CPython exposes through tb_getsetters.
//
// CPython: Python/traceback.c:148 traceback_tb_next_set
func tracebackSetattr(o objects.Object, name, value objects.Object) error {
	tb := o.(*Traceback)
	n, ok := name.(*objects.Unicode)
	if !ok {
		return fmt.Errorf("TypeError: attribute name must be string, not '%s'", name.Type().Name)
	}
	if n.Value() != "tb_next" {
		return fmt.Errorf("AttributeError: 'traceback' object attribute %q is read-only", n.Value())
	}
	if value == nil || value == objects.None() {
		tb.Next = nil
		return nil
	}
	next, ok := value.(*Traceback)
	if !ok {
		return fmt.Errorf("TypeError: expected traceback object, got '%s'", value.Type().Name)
	}
	tb.Next = next
	return nil
}

// tracebackRepr returns a CPython-style identity repr.
//
// CPython: traceback objects have no tp_repr, so falling back to the
// generic <traceback object at 0x...> is the right shape.
func tracebackRepr(o objects.Object) (string, error) {
	tb := o.(*Traceback)
	return fmt.Sprintf("<traceback object at %p>", tb), nil
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
