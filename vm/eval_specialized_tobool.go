// Fast-path arms for the TO_BOOL family.
//
// Cache layout (3 codeunits, _PyToBoolCache):
//   cell 1   counter
//   cells 2-3 type_version (TO_BOOL_ALWAYS_TRUE only)
//
// Each variant validates the operand type, replaces TOS with the
// constant truth value the type guarantees, and advances past the
// LOAD_ATTR/TO_BOOL inline cache.
//
// CPython: Python/bytecodes.c TO_BOOL_<variant>

package vm

import (
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/specialize"
)

// fastToBoolBool: TOS is already a *Bool, leave it.
//
// CPython: Python/bytecodes.c TO_BOOL_BOOL
func (e *evalState) fastToBoolBool() (int, bool) {
	if !objects.IsExactBool(e.peek(0).AsObject()) {
		return 0, false
	}
	return e.cacheAdvance(compile.TO_BOOL), true
}

// fastToBoolInt: 0 → False, anything else → True.
//
// CPython: Python/bytecodes.c TO_BOOL_INT
func (e *evalState) fastToBoolInt() (int, bool) {
	i, ok := e.peek(0).AsObject().(*objects.Int)
	if !ok || !objects.IsExactInt(i) {
		return 0, false
	}
	e.pop()
	if i.Sign() == 0 {
		e.pushObject(objects.False())
	} else {
		e.pushObject(objects.True())
	}
	return e.cacheAdvance(compile.TO_BOOL), true
}

// fastToBoolList: empty list → False, else → True.
//
// CPython: Python/bytecodes.c TO_BOOL_LIST
func (e *evalState) fastToBoolList() (int, bool) {
	l, ok := e.peek(0).AsObject().(*objects.List)
	if !ok || !objects.IsExactList(l) {
		return 0, false
	}
	e.pop()
	if l.Len() == 0 {
		e.pushObject(objects.False())
	} else {
		e.pushObject(objects.True())
	}
	return e.cacheAdvance(compile.TO_BOOL), true
}

// fastToBoolNone: None → False.
//
// CPython: Python/bytecodes.c TO_BOOL_NONE
func (e *evalState) fastToBoolNone() (int, bool) {
	if !objects.IsNone(e.peek(0).AsObject()) {
		return 0, false
	}
	e.pop()
	e.pushObject(objects.False())
	return e.cacheAdvance(compile.TO_BOOL), true
}

// fastToBoolStr: empty str → False, else → True.
//
// CPython: Python/bytecodes.c TO_BOOL_STR
func (e *evalState) fastToBoolStr() (int, bool) {
	s, ok := e.peek(0).AsObject().(*objects.Unicode)
	if !ok || !objects.IsExactStr(s) {
		return 0, false
	}
	e.pop()
	if s.Value() == "" {
		e.pushObject(objects.False())
	} else {
		e.pushObject(objects.True())
	}
	return e.cacheAdvance(compile.TO_BOOL), true
}

// fastToBoolAlwaysTrue: user-defined class whose type pinned at
// specialize time. Validates type_version and replaces with True.
//
// CPython: Python/bytecodes.c TO_BOOL_ALWAYS_TRUE
func (e *evalState) fastToBoolAlwaysTrue() (int, bool) {
	v := e.peek(0).AsObject()
	tp := objects.ExactType(v)
	if tp == nil {
		return 0, false
	}
	idx := e.instrIdx()
	cachedVer := specialize.CacheU32(e.f.Code.Code, idx, 2)
	if curVer := tp.VersionTag(); curVer == 0 || curVer != cachedVer {
		return 0, false
	}
	e.pop()
	e.pushObject(objects.True())
	return e.cacheAdvance(compile.TO_BOOL), true
}
