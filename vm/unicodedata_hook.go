// vm-side wiring for the \N{NAME} escape decoder. parser/string can not
// drive the import machinery itself without a cycle, so it consults the
// UnicodeDataLoadCheck hook installed here. This mirrors CPython, which
// imports the unicodedata module lazily the first time a \N escape is
// decoded and raises a UnicodeError when the import is blocked.
//
// CPython: Objects/unicodeobject.c:6791 load_ucnhash

package vm

import (
	"fmt"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
	pystr "github.com/tamnd/gopy/parser/string"
	"github.com/tamnd/gopy/state"
)

func init() {
	pystr.UnicodeDataLoadCheck = unicodedataLoadCheck
}

// unicodedataLoadCheck mirrors CPython's PyCapsule_Import of the
// unicodedata module during \N{NAME} decoding: it honors a None sentinel
// in sys.modules (the way `sys.modules['unicodedata'] = None` blocks the
// import) and otherwise drives a real import, returning an error when the
// module can not be loaded.
//
// CPython: Python/import.c:1450 PyImport_ImportModule
func unicodedataLoadCheck() error {
	if v, present := imp.GetModuleRaw("unicodedata"); present {
		if v == nil || v == objects.None() {
			return fmt.Errorf("ImportError: import of unicodedata halted; None in sys.modules")
		}
		return nil
	}
	ts := currentThread()
	if ts == nil {
		ts = state.NewThread()
	}
	exec := &vmExecutor{ts: ts}
	if _, err := imp.ImportModule(exec, "unicodedata"); err != nil {
		return err
	}
	return nil
}
