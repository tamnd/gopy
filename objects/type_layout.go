package objects

// The built-in object layouts each occupy a distinct C struct, so their
// tp_basicsize / tp_itemsize pairs differ. solid_base reads those sizes
// (via shape_differs) to decide which ancestor fixes a type's instance
// layout, and best_base uses that to reject combining two unrelated
// layouts: type('A', (int, str), {}) raises an instance lay-out conflict
// because int and str are each their own solid base. int stamps its own
// pair next to its other slot wiring; the rest are stamped here in one
// init so the values stay together and easy to audit against CPython.
//
// Values are the CPython 3.14 64-bit __basicsize__ / __itemsize__ pairs.
// object stays at gopy's 0/0 so plain user classes keep reporting 0 (they
// carry no fixed C payload of their own); every variable- or fixed-layout
// built-in below differs from that, which is all solid_base needs.
func init() {
	// CPython: Objects/floatobject.c PyFloat_Type.tp_basicsize
	FloatType.BaseSize = 24
	// CPython: Objects/complexobject.c PyComplex_Type.tp_basicsize
	ComplexType.BaseSize = 32
	// CPython: Objects/unicodeobject.c PyUnicode_Type.tp_basicsize
	strType.BaseSize = 64
	// CPython: Objects/bytesobject.c PyBytes_Type (tp_basicsize/tp_itemsize)
	BytesType.BaseSize = 33
	BytesType.ItemSize = 1
	// CPython: Objects/bytearrayobject.c PyByteArray_Type.tp_basicsize
	ByteArrayType.BaseSize = 56
	// CPython: Objects/tupleobject.c PyTuple_Type (tp_basicsize/tp_itemsize)
	TupleType.BaseSize = 32
	TupleType.ItemSize = 8
	// CPython: Objects/listobject.c PyList_Type.tp_basicsize
	ListType.BaseSize = 40
	// CPython: Objects/dictobject.c PyDict_Type.tp_basicsize
	DictType.BaseSize = 48
	// CPython: Objects/setobject.c PySet_Type.tp_basicsize
	SetType.BaseSize = 200
	// CPython: Objects/setobject.c PyFrozenSet_Type.tp_basicsize
	FrozensetType.BaseSize = 200
	// CPython: Objects/moduleobject.c PyModule_Type.tp_basicsize. A module
	// is its own solid base (md_dict / md_state struct), so combining it
	// with another solid base (e.g. str) raises an instance lay-out
	// conflict, matching CPython.
	ModuleType.BaseSize = 56
}
