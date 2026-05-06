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
	// Hash + RichCmp lets strings act as dict keys. Empty strings
	// hash to 0, matching CPython; everything else routes through
	// SipHash-1-3 with the runtime hash secret.
	//
	// CPython: Objects/unicodeobject.c:11532 unicode_hash
	strStubType.Hash = func(o Object) (int64, error) {
		return HashString(o.(*strStub).v), nil
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
