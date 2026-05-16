// LOAD_GLOBAL family specializer.
//
// _Py_Specialize_LoadGlobal looks at the globals + builtins dicts on
// every frame and, if the name resolves cleanly out of one of them,
// rewrites the in-stream LOAD_GLOBAL into either LOAD_GLOBAL_MODULE
// (key found in globals) or LOAD_GLOBAL_BUILTIN (key found only in
// builtins). The cache stamps the matching keys version(s) so the
// dispatch arm can validate the table hasn't been resized or
// re-keyed before reading the cached slot index.
//
// CPython: Python/specialize.c:1683 specialize_load_global_lock_held
// CPython: Python/specialize.c:1775 _Py_Specialize_LoadGlobal

package specialize

// DEPRECATED (spec 1714): Spec 1714 phases 3+4: raw cache writes migrate to typed accessors; family/deopt literals move to specialize/family_gen.go. File shrinks to specialize-policy.
// See website/docs/specs/1700/1714_bytecodes_dsl_codegen.md.

import (
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

// LoadGlobal rewrites the LOAD_GLOBAL at instr to either
// LOAD_GLOBAL_MODULE or LOAD_GLOBAL_BUILTIN if name resolves
// cleanly in globals or builtins. On any miss the opcode falls
// back to its adaptive parent and the counter is rolled back to
// the next backoff.
//
// CPython: Python/specialize.c:1775 _Py_Specialize_LoadGlobal
func LoadGlobal(globals, builtins objects.Object, code []byte, instr int, name *objects.Unicode) {
	if specializeLoadGlobalLockHeld(globals, builtins, code, instr, name) {
		return
	}
	Unspecialize(code, instr)
}

// specializeLoadGlobalLockHeld returns true when it successfully
// rewrites the opcode plus stamps the cache. False signals "fall
// back to unspecialize" (CPython does this via a `goto fail` path).
//
// CPython: Python/specialize.c:1683 specialize_load_global_lock_held
func specializeLoadGlobalLockHeld(globals, builtins objects.Object, code []byte, instr int, name *objects.Unicode) bool {
	if !objects.IsExactDict(globals) {
		return false
	}
	gd := globals.(*objects.Dict)
	if !gd.IsKeysUnicode() {
		return false
	}
	idx, ok := gd.LookupString(name)
	if !ok {
		return false
	}
	if idx >= 0 {
		// Found in globals: LOAD_GLOBAL_MODULE.
		if idx > 0xFFFF {
			return false
		}
		gv := gd.GetKeysVersion()
		if gv == 0 || gv > 0xFFFF {
			return false
		}
		writeLoadGlobalCache(code, instr, uint16(idx), uint16(gv), 0)
		Specialize(code, instr, compile.LOAD_GLOBAL_MODULE)
		return true
	}
	// Not in globals: try builtins.
	if !objects.IsExactDict(builtins) {
		return false
	}
	bd := builtins.(*objects.Dict)
	if !bd.IsKeysUnicode() {
		return false
	}
	bidx, ok := bd.LookupString(name)
	if !ok || bidx == objects.DictKeyAbsent {
		return false
	}
	if bidx > 0xFFFF {
		return false
	}
	gv := gd.GetKeysVersion()
	if gv == 0 || gv > 0xFFFF {
		return false
	}
	bv := bd.GetKeysVersion()
	if bv == 0 || bv > 0xFFFF {
		return false
	}
	writeLoadGlobalCache(code, instr, uint16(bidx), uint16(gv), uint16(bv))
	Specialize(code, instr, compile.LOAD_GLOBAL_BUILTIN)
	return true
}

// writeLoadGlobalCache stamps the LOAD_GLOBAL inline cache.
//
// Layout per pycore_code.h:_PyLoadGlobalCache:
//
//	cell 1: counter            (written by Specialize, skipped here)
//	cell 2: module_keys_version
//	cell 3: builtin_keys_version
//	cell 4: index
//
// CPython: Python/specialize.c:1722-1768 cache->index / module_keys_version
// CPython: Include/internal/pycore_code.h:67 _PyLoadGlobalCache
func writeLoadGlobalCache(code []byte, instr int, idx, moduleKeys, builtinKeys uint16) {
	c := loadGlobalCacheAt(code, instr)
	c.setModuleKeysVersion(moduleKeys)
	c.setBuiltinKeysVersion(builtinKeys)
	c.setIndex(idx)
}

// loadGlobalCacheView mirrors _PyLoadGlobalCache. Field accessors
// match CPython's `cache->field` spellings. This is the typed shape
// that spec 1714 phase 3.5 will replace with the generated wrapper
// in specialize/cache_layouts_gen.go once the byte/codeunit slice
// boundary is unified.
//
// CPython: Include/internal/pycore_code.h:67 _PyLoadGlobalCache
type loadGlobalCacheView struct {
	code  []byte
	instr int
}

func loadGlobalCacheAt(code []byte, instr int) loadGlobalCacheView {
	return loadGlobalCacheView{code, instr}
}

func (c loadGlobalCacheView) moduleKeysVersion() uint16    { return readCell(c.code, c.instr, 2) }
func (c loadGlobalCacheView) setModuleKeysVersion(v uint16) { writeCell(c.code, c.instr, 2, v) }
func (c loadGlobalCacheView) builtinKeysVersion() uint16    { return readCell(c.code, c.instr, 3) }
func (c loadGlobalCacheView) setBuiltinKeysVersion(v uint16) { writeCell(c.code, c.instr, 3, v) }
func (c loadGlobalCacheView) index() uint16                  { return readCell(c.code, c.instr, 4) }
func (c loadGlobalCacheView) setIndex(v uint16)              { writeCell(c.code, c.instr, 4, v) }

// LoadGlobalIndex exposes the cached slot index from the dispatch
// loop without leaking cell numbers. Mirror of `cache->index`.
//
// CPython: Python/bytecodes.c _LOAD_GLOBAL_MODULE index operand
func LoadGlobalIndex(code []byte, instr int) uint16 {
	return loadGlobalCacheAt(code, instr).index()
}

// LoadGlobalModuleKeysVersion mirrors `cache->module_keys_version`.
func LoadGlobalModuleKeysVersion(code []byte, instr int) uint16 {
	return loadGlobalCacheAt(code, instr).moduleKeysVersion()
}

// LoadGlobalBuiltinKeysVersion mirrors `cache->builtin_keys_version`.
func LoadGlobalBuiltinKeysVersion(code []byte, instr int) uint16 {
	return loadGlobalCacheAt(code, instr).builtinKeysVersion()
}
