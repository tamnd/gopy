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
	// Vectorcall is the PEP 590 fast-call slot. When non-nil, the call
	// machinery uses this instead of going through Call. args is a flat
	// array of positional values followed by keyword values; nargsf is
	// the positional count optionally combined with the
	// VectorcallArgumentsOffset flag; kwnames is a tuple of keyword
	// name strings (or nil).
	//
	// CPython: Include/cpython/object.h tp_vectorcall_offset + Py_TPFLAGS_HAVE_VECTORCALL
	Vectorcall func(callable Object, args []Object, nargsf uint, kwnames *Tuple) (Object, error)
	// Getattro is the tp_getattro slot. Receives the owner and the
	// attribute name (as a Str), returns the attribute value or an
	// error. Mirrors getattrofunc.
	//
	// CPython: Include/cpython/object.h tp_getattro
	Getattro func(o Object, name Object) (Object, error)
	// Setattro is the tp_setattro slot. value==nil signals a delete
	// (PyObject_DelAttr forwards there). Mirrors setattrofunc.
	//
	// CPython: Include/cpython/object.h tp_setattro
	Setattro func(o Object, name Object, value Object) error
	Dealloc  func(o Object)

	// TpTraverse mirrors tp_traverse. It calls visit on every Object
	// reachable directly from o so the cycle collector can walk the
	// reference graph. Returning a non-nil error short-circuits the
	// traversal.
	//
	// CPython: Include/cpython/object.h tp_traverse
	TpTraverse func(o Object, visit Visitor) error

	Number   *NumberMethods
	Sequence *SequenceMethods
	Mapping  *MappingMethods

	// TpFlags mirrors CPython's tp_flags bitset for the subset of flags
	// that affect VM dispatch (MATCH_MAPPING / MATCH_SEQUENCE).
	//
	// CPython: Include/object.h Py_TPFLAGS_*
	TpFlags uint64
}

// Visitor is the visitproc shape passed to TpTraverse. CPython's
// visitproc takes a void* arg; gopy closures already capture state
// so the second argument is unnecessary here.
//
// CPython: Include/cpython/object.h:L83 visitproc
type Visitor func(Object) error

// TpFlag values used by MATCH_MAPPING and MATCH_SEQUENCE.
//
// CPython: Include/object.h:L284 Py_TPFLAGS_MAPPING / Py_TPFLAGS_SEQUENCE
const (
	TpFlagMapping  uint64 = 1 << 6
	TpFlagSequence uint64 = 1 << 5
)

// NumberMethods is the v0.2 subset of tp_as_number. The reflected
// variants are handled by the abstract layer; concrete types only
// implement the forward direction.
//
// CPython: Include/cpython/object.h:L195 PyNumberMethods
type NumberMethods struct {
	Add         func(a, b Object) (Object, error)
	Subtract    func(a, b Object) (Object, error)
	Multiply    func(a, b Object) (Object, error)
	TrueDivide  func(a, b Object) (Object, error)
	FloorDivide func(a, b Object) (Object, error)
	Remainder   func(a, b Object) (Object, error)
	And         func(a, b Object) (Object, error)
	Or          func(a, b Object) (Object, error)
	Xor         func(a, b Object) (Object, error)
	Lshift      func(a, b Object) (Object, error)
	Rshift      func(a, b Object) (Object, error)
	Power       func(a, b, mod Object) (Object, error)
	Divmod      func(a, b Object) (Object, error)
	Negative    func(o Object) (Object, error)
	Positive    func(o Object) (Object, error)
	Absolute    func(o Object) (Object, error)
	Invert      func(o Object) (Object, error)
	Bool        func(o Object) (bool, error)
	Int         func(o Object) (Object, error)
	Float       func(o Object) (Object, error)
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
