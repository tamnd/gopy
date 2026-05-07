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

import (
	"fmt"
	"reflect"
)

// Code is the AST -> bytecode handoff value. Compile produces
// one of these per code-bearing node (module, function body,
// class body, comprehension).
//
// CPython: Include/internal/pycore_code.h:115 _PyCodeObject
type Code struct {
	Header

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

	// Quickened is true when the bytecode has been Quickened: every
	// adaptive opcode is followed by its inline cache cells, with
	// counters seeded by specialize.Quicken. The dispatch loop only
	// drives the adaptive specializer / deopt loop on Quickened code.
	//
	// CPython: Include/internal/pycore_code.h _PyCode_QUICKENED
	Quickened bool
}

// CodeType is the type singleton for code objects.
//
// CPython: Objects/codeobject.c:2980 PyCode_Type
var CodeType = NewType("code", []*Type{objectType})

func init() {
	CodeType.Repr = codeRepr
	CodeType.Str = codeRepr
	CodeType.Hash = codeHash
	CodeType.RichCmp = codeRichCompare
}

// NewCode returns a Code with its header bound to CodeType.
func NewCode() *Code {
	c := &Code{}
	c.init(CodeType)
	return c
}

// codeRepr formats as <code object NAME at PTR, file "FILE", line LINE>.
// Mirrors the format string code_repr feeds to PyUnicode_FromFormat.
//
// CPython: Objects/codeobject.c:2572 code_repr
func codeRepr(o Object) (string, error) {
	c := o.(*Code)
	lineno := c.Firstlineno
	if lineno == 0 {
		lineno = -1
	}
	if c.Filename != "" {
		return fmt.Sprintf("<code object %s at %p, file %q, line %d>", c.Name, c, c.Filename, lineno), nil
	}
	return fmt.Sprintf("<code object %s at %p, file ???, line %d>", c.Name, c, lineno), nil
}

// codeHash scrambles the same identity-defining fields code_hash mixes
// in CPython: name, consts, names, localsplus names, linetable,
// exceptiontable, then the integer counts and the bytecode stream.
// PyHASH_MULTIPLIER is the same prime CPython uses for tuples and
// frozenset.
//
// CPython: Objects/codeobject.c:2681 code_hash
func codeHash(o Object) (int64, error) {
	c := o.(*Code)
	const mult uint64 = 0xf4243
	uhash := uint64(20221211)
	mix := func(h int64) {
		uhash ^= uint64(h)
		uhash *= mult
	}
	mix(HashString(c.Name))
	mix(constsHash(c.Consts))
	mix(stringsHash(c.Names))
	mix(stringsHash(c.Varnames))
	mix(stringsHash(c.Cellvars))
	mix(stringsHash(c.Freevars))
	mix(HashBytes(c.Linetable))
	mix(HashBytes(c.ExceptionTable))
	mix(int64(c.Argcount))
	mix(int64(c.PosonlyArgcount))
	mix(int64(c.KwonlyArgcount))
	mix(int64(c.Flags))
	mix(int64(c.Firstlineno))
	mix(int64(len(c.Code)))
	mix(HashBytes(c.Code))
	h := int64(uhash)
	if h == -1 {
		return -2, nil
	}
	return h, nil
}

// codeRichCompare implements == and != by deep-comparing every field
// code_richcompare looks at. <, <=, >, >= return NotImplemented,
// matching the early-out in the CPython implementation.
//
// CPython: Objects/codeobject.c:2592 code_richcompare
func codeRichCompare(a, b Object, op CompareOp) (Object, error) {
	if op != CompareEQ && op != CompareNE {
		return NotImplemented(), nil
	}
	cb, ok := b.(*Code)
	if !ok {
		return NotImplemented(), nil
	}
	ca := a.(*Code)
	eq := codeEqual(ca, cb)
	if op == CompareNE {
		eq = !eq
	}
	return NewBool(eq), nil
}

func codeEqual(a, b *Code) bool {
	if a.Name != b.Name ||
		a.Argcount != b.Argcount ||
		a.PosonlyArgcount != b.PosonlyArgcount ||
		a.KwonlyArgcount != b.KwonlyArgcount ||
		a.Flags != b.Flags ||
		a.Firstlineno != b.Firstlineno ||
		!byteSliceEqual(a.Code, b.Code) ||
		!stringSliceEqual(a.Names, b.Names) ||
		!stringSliceEqual(a.Varnames, b.Varnames) ||
		!stringSliceEqual(a.Cellvars, b.Cellvars) ||
		!stringSliceEqual(a.Freevars, b.Freevars) ||
		!byteSliceEqual(a.Linetable, b.Linetable) ||
		!byteSliceEqual(a.ExceptionTable, b.ExceptionTable) {
		return false
	}
	return constsEqual(a.Consts, b.Consts)
}

func byteSliceEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func constsEqual(a, b []any) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !reflect.DeepEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}

func stringsHash(ss []string) int64 {
	const mult uint64 = 0xf4243
	uhash := uint64(len(ss))
	for _, s := range ss {
		uhash ^= uint64(HashString(s))
		uhash *= mult
	}
	return int64(uhash)
}

func constsHash(consts []any) int64 {
	const mult uint64 = 0xf4243
	uhash := uint64(len(consts))
	for _, v := range consts {
		uhash ^= uint64(HashString(fmt.Sprintf("%T:%v", v, v)))
		uhash *= mult
	}
	return int64(uhash)
}

// CodeReplace lists every field code.replace accepts in CPython. A
// nil pointer means "leave the field unchanged"; a non-nil pointer
// (or non-nil slice) installs the override. Mirrors the keyword-arg
// surface of code.replace and its sibling code.__replace__ method.
//
// CPython: Objects/codeobject.c:2858 code_replace_impl
type CodeReplace struct {
	Argcount        *int
	PosonlyArgcount *int
	KwonlyArgcount  *int
	Stacksize       *int
	Flags           *int
	Firstlineno     *int
	Code            []byte
	Consts          []any
	Names           []string
	Varnames        []string
	Freevars        []string
	Cellvars        []string
	Filename        *string
	Name            *string
	Qualname        *string
	Linetable       []byte
	ExceptionTable  []byte

	// SetCode / SetConsts / ... let callers explicitly install a nil
	// or empty slice override. Without these the zero-length slice
	// is indistinguishable from "no override" and would always fall
	// through to the original.
	SetCode           bool
	SetConsts         bool
	SetNames          bool
	SetVarnames       bool
	SetFreevars       bool
	SetCellvars       bool
	SetLinetable      bool
	SetExceptionTable bool
}

// Replace returns a copy of c with the fields named in r overridden.
// The receiver is not modified.
//
// CPython: Objects/codeobject.c:2858 code_replace_impl
func (c *Code) Replace(r CodeReplace) (*Code, error) {
	if err := nonNegative(r.Argcount, "co_argcount"); err != nil {
		return nil, err
	}
	if err := nonNegative(r.PosonlyArgcount, "co_posonlyargcount"); err != nil {
		return nil, err
	}
	if err := nonNegative(r.KwonlyArgcount, "co_kwonlyargcount"); err != nil {
		return nil, err
	}
	if err := nonNegative(r.Stacksize, "co_stacksize"); err != nil {
		return nil, err
	}
	if err := nonNegative(r.Flags, "co_flags"); err != nil {
		return nil, err
	}
	if err := nonNegative(r.Firstlineno, "co_firstlineno"); err != nil {
		return nil, err
	}
	out := c.Copy()
	if r.Argcount != nil {
		out.Argcount = *r.Argcount
	}
	if r.PosonlyArgcount != nil {
		out.PosonlyArgcount = *r.PosonlyArgcount
	}
	if r.KwonlyArgcount != nil {
		out.KwonlyArgcount = *r.KwonlyArgcount
	}
	if r.Stacksize != nil {
		out.Stacksize = *r.Stacksize
	}
	if r.Flags != nil {
		out.Flags = *r.Flags
	}
	if r.Firstlineno != nil {
		out.Firstlineno = *r.Firstlineno
	}
	if r.SetCode {
		out.Code = cloneBytes(r.Code)
	}
	if r.SetConsts {
		out.Consts = cloneConsts(r.Consts)
	}
	if r.SetNames {
		out.Names = cloneStrings(r.Names)
	}
	if r.SetVarnames {
		out.Varnames = cloneStrings(r.Varnames)
	}
	if r.SetFreevars {
		out.Freevars = cloneStrings(r.Freevars)
	}
	if r.SetCellvars {
		out.Cellvars = cloneStrings(r.Cellvars)
	}
	if r.Filename != nil {
		out.Filename = *r.Filename
	}
	if r.Name != nil {
		out.Name = *r.Name
	}
	if r.Qualname != nil {
		out.Qualname = *r.Qualname
	}
	if r.SetLinetable {
		out.Linetable = cloneBytes(r.Linetable)
	}
	if r.SetExceptionTable {
		out.ExceptionTable = cloneBytes(r.ExceptionTable)
	}
	return out, nil
}

// Copy returns a deep copy of c. Slice fields are duplicated so the
// result can be mutated independently.
func (c *Code) Copy() *Code {
	out := NewCode()
	out.Argcount = c.Argcount
	out.PosonlyArgcount = c.PosonlyArgcount
	out.KwonlyArgcount = c.KwonlyArgcount
	out.Stacksize = c.Stacksize
	out.Flags = c.Flags
	out.Firstlineno = c.Firstlineno
	out.Filename = c.Filename
	out.Name = c.Name
	out.Qualname = c.Qualname
	out.Code = cloneBytes(c.Code)
	out.Consts = cloneConsts(c.Consts)
	out.Names = cloneStrings(c.Names)
	out.Varnames = cloneStrings(c.Varnames)
	out.Freevars = cloneStrings(c.Freevars)
	out.Cellvars = cloneStrings(c.Cellvars)
	out.Linetable = cloneBytes(c.Linetable)
	out.ExceptionTable = cloneBytes(c.ExceptionTable)
	return out
}

func nonNegative(p *int, name string) error {
	if p != nil && *p < 0 {
		return fmt.Errorf("ValueError: %s must be a positive integer", name)
	}
	return nil
}

func cloneBytes(s []byte) []byte {
	if s == nil {
		return nil
	}
	out := make([]byte, len(s))
	copy(out, s)
	return out
}

func cloneStrings(s []string) []string {
	if s == nil {
		return nil
	}
	out := make([]string, len(s))
	copy(out, s)
	return out
}

func cloneConsts(s []any) []any {
	if s == nil {
		return nil
	}
	out := make([]any, len(s))
	copy(out, s)
	return out
}
