// sys.modules registry. Mirrors the interp->modules dict that CPython
// uses as the global module cache. The dict returned by SysModules is
// the same object Python code sees as sys.modules: every import path
// writes through it, every cache hit reads from it.
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
	sysModules   = objects.NewDict()
)

// SysModules returns the dict backing sys.modules. The same pointer is
// stamped onto the sys module as `sys.modules` so Python and Go share
// one view of the cache.
//
// CPython: Python/sysmodule.c interp->modules
func SysModules() *objects.Dict { return sysModules }

// GetModule returns the module registered under name in sys.modules,
// or (nil, false) if absent or if the entry is not a module.
//
// CPython: Python/import.c:L276 PyImport_GetModule
func GetModule(name string) (*objects.Module, bool) {
	sysModulesMu.RLock()
	v, err := sysModules.GetItem(objects.NewStr(name))
	sysModulesMu.RUnlock()
	if err != nil || v == nil {
		return nil, false
	}
	m, ok := v.(*objects.Module)
	if !ok {
		return nil, false
	}
	return m, true
}

// AddModule adds mod to sys.modules under name. If an entry already
// exists it is replaced. The module pointer is returned for chaining.
//
// CPython: Python/import.c:L297 PyImport_AddModule
func AddModule(name string, mod *objects.Module) *objects.Module {
	sysModulesMu.Lock()
	_ = sysModules.SetItem(objects.NewStr(name), mod)
	sysModulesMu.Unlock()
	return mod
}

// RemoveModule removes the module registered under name. It is a
// no-op if name is not present.
//
// CPython: Python/import.c:L329 PyImport_RemoveModule
func RemoveModule(name string) {
	sysModulesMu.Lock()
	_ = sysModules.DelItem(objects.NewStr(name))
	sysModulesMu.Unlock()
}

// SysModulesSnapshot returns a shallow copy of sys.modules as a plain
// Go map. Callers receive a stable snapshot; later mutations to the
// registry are not reflected. Non-module entries are skipped.
//
// CPython: Python/sysmodule.c interp->modules
func SysModulesSnapshot() map[string]*objects.Module {
	sysModulesMu.RLock()
	defer sysModulesMu.RUnlock()
	keys := sysModules.Keys()
	snap := make(map[string]*objects.Module, len(keys))
	for _, k := range keys {
		ks, err := objects.Str(k)
		if err != nil {
			continue
		}
		v, err := sysModules.GetItem(k)
		if err != nil || v == nil {
			continue
		}
		if m, ok := v.(*objects.Module); ok {
			snap[ks] = m
		}
	}
	return snap
}
