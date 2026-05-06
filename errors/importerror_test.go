package errors

import (
	"strings"
	"testing"
)

// TestNewImportErrorMessage pins the message and type.
func TestNewImportErrorMessage(t *testing.T) {
	e := NewImportError("bad import", "mymod", "/path/to/mymod.py")
	if e.ExcType != PyExc_ImportError {
		t.Errorf("type = %v, want ImportError", e.ExcType)
	}
	if !strings.Contains(e.Message(), "bad import") {
		t.Errorf("message = %q, want bad import", e.Message())
	}
	if e.Name != "mymod" {
		t.Errorf("Name = %q, want mymod", e.Name)
	}
	if e.Path != "/path/to/mymod.py" {
		t.Errorf("Path = %q, want /path/to/mymod.py", e.Path)
	}
}

// TestNewModuleNotFoundErrorType pins the ModuleNotFoundError type.
func TestNewModuleNotFoundErrorType(t *testing.T) {
	e := NewModuleNotFoundError("nomod")
	if e.ExcType != PyExc_ModuleNotFoundError {
		t.Errorf("type = %v, want ModuleNotFoundError", e.ExcType)
	}
	if e.Name != "nomod" {
		t.Errorf("Name = %q, want nomod", e.Name)
	}
	if !strings.Contains(e.Message(), "nomod") {
		t.Errorf("message = %q, want nomod in message", e.Message())
	}
}

// TestModuleNotFoundErrorIsImportError pins the inheritance via IsSubtype.
func TestModuleNotFoundErrorIsImportError(t *testing.T) {
	if !IsSubtype(PyExc_ModuleNotFoundError, PyExc_ImportError) {
		t.Error("ModuleNotFoundError should be a subtype of ImportError")
	}
}

// TestImportErrorIsException pins that ImportError inherits from Exception.
func TestImportErrorIsException(t *testing.T) {
	if !IsSubtype(PyExc_ImportError, PyExc_Exception) {
		t.Error("ImportError should be a subtype of Exception")
	}
}

// TestImportErrorMatchesImportError pins Match on a raised ImportError.
func TestImportErrorMatchesImportError(t *testing.T) {
	e := NewImportError("boom", "", "")
	if !Match(&e.Exception, PyExc_ImportError) {
		t.Error("Match(ImportError, PyExc_ImportError) should be true")
	}
}
