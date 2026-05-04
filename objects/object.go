package objects

// objectType is the root type that every other type inherits from.
// Mirrors PyBaseObject_Type.
//
// CPython: Objects/typeobject.c:L6299 PyBaseObject_Type
var objectType = func() *Type {
	t := &Type{Name: "object"}
	t.init(typeType)
	t.Bases = []*Type{}
	t.MRO = []*Type{t}
	return t
}()

// ObjectType returns the root `object` type singleton. Mirrors
// PyBaseObject_Type.
//
// CPython: Objects/typeobject.c:L6299 PyBaseObject_Type
func ObjectType() *Type {
	return objectType
}
