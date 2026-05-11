// Unicode is the Python str object. The layout pins PEP 393's
// compact representation: a kind tag (1, 2, or 4 bytes per
// character), length in characters (not bytes), an ASCII fast-path
// flag, a ready flag, and a hash cache.
//
// CPython stores the character data in three parallel layouts and
// picks the narrowest that fits the maximum codepoint. This port
// keeps the Go string as the source of truth and selects the kind on
// construction by walking the runes.
//
// The Go-side type is named Unicode to mirror CPython's
// PyUnicodeObject and to avoid colliding with the package-level
// Str(o) helper that ports PyObject_Str. From the Python side both
// are still spelled "str".
//
// CPython: Include/cpython/unicodeobject.h:L86 PyASCIIObject /
// PyCompactUnicodeObject layout
// CPython: Objects/unicodeobject.c:L15188 PyUnicode_Type

package objects

import "fmt"

// PEP 393 kind values. Match CPython's PyUnicode_Kind enum.
//
// CPython: Include/cpython/unicodeobject.h:L75 PyUnicode_Kind
const (
	StrKind1Byte byte = 1
	StrKind2Byte byte = 2
	StrKind4Byte byte = 4
)

// Unicode is the Python str type. The struct layout pins the PEP 393
// fields (kind, length, ascii, ready, hash cache) plus the Go string
// the constructor preserves so methods that need byte-level access
// (encode, repr) can read it without re-walking the kind data.
//
// CPython: Include/cpython/unicodeobject.h PyASCIIObject + PyCompactUnicodeObject
type Unicode struct {
	Header
	v      string // canonical UTF-8 storage
	kind   byte   // PEP 393 kind: 1, 2, or 4
	length int    // number of code points
	ascii  bool   // all code points < 0x80
	ready  bool   // CPython's "interned/canonicalized" flag
	hash   int64  // -1 if not yet computed
}

// strType is the type singleton for str.
//
// CPython: Objects/unicodeobject.c:L15188 PyUnicode_Type
var strType = NewType("str", []*Type{objectType})

func init() {
	strType.Repr = unicodeRepr
	strType.Str = unicodeStr
	strType.Hash = unicodeHash
	strType.RichCmp = unicodeRichCmp
	strType.Getattro = GenericGetAttr
	// Sequence.Repeat: 'ab' * 3 == 'ababab'.
	//
	// CPython: Objects/unicodeobject.c:11556 unicode_repeat
	strType.Sequence = &SequenceMethods{
		Length: func(o Object) (int, error) {
			s := o.(*Unicode)
			return s.length, nil
		},
		Repeat: func(o Object, n int) (Object, error) {
			s := o.(*Unicode)
			if n <= 0 {
				return NewStr(""), nil
			}
			b := make([]byte, 0, len(s.v)*n)
			for i := 0; i < n; i++ {
				b = append(b, s.v...)
			}
			return NewStr(string(b)), nil
		},
		Concat: func(a, b Object) (Object, error) {
			sa, _ := a.(*Unicode)
			sb, _ := b.(*Unicode)
			if sa == nil || sb == nil {
				return nil, fmt.Errorf("TypeError: can only concatenate str to str")
			}
			return NewStr(sa.v + sb.v), nil
		},
	}
}

// NewStr wraps s in a Unicode object. The constructor walks the
// runes to determine the narrowest PEP 393 kind: ASCII-only strings
// get kind 1 + ascii=true; strings with all code points < 0x100 get
// kind 1 (Latin-1); < 0x10000 get kind 2; anything with an astral
// codepoint gets kind 4.
//
// CPython: Objects/unicodeobject.c:L1985 PyUnicode_FromString
func NewStr(s string) Object {
	o := &Unicode{
		v:    s,
		hash: -1,
	}
	o.classify()
	o.init(strType)
	return o
}

// classify scans v to fill in length, kind, and ascii. Called from
// NewStr; idempotent.
//
// CPython: Objects/unicodeobject.c:L1696 find_maxchar_surrogates
func (s *Unicode) classify() {
	maxr := rune(0)
	n := 0
	for _, r := range s.v {
		if r > maxr {
			maxr = r
		}
		n++
	}
	s.length = n
	switch {
	case maxr < 0x80:
		s.kind = StrKind1Byte
		s.ascii = true
	case maxr < 0x100:
		s.kind = StrKind1Byte
	case maxr < 0x10000:
		s.kind = StrKind2Byte
	default:
		s.kind = StrKind4Byte
	}
	s.ready = true
}

// Value returns the canonical Go string. Same-package callers may
// also read s.v directly; this getter exists for cross-package use.
func (s *Unicode) Value() string { return s.v }

// Kind returns the PEP 393 kind tag.
func (s *Unicode) Kind() byte { return s.kind }

// Length returns the number of code points.
func (s *Unicode) Length() int { return s.length }

// IsASCII reports whether every code point is < 0x80.
func (s *Unicode) IsASCII() bool { return s.ascii }

// IsReady reports whether the canonical layout is built. Always true
// for strings created via NewStr.
func (s *Unicode) IsReady() bool { return s.ready }

// unicodeRepr returns "'value'" with no escaping. The full repr that
// escapes control characters and quotes lives in 1677-B.
//
// CPython: Objects/unicodeobject.c:L11756 unicode_repr
func unicodeRepr(o Object) (string, error) {
	return "'" + o.(*Unicode).v + "'", nil
}

// unicodeStr returns the raw value.
//
// CPython: Objects/unicodeobject.c:L11823 unicode_str
func unicodeStr(o Object) (string, error) {
	return o.(*Unicode).v, nil
}

// unicodeHash routes through SipHash-1-3 with the runtime hash
// secret. Empty strings hash to 0. The result is cached on the
// instance.
//
// CPython: Objects/unicodeobject.c:L11532 unicode_hash
func unicodeHash(o Object) (int64, error) {
	s := o.(*Unicode)
	if s.hash != -1 {
		return s.hash, nil
	}
	h := HashString(s.v)
	s.hash = h
	return h, nil
}

// unicodeRichCmp implements all six rich comparisons. The lexicographic
// ordering for <, <=, >, >= compares the underlying Go strings, which
// matches CPython for ASCII and well-formed UTF-8 byte ordering.
//
// CPython: Objects/unicodeobject.c:L11297 unicode_richcompare
func unicodeRichCmp(a, b Object, op CompareOp) (Object, error) {
	bs, ok := b.(*Unicode)
	if !ok {
		return NotImplemented(), nil
	}
	as := a.(*Unicode)
	switch op {
	case CompareEQ:
		return NewBool(as.v == bs.v), nil
	case CompareNE:
		return NewBool(as.v != bs.v), nil
	case CompareLT:
		return NewBool(as.v < bs.v), nil
	case CompareLE:
		return NewBool(as.v <= bs.v), nil
	case CompareGT:
		return NewBool(as.v > bs.v), nil
	case CompareGE:
		return NewBool(as.v >= bs.v), nil
	}
	return NotImplemented(), nil
}

// StrType returns the type singleton for str.
//
// CPython: Objects/unicodeobject.c:L15188 PyUnicode_Type
func StrType() *Type { return strType }
