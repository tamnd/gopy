// Per-opcode inline-cache widths.
//
// _PyOpcode_Caches[op] is the number of trailing _Py_CODEUNIT slots
// reserved as the inline cache after opcode op. The assembler emits
// these as zero codeunits (CACHE pseudo-op, oparg 0) so the
// specializer can rewrite them in place via specialize.Quicken later.
//
// Single source of truth: this table lives in compile/ alongside the
// opcode declarations. specialize.CacheCount delegates here so the
// runtime, the assembler, the disassembler, and the jump-offset
// calculator all agree on instruction stride.
//
// CPython: Include/internal/pycore_opcode_metadata.h _PyOpcode_Caches
// CPython: Lib/opcode.py _inline_cache_entries (the generator source).

package compile

// opcodeCaches indexes by Opcode and returns the cache width in
// codeunits. Zero means the opcode has no cache.
//
// CPython: Include/internal/pycore_opcode_metadata.h:1764 _PyOpcode_Caches
var opcodeCaches = [256]uint8{
	TO_BOOL:              3,
	STORE_SUBSCR:         1,
	SEND:                 1,
	UNPACK_SEQUENCE:      1,
	STORE_ATTR:           4,
	LOAD_GLOBAL:          4,
	LOAD_SUPER_ATTR:      1,
	LOAD_ATTR:            9,
	COMPARE_OP:           1,
	CONTAINS_OP:          1,
	JUMP_BACKWARD:        1,
	POP_JUMP_IF_TRUE:     1,
	POP_JUMP_IF_FALSE:    1,
	POP_JUMP_IF_NONE:     1,
	POP_JUMP_IF_NOT_NONE: 1,
	FOR_ITER:             1,
	CALL:                 3,
	CALL_KW:              3,
	BINARY_OP:            5,
}

// CacheCount returns the number of trailing CACHE codeunits the
// assembler must emit after op. Zero means op has no inline cache.
//
// CPython: Include/internal/pycore_opcode_metadata.h _PyOpcode_Caches
func CacheCount(op Opcode) int {
	if op < 0 || int(op) >= len(opcodeCaches) {
		return 0
	}
	return int(opcodeCaches[op])
}
