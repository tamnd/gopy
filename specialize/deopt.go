// Per-opcode cache widths and the de-optimization lookup.
//
// _PyOpcode_Caches[op] is the number of trailing _Py_CODEUNIT slots
// reserved as the inline cache for opcode op. Zero means the opcode
// has no cache. The width table lives in compile/; CacheCount is a
// thin alias so specialize-side code does not have to import compile
// at every call site.
//
// _PyOpcode_Deopt[op] maps every specialized opcode back to its
// adaptive parent. The fan-out lives in DeoptParent (specialize/family_gen.go,
// emitted from Python/bytecodes.c family() declarations). Deopt is the
// public accessor: unspecialized opcodes are fixed points (op -> op)
// so the dispatch loop never reads a zero accidentally.
//
// CPython: Include/internal/pycore_opcode_metadata.h _PyOpcode_Caches
// CPython: Include/internal/pycore_opcode_metadata.h _PyOpcode_Deopt

package specialize

import "github.com/tamnd/gopy/compile"

// CacheCount returns the number of trailing codeunits reserved as
// inline cache after op. Zero means op carries no cache. Source of
// truth lives in compile/opcode_caches.go; this wrapper exists so
// callers inside the specialize package don't pay the import-cycle
// awkwardness of taking compile directly at every site.
func CacheCount(op compile.Opcode) int {
	return compile.CacheCount(op)
}

// Deopt returns the adaptive parent of op. For an unspecialized
// opcode the result is op itself. The dispatch loop calls this
// when a specialized arm hits a shape mismatch and needs to fall
// back to the adaptive parent before re-specializing.
//
// CPython: Include/internal/pycore_code.h _PyOpcode_Deopt access
func Deopt(op compile.Opcode) compile.Opcode {
	if parent, ok := DeoptParent[op]; ok {
		return parent
	}
	return op
}
