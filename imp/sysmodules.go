// sys.modules registry. Mirrors the interp->modules dict that CPython
// uses as the global module cache. All public functions are safe for
// concurrent use.
//
// CPython: Python/import.c:L276 PyImport_GetModule
// CPython: Python/import.c:L297 PyImport_AddModule
// CPython: Python/import.c:L329 PyImport_RemoveModule
package imp

import (
	"sync"

	"github.com/tamnd/gopy/objects"
)

var (
	sysModulesMu sync.RWMutex
	sysModules   = map[string]*objects.Module{}
)

// GetModule returns the module registered under name in sys.modules,
// or nil if absent.
//
// CPython: Python/import.c:L276 PyImport_GetModule
func GetModule(name string) (*objects.Module, bool) {
	sysModulesMu.RLock()
	m, ok := sysModules[name]
	sysModulesMu.RUnlock()
	return m, ok
}

// AddModule adds mod to sys.modules under name. If an entry already
// exists it is replaced. The module pointer is returned for chaining.
//
// CPython: Python/import.c:L297 PyImport_AddModule
func AddModule(name string, mod *objects.Module) *objects.Module {
	sysModulesMu.Lock()
	sysModules[name] = mod
	sysModulesMu.Unlock()
	return mod
}

// RemoveModule removes the module registered under name. It is a
// no-op if name is not present.
//
// CPython: Python/import.c:L329 PyImport_RemoveModule
func RemoveModule(name string) {
	sysModulesMu.Lock()
	delete(sysModules, name)
	sysModulesMu.Unlock()
}

// SysModulesSnapshot returns a shallow copy of sys.modules as a plain
// Go map. Callers receive a stable snapshot; later mutations to the
// registry are not reflected.
//
// CPython: Python/sysmodule.c interp->modules
func SysModulesSnapshot() map[string]*objects.Module {
	sysModulesMu.RLock()
	snap := make(map[string]*objects.Module, len(sysModules))
	for k, v := range sysModules {
		snap[k] = v
	}
	sysModulesMu.RUnlock()
	return snap
}
