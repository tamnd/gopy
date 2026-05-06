// ImportError and ModuleNotFoundError with the extra `name` and `path`
// named attributes that the import system stamps on every raise.
//
// CPython: Objects/exceptions.c:L2932 ImportError_init
package errors

import (
	"github.com/tamnd/gopy/objects"
)

// ImportError wraps Exception and adds the `name` and `path` fields
// that the import machinery stamps on every ImportError instance.
//
// CPython: Objects/exceptions.c:L2915 ImportErrorObject
type ImportError struct {
	Exception
	Name string // the module name that failed to import
	Path string // the file path associated with the error
}

// NewImportError constructs an ImportError with the given message,
// module name, and path. Pass empty strings for name/path when unknown.
//
// CPython: Objects/exceptions.c:L2932 ImportError_init
func NewImportError(msg, name, path string) *ImportError {
	args := objects.NewTuple([]objects.Object{objects.NewStr(msg)})
	e := &ImportError{
		Exception: *New(PyExc_ImportError, args),
		Name:      name,
		Path:      path,
	}
	return e
}

// NewModuleNotFoundError constructs a ModuleNotFoundError.
//
// CPython: Objects/exceptions.c:L3065 PyExc_ModuleNotFoundError
func NewModuleNotFoundError(name string) *ImportError {
	msg := "No module named '" + name + "'"
	args := objects.NewTuple([]objects.Object{objects.NewStr(msg)})
	e := &ImportError{
		Exception: *New(PyExc_ModuleNotFoundError, args),
		Name:      name,
	}
	return e
}
