package _codecs

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

// TestEncodeUTF8 checks that encode("hello", "utf-8") returns b"hello".
func TestEncodeUTF8(t *testing.T) {
	args := []objects.Object{
		objects.NewStr("hello"),
		objects.NewStr("utf-8"),
	}
	got, err := codecsEncode(args, nil)
	if err != nil {
		t.Fatalf("encode: unexpected error: %v", err)
	}
	b, ok := got.(*objects.Bytes)
	if !ok {
		t.Fatalf("encode: expected bytes, got %T", got)
	}
	if string(b.Bytes()) != "hello" {
		t.Fatalf("encode: got %q, want %q", b.Bytes(), "hello")
	}
}

// TestDecodeUTF8 checks that decode(b"hello", "utf-8") returns "hello".
func TestDecodeUTF8(t *testing.T) {
	args := []objects.Object{
		objects.NewBytes([]byte("hello")),
		objects.NewStr("utf-8"),
	}
	got, err := codecsDecode(args, nil)
	if err != nil {
		t.Fatalf("decode: unexpected error: %v", err)
	}
	s, ok := got.(*objects.Unicode)
	if !ok {
		t.Fatalf("decode: expected str, got %T", got)
	}
	if s.Value() != "hello" {
		t.Fatalf("decode: got %q, want %q", s.Value(), "hello")
	}
}

// TestUTF8EncodeDecode checks the per-codec helpers return (bytes, length) / (str, length).
func TestUTF8EncodeDecode(t *testing.T) {
	encArgs := []objects.Object{objects.NewStr("hi")}
	encOut, err := utf8Encode(encArgs, nil)
	if err != nil {
		t.Fatalf("utf_8_encode: %v", err)
	}
	tup, ok := encOut.(*objects.Tuple)
	if !ok || tup.Len() != 2 {
		t.Fatalf("utf_8_encode: expected 2-tuple, got %T", encOut)
	}
	b := tup.Item(0).(*objects.Bytes)
	if string(b.Bytes()) != "hi" {
		t.Fatalf("utf_8_encode: bytes = %q, want %q", b.Bytes(), "hi")
	}

	decArgs := []objects.Object{b}
	decOut, err := utf8Decode(decArgs, nil)
	if err != nil {
		t.Fatalf("utf_8_decode: %v", err)
	}
	tup2, ok := decOut.(*objects.Tuple)
	if !ok || tup2.Len() != 2 {
		t.Fatalf("utf_8_decode: expected 2-tuple, got %T", decOut)
	}
	s := tup2.Item(0).(*objects.Unicode)
	if s.Value() != "hi" {
		t.Fatalf("utf_8_decode: str = %q, want %q", s.Value(), "hi")
	}
}

// TestASCIIEncodeDecode checks ASCII encode/decode helpers.
func TestASCIIEncodeDecode(t *testing.T) {
	out, err := asciiEncode([]objects.Object{objects.NewStr("abc")}, nil)
	if err != nil {
		t.Fatalf("ascii_encode: %v", err)
	}
	tup := out.(*objects.Tuple)
	b := tup.Item(0).(*objects.Bytes)
	if string(b.Bytes()) != "abc" {
		t.Fatalf("ascii_encode: %q != %q", b.Bytes(), "abc")
	}

	out2, err := asciiDecode([]objects.Object{b}, nil)
	if err != nil {
		t.Fatalf("ascii_decode: %v", err)
	}
	tup2 := out2.(*objects.Tuple)
	s := tup2.Item(0).(*objects.Unicode)
	if s.Value() != "abc" {
		t.Fatalf("ascii_decode: %q != %q", s.Value(), "abc")
	}
}

// TestLatin1EncodeDecode checks Latin-1 encode/decode helpers.
func TestLatin1EncodeDecode(t *testing.T) {
	out, err := latin1Encode([]objects.Object{objects.NewStr("café")}, nil)
	if err != nil {
		t.Fatalf("latin_1_encode: %v", err)
	}
	tup := out.(*objects.Tuple)
	b := tup.Item(0).(*objects.Bytes)
	if b.Bytes()[3] != 0xe9 {
		t.Fatalf("latin_1_encode: byte[3] = %02x, want e9", b.Bytes()[3])
	}

	out2, err := latin1Decode([]objects.Object{b}, nil)
	if err != nil {
		t.Fatalf("latin_1_decode: %v", err)
	}
	tup2 := out2.(*objects.Tuple)
	s := tup2.Item(0).(*objects.Unicode)
	if s.Value() != "café" {
		t.Fatalf("latin_1_decode: %q != %q", s.Value(), "café")
	}
}

// TestLookup checks that lookup("utf-8") returns a CodecInfo instance.
func TestLookup(t *testing.T) {
	out, err := codecsLookup([]objects.Object{objects.NewStr("utf-8")}, nil)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if out == nil {
		t.Fatal("lookup: returned nil")
	}
}

// TestRegisterError checks that register_error and lookup_error round-trip.
func TestRegisterError(t *testing.T) {
	handler := objects.NewBuiltinFunction("myhandler", func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
		return objects.NewTuple([]objects.Object{objects.NewStr("?"), objects.NewInt(1)}), nil
	})
	_, err := codecsRegisterError([]objects.Object{objects.NewStr("myhandler"), handler}, nil)
	if err != nil {
		t.Fatalf("register_error: %v", err)
	}
	got, err := codecsLookupError([]objects.Object{objects.NewStr("myhandler")}, nil)
	if err != nil {
		t.Fatalf("lookup_error: %v", err)
	}
	if got == nil {
		t.Fatal("lookup_error: returned nil")
	}
}

// TestBuildModule checks the module is constructed without errors.
func TestBuildModule(t *testing.T) {
	m, err := buildModule()
	if err != nil {
		t.Fatalf("buildModule: %v", err)
	}
	if m == nil {
		t.Fatal("buildModule: returned nil")
	}
}
