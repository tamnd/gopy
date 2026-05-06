// Slot bundle structs that hang off Type: PyNumberMethods,
// PySequenceMethods, PyMappingMethods. Each bundle groups the function
// pointers a type fills in to opt into a protocol; the abstract layer
// fans out across these bundles when an operator runs.
//
// CPython: Include/cpython/object.h:195 PyNumberMethods

package objects

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
