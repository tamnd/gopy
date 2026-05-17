// Fast-path arms for the LOAD_GLOBAL family.
//
// LOAD_GLOBAL packs a "push NULL" flag into bit 0 of oparg, with the
// real name index in bits 1+. The fast-path arms ignore the name
// index entirely: the specializer has already stamped a slot index
// in the inline cache, so the arm reads straight out of the dict's
// entry table without walking the names array.
//
// Cache layout per pycore_code.h:_PyLoadGlobalCache:
//   cell 1: counter (managed by Specialize/Unspecialize)
//   cell 2: module_keys_version (globals)
//   cell 3: builtin_keys_version (BUILTIN variant only)
//   cell 4: index (slot in the matching dict)
//
// CPython: Python/bytecodes.c LOAD_GLOBAL_MODULE, LOAD_GLOBAL_BUILTIN

package vm

// DEPRECATED (spec 1714): Spec 1714 phase 6: LOAD_GLOBAL_* arms migrate to typed op<NAME> bodies; cache decode/deopt/advance live in the generated harness.
// See website/docs/specs/1700/1714_bytecodes_dsl_codegen.md.

import (
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/specialize"
	"github.com/tamnd/gopy/stackref"
)

// pushGlobalResult finalizes the stack shape for a LOAD_GLOBAL fast
// path hit: the loaded value first, then an optional NULL slot on top.
// CPython: Python/bytecodes.c LOAD_GLOBAL output order is [null, v]
// (v on TOS, null below) so callable lands at the bottom of the call
// frame, matching the [callable, NULL_or_self, args...] layout CALL
// expects.
func (e *evalState) pushGlobalResult(value objects.Object, oparg uint32) {
	e.pushObject(value)
	if oparg&1 != 0 {
		e.push(stackref.Null)
	}
}

// fastLoadGlobalModule implements LOAD_GLOBAL_MODULE.
//
// Guards: globals is exactly a dict, module_keys_version matches,
// slot index still resolves to a live entry.
//
// CPython: Python/bytecodes.c:1797 op(_LOAD_GLOBAL_MODULE)
func (e *evalState) fastLoadGlobalModule(oparg uint32) (int, bool) {
	if !objects.IsExactDict(e.f.Globals) {
		return 0, false
	}
	gd := e.f.Globals.(*objects.Dict)
	idx := e.instrIdx()
	code := e.f.Code.Code
	cachedVer := uint32(specialize.LoadGlobalModuleKeysVersion(code, idx))
	curVer := gd.GetKeysVersion()
	if curVer == 0 || curVer != cachedVer {
		return 0, false
	}
	slot := int(specialize.LoadGlobalIndex(code, idx))
	_, value, found := gd.EntryAt(slot)
	if !found || value == nil {
		return 0, false
	}
	e.pushGlobalResult(value, oparg)
	return e.cacheAdvance(compile.LOAD_GLOBAL), true
}

// fastLoadGlobalBuiltin implements LOAD_GLOBAL_BUILTIN.
//
// Guards: globals + builtins are exactly dicts, each keys_version
// matches the cached value, slot index resolves in the builtins
// dict. The globals version is still checked: a write to globals
// could shadow the builtin lookup, which must invalidate the
// cache.
//
// CPython: Python/bytecodes.c:1817 op(_LOAD_GLOBAL_BUILTINS)
func (e *evalState) fastLoadGlobalBuiltin(oparg uint32) (int, bool) {
	if !objects.IsExactDict(e.f.Globals) || !objects.IsExactDict(e.f.Builtins) {
		return 0, false
	}
	gd := e.f.Globals.(*objects.Dict)
	bd := e.f.Builtins.(*objects.Dict)
	idx := e.instrIdx()
	code := e.f.Code.Code
	cachedGV := uint32(specialize.LoadGlobalModuleKeysVersion(code, idx))
	if curGV := gd.GetKeysVersion(); curGV == 0 || curGV != cachedGV {
		return 0, false
	}
	cachedBV := uint32(specialize.LoadGlobalBuiltinKeysVersion(code, idx))
	if curBV := bd.GetKeysVersion(); curBV == 0 || curBV != cachedBV {
		return 0, false
	}
	slot := int(specialize.LoadGlobalIndex(code, idx))
	_, value, found := bd.EntryAt(slot)
	if !found || value == nil {
		return 0, false
	}
	e.pushGlobalResult(value, oparg)
	return e.cacheAdvance(compile.LOAD_GLOBAL), true
}
