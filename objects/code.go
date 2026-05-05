// Code object port of cpython/Include/internal/pycore_code.h and
// Objects/codeobject.c. The Code struct holds the bytecode, the
// constants tuple, the variable name tables, the linetable and
// exception table blobs, and the meta fields that bytecode
// dispatch and traceback formatting both consume.
//
// The runtime's bytecode interpreter (1685) and the traceback
// renderer (1686) read this struct without writing back, so
// fields stay public.

package objects

// Code is the AST -> bytecode handoff value. Compile produces
// one of these per code-bearing node (module, function body,
// class body, comprehension).
//
// CPython: Include/internal/pycore_code.h:115 _PyCodeObject
type Code struct {
	// Argument shape. CPython sets all four counts at compile time;
	// the interpreter reads them when binding a CALL_FUNCTION.
	Argcount        int
	PosonlyArgcount int
	KwonlyArgcount  int
	Stacksize       int

	// Flags carries the CO_* bitset (CO_OPTIMIZED, CO_NEWLOCALS,
	// CO_VARARGS, CO_VARKEYWORDS, CO_NESTED, CO_GENERATOR,
	// CO_COROUTINE, CO_ASYNC_GENERATOR, etc.).
	Flags int

	// Code is the bytecode blob. Each instruction is a 16-bit
	// (op, arg) pair; the interpreter dispatch loop walks it.
	Code []byte

	// Consts is the literal table the LOAD_CONST opcode indexes into.
	Consts []any

	// Names, Varnames, Freevars, Cellvars are name tables indexed
	// by their respective LOAD_/STORE_ opcodes.
	Names    []string
	Varnames []string
	Freevars []string
	Cellvars []string

	// Filename, Name, Qualname mirror co_filename / co_name /
	// co_qualname. The traceback renderer reads them.
	Filename string
	Name     string
	Qualname string

	// Firstlineno is the source line of the first statement in
	// the code; the linetable encodes deltas from this anchor.
	Firstlineno int

	// Linetable is the PEP 626 location table. Decoded via
	// CoLines / CoPositions in code_tables.go.
	Linetable []byte

	// ExceptionTable is the compact try/except table the
	// interpreter walks on RAISE_VARARGS / END_FINALLY.
	ExceptionTable []byte
}
