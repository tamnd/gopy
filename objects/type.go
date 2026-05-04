package objects

// CompareOp identifies the six comparison operators Python exposes
// through __lt__/__le__/__eq__/__ne__/__gt__/__ge__. Numeric values
// match the C macros Py_LT..Py_GE so direct ports of slot
// implementations can pass the integer through.
//
// CPython: Include/object.h:L375 Py_LT
type CompareOp int

const (
	// CompareLT is `<`.
	CompareLT CompareOp = iota
	// CompareLE is `<=`.
	CompareLE
	// CompareEQ is `==`.
	CompareEQ
	// CompareNE is `!=`.
	CompareNE
	// CompareGT is `>`.
	CompareGT
	// CompareGE is `>=`.
	CompareGE
)

// Type is the meta-class for every Python value. Slots not yet
// implemented in v0.2 are nil; the runtime checks for nil before
// dispatching.
//
// CPython: Include/cpython/typeobject.h:L139 _typeobject
type Type struct {
	Header

	Name     string
	BaseSize int
	ItemSize int

	Bases []*Type
	MRO   []*Type

	Repr     func(o Object) (string, error)
	Str      func(o Object) (string, error)
	Hash     func(o Object) (int64, error)
	RichCmp  func(a, b Object, op CompareOp) (Object, error)
	Iter     func(o Object) (Object, error)
	IterNext func(o Object) (Object, error)
	Call     func(o Object, args []Object, kwargs map[string]Object) (Object, error)
	Dealloc  func(o Object)

	Number   *NumberMethods
	Sequence *SequenceMethods
	Mapping  *MappingMethods
}

// NumberMethods is the v0.2 subset of tp_as_number. The reflected
// variants are handled by the abstract layer; concrete types only
// implement the forward direction.
//
// CPython: Include/cpython/object.h:L195 PyNumberMethods
type NumberMethods struct {
	Add      func(a, b Object) (Object, error)
	Subtract func(a, b Object) (Object, error)
	Multiply func(a, b Object) (Object, error)
	Negative func(o Object) (Object, error)
	Bool     func(o Object) (bool, error)
	Int      func(o Object) (Object, error)
	Float    func(o Object) (Object, error)
}

// SequenceMethods is the v0.2 subset of tp_as_sequence.
//
// CPython: Include/cpython/object.h:L262 PySequenceMethods
type SequenceMethods struct {
	Length   func(o Object) (int, error)
	Concat   func(a, b Object) (Object, error)
	Repeat   func(o Object, n int) (Object, error)
	GetItem  func(o Object, i int) (Object, error)
	SetItem  func(o Object, i int, v Object) error
	Contains func(o, v Object) (bool, error)
}

// MappingMethods is the v0.2 subset of tp_as_mapping.
//
// CPython: Include/cpython/object.h:L255 PyMappingMethods
type MappingMethods struct {
	Length  func(o Object) (int, error)
	GetItem func(o, key Object) (Object, error)
	SetItem func(o, key, v Object) error
	DelItem func(o, key Object) error
}

// typeType is the type of Type itself. Lazily initialized on first
// use to break the bootstrap cycle (Type.Header.typ == typeType).
//
// CPython: Objects/typeobject.c:L6361 PyType_Type
var typeType = &Type{Name: "type"}

func init() {
	typeType.typ = typeType
	typeType.refcnt.Store(1)
	typeType.Bases = []*Type{}
	typeType.MRO = []*Type{typeType}
}

// TypeType returns the type singleton for `type` itself. Mirrors
// PyType_Type.
//
// CPython: Objects/typeobject.c:L6361 PyType_Type
func TypeType() *Type {
	return typeType
}

// NewType constructs a built-in type with the given name and bases.
// Bases must be non-empty for everything except `object`. The MRO
// is computed via C3 linearization.
//
// CPython: Objects/typeobject.c:L4153 type_new (adapted from)
func NewType(name string, bases []*Type) *Type {
	t := &Type{Name: name, Bases: bases}
	t.init(typeType)
	t.MRO = c3Linearize(t)
	return t
}
