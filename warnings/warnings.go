// _Py_Warn dispatch entry. The eval loop and codecs path call into
// Warn / WarnAt with the category, the message, and the source
// position. _Py_Warn walks the filter list, applies the matching
// action, and either raises (ActionError), suppresses (ActionIgnore),
// or prints to the configured writer (everything else).
//
// CPython: Python/_warnings.c:980 _PyErr_WarnEx

package warnings

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

// ErrWarning is the sentinel returned when the matched filter is
// ActionError. Callers turn this into the right Python-level
// exception once the warning category port lands.
//
// CPython: Python/_warnings.c:551 warn_explicit raises the category
type ErrWarning struct {
	Category *Category
	Message  string
	Module   string
	Lineno   int
}

// Error renders the sentinel into the "Module: Message" form
// PyErr_WarnEx ultimately formats.
func (e *ErrWarning) Error() string {
	return e.Category.Name + ": " + e.Message
}

// Is implements errors.Is. Two ErrWarning values match when their
// categories are equal; the message is data, not identity.
func (e *ErrWarning) Is(target error) bool {
	other, ok := target.(*ErrWarning)
	if !ok {
		return false
	}
	return e.Category == other.Category
}

var (
	writerMu sync.RWMutex
	writer   io.Writer = os.Stderr
)

// SetWriter installs the io.Writer used for non-ignore, non-error
// actions. Tests swap in a buffer; Py_Initialize wires sys.stderr.
//
// CPython: Python/_warnings.c get_default_action implicit sys.stderr
func SetWriter(w io.Writer) {
	writerMu.Lock()
	defer writerMu.Unlock()
	writer = w
}

func currentWriter() io.Writer {
	writerMu.RLock()
	defer writerMu.RUnlock()
	return writer
}

// Warn is the primary entry point. Matches PyErr_WarnEx(category,
// message, stacklevel=1); the source-location lookup falls out of
// the caller-provided module/lineno pair the eval loop computes.
//
// CPython: Python/_warnings.c:980 PyErr_WarnEx
func Warn(category *Category, message string) error {
	return WarnAt(category, message, "", 0)
}

// WarnAt is _Py_Warn's full surface: emit message under category
// from (module, lineno). The dedup hook (1651-warn-B) attaches here.
//
// CPython: Python/_warnings.c:551 warn_explicit
func WarnAt(category *Category, message, module string, lineno int) error {
	if category == nil {
		category = UserWarning
	}
	action := findAction(message, module, lineno, category)
	switch action {
	case ActionIgnore:
		return nil
	case ActionError:
		return &ErrWarning{Category: category, Message: message, Module: module, Lineno: lineno}
	}
	return emit(currentWriter(), category, message, module, lineno)
}

// emit writes the warning to the destination io.Writer in the same
// shape PyErr_ShowWarning produces: "module:lineno: Category: message\n".
//
// CPython: Python/_warnings.c:445 show_warning
func emit(w io.Writer, category *Category, message, module string, lineno int) error {
	if w == nil {
		return nil
	}
	if module == "" {
		module = "<unknown>"
	}
	_, err := fmt.Fprintf(w, "%s:%d: %s: %s\n", module, lineno, category.Name, message)
	return err
}

// IsWarning returns true when err is a warning sentinel; the eval
// loop uses this to decide whether to translate the Go error into
// the matching Python exception.
//
// CPython: Python/_warnings.c PyErr_WarnEx return-code branch
func IsWarning(err error) bool {
	var w *ErrWarning
	return errors.As(err, &w)
}
