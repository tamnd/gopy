// Forward declaration of objects.PyCodeObject for the v0.5 compile
// pipeline. The full PyCodeObject port lands in objects/codeobject.c
// (out of scope for the 1600 series); for now compile owns a minimal
// shape that the marshal package and tests can consume.
//
// CPython: Include/cpython/code.h PyCodeObject

package compile

// FastLocalKind bits. Mirror cpython/Include/internal/pycore_code.h
// CO_FAST_LOCAL / CO_FAST_CELL / CO_FAST_FREE / CO_FAST_HIDDEN.
//
// CPython: Include/internal/pycore_code.h:L42 CO_FAST_*
const (
	FastLocal  uint8 = 0x20
	FastCell   uint8 = 0x40
	FastFree   uint8 = 0x80
	FastHidden uint8 = 0x10
)

// Code is the v0.5 placeholder for objects.Code. It carries every
// field the assembler fills today (marshal parity is the v0.5 gate;
// fields the VM needs land alongside the v0.6 interpreter port).
//
// CPython: Include/cpython/code.h PyCodeObject
type Code struct {
	Argcount        int
	PosOnlyArgCount int
	KwOnlyArgCount  int
	NLocals         int
	Stacksize       int
	Flags           uint32
	Code            []byte
	Consts          []any
	Names           []string
	VarNames        []string
	FreeVars        []string
	CellVars        []string
	LocalsPlusNames []string
	LocalsPlusKinds []uint8
	Filename        string
	Name            string
	Qualname        string
	Firstlineno     int
	Linetable       []byte
	ExceptionTable  []byte
	Nested          []*Code
	// Lifted caches the *objects.Code wrapper minted by vm.liftNestedCode.
	// The wrapper carries the Quickened flag and CacheObjects slab that
	// pair with this Code's bytes; reusing it across LOAD_CONST hits
	// keeps specialize.Enable idempotent and prevents Quicken from
	// re-running on already-specialized bytecode.
	Lifted any
}
