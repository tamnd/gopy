package _codecs

import (
	"bytes"
	"testing"

	"github.com/tamnd/gopy/objects"
)

// TestCharmapDecodeLatin1Default mirrors CPython's PyUnicode_DecodeCharmap
// when mapping is None: behaves like latin-1.
//
// CPython: Objects/unicodeobject.c:8625 (mapping == NULL fallback)
func TestCharmapDecodeLatin1Default(t *testing.T) {
	res, err := charmapDecode([]objects.Object{
		objects.NewBytes([]byte{0x48, 0x69, 0xE9}), // "Hié" in latin-1
	}, nil)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	tup := res.(*objects.Tuple)
	got := tup.Item(0).(*objects.Unicode).Value()
	if got != "Hié" {
		t.Errorf("got %q, want %q", got, "Hié")
	}
}

// TestCharmapDecodeStringTable: 256-char table mapping.
//
// CPython: Objects/unicodeobject.c:8398 charmap_decode_string
func TestCharmapDecodeStringTable(t *testing.T) {
	// Build a 256-char table where every byte maps to itself + 0x100.
	tbl := make([]rune, 256)
	for i := range tbl {
		tbl[i] = rune(i + 0x100)
	}
	table := objects.NewStr(string(tbl))
	res, err := charmapDecode([]objects.Object{
		objects.NewBytes([]byte{0x41, 0x42}),
		objects.None(),
		table,
	}, nil)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	tup := res.(*objects.Tuple)
	got := tup.Item(0).(*objects.Unicode).Value()
	if got != "Łł" {
		t.Errorf("got %q, want %q", got, "Łł")
	}
}

// TestCharmapDecodeStringTableUndefined: 0xFFFE in table raises through
// strict error handler.
//
// CPython: Objects/unicodeobject.c:8484 (x == 0xFFFE branch)
func TestCharmapDecodeStringTableUndefined(t *testing.T) {
	tbl := make([]rune, 256)
	for i := range tbl {
		tbl[i] = 0xFFFE
	}
	table := objects.NewStr(string(tbl))
	_, err := charmapDecode([]objects.Object{
		objects.NewBytes([]byte{0x41}),
		objects.NewStr("strict"),
		table,
	}, nil)
	if err == nil {
		t.Fatal("expected error for undefined mapping under strict")
	}
}

// TestCharmapDecodeMapping uses a dict mapping byte -> int / str / None.
//
// CPython: Objects/unicodeobject.c:8513 charmap_decode_mapping
func TestCharmapDecodeMapping(t *testing.T) {
	m := objects.NewDict()
	_ = m.SetItem(objects.NewInt(0x41), objects.NewInt(0x2660)) // A -> ♠
	_ = m.SetItem(objects.NewInt(0x42), objects.NewStr("xy"))   // B -> "xy"
	// 0x43 unmapped -> ignore
	res, err := charmapDecode([]objects.Object{
		objects.NewBytes([]byte{0x41, 0x42, 0x43}),
		objects.NewStr("ignore"),
		m,
	}, nil)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	got := res.(*objects.Tuple).Item(0).(*objects.Unicode).Value()
	if got != "♠xy" {
		t.Errorf("got %q, want %q", got, "♠xy")
	}
}

// TestCharmapEncodeLatin1Default: mapping None -> latin-1.
//
// CPython: Objects/unicodeobject.c:9136 (mapping == NULL fallback)
func TestCharmapEncodeLatin1Default(t *testing.T) {
	res, err := charmapEncode([]objects.Object{
		objects.NewStr("Hié"),
	}, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got := res.(*objects.Tuple).Item(0).(*objects.Bytes).Bytes()
	want := []byte{0x48, 0x69, 0xE9}
	if !bytes.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestCharmapEncodeStringTable: encoding via inverted 256-char table.
//
// CPython: Objects/unicodeobject.c:8838 charmapencode_lookup (string path)
func TestCharmapEncodeStringTable(t *testing.T) {
	// Identity table: byte i -> rune i. Encoding "Hi" should yield "Hi".
	tbl := make([]rune, 256)
	for i := range tbl {
		tbl[i] = rune(i)
	}
	table := objects.NewStr(string(tbl))
	res, err := charmapEncode([]objects.Object{
		objects.NewStr("Hi"),
		objects.None(),
		table,
	}, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got := res.(*objects.Tuple).Item(0).(*objects.Bytes).Bytes()
	if string(got) != "Hi" {
		t.Errorf("got %q, want %q", got, "Hi")
	}
}

// TestCharmapEncodeMappingDictBytes: dict mapping codepoint -> bytes.
//
// CPython: Objects/unicodeobject.c:8880 (PyBytes_Check branch in charmapencode_lookup)
func TestCharmapEncodeMappingDictBytes(t *testing.T) {
	m := objects.NewDict()
	_ = m.SetItem(objects.NewInt(int64('A')), objects.NewBytes([]byte{0x80, 0x81}))
	_ = m.SetItem(objects.NewInt(int64('B')), objects.NewInt(0x42))
	res, err := charmapEncode([]objects.Object{
		objects.NewStr("AB"),
		objects.None(),
		m,
	}, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got := res.(*objects.Tuple).Item(0).(*objects.Bytes).Bytes()
	want := []byte{0x80, 0x81, 0x42}
	if !bytes.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestCharmapEncodeStrictRaises: unmappable char under strict.
//
// CPython: Objects/unicodeobject.c:9026 (_Py_ERROR_STRICT)
func TestCharmapEncodeStrictRaises(t *testing.T) {
	m := objects.NewDict()
	_, err := charmapEncode([]objects.Object{
		objects.NewStr("X"),
		objects.NewStr("strict"),
		m,
	}, nil)
	if err == nil {
		t.Fatal("expected error for unmappable char under strict")
	}
}

// TestCharmapEncodeReplace: '?' replacement for unmappable chars.
//
// CPython: Objects/unicodeobject.c:9030 (_Py_ERROR_REPLACE)
func TestCharmapEncodeReplace(t *testing.T) {
	m := objects.NewDict()
	_ = m.SetItem(objects.NewInt(int64('?')), objects.NewInt(int64('?')))
	res, err := charmapEncode([]objects.Object{
		objects.NewStr("X"),
		objects.NewStr("replace"),
		m,
	}, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got := res.(*objects.Tuple).Item(0).(*objects.Bytes).Bytes()
	if string(got) != "?" {
		t.Errorf("got %q, want %q", got, "?")
	}
}

// TestCharmapBuildBasic: build a mapping dict from a 256-char string.
//
// CPython: Objects/unicodeobject.c:8694 PyUnicode_BuildEncodingMap
func TestCharmapBuildBasic(t *testing.T) {
	res, err := charmapBuild([]objects.Object{objects.NewStr("ABC")}, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	d := res.(*objects.Dict)
	for i, c := range []rune("ABC") {
		got, err := d.GetItem(objects.NewInt(int64(c)))
		if err != nil {
			t.Fatalf("missing key %q: %v", c, err)
		}
		iv, _ := got.(*objects.Int).Int64()
		if iv != int64(i) {
			t.Errorf("[%q] = %d, want %d", c, iv, i)
		}
	}
}

// TestCharmapRoundTrip: build -> encode -> decode round-trips ASCII.
func TestCharmapRoundTrip(t *testing.T) {
	tbl := make([]rune, 256)
	for i := range tbl {
		tbl[i] = rune(i)
	}
	table := objects.NewStr(string(tbl))
	mapped, err := charmapBuild([]objects.Object{table}, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	enc, err := charmapEncode([]objects.Object{
		objects.NewStr("Hello"),
		objects.None(),
		mapped,
	}, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	encBytes := enc.(*objects.Tuple).Item(0).(*objects.Bytes)
	dec, err := charmapDecode([]objects.Object{
		encBytes,
		objects.None(),
		table,
	}, nil)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	got := dec.(*objects.Tuple).Item(0).(*objects.Unicode).Value()
	if got != "Hello" {
		t.Errorf("round-trip got %q, want %q", got, "Hello")
	}
}
