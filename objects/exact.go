// Exact-type predicates the adaptive specializer reads.
//
// PEP 659 specializers narrow on exact-type checks rather than
// MRO-aware checks: BINARY_OP_ADD_INT only fires when both operands
// are exactly int, never an int subclass, because subclasses can
// override __add__ and that path is what the specializer is
// stepping around. Mirrors the CPython Py*_CheckExact macros.

package objects

// IsExactInt reports whether o is exactly *Int.
//
// CPython: Include/cpython/longintrepr.h PyLong_CheckExact
func IsExactInt(o Object) bool {
	return Type_(o) == IntType
}

// IsExactFloat reports whether o is exactly *Float.
//
// CPython: Include/cpython/floatobject.h PyFloat_CheckExact
func IsExactFloat(o Object) bool {
	return Type_(o) == FloatType
}

// IsExactStr reports whether o is exactly *Unicode.
//
// CPython: Include/cpython/unicodeobject.h PyUnicode_CheckExact
func IsExactStr(o Object) bool {
	return Type_(o) == strType
}

// IsExactList reports whether o is exactly *List.
//
// CPython: Include/cpython/listobject.h PyList_CheckExact
func IsExactList(o Object) bool {
	return Type_(o) == ListType
}

// IsExactTuple reports whether o is exactly *Tuple.
//
// CPython: Include/cpython/tupleobject.h PyTuple_CheckExact
func IsExactTuple(o Object) bool {
	return Type_(o) == TupleType
}

// IsExactBool reports whether o is exactly *Bool.
//
// CPython: Include/cpython/boolobject.h PyBool_Check
func IsExactBool(o Object) bool {
	return Type_(o) == BoolType
}

// IsExactSlice reports whether o is exactly *Slice.
//
// CPython: Include/cpython/sliceobject.h PySlice_Check
func IsExactSlice(o Object) bool {
	return Type_(o) == SliceType
}

// IsExactSet reports whether o is exactly *Set.
//
// CPython: Include/cpython/setobject.h PySet_CheckExact
func IsExactSet(o Object) bool {
	return Type_(o) == SetType
}

// IsExactFrozenSet reports whether o is exactly *Set with the
// frozenset type. gopy stores both kinds in the same struct and
// distinguishes them by their type pointer.
//
// CPython: Include/cpython/setobject.h PyFrozenSet_CheckExact
func IsExactFrozenSet(o Object) bool {
	return Type_(o) == FrozensetType
}

// IsListIter reports whether o is exactly a list_iterator. The
// FOR_ITER specializer reads this to pick FOR_ITER_LIST.
//
// CPython: Objects/listobject.c PyListIter_Type
func IsListIter(o Object) bool {
	return Type_(o) == listIterType
}

// IsTupleIter reports whether o is exactly a tuple_iterator. The
// FOR_ITER specializer reads this to pick FOR_ITER_TUPLE.
//
// CPython: Objects/tupleobject.c PyTupleIter_Type
func IsTupleIter(o Object) bool {
	return Type_(o) == tupleIterType
}

// IsRangeIter reports whether o is exactly a range_iterator. The
// FOR_ITER specializer reads this to pick FOR_ITER_RANGE.
//
// CPython: Objects/rangeobject.c PyRangeIter_Type
func IsRangeIter(o Object) bool {
	return Type_(o) == rangeIterType
}

// IsGenerator reports whether o is exactly a generator. The FOR_ITER
// specializer reads this to pick FOR_ITER_GEN, and SEND uses it to
// pick SEND_GEN.
//
// CPython: Objects/genobject.c PyGen_Type
func IsGenerator(o Object) bool {
	return Type_(o) == GeneratorType
}

// IsCoroutine reports whether o is exactly a coroutine. SEND uses
// this together with IsGenerator to pick SEND_GEN.
//
// CPython: Objects/genobject.c PyCoro_Type
func IsCoroutine(o Object) bool {
	return Type_(o) == CoroutineType
}

// Type_ is the package-internal accessor used by the exact-type
// predicates so they read uniformly. It cannot be named Type
// because that already exists as the type-object struct name.
func Type_(o Object) *Type {
	if o == nil {
		return nil
	}
	return o.Type()
}
