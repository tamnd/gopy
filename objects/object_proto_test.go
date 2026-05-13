package objects

import "testing"

// ObjectBytes returns the bytes verbatim when handed a *Bytes.
//
// CPython: Objects/object.c:870 PyObject_Bytes
func TestObjectBytesPassesBytesThrough(t *testing.T) {
	b := NewBytes([]byte("hi"))
	got, err := ObjectBytes(b)
	if err != nil {
		t.Fatal(err)
	}
	if got != b {
		t.Error("ObjectBytes(*Bytes) should return the same identity")
	}
}

// ObjectBytes on nil returns the literal "<NULL>" sentinel, mirroring
// CPython's PyBytes_FromString("<NULL>") fallback.
//
// CPython: Objects/object.c:874 PyObject_Bytes (v == NULL branch)
func TestObjectBytesNilReturnsNullSentinel(t *testing.T) {
	got, err := ObjectBytes(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "<NULL>" {
		t.Errorf("got %q, want <NULL>", got.String())
	}
}

// ObjectBytes on a str uses the UTF-8 bytes; this matches CPython's
// PyBytes_FromObject fallback for strings.
//
// CPython: Objects/object.c:898 PyObject_Bytes (PyBytes_FromObject branch)
func TestObjectBytesEncodesString(t *testing.T) {
	got, err := ObjectBytes(NewStr("hi"))
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "hi" {
		t.Errorf("got %q, want hi", got.String())
	}
}

// ObjectBytes on an int with no __bytes__ raises TypeError.
//
// CPython: Objects/object.c:898 PyObject_Bytes
func TestObjectBytesRejectsInt(t *testing.T) {
	if _, err := ObjectBytes(NewInt(1)); err == nil {
		t.Error("ObjectBytes(*Int) should TypeError")
	}
}

// ASCII passes a repr through when it's already ASCII.
//
// CPython: Objects/object.c:843 PyObject_ASCII
func TestASCIIPassesASCIIThrough(t *testing.T) {
	got, err := ASCII(NewStr("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "'hello'" {
		t.Errorf("got %q, want 'hello'", got)
	}
}

// ASCII backslash-escapes non-ASCII codepoints.
//
// CPython: Objects/object.c:843 PyObject_ASCII
func TestASCIIEscapesNonASCII(t *testing.T) {
	got, err := ASCII(NewStr("é"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "'\\xe9'" {
		t.Errorf("got %q, want 'xe9'", got)
	}
}

// Not flips IsTruthy and propagates errors.
//
// CPython: Objects/object.c:2088 PyObject_Not
func TestNot(t *testing.T) {
	if v, _ := Not(NewInt(0)); !v {
		t.Error("Not(0) should be true")
	}
	if v, _ := Not(NewInt(1)); v {
		t.Error("Not(1) should be false")
	}
	if v, _ := Not(None()); !v {
		t.Error("Not(None) should be true")
	}
}

// Callable distinguishes a function from a plain value.
//
// CPython: Objects/object.c:2100 PyCallable_Check
func TestCallable(t *testing.T) {
	fn := NewBuiltinFunction("f", func(_ []Object, _ map[string]Object) (Object, error) {
		return None(), nil
	})
	if !Callable(fn) {
		t.Error("BuiltinFunction should be callable")
	}
	if Callable(NewInt(1)) {
		t.Error("Int should not be callable")
	}
	if Callable(nil) {
		t.Error("nil should not be callable")
	}
}

// SelfIter returns the same identity.
//
// CPython: Objects/object.c:1548 PyObject_SelfIter
func TestSelfIter(t *testing.T) {
	o := NewStr("x")
	got, err := SelfIter(o)
	if err != nil {
		t.Fatal(err)
	}
	if got != o {
		t.Error("SelfIter should return the same identity")
	}
}

// LookupAttr returns nil with no error when the attribute is missing
// (instead of the AttributeError that GetAttr would raise).
//
// CPython: Objects/object.c:1324 PyObject_GetOptionalAttr
func TestLookupAttrMissingReturnsNil(t *testing.T) {
	got, err := LookupAttr(NewInt(1), NewStr("notathing"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

// LookupAttr surfaces actual attributes the same way GetAttr would.
//
// CPython: Objects/object.c:1324 PyObject_GetOptionalAttr
func TestLookupAttrFoundReturnsValue(t *testing.T) {
	got, err := LookupAttr(NewInt(1), NewStr("__class__"))
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Error("__class__ should resolve")
	}
}

// HasAttr returns false (not error) when the attribute is missing.
//
// CPython: Objects/object.c:1417 PyObject_HasAttrWithError
func TestHasAttrMissing(t *testing.T) {
	has, err := HasAttr(NewInt(1), NewStr("notathing"))
	if err != nil {
		t.Fatal(err)
	}
	if has {
		t.Error("HasAttr(missing) should be false")
	}
}

// HasAttr returns true for present attributes.
//
// CPython: Objects/object.c:1417 PyObject_HasAttrWithError
func TestHasAttrPresent(t *testing.T) {
	has, err := HasAttr(NewInt(1), NewStr("__class__"))
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Error("HasAttr(__class__) should be true")
	}
}

// HasAttrString is the convenience wrapper.
//
// CPython: Objects/object.c:1189 PyObject_HasAttrStringWithError
func TestHasAttrString(t *testing.T) {
	has, err := HasAttrString(NewInt(1), "__class__")
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Error("HasAttrString(__class__) should be true")
	}
}

// Dir on an int returns a sorted list of attribute names.
//
// CPython: Objects/object.c:2189 PyObject_Dir
func TestDirReturnsSortedNames(t *testing.T) {
	l, err := Dir(NewInt(1))
	if err != nil {
		t.Fatal(err)
	}
	if l.Len() == 0 {
		t.Error("Dir(*Int) should expose at least one descriptor")
	}
	for i := 1; i < l.Len(); i++ {
		prev, _ := Str(l.Item(i - 1))
		cur, _ := Str(l.Item(i))
		if prev > cur {
			t.Errorf("Dir not sorted: %q > %q", prev, cur)
		}
	}
}
