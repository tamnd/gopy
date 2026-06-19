// Built-in module initializer table. Mirrors _PyImport_Inittab: a
// list of (name, initfunc) pairs for modules that are compiled in.
// AppendInittab registers new entries; FindInitFunc looks them up.
//
// CPython: Python/import.c:L173 _PyImport_Inittab
// CPython: Python/import.c:L243 PyImport_AppendInittab
// CPython: Python/import.c:L212 PyImport_ExtendInittab
package imp

import (
	"fmt"
	"sync"

	"github.com/tamnd/gopy/objects"
)

// InitFunc is the initializer signature for a built-in module.
// It mirrors the C Py_InitProc signature.
//
// CPython: Include/import.h:L8 PyImport_InitModuleFunc
type InitFunc func() (*objects.Module, error)

// InittabEntry is one row in the built-in module table.
//
// CPython: Include/import.h:L12 struct _inittab
type InittabEntry struct {
	Name string
	Init InitFunc
}

var (
	inittabMu  sync.RWMutex
	inittab    []InittabEntry
	inittabIdx = map[string]InitFunc{}
)

// AppendInittab adds a single built-in module entry. It is safe to
// call from multiple goroutines and from init().
//
// CPython: Python/import.c:L243 PyImport_AppendInittab
func AppendInittab(name string, init InitFunc) error {
	if init == nil {
		return fmt.Errorf("imp: AppendInittab %q: nil InitFunc", name)
	}
	inittabMu.Lock()
	if _, dup := inittabIdx[name]; !dup {
		inittab = append(inittab, InittabEntry{Name: name, Init: init})
		inittabIdx[name] = init
	}
	inittabMu.Unlock()
	return nil
}

// ExtendInittab appends multiple entries at once. It stops and returns
// an error if any entry has a nil Init.
//
// CPython: Python/import.c:L212 PyImport_ExtendInittab
func ExtendInittab(entries []InittabEntry) error {
	for _, e := range entries {
		if err := AppendInittab(e.Name, e.Init); err != nil {
			return err
		}
	}
	return nil
}

// shadowedByStdlib lists inittab names that CPython ships as pure-Python
// stdlib modules (.py files on sys.path), so they never appear in
// CPython's PyImport_Inittab. gopy keeps a Go implementation in the
// inittab as an early-bootstrap import shortcut, but the live import
// machinery must treat them as not-built-in: BuiltinImporter declines
// them and PathFinder loads the vendored source, so e.g.
// 'fnmatch' in sys.builtin_module_names stays False as on a normal
// CPython build, and is_builtin agrees with builtin_module_names.
var shadowedByStdlib = map[string]bool{
	"os":          true,
	"warnings":    true,
	"dataclasses": true,
	"difflib":     true,
	"fnmatch":     true,
}

// ShadowedByStdlib reports whether name is registered in the inittab only
// as a bootstrap shortcut while CPython ships it as pure-Python stdlib,
// so it must be reported as not-built-in by is_builtin and excluded from
// sys.builtin_module_names.
func ShadowedByStdlib(name string) bool {
	return shadowedByStdlib[name]
}

// IsBuiltinName reports whether name resolves to a statically linked
// built-in module, the membership test behind both _imp.is_builtin and
// sys.builtin_module_names. Names shadowed by a pure-Python stdlib module
// are excluded so they load from source the way they do on CPython.
//
// CPython: Python/import.c:4720 _imp_is_builtin_impl
func IsBuiltinName(name string) bool {
	if shadowedByStdlib[name] {
		return false
	}
	return FindInitFunc(name) != nil
}

// FindInitFunc returns the InitFunc registered for name, or nil if the
// module is not in the built-in table.
//
// CPython: Python/import.c:L256 _PyImport_FindExtensionObjectUnicode
func FindInitFunc(name string) InitFunc {
	inittabMu.RLock()
	fn := inittabIdx[name]
	inittabMu.RUnlock()
	return fn
}

// InittabSnapshot returns a copy of the current built-in module table.
func InittabSnapshot() []InittabEntry {
	inittabMu.RLock()
	snap := make([]InittabEntry, len(inittab))
	copy(snap, inittab)
	inittabMu.RUnlock()
	return snap
}
