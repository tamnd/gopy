// Inline cache layouts. Each adaptive opcode reserves a fixed
// number of trailing _Py_CODEUNIT (uint16) entries in the bytecode
// stream as its inline cache. CPython types those entries by
// casting the cache pointer to one of the structs in
// pycore_code.h. The Go layout has to match the C layout
// byte-for-byte (every field is uint16 to make padding moot) so a
// cache pointer can be cast either way at the dispatch boundary.
//
// CPython: Include/internal/pycore_code.h:67-161

package specialize

import "unsafe"

// LoadGlobalCache backs LOAD_GLOBAL. CACHE_ENTRIES = 4.
//
// CPython: Include/internal/pycore_code.h:67 _PyLoadGlobalCache
type LoadGlobalCache struct {
	Counter            BackoffCounter
	ModuleKeysVersion  uint16
	BuiltinKeysVersion uint16
	Index              uint16
}

// BinaryOpCache backs BINARY_OP. CACHE_ENTRIES = 5.
//
// CPython: Include/internal/pycore_code.h:76 _PyBinaryOpCache
type BinaryOpCache struct {
	Counter       BackoffCounter
	ExternalCache [4]uint16
}

// UnpackSequenceCache backs UNPACK_SEQUENCE. CACHE_ENTRIES = 1.
//
// CPython: Include/internal/pycore_code.h:83 _PyUnpackSequenceCache
type UnpackSequenceCache struct {
	Counter BackoffCounter
}

// CompareOpCache backs COMPARE_OP. CACHE_ENTRIES = 1.
//
// CPython: Include/internal/pycore_code.h:90 _PyCompareOpCache
type CompareOpCache struct {
	Counter BackoffCounter
}

// SuperAttrCache backs LOAD_SUPER_ATTR. CACHE_ENTRIES = 1.
//
// CPython: Include/internal/pycore_code.h:96 _PySuperAttrCache
type SuperAttrCache struct {
	Counter BackoffCounter
}

// AttrCache backs STORE_ATTR. CACHE_ENTRIES = 4.
//
// CPython: Include/internal/pycore_code.h:102 _PyAttrCache
type AttrCache struct {
	Counter BackoffCounter
	Version [2]uint16
	Index   uint16
}

// LoadMethodCache backs LOAD_ATTR. CACHE_ENTRIES = 10. The widest
// adaptive cache; LOAD_ATTR specializes into both attribute lookups
// and unbound-method dispatch, hence the type/keys versions plus
// the four-slot descr field.
//
// The C layout uses a union for keys_version / dict_offset; in Go
// we keep two uint16 fields because the in-memory width is
// identical and the union arm is selected at the call site.
//
// CPython: Include/internal/pycore_code.h:108 _PyLoadMethodCache
type LoadMethodCache struct {
	Counter     BackoffCounter
	TypeVersion [2]uint16
	Keys        [2]uint16
	Descr       [4]uint16
}

// CallCache backs CALL and CALL_KW. CACHE_ENTRIES = 3.
//
// CPython: Include/internal/pycore_code.h:124 _PyCallCache
type CallCache struct {
	Counter     BackoffCounter
	FuncVersion [2]uint16
}

// StoreSubscrCache backs STORE_SUBSCR. CACHE_ENTRIES = 1.
//
// CPython: Include/internal/pycore_code.h:132 _PyStoreSubscrCache
type StoreSubscrCache struct {
	Counter BackoffCounter
}

// ForIterCache backs FOR_ITER. CACHE_ENTRIES = 1.
//
// CPython: Include/internal/pycore_code.h:138 _PyForIterCache
type ForIterCache struct {
	Counter BackoffCounter
}

// SendCache backs SEND. CACHE_ENTRIES = 1.
//
// CPython: Include/internal/pycore_code.h:144 _PySendCache
type SendCache struct {
	Counter BackoffCounter
}

// ToBoolCache backs TO_BOOL. CACHE_ENTRIES = 3.
//
// CPython: Include/internal/pycore_code.h:150 _PyToBoolCache
type ToBoolCache struct {
	Counter BackoffCounter
	Version [2]uint16
}

// ContainsOpCache backs CONTAINS_OP. CACHE_ENTRIES = 1.
//
// CPython: Include/internal/pycore_code.h:157 _PyContainsOpCache
type ContainsOpCache struct {
	Counter BackoffCounter
}

// CodeUnitWidth is the width in bytes of one bytecode codeunit.
// All cache structs are sized as a whole number of codeunits.
const CodeUnitWidth = 2

// Inline cache widths in codeunits (CACHE_ENTRIES_<FAMILY>). These
// numbers are pinned by cache_test.go against unsafe.Sizeof so the
// Go structs can never silently drift from the C layout.
//
// CPython: Include/internal/pycore_code.h INLINE_CACHE_ENTRIES_*
const (
	InlineCacheEntriesLoadGlobal     = int(unsafe.Sizeof(LoadGlobalCache{})) / CodeUnitWidth
	InlineCacheEntriesBinaryOp       = int(unsafe.Sizeof(BinaryOpCache{})) / CodeUnitWidth
	InlineCacheEntriesUnpackSequence = int(unsafe.Sizeof(UnpackSequenceCache{})) / CodeUnitWidth
	InlineCacheEntriesCompareOp      = int(unsafe.Sizeof(CompareOpCache{})) / CodeUnitWidth
	InlineCacheEntriesLoadSuperAttr  = int(unsafe.Sizeof(SuperAttrCache{})) / CodeUnitWidth
	InlineCacheEntriesLoadAttr       = int(unsafe.Sizeof(LoadMethodCache{})) / CodeUnitWidth
	InlineCacheEntriesStoreAttr      = int(unsafe.Sizeof(AttrCache{})) / CodeUnitWidth
	InlineCacheEntriesCall           = int(unsafe.Sizeof(CallCache{})) / CodeUnitWidth
	InlineCacheEntriesCallKw         = int(unsafe.Sizeof(CallCache{})) / CodeUnitWidth
	InlineCacheEntriesStoreSubscr    = int(unsafe.Sizeof(StoreSubscrCache{})) / CodeUnitWidth
	InlineCacheEntriesForIter        = int(unsafe.Sizeof(ForIterCache{})) / CodeUnitWidth
	InlineCacheEntriesSend           = int(unsafe.Sizeof(SendCache{})) / CodeUnitWidth
	InlineCacheEntriesToBool         = int(unsafe.Sizeof(ToBoolCache{})) / CodeUnitWidth
	InlineCacheEntriesContainsOp     = int(unsafe.Sizeof(ContainsOpCache{})) / CodeUnitWidth
)
