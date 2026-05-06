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
