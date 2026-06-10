package errors

import (
	"fmt"
	"strings"

	"github.com/tamnd/gopy/objects"
)

// SyntaxErrorState carries the structured PyMemberDef payload CPython's
// PySyntaxErrorObject stores next to the base PyBaseException fields.
// Each field is an Object so the str/int/None storage round-trips
// through Python without coercion.
//
// CPython: Objects/exceptions.c:2670 PySyntaxErrorObject
type SyntaxErrorState struct {
	Msg              objects.Object
	Filename         objects.Object
	Lineno           objects.Object
	Offset           objects.Object
	Text             objects.Object
	EndLineno        objects.Object
	EndOffset        objects.Object
	PrintFileAndLine objects.Object
	Metadata         objects.Object
}

// IndentationError fires for any block-structure mismatch (the parser
// raises it instead of SyntaxError when the offending token is INDENT
// or DEDENT). TabError is a stricter form that only fires when the
// mix is between tabs and spaces; CPython routes the -tt flag through
// it.
//
// CPython: Objects/exceptions.c:2906 MiddlingExtendsException IndentationError
// CPython: Objects/exceptions.c:2913 MiddlingExtendsException TabError
var (
	PyExc_SyntaxError          = newSyntaxExcType("SyntaxError", []*objects.Type{PyExc_Exception})
	PyExc_IndentationError     = newSyntaxExcType("IndentationError", []*objects.Type{PyExc_SyntaxError})
	PyExc_TabError             = newSyntaxExcType("TabError", []*objects.Type{PyExc_IndentationError})
	PyExc_IncompleteInputError = newSyntaxExcType("_IncompleteInputError", []*objects.Type{PyExc_SyntaxError})
)

// newSyntaxExcType wires every SyntaxError subclass to syntaxExcTpNew so
// the Python-level call `SyntaxError(msg, (filename, lineno, ...))`
// flows through SyntaxError_init. excStr remains the default for
// no-arg / single-arg paths; multi-arg renders via syntaxStr.
//
// CPython: Objects/exceptions.c:2898 ComplexExtendsException SyntaxError
func newSyntaxExcType(name string, bases []*objects.Type) *objects.Type {
	t := newExcType(name, bases)
	t.TpNew = syntaxExcTpNew
	t.Str = syntaxStr
	return t
}

// syntaxExcTpNew mirrors excTpNew: allocate a fresh Exception with the
// positional args stored on .args, then run SyntaxError_init on it so
// the member fields are populated even when callers bypass tp_call.
//
// CPython: Objects/exceptions.c:42 BaseException_new
// CPython: Objects/exceptions.c:2713 SyntaxError_init
func syntaxExcTpNew(cls *objects.Type, args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	e := New(cls, objects.NewTuple(args))
	if err := syntaxErrorInit(e, args); err != nil {
		return nil, err
	}
	return e, nil
}

// syntaxErrorInit unpacks args = (msg, (filename, lineno, offset, text,
// end_lineno?, end_offset?, metadata?)) into the SyntaxErrorState. The
// 2-arg shape is the canonical raise(msg, info) tuple; the 1-arg shape
// stores msg only; the 0-arg shape leaves every field nil.
//
// CPython: Objects/exceptions.c:2713 SyntaxError_init
func syntaxErrorInit(e *Exception, args []objects.Object) error {
	if e.SyntaxErr == nil {
		e.SyntaxErr = &SyntaxErrorState{}
	}
	state := e.SyntaxErr
	if len(args) == 0 {
		return nil
	}
	state.Msg = args[0]
	if len(args) != 2 {
		return nil
	}
	info, err := tupleFromInfo(args[1])
	if err != nil {
		return err
	}
	if info.Len() < 4 {
		return fmt.Errorf("TypeError: function takes 4-7 arguments (%d given)", info.Len())
	}
	state.Filename = info.Item(0)
	state.Lineno = info.Item(1)
	state.Offset = info.Item(2)
	state.Text = info.Item(3)
	if info.Len() >= 5 {
		state.EndLineno = info.Item(4)
	}
	if info.Len() >= 6 {
		state.EndOffset = info.Item(5)
	}
	if info.Len() >= 7 {
		state.Metadata = info.Item(6)
	}
	if state.EndLineno != nil && state.EndOffset == nil {
		return fmt.Errorf("TypeError: end_offset must be provided when end_lineno is provided")
	}
	return nil
}

// tupleFromInfo runs PySequence_Tuple(info): pass-through for tuple,
// materialize via iteration otherwise.
//
// CPython: Objects/exceptions.c:2727 PySequence_Tuple(info)
func tupleFromInfo(info objects.Object) (*objects.Tuple, error) {
	if t, ok := info.(*objects.Tuple); ok {
		return t, nil
	}
	iter, err := objects.Iter(info)
	if err != nil {
		return nil, err
	}
	var items []objects.Object
	for {
		next, err := objects.IterNext(iter)
		if err != nil {
			return nil, err
		}
		if next == nil {
			break
		}
		items = append(items, next)
	}
	return objects.NewTuple(items), nil
}

// syntaxStr renders the SyntaxError instance as CPython does:
//   - no filename and no lineno: str(msg or None)
//   - filename + lineno: "<msg> (<basename>, line <n>)"
//   - filename only:     "<msg> (<basename>)"
//   - lineno only:       "<msg> (line <n>)"
//
// CPython: Objects/exceptions.c:2830 SyntaxError_str
func syntaxStr(o objects.Object) (string, error) {
	e, ok := o.(*Exception)
	if !ok {
		return excStr(o)
	}
	state := e.SyntaxErr
	if state == nil {
		return excStr(o)
	}
	haveFilename := state.Filename != nil && state.Filename.Type() == objects.StrType()
	haveLineno := state.Lineno != nil && state.Lineno.Type() == objects.IntType
	msgStr, err := messageString(state.Msg)
	if err != nil {
		return "", err
	}
	if !haveFilename && !haveLineno {
		return msgStr, nil
	}
	filename := ""
	if haveFilename {
		s, err := objects.Str(state.Filename)
		if err != nil {
			return "", err
		}
		filename = myBasename(s)
	}
	lineno := int64(0)
	if haveLineno {
		ln, _ := state.Lineno.(*objects.Int).Int64()
		lineno = ln
	}
	switch {
	case haveFilename && haveLineno:
		return fmt.Sprintf("%s (%s, line %d)", msgStr, filename, lineno), nil
	case haveFilename:
		return fmt.Sprintf("%s (%s)", msgStr, filename), nil
	default:
		return fmt.Sprintf("%s (line %d)", msgStr, lineno), nil
	}
}

// messageString renders the SyntaxError msg field. None / nil prints as
// "None" to match `PyObject_Str(self->msg ? self->msg : Py_None)`.
//
// CPython: Objects/exceptions.c:2854 SyntaxError_str
func messageString(msg objects.Object) (string, error) {
	if msg == nil {
		return "None", nil
	}
	if objects.IsNone(msg) {
		return "None", nil
	}
	return objects.Str(msg)
}

// myBasename returns the filename portion of name following CPython's
// `my_basename`: trim everything through the last "/" (and on Windows,
// "\\", but gopy ports only the POSIX case). Empty input passes through.
//
// CPython: Objects/exceptions.c:2805 my_basename
func myBasename(name string) string {
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		return name[idx+1:]
	}
	return name
}

// init registers SyntaxError_members + the SyntaxError-specific str on
// PyExc_SyntaxError. Subclasses inherit the descriptors through MRO
// lookup.
//
// CPython: Objects/exceptions.c:2875 SyntaxError_members
func init() {
	t := PyExc_SyntaxError
	for name, field := range syntaxFieldGetters() {
		set := syntaxFieldSetters()[name]
		objects.SetTypeDescr(t, name, objects.NewGetSetDescr(name, field, set))
	}
}

// syntaxFieldGetters returns the read-side of SyntaxError_members.
// Each entry returns the raw field value, or None if unset (mirrors
// Py_T_OBJECT's "NULL reads as None" semantics in PyMember_GetOne).
//
// CPython: Include/descrobject.h Py_T_OBJECT (member_get NULL -> None)
func syntaxFieldGetters() map[string]func(objects.Object) (objects.Object, error) {
	return map[string]func(objects.Object) (objects.Object, error){
		"msg": func(o objects.Object) (objects.Object, error) {
			return syntaxField(o, func(s *SyntaxErrorState) objects.Object { return s.Msg })
		},
		"filename": func(o objects.Object) (objects.Object, error) {
			return syntaxField(o, func(s *SyntaxErrorState) objects.Object { return s.Filename })
		},
		"lineno": func(o objects.Object) (objects.Object, error) {
			return syntaxField(o, func(s *SyntaxErrorState) objects.Object { return s.Lineno })
		},
		"offset": func(o objects.Object) (objects.Object, error) {
			return syntaxField(o, func(s *SyntaxErrorState) objects.Object { return s.Offset })
		},
		"text": func(o objects.Object) (objects.Object, error) {
			return syntaxField(o, func(s *SyntaxErrorState) objects.Object { return s.Text })
		},
		"end_lineno": func(o objects.Object) (objects.Object, error) {
			return syntaxField(o, func(s *SyntaxErrorState) objects.Object { return s.EndLineno })
		},
		"end_offset": func(o objects.Object) (objects.Object, error) {
			return syntaxField(o, func(s *SyntaxErrorState) objects.Object { return s.EndOffset })
		},
		"print_file_and_line": func(o objects.Object) (objects.Object, error) {
			return syntaxField(o, func(s *SyntaxErrorState) objects.Object { return s.PrintFileAndLine })
		},
		"_metadata": func(o objects.Object) (objects.Object, error) {
			return syntaxField(o, func(s *SyntaxErrorState) objects.Object { return s.Metadata })
		},
	}
}

// syntaxFieldSetters returns the write-side of SyntaxError_members.
// Each entry writes through the SyntaxErrorState, allocating it lazily
// so a setattr from Python on a SyntaxError that skipped __init__
// (BaseException.__new__ alone) still works.
//
// CPython: Objects/exceptions.c:2875 SyntaxError_members (member_set
// path)
func syntaxFieldSetters() map[string]func(objects.Object, objects.Object) error {
	return map[string]func(objects.Object, objects.Object) error{
		"msg": func(o, v objects.Object) error {
			return syntaxFieldSet(o, v, func(s *SyntaxErrorState, x objects.Object) { s.Msg = x })
		},
		"filename": func(o, v objects.Object) error {
			return syntaxFieldSet(o, v, func(s *SyntaxErrorState, x objects.Object) { s.Filename = x })
		},
		"lineno": func(o, v objects.Object) error {
			return syntaxFieldSet(o, v, func(s *SyntaxErrorState, x objects.Object) { s.Lineno = x })
		},
		"offset": func(o, v objects.Object) error {
			return syntaxFieldSet(o, v, func(s *SyntaxErrorState, x objects.Object) { s.Offset = x })
		},
		"text": func(o, v objects.Object) error {
			return syntaxFieldSet(o, v, func(s *SyntaxErrorState, x objects.Object) { s.Text = x })
		},
		"end_lineno": func(o, v objects.Object) error {
			return syntaxFieldSet(o, v, func(s *SyntaxErrorState, x objects.Object) { s.EndLineno = x })
		},
		"end_offset": func(o, v objects.Object) error {
			return syntaxFieldSet(o, v, func(s *SyntaxErrorState, x objects.Object) { s.EndOffset = x })
		},
		"print_file_and_line": func(o, v objects.Object) error {
			return syntaxFieldSet(o, v, func(s *SyntaxErrorState, x objects.Object) { s.PrintFileAndLine = x })
		},
		"_metadata": func(o, v objects.Object) error {
			return syntaxFieldSet(o, v, func(s *SyntaxErrorState, x objects.Object) { s.Metadata = x })
		},
	}
}

// syntaxField is the shared read path for every SyntaxError member.
//
// CPython: Include/descrobject.h member_get
func syntaxField(o objects.Object, pick func(*SyntaxErrorState) objects.Object) (objects.Object, error) {
	e, ok := o.(*Exception)
	if !ok || e.SyntaxErr == nil {
		return objects.None(), nil
	}
	v := pick(e.SyntaxErr)
	if v == nil {
		return objects.None(), nil
	}
	return v, nil
}

// syntaxFieldSet is the shared write path for every SyntaxError member.
// nil value signals a delete (None stand-in); CPython's Py_T_OBJECT
// member_set rejects NULL with "cannot delete numeric/char attribute"
// for non-OBJECT_EX kinds, but Py_T_OBJECT lets NULL through (it just
// clears the slot).
//
// CPython: Include/descrobject.h member_set (Py_T_OBJECT)
func syntaxFieldSet(o objects.Object, v objects.Object, place func(*SyntaxErrorState, objects.Object)) error {
	e, ok := o.(*Exception)
	if !ok {
		return fmt.Errorf("TypeError: descriptor expects a SyntaxError")
	}
	if e.SyntaxErr == nil {
		e.SyntaxErr = &SyntaxErrorState{}
	}
	if v == nil || objects.IsNone(v) {
		place(e.SyntaxErr, nil)
		return nil
	}
	place(e.SyntaxErr, v)
	return nil
}
