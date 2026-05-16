// Typed inline-cache views. Each view wraps the codeunit slice +
// instruction offset that addresses a specific opcode's cache and
// exposes one accessor per CPython struct field.
//
// These mirror the structs in Include/internal/pycore_code.h byte
// for byte. They replace the deprecated kth-cell shims
// (CacheCell/SetCacheCell/CacheU32/SetCacheU32) at the call sites
// where the original C code says `cache->field`. Spec 1714 phase 3.5
// retires the byte-slice plumbing and rebases these on the
// generated specialize/cache_layouts_gen.go wrappers.
//
// CPython: Include/internal/pycore_code.h:67-160 inline cache structs

package specialize

// ---- _PyAttrCache: STORE_ATTR, LOAD_ATTR_MODULE -----------------
//
// CPython: Include/internal/pycore_code.h:102 _PyAttrCache
//
//	uint16_t version[2];   // cells 2..3 (uint32 LE)
//	uint16_t index;        // cell 4

type attrCacheView struct {
	code  []byte
	instr int
}

func attrCacheAt(code []byte, instr int) attrCacheView { return attrCacheView{code, instr} }

func (c attrCacheView) version() uint32         { return readU32(c.code, c.instr, 2) }
func (c attrCacheView) setVersion(v uint32)      { writeU32(c.code, c.instr, 2, v) }
func (c attrCacheView) index() uint16            { return readCell(c.code, c.instr, 4) }
func (c attrCacheView) setIndex(v uint16)        { writeCell(c.code, c.instr, 4, v) }

// AttrCacheVersion / AttrCacheIndex are the public dispatch-loop
// accessors. Both STORE_ATTR and LOAD_ATTR_MODULE share this layout.
//
// CPython: Python/bytecodes.c STORE_ATTR_* / LOAD_ATTR_MODULE
func AttrCacheVersion(code []byte, instr int) uint32 { return attrCacheAt(code, instr).version() }
func AttrCacheIndex(code []byte, instr int) uint16   { return attrCacheAt(code, instr).index() }

// SetAttrCacheVersion / SetAttrCacheIndex stamp AttrCache fields.
// Production paths go through the specializer; these exist so test
// fixtures can hand-stamp the inline cache.
func SetAttrCacheVersion(code []byte, instr int, v uint32) { attrCacheAt(code, instr).setVersion(v) }
func SetAttrCacheIndex(code []byte, instr int, v uint16)   { attrCacheAt(code, instr).setIndex(v) }

// ---- _PyLoadMethodCache: LOAD_ATTR (all arms) -------------------
//
// CPython: Include/internal/pycore_code.h:108 _PyLoadMethodCache
//
//	uint16_t type_version[2];     // cells 2..3 (uint32 LE)
//	union {
//	    uint16_t keys_version[2]; // cells 4..5 (uint32 LE)
//	    uint16_t dict_offset;     // cell 4 alone
//	};
//	uint16_t descr[4];            // cells 6..9 (pointer slab in gopy)
//
// In gopy the descr pointer lives in the parallel CacheObjects slab
// (see SetCacheObject / CacheObject). The cells 6..9 are unused by
// gopy's runtime; CPython packs the same pointer there via write_obj.
//
// The metaclass-check arm reuses cells 4..5 to stamp the metaclass
// version. We expose it under a separate name (metaVersion) at the
// same offset so call sites read like CPython.

type loadMethodCacheView struct {
	code  []byte
	instr int
}

func loadMethodCacheAt(code []byte, instr int) loadMethodCacheView {
	return loadMethodCacheView{code, instr}
}

func (c loadMethodCacheView) typeVersion() uint32     { return readU32(c.code, c.instr, 2) }
func (c loadMethodCacheView) setTypeVersion(v uint32) { writeU32(c.code, c.instr, 2, v) }
func (c loadMethodCacheView) keysVersion() uint32     { return readU32(c.code, c.instr, 4) }
func (c loadMethodCacheView) setKeysVersion(v uint32) { writeU32(c.code, c.instr, 4, v) }
func (c loadMethodCacheView) metaVersion() uint32     { return readU32(c.code, c.instr, 4) }
func (c loadMethodCacheView) setMetaVersion(v uint32) { writeU32(c.code, c.instr, 4, v) }
func (c loadMethodCacheView) dictOffset() uint16      { return readCell(c.code, c.instr, 4) }
func (c loadMethodCacheView) setDictOffset(v uint16)  { writeCell(c.code, c.instr, 4, v) }

// Public read helpers for the LOAD_ATTR dispatch arms.
//
// CPython: Python/bytecodes.c LOAD_ATTR_*
func LoadMethodTypeVersion(code []byte, instr int) uint32 {
	return loadMethodCacheAt(code, instr).typeVersion()
}
func LoadMethodKeysVersion(code []byte, instr int) uint32 {
	return loadMethodCacheAt(code, instr).keysVersion()
}
func LoadMethodMetaVersion(code []byte, instr int) uint32 {
	return loadMethodCacheAt(code, instr).metaVersion()
}

// Test/setup helpers for the LoadMethodCache fields. See the note
// on SetAttrCacheVersion.
func SetLoadMethodTypeVersion(code []byte, instr int, v uint32) {
	loadMethodCacheAt(code, instr).setTypeVersion(v)
}
func SetLoadMethodKeysVersion(code []byte, instr int, v uint32) {
	loadMethodCacheAt(code, instr).setKeysVersion(v)
}
func SetLoadMethodMetaVersion(code []byte, instr int, v uint32) {
	loadMethodCacheAt(code, instr).setMetaVersion(v)
}
func SetLoadAttrInstanceValueSlot(code []byte, instr int, v uint16) {
	writeCell(code, instr, 6, v)
}

// LoadAttrInstanceValueSlot reads the slot index for the
// LOAD_ATTR_INSTANCE_VALUE arm. The slot lives one cell past the
// keys_version union, at cell 6, in the descr quadword.
//
// CPython: Python/specialize.c specialize_dict_access INSTANCE_VALUE
func LoadAttrInstanceValueSlot(code []byte, instr int) uint16 {
	return readCell(code, instr, 6)
}

// ---- _PyCallCache: CALL, CALL_KW --------------------------------
//
// CPython: Include/internal/pycore_code.h:124 _PyCallCache
//
//	uint16_t func_version[2];  // cells 2..3 (uint32 LE)

type callCacheView struct {
	code  []byte
	instr int
}

func callCacheAt(code []byte, instr int) callCacheView { return callCacheView{code, instr} }

func (c callCacheView) funcVersion() uint32     { return readU32(c.code, c.instr, 2) }
func (c callCacheView) setFuncVersion(v uint32) { writeU32(c.code, c.instr, 2, v) }

func CallFuncVersion(code []byte, instr int) uint32 { return callCacheAt(code, instr).funcVersion() }
func SetCallFuncVersion(code []byte, instr int, v uint32) {
	callCacheAt(code, instr).setFuncVersion(v)
}

// ---- _PyToBoolCache: TO_BOOL ------------------------------------
//
// CPython: Include/internal/pycore_code.h:150 _PyToBoolCache
//
//	uint16_t version[2];  // cells 2..3 (uint32 LE)

type toBoolCacheView struct {
	code  []byte
	instr int
}

func toBoolCacheAt(code []byte, instr int) toBoolCacheView { return toBoolCacheView{code, instr} }

func (c toBoolCacheView) version() uint32     { return readU32(c.code, c.instr, 2) }
func (c toBoolCacheView) setVersion(v uint32) { writeU32(c.code, c.instr, 2, v) }

func ToBoolVersion(code []byte, instr int) uint32 { return toBoolCacheAt(code, instr).version() }
func SetToBoolVersion(code []byte, instr int, v uint32) {
	toBoolCacheAt(code, instr).setVersion(v)
}

// ---- shared u32 split helpers -----------------------------------
//
// Cell k holds the low 16 bits, cell k+1 the high 16 bits, matching
// CPython's write_u32 / read_u32 on little-endian targets.
//
// CPython: Include/internal/pycore_code.h:339 write_u32
// CPython: Include/internal/pycore_code.h:362 read_u32

func readU32(code []byte, instr, k int) uint32 {
	return uint32(readCell(code, instr, k)) | uint32(readCell(code, instr, k+1))<<16
}

func writeU32(code []byte, instr, k int, value uint32) {
	writeCell(code, instr, k, uint16(value))
	writeCell(code, instr, k+1, uint16(value>>16))
}
