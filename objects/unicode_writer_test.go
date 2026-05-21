package objects

import (
	"strings"
	"testing"
)

func TestUnicodeWriterEmptyFinish(t *testing.T) {
	var w UnicodeWriter
	w.Init()
	out := w.Finish()
	if out.Value() != "" {
		t.Fatalf("empty Finish got %q want %q", out.Value(), "")
	}
}

func TestUnicodeWriterWriteCharASCII(t *testing.T) {
	var w UnicodeWriter
	w.Init()
	w.overallocate = true
	if err := w.WriteChar('h'); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteChar('i'); err != nil {
		t.Fatal(err)
	}
	out := w.Finish()
	if out.Value() != "hi" {
		t.Fatalf("got %q want %q", out.Value(), "hi")
	}
	if !out.IsASCII() {
		t.Fatalf("ascii flag should be set")
	}
	if out.Kind() != StrKind1Byte {
		t.Fatalf("kind %d want %d", out.Kind(), StrKind1Byte)
	}
	if out.Length() != 2 {
		t.Fatalf("len %d want 2", out.Length())
	}
}

func TestUnicodeWriterWriteCharWiden(t *testing.T) {
	var w UnicodeWriter
	w.Init()
	w.overallocate = true
	chars := []rune{'a', 'é', 'あ', '𝄞'}
	for _, ch := range chars {
		if err := w.WriteChar(ch); err != nil {
			t.Fatal(err)
		}
	}
	out := w.Finish()
	want := string(chars)
	if out.Value() != want {
		t.Fatalf("got %q want %q", out.Value(), want)
	}
	if out.IsASCII() {
		t.Fatalf("ascii flag should be unset")
	}
	if out.Kind() != StrKind4Byte {
		t.Fatalf("kind %d want %d", out.Kind(), StrKind4Byte)
	}
	if out.Length() != 4 {
		t.Fatalf("len %d want 4", out.Length())
	}
}

func TestUnicodeWriterWriteStrReadonlyAlias(t *testing.T) {
	src := NewStr("aloha").(*Unicode)
	var w UnicodeWriter
	w.Init()
	if err := w.WriteStr(src); err != nil {
		t.Fatal(err)
	}
	out := w.Finish()
	if out != src {
		t.Fatalf("expected readonly alias to return src; got separate object")
	}
	if out.Value() != "aloha" {
		t.Fatalf("got %q", out.Value())
	}
}

func TestUnicodeWriterWriteStrConcat(t *testing.T) {
	var w UnicodeWriter
	w.Init()
	w.overallocate = true
	for _, s := range []string{"foo", "bar", "baz"} {
		if err := w.WriteStr(NewStr(s).(*Unicode)); err != nil {
			t.Fatal(err)
		}
	}
	out := w.Finish()
	if out.Value() != "foobarbaz" {
		t.Fatalf("got %q want foobarbaz", out.Value())
	}
	if out.Length() != 9 {
		t.Fatalf("len %d", out.Length())
	}
}

func TestUnicodeWriterWriteStrPromoteAfterAlias(t *testing.T) {
	a := NewStr("hello ").(*Unicode)
	b := NewStr("world").(*Unicode)
	var w UnicodeWriter
	w.Init()
	if err := w.WriteStr(a); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteStr(b); err != nil {
		t.Fatal(err)
	}
	out := w.Finish()
	if out.Value() != "hello world" {
		t.Fatalf("got %q want %q", out.Value(), "hello world")
	}
	if out == a || out == b {
		t.Fatalf("expected new buffer, not alias")
	}
}

func TestUnicodeWriterWriteSubstring(t *testing.T) {
	src := NewStr("alphabet").(*Unicode)
	var w UnicodeWriter
	w.Init()
	w.overallocate = true
	if err := w.WriteSubstring(src, 1, 5); err != nil {
		t.Fatal(err)
	}
	out := w.Finish()
	if out.Value() != "lpha" {
		t.Fatalf("got %q want lpha", out.Value())
	}
}

func TestUnicodeWriterWriteSubstringUnicode(t *testing.T) {
	src := NewStr("aéあ𝄞z").(*Unicode)
	var w UnicodeWriter
	w.Init()
	w.overallocate = true
	if err := w.WriteSubstring(src, 1, 4); err != nil {
		t.Fatal(err)
	}
	out := w.Finish()
	want := "éあ𝄞"
	if out.Value() != want {
		t.Fatalf("got %q want %q", out.Value(), want)
	}
	if out.Kind() != StrKind4Byte {
		t.Fatalf("kind %d want %d", out.Kind(), StrKind4Byte)
	}
}

func TestUnicodeWriterWriteASCIIString(t *testing.T) {
	var w UnicodeWriter
	w.Init()
	w.overallocate = true
	if err := w.WriteASCIIString("hello", -1); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteASCIIString(", world!", -1); err != nil {
		t.Fatal(err)
	}
	out := w.Finish()
	if out.Value() != "hello, world!" {
		t.Fatalf("got %q", out.Value())
	}
	if !out.IsASCII() {
		t.Fatalf("ascii should be set")
	}
}

func TestUnicodeWriterWriteASCIIStringRejectNonASCII(t *testing.T) {
	var w UnicodeWriter
	w.Init()
	w.overallocate = true
	if err := w.WriteASCIIString("\xff", -1); err == nil {
		t.Fatalf("expected SystemError for non-ASCII byte")
	}
}

func TestUnicodeWriterWriteLatin1String(t *testing.T) {
	var w UnicodeWriter
	w.Init()
	w.overallocate = true
	// 0xe9 is é in Latin-1 (codepoint U+00E9).
	if err := w.WriteLatin1String("h\xe9llo", -1); err != nil {
		t.Fatal(err)
	}
	out := w.Finish()
	want := "héllo"
	if out.Value() != want {
		t.Fatalf("got %q want %q", out.Value(), want)
	}
	if out.IsASCII() {
		t.Fatalf("ascii flag should be unset")
	}
	if out.Kind() != StrKind1Byte {
		t.Fatalf("kind %d want %d", out.Kind(), StrKind1Byte)
	}
}

func TestUnicodeWriterWriteCharRangeError(t *testing.T) {
	var w UnicodeWriter
	w.Init()
	if err := w.WriteChar(0x110000); err == nil {
		t.Fatalf("expected ValueError for codepoint out of range")
	}
}

func TestUnicodeWriterPrepareKindInternal(t *testing.T) {
	var w UnicodeWriter
	w.Init()
	if err := w.PrepareKindInternal(StrKind2Byte); err != nil {
		t.Fatal(err)
	}
	if w.maxchar < 0xffff {
		t.Fatalf("maxchar %x want >= 0xffff", w.maxchar)
	}
}

func TestUnicodeWriterCreateLargeBuffer(t *testing.T) {
	w, err := NewUnicodeWriter(1000)
	if err != nil {
		t.Fatal(err)
	}
	if !w.overallocate {
		t.Fatalf("Create should set overallocate")
	}
	for range 1000 {
		if err := w.WriteChar('x'); err != nil {
			t.Fatal(err)
		}
	}
	out := w.Finish()
	if out.Length() != 1000 {
		t.Fatalf("len %d", out.Length())
	}
	if out.Value() != strings.Repeat("x", 1000) {
		t.Fatalf("body mismatch")
	}
}

func TestUnicodeWriterInitWithBuffer(t *testing.T) {
	src := NewStr("seed").(*Unicode)
	var w UnicodeWriter
	w.InitWithBuffer(src)
	// minLength should be set to src.length so Finish never shrinks below.
	if w.minLength != src.length {
		t.Fatalf("minLength %d want %d", w.minLength, src.length)
	}
	// Pre-loaded: pos starts at length.
	if w.pos != src.length {
		t.Fatalf("pos %d want %d", w.pos, src.length)
	}
}

func TestUnicodeWriterFinishAfterAliasPlusGrow(t *testing.T) {
	a := NewStr("alias").(*Unicode)
	var w UnicodeWriter
	w.Init()
	if err := w.WriteStr(a); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteChar('!'); err != nil {
		t.Fatal(err)
	}
	out := w.Finish()
	if out.Value() != "alias!" {
		t.Fatalf("got %q want alias!", out.Value())
	}
	if out == a {
		t.Fatalf("expected a fresh str after growth, got alias")
	}
}
