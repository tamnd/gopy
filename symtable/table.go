package symtable

import (
	"github.com/tamnd/gopy/future"
)

// Table holds the per-module symbol table built by Build. It mirrors
// `struct symtable` in CPython: a top-level module Entry plus a flat
// map of every block's Entry keyed by the AST node that introduced
// it.
//
// CPython: Include/internal/pycore_symtable.h:L71 struct symtable
type Table struct {
	// Filename is the source file name, used to address syntax errors.
	Filename string

	// Top is the module-level Entry. It is also reachable via
	// Blocks[<the Module node>].
	Top *Entry

	// Blocks maps each AST node that opens a block to the Entry
	// produced for it. CPython keys this dict by the C void*
	// address of the AST node; we key by the Go AST node pointer
	// (held inside an interface), which is just as unique within
	// a single Build call.
	Blocks map[any]*Entry

	// Future captures the module's __future__ feature flags. The
	// build pass consults Future.Bits & Annotations to skip
	// AnnotationBlock evaluation under PEP 563.
	Future *future.Features
}

// Lookup returns the Entry for the given AST key, or nil if no block
// was registered for it. Mirrors _PySymtable_Lookup but returns nil
// instead of raising KeyError.
//
// CPython: Python/symtable.c:L501 _PySymtable_Lookup
func (t *Table) Lookup(key any) *Entry {
	if t == nil {
		return nil
	}
	return t.Blocks[key]
}
