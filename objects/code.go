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
	"sync/atomic"
)

// nextCodeVersion is the monotonic counter that backs co_version.
// Each fresh Code object claims one slot via AllocCodeVersion so the
// CALL family specializer can stamp a stable cache key for the
// _CHECK_FUNCTION_VERSION guard.
//
// CPython: Include/internal/pycore_function.h func_state.next_version
var nextCodeVersion atomic.Uint32

// AllocCodeVersion returns the next monotonic co_version. Mirrors
// CPython's func_state.next_version bump in _PyCode_New; values start
// at FUNC_VERSION_FIRST_VALID (1) so 0 stays the "unset" sentinel.
//
// CPython: Objects/codeobject.c:556 (co_version = next_version)
func AllocCodeVersion() uint32 {
	return nextCodeVersion.Add(1)
}

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

	// ConstObjs is the cached Object form of Consts, populated at
	// Code construction time by SyncConstObjs. CPython stores co_consts
	// as a tuple of PyObject* directly so LOAD_CONST is one pointer
	// load. Without this slice every LOAD_CONST would re-run wrapConst's
	// type switch and re-allocate an Int / Str / Tuple per dispatch,
	// which the dispatch profile showed at 5.54% of CPU. Built lazily
	// via SyncConstObjs to keep marshal round-trips intact (Consts is
	// still the wire form).
	//
	// CPython: Include/cpython/code.h:107 co_consts
	ConstObjs []Object

	// Names, Varnames, Freevars, Cellvars are name tables indexed
	// by their respective LOAD_/STORE_ opcodes.
	Names    []string
	Varnames []string
	Freevars []string
	Cellvars []string

	// NameObjs is the cached *Unicode form of Names, populated at
	// Code construction time. CPython stores co_names as a tuple of
	// interned PyUnicode objects so every LOAD_NAME / LOAD_GLOBAL /
	// LOAD_ATTR / STORE_ATTR reuses the same object (and its cached
	// hash) across calls. Without this slice each name lookup would
	// allocate a fresh *Unicode and re-classify the string + recompute
	// the SipHash on every dispatch, which dominates the LOAD_GLOBAL
	// fallback path. Built lazily via SyncNameObjs to keep marshal
	// round-trips intact (Names is still the wire form).
	//
	// CPython: Include/cpython/code.h:108 co_names
	NameObjs []*Unicode

	// LocalsplusNames / LocalsplusKinds carry the flat 3.11+
	// co_localsplus layout: every named slot the frame allocates
	// for fastlocals, cells, and frees, paired with the
	// CO_FAST_* kind byte computed by Python/assemble.c's
	// compute_localsplus_info. Marshal writes both fields straight
	// to the wire so .pyc round-trips byte-for-byte.
	//
	// CPython: Include/cpython/code.h:91 co_localsplusnames
	// CPython: Python/assemble.c:483 compute_localsplus_info
	LocalsplusNames []string
	LocalsplusKinds []byte

	// Nlocalsplus / Nlocals / Ncellvars / Nfreevars are the cached
	// counts CPython precomputes in init_code from
	// localspluskinds. They are NOT
	// len(Varnames)+len(Cellvars)+len(Freevars): when a cellvar's
	// name overlaps with a varname (arg cells), fix_cell_offsets
	// merges the two slots and the compacted nlocalsplus drops
	// below the naive sum. Frame layout, COPY_FREE_VARS, and
	// LOAD_DEREF / STORE_DEREF all read these counts; without them
	// the frame allocates one slot too many per arg cell and the
	// fix_cell_offsets-rewritten opargs land on un-populated cells.
	//
	// CPython: Include/cpython/code.h:84 co_nlocalsplus
	// CPython: Objects/codeobject.c:389 get_localsplus_counts
	// CPython: Objects/codeobject.c:548 (init_code stores derived counts)
	Nlocalsplus int
	Nlocals     int
	Ncellvars   int
	Nfreevars   int

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

	// MonitoringData is the per-code PEP 669 instrumentation slab,
	// allocated lazily by the instrument pass. Stored as any so the
	// objects package stays independent of monitor; the monitor
	// package owns the concrete *monitor.CoMonitoringData type and
	// asserts it back at use sites.
	//
	// CPython: Include/internal/pycore_code.h _co_monitoring
	MonitoringData any

	// MonitoringVersion is the global monitoring version snapshot
	// from the last instrument pass. The shadow walk re-instruments
	// when this drifts from the interpreter's current version.
	//
	// CPython: Include/internal/pycore_code.h _co_instrumentation_version
	MonitoringVersion uint32

	// Executors is the lazily-allocated Tier-2 executor side table.
	// ENTER_EXECUTOR oparg indexes into Executors.Entries[]. Stored as
	// any so the objects package stays independent of optimizer; the
	// optimizer package owns the concrete *optimizer.ExecutorArray
	// type and asserts it back at use sites.
	//
	// CPython: Include/internal/pycore_code.h co_executors
	Executors any

	// Version is the per-code monotonic id stamped by AllocCodeVersion
	// at construction time. MAKE_FUNCTION copies it into the Function's
	// Version field so the CALL specializer can write a stable
	// _CHECK_FUNCTION_VERSION guard. Zero means "not yet versioned"
	// and matches CPython's FUNC_VERSION_UNSET sentinel.
	//
	// CPython: Include/cpython/code.h:90 co_version
	Version uint32

	// CacheObjects is gopy's stand-in for CPython's in-cache pointer
	// slots. CPython packs the cached descriptor / function object
	// pointer into 4 codeunits of the inline cache (write_obj +
	// read_obj in pycore_code.h). Go cannot stash GC-tracked pointers
	// inside a []byte, so per-codeunit pointer cells live here,
	// indexed by codeunit index of the opcode that owns the slot.
	// Allocated by specialize.Enable; nil for opcodes that don't
	// cache a pointer. Validity is gated by the same version cells in
	// Code so a stale pointer is never observed.
	//
	// CPython: Include/internal/pycore_code.h:175 write_obj / read_obj
	CacheObjects []Object
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
	CodeType.Getattro = codeGetAttr
	// code.replace(**kwargs) returns a new code object with the named
	// fields overridden. types.coroutine and test.support both reach
	// for this.
	//
	// CPython: Objects/codeobject.c:2858 code_replace_impl
	SetTypeDescr(CodeType, "replace", NewMethodDescr(CodeType, "replace", codeReplaceMethod))
	// copy.replace() dispatches through __replace__; wire it to the same impl.
	//
	// CPython: Objects/codeobject.c:2858 code_replace_impl (same function)
	SetTypeDescr(CodeType, "__replace__", NewMethodDescr(CodeType, "__replace__", codeReplaceMethod))
}

// codeReplaceMethod backs code.replace. The first positional argument
// is the receiver; everything else comes via kwargs and maps onto the
// CodeReplace struct.
//
// CPython: Objects/codeobject.c:2858 code_replace_impl
func codeReplaceMethod(args []Object, kwargs map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: code.replace() takes no positional arguments (%d given)", len(args)-1)
	}
	c := args[0].(*Code)
	r := CodeReplace{}
	intPtr := func(o Object) (*int, error) {
		i, ok := o.(*Int)
		if !ok {
			return nil, fmt.Errorf("TypeError: replace() argument must be int, not %s", o.Type().Name)
		}
		iv, _ := i.Int64()
		v := int(iv)
		return &v, nil
	}
	strPtr := func(o Object) (*string, error) {
		s, ok := o.(*Unicode)
		if !ok {
			return nil, fmt.Errorf("TypeError: replace() argument must be str, not %s", o.Type().Name)
		}
		v := s.v
		return &v, nil
	}
	for k, v := range kwargs {
		switch k {
		case "co_argcount":
			p, err := intPtr(v)
			if err != nil {
				return nil, err
			}
			r.Argcount = p
		case "co_posonlyargcount":
			p, err := intPtr(v)
			if err != nil {
				return nil, err
			}
			r.PosonlyArgcount = p
		case "co_kwonlyargcount":
			p, err := intPtr(v)
			if err != nil {
				return nil, err
			}
			r.KwonlyArgcount = p
		case "co_stacksize":
			p, err := intPtr(v)
			if err != nil {
				return nil, err
			}
			r.Stacksize = p
		case "co_flags":
			p, err := intPtr(v)
			if err != nil {
				return nil, err
			}
			r.Flags = p
		case "co_firstlineno":
			p, err := intPtr(v)
			if err != nil {
				return nil, err
			}
			r.Firstlineno = p
		case "co_filename":
			p, err := strPtr(v)
			if err != nil {
				return nil, err
			}
			r.Filename = p
		case "co_name":
			p, err := strPtr(v)
			if err != nil {
				return nil, err
			}
			r.Name = p
		case "co_qualname":
			p, err := strPtr(v)
			if err != nil {
				return nil, err
			}
			r.Qualname = p
		case "co_nlocals":
			p, err := intPtr(v)
			if err != nil {
				return nil, err
			}
			r.Nlocals = p
		case "co_code":
			b, ok := v.(*Bytes)
			if !ok {
				return nil, fmt.Errorf("TypeError: replace() co_code must be bytes, not %s", v.Type().Name)
			}
			r.Code = b.Bytes()
			r.SetCode = true
		case "co_linetable":
			b, ok := v.(*Bytes)
			if !ok {
				return nil, fmt.Errorf("TypeError: replace() co_linetable must be bytes, not %s", v.Type().Name)
			}
			r.Linetable = b.Bytes()
			r.SetLinetable = true
		case "co_exceptiontable":
			b, ok := v.(*Bytes)
			if !ok {
				return nil, fmt.Errorf("TypeError: replace() co_exceptiontable must be bytes, not %s", v.Type().Name)
			}
			r.ExceptionTable = b.Bytes()
			r.SetExceptionTable = true
		case "co_consts":
			t, ok := v.(*Tuple)
			if !ok {
				return nil, fmt.Errorf("TypeError: replace() co_consts must be tuple, not %s", v.Type().Name)
			}
			consts := make([]any, t.Len())
			for i := range consts {
				consts[i] = t.Item(i)
			}
			r.Consts = consts
			r.SetConsts = true
		case "co_names":
			strs, err := tupleToStrings(v, "co_names")
			if err != nil {
				return nil, err
			}
			r.Names = strs
			r.SetNames = true
		case "co_varnames":
			strs, err := tupleToStrings(v, "co_varnames")
			if err != nil {
				return nil, err
			}
			r.Varnames = strs
			r.SetVarnames = true
		case "co_freevars":
			strs, err := tupleToStrings(v, "co_freevars")
			if err != nil {
				return nil, err
			}
			r.Freevars = strs
			r.SetFreevars = true
		case "co_cellvars":
			strs, err := tupleToStrings(v, "co_cellvars")
			if err != nil {
				return nil, err
			}
			r.Cellvars = strs
			r.SetCellvars = true
		default:
			return nil, fmt.Errorf("TypeError: code.replace() got unexpected keyword argument %q", k)
		}
	}
	return c.Replace(r)
}

// tupleToStrings converts a Python tuple-of-str to a []string.
//
// CPython: Objects/codeobject.c:2858 code_replace_impl (varnames / names validation)
func tupleToStrings(o Object, field string) ([]string, error) {
	t, ok := o.(*Tuple)
	if !ok {
		return nil, fmt.Errorf("TypeError: replace() %s must be tuple, not %s", field, o.Type().Name)
	}
	out := make([]string, t.Len())
	for i := range out {
		s, ok := t.Item(i).(*Unicode)
		if !ok {
			return nil, fmt.Errorf("TypeError: replace() %s items must be str", field)
		}
		out[i] = s.v
	}
	return out, nil
}

// codeGetAttr exposes the read-only co_* fields the traceback
// renderer and pdb consult: co_filename, co_name, co_qualname,
// co_firstlineno, co_argcount, co_posonlyargcount, co_kwonlyargcount,
// co_stacksize, co_flags.
//
// CPython: Objects/codeobject.c:2960 code_memberlist
func codeGetAttr(o Object, name Object) (Object, error) {
	c, ok := o.(*Code)
	if !ok {
		return GenericGetAttr(o, name)
	}
	n, ok := name.(*Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: attribute name must be string, not '%s'", typeNameOf(name))
	}
	switch n.v {
	case "co_filename":
		return NewStr(c.Filename), nil
	case "co_name":
		return NewStr(c.Name), nil
	case "co_qualname":
		q := c.Qualname
		if q == "" {
			q = c.Name
		}
		return NewStr(q), nil
	case "co_firstlineno":
		return NewInt(int64(c.Firstlineno)), nil
	case "co_argcount":
		return NewInt(int64(c.Argcount)), nil
	case "co_posonlyargcount":
		return NewInt(int64(c.PosonlyArgcount)), nil
	case "co_kwonlyargcount":
		return NewInt(int64(c.KwonlyArgcount)), nil
	case "co_stacksize":
		return NewInt(int64(c.Stacksize)), nil
	case "co_flags":
		return NewInt(int64(c.Flags)), nil
	}
	if v, ok := codeAttrLookup(c, n.v); ok {
		return v, nil
	}
	return GenericGetAttr(o, name)
}

// NewCode returns a Code with its header bound to CodeType and a
// fresh monotonic Version stamped in.
func NewCode() *Code {
	c := &Code{Version: AllocCodeVersion()}
	c.init(CodeType)
	return c
}

// CO_FAST_* kind bits mirror Include/internal/pycore_code.h:180.
// gopy reuses the CPython values byte-for-byte so marshal can write
// LocalsplusKinds straight to the .pyc wire format.
//
// CPython: Include/internal/pycore_code.h:180 CO_FAST_*
const (
	CoFastArgPos uint8 = 0x02
	CoFastArgKw  uint8 = 0x04
	CoFastArgVar uint8 = 0x08
	CoFastArg    uint8 = CoFastArgPos | CoFastArgKw | CoFastArgVar
	CoFastHidden uint8 = 0x10
	CoFastLocal  uint8 = 0x20
	CoFastCell   uint8 = 0x40
	CoFastFree   uint8 = 0x80
)

// SyncLocalsplusCounts walks LocalsplusKinds and refreshes Nlocalsplus
// / Nlocals / Ncellvars / Nfreevars to match. CPython precomputes the
// same trio in init_code from the kinds tuple so every later read
// (frame layout, COPY_FREE_VARS, dis) gets one consistent view.
// Construction sites (liftNestedCode, marshal decode, test fixtures)
// call this after populating LocalsplusNames / LocalsplusKinds.
//
// CPython: Objects/codeobject.c:389 get_localsplus_counts
// CPython: Objects/codeobject.c:548 (init_code stores the derived counts)
func (c *Code) SyncLocalsplusCounts() {
	c.Nlocalsplus = len(c.LocalsplusNames)
	nlocals := 0
	ncellvars := 0
	nfreevars := 0
	for i := 0; i < len(c.LocalsplusKinds) && i < c.Nlocalsplus; i++ {
		kind := c.LocalsplusKinds[i]
		switch {
		case kind&CoFastLocal != 0:
			nlocals++
			if kind&CoFastCell != 0 {
				ncellvars++
			}
		case kind&CoFastCell != 0:
			ncellvars++
		case kind&CoFastFree != 0:
			nfreevars++
		}
	}
	c.Nlocals = nlocals
	c.Ncellvars = ncellvars
	c.Nfreevars = nfreevars
}

// SyncNameObjs rebuilds NameObjs to match the current Names slice.
// Construction sites (NewCode caller, lift helpers, marshal decode)
// call this after populating Names so the dispatch loop can index
// straight into NameObjs without minting a fresh *Unicode per call.
//
// CPython: Objects/codeobject.c:421 _PyCode_New (co_names tuple is
// stored verbatim from the compiler).
func (c *Code) SyncNameObjs() {
	if len(c.Names) == 0 {
		c.NameObjs = nil
		return
	}
	if cap(c.NameObjs) < len(c.Names) {
		c.NameObjs = make([]*Unicode, len(c.Names))
	} else {
		c.NameObjs = c.NameObjs[:len(c.Names)]
	}
	for i, s := range c.Names {
		c.NameObjs[i] = NewStr(s).(*Unicode)
	}
}

// SyncConstObjs rebuilds ConstObjs to match the current Consts slice.
// Construction sites populate Consts then call this so LOAD_CONST can
// read straight from the cached slice without re-running wrapConst per
// dispatch. Mirrors SyncNameObjs for the consts side.
//
// CPython: Objects/codeobject.c:421 _PyCode_New (co_consts tuple is
// stored verbatim from the compiler so the runtime reads it directly).
func (c *Code) SyncConstObjs() {
	if len(c.Consts) == 0 {
		c.ConstObjs = nil
		return
	}
	// Release the pin held on any tuple const from a previous sync before
	// the slice is overwritten, so a re-sync does not leak the old pin.
	for _, old := range c.ConstObjs {
		if t, ok := old.(*Tuple); ok {
			Decref(t)
		}
	}
	if cap(c.ConstObjs) < len(c.Consts) {
		c.ConstObjs = make([]Object, len(c.Consts))
	} else {
		c.ConstObjs = c.ConstObjs[:len(c.Consts)]
	}
	for i, v := range c.Consts {
		obj := wrapConstAttr(v)
		// Pin tuple consts: co_consts is an un-counted Go field, so a
		// constant tuple would otherwise sit at refcount 0 while live
		// and be torn down by tuple_dealloc the first time the VM
		// transiently decrefs it (e.g. after a LOAD_CONST is consumed).
		// CPython holds co_consts as a counted reference; this Incref is
		// that reference.
		if t, ok := obj.(*Tuple); ok {
			Incref(t)
		}
		c.ConstObjs[i] = obj
	}
}

// ConstObj returns the cached Object for Consts[i]. Falls back to
// re-wrapping when ConstObjs is missing or out of sync, which covers
// test fixtures that build Code by struct literal without calling
// SyncConstObjs.
func (c *Code) ConstObj(i int) Object {
	if i >= 0 && i < len(c.ConstObjs) && c.ConstObjs[i] != nil {
		return c.ConstObjs[i]
	}
	if i < 0 || i >= len(c.Consts) {
		return nil
	}
	return wrapConstAttr(c.Consts[i])
}

// NameObj returns the cached *Unicode for Names[i]. Falls back to
// minting a fresh object when NameObjs is missing or out of sync,
// which covers test fixtures that build Code by struct literal
// without calling SyncNameObjs.
func (c *Code) NameObj(i int) *Unicode {
	if i >= 0 && i < len(c.NameObjs) && c.NameObjs[i] != nil {
		return c.NameObjs[i]
	}
	if i < 0 || i >= len(c.Names) {
		return nil
	}
	return NewStr(c.Names[i]).(*Unicode)
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
		return fmt.Sprintf("<code object %s at %p, file \"%s\", line %d>", c.Name, c, c.Filename, lineno), nil
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
	Nlocals         *int
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
		out.SyncConstObjs()
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
	if r.SetVarnames || r.SetCellvars || r.SetFreevars {
		// One of the name tables changed, so the flat localsplus layout
		// Copy carried over is stale. Rebuild it the way the constructor
		// would. CPython: Objects/codeobject.c:2932 routes replace through
		// PyCode_NewWithPosOnlyArgs, which recomputes co_localsplusnames.
		out.rebuildLocalsplus()
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
	// co_nlocals must equal len(co_varnames) after all replacements are applied.
	// CPython: Objects/codeobject.c:2858 code_replace_impl nlocals validation
	if r.Nlocals != nil && *r.Nlocals != len(out.Varnames) {
		return nil, fmt.Errorf("ValueError: co_nlocals (%d) != len(co_varnames) (%d)", *r.Nlocals, len(out.Varnames))
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
	// The flat localsplus layout (and its derived counts) is part of the
	// code object's identity: co_localsplusnames is what _varname_from_oparg,
	// the frame allocator, and dis all index. CPython rebuilds it in the
	// constructor; carrying it here keeps an unmodified Copy / no-arg
	// replace() truly identical instead of dropping the layout and tripping
	// "_varname_from_oparg(): oparg out of range".
	//
	// CPython: Objects/codeobject.c:536 _PyCode_New (co_localsplusnames)
	out.LocalsplusNames = cloneStrings(c.LocalsplusNames)
	out.LocalsplusKinds = cloneBytes(c.LocalsplusKinds)
	out.Nlocalsplus = c.Nlocalsplus
	out.Nlocals = c.Nlocals
	out.Ncellvars = c.Ncellvars
	out.Nfreevars = c.Nfreevars
	out.Linetable = cloneBytes(c.Linetable)
	out.ExceptionTable = cloneBytes(c.ExceptionTable)
	out.SyncNameObjs()
	out.SyncConstObjs()
	return out
}

// rebuildLocalsplus recomputes LocalsplusNames / LocalsplusKinds and the
// derived counts from Varnames / Cellvars / Freevars, mirroring the flat
// layout the constructor builds: varnames first (CO_FAST_LOCAL), then
// cellvars (CO_FAST_CELL, merged into the matching arg slot when a cell
// shares a name with a varname), then freevars (CO_FAST_FREE). Replace
// calls this whenever one of those three name tables changes so the
// localsplus view stays consistent, exactly as code_replace_impl does by
// routing through PyCode_NewWithPosOnlyArgs.
//
// CPython: Objects/codeobject.c:802 _PyCode_New localsplus build
func (c *Code) rebuildLocalsplus() {
	names := make([]string, 0, len(c.Varnames)+len(c.Cellvars)+len(c.Freevars))
	kinds := make([]byte, 0, cap(names))
	for _, name := range c.Varnames {
		names = append(names, name)
		kinds = append(kinds, CoFastLocal)
	}
	for _, cell := range c.Cellvars {
		argoffset := -1
		for j, v := range c.Varnames {
			if v == cell {
				argoffset = j
				break
			}
		}
		if argoffset >= 0 {
			// Cell shares a slot with the argument of the same name.
			kinds[argoffset] |= CoFastCell
			continue
		}
		names = append(names, cell)
		kinds = append(kinds, CoFastCell)
	}
	for _, free := range c.Freevars {
		names = append(names, free)
		kinds = append(kinds, CoFastFree)
	}
	c.LocalsplusNames = names
	c.LocalsplusKinds = kinds
	c.SyncLocalsplusCounts()
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
