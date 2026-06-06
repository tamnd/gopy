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
	// CPython exposes tb_frame/tb_lasti/tb_lineno as read-only members
	// and tb_next as a getset, all living in the type dict so that
	// generic attribute access and dir() see them. The gopy port mirrors
	// that with getset descriptors and the default generic getattr.
	//
	// CPython: Python/traceback.c:229 tb_memberlist
	// CPython: Python/traceback.c:235 tb_getsetters
	objects.SetTypeDescr(Type, "tb_frame", objects.NewGetSetDescr("tb_frame", tbGetFrame, nil))
	objects.SetTypeDescr(Type, "tb_lasti", objects.NewGetSetDescr("tb_lasti", tbGetLasti, nil))
	objects.SetTypeDescr(Type, "tb_lineno", objects.NewGetSetDescr("tb_lineno", tbGetLineno, nil))
	objects.SetTypeDescr(Type, "tb_next", objects.NewGetSetDescr("tb_next", tbGetNext, tbSetNext))
	// The traceback type ships its own __dir__ that returns exactly the
	// four attribute names, rather than inheriting object.__dir__ (which
	// would also surface every object dunder). dir(tb) is therefore the
	// fixed list below.
	//
	// CPython: Python/traceback.c:129 tb_dir
	objects.SetTypeDescr(Type, "__dir__", objects.NewMethodDescr(Type, "__dir__", tbDir))
	// Route attribute access through the generic getattr/setattr so the
	// getset descriptors above drive both reads and the single writable
	// slot (tb_next). CPython inherits tp_getattro/tp_setattro from object
	// (PyObject_GenericGetAttr/SetAttr) for the traceback type.
	//
	// CPython: Python/traceback.c:269 PyTraceBack_Type (slots left to object)
	Type.Getattro = objects.GenericGetAttr
	Type.Setattro = objects.GenericSetAttr
	Type.Repr = tracebackRepr
	Type.Str = tracebackRepr
	// TpTraverse visits TbFrame and Next so the cycle collector can walk
	// Exception→Traceback→Frame chains and identify unreachable cycles.
	// Mirrors CPython's tb_traverse which visits tb_frame and tb_next.
	//
	// CPython: Python/traceback.c:244 tb_traverse
	Type.TpTraverse = func(o objects.Object, visit objects.Visitor) error {
		tb := o.(*Traceback)
		if tb.TbFrame != nil {
			if err := visit(tb.TbFrame); err != nil {
				return err
			}
		}
		if tb.Next != nil {
			if err := visit(tb.Next); err != nil {
				return err
			}
		}
		return nil
	}
}

// tbDir backs the traceback __dir__ method. CPython returns the literal
// four-name list and nothing else.
//
// CPython: Python/traceback.c:129 tb_dir
func tbDir(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewList([]objects.Object{
		objects.NewStr("tb_frame"),
		objects.NewStr("tb_next"),
		objects.NewStr("tb_lasti"),
		objects.NewStr("tb_lineno"),
	}), nil
}

// New builds a single-entry traceback. The errors package uses this
// when v0.3 runtime code wants to attach a position from Go.
//
// CPython: Python/traceback.c:L154 PyTraceBack_Here
func New(entry Entry) *Traceback {
	tb := &Traceback{Entry: entry}
	tb.Init(Type)
	// CPython: Python/traceback.c:154 PyTraceBack_Here PyObject_GC_Track
	if h := objects.GCTrackHook; h != nil {
		h(tb)
	}
	return tb
}

// Push prepends a new entry. The result becomes the new head.
//
// CPython: Python/traceback.c:L154 PyTraceBack_Here
func Push(tb *Traceback, entry Entry) *Traceback {
	out := &Traceback{Entry: entry, Next: tb}
	out.Init(Type)
	// CPython: Python/traceback.c:154 PyTraceBack_Here PyObject_GC_Track
	if h := objects.GCTrackHook; h != nil {
		h(out)
	}
	return out
}

// tbGetFrame backs the tb_frame member (read-only).
//
// CPython: Python/traceback.c:229 tb_memberlist
func tbGetFrame(o objects.Object) (objects.Object, error) {
	tb := o.(*Traceback)
	if tb.TbFrame == nil {
		return objects.None(), nil
	}
	return tb.TbFrame, nil
}

// tbGetLasti backs the tb_lasti member (read-only).
//
// CPython: Python/traceback.c:229 tb_memberlist
func tbGetLasti(o objects.Object) (objects.Object, error) {
	tb := o.(*Traceback)
	return objects.NewInt(int64(tb.TbLasti)), nil
}

// tbGetLineno backs the tb_lineno member (read-only).
//
// CPython: Python/traceback.c:229 tb_memberlist
func tbGetLineno(o objects.Object) (objects.Object, error) {
	tb := o.(*Traceback)
	return objects.NewInt(int64(tb.Entry.Line)), nil
}

// tbGetNext backs the tb_next getset.
//
// CPython: Python/traceback.c:131 tb_next_get
func tbGetNext(o objects.Object) (objects.Object, error) {
	tb := o.(*Traceback)
	if tb.Next == nil {
		return objects.None(), nil
	}
	return tb.Next, nil
}

// tbSetNext backs the tb_next getset; it is the only settable
// traceback attribute CPython exposes.
//
// CPython: Python/traceback.c:148 traceback_tb_next_set
func tbSetNext(o objects.Object, value objects.Object) error {
	tb := o.(*Traceback)
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
