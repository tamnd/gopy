// Exception unwind. When a dispatch arm returns an error, the loop
// walks the code object's PEP 657 exception table to find a handler.
// On hit, the loop pushes the exception, repoints InstrPtr at the
// handler, and resumes dispatch. On miss, the error escapes to the
// caller (or to the next frame up the chain).
//
// CPython: Python/ceval.c exception_unwind / get_exception_handler

package vm

// DEPRECATED (spec 1714): Spec 1714 phase 5: error-label dispatch is generated; this file shrinks to error helpers.
// See website/docs/specs/1700/1714_bytecodes_dsl_codegen.md.

import (
	"errors"
	"os"
	"strings"
	"syscall"

	"github.com/tamnd/gopy/compile"
	pyerrors "github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/future"
	"github.com/tamnd/gopy/gil"
	"github.com/tamnd/gopy/objects"
	parsererrors "github.com/tamnd/gopy/parser/errors"
	"github.com/tamnd/gopy/symtable"
	"github.com/tamnd/gopy/traceback"
)

// errorPrefixToType maps the "TypeError: ..." style messages large
// chunks of the runtime still emit as bare Go errors back to the
// matching PyExc_* class. Until every callsite raises through
// errors.SetString the unwind path has to guess from the message;
// otherwise `try: ... except TypeError: ...` in user code never
// catches anything because the exception is a bare Exception.
//
// CPython: Python/errors.c:218 PyErr_GivenExceptionMatches reads
// the typed PyObject; gopy's bridge is this prefix table.
var errorPrefixToType = map[string]*objects.Type{
	"TypeError:":               pyerrors.PyExc_TypeError,
	"ValueError:":              pyerrors.PyExc_ValueError,
	"NameError:":               pyerrors.PyExc_NameError,
	"UnboundLocalError:":       pyerrors.PyExc_UnboundLocalError,
	"AttributeError:":          pyerrors.PyExc_AttributeError,
	"KeyError:":                pyerrors.PyExc_KeyError,
	"IndexError:":              pyerrors.PyExc_IndexError,
	"RuntimeError:":            pyerrors.PyExc_RuntimeError,
	"StopIteration:":           pyerrors.PyExc_StopIteration,
	"StopAsyncIteration:":      pyerrors.PyExc_StopAsyncIteration,
	"ArithmeticError:":         pyerrors.PyExc_ArithmeticError,
	"ZeroDivisionError:":       pyerrors.PyExc_ZeroDivisionError,
	"OverflowError:":           pyerrors.PyExc_OverflowError,
	"FloatingPointError:":      pyerrors.PyExc_FloatingPointError,
	"LookupError:":             pyerrors.PyExc_LookupError,
	"AssertionError:":          pyerrors.PyExc_AssertionError,
	"NotImplementedError:":     pyerrors.PyExc_NotImplementedError,
	"UnicodeDecodeError:":      pyerrors.PyExc_UnicodeDecodeError,
	"UnicodeEncodeError:":      pyerrors.PyExc_UnicodeEncodeError,
	"UnicodeTranslateError:":   pyerrors.PyExc_UnicodeTranslateError,
	"UnicodeError:":            pyerrors.PyExc_UnicodeError,
	"SystemError:":             pyerrors.PyExc_SystemError,
	"RecursionError:":          pyerrors.PyExc_RecursionError,
	"OSError:":                 pyerrors.PyExc_OSError,
	"io.UnsupportedOperation:": pyerrors.PyExc_UnsupportedOperation,
	"MemoryError:":             pyerrors.PyExc_MemoryError,
	"ReferenceError:":          pyerrors.PyExc_ReferenceError,
	"BufferError:":             pyerrors.PyExc_BufferError,
	"EOFError:":                pyerrors.PyExc_EOFError,
	"ImportError:":             pyerrors.PyExc_ImportError,
	"ModuleNotFoundError:":     pyerrors.PyExc_ModuleNotFoundError,
	"SyntaxError:":             pyerrors.PyExc_SyntaxError,
	"IndentationError:":        pyerrors.PyExc_IndentationError,
}

// synthesizeException promotes an unmatched Go error into the closest
// typed Python exception. When the message lacks a recognized prefix
// the result falls back to a plain Exception, matching the previous
// behavior.
func synthesizeException(err error) *pyerrors.Exception {
	// RaisedError carries an already-typed Python exception across the
	// generator/coroutine channel boundary (or any other Go error
	// surface). Unwrap so the original instance lands on the thread
	// state with its args (and therefore StopIteration.value) intact.
	//
	// CPython: Objects/genobject.c:225 gen_send_ex2 (StopIteration
	// carries the body's return value through args[0]).
	var re *objects.RaisedError
	if errors.As(err, &re) {
		if exc, ok := re.Exc.(*pyerrors.Exception); ok {
			return exc
		}
	}
	// Iterator-protocol sentinels: the Go side carries them as bare
	// errors.New("StopIteration") / "StopAsyncIteration", which the
	// prefix table below misses (no trailing colon). Recognize them
	// up front so `except StopIteration:` actually catches.
	if errors.Is(err, objects.ErrStopIteration) {
		return pyerrors.New(pyerrors.PyExc_StopIteration, nil)
	}
	if errors.Is(err, objects.ErrStopAsyncIteration) {
		return pyerrors.New(pyerrors.PyExc_StopAsyncIteration, nil)
	}
	// Structured parser SyntaxError: lift filename/lineno/offset/text
	// into the (msg, info) 2-arg form so the SyntaxError instance
	// carries the full set of attributes Python user code expects.
	// CPython: Parser/pegen_errors.c:317 _PyPegen_raise_error_known_location
	// (PyErr_SetObject builds the typed instance from these fields).
	var se *parsererrors.SyntaxError
	if errors.As(err, &se) {
		return pyerrors.SyntaxFromParser(se)
	}
	// Structured symtable SyntaxError: same idea as the parser branch,
	// but the location data lives in symtable.SyntaxError.Pos rather
	// than the parser record.
	var stse *symtable.SyntaxError
	if errors.As(err, &stse) {
		return pyerrors.SyntaxFromSymtable(stse)
	}
	// Structured compile-time SyntaxError: codegen visitor passes
	// surface _PyCompile_Error through compile.SyntaxError with
	// filename / ast.Pos already pinned to the offending node.
	// CPython: Python/compile.c:1191 _PyCompile_Error
	var cse *compile.SyntaxError
	if errors.As(err, &cse) {
		return pyerrors.SyntaxFromCompile(cse)
	}
	// Structured future-scanner SyntaxError: future_check_features raises
	// for "braces" (easter egg) and unknown feature names.
	// CPython: Python/future.c:L8 future_check_features
	var fse *future.SyntaxError
	if errors.As(err, &fse) {
		return pyerrors.SyntaxFromFuture(fse)
	}
	msg := err.Error()
	// Drop a leading "vm: " prefix added by some callers.
	if rest, ok := strings.CutPrefix(msg, "vm: "); ok {
		msg = rest
	}
	for prefix, typ := range errorPrefixToType {
		if strings.HasPrefix(msg, prefix) {
			typ = promoteOSErrorByErrno(typ, err)
			return buildExceptionForType(typ, strings.TrimSpace(msg[len(prefix):]))
		}
	}
	// Fallback: scan for a typed prefix anywhere in the message. The
	// import loader wraps compile() errors in `imp: loadAsModule ...:
	// compile: <inner>` envelopes, so a SyntaxError from the parser
	// arrives without the prefix at byte 0. Pick the earliest match so
	// callers can still `except SyntaxError`.
	for prefix, typ := range errorPrefixToType {
		if i := strings.Index(msg, prefix); i >= 0 {
			typ = promoteOSErrorByErrno(typ, err)
			return buildExceptionForType(typ, strings.TrimSpace(msg[i+len(prefix):]))
		}
	}
	// Third pass: bare-name match. setPendingErr("NameError") produces
	// "NameError" with no colon, so neither HasPrefix nor Index finds
	// "NameError:" in it. Check for an exact match against the name
	// portion (prefix without the trailing colon).
	//
	// CPython: Python/errors.c PyErr_SetNone / _PyErr_SetString — callers
	// always pass a typed exception; this compensates for generator code
	// that can only emit the bare type name.
	for prefix, typ := range errorPrefixToType {
		bare := strings.TrimSuffix(prefix, ":")
		if msg == bare {
			return buildExceptionForType(typ, "")
		}
	}
	return pyerrors.New(pyerrors.PyExc_Exception, objects.NewTuple([]objects.Object{
		objects.NewStr(msg),
	}))
}

// buildExceptionForType is the single-arg constructor path used by the
// prefix-table arms. Most exception types are happy with pyerrors.New
// (which calls BaseException_init via tp.Init), but the SyntaxError
// subclass family needs SyntaxError_init to populate the .msg member
// descriptor. Without that path the descriptor reads SyntaxErr.Msg ==
// nil and surfaces as None, which traceback.py turns into the bogus
// "<no detail available>" string.
//
// CPython: Objects/exceptions.c:2713 SyntaxError_init runs through the
// type's tp_init / tp_call, so a bare PyObject_New does not populate
// the members.
func buildExceptionForType(typ *objects.Type, msg string) *pyerrors.Exception {
	args := []objects.Object{objects.NewStr(msg)}
	if isSyntaxErrorType(typ) {
		out, err := typ.Call(typ, args, nil)
		if err == nil {
			if exc, ok := out.(*pyerrors.Exception); ok {
				return exc
			}
		}
	}
	exc := pyerrors.New(typ, objects.NewTuple(args))
	attachExcNameAttr(exc, typ, msg)
	return exc
}

// attachExcNameAttr populates the `.name` (and optionally `.obj`) field on
// AttributeError and NameError exceptions synthesized from Go error strings.
// CPython populates these fields in PyErr_SetObject when raising via C; gopy
// mirrors the behavior by parsing the message string.
//
// CPython: Objects/exceptions.c:NameError_init, AttributeError_init
func attachExcNameAttr(exc *pyerrors.Exception, typ *objects.Type, msg string) {
	switch typ {
	case pyerrors.PyExc_AttributeError:
		// Pattern: "'TYPE' object has no attribute 'NAME'"
		if i := strings.LastIndex(msg, "no attribute '"); i >= 0 {
			rest := msg[i+len("no attribute '"):]
			if j := strings.IndexByte(rest, '\''); j >= 0 {
				name := rest[:j]
				d := exc.EnsureAttrDict()
				_ = d.SetItem(objects.NewStr("name"), objects.NewStr(name))
				_ = d.SetItem(objects.NewStr("obj"), objects.None())
			}
		}
	case pyerrors.PyExc_NameError, pyerrors.PyExc_UnboundLocalError:
		// Pattern: "name 'NAME' is not defined" or "free variable 'NAME' referenced..."
		if i := strings.Index(msg, "name '"); i >= 0 {
			rest := msg[i+len("name '"):]
			if j := strings.IndexByte(rest, '\''); j >= 0 {
				name := rest[:j]
				d := exc.EnsureAttrDict()
				_ = d.SetItem(objects.NewStr("name"), objects.NewStr(name))
			}
		}
	}
}

// promoteOSErrorByErrno mirrors CPython's PyErr_SetFromErrnoWithFilename
// promotion: when the synthesized exception lands in the OSError family
// and the underlying Go error carries a syscall.Errno (PathError /
// LinkError / SyscallError), look up the matching subclass via
// errnomap so `except FileNotFoundError:` / `except PermissionError:`
// actually catch the right thing. Non-OSError types pass through.
//
// CPython: Python/errors.c:1031 _PyErr_SetFromErrnoWithFilenameObjects
func promoteOSErrorByErrno(typ *objects.Type, err error) *objects.Type {
	if !isOSErrorType(typ) {
		return typ
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		var errno syscall.Errno
		if errors.As(pathErr.Err, &errno) {
			return pyerrors.ErrnoSubclass(int(errno))
		}
	}
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		var errno syscall.Errno
		if errors.As(linkErr.Err, &errno) {
			return pyerrors.ErrnoSubclass(int(errno))
		}
	}
	var sysErr *os.SyscallError
	if errors.As(err, &sysErr) {
		var errno syscall.Errno
		if errors.As(sysErr.Err, &errno) {
			return pyerrors.ErrnoSubclass(int(errno))
		}
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return pyerrors.ErrnoSubclass(int(errno))
	}
	return typ
}

// isOSErrorType reports whether typ is OSError itself. Subclasses keep
// their original type. The promotion only fires when the synthesizer
// already picked plain OSError.
func isOSErrorType(typ *objects.Type) bool {
	return typ == pyerrors.PyExc_OSError
}

// isSyntaxErrorType reports whether typ is SyntaxError or a SyntaxError
// subclass (IndentationError, TabError). The check follows the bases
// chain so future subclasses pick up the same routing.
func isSyntaxErrorType(typ *objects.Type) bool {
	for t := typ; t != nil; {
		if t == pyerrors.PyExc_SyntaxError {
			return true
		}
		if len(t.Bases) == 0 {
			return false
		}
		t = t.Bases[0]
	}
	return false
}

// handleException tries to find a handler for err in the current
// frame. Returns (residualValue, true) on hit (caller continues
// dispatch); (nil, false) on miss (caller propagates).
//
// CPython: Python/ceval.c:L1815 get_exception_handler + exception_unwind
func (e *evalState) handleException(err error) bool {
	co := e.f.Code
	// Make sure an Exception object lives on the thread state before
	// attaching the traceback. Bare Go errors (e.g. "ZeroDivisionError:
	// integer division or modulo by zero") returned by dispatch arms
	// never installed one, so attachFrameTraceback would have nothing to
	// hang the frame entry off of. Synthesize once at the bottom frame
	// and let it propagate up, picking up one TB entry per frame.
	if pyerrors.Occurred(e.ts) == nil {
		// Restore (not Raise) so we skip __context__ chaining: this
		// exception is being born here, it has no causal predecessor
		// in the currently-handled chain.
		exc := synthesizeException(err)
		// Attach any __notes__ queued by FormatNoteHook before this
		// Go error became a typed Exception (e.g. arg-binding
		// TypeError raised inside type_new_set_names).
		//
		// CPython: Python/errors.c:1567 _PyErr_FormatNote
		if notes := drainPendingNotes(); len(notes) > 0 {
			if exc.Notes == nil {
				exc.Notes = objects.NewList(nil)
			}
			for _, n := range notes {
				exc.Notes.Append(objects.NewStr(n))
			}
		}
		pyerrors.Restore(e.ts, exc.ExcType, exc, nil)
	}
	// Prepend a traceback entry for this frame before considering
	// handlers. CPython does the same in exception_unwind so an
	// exception that propagates up through several frames carries one
	// entry per frame regardless of whether any of them caught it.
	//
	// CPython: Python/ceval.c exception_unwind (PyTraceBack_Here call)
	e.attachFrameTraceback()
	if co == nil || len(co.ExceptionTable) == 0 {
		return false
	}
	entry, ok := findExcHandler(co.ExceptionTable, e.f.InstrPtr)
	if !ok {
		return false
	}

	exc := pyerrors.Occurred(e.ts)
	pyerrors.Clear(e.ts)

	// Unconditionally restore the stack depth to the value recorded in
	// the exception table entry. CPython always sets the stack pointer
	// here; only reducing it was a bug (StackTop could be below
	// entry.depth if the exception fired before any pushes in the try
	// body).
	//
	// CPython: Python/ceval.c exception_unwind `_PyFrame_SetStackPointer`
	e.f.StackTop = entry.depth

	// For SETUP_WITH / SETUP_CLEANUP regions, push the bytecode lasti
	// in code-units. The with-statement teardown reads it to resume at
	// the right offset.
	//
	// CPython: Python/ceval.c exception_unwind (`if (lasti)` branch)
	if entry.preserveLasti {
		e.pushObject(objects.NewInt(int64(e.f.InstrPtr / 2)))
	}

	// Push the exception value. The handler's first opcode is
	// PUSH_EXC_INFO, which pops this, pushes the previous exc_info
	// under it, then re-pushes it on top.
	//
	// CPython: Python/ceval.c exception_unwind (`PUSH(exc)`)
	e.pushObject(exc)

	e.f.InstrPtr = entry.target
	e.f.PrevInstr = entry.target
	return true
}

// attachFrameTraceback prepends a traceback entry for the current
// frame onto the live exception's TB chain. Mirrors PyTraceBack_Here,
// which the CPython unwind invokes for every frame on the way up.
//
// Only attaches when an exception is already on the thread state.
// Bare Go errors that the runtime synthesizes into a typed exception
// downstream (synthesizeException) are not raised here, so they have
// no associated frame data worth recording at this layer.
//
// TbFrame wraps the live interpreter frame so traceback.py can walk
// f.f_code / f.f_globals. TbLasti=-1 makes _get_code_position return
// (None,)*4 so traceback.py skips the co_positions lookup (which the
// gopy Code object does not expose yet).
//
// CPython: Python/traceback.c:154 PyTraceBack_Here
func (e *evalState) attachFrameTraceback() {
	exc := pyerrors.Occurred(e.ts)
	if exc == nil {
		return
	}
	co := e.f.Code
	if co == nil {
		return
	}
	// CPython resolves the traceback line from the *previous* dispatched
	// instruction's offset, since InstrPtr already points at whatever
	// follows the raising op.
	//
	// CPython: Python/traceback.c:154 PyTraceBack_Here (frame->f_lasti)
	off := e.f.PrevInstr
	if off < 0 {
		off = e.f.InstrPtr
	}
	line := -1
	if entry, ok := objects.CoAddr2Location(co, off); ok {
		line = entry.Line
	}
	if line < 0 && co.Firstlineno > 0 {
		line = co.Firstlineno
	}
	name := co.Name
	if co.Qualname != "" {
		name = co.Qualname
	}
	entry := traceback.Entry{File: co.Filename, Line: line, Name: name}
	// Snapshot the live activation record so tb.tb_frame.f_code stays
	// readable after the frame returns and its chunk-arena slot gets
	// recycled. CPython does not need this because PyFrameObject is
	// reference-counted; gopy reuses interpreter-frame storage.
	snap := objects.SnapshotFrame(e.f)
	tb := &traceback.Traceback{
		Entry:   entry,
		Next:    exc.TB,
		TbFrame: objects.NewFrame(snap),
		TbLasti: -1,
	}
	tb.Init(traceback.Type)
	exc.TB = tb
}

// handleEvalBreaker drains pending state visible through the breaker:
// requested GIL drops, queued pending calls, async exceptions, GC
// requests. Returns an error if any handler errored.
//
// CPython: Python/ceval_gil.c handle_signals + _Py_HandlePending
func (e *evalState) handleEvalBreaker() error {
	b := e.breaker
	if b == nil {
		return nil
	}
	if b.IsSet(gil.BreakerCallsPending) {
		b.Clear(gil.BreakerCallsPending)
		if p := PendingFor(e.ts); p != nil {
			if err := p.Drain(); err != nil {
				return err
			}
		}
	}
	return nil
}
