// Package imp is the Go port of CPython's import machinery.
// imp/frozen.go implements the frozen module table: code objects that
// are compiled into the interpreter and can be imported without
// touching the filesystem.
//
// CPython: Python/frozen.c
// CPython: Python/import.c:L1240 import_find_frozen
// CPython: Include/import.h:L24 struct _frozen
package imp

import (
	"sync"

	"github.com/tamnd/gopy/objects"
)

// FrozenModule holds a single frozen module entry. A nil Code pointer
// means the module is a known-frozen name but the bytecode has not yet
// been embedded (placeholder state).
//
// CPython: Include/import.h:L24 struct _frozen
type FrozenModule struct {
	// Name is the dotted module name, e.g. "importlib._bootstrap".
	Name string
	// Code is the precompiled code object. nil for placeholder entries.
	Code *objects.Code
	// IsPackage is true when the frozen module is a package (has __path__).
	IsPackage bool
}

var (
	frozenMu      sync.RWMutex
	frozenModules = map[string]*FrozenModule{}
)

// RegisterFrozen adds or replaces a frozen module in the table. It is
// safe to call from multiple goroutines and from init().
//
// CPython: Python/frozen.c — populated at link time via _PyImport_FrozenModules
func RegisterFrozen(m *FrozenModule) {
	frozenMu.Lock()
	frozenModules[m.Name] = m
	frozenMu.Unlock()
}

// FindFrozen looks up a frozen module by exact dotted name. It returns
// the entry and true if found, nil and false otherwise.
//
// CPython: Python/import.c:L1240 import_find_frozen
func FindFrozen(name string) (*FrozenModule, bool) {
	frozenMu.RLock()
	m, ok := frozenModules[name]
	frozenMu.RUnlock()
	return m, ok
}

// IsFrozen reports whether name is in the frozen module table,
// regardless of whether its Code field is populated.
//
// CPython: Python/import.c:L1268 PyImport_IsFrozenModule
func IsFrozen(name string) bool {
	_, ok := FindFrozen(name)
	return ok
}

// FrozenList returns a snapshot of all registered frozen modules.
func FrozenList() []*FrozenModule {
	frozenMu.RLock()
	list := make([]*FrozenModule, 0, len(frozenModules))
	for _, m := range frozenModules {
		list = append(list, m)
	}
	frozenMu.RUnlock()
	return list
}
