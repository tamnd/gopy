package objects

import "testing"

func TestByteArrayRepr(t *testing.T) {
	cases := []struct {
		in   []byte
		want string
	}{
		{nil, "bytearray(b'')"},
		{[]byte("hello"), "bytearray(b'hello')"},
		{[]byte{0x01}, `bytearray(b'\x01')`},
	}
	for _, c := range cases {
		got, err := byteArrayRepr(NewByteArray(c.in))
		if err != nil {
			t.Fatalf("byteArrayRepr(%v) error: %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("byteArrayRepr(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestByteArrayUnhashable(t *testing.T) {
	if _, err := byteArrayHash(NewByteArray([]byte("x"))); err == nil {
		t.Fatal("byteArrayHash should error")
	}
}

func TestByteArrayMutate(t *testing.T) {
	b := NewByteArray([]byte("abc"))
	if err := b.Append('d'); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if string(b.v) != "abcd" {
		t.Fatalf("after Append: %q", b.v)
	}
	b.Extend([]byte("ef"))
	if string(b.v) != "abcdef" {
		t.Fatalf("after Extend: %q", b.v)
	}
	if err := b.Insert(0, 'Z'); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if string(b.v) != "Zabcdef" {
		t.Fatalf("after Insert: %q", b.v)
	}
	v, err := b.Pop(-1)
	if err != nil || v != 'f' {
		t.Fatalf("Pop: %v %v", v, err)
	}
	if string(b.v) != "Zabcde" {
		t.Fatalf("after Pop: %q", b.v)
	}
	b.Reverse()
	if string(b.v) != "edcbaZ" {
		t.Fatalf("after Reverse: %q", b.v)
	}
	b.Clear()
	if len(b.v) != 0 {
		t.Fatalf("after Clear: %q", b.v)
	}
}

func TestByteArrayAppendOutOfRange(t *testing.T) {
	b := NewByteArray(nil)
	if err := b.Append(256); err == nil {
		t.Fatal("Append(256) should error")
	}
	if err := b.Append(-1); err == nil {
		t.Fatal("Append(-1) should error")
	}
}

func TestByteArrayRichCmp(t *testing.T) {
	a := NewByteArray([]byte("abc"))
	b := NewBytes([]byte("abc"))
	got, err := byteArrayRichCmp(a, b, CompareEQ)
	if err != nil || got != True() {
		t.Errorf("ba == bytes: %v %v", got, err)
	}
}
