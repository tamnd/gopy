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
	"errors"
	"sync"
	"sync/atomic"

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
	// Code is the precompiled code object. nil for placeholder entries
	// and for source-backed entries (compiled lazily from Source).
	Code *objects.Code
	// Source is the canonical .py source for entries whose bytecode is
	// produced lazily by FrozenCompiler rather than pre-embedded. This
	// stands in for CPython's marshaled frozen blob: gopy stores the
	// source text (vendored verbatim) and compiles it on first use.
	Source string
	// OrigName is the name find_frozen reports for the entry. Frozen
	// aliases (e.g. __phello_alias__ -> __hello__) point at a different
	// source module; FrozenImporter._resolve_filename keys the on-disk
	// __file__ off this. Empty means the entry is its own origin.
	//
	// CPython: Python/frozen.c _PyImport_FrozenAliases
	OrigName string
	// IsPackage is true when the frozen module is a package (has __path__).
	IsPackage bool

	compileMu  sync.Mutex
	compiled   *objects.Code
	compileErr error
	didCompile bool
}

// FrozenCompiler turns frozen module source into a code object. It is
// installed once at interpreter startup (cmd/gopy wires gopyCompile)
// so the imp package need not depend on parser/compile directly,
// mirroring the SourceCompiler indirection used for path imports.
//
// CPython: Python/pythonrun.c:1102 Py_CompileStringExFlags
var FrozenCompiler func(src []byte, filename string) (*objects.Code, error)

// CodeObject returns the entry's code object, compiling Source on first
// use. It returns (nil, nil) for a pure placeholder (no Code, no
// Source). The compiled result is cached so repeated imports reuse one
// code object, matching CPython's single marshaled blob per entry.
func (m *FrozenModule) CodeObject() (*objects.Code, error) {
	if m.Code != nil {
		return m.Code, nil
	}
	if m.Source == "" {
		return nil, nil
	}
	m.compileMu.Lock()
	defer m.compileMu.Unlock()
	if m.didCompile {
		return m.compiled, m.compileErr
	}
	m.didCompile = true
	if FrozenCompiler == nil {
		m.compileErr = errors.New("imp: frozen compiler not installed")
		return nil, m.compileErr
	}
	m.compiled, m.compileErr = FrozenCompiler([]byte(m.Source), "<frozen "+m.Name+">")
	return m.compiled, m.compileErr
}

// HasCode reports whether the entry can yield a code object, either
// pre-embedded or compilable from Source. Placeholder entries (the
// importlib bootstrap stubs, which gopy loads from disk) return false.
func (m *FrozenModule) HasCode() bool {
	return m.Code != nil || m.Source != ""
}

// Origin returns the name find_frozen reports for the entry: OrigName
// when set, otherwise the entry's own Name.
func (m *FrozenModule) Origin() string {
	if m.OrigName != "" {
		return m.OrigName
	}
	return m.Name
}

var (
	frozenMu      sync.RWMutex
	frozenModules = map[string]*FrozenModule{}

	// frozenOverride mirrors PyConfig.use_frozen_modules under the test
	// override: >0 forces frozen on, <0 forces it off, 0 uses the
	// default. test.support.import_helper toggles it via
	// _imp._override_frozen_modules_for_tests.
	//
	// CPython: Python/import.c:2821 use_frozen
	frozenOverride atomic.Int32
)

// SetFrozenOverride records the test override for frozen-module lookup
// and returns the previous value.
//
// CPython: Python/import.c:5034 _imp__override_frozen_modules_for_tests_impl
func SetFrozenOverride(v int) int {
	return int(frozenOverride.Swap(int32(v)))
}

// UseFrozen reports whether frozen-module lookup is currently enabled.
// gopy's default (override 0) is on, matching CPython's release-build
// PyConfig.use_frozen_modules default; entries without embedded code
// still fall through to the path finder via HasCode.
//
// CPython: Python/import.c:2821 use_frozen
func UseFrozen() bool {
	switch v := frozenOverride.Load(); {
	case v > 0:
		return true
	case v < 0:
		return false
	default:
		return true
	}
}

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
