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

// writeLoadGlobalCache stamps the LOAD_GLOBAL inline cache. The
// cache layout is { counter, module_keys_version, builtin_keys_version,
// index } per pycore_code.h:_PyLoadGlobalCache; counter is rewritten
// by Specialize, so we only fill the trailing three cells here.
func writeLoadGlobalCache(code []byte, instr int, idx, moduleKeys, builtinKeys uint16) {
	SetCacheCell(code, instr, 2, moduleKeys)
	SetCacheCell(code, instr, 3, builtinKeys)
	SetCacheCell(code, instr, 4, idx)
}
