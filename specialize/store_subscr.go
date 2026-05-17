// STORE_SUBSCR family specializer.
//
// _Py_Specialize_StoreSubscr narrows STORE_SUBSCR to the in-range
// list[int] arm and the dict arm. Slice indexing has no specialized
// variant; the list[slice] case bails to the generic op.
//
// CPython: Python/specialize.c:1894 _Py_Specialize_StoreSubscr

package specialize

// DEPRECATED (spec 1714): Spec 1714 phases 3+4: raw cache writes migrate to typed accessors; family/deopt literals move to specialize/family_gen.go. File shrinks to specialize-policy.
// See website/docs/specs/1700/1714_bytecodes_dsl_codegen.md.

import (
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

// StoreSubscr specializes the STORE_SUBSCR at instr based on the
// container and subscript operands. Order matches CPython: list
// first (with bounds check on int subscript), then dict.
//
// CPython: Python/specialize.c:1894 _Py_Specialize_StoreSubscr
func StoreSubscr(container, sub objects.Object, code []byte, instr int) {
	if objects.IsExactList(container) {
		if objects.IsExactInt(sub) {
			if i, ok := sub.(*objects.Int); ok {
				if v, ok := i.Int64(); ok && v >= 0 {
					l := container.(*objects.List)
					if v < int64(l.Len()) {
						Specialize(code, instr, compile.STORE_SUBSCR_LIST_INT)
						return
					}
				}
			}
		}
		Unspecialize(code, instr)
		return
	}
	if objects.IsExactDict(container) {
		Specialize(code, instr, compile.STORE_SUBSCR_DICT)
		return
	}
	Unspecialize(code, instr)
}
