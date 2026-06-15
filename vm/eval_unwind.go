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
	"runtime"
	"strings"
	"syscall"

	"github.com/tamnd/gopy/codecs"

	"github.com/tamnd/gopy/compile"
	pyerrors "github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/future"
	"github.com/tamnd/gopy/gil"
	"github.com/tamnd/gopy/module/gc"
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

// RegisterErrorPrefix lets extension modules register their own typed
// exception prefixes (e.g. "socket.gaierror:" -> socketGAIError). Must
// be called before any VM is created (typically from package init()).
func RegisterErrorPrefix(prefix string, typ *objects.Type) {
	errorPrefixToType[prefix] = typ
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
	// GeneratorExit thrown into a generator body by gen.close() must land as
	// a GeneratorExit exception, not as a bare Exception("GeneratorExit").
	// CPython: Objects/genobject.c:388 gen_close (PyErr_SetNone(PyExc_GeneratorExit))
	if errors.Is(err, objects.ErrGeneratorExit) {
		return pyerrors.New(pyerrors.PyExc_GeneratorExit, nil)
	}
	// Structured errors (SyntaxError variants, codec errors) carry their
	// own location/codec fields and convert directly to a typed instance.
	if exc, ok := synthesizeStructured(err); ok {
		return exc
	}
	msg := err.Error()
	// Drop a leading "vm: " prefix added by some callers.
	if rest, ok := strings.CutPrefix(msg, "vm: "); ok {
		msg = rest
	}
	for prefix, typ := range errorPrefixToType {
		if strings.HasPrefix(msg, prefix) {
			if isOSErrorType(typ) {
				if exc := buildOSErrorFromGo(err); exc != nil {
					return exc
				}
			}
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

// synthesizeStructured converts the Go error types that carry their own
// structured fields (the SyntaxError family plus the codec errors) into a
// typed Python exception. ok is false when err is not one of these shapes, so
// the caller falls through to prefix-based message parsing.
func synthesizeStructured(err error) (*pyerrors.Exception, bool) {
	// Structured parser SyntaxError: lift filename/lineno/offset/text
	// into the (msg, info) 2-arg form so the SyntaxError instance
	// carries the full set of attributes Python user code expects.
	// CPython: Parser/pegen_errors.c:317 _PyPegen_raise_error_known_location
	// (PyErr_SetObject builds the typed instance from these fields).
	var se *parsererrors.SyntaxError
	if errors.As(err, &se) {
		return pyerrors.SyntaxFromParser(se), true
	}
	// Structured symtable SyntaxError: same idea as the parser branch,
	// but the location data lives in symtable.SyntaxError.Pos rather
	// than the parser record.
	var stse *symtable.SyntaxError
	if errors.As(err, &stse) {
		return pyerrors.SyntaxFromSymtable(stse), true
	}
	// Structured compile-time SyntaxError: codegen visitor passes
	// surface _PyCompile_Error through compile.SyntaxError with
	// filename / ast.Pos already pinned to the offending node.
	// CPython: Python/compile.c:1191 _PyCompile_Error
	var cse *compile.SyntaxError
	if errors.As(err, &cse) {
		return pyerrors.SyntaxFromCompile(cse), true
	}
	// Structured future-scanner SyntaxError: future_check_features raises
	// for "braces" (easter egg) and unknown feature names.
	// CPython: Python/future.c:L8 future_check_features
	var fse *future.SyntaxError
	if errors.As(err, &fse) {
		return pyerrors.SyntaxFromFuture(fse), true
	}
	// Structured codec UnicodeEncodeError: carries encoding, object, start,
	// end, reason so Python code can access exc.object[exc.start:exc.end].
	// CPython: Objects/exceptions.c:3040 UnicodeError_init
	var uee *codecs.UnicodeEncodeErr
	if errors.As(err, &uee) {
		return pyerrors.NewUnicodeEncodeError(uee.Encoding, objects.NewStr(uee.Object), uee.Start, uee.End, uee.Reason), true
	}
	var ude *codecs.UnicodeDecodeErr
	if errors.As(err, &ude) {
		return pyerrors.NewUnicodeDecodeError(ude.Encoding, ude.Object, ude.Start, ude.End, ude.Reason), true
	}
	return nil, false
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
// type's tp_new / tp_init, so a bare PyObject_New does not populate
// the members.
func buildExceptionForType(typ *objects.Type, msg string) *pyerrors.Exception {
	args := []objects.Object{objects.NewStr(msg)}
	if isSyntaxErrorType(typ) {
		out, err := objects.Call(typ, objects.NewTuple(args), nil)
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

// buildOSErrorFromGo reconstructs the full OSError instance CPython
// would raise from a failed syscall. It pulls the errno, strerror and
// filename(s) out of the Go error (os.PathError carries one filename,
// os.LinkError carries two) and runs them through errors.NewOSError,
// which promotes to the errnomap subclass and populates exc.errno /
// exc.strerror / exc.filename. Returns nil when the error carries no
// errno, so the caller falls back to the plain message path.
//
// CPython: Python/errors.c:778 PyErr_SetFromErrnoWithFilenameObjects
func buildOSErrorFromGo(err error) *pyerrors.Exception {
	var errno syscall.Errno
	var filename, filename2 string
	var pathErr *os.PathError
	var linkErr *os.LinkError
	var sysErr *os.SyscallError
	switch {
	case errors.As(err, &pathErr):
		filename = pathErr.Path
		_ = errors.As(pathErr.Err, &errno)
	case errors.As(err, &linkErr):
		filename = linkErr.Old
		filename2 = linkErr.New
		_ = errors.As(linkErr.Err, &errno)
	case errors.As(err, &sysErr):
		_ = errors.As(sysErr.Err, &errno)
	default:
		if !errors.As(err, &errno) {
			return nil
		}
	}
	if errno == 0 {
		return nil
	}
	return pyerrors.NewOSError(winerrorToErrno(int(errno)), strerrorString(errno), filename, filename2)
}

// strerrorString renders the errno's message the way CPython's
// os.strerror does: the platform strerror text with a leading capital
// letter ("No such file or directory"). Go's syscall.Errno.Error()
// returns the same text lower-cased, so we upcase the first rune.
//
// CPython: Modules/posixmodule.c os_strerror_impl
func strerrorString(errno syscall.Errno) string {
	s := errno.Error()
	if s == "" {
		return s
	}
	r := []rune(s)
	if r[0] >= 'a' && r[0] <= 'z' {
		r[0] -= 'a' - 'A'
	}
	return string(r)
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
			return pyerrors.ErrnoSubclass(winerrorToErrno(int(errno)))
		}
	}
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		var errno syscall.Errno
		if errors.As(linkErr.Err, &errno) {
			return pyerrors.ErrnoSubclass(winerrorToErrno(int(errno)))
		}
	}
	var sysErr *os.SyscallError
	if errors.As(err, &sysErr) {
		var errno syscall.Errno
		if errors.As(sysErr.Err, &errno) {
			return pyerrors.ErrnoSubclass(winerrorToErrno(int(errno)))
		}
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return pyerrors.ErrnoSubclass(winerrorToErrno(int(errno)))
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
		// Use Raise (not Restore) so that __context__ is chained when this
		// exception is born inside an active except-block, matching CPython's
		// _PyErr_SetObject which always chains. This makes
		// `except E: <Go-raised SyntaxError>` set SyntaxError.__context__=E.
		//
		// CPython: Python/errors.c:83 _PyErr_SetObject (chaining branch)
		pyerrors.Raise(e.ts, exc)
	}
	// Prepend a traceback entry for this frame before considering
	// handlers. CPython does the same in exception_unwind so an
	// exception that propagates up through several frames carries one
	// entry per frame regardless of whether any of them caught it.
	//
	// RERAISE is the exception: it jumps straight to exception_unwind,
	// bypassing the `error` label's PyTraceBack_Here, so a re-raised
	// exception keeps the tb it already carries instead of gaining a
	// fresh (and, for a with-cleanup site, line-0) frame entry. A
	// multi-manager `with` re-raises once per cleanup block, so without
	// this guard each block stamps a duplicate frame.
	//
	// CPython: Python/ceval.c exception_unwind (PyTraceBack_Here call);
	// Python/bytecodes.c RERAISE (goto exception_unwind, no tb-here)
	var rr *reraiseError
	if !errors.As(err, &rr) {
		e.attachFrameTraceback()
	}
	if co == nil || len(co.ExceptionTable) == 0 {
		return false
	}
	entry, ok := findExcHandler(co.ExceptionTable, e.f.InstrPtr)
	if !ok {
		return false
	}

	exc := pyerrors.Occurred(e.ts)
	pyerrors.Clear(e.ts)

	// Restore the stack depth to the value recorded in the exception
	// table entry. When the current top is above the handler depth, the
	// slots in between are temporaries the try body pushed and never
	// consumed; CPython's exception_unwind pops each one with Py_XDECREF
	// before jumping to the handler. DropStack closes each slot, so
	// references those temporaries owned (e.g. a coroutine GET_ITER
	// rejected with TypeError) are released here instead of being pinned
	// as a phantom GC root that blocks finalization. When the top is at
	// or below the handler depth (the exception fired before the try
	// body pushed anything), there is nothing to release and we just set
	// the pointer.
	//
	// CPython: Python/ceval.c exception_unwind (the `while (stack_pointer
	// > new_top) PyStackRef_XCLOSE(POP())` loop before `goto handle`)
	if e.f.StackTop > entry.depth {
		e.f.DropStack(e.f.StackTop - entry.depth)
	} else {
		e.f.StackTop = entry.depth
	}

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
// f.f_code / f.f_globals. TbLasti is set to the byte offset of the
// last-executed instruction so _get_code_position can resolve column
// info from co_positions().
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
	// Skip only if the most recent tb entry is for the same frame at the
	// same instruction offset. This matches CPython's exception_unwind,
	// which calls PyTraceBack_Here once per unwind, but allows the same
	// frame to appear multiple times in the chain when the exception
	// leaves the frame via a sub-call and re-raises at a later op
	// (e.g. the original raise site plus a later call that re-raises).
	//
	// CPython: Python/traceback.c:154 PyTraceBack_Here (no dedup; same
	// frame can appear repeatedly when an exception travels through it)
	// CPython: Python/traceback.c:154 PyTraceBack_Here adds a new entry
	// every time it is invoked. No deduplication: the same frame can
	// appear multiple times when the exception leaves the frame via a
	// sub-call and is re-raised at a later op (raise / re-raise after
	// re-entry).
	_ = exc.TB
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
	// Wrap the live activation record so tb.tb_frame reads the iframe's
	// current state (notably f_lineno) until the frame returns. NewFrame
	// auto-registers as a wrapper, so chunk.Pop will SnapshotFrameWithLocals
	// the iframe and SwapInterp the wrapper before the chunk slot recycles.
	// This keeps the catch-site frame's f_lineno live (CPython behavior)
	// while still preserving locals across the natural return of unwinding
	// frames.
	//
	// CPython: Objects/frameobject.c:1109 _PyFrame_New_NoTrack
	// CPython: Python/traceback.c:154 PyTraceBack_Here PyObject_GC_Track
	tb := &traceback.Traceback{
		Entry:   entry,
		Next:    exc.TB,
		TbFrame: objects.NewFrame(e.f),
		TbLasti: off,
	}
	tb.Init(traceback.Type)
	gc.TrackSilent(tb)
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
		// Run finalizer teardown the Go runtime finalizer goroutine
		// handed off (snapshot Decref, weakref callbacks) here on the
		// interpreter goroutine, the way CPython runs tp_finalize under
		// the GIL.
		objects.DrainDeferredFinalizers()
		if p := PendingFor(e.ts); p != nil {
			if err := p.Drain(); err != nil {
				return err
			}
		}
	}
	// Honor a GIL-drop request from another thread: release the lock,
	// let the Go scheduler hand it to a waiter, then re-take it. Only the
	// goroutine that actually owns the lock yields; a generator body
	// running under its driver's hold (different goroutine, same
	// PyThreadState) leaves the bit set for the driver to service when it
	// resumes.
	//
	// CPython: Python/ceval_gil.c eval_breaker GIL_DROP_REQUEST branch
	if b.IsSet(gil.BreakerGILDropRequest) {
		b.Clear(gil.BreakerGILDropRequest)
		if e.gil != nil {
			g := goid()
			if e.gil.HeldByGoid(g) {
				e.gil.ReleaseGoid(e.ts, g)
				runtime.Gosched()
				e.gil.AcquireReentrant(e.ts, g)
				if e.gilTimer != nil {
					e.gilTimer.reset()
				}
			}
		}
	}
	return nil
}
