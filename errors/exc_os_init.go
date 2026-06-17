package errors

import (
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// OSErrorState carries the PyOSErrorObject member payload CPython stores
// alongside the base PyBaseException fields: the POSIX errno, the
// strerror string, the one or two filenames involved, and the
// characters-written counter used by BlockingIOError. Each slot is an
// Object so str / int / None storage round-trips through Python without
// coercion. Written is -1 when unset, matching the C struct default.
//
// CPython: Objects/exceptions.c:1940 PyOSErrorObject
type OSErrorState struct {
	Myerrno   objects.Object
	Strerror  objects.Object
	Filename  objects.Object
	Filename2 objects.Object
	Written   int
}

// osErrFamily lists every OSError subclass that must route construction
// through OSError_new / OSError_init and str through OSError_str. The
// slots are wired in init() rather than at type-construction time so the
// promotion path (which references these package vars) stays out of the
// package-variable initialization cycle.
//
// CPython: Objects/exceptions.c:2410 ComplexExtendsException OSError
func osErrFamily() []*objects.Type {
	return []*objects.Type{
		PyExc_OSError, PyExc_BlockingIOError, PyExc_ConnectionError,
		PyExc_ChildProcessError, PyExc_BrokenPipeError,
		PyExc_ConnectionAbortedError, PyExc_ConnectionRefusedError,
		PyExc_ConnectionResetError, PyExc_FileExistsError,
		PyExc_FileNotFoundError, PyExc_IsADirectoryError,
		PyExc_NotADirectoryError, PyExc_InterruptedError,
		PyExc_PermissionError, PyExc_ProcessLookupError,
		PyExc_TimeoutError,
	}
}

// osErrorTpNew mirrors OSError_new: allocate a fresh Exception, run the
// arg parse + errnomap promotion, then OSError_init to populate the
// member fields.
//
// CPython: Objects/exceptions.c:2132 OSError_new
func osErrorTpNew(cls *objects.Type, args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return osErrorConstruct(cls, args)
}

// osErrorConstruct runs the OSError_new body: it parses the (errno,
// strerror, filename, winerror, filename2) argument shape, promotes a
// bare OSError to the errnomap subclass when the first argument is an
// integer errno, allocates the instance, and runs oserror_init.
//
// CPython: Objects/exceptions.c:2132 OSError_new
func osErrorConstruct(cls *objects.Type, args []objects.Object) (objects.Object, error) {
	myerrno, strerror, filename, filename2 := osErrorParseArgs(args)
	// errnomap promotion only fires when the type is plain OSError and
	// the first argument is an integer errno.
	//
	// CPython: Objects/exceptions.c:2188 errnomap lookup in OSError_new
	if cls == PyExc_OSError && myerrno != nil {
		if code, ok := myerrno.(*objects.Int); ok {
			n, _ := code.Int64()
			cls = ErrnoSubclass(int(n))
		}
	}
	e := New(cls, objects.NewTuple(args))
	osErrorInit(e, cls, args, myerrno, strerror, filename, filename2)
	return e, nil
}

// osErrorParseArgs unpacks args into (myerrno, strerror, filename,
// filename2). The winerror slot (args[3]) is parsed for signature
// consistency but ignored on non-Windows platforms, matching the
// `#ifndef MS_WINDOWS` branch. Only the 2-to-5-argument shape pulls out
// the extra members; other lengths leave them nil.
//
// CPython: Objects/exceptions.c:1998 oserror_parse_args
func osErrorParseArgs(args []objects.Object) (myerrno, strerror, filename, filename2 objects.Object) {
	nargs := len(args)
	if nargs < 2 || nargs > 5 {
		return nil, nil, nil, nil
	}
	myerrno = args[0]
	strerror = args[1]
	if nargs >= 3 {
		filename = args[2]
	}
	// args[3] is winerror (ignored here); args[4] is filename2.
	if nargs >= 5 {
		filename2 = args[4]
	}
	return myerrno, strerror, filename, filename2
}

// osErrorInit ports oserror_init: store filename / filename2, trim the
// args tuple back to (errno, strerror) when a filename was supplied (so
// in-place unpacking `errno, strerror = exc.args` keeps working), and
// store myerrno / strerror. BlockingIOError's third argument is the
// character-written count rather than a filename.
//
// CPython: Objects/exceptions.c:2061 oserror_init
func osErrorInit(e *Exception, cls *objects.Type, args []objects.Object, myerrno, strerror, filename, filename2 objects.Object) {
	if e.OSErr == nil {
		e.OSErr = &OSErrorState{Written: -1}
	}
	state := e.OSErr
	nargs := len(args)
	if filename != nil && !objects.IsNone(filename) {
		if isBlockingIOError(cls) {
			if n, ok := filename.(*objects.Int); ok {
				v, _ := n.Int64()
				state.Written = int(v)
				goto errnoStore
			}
		}
		state.Filename = filename
		if filename2 != nil && !objects.IsNone(filename2) {
			state.Filename2 = filename2
		}
		if nargs >= 2 && nargs <= 5 {
			// filename, filename2 and winerror are stripped from args
			// for compatibility with old in-place unpacking code.
			e.setArgsSteal(objects.NewTuple([]objects.Object{args[0], args[1]}))
		}
	}
errnoStore:
	state.Myerrno = myerrno
	state.Strerror = strerror
}

// isBlockingIOError reports whether cls is BlockingIOError, where the
// third constructor argument means characters written rather than a
// filename.
//
// CPython: Objects/exceptions.c:2071 Py_IS_TYPE(self, PyExc_BlockingIOError)
func isBlockingIOError(cls *objects.Type) bool {
	return cls == PyExc_BlockingIOError
}

// osErrorStr ports OSError_str: render "[Errno N] strerror: 'filename'"
// (and "-> 'filename2'" when a second filename is present), falling back
// to "[Errno N] strerror" with no filename and finally to
// BaseException_str.
//
// CPython: Objects/exceptions.c:2275 OSError_str
func osErrorStr(o objects.Object) (string, error) {
	e, ok := o.(*Exception)
	if !ok || e.OSErr == nil {
		return excStr(o)
	}
	state := e.OSErr
	orNone := func(x objects.Object) objects.Object {
		if x == nil {
			return objects.None()
		}
		return x
	}
	if state.Filename != nil {
		errnoStr, err := objects.Str(orNone(state.Myerrno))
		if err != nil {
			return "", err
		}
		strerrStr, err := objects.Str(orNone(state.Strerror))
		if err != nil {
			return "", err
		}
		fnRepr, err := objects.Repr(state.Filename)
		if err != nil {
			return "", err
		}
		if state.Filename2 != nil {
			fn2Repr, err := objects.Repr(state.Filename2)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("[Errno %s] %s: %s -> %s", errnoStr, strerrStr, fnRepr, fn2Repr), nil
		}
		return fmt.Sprintf("[Errno %s] %s: %s", errnoStr, strerrStr, fnRepr), nil
	}
	if state.Myerrno != nil && state.Strerror != nil {
		errnoStr, err := objects.Str(state.Myerrno)
		if err != nil {
			return "", err
		}
		strerrStr, err := objects.Str(state.Strerror)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("[Errno %s] %s", errnoStr, strerrStr), nil
	}
	return excStr(o)
}

// NewOSError builds an OSError-family instance from a Go-side errno,
// strerror and (optional) filenames, running the same arg parse + init
// path Python callers take. It mirrors CPython's
// PyErr_SetFromErrnoWithFilenameObjects, which the os module uses to
// raise from a failed syscall: the errno picks the subclass, and the
// filename lands on exc.filename so callers reading c.exception.filename
// see the path that failed. Empty filename strings are dropped (passed
// as absent) so single-filename calls do not synthesize a bogus path.
//
// CPython: Python/errors.c:778 PyErr_SetFromErrnoWithFilenameObjects
func NewOSError(errno int, strerror, filename, filename2 string) *Exception {
	args := []objects.Object{objects.NewInt(int64(errno)), objects.NewStr(strerror)}
	if filename != "" {
		args = append(args, objects.NewStr(filename))
		if filename2 != "" {
			args = append(args, objects.None(), objects.NewStr(filename2))
		}
	}
	out, _ := osErrorConstruct(PyExc_OSError, args)
	exc, _ := out.(*Exception)
	return exc
}

// init registers OSError_members on PyExc_OSError. Subclasses inherit
// the descriptors through MRO lookup, exactly as CPython shares
// OSError_members across the errno family.
//
// CPython: Objects/exceptions.c:2400 OSError_members / OSError_getset
func init() {
	// Wire the OSError construction + str slots on every family member.
	// Done here (not in newExcType) so osErrorConstruct's references to
	// PyExc_OSError / PyExc_BlockingIOError stay out of the package-var
	// initialization cycle.
	//
	// CPython: Objects/exceptions.c:2410 OSError tp_new / tp_init / tp_str
	for _, ft := range osErrFamily() {
		ft.TpNew = osErrorTpNew
		ft.Str = osErrorStr
	}
	t := PyExc_OSError
	objects.SetTypeDescr(t, "errno", objects.NewGetSetDescr("errno",
		osErrorMember(func(s *OSErrorState) objects.Object { return s.Myerrno }),
		osErrorMemberSet(func(s *OSErrorState, v objects.Object) { s.Myerrno = v })))
	objects.SetTypeDescr(t, "strerror", objects.NewGetSetDescr("strerror",
		osErrorMember(func(s *OSErrorState) objects.Object { return s.Strerror }),
		osErrorMemberSet(func(s *OSErrorState, v objects.Object) { s.Strerror = v })))
	objects.SetTypeDescr(t, "filename", objects.NewGetSetDescr("filename",
		osErrorMember(func(s *OSErrorState) objects.Object { return s.Filename }),
		osErrorMemberSet(func(s *OSErrorState, v objects.Object) { s.Filename = v })))
	objects.SetTypeDescr(t, "filename2", objects.NewGetSetDescr("filename2",
		osErrorMember(func(s *OSErrorState) objects.Object { return s.Filename2 }),
		osErrorMemberSet(func(s *OSErrorState, v objects.Object) { s.Filename2 = v })))
	objects.SetTypeDescr(t, "characters_written", objects.NewGetSetDescr("characters_written",
		osErrorWrittenGet, osErrorWrittenSet))
}

// osErrorMember is the shared read path for the errno / strerror /
// filename / filename2 members. A nil slot reads as None, matching
// Py_T_OBJECT's "NULL -> None" rule in PyMember_GetOne.
//
// CPython: Include/descrobject.h Py_T_OBJECT (member_get NULL -> None)
func osErrorMember(pick func(*OSErrorState) objects.Object) func(objects.Object) (objects.Object, error) {
	return func(o objects.Object) (objects.Object, error) {
		e, ok := o.(*Exception)
		if !ok || e.OSErr == nil {
			return objects.None(), nil
		}
		if v := pick(e.OSErr); v != nil {
			return v, nil
		}
		return objects.None(), nil
	}
}

// osErrorMemberSet is the shared write path. It allocates the state
// lazily so a setattr on an OSError that skipped __init__ still works,
// and treats None / nil as clearing the slot.
//
// CPython: Objects/exceptions.c:2400 OSError_members (Py_T_OBJECT set)
func osErrorMemberSet(place func(*OSErrorState, objects.Object)) func(objects.Object, objects.Object) error {
	return func(o, v objects.Object) error {
		e, ok := o.(*Exception)
		if !ok {
			return fmt.Errorf("TypeError: descriptor expects an OSError")
		}
		if e.OSErr == nil {
			e.OSErr = &OSErrorState{Written: -1}
		}
		if v == nil || objects.IsNone(v) {
			place(e.OSErr, nil)
			return nil
		}
		place(e.OSErr, v)
		return nil
	}
}

// osErrorWrittenGet ports OSError_written_get: the characters_written
// member raises AttributeError when unset (written == -1) rather than
// reading as None.
//
// CPython: Objects/exceptions.c:2360 OSError_written_get
func osErrorWrittenGet(o objects.Object) (objects.Object, error) {
	e, ok := o.(*Exception)
	if !ok || e.OSErr == nil || e.OSErr.Written == -1 {
		return nil, fmt.Errorf("AttributeError: characters_written")
	}
	return objects.NewInt(int64(e.OSErr.Written)), nil
}

// osErrorWrittenSet ports OSError_written_set: storing an int updates the
// counter; deleting (nil) resets it to -1, but deleting when already
// unset raises AttributeError.
//
// CPython: Objects/exceptions.c:2373 OSError_written_set
func osErrorWrittenSet(o, v objects.Object) error {
	e, ok := o.(*Exception)
	if !ok {
		return fmt.Errorf("TypeError: descriptor expects an OSError")
	}
	if e.OSErr == nil {
		e.OSErr = &OSErrorState{Written: -1}
	}
	if v == nil {
		if e.OSErr.Written == -1 {
			return fmt.Errorf("AttributeError: characters_written")
		}
		e.OSErr.Written = -1
		return nil
	}
	n, ok := v.(*objects.Int)
	if !ok {
		return fmt.Errorf("ValueError: characters_written requires an integer")
	}
	val, _ := n.Int64()
	e.OSErr.Written = int(val)
	return nil
}
