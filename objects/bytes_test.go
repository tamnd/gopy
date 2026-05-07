package objects

import "testing"

func TestBytesRepr(t *testing.T) {
	cases := []struct {
		in   []byte
		want string
	}{
		{nil, "b''"},
		{[]byte("hello"), "b'hello'"},
		{[]byte("it's"), `b"it's"`},
		{[]byte(`hi "you"`), `b'hi "you"'`},
		{[]byte("a\nb\tc"), `b'a\nb\tc'`},
		{[]byte{0x01, 0x7f, 0xff}, `b'\x01\x7f\xff'`},
		{[]byte("a\\b"), `b'a\\b'`},
	}
	for _, c := range cases {
		got, err := bytesRepr(NewBytes(c.in))
		if err != nil {
			t.Fatalf("bytesRepr(%v) error: %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("bytesRepr(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestBytesEmptySingleton(t *testing.T) {
	a := NewBytes(nil)
	b := EmptyBytes()
	c := NewBytesFromString("")
	if a != b || b != c {
		t.Fatalf("empty bytes objects are not the same singleton: %p %p %p", a, b, c)
	}
}

func TestBytesRichCmp(t *testing.T) {
	a := NewBytes([]byte("abc"))
	b := NewBytes([]byte("abd"))
	c := NewBytes([]byte("abc"))
	cases := []struct {
		x, y *Bytes
		op   CompareOp
		want bool
	}{
		{a, c, CompareEQ, true},
		{a, b, CompareEQ, false},
		{a, b, CompareLT, true},
		{b, a, CompareGT, true},
		{a, c, CompareLE, true},
		{a, c, CompareGE, true},
		{a, b, CompareNE, true},
	}
	for _, t1 := range cases {
		got, err := bytesRichCmp(t1.x, t1.y, t1.op)
		if err != nil {
			t.Fatalf("bytesRichCmp error: %v", err)
		}
		if got != NewBool(t1.want) {
			t.Errorf("bytesRichCmp(%v, %v, %v) = %v, want %v", t1.x.v, t1.y.v, t1.op, got, t1.want)
		}
	}
}

func TestBytesContains(t *testing.T) {
	b := NewBytes([]byte("hello"))
	got, err := bytesContains(b, NewBytes([]byte("ell")))
	if err != nil || !got {
		t.Errorf("'ell' in 'hello' = %v %v, want true nil", got, err)
	}
	got, err = bytesContains(b, NewBytes([]byte("xy")))
	if err != nil || got {
		t.Errorf("'xy' in 'hello' = %v %v, want false nil", got, err)
	}
	got, err = bytesContains(b, NewInt(int64('e')))
	if err != nil || !got {
		t.Errorf("ord('e') in 'hello' = %v %v, want true nil", got, err)
	}
	got, err = bytesContains(b, NewInt(99999))
	if err == nil {
		t.Errorf("99999 in bytes should error, got %v %v", got, err)
	}
}
