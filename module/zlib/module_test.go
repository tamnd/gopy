package zlib

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

// TestCompressDecompressRoundtrip checks that compress followed by decompress
// returns the original data unchanged.
func TestCompressDecompressRoundtrip(t *testing.T) {
	payload := []byte("hello, zlib compression roundtrip test")

	compressed, err := zlibCompress([]objects.Object{objects.NewBytes(payload)}, nil)
	if err != nil {
		t.Fatalf("compress: %v", err)
	}

	cb, ok := compressed.(*objects.Bytes)
	if !ok {
		t.Fatalf("compress returned %T, want *objects.Bytes", compressed)
	}

	decompressed, err := zlibDecompress([]objects.Object{cb}, nil)
	if err != nil {
		t.Fatalf("decompress: %v", err)
	}

	db, ok := decompressed.(*objects.Bytes)
	if !ok {
		t.Fatalf("decompress returned %T, want *objects.Bytes", decompressed)
	}

	got := string(db.Bytes())
	want := string(payload)
	if got != want {
		t.Errorf("roundtrip mismatch: got %q, want %q", got, want)
	}
}

// TestCompressLevelBestSpeed checks that Z_BEST_SPEED level still produces
// valid output by decompressing it.
func TestCompressLevelBestSpeed(t *testing.T) {
	payload := []byte("speed level test")
	args := []objects.Object{objects.NewBytes(payload), objects.NewInt(int64(zBestSpeed))}
	compressed, err := zlibCompress(args, nil)
	if err != nil {
		t.Fatalf("compress(level=1): %v", err)
	}
	cb := compressed.(*objects.Bytes)
	decompressed, err := zlibDecompress([]objects.Object{cb}, nil)
	if err != nil {
		t.Fatalf("decompress: %v", err)
	}
	db := decompressed.(*objects.Bytes)
	if got := string(db.Bytes()); got != string(payload) {
		t.Errorf("roundtrip: got %q, want %q", got, string(payload))
	}
}

// TestCRC32KnownValue checks crc32 against well-known test vectors.
// crc32(b"") == 0, crc32(b"hello") == 907060870.
func TestCRC32KnownValue(t *testing.T) {
	// Empty input with seed 0 -> 0.
	res, err := zlibCRC32([]objects.Object{objects.NewBytes([]byte{})}, nil)
	if err != nil {
		t.Fatalf("crc32(b''): %v", err)
	}
	v, _ := res.(*objects.Int).Int64()
	if v != 0 {
		t.Errorf("crc32(b'') = %d, want 0", v)
	}

	// crc32(b"hello") == 907060870 (verified against CPython).
	res2, err := zlibCRC32([]objects.Object{objects.NewBytes([]byte("hello"))}, nil)
	if err != nil {
		t.Fatalf("crc32(b'hello'): %v", err)
	}
	v2, _ := res2.(*objects.Int).Int64()
	if v2 != 907060870 {
		t.Errorf("crc32(b'hello') = %d, want 907060870", v2)
	}
}

// TestCRC32Update checks that chained crc32 calls match a single call over
// the concatenated input.
func TestCRC32Update(t *testing.T) {
	part1 := []byte("hello")
	part2 := []byte(" world")

	res1, _ := zlibCRC32([]objects.Object{objects.NewBytes(part1)}, nil)
	v1, _ := res1.(*objects.Int).Int64()

	res2, err := zlibCRC32([]objects.Object{objects.NewBytes(part2), objects.NewInt(v1)}, nil)
	if err != nil {
		t.Fatalf("crc32 update: %v", err)
	}
	got, _ := res2.(*objects.Int).Int64()

	full := make([]byte, 0, len(part1)+len(part2))
	full = append(full, part1...)
	full = append(full, part2...)
	resAll, _ := zlibCRC32([]objects.Object{objects.NewBytes(full)}, nil)
	want, _ := resAll.(*objects.Int).Int64()

	if got != want {
		t.Errorf("chained crc32 = %d, want %d", got, want)
	}
}

// TestAdler32KnownValue checks adler32 against CPython-verified values.
// adler32(b"") == 1, adler32(b"hello") == 103547413.
func TestAdler32KnownValue(t *testing.T) {
	// Empty input -> 1 (initial s1=1, s2=0 -> result = 1).
	res, err := zlibAdler32([]objects.Object{objects.NewBytes([]byte{})}, nil)
	if err != nil {
		t.Fatalf("adler32(b''): %v", err)
	}
	v, _ := res.(*objects.Int).Int64()
	if v != 1 {
		t.Errorf("adler32(b'') = %d, want 1", v)
	}

	// adler32(b"hello") == 103547413 per CPython.
	res2, err := zlibAdler32([]objects.Object{objects.NewBytes([]byte("hello"))}, nil)
	if err != nil {
		t.Fatalf("adler32(b'hello'): %v", err)
	}
	v2, _ := res2.(*objects.Int).Int64()
	if v2 != 103547413 {
		t.Errorf("adler32(b'hello') = %d, want 103547413", v2)
	}
}

// TestCompressobjRoundtrip checks streaming compression via compressobj
// followed by one-shot decompress.
// The test feeds the full payload, calls flush(Z_FINISH), concatenates all
// chunks, and verifies that decompress recovers the original data.
func TestCompressobjRoundtrip(t *testing.T) {
	payload := []byte("streaming compressor test payload")

	obj, err := zlibCompressobj(nil, nil)
	if err != nil {
		t.Fatalf("compressobj(): %v", err)
	}
	co := obj.(*compressObj)

	// Feed the full payload; Go's zlib writer buffers internally so
	// compress() may return empty bytes.
	chunk1, err := compressObjCompress([]objects.Object{co, objects.NewBytes(payload)}, nil)
	if err != nil {
		t.Fatalf("compress(payload): %v", err)
	}

	// Z_FINISH closes the stream and flushes all remaining bytes.
	chunk2, err := compressObjFlush([]objects.Object{co, objects.NewInt(int64(zFinish))}, nil)
	if err != nil {
		t.Fatalf("flush(Z_FINISH): %v", err)
	}

	// Concatenate all output chunks to form the complete compressed stream.
	c1 := chunk1.(*objects.Bytes).Bytes()
	c2 := chunk2.(*objects.Bytes).Bytes()
	compressed := make([]byte, 0, len(c1)+len(c2))
	compressed = append(compressed, c1...)
	compressed = append(compressed, c2...)

	decompressed, err := zlibDecompress([]objects.Object{objects.NewBytes(compressed)}, nil)
	if err != nil {
		t.Fatalf("decompress: %v", err)
	}
	got := string(decompressed.(*objects.Bytes).Bytes())
	if got != string(payload) {
		t.Errorf("streaming roundtrip: got %q, want %q", got, string(payload))
	}
}

// TestDecompressobjRoundtrip checks that decompressobj handles data that
// was compressed with the one-shot compress function.
func TestDecompressobjRoundtrip(t *testing.T) {
	payload := []byte("decompressobj streaming test")

	compressed, err := zlibCompress([]objects.Object{objects.NewBytes(payload)}, nil)
	if err != nil {
		t.Fatalf("compress: %v", err)
	}

	dobj, err := zlibDecompressobj(nil, nil)
	if err != nil {
		t.Fatalf("decompressobj(): %v", err)
	}
	do := dobj.(*decompressObj)

	out, err := decompressObjDecompress([]objects.Object{do, compressed}, nil)
	if err != nil {
		t.Fatalf("decompressobj.decompress: %v", err)
	}
	got := string(out.(*objects.Bytes).Bytes())
	if got != string(payload) {
		t.Errorf("decompressobj roundtrip: got %q, want %q", got, string(payload))
	}
	if !do.eof {
		t.Errorf("decompressObj.eof should be true after full stream")
	}
}

// TestBuildModule checks that buildModule returns a non-nil module with the
// expected constant entries.
func TestBuildModule(t *testing.T) {
	m, err := buildModule()
	if err != nil {
		t.Fatalf("buildModule: %v", err)
	}
	if m == nil {
		t.Fatal("buildModule returned nil")
	}
	d := m.Dict()
	for _, key := range []string{
		"compress", "decompress", "crc32", "adler32",
		"compressobj", "decompressobj",
		"Z_DEFAULT_COMPRESSION", "MAX_WBITS", "DEF_BUF_SIZE",
		"ZLIB_VERSION", "ZLIB_RUNTIME_VERSION",
	} {
		v, err := d.GetItem(objects.NewStr(key))
		if err != nil || v == nil {
			t.Errorf("module missing key %q", key)
		}
	}
}
