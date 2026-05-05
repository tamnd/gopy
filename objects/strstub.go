package objects

// strStub is the v0.3 placeholder for the str type. It carries a Go
// string and supports only Repr/Str/Hash so exception args round-trip
// through the protocol. The full unicodeobject.c port lands in v0.4
// (spec 1616) and replaces this stub.
//
// CPython: Objects/unicodeobject.c:L15188 PyUnicode_Type
type strStub struct {
	Header
	v string
}

var strStubType = NewType("str", []*Type{objectType})

func init() {
	strStubType.Repr = func(o Object) (string, error) {
		return "'" + o.(*strStub).v + "'", nil
	}
	strStubType.Str = func(o Object) (string, error) {
		return o.(*strStub).v, nil
	}
	// Hash + RichCmp lets strings act as dict keys for the v0.6 VM.
	// The real Unicode hash (siphash with the runtime salt) lands
	// with the unicodeobject port in v0.4 / spec 1616.
	strStubType.Hash = func(o Object) (int64, error) {
		s := o.(*strStub).v
		var h uint64 = 1469598103934665603
		for i := 0; i < len(s); i++ {
			h ^= uint64(s[i])
			h *= 1099511628211
		}
		return int64(h), nil
	}
	strStubType.RichCmp = func(a, b Object, op CompareOp) (Object, error) {
		bs, ok := b.(*strStub)
		if !ok {
			return NotImplemented(), nil
		}
		as := a.(*strStub)
		switch op {
		case CompareEQ:
			return NewBool(as.v == bs.v), nil
		case CompareNE:
			return NewBool(as.v != bs.v), nil
		}
		return NotImplemented(), nil
	}
}

// NewStr wraps s in the v0.3 placeholder str object. The real
// unicodeobject port (v0.4) will replace this with a kind/length/data
// layout that matches CPython's compact-string representation.
//
// CPython: Objects/unicodeobject.c:L1985 PyUnicode_FromString
func NewStr(s string) Object {
	o := &strStub{v: s}
	o.init(strStubType)
	return o
}

// StrType returns the type singleton for str.
//
// CPython: Objects/unicodeobject.c:L15188 PyUnicode_Type
func StrType() *Type {
	return strStubType
}
