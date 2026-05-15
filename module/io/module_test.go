// Tests for the _io module types: BytesIO, FileIO, and TextIOWrapper.
// Each group of tests covers construction, read, write, seek, tell,
// truncate, close, and the capability predicates.

package io

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamnd/gopy/objects"
)

// ---- BytesIO tests --------------------------------------------------------

func TestBytesIOBasicReadWrite(t *testing.T) {
	// CPython: Modules/_io/bytesio.c _io_BytesIO_write_impl / _io_BytesIO_read_impl
	b := NewBytesIO(nil)
	n := b.Write([]byte("hello"))
	if n != 5 {
		t.Fatalf("Write returned %d, want 5", n)
	}
	if _, err := b.Seek(0, 0); err != nil {
		t.Fatalf("Seek: %v", err)
	}
	got := b.Read(5)
	if string(got) != "hello" {
		t.Fatalf("Read = %q, want %q", got, "hello")
	}
}

func TestBytesIOGetValue(t *testing.T) {
	// CPython: Modules/_io/bytesio.c _io_BytesIO_getvalue_impl
	b := NewBytesIO([]byte("abc"))
	b.Write([]byte("xyz"))
	if _, err := b.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	val := b.GetValue()
	if string(val) != "xyz" {
		t.Fatalf("GetValue = %q, want %q", val, "xyz")
	}
}

func TestBytesIOInitialBytes(t *testing.T) {
	b := NewBytesIO([]byte("init"))
	val := b.GetValue()
	if string(val) != "init" {
		t.Fatalf("initial bytes = %q, want %q", val, "init")
	}
}

func TestBytesIOReadline(t *testing.T) {
	// CPython: Modules/_io/bytesio.c:80 scan_eol_lock_held
	b := NewBytesIO([]byte("line1\nline2\n"))
	line := b.readline(-1)
	if string(line) != "line1\n" {
		t.Fatalf("readline = %q, want %q", line, "line1\n")
	}
	line = b.readline(-1)
	if string(line) != "line2\n" {
		t.Fatalf("readline = %q, want %q", line, "line2\n")
	}
	line = b.readline(-1)
	if len(line) != 0 {
		t.Fatalf("readline at EOF = %q, want empty", line)
	}
}

func TestBytesIOSeek(t *testing.T) {
	// CPython: Modules/_io/bytesio.c _io_BytesIO_seek_impl
	b := NewBytesIO([]byte("abcdef"))
	if _, err := b.Seek(2, 0); err != nil {
		t.Fatal(err)
	}
	if b.Tell() != 2 {
		t.Fatalf("Tell after seek = %d, want 2", b.Tell())
	}
	// seek relative
	if _, err := b.Seek(1, 1); err != nil {
		t.Fatal(err)
	}
	if b.Tell() != 3 {
		t.Fatalf("Tell after rel seek = %d, want 3", b.Tell())
	}
	// seek from end
	if _, err := b.Seek(-1, 2); err != nil {
		t.Fatal(err)
	}
	if b.Tell() != 5 {
		t.Fatalf("Tell after end seek = %d, want 5", b.Tell())
	}
}

func TestBytesIOTruncate(t *testing.T) {
	// CPython: Modules/_io/bytesio.c _io_BytesIO_truncate_impl
	b := NewBytesIO([]byte("hello world"))
	if _, err := b.Truncate(5); err != nil {
		t.Fatal(err)
	}
	if string(b.GetValue()) != "hello" {
		t.Fatalf("after truncate = %q, want %q", b.GetValue(), "hello")
	}
}

func TestBytesIOClose(t *testing.T) {
	b := NewBytesIO(nil)
	b.Close()
	if !b.closed {
		t.Fatal("closed flag not set")
	}
	if err := b.checkUsable(); err == nil {
		t.Fatal("checkUsable on closed should error")
	}
}

func TestBytesIOCall(t *testing.T) {
	// Exercise the type-call slot with initial_bytes kwarg.
	obj, err := bytesIOCall(nil, nil, map[string]objects.Object{
		"initial_bytes": objects.NewBytes([]byte("kw")),
	})
	if err != nil {
		t.Fatalf("bytesIOCall: %v", err)
	}
	b := obj.(*BytesIO)
	if string(b.GetValue()) != "kw" {
		t.Fatalf("initial_bytes via kwarg = %q, want %q", b.GetValue(), "kw")
	}
}

func TestBytesIOGetattr(t *testing.T) {
	b := NewBytesIO([]byte("data"))
	// closed attribute
	attr, err := bytesIOGetattr(b, objects.NewStr("closed"))
	if err != nil {
		t.Fatalf("getattr closed: %v", err)
	}
	if attr == objects.True() {
		t.Fatal("closed should be false")
	}
	// getvalue method
	attr, err = bytesIOGetattr(b, objects.NewStr("getvalue"))
	if err != nil {
		t.Fatalf("getattr getvalue: %v", err)
	}
	fn := attr.(*objects.BuiltinFunction)
	result, err := fn.Fn(nil, nil)
	if err != nil {
		t.Fatalf("getvalue(): %v", err)
	}
	if string(result.(*objects.Bytes).Bytes()) != "data" {
		t.Fatalf("getvalue = %q, want %q", result.(*objects.Bytes).Bytes(), "data")
	}
}

func TestBytesIOWriteMethod(t *testing.T) {
	b := NewBytesIO(nil)
	fn := bytesIOMethod(b, "write")
	if fn == nil {
		t.Fatal("write method not found")
	}
	bf := fn.(*objects.BuiltinFunction)
	_, err := bf.Fn([]objects.Object{objects.NewBytes([]byte("go"))}, nil)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if string(b.GetValue()) != "go" {
		t.Fatalf("after write = %q, want %q", b.GetValue(), "go")
	}
}

// ---- FileIO tests ---------------------------------------------------------

func TestFileIOReadText(t *testing.T) {
	// CPython: Modules/_io/fileio.c:706 fileio_read
	dir := t.TempDir()
	path := filepath.Join(dir, "r.txt")
	if err := os.WriteFile(path, []byte("filedata"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	fi := NewFileIO(f, path, "r", true, false)
	defer fi.Close()
	data, err := fi.Read(-1)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(data) != "filedata" {
		t.Fatalf("Read = %q, want %q", data, "filedata")
	}
}

func TestFileIOWrite(t *testing.T) {
	// CPython: Modules/_io/fileio.c:887 fileio_write
	dir := t.TempDir()
	path := filepath.Join(dir, "w.bin")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	fi := NewFileIO(f, path, "w", false, true)
	n, err := fi.Write([]byte("written"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != 7 {
		t.Fatalf("Write returned %d, want 7", n)
	}
	fi.Close()
	disk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(disk) != "written" {
		t.Fatalf("on disk = %q, want %q", disk, "written")
	}
}

func TestFileIOSeekTell(t *testing.T) {
	// CPython: Modules/_io/fileio.c:950 fileio_seek / :1001 fileio_tell
	dir := t.TempDir()
	path := filepath.Join(dir, "st.bin")
	if err := os.WriteFile(path, []byte("abcdef"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	fi := NewFileIO(f, path, "r", true, false)
	defer fi.Close()
	if _, err := fi.Seek(2, 0); err != nil {
		t.Fatalf("Seek: %v", err)
	}
	pos, err := fi.Tell()
	if err != nil {
		t.Fatalf("Tell: %v", err)
	}
	if pos != 2 {
		t.Fatalf("Tell = %d, want 2", pos)
	}
	data, err := fi.Read(3)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(data) != "cde" {
		t.Fatalf("Read after seek = %q, want %q", data, "cde")
	}
}

func TestFileIOClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.txt")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	fi := NewFileIO(f, path, "r", true, false)
	if err := fi.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !fi.closed {
		t.Fatal("closed flag not set")
	}
}

func TestFileIOCall(t *testing.T) {
	// Exercise the type-call slot.
	dir := t.TempDir()
	path := filepath.Join(dir, "fc.txt")
	if err := os.WriteFile(path, []byte("call"), 0o600); err != nil {
		t.Fatal(err)
	}
	obj, err := fileIOCall(nil, []objects.Object{objects.NewStr(path)}, nil)
	if err != nil {
		t.Fatalf("fileIOCall: %v", err)
	}
	fi := obj.(*FileIO)
	defer fi.Close()
	data, err := fi.Read(-1)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(data) != "call" {
		t.Fatalf("Read = %q, want %q", data, "call")
	}
}

func TestFileIOGetattr(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ga.txt")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	fi := NewFileIO(f, path, "r", true, false)
	defer fi.Close()

	attr, err := fileIOGetattr(fi, objects.NewStr("name"))
	if err != nil {
		t.Fatalf("getattr name: %v", err)
	}
	if attr.(*objects.Unicode).Value() != path {
		t.Fatalf("name = %q, want %q", attr.(*objects.Unicode).Value(), path)
	}

	attr, err = fileIOGetattr(fi, objects.NewStr("closed"))
	if err != nil {
		t.Fatalf("getattr closed: %v", err)
	}
	if attr == objects.True() {
		t.Fatal("closed should be false")
	}
}

func TestFileIONotReadableError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nr.bin")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	fi := NewFileIO(f, path, "w", false, true)
	defer fi.Close()
	_, err = fi.Read(1)
	if err == nil || !strings.Contains(err.Error(), "not open for reading") {
		t.Fatalf("err = %v, want not-open-for-reading", err)
	}
}

// ---- TextIOWrapper tests --------------------------------------------------

func TestTextIOWrapperRead(t *testing.T) {
	// CPython: Modules/_io/textio.c:1804 textiowrapper_read_chunk
	dir := t.TempDir()
	path := filepath.Join(dir, "t.txt")
	if err := os.WriteFile(path, []byte("hello\nworld\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	fi := NewFileIO(f, path, "r", true, false)
	tw := NewTextIOWrapper(fi, "utf-8", "strict", path, "r")
	defer tw.Close()

	got, err := tw.Read(-1)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got != "hello\nworld\n" {
		t.Fatalf("Read = %q, want %q", got, "hello\nworld\n")
	}
}

func TestTextIOWrapperReadline(t *testing.T) {
	// CPython: Modules/_io/textio.c:1885 textiowrapper_readline
	dir := t.TempDir()
	path := filepath.Join(dir, "rl.txt")
	if err := os.WriteFile(path, []byte("line1\nline2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	fi := NewFileIO(f, path, "r", true, false)
	tw := NewTextIOWrapper(fi, "utf-8", "strict", path, "r")
	defer tw.Close()

	line, err := tw.Readline(-1)
	if err != nil {
		t.Fatalf("Readline: %v", err)
	}
	if line != "line1\n" {
		t.Fatalf("Readline = %q, want %q", line, "line1\n")
	}
}

func TestTextIOWrapperWrite(t *testing.T) {
	// CPython: Modules/_io/textio.c:1499 textiowrapper_write
	dir := t.TempDir()
	path := filepath.Join(dir, "tw.txt")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	fi := NewFileIO(f, path, "w", false, true)
	tw := NewTextIOWrapper(fi, "utf-8", "strict", path, "w")

	if _, err := tw.Write("test content"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	disk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(disk) != "test content" {
		t.Fatalf("on disk = %q, want %q", disk, "test content")
	}
}

// TestTextIOWrapperWriteflushBatching pins the pending_bytes batching
// behaviour ported from CPython textio.c _textiowrapper_writeflush.
// Many sub-chunkSize writes must coalesce: the on-disk size stays at 0
// until either Flush, Close, or the pending_bytes_count crosses
// chunk_size. line_buffering off and write_through off here so neither
// fast-path forces an early drain.
//
// CPython: Modules/_io/textio.c:1583 _textiowrapper_writeflush
func TestTextIOWrapperWriteflushBatching(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "twb.txt")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	fi := NewFileIO(f, path, "w", false, true)
	tw := NewTextIOWrapper(fi, "utf-8", "strict", path, "w")

	for range 10 {
		if _, err := tw.Write("abc"); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	// FileIO writes straight to the OS file. The pending_bytes slab is
	// still held inside tw, so nothing has hit the file yet.
	if info, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if info.Size() != 0 {
		t.Fatalf("pre-flush size = %d, want 0 (data should be batched)", info.Size())
	}

	if err := tw.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	disk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(disk) != "abcabcabcabcabcabcabcabcabcabc" {
		t.Fatalf("on disk = %q, want 30 bytes of repeated 'abc'", disk)
	}
}

func TestTextIOWrapperGetattr(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tga.txt")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	fi := NewFileIO(f, path, "r", true, false)
	tw := NewTextIOWrapper(fi, "utf-8", "strict", path, "r")
	defer tw.Close()

	for _, attrName := range []string{"name", "mode", "encoding", "errors", "closed"} {
		attr, err := textIOWrapperGetattr(tw, objects.NewStr(attrName))
		if err != nil {
			t.Fatalf("getattr %q: %v", attrName, err)
		}
		if attr == nil {
			t.Fatalf("getattr %q returned nil", attrName)
		}
	}
}

func TestTextIOWrapperClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tcl.txt")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	fi := NewFileIO(f, path, "r", true, false)
	tw := NewTextIOWrapper(fi, "utf-8", "strict", path, "r")
	if err := tw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !tw.closed {
		t.Fatal("closed flag not set")
	}
}

// ---- _io.Open round-trip --------------------------------------------------

func TestIOOpenTextRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rt.txt")

	// Write via _io.Open text mode.
	wobj, err := Open([]objects.Object{objects.NewStr(path), objects.NewStr("w")}, nil)
	if err != nil {
		t.Fatalf("open(w): %v", err)
	}
	tw := wobj.(*TextIOWrapper)
	if _, err := tw.Write("round-trip"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("Close(w): %v", err)
	}

	// Read it back.
	robj, err := Open([]objects.Object{objects.NewStr(path)}, nil)
	if err != nil {
		t.Fatalf("open(r): %v", err)
	}
	tr := robj.(*TextIOWrapper)
	defer tr.Close()
	got, err := tr.Read(-1)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got != "round-trip" {
		t.Fatalf("Read = %q, want %q", got, "round-trip")
	}
}

func TestIOOpenBinaryRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rb.bin")

	call := func(o objects.Object, name string, args []objects.Object) (objects.Object, error) {
		fn, err := objects.GetAttr(o, objects.NewStr(name))
		if err != nil {
			return nil, err
		}
		return objects.Call(fn, objects.NewTuple(args), nil)
	}

	// Write via _io.Open binary mode (returns a BufferedWriter wrapping FileIO).
	wobj, err := Open([]objects.Object{objects.NewStr(path), objects.NewStr("wb")}, nil)
	if err != nil {
		t.Fatalf("open(wb): %v", err)
	}
	if _, err := call(wobj, "write", []objects.Object{objects.NewBytes([]byte{1, 2, 3})}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := call(wobj, "close", nil); err != nil {
		t.Fatalf("Close(wb): %v", err)
	}

	// Read it back (returns a BufferedReader).
	robj, err := Open([]objects.Object{objects.NewStr(path), objects.NewStr("rb")}, nil)
	if err != nil {
		t.Fatalf("open(rb): %v", err)
	}
	defer call(robj, "close", nil)
	got, err := call(robj, "read", []objects.Object{objects.NewInt(-1)})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	b, ok := got.(*objects.Bytes)
	if !ok {
		t.Fatalf("read returned %T, want bytes", got)
	}
	if string(b.Bytes()) != "\x01\x02\x03" {
		t.Fatalf("Read = %q, want %q", b.Bytes(), "\x01\x02\x03")
	}
}

func TestIOOpenInvalidMode(t *testing.T) {
	_, err := Open([]objects.Object{objects.NewStr("/tmp/x"), objects.NewStr("z")}, nil)
	if err == nil || !strings.Contains(err.Error(), "ValueError: invalid mode") {
		t.Fatalf("err = %v, want invalid-mode ValueError", err)
	}
}

func TestIOOpenBinaryWithEncoding(t *testing.T) {
	_, err := Open([]objects.Object{
		objects.NewStr("/tmp/x"),
		objects.NewStr("rb"),
	}, map[string]objects.Object{
		"encoding": objects.NewStr("utf-8"),
	})
	if err == nil || !strings.Contains(err.Error(), "binary mode doesn't take an encoding") {
		t.Fatalf("err = %v, want encoding-in-binary-mode error", err)
	}
}
