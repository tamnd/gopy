// Tests for the memoryview type. Cover the v0.10 surface: wrap
// bytes/bytearray, len, indexing, slicing, attribute access, hash
// (read-only only), equality, iteration, tobytes/tolist.

package objects

import (
	"errors"
	"testing"
)

func TestMemoryViewWrapsBytes(t *testing.T) {
	src := NewBytes([]byte("hello"))
	mv, err := NewMemoryView(src)
	if err != nil {
		t.Fatalf("NewMemoryView: %v", err)
	}
	if mv.Len() != 5 {
		t.Fatalf("Len = %d, want 5", mv.Len())
	}
}

func TestMemoryViewIntIndexing(t *testing.T) {
	mv, _ := NewMemoryView(NewBytes([]byte{10, 20, 30}))
	got, err := GetItem(mv, NewInt(1))
	if err != nil {
		t.Fatalf("GetItem: %v", err)
	}
	n, ok := got.(*Int).Int64()
	if !ok || n != 20 {
		t.Fatalf("GetItem(1) = %v, want 20", got)
	}
	if _, err := GetItem(mv, NewInt(99)); err == nil {
		t.Fatalf("GetItem out of range should error")
	}
}

func TestMemoryViewNegativeIndex(t *testing.T) {
	mv, _ := NewMemoryView(NewBytes([]byte{1, 2, 3}))
	got, err := GetItem(mv, NewInt(-1))
	if err != nil {
		t.Fatalf("GetItem(-1): %v", err)
	}
	n, _ := got.(*Int).Int64()
	if n != 3 {
		t.Fatalf("GetItem(-1) = %d, want 3", n)
	}
}

func TestMemoryViewSliceReturnsView(t *testing.T) {
	mv, _ := NewMemoryView(NewBytes([]byte("abcdef")))
	sl := NewSlice(NewInt(1), NewInt(4), nil)
	got, err := GetItem(mv, sl)
	if err != nil {
		t.Fatalf("GetItem(slice): %v", err)
	}
	sub, ok := got.(*MemoryView)
	if !ok {
		t.Fatalf("slice result type = %T, want *MemoryView", got)
	}
	if string(sub.Bytes()) != "bcd" {
		t.Fatalf("slice = %q, want bcd", string(sub.Bytes()))
	}
}

func TestMemoryViewSliceWithStep(t *testing.T) {
	mv, _ := NewMemoryView(NewBytes([]byte("abcdef")))
	sl := NewSlice(nil, nil, NewInt(2))
	got, err := GetItem(mv, sl)
	if err != nil {
		t.Fatalf("GetItem(slice step=2): %v", err)
	}
	sub := got.(*MemoryView)
	if string(sub.Bytes()) != "ace" {
		t.Fatalf("step=2 slice = %q, want ace", string(sub.Bytes()))
	}
}

func TestMemoryViewIteration(t *testing.T) {
	mv, _ := NewMemoryView(NewBytes([]byte{1, 2, 3}))
	it, err := Iter(mv)
	if err != nil {
		t.Fatalf("Iter: %v", err)
	}
	var got []int64
	for {
		v, err := IterNext(it)
		if errors.Is(err, ErrStopIteration) || v == nil {
			break
		}
		if err != nil {
			t.Fatalf("IterNext: %v", err)
		}
		n, _ := v.(*Int).Int64()
		got = append(got, n)
	}
	if len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Fatalf("iter = %v, want [1 2 3]", got)
	}
}

func TestMemoryViewAttributes(t *testing.T) {
	mv, _ := NewMemoryView(NewBytes([]byte("xyz")))
	cases := map[string]string{
		"format":   "B",
		"itemsize": "1",
		"nbytes":   "3",
		"readonly": "True",
		"ndim":     "1",
	}
	for name, want := range cases {
		got, err := GetAttr(mv, NewStr(name))
		if err != nil {
			t.Fatalf("GetAttr %q: %v", name, err)
		}
		s, err := Repr(got)
		if err != nil {
			t.Fatalf("Repr %q: %v", name, err)
		}
		if s != want && (name != "format" || s != "'B'") {
			t.Fatalf("attr %q = %q, want %q", name, s, want)
		}
	}
}

func TestMemoryViewEqualityWithBytes(t *testing.T) {
	mv, _ := NewMemoryView(NewBytes([]byte("abc")))
	other := NewBytes([]byte("abc"))
	got, err := mv.Type().RichCmp(mv, other, CompareEQ)
	if err != nil {
		t.Fatalf("RichCmp: %v", err)
	}
	if got != True() {
		t.Fatalf("memoryview('abc') == b'abc' = %v, want True", got)
	}
}

func TestMemoryViewHashReadOnly(t *testing.T) {
	mv, _ := NewMemoryView(NewBytes([]byte("hash me")))
	h, err := Hash(mv)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if h == 0 {
		t.Fatalf("Hash returned 0; expected non-zero")
	}
}

func TestMemoryViewTobytesCopiesBuffer(t *testing.T) {
	mv, _ := NewMemoryView(NewBytes([]byte("hello")))
	cp := mv.Tobytes()
	if string(cp.Bytes()) != "hello" {
		t.Fatalf("Tobytes = %q, want hello", string(cp.Bytes()))
	}
	// Mutating the copy must not affect the view.
	cp.Bytes()[0] = 'X'
	if mv.Bytes()[0] == 'X' {
		t.Fatalf("Tobytes did not copy; mutation leaked into view")
	}
}

func TestMemoryViewTolistReturnsInts(t *testing.T) {
	mv, _ := NewMemoryView(NewBytes([]byte{4, 5}))
	l := mv.Tolist()
	if l.Len() != 2 {
		t.Fatalf("len = %d, want 2", l.Len())
	}
	first, _ := l.Item(0).(*Int).Int64()
	second, _ := l.Item(1).(*Int).Int64()
	if first != 4 || second != 5 {
		t.Fatalf("Tolist = [%d %d], want [4 5]", first, second)
	}
}

func TestMemoryViewWrapsByteArray(t *testing.T) {
	mv, err := NewMemoryView(NewByteArray([]byte("ba")))
	if err != nil {
		t.Fatalf("NewMemoryView(bytearray): %v", err)
	}
	if mv.Len() != 2 {
		t.Fatalf("Len = %d, want 2", mv.Len())
	}
}

func TestMemoryViewRejectsNonBufferType(t *testing.T) {
	if _, err := NewMemoryView(NewList(nil)); err == nil {
		t.Fatalf("NewMemoryView(list) should error")
	}
}

func TestMemoryViewWrapsAnotherView(t *testing.T) {
	src, _ := NewMemoryView(NewBytes([]byte("abc")))
	wrap, err := NewMemoryView(src)
	if err != nil {
		t.Fatalf("NewMemoryView(memoryview): %v", err)
	}
	if string(wrap.Bytes()) != "abc" {
		t.Fatalf("wrap = %q, want abc", string(wrap.Bytes()))
	}
}

func TestMemoryViewContainsByte(t *testing.T) {
	mv, _ := NewMemoryView(NewBytes([]byte{1, 2, 3}))
	got, err := Contains(mv, NewInt(2))
	if err != nil {
		t.Fatalf("Contains: %v", err)
	}
	if !got {
		t.Fatalf("2 in mv = false, want true")
	}
	got, err = Contains(mv, NewInt(99))
	if err != nil {
		t.Fatalf("Contains: %v", err)
	}
	if got {
		t.Fatalf("99 in mv = true, want false")
	}
}
