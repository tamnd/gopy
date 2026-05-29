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

import (
	"fmt"
	"strings"
)

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
	// Pre-encoded PEP-393 narrow storage. CPython's PyUnicodeObject
	// keeps one of `Py_UCS1*` / `Py_UCS2*` / `Py_UCS4*` based on the
	// kind tag; gopy keeps the canonical UTF-8 view in `v` and builds
	// the matching slab in `classify` for non-ASCII strings so codepoint
	// indexing runs in O(1) (data1[i] / data2[i] / data4[i]) instead of
	// re-walking the UTF-8 bytes. ASCII strings leave all three slabs
	// nil since byte index equals codepoint index in `v`.
	//
	// CPython: Include/cpython/unicodeobject.h:75 PyUnicode_Kind
	// CPython: Include/cpython/unicodeobject.h:135 PyUnicode_DATA
	data1 []uint8  // kind=1, !ascii: latin1 codepoints 0x80..0xFF
	data2 []uint16 // kind=2: UCS-2 BMP codepoints
	data4 []uint32 // kind=4: full Unicode codepoints
	// attrs holds instance attributes for str subclass objects. Nil for
	// plain str instances; allocated by strSubclassSetAttr when first
	// written. Mirrors CPython's tp_dictoffset on str subclasses (which
	// CPython sets via type_new_descriptors when the subclass picks up
	// __dict__ from object).
	attrs *Dict
}

// AttrDict implements AttrDictHolder so MemberDescr can store __slots__
// values in the attrs dict for str subclasses.
// CPython: Objects/object.c _PyObject_GetDictPtr (str-subclass path)
func (u *Unicode) AttrDict() *Dict { return u.attrs }

// EnsureAttrDict allocates the attrs dict on first use.
func (u *Unicode) EnsureAttrDict() *Dict {
	if u.attrs == nil {
		u.attrs = NewDict()
	}
	return u.attrs
}

// strType is the type singleton for str.
//
// CPython: Objects/unicodeobject.c:L15188 PyUnicode_Type
var strType = NewType("str", []*Type{objectType})

// strTpNew is the type-aware tp_new for str. When cls is strType itself,
// callers go through the builtin str() factory wired by bindStrCtor. When
// cls is a strict subclass (e.g. enum.StrEnum), allocate a *Unicode
// tagged with cls so isinstance(obj, MyStr) holds and the subclass's
// __dict__ slot (attrs field) is reachable.
//
// CPython: Objects/unicodeobject.c:13993 unicode_subtype_new
var strTpNew func(cls *Type, args []Object, kwargs map[string]Object) (Object, error)

// SetStrTpNewBase is called from package builtins to wire the value-side
// constructor (str(obj, [encoding], [errors])). The subtype path here
// runs the same constructor, then re-tags the *Unicode with cls.
func SetStrTpNewBase(fn func(args []Object, kwargs map[string]Object) (Object, error)) {
	strTpNew = func(cls *Type, args []Object, kwargs map[string]Object) (Object, error) {
		out, err := fn(args, kwargs)
		if err != nil {
			return nil, err
		}
		if cls == nil || cls == strType {
			return out, nil
		}
		// Subclass: re-wrap so the instance's type is cls (and its
		// attrs slot is available).
		u, ok := out.(*Unicode)
		if !ok {
			return nil, fmt.Errorf("TypeError: str.__new__ expected str result, got %s", out.Type().Name)
		}
		return newStrAs(u.v, cls), nil
	}
	strType.TpNew = strTpNew
}

// unicodeReprDescr backs str.__dict__["__repr__"]. CPython installs a
// slot wrapper for tp_repr; subclasses look it up in str's __dict__ to
// avoid inheriting object.__repr__'s `<addr object>` format. Without
// this, fixupCallReprStr finds object's __repr__ via MRO and binds the
// generic dispatcher, so MyStr("hello") prints as `<MyStr object at
// 0x...>` instead of 'hello'.
//
// CPython: Objects/typeobject.c slotdefs (TPSLOT __repr__ +
// PyUnicode_Type.tp_repr)
func unicodeReprDescr(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: expected 1 argument, got %d", len(args))
	}
	s, ok := args[0].(*Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__repr__' requires a 'str' object")
	}
	out, err := unicodeRepr(s)
	if err != nil {
		return nil, err
	}
	return NewStr(out), nil
}

func unicodeStrDescr(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: expected 1 argument, got %d", len(args))
	}
	s, ok := args[0].(*Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__str__' requires a 'str' object")
	}
	if s.Type() == strType {
		return s, nil
	}
	return NewStr(s.v), nil
}

// unicodeHashDescr backs str.__dict__["__hash__"]. Mirrors CPython's
// slot wrapper, so str subclasses inherit unicode_hash rather than
// object.__hash__ (which returns id(obj)).
//
// CPython: Objects/typeobject.c slotdefs (TPSLOT __hash__ +
// PyUnicode_Type.tp_hash)
func unicodeHashDescr(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: expected 1 argument, got %d", len(args))
	}
	s, ok := args[0].(*Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__hash__' requires a 'str' object")
	}
	h, err := unicodeHash(s)
	if err != nil {
		return nil, err
	}
	return NewInt(h), nil
}

// unicodeEqDescr backs str.__dict__["__eq__"]. Without an entry here,
// fixupRichCmpAndBool finds object.__eq__ in the MRO and routes
// comparison through identity, so MyStr("x") == "x" is False.
//
// CPython: Objects/typeobject.c slotdefs (RICHCMP slot map for tp_richcompare)
func unicodeEqDescr(args []Object, _ map[string]Object) (Object, error) {
	return unicodeRichCmpDescr(args, CompareEQ)
}

func unicodeNeDescr(args []Object, _ map[string]Object) (Object, error) {
	return unicodeRichCmpDescr(args, CompareNE)
}

func unicodeLtDescr(args []Object, _ map[string]Object) (Object, error) {
	return unicodeRichCmpDescr(args, CompareLT)
}

func unicodeLeDescr(args []Object, _ map[string]Object) (Object, error) {
	return unicodeRichCmpDescr(args, CompareLE)
}

func unicodeGtDescr(args []Object, _ map[string]Object) (Object, error) {
	return unicodeRichCmpDescr(args, CompareGT)
}

func unicodeGeDescr(args []Object, _ map[string]Object) (Object, error) {
	return unicodeRichCmpDescr(args, CompareGE)
}

func unicodeRichCmpDescr(args []Object, op CompareOp) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: expected 2 arguments, got %d", len(args))
	}
	if _, ok := args[0].(*Unicode); !ok {
		return nil, fmt.Errorf("TypeError: descriptor requires a 'str' object")
	}
	return unicodeRichCmp(args[0], args[1], op)
}

func init() {
	strType.Repr = unicodeRepr
	strType.Str = unicodeStr
	strType.Hash = unicodeHash
	strType.RichCmp = unicodeRichCmp
	strType.Getattro = GenericGetAttr
	strType.TpFlags |= TpFlagMatchSelf
	SetTypeDescr(strType, "__repr__", NewMethodDescr(strType, "__repr__", unicodeReprDescr))
	SetTypeDescr(strType, "__str__", NewMethodDescr(strType, "__str__", unicodeStrDescr))
	SetTypeDescr(strType, "__hash__", NewMethodDescr(strType, "__hash__", unicodeHashDescr))
	SetTypeDescr(strType, "__eq__", NewMethodDescr(strType, "__eq__", unicodeEqDescr))
	SetTypeDescr(strType, "__ne__", NewMethodDescr(strType, "__ne__", unicodeNeDescr))
	SetTypeDescr(strType, "__lt__", NewMethodDescr(strType, "__lt__", unicodeLtDescr))
	SetTypeDescr(strType, "__le__", NewMethodDescr(strType, "__le__", unicodeLeDescr))
	SetTypeDescr(strType, "__gt__", NewMethodDescr(strType, "__gt__", unicodeGtDescr))
	SetTypeDescr(strType, "__ge__", NewMethodDescr(strType, "__ge__", unicodeGeDescr))
	// Sequence.Repeat: 'ab' * 3 == 'ababab'.
	//
	// CPython: Objects/unicodeobject.c:11556 unicode_repeat
	strType.Iter = strIter
	strType.Sequence = &SequenceMethods{
		Length: func(o Object) (int, error) {
			s := o.(*Unicode)
			return s.length, nil
		},
		GetItem: unicodeGetItem,
		// CPython: Objects/unicodeobject.c:11556 unicode_repeat
		Repeat: func(o Object, n int) (Object, error) {
			s := o.(*Unicode)
			if n <= 0 {
				return NewStr(""), nil
			}
			if n == 1 && s.Type() == strType {
				return s, nil
			}
			b := make([]byte, 0, len(s.v)*n)
			for range n {
				b = append(b, s.v...)
			}
			return NewStr(string(b)), nil
		},
		Concat: func(a, b Object) (Object, error) {
			sa, _ := a.(*Unicode)
			sb, _ := b.(*Unicode)
			if sa == nil || sb == nil {
				// CPython names the bad-side type in the message
				// (typically the right operand, since ensure_unicode on
				// `a` would have raised before we got here in real use).
				//
				// CPython: Objects/unicodeobject.c:11641 PyUnicode_Concat
				other := b
				if sa == nil {
					other = a
				}
				return nil, fmt.Errorf("TypeError: can only concatenate str (not %q) to str", typeNameOf(other))
			}
			return NewStr(sa.v + sb.v), nil
		},
		// `needle in haystack` for str is substring containment, not the
		// generic item-by-item walk PySequence_Contains falls back to.
		// CPython routes through unicode_contains which calls
		// PyUnicode_Contains; that's a memmem-style search.
		// CPython: Objects/unicodeobject.c:11192 unicode_contains
		Contains: func(haystack, needle Object) (bool, error) {
			hs, ok := haystack.(*Unicode)
			if !ok {
				return false, fmt.Errorf("TypeError: 'in <string>' requires str as left operand, not %s", haystack.Type().Name)
			}
			ns, ok := needle.(*Unicode)
			if !ok {
				return false, fmt.Errorf("TypeError: 'in <string>' requires str as left operand, not %s", needle.Type().Name)
			}
			return strings.Contains(hs.v, ns.v), nil
		},
	}
	// Mapping.GetItem handles both integer and slice subscript on str,
	// mirroring unicode_subscript which routes to unicode_getitem for
	// integers and PyUnicode_Substring for slices.
	//
	// CPython: Objects/unicodeobject.c:15232 unicode_subscript
	strType.Mapping = &MappingMethods{
		GetItem: func(o, key Object) (Object, error) {
			u := o.(*Unicode)
			if sl, ok := key.(*Slice); ok {
				return StrGetSlice(u, sl)
			}
			n, err := NumberIndex(key)
			if err != nil {
				return nil, fmt.Errorf("TypeError: string indices must be integers, not '%s'", typeNameOf(key))
			}
			i, ok := n.(*Int)
			if !ok {
				return nil, fmt.Errorf("TypeError: string indices must be integers, not '%s'", typeNameOf(key))
			}
			v, _ := i.Int64()
			return unicodeGetItemKind(u, int(v))
		},
	}
	// CPython: Objects/typeobject.c add_operators slotdefs tp_iter row
	AddIterSlotWrappers(strType)
}

// strIterator is the iterator returned by iter(str). It walks the
// codepoint count without materializing a []rune; each next() reads
// the i-th codepoint through the source's PEP-393 slab in O(1) and
// returns the cached latin1 singleton for codepoints < 0x100.
//
// CPython: Objects/unicodeobject.c:15126 unicodeiter_new
type strIterator struct {
	Header
	src *Unicode
	pos int
}

var strIterType = NewType("str_iterator", []*Type{objectType})

func init() {
	strIterType.Iter = func(o Object) (Object, error) { return o, nil }
	strIterType.IterNext = func(o Object) (Object, error) {
		it := o.(*strIterator)
		if it.src == nil || it.pos >= it.src.length {
			it.src = nil
			return nil, ErrStopIteration
		}
		r := it.src.RuneAt(it.pos)
		it.pos++
		if r < 0x100 {
			return latin1Cache[r], nil
		}
		return NewStr(string(r)), nil
	}
	AddIterSlotWrappers(strIterType)
	// CPython: Objects/unicodeobject.c:15178 unicodeiter_reduce
	SetTypeDescr(strIterType, "__reduce__", NewMethodDescr(strIterType, "__reduce__",
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("TypeError: __reduce__() takes no arguments")
			}
			it := args[0].(*strIterator)
			if BuiltinLookup == nil {
				return nil, fmt.Errorf("PicklingError: builtins not loaded")
			}
			iterFn, err := BuiltinLookup("iter")
			if err != nil {
				return nil, err
			}
			if it.src == nil {
				return NewTuple([]Object{iterFn, NewTuple([]Object{NewStr("")})}), nil
			}
			return NewTuple([]Object{iterFn, NewTuple([]Object{it.src}), NewInt(int64(it.pos))}), nil
		},
	))
	// CPython: Objects/unicodeobject.c:15195 unicodeiter_setstate
	SetTypeDescr(strIterType, "__setstate__", NewMethodDescr(strIterType, "__setstate__",
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("TypeError: __setstate__() takes exactly one argument")
			}
			it := args[0].(*strIterator)
			idx, ok := args[1].(*Int)
			if !ok {
				return nil, fmt.Errorf("TypeError: __setstate__ requires int argument")
			}
			n := int64(0)
			if it.src != nil {
				n = int64(it.src.length)
			}
			v, _ := idx.Int64()
			if v < 0 {
				v = 0
			} else if v > n {
				v = n
			}
			it.pos = int(v)
			return None(), nil
		},
	))
}

// strIter returns an iterator over the code points of a string.
//
// CPython: Objects/unicodeobject.c:15126 unicodeiter_new
func strIter(o Object) (Object, error) {
	s := o.(*Unicode)
	it := &strIterator{src: s}
	it.init(strIterType)
	return it, nil
}

// NewStr wraps s in a Unicode object. The constructor walks the
// runes to determine the narrowest PEP 393 kind: ASCII-only strings
// get kind 1 + ascii=true; strings with all code points < 0x100 get
// kind 1 (Latin-1); < 0x10000 get kind 2; anything with an astral
// codepoint gets kind 4.
//
// CPython: Objects/unicodeobject.c:L1985 PyUnicode_FromString
func NewStr(s string) Object {
	if u, ok := latin1StringHit(s); ok {
		return u
	}
	o := &Unicode{
		v:    s,
		hash: -1,
	}
	o.classify()
	o.init(strType)
	return o
}

// classify scans v to fill in length, kind, ascii, and the matching
// PEP-393 narrow slab. Called from NewStr; idempotent.
//
// The single-pass scan tracks the largest codepoint to pick the
// narrowest kind that fits, then a second pass fills the chosen slab
// (data1 for non-ASCII latin1, data2 for BMP, data4 for full
// unicode). ASCII strings skip slab allocation: byte index equals
// codepoint index in `v` so `unicodeGetItemKind` reads s.v[i]
// directly.
//
// CPython: Objects/unicodeobject.c:1696 find_maxchar_surrogates
// CPython: Objects/unicodeobject.c:1731 _PyUnicode_Ready (slab fill)
func (u *Unicode) classify() {
	runes := strLenientRunes(u.v)
	n := len(runes)
	maxr := rune(0)
	for _, r := range runes {
		if r > maxr {
			maxr = r
		}
	}
	u.length = n
	switch {
	case maxr < 0x80:
		u.kind = StrKind1Byte
		u.ascii = true
	case maxr < 0x100:
		u.kind = StrKind1Byte
		u.data1 = encodeUCS1Runes(runes)
	case maxr < 0x10000:
		u.kind = StrKind2Byte
		u.data2 = encodeUCS2Runes(runes)
	default:
		u.kind = StrKind4Byte
		u.data4 = encodeUCS4Runes(runes)
	}
	u.ready = true
}

// strLenientRunes decodes a Go string to runes, accepting lone surrogates
// stored as 3-byte pseudo-UTF-8 (0xED 0xA0-0xBF 0x80-0xBF). Go's standard
// range loop rejects surrogates; this decoder passes them through.
//
// CPython: Objects/unicodeobject.c:1696 find_maxchar_surrogates
func strLenientRunes(s string) []rune {
	b := []byte(s)
	var out []rune
	for i := 0; i < len(b); {
		c := b[i]
		if c < 0x80 { //nolint:gocritic // manual UTF-8 decoder needed for surrogate pass-through
			out = append(out, rune(c))
			i++
		} else if c&0xE0 == 0xC0 && i+1 < len(b) && b[i+1]&0xC0 == 0x80 {
			r := rune(c&0x1F)<<6 | rune(b[i+1]&0x3F)
			out = append(out, r)
			i += 2
		} else if c&0xF0 == 0xE0 && i+2 < len(b) && b[i+1]&0xC0 == 0x80 && b[i+2]&0xC0 == 0x80 {
			r := rune(c&0x0F)<<12 | rune(b[i+1]&0x3F)<<6 | rune(b[i+2]&0x3F)
			out = append(out, r) // includes surrogates U+D800..U+DFFF
			i += 3
		} else if c&0xF8 == 0xF0 && i+3 < len(b) && b[i+1]&0xC0 == 0x80 && b[i+2]&0xC0 == 0x80 && b[i+3]&0xC0 == 0x80 {
			r := rune(c&0x07)<<18 | rune(b[i+1]&0x3F)<<12 | rune(b[i+2]&0x3F)<<6 | rune(b[i+3]&0x3F)
			out = append(out, r)
			i += 4
		} else {
			out = append(out, 0xFFFD)
			i++
		}
	}
	return out
}

func encodeUCS1Runes(runes []rune) []uint8 {
	out := make([]uint8, len(runes))
	for i, r := range runes {
		out[i] = uint8(r)
	}
	return out
}

func encodeUCS2Runes(runes []rune) []uint16 {
	out := make([]uint16, len(runes))
	for i, r := range runes {
		out[i] = uint16(r)
	}
	return out
}

func encodeUCS4Runes(runes []rune) []uint32 {
	out := make([]uint32, len(runes))
	for i, r := range runes {
		out[i] = uint32(r)
	}
	return out
}

// runeToStr converts a rune to a Go string, emitting pseudo-UTF-8
// for surrogates (U+D800..U+DFFF) instead of U+FFFD. Go's string(r)
// maps surrogates to the replacement character; this function preserves
// them so Python strings containing lone surrogates round-trip correctly.
func runeToStr(r rune) string {
	if r >= 0xD800 && r <= 0xDFFF {
		return string([]byte{
			byte(0xE0 | (r >> 12)),
			byte(0x80 | ((r >> 6) & 0x3F)),
			byte(0x80 | (r & 0x3F)),
		})
	}
	return string(r)
}

// Value returns the canonical Go string. Same-package callers may
// also read s.v directly; this getter exists for cross-package use.
func (u *Unicode) Value() string { return u.v }

// Kind returns the PEP 393 kind tag.
func (u *Unicode) Kind() byte { return u.kind }

// Length returns the number of code points.
func (u *Unicode) Length() int { return u.length }

// IsASCII reports whether every code point is < 0x80.
func (u *Unicode) IsASCII() bool { return u.ascii }

// IsReady reports whether the canonical layout is built. Always true
// for strings created via NewStr.
func (u *Unicode) IsReady() bool { return u.ready }

// Data1 returns the pre-encoded UCS-1 (latin1) slab. Non-nil only for
// non-ASCII kind-1 strings (max codepoint in 0x80..0xFF). ASCII
// callers should use u.Value() directly since byte index equals
// codepoint index.
//
// CPython: Include/cpython/unicodeobject.h:135 PyUnicode_1BYTE_DATA
func (u *Unicode) Data1() []uint8 { return u.data1 }

// Data2 returns the pre-encoded UCS-2 (BMP) slab. Non-nil only for
// kind-2 strings (max codepoint in 0x100..0xFFFF).
//
// CPython: Include/cpython/unicodeobject.h:138 PyUnicode_2BYTE_DATA
func (u *Unicode) Data2() []uint16 { return u.data2 }

// Data4 returns the pre-encoded UCS-4 (full unicode) slab. Non-nil
// only for kind-4 strings (max codepoint >= 0x10000).
//
// CPython: Include/cpython/unicodeobject.h:141 PyUnicode_4BYTE_DATA
func (u *Unicode) Data4() []uint32 { return u.data4 }

// RuneAt returns the i-th codepoint of u as a Go rune, using the
// pre-encoded slab so no UTF-8 walk is needed. Caller is responsible
// for clamping i to [0, u.length).
//
// CPython: Include/cpython/unicodeobject.h:151 PyUnicode_READ
func (u *Unicode) RuneAt(i int) rune {
	if u.ascii {
		return rune(u.v[i])
	}
	switch u.kind {
	case StrKind1Byte:
		return rune(u.data1[i])
	case StrKind2Byte:
		return rune(u.data2[i])
	case StrKind4Byte:
		return rune(u.data4[i])
	}
	return 0
}

// unicodeRepr emits the Python repr() of a str. Mirrors CPython's
// two-pass scan: first count single/double quotes to decide the outer
// quote, then walk the runes escaping the special characters
// (\\, \', or \", \t, \n, \r), encoding ASCII controls as \xHH, and
// rendering non-printable Unicode as \xHH / \uHHHH / \UHHHHHHHH.
//
// CPython: Objects/unicodeobject.c:12956 unicode_repr
// CPython: Objects/stringlib/repr.h:9 STRINGLIB(repr) inner loop
func unicodeRepr(o Object) (string, error) {
	s := o.(*Unicode).v

	squote, dquote := 0, 0
	for _, ch := range s {
		switch ch {
		case '\'':
			squote++
		case '"':
			dquote++
		}
	}

	quote := byte('\'')
	if squote > 0 && dquote == 0 {
		quote = '"'
	}

	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte(quote)
	for _, ch := range s {
		switch {
		case ch == rune(quote) || ch == '\\':
			b.WriteByte('\\')
			b.WriteRune(ch)
		case ch == '\t':
			b.WriteString(`\t`)
		case ch == '\n':
			b.WriteString(`\n`)
		case ch == '\r':
			b.WriteString(`\r`)
		case ch < ' ' || ch == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, ch)
		case ch < 0x7f:
			b.WriteRune(ch)
		default:
			if !IsPrintableRune(ch) {
				switch {
				case ch <= 0xff:
					fmt.Fprintf(&b, `\x%02x`, ch)
				case ch <= 0xffff:
					fmt.Fprintf(&b, `\u%04x`, ch)
				default:
					fmt.Fprintf(&b, `\U%08x`, ch)
				}
			} else {
				b.WriteRune(ch)
			}
		}
	}
	b.WriteByte(quote)
	return b.String(), nil
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
	return o.(*Unicode).HashCached(), nil
}

// HashCached returns the cached SipHash of u, computing and caching
// it on first call. The version without the Type-slot indirection
// lets hot callers (LOAD_NAME / LOAD_GLOBAL / Dict KnownHash arms)
// skip PyObject_Hash's vtable dispatch when the key is statically
// known to be a *Unicode.
//
// CPython: Objects/unicodeobject.c:L11532 unicode_hash (the cached
// branch is the same; PyUnicode's _PyObject_HashFast inlines the
// cached-hash read directly into the dict lookup).
func (u *Unicode) HashCached() int64 {
	if u.hash != -1 {
		return u.hash
	}
	h := HashString(u.v)
	u.hash = h
	return h
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

// newStrAs is NewStr but tagged with t instead of strType. Used by the
// str subtype path so a class like `class MyStr(str): pass` yields
// instances whose Type() is MyStr.
//
// CPython: Objects/unicodeobject.c:13993 unicode_subtype_new
func newStrAs(s string, t *Type) *Unicode {
	o := &Unicode{
		v:    s,
		hash: -1,
	}
	o.classify()
	o.init(t)
	return o
}

// strSubclassGetAttr is the tp_getattro slot for user-defined str
// subclasses. Instance attributes from u.attrs win over non-data
// descriptors; data descriptors on the type still take priority.
//
// CPython: Objects/typeobject.c:5165 slot_tp_getattr_hook (str path)
func strSubclassGetAttr(o Object, name Object) (Object, error) {
	u, ok := o.(*Unicode)
	if !ok {
		return GenericGetAttr(o, name)
	}
	tp := u.Type()
	nameStr := attrNameStr(name)
	descr, _ := LookupDescriptor(tp, nameStr)
	if descr != nil {
		if dget := descr.Type().DescrGet; dget != nil {
			if descr.Type().DescrSet != nil {
				return dget(descr, o, tp)
			}
		}
	}
	if u.attrs != nil {
		if v, err := u.attrs.GetItem(name); err == nil {
			return v, nil
		}
	}
	if descr != nil {
		if dget := descr.Type().DescrGet; dget != nil {
			return dget(descr, o, tp)
		}
		return descr, nil
	}
	return nil, fmt.Errorf("AttributeError: '%s' object has no attribute '%s'", tp.Name, nameStr)
}

// strSubclassSetAttr is the tp_setattro slot for user-defined str
// subclasses. Instance attributes land in u.attrs.
//
// CPython: Objects/object.c:2040 PyObject_GenericSetAttr (str-subclass path)
func strSubclassSetAttr(o Object, name Object, value Object) error {
	u, ok := o.(*Unicode)
	if !ok {
		return GenericSetAttr(o, name, value)
	}
	tp := u.Type()
	nameStr := attrNameStr(name)
	descr, _ := LookupDescriptor(tp, nameStr)
	if descr != nil {
		if dset := descr.Type().DescrSet; dset != nil {
			return dset(descr, o, value)
		}
	}
	if u.attrs == nil {
		u.attrs = NewDict()
	}
	if value == nil {
		if _, err := u.attrs.GetItem(name); err != nil {
			return fmt.Errorf("AttributeError: '%s' object has no attribute '%s'", tp.Name, nameStr)
		}
		return u.attrs.DelItem(name)
	}
	return u.attrs.SetItem(name, value)
}

// unicodeGetItem returns the Nth code point of a string as a single-char
// string. Supports negative indexing. Routes through the kind-dispatched
// fast path so ASCII strings index in O(1) via byte addressing.
//
// CPython: Objects/unicodeobject.c:1848 unicode_getitem
func unicodeGetItem(o Object, i int) (Object, error) {
	return unicodeGetItemKind(o.(*Unicode), i)
}
