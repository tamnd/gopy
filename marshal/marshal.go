// Package marshal is the Go port of cpython/Python/marshal.c. It
// reads and writes the .pyc wire format that the import system uses
// to cache compiled code objects.
//
// v0.8 adds: TYPE_LONG (arbitrary-precision ints via *big.Int), FLAG_REF
// back-reference dedup in the decoder, TYPE_REF, and the full set of
// interned-string tags (TYPE_ASCII, TYPE_ASCII_INTERNED,
// TYPE_SHORT_ASCII_INTERNED, TYPE_INTERNED).
//
// CPython: Python/marshal.c
package marshal

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"unsafe"

	"github.com/tamnd/gopy/ast"
	"github.com/tamnd/gopy/objects"
)

// Version is the wire-format version Dump/Load speak. CPython 3.14
// emits version 5; the v0.5 skeleton supports the version-5 framing
// for the value subset listed in the package doc.
//
// CPython: Include/marshal.h Py_MARSHAL_VERSION
const Version = 5

// Type tags from Python/marshal.c. Single-byte discriminators on the
// wire. The high bit (0x80) is the FLAG_REF marker.
//
// CPython: Python/marshal.c TYPE_*
const (
	typeNull               = '0'
	typeNone               = 'N'
	typeFalse              = 'F'
	typeTrue               = 'T'
	typeStopIter           = 'S'
	typeEllipsis           = '.'
	typeInt                = 'i'
	typeInt64              = 'I'
	typeFloat              = 'f'
	typeBinaryFloat        = 'g'
	typeLong               = 'l'
	typeString             = 's'
	typeInterned           = 't'
	typeRef                = 'r'
	typeTuple              = '('
	typeSmallTuple         = ')'
	typeList               = '['
	typeDict               = '{'
	typeCode               = 'c'
	typeUnicode            = 'u'
	typeASCII              = 'a'
	typeASCIIInterned      = 'A'
	typeShortASCII         = 'z'
	typeShortASCIIInterned = 'Z'
	typeSet                = '<'
	typeFrozenset          = '>'
	typeComplex            = 'x'
	typeBinaryComplex      = 'y'
)

// flagRef is OR'd onto a type tag to signal that the object should be
// added to the back-reference table at the current index slot.
//
// CPython: Python/marshal.c FLAG_REF
const flagRef = 0x80

// ErrUnmarshallable matches CPython's WFERR_UNMARSHALLABLE.
//
// CPython: Python/marshal.c WFERR_UNMARSHALLABLE
var ErrUnmarshallable = errors.New("marshal: object cannot be marshaled")

// Dump writes v to w in the version-5 wire format.
//
// CPython: Python/marshal.c PyMarshal_WriteObjectToFile
func Dump(w io.Writer, v any) error {
	enc := encoder{
		w:           w,
		refs:        map[refKey]int{},
		sharedBytes: scanSharedBytes(v),
	}
	return enc.write(v)
}

// scanSharedBytes walks v counting linetable / exceptiontable
// contents. Any blob that appears in more than one code object is
// marked so writeCachedBytes will FLAG_REF + memo it; unique blobs
// emit plain TYPE_STRING. co_code is always memoized regardless of
// the count because _PyCode_GetCode caches it.
//
// CPython: Python/assemble.c:447 _PyCompile_ConstCacheMergeOne calls
// CPython: Python/marshal.c:391 _PyObject_IsUniquelyReferenced.
func scanSharedBytes(v any) map[string]bool {
	counts := map[string]int{}
	var visit func(x any)
	visit = func(x any) {
		switch t := x.(type) {
		case *objects.Code:
			counts[string(t.Linetable)]++
			counts[string(t.ExceptionTable)]++
			for _, k := range t.Consts {
				visit(k)
			}
		case []any:
			for _, k := range t {
				visit(k)
			}
		}
	}
	visit(v)
	out := make(map[string]bool, len(counts))
	for s, c := range counts {
		if c > 1 {
			out[s] = true
		}
	}
	return out
}

// Load reads one object from r in the version-5 wire format.
//
// CPython: Python/marshal.c PyMarshal_ReadObjectFromFile
func Load(r io.Reader) (any, error) {
	dec := decoder{r: byteReaderOf(r)}
	return dec.read()
}

type encoder struct {
	w io.Writer
	// refs maps already-emitted object identities to their assigned
	// ref-table index. Mirrors CPython's WFILE.hashtable. CPython:
	// Python/marshal.c:116 WFILE.hashtable.
	refs  map[refKey]int
	nrefs int
	// depth tracks how deep into the object graph the encoder is at the
	// moment refKeyFor is consulted. The root call sees depth==1; nested
	// recursion increments further. CPython's w_ref uses Py_REFCNT(v)
	// to decide skip vs memo: the outer code passed to marshal.dumps
	// has refcount>=2 (caller frame + arg slot) so it memos with
	// FLAG_REF, while an inner code object owned solely by its parent
	// consts tuple has refcount==1 and skips entirely. We model the
	// same emergent split via depth instead of refcounts.
	//
	// CPython: Python/marshal.c:391 _PyObject_IsUniquelyReferenced.
	depth int
	// sharedBytes records linetable / exceptiontable contents that
	// appear in more than one code object. CPython funnels both through
	// _PyCompile_ConstCacheMergeOne, so identical-content blobs end up
	// as the same PyBytes object; once the compiler frees its
	// c_const_cache the refcount on each blob equals the number of code
	// objects that point at it. Marshal's w_ref then memoizes the ones
	// with refcount > 1 and skips the singletons. We mirror that with a
	// pre-pass walk over the code tree.
	//
	// CPython: Python/compile.c:159 Py_XDECREF(c->c_const_cache)
	// CPython: Python/marshal.c:391 _PyObject_IsUniquelyReferenced.
	sharedBytes map[string]bool
}

// literalString marks a string that originated as a literal inside a
// code object's co_consts. CPython interns identifiers during parsing
// via _PyUnicode_InternImmortal so their refcount is permanently > 1
// and w_ref memoizes them with FLAG_REF + the _INTERNED tag; literals
// in co_consts go through PyUnicode_FromString / parsestr which does
// not intern, so the marshaled object has refcount 1 and w_ref skips
// the memo entirely, emitting plain TYPE_SHORT_ASCII / TYPE_ASCII /
// TYPE_UNICODE without FLAG_REF. marshalCode wraps consts strings in
// this marker to drive the same split.
//
// CPython: Python/marshal.c:380 w_ref refcount skip
// CPython: Parser/string_parser.c parsestr (literal path, no intern)
type literalString string

// refKind enumerates the comparable identity classes the encoder
// deduplicates on. CPython keys directly on PyObject*; gopy uses a
// tagged union because strings/bytes deduplicate by content (CPython
// interns them) while *objects.Code deduplicates by pointer.
type refKind uint8

const (
	refKindNone refKind = iota
	refKindStr
	refKindBytes
	refKindSmallInt
	refKindEmptyTuple
	refKindCode
)

// refKey is a comparable identity for the marshal refs table.
type refKey struct {
	kind refKind
	s    string  // str/bytes content
	i    int64   // small int value
	p    uintptr // pointer identity (code)
}

// refKeyFor decides whether v should participate in dedup, returning
// (key, true) for dedupable values and the zero key with false to
// skip. Mirrors w_ref's "skip if uniquely-referenced and not interned"
// rule: scalars that aren't CPython singletons (floats, big ints,
// non-empty tuples, non-empty mutable bytes) skip; everything else
// (strings always interned in code objects, small ints in the -5..256
// cache, empty container singletons, code objects at the root) goes
// through the memo so a duplicate triggers TYPE_REF.
//
// Depth matters for code objects: CPython skips ref machinery when
// Py_REFCNT(v) == 1. The outer code passed to marshal.dumps lives
// on at least two Python stack frames (caller and arg slot) so it
// has refcount >= 2 and lands in the refs table; a nested code held
// solely by its parent consts tuple has refcount 1 and is skipped.
//
// CPython: Python/marshal.c:380 w_ref skip arm.
func (e *encoder) refKeyFor(v any) (refKey, bool) {
	switch x := v.(type) {
	case literalString:
		_ = x
		return refKey{}, false
	case string:
		return refKey{kind: refKindStr, s: x}, true
	case []byte:
		// CPython treats empty b"" as a singleton (refcount > 1) and
		// the cached co_code bytes also share, but a freshly built
		// non-empty linetable is uniquely referenced. For the general
		// dispatch we only memo the empty singleton here; marshalCode
		// force-memos co_code through writeCodeBytes.
		if len(x) == 0 {
			return refKey{kind: refKindBytes}, true
		}
		return refKey{}, false
	case int:
		if isSmallIntCacheRange(int64(x)) {
			return refKey{kind: refKindSmallInt, i: int64(x)}, true
		}
		return refKey{}, false
	case int32:
		if isSmallIntCacheRange(int64(x)) {
			return refKey{kind: refKindSmallInt, i: int64(x)}, true
		}
		return refKey{}, false
	case int64:
		if isSmallIntCacheRange(x) {
			return refKey{kind: refKindSmallInt, i: x}, true
		}
		return refKey{}, false
	case []any:
		if len(x) == 0 {
			return refKey{kind: refKindEmptyTuple}, true
		}
		return refKey{}, false
	case *objects.Code:
		if e.depth > 1 {
			return refKey{}, false
		}
		return refKey{kind: refKindCode, p: uintptr(unsafe.Pointer(x))}, true
	}
	return refKey{}, false
}

// isSmallIntCacheRange matches CPython's interpreter-wide cached
// integers (-5..256, _PY_NSMALLNEGINTS / _PY_NSMALLPOSINTS). Those
// PyLong objects are interned, so any reference from a code object's
// const tuple sees refcount > 1 and w_ref memoizes with FLAG_REF.
// Larger ints are freshly created, uniquely referenced, and skipped.
//
// CPython: Include/internal/pycore_global_objects_fini_generated.h
// small_ints; Objects/longobject.c get_small_int.
func isSmallIntCacheRange(x int64) bool {
	return x >= -5 && x <= 256
}

// memoize stores key in the refs map at the next available index and
// advances nrefs. CPython: Python/marshal.c:421 _Py_hashtable_set.
func (e *encoder) memoize(key refKey) {
	if e.refs == nil {
		e.refs = map[refKey]int{}
	}
	e.refs[key] = e.nrefs
	e.nrefs++
}

// writeRef emits a TYPE_REF tag pointing at the given index.
//
// CPython: Python/marshal.c:406 w_byte(TYPE_REF, ...).
func (e *encoder) writeRef(idx int) error {
	if err := e.writeByte(typeRef); err != nil {
		return err
	}
	return e.writeInt32(int32(idx))
}

func (e *encoder) writeByte(b byte) error {
	_, err := e.w.Write([]byte{b})
	return err
}

func (e *encoder) writeInt32(v int32) error {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], uint32(v))
	_, err := e.w.Write(buf[:])
	return err
}

func (e *encoder) writeInt64(v int64) error {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], uint64(v))
	_, err := e.w.Write(buf[:])
	return err
}

// write dispatches on the dynamic type of v. The accepted set is
// documented in the package doc; anything else is a marshal error.
// The w_object/w_ref split is faithful to CPython: short-circuit
// singletons that get no FLAG_REF, otherwise do the memo lookup,
// returning a TYPE_REF tag if seen before and falling through to
// writeBody with FLAG_REF on first encounter.
//
// CPython: Python/marshal.c:458 w_object
func (e *encoder) write(v any) error {
	if v == nil {
		return e.writeByte(typeNone)
	}
	if b, ok := v.(bool); ok {
		if b {
			return e.writeByte(typeTrue)
		}
		return e.writeByte(typeFalse)
	}
	// The Ellipsis singleton (the `...` const) is short-circuited before
	// the FLAG_REF memo, exactly like None / True / False. gopy spells the
	// const as ast.EllipsisType; the runtime ellipsis object maps here too.
	//
	// CPython: Python/marshal.c:476 w_object (v == Py_Ellipsis)
	if isEllipsisValue(v) {
		return e.writeByte(typeEllipsis)
	}

	e.depth++
	defer func() { e.depth-- }()

	flag := byte(0)
	if key, dedupable := e.refKeyFor(v); dedupable {
		if idx, seen := e.refs[key]; seen {
			return e.writeRef(idx)
		}
		e.memoize(key)
		flag = flagRef
	}
	return e.writeBody(v, flag)
}

// writeBody emits the type byte (OR'd with flag) and the value's
// payload. Counterpart to w_complex_object; w_ref has already decided
// the FLAG_REF bit by the time we land here.
//
// CPython: Python/marshal.c:496 w_complex_object
func (e *encoder) writeBody(v any, flag byte) error {
	switch x := v.(type) {
	case int:
		return e.writeIntLike(int64(x), flag)
	case int32:
		return e.writeIntLike(int64(x), flag)
	case int64:
		return e.writeIntLike(x, flag)
	case *big.Int:
		return writeLong(e.w, x, typeLong|flag)
	case *objects.Code:
		return marshalCode(e, x, flag)
	case *objects.Set:
		return e.writeSet(x, flag)
	case map[any]any:
		return e.writeDict(x, flag)
	case complex128:
		return e.writeComplex(x, flag)
	case float64:
		if err := e.writeByte(typeBinaryFloat | flag); err != nil {
			return err
		}
		return e.writeInt64(int64(math.Float64bits(x)))
	case literalString:
		return e.writeUnicode(string(x), 0)
	case string:
		return e.writeUnicode(x, flag)
	case []byte:
		if err := e.writeByte(typeString | flag); err != nil {
			return err
		}
		if err := e.writeInt32(int32(len(x))); err != nil {
			return err
		}
		if _, err := e.w.Write(x); err != nil {
			return err
		}
		return nil
	case []any:
		return e.writeTuple(x, flag)
	}
	return fmt.Errorf("%w: %T", ErrUnmarshallable, v)
}

// writeTuple emits a tuple body. CPython picks TYPE_SMALL_TUPLE for
// length < 256 and TYPE_TUPLE otherwise.
//
// CPython: Python/marshal.c:584 w_complex_object PyTuple arm
func (e *encoder) writeTuple(items []any, flag byte) error {
	if len(items) < 256 {
		if err := e.writeByte(typeSmallTuple | flag); err != nil {
			return err
		}
		if err := e.writeByte(byte(len(items))); err != nil {
			return err
		}
	} else {
		if err := e.writeByte(typeTuple | flag); err != nil {
			return err
		}
		if err := e.writeInt32(int32(len(items))); err != nil {
			return err
		}
	}
	for _, item := range items {
		if err := e.write(item); err != nil {
			return err
		}
	}
	return nil
}

// writeCachedBytes emits a bytes payload that the compiler passed
// through _PyCompile_ConstCacheMergeOne. The three call sites in
// CPython's Python/assemble.c makecode are co_code (a_bytecode),
// co_linetable (a_linetable), and co_exceptiontable (a_except_table).
// alwaysShared marks the co_code path: _PyCode_GetCode caches the
// bytes on the code object so it carries refcount > 1 regardless of
// duplication and always memoizes with FLAG_REF. The other two only
// take the FLAG_REF path when scanSharedBytes saw them in multiple
// code objects (refcount > 1 after the compiler's const cache is
// freed); uniquely-referenced ones emit plain TYPE_STRING and skip
// the memo.
//
// CPython: Python/assemble.c:447 _PyCompile_ConstCacheMergeOne (except)
// CPython: Python/assemble.c:450 _PyCompile_ConstCacheMergeOne (line)
// CPython: Python/assemble.c:453 _PyCompile_ConstCacheMergeOne (code)
// CPython: Python/marshal.c:686 w_complex_object PyCode arm.
func (e *encoder) writeCachedBytes(b []byte, alwaysShared bool) error {
	key := refKey{kind: refKindBytes, s: string(b)}
	if idx, seen := e.refs[key]; seen {
		return e.writeRef(idx)
	}
	memo := alwaysShared || len(b) == 0 || e.sharedBytes[string(b)]
	if memo {
		e.memoize(key)
	}
	tag := byte(typeString)
	if memo {
		tag |= flagRef
	}
	if err := e.writeByte(tag); err != nil {
		return err
	}
	if err := e.writeInt32(int32(len(b))); err != nil {
		return err
	}
	_, err := e.w.Write(b)
	return err
}

// writeIntLike picks TYPE_INT for values that fit in int32. Anything
// wider goes through TYPE_LONG, matching CPython's w_object PyLong
// arm where the int32-overflow branch falls through to w_PyLong.
// TYPE_INT64 ('I') is "not generated any more" per marshal.c so we
// never emit it. flag carries FLAG_REF from the w_object dispatch.
//
// CPython: Python/marshal.c:88 TYPE_INT comment "Not generated any more"
// CPython: Python/marshal.c:501 w_object PyLong arm
func (e *encoder) writeIntLike(v int64, flag byte) error {
	if v >= math.MinInt32 && v <= math.MaxInt32 {
		if err := e.writeByte(typeInt | flag); err != nil {
			return err
		}
		return e.writeInt32(int32(v))
	}
	return writeLong(e.w, big.NewInt(v), typeLong|flag)
}

// writeUnicode emits a string. CPython interns every string that
// reaches the marshal layer through a code object (filename, name,
// qualname, co_names, co_varnames, co_cellvars, co_freevars), and the
// FLAG_REF bit is the proxy for that: w_object only memoizes strings
// that participate in dedup, which gopy refKeyFor stamps for every
// string. Pick TYPE_SHORT_ASCII_INTERNED / TYPE_ASCII_INTERNED when
// flag carries FLAG_REF; otherwise TYPE_SHORT_ASCII / TYPE_ASCII /
// TYPE_UNICODE.
//
// CPython: Python/marshal.c:528 w_PyUnicode arm
func (e *encoder) writeUnicode(s string, flag byte) error {
	if len(s) < 256 && isASCII(s) {
		tag := byte(typeShortASCII)
		if flag != 0 {
			tag = typeShortASCIIInterned
		}
		if err := e.writeByte(tag | flag); err != nil {
			return err
		}
		if err := e.writeByte(byte(len(s))); err != nil {
			return err
		}
		_, err := e.w.Write([]byte(s))
		return err
	}
	if isASCII(s) {
		tag := byte(typeASCII)
		if flag != 0 {
			tag = typeASCIIInterned
		}
		if err := e.writeByte(tag | flag); err != nil {
			return err
		}
		if err := e.writeInt32(int32(len(s))); err != nil {
			return err
		}
		_, err := e.w.Write([]byte(s))
		return err
	}
	if err := e.writeByte(typeUnicode | flag); err != nil {
		return err
	}
	if err := e.writeInt32(int32(len(s))); err != nil {
		return err
	}
	_, err := e.w.Write([]byte(s))
	return err
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

type decoder struct {
	r    io.ByteReader
	refs []any
}

func byteReaderOf(r io.Reader) io.ByteReader {
	if br, ok := r.(io.ByteReader); ok {
		return br
	}
	return &byteReader{r: r}
}

type byteReader struct {
	r io.Reader
}

func (b *byteReader) ReadByte() (byte, error) {
	var buf [1]byte
	_, err := io.ReadFull(b.r, buf[:])
	return buf[0], err
}

func (d *decoder) readByte() (byte, error) {
	return d.r.ReadByte()
}

func (d *decoder) readN(n int) ([]byte, error) {
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		b, err := d.r.ReadByte()
		if err != nil {
			return nil, err
		}
		out[i] = b
	}
	return out, nil
}

func (d *decoder) readInt32() (int32, error) {
	buf, err := d.readN(4)
	if err != nil {
		return 0, err
	}
	return int32(binary.LittleEndian.Uint32(buf)), nil
}

func (d *decoder) readInt64() (int64, error) {
	buf, err := d.readN(8)
	if err != nil {
		return 0, err
	}
	return int64(binary.LittleEndian.Uint64(buf)), nil
}

// read decodes the next value. FLAG_REF objects are appended to the
// refs slice so later TYPE_REF tags can resolve them.
//
// CPython: Python/marshal.c r_object
func (d *decoder) read() (any, error) {
	tag, err := d.readByte()
	if err != nil {
		return nil, err
	}

	hasRef := (tag & flagRef) != 0
	tag &^= flagRef

	// Reserve a slot in the ref table before decoding so that
	// self-referential structures work correctly.
	refIdx := -1
	if hasRef {
		refIdx = len(d.refs)
		d.refs = append(d.refs, nil)
	}

	var v any
	v, err = d.decodeTag(tag)
	if err != nil {
		return nil, err
	}
	if hasRef {
		d.refs[refIdx] = v
	}
	return v, nil
}

//nolint:gocyclo // mirrors CPython's per-tag wire dispatch.
func (d *decoder) decodeTag(tag byte) (any, error) {
	switch tag {
	case typeNone, typeNull:
		return nil, nil
	case typeTrue:
		return true, nil
	case typeFalse:
		return false, nil
	case typeEllipsis:
		// Decode to ast.EllipsisType, the same `...` const the compiler
		// emits, so a marshalled-then-loaded code object round-trips
		// identically to a freshly compiled one.
		//
		// CPython: Python/marshal.c r_object TYPE_ELLIPSIS
		return ast.Ellipsis, nil
	case typeInt:
		v, err := d.readInt32()
		return int64(v), err
	case typeInt64:
		return d.readInt64()
	case typeLong:
		bi, err := readLong(d.r)
		if err != nil {
			return nil, err
		}
		if bi.IsInt64() {
			return bi.Int64(), nil
		}
		return bi, nil
	case typeBinaryFloat:
		bits, err := d.readInt64()
		if err != nil {
			return nil, err
		}
		return math.Float64frombits(uint64(bits)), nil
	case typeShortASCII, typeShortASCIIInterned:
		n, err := d.readByte()
		if err != nil {
			return nil, err
		}
		buf, err := d.readN(int(n))
		if err != nil {
			return nil, err
		}
		return string(buf), nil
	case typeASCII, typeASCIIInterned, typeInterned, typeUnicode:
		n, err := d.readInt32()
		if err != nil {
			return nil, err
		}
		buf, err := d.readN(int(n))
		if err != nil {
			return nil, err
		}
		return string(buf), nil
	case typeString:
		n, err := d.readInt32()
		if err != nil {
			return nil, err
		}
		return d.readN(int(n))
	case typeSmallTuple:
		n, err := d.readByte()
		if err != nil {
			return nil, err
		}
		return d.readTuple(int(n))
	case typeTuple:
		n, err := d.readInt32()
		if err != nil {
			return nil, err
		}
		return d.readTuple(int(n))
	case typeCode:
		return unmarshalCode(d)
	case typeSet:
		return d.readSet(false)
	case typeFrozenset:
		return d.readSet(true)
	case typeDict:
		return d.readDict()
	case typeComplex:
		return d.readComplexText()
	case typeBinaryComplex:
		return d.readBinaryComplex()
	case typeRef:
		n, err := d.readInt32()
		if err != nil {
			return nil, err
		}
		if int(n) < 0 || int(n) >= len(d.refs) {
			return nil, fmt.Errorf("marshal: invalid ref index %d (table size %d)", n, len(d.refs))
		}
		return d.refs[n], nil
	}
	return nil, fmt.Errorf("marshal: unknown type tag %q (0x%02x)", rune(tag), tag)
}

func (d *decoder) readTuple(n int) ([]any, error) {
	out := make([]any, n)
	for i := 0; i < n; i++ {
		v, err := d.read()
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

// writeSet encodes a set or frozenset. flag carries FLAG_REF from the
// w_object dispatch.
//
// CPython: Python/marshal.c w_object PySet_Type / PyFrozenSet_Type
func (e *encoder) writeSet(s *objects.Set, flag byte) error {
	tag := byte(typeSet)
	if s.Type() == objects.FrozensetType {
		tag = typeFrozenset
	}
	if err := e.writeByte(tag | flag); err != nil {
		return err
	}
	items := s.Items()
	if err := e.writeInt32(int32(len(items))); err != nil {
		return err
	}
	for _, item := range items {
		v, err := fromObject(item)
		if err != nil {
			return err
		}
		if err := e.write(v); err != nil {
			return err
		}
	}
	return nil
}

// writeDict encodes a map[any]any as TYPE_DICT with null terminator.
// flag carries FLAG_REF from the w_object dispatch.
//
// CPython: Python/marshal.c w_object PyDict_Type
func (e *encoder) writeDict(m map[any]any, flag byte) error {
	if err := e.writeByte(typeDict | flag); err != nil {
		return err
	}
	for k, v := range m {
		if err := e.write(k); err != nil {
			return err
		}
		if err := e.write(v); err != nil {
			return err
		}
	}
	return e.writeByte(typeNull)
}

// writeComplex encodes a complex128 as TYPE_BINARY_COMPLEX. flag
// carries FLAG_REF from the w_object dispatch.
//
// CPython: Python/marshal.c w_object PyComplex_Type (binary form)
func (e *encoder) writeComplex(c complex128, flag byte) error {
	if err := e.writeByte(typeBinaryComplex | flag); err != nil {
		return err
	}
	if err := e.writeInt64(int64(math.Float64bits(real(c)))); err != nil {
		return err
	}
	return e.writeInt64(int64(math.Float64bits(imag(c))))
}

// readSet decodes a TYPE_SET or TYPE_FROZENSET. If frozen is true the
// result is an immutable frozenset.
//
// CPython: Python/marshal.c r_object TYPE_SET / TYPE_FROZENSET
func (d *decoder) readSet(frozen bool) (any, error) {
	n, err := d.readInt32()
	if err != nil {
		return nil, err
	}
	items := make([]objects.Object, 0, int(n))
	for i := int32(0); i < n; i++ {
		v, err := d.read()
		if err != nil {
			return nil, err
		}
		obj, err := toObject(v)
		if err != nil {
			return nil, fmt.Errorf("marshal: set element: %w", err)
		}
		items = append(items, obj)
	}
	if frozen {
		return objects.NewFrozenset(items)
	}
	s := objects.NewSet()
	for _, item := range items {
		if err := s.Add(item); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// toObject converts a plain Go marshal value to an objects.Object for
// use as a set key or dict key. Only hashable types are accepted.
func toObject(v any) (objects.Object, error) {
	switch x := v.(type) {
	case nil:
		return objects.None(), nil
	case bool:
		return objects.NewBool(x), nil
	case int64:
		return objects.NewInt(x), nil
	case float64:
		return objects.NewFloat(x), nil
	case string:
		return objects.NewStr(x), nil
	case ast.EllipsisType:
		return objects.Ellipsis(), nil
	case objects.Object:
		return x, nil
	}
	return nil, fmt.Errorf("marshal: cannot convert %T to Object", v)
}

// isEllipsisValue reports whether v is the marshalable Ellipsis const,
// in either of the two spellings gopy uses: the compiler emits the
// ast.EllipsisType node for a `...` literal, while a code object built
// at runtime (e.g. via code.replace) may carry the runtime ellipsis
// singleton instead. Both serialize to TYPE_ELLIPSIS.
//
// CPython: Python/marshal.c:476 w_object (v == Py_Ellipsis)
func isEllipsisValue(v any) bool {
	if _, ok := v.(ast.EllipsisType); ok {
		return true
	}
	if obj, ok := v.(objects.Object); ok {
		return obj == objects.Ellipsis()
	}
	return false
}

// fromObject converts an objects.Object back to a plain Go marshal
// value so that set and frozenset items can pass through write().
func fromObject(obj objects.Object) (any, error) {
	switch x := obj.(type) {
	case *objects.Int:
		if v, ok := x.Int64(); ok {
			return v, nil
		}
		return x.BigInt(), nil
	case *objects.Float:
		return x.Float64(), nil
	case *objects.Bool:
		v, _ := x.Int64()
		return v != 0, nil
	case *objects.Set:
		return x, nil
	case *objects.Code:
		return x, nil
	case *objects.Complex:
		return x.Complex128(), nil
	case *objects.Bytes:
		return x.Bytes(), nil
	case *objects.Tuple:
		out := make([]any, x.Len())
		for i := 0; i < x.Len(); i++ {
			n, err := fromObject(x.Item(i))
			if err != nil {
				return nil, err
			}
			out[i] = n
		}
		return out, nil
	}
	// None and str use unexported concrete types; dispatch via type slots.
	if obj.Type() == objects.NoneType() {
		return nil, nil
	}
	if obj.Type().Str != nil {
		s, err := obj.Type().Str(obj)
		if err == nil {
			return s, nil
		}
	}
	return nil, fmt.Errorf("marshal: cannot convert %T to plain value", obj)
}

// readDict decodes a TYPE_DICT (null-terminated key/value pairs).
//
// CPython: Python/marshal.c r_object TYPE_DICT
func (d *decoder) readDict() (any, error) {
	m := make(map[any]any)
	for {
		k, err := d.read()
		if err != nil {
			return nil, err
		}
		if k == nil {
			break
		}
		v, err := d.read()
		if err != nil {
			return nil, err
		}
		m[k] = v
	}
	return m, nil
}

// readComplexText decodes a TYPE_COMPLEX (text-encoded float pair).
// Each component is a length-prefixed ASCII float string.
//
// CPython: Python/marshal.c r_object TYPE_COMPLEX
func (d *decoder) readComplexText() (any, error) {
	rn, err := d.readByte()
	if err != nil {
		return nil, err
	}
	rbuf, err := d.readN(int(rn))
	if err != nil {
		return nil, err
	}
	var r float64
	if _, err := fmt.Sscanf(string(rbuf), "%g", &r); err != nil {
		return nil, fmt.Errorf("marshal: complex real part %q: %w", rbuf, err)
	}
	in, err := d.readByte()
	if err != nil {
		return nil, err
	}
	ibuf, err := d.readN(int(in))
	if err != nil {
		return nil, err
	}
	var im float64
	if _, err := fmt.Sscanf(string(ibuf), "%g", &im); err != nil {
		return nil, fmt.Errorf("marshal: complex imag part %q: %w", ibuf, err)
	}
	return complex(r, im), nil
}

// readBinaryComplex decodes a TYPE_BINARY_COMPLEX (two IEEE 754 doubles).
//
// CPython: Python/marshal.c r_object TYPE_BINARY_COMPLEX
func (d *decoder) readBinaryComplex() (any, error) {
	rb, err := d.readInt64()
	if err != nil {
		return nil, err
	}
	ib, err := d.readInt64()
	if err != nil {
		return nil, err
	}
	return complex(math.Float64frombits(uint64(rb)), math.Float64frombits(uint64(ib))), nil
}
