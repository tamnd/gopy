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

	Name string
	// Module mirrors __module__ on a type. Empty (treated as
	// "builtins") for the built-in types ported in objects/.
	//
	// CPython: Objects/typeobject.c:907 type_module
	Module   string
	BaseSize int
	ItemSize int

	Bases []*Type
	MRO   []*Type

	Repr    func(o Object) (string, error)
	Str     func(o Object) (string, error)
	Hash    func(o Object) (int64, error)
	RichCmp func(a, b Object, op CompareOp) (Object, error)
	// Format is the tp_format slot. Receives the object and the
	// format-spec string (the part after the colon in str.format).
	// When nil, the protocol-level Format helper falls back to Str
	// for empty specs and raises TypeError otherwise.
	//
	// CPython: Objects/typeobject.c:L8260 slot_tp_format
	Format   func(o Object, spec string) (string, error)
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

	// DescrGet is the tp_descr_get slot. When a class attribute that
	// implements this slot is found during instance attribute lookup,
	// the lookup machinery hands it to DescrGet so the descriptor can
	// produce the bound value. owner is the instance the lookup
	// started from (nil when accessed on the class itself); ownerType
	// is the type whose __mro__ supplied the descriptor.
	//
	// CPython: Include/cpython/object.h tp_descr_get
	DescrGet func(descr Object, owner Object, ownerType *Type) (Object, error)
	// DescrSet is the tp_descr_set slot. value==nil signals a delete.
	//
	// CPython: Include/cpython/object.h tp_descr_set
	DescrSet func(descr Object, owner Object, value Object) error

	Dealloc func(o Object)

	// Finalize is the tp_finalize slot. The cycle collector invokes
	// it once on each unreachable object before reclaiming, and the
	// destruction path fires it from PyObject_CallFinalizer. User
	// classes get this slot populated with a wrapper that calls
	// __del__; built-in types fill it in directly when they need
	// cleanup that runs before memory goes back.
	//
	// CPython: Include/cpython/object.h:237 tp_finalize
	// CPython: Objects/typeobject.c slot_tp_finalize
	Finalize func(o Object)

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
	Async    *AsyncMethods

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
