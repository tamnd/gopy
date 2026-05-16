// Core helpers used by every per-family specializer.
//
// CPython treats bytecode as an array of _Py_CODEUNIT structs, each
// of which is a packed (op, arg) byte pair that may also be read as
// a 16-bit cache or counter cell. gopy stores bytecode as a flat
// []byte; an "instr index" is the codeunit index, so byte offset
// 2*instr is the opcode and 2*(instr+k) is the kth following cache
// codeunit.
//
// CPython: Python/specialize.c:702 set_opcode
// CPython: Python/specialize.c:723 set_counter
// CPython: Python/specialize.c:739 specialize
// CPython: Python/specialize.c:753 unspecialize

package specialize

import (
	"encoding/binary"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

// SetOpcode rewrites the opcode at instr to op. Returns false when
// the slot already holds an INSTRUMENTED_* opcode (the GIL-disabled
// build's race-with-instrumentation path); the caller must abandon
// the specialize attempt.
//
// CPython: Python/specialize.c:702 set_opcode
func SetOpcode(code []byte, instr int, op compile.Opcode) bool {
	old := compile.Opcode(code[2*instr])
	if old >= compile.MIN_INSTRUMENTED_OPCODE {
		return false
	}
	code[2*instr] = byte(op)
	return true
}

// SetOparg rewrites the oparg byte at instr.
func SetOparg(code []byte, instr int, arg byte) {
	code[2*instr+1] = arg
}

// LoadCounter reads the BackoffCounter that lives in the first cache
// codeunit of an adaptive instruction. instr is the codeunit index
// of the opcode itself; the counter sits at instr+1.
//
// CPython: Python/specialize.c:730 load_counter
func LoadCounter(code []byte, instr int) BackoffCounter {
	return ForgeBackoffCounter(binary.LittleEndian.Uint16(code[2*(instr+1):]))
}

// StoreCounter writes value into the counter cell of the adaptive
// instruction at instr.
//
// CPython: Python/specialize.c:723 set_counter
func StoreCounter(code []byte, instr int, value BackoffCounter) {
	binary.LittleEndian.PutUint16(code[2*(instr+1):], value.ValueAndBackoff)
}

// CodeUnitWidth is the width in bytes of one bytecode codeunit. All
// inline-cache layouts are sized as a whole number of codeunits.
const CodeUnitWidth = 2

// readCell / writeCell are the internal building blocks used by the
// typed cache views (loadGlobalCacheView, attrCacheView, etc.). They
// stay package-private so call sites must go through a typed wrapper
// that names the CPython struct field being addressed.
//
// CPython: Include/internal/pycore_code.h:175 read_obj / write_obj
func readCell(code []byte, instr, k int) uint16 {
	return binary.LittleEndian.Uint16(code[2*(instr+k):])
}

func writeCell(code []byte, instr, k int, value uint16) {
	binary.LittleEndian.PutUint16(code[2*(instr+k):], value)
}

// SetCacheObject stashes a Go pointer in the parallel CacheObjects
// slab. CPython packs the same pointer into 4 codeunits of the inline
// cache via write_obj; gopy keeps the slab side-by-side because the
// runtime can't tuck GC-tracked pointers inside a []byte. The slot
// index is the codeunit offset of the opcode itself, matching how the
// per-instruction cache cells are addressed by `instr`.
//
// CPython: Include/internal/pycore_code.h:175 write_obj
func SetCacheObject(cache []objects.Object, instr int, value objects.Object) {
	if instr >= 0 && instr < len(cache) {
		cache[instr] = value
	}
}

// CacheObject reads the pointer slot at instr from the parallel slab.
// Returns nil when the slot is out of range or unset.
//
// CPython: Include/internal/pycore_code.h:175 read_obj
func CacheObject(cache []objects.Object, instr int) objects.Object {
	if instr < 0 || instr >= len(cache) {
		return nil
	}
	return cache[instr]
}

// Specialize rewrites the opcode at instr to specialized and stamps
// the counter cell with the cooldown shape so the next miss has
// time to settle before re-specializing. Mirrors CPython's static
// inline `specialize` helper.
//
// CPython: Python/specialize.c:739 specialize
func Specialize(code []byte, instr int, specialized compile.Opcode) {
	if !SetOpcode(code, instr, specialized) {
		return
	}
	StoreCounter(code, instr, AdaptiveCounterCooldown())
}

// Unspecialize rewrites the opcode at instr back to its adaptive
// parent and restarts the backoff counter so the next attempt waits
// exponentially longer. Used by the dispatch loop on shape mismatch.
//
// CPython: Python/specialize.c:753 unspecialize
func Unspecialize(code []byte, instr int) {
	op := compile.Opcode(code[2*instr])
	parent := Deopt(op)
	if !SetOpcode(code, instr, parent) {
		return
	}
	cur := LoadCounter(code, instr)
	StoreCounter(code, instr, AdaptiveCounterBackoff(cur))
}
