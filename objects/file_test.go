// Coverage for the File type. The open() builtin tests live next door
// in builtins/open_test.go; this file pins the read / write / readline
// / iteration / close / attribute behavior on a File constructed
// directly via NewFile.

package objects

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeReadFile writes contents to a tempfile and returns it opened
// for reading via NewFile. binary toggles between bytes and str
// surfaces.
func makeReadFile(t *testing.T, binary bool, contents string) *File {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "in.txt")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	mode := "r"
	if binary {
		mode = "rb"
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("os.Open: %v", err)
	}
	return NewFile(f, path, mode, binary, true, false)
}

// makeWriteFile opens a fresh tempfile for writing.
func makeWriteFile(t *testing.T, binary bool) (*File, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	mode := "w"
	if binary {
		mode = "wb"
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatalf("os.OpenFile: %v", err)
	}
	return NewFile(f, path, mode, binary, false, true), path
}

func TestFileReadAllText(t *testing.T) {
	fi := makeReadFile(t, false, "hello world\n")
	out, err := fi.Read(-1)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	got, ok := out.(*Unicode)
	if !ok {
		t.Fatalf("Read returned %T, want *Unicode", out)
	}
	if got.Value() != "hello world\n" {
		t.Fatalf("Read = %q", got.Value())
	}
}

func TestFileReadAllBinary(t *testing.T) {
	fi := makeReadFile(t, true, "\x00\x01\x02hi")
	out, err := fi.Read(-1)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	got, ok := out.(*Bytes)
	if !ok {
		t.Fatalf("Read returned %T, want *Bytes", out)
	}
	if string(got.Bytes()) != "\x00\x01\x02hi" {
		t.Fatalf("Read = %q", got.Bytes())
	}
}

func TestFileReadSizedShort(t *testing.T) {
	fi := makeReadFile(t, false, "abcdef")
	out, err := fi.Read(3)
	if err != nil {
		t.Fatalf("Read(3): %v", err)
	}
	if out.(*Unicode).Value() != "abc" {
		t.Fatalf("Read(3) = %q, want abc", out.(*Unicode).Value())
	}
	out, err = fi.Read(10)
	if err != nil {
		t.Fatalf("Read(10): %v", err)
	}
	if out.(*Unicode).Value() != "def" {
		t.Fatalf("Read(10) = %q, want def (clipped at EOF)", out.(*Unicode).Value())
	}
}

func TestFileReadlineYieldsLines(t *testing.T) {
	fi := makeReadFile(t, false, "first\nsecond\nthird\n")
	for _, want := range []string{"first\n", "second\n", "third\n", ""} {
		got, err := fi.Readline(-1)
		if err != nil {
			t.Fatalf("Readline: %v", err)
		}
		if got.(*Unicode).Value() != want {
			t.Fatalf("Readline = %q, want %q", got.(*Unicode).Value(), want)
		}
	}
}

func TestFileIteratorYieldsLinesUntilEOF(t *testing.T) {
	fi := makeReadFile(t, false, "a\nb\n")
	it, err := FileType.Iter(fi)
	if err != nil {
		t.Fatalf("Iter: %v", err)
	}
	if it != fi {
		t.Fatalf("Iter returned %v, want self", it)
	}
	var got []string
	for {
		next, err := FileType.IterNext(fi)
		if errors.Is(err, ErrStopIteration) {
			break
		}
		if err != nil {
			t.Fatalf("IterNext: %v", err)
		}
		got = append(got, next.(*Unicode).Value())
	}
	if strings.Join(got, "|") != "a\n|b\n" {
		t.Fatalf("iteration produced %q", got)
	}
}

func TestFileWriteAndClose(t *testing.T) {
	fi, path := makeWriteFile(t, false)
	n, err := fi.Write(NewStr("hello"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != 5 {
		t.Fatalf("Write returned %d, want 5", n)
	}
	if err := fi.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(out) != "hello" {
		t.Fatalf("on disk = %q", out)
	}
}

func TestFileWriteBinaryRejectsStr(t *testing.T) {
	fi, _ := makeWriteFile(t, true)
	defer fi.Close()
	if _, err := fi.Write(NewStr("hello")); err == nil {
		t.Fatalf("Write(str) on binary file accepted")
	}
}

func TestFileWriteTextRejectsBytes(t *testing.T) {
	fi, _ := makeWriteFile(t, false)
	defer fi.Close()
	if _, err := fi.Write(NewBytes([]byte("hello"))); err == nil {
		t.Fatalf("Write(bytes) on text file accepted")
	}
}

func TestFileReadOnClosedFails(t *testing.T) {
	fi := makeReadFile(t, false, "x")
	fi.Close()
	if _, err := fi.Read(1); err == nil {
		t.Fatalf("Read on closed file returned nil error")
	}
}

func TestFileAttributes(t *testing.T) {
	fi := makeReadFile(t, false, "hi")
	cases := []struct {
		name string
		want string
	}{
		{"name", fi.name},
		{"mode", "r"},
	}
	for _, c := range cases {
		out, err := fileGetattr(fi, NewStr(c.name))
		if err != nil {
			t.Fatalf("getattr %q: %v", c.name, err)
		}
		if out.(*Unicode).Value() != c.want {
			t.Fatalf("getattr %q = %q, want %q", c.name, out.(*Unicode).Value(), c.want)
		}
	}
	out, err := fileGetattr(fi, NewStr("closed"))
	if err != nil {
		t.Fatalf("getattr closed: %v", err)
	}
	if out != False() {
		t.Fatalf("closed = %v on fresh file, want False", out)
	}
	fi.Close()
	out, _ = fileGetattr(fi, NewStr("closed"))
	if out != True() {
		t.Fatalf("closed = %v after Close, want True", out)
	}
}

func TestFileMethodLookup(t *testing.T) {
	fi := makeReadFile(t, false, "")
	for _, name := range []string{"read", "readline", "write", "close", "flush", "__enter__", "__exit__", "readable", "writable"} {
		out, err := fileGetattr(fi, NewStr(name))
		if err != nil {
			t.Fatalf("getattr %q: %v", name, err)
		}
		if _, ok := out.(*BuiltinFunction); !ok {
			t.Fatalf("getattr %q returned %T, want *BuiltinFunction", name, out)
		}
	}
	if _, err := fileGetattr(fi, NewStr("bogus")); err == nil {
		t.Fatalf("bogus attr returned nil error")
	}
}

func TestFileContextManagerCloses(t *testing.T) {
	fi := makeReadFile(t, false, "x")
	enter, err := fileGetattr(fi, NewStr("__enter__"))
	if err != nil {
		t.Fatalf("__enter__ getattr: %v", err)
	}
	out, err := enter.(*BuiltinFunction).Fn(nil, nil)
	if err != nil {
		t.Fatalf("__enter__: %v", err)
	}
	if out != fi {
		t.Fatalf("__enter__ returned %v, want self", out)
	}
	exit, err := fileGetattr(fi, NewStr("__exit__"))
	if err != nil {
		t.Fatalf("__exit__ getattr: %v", err)
	}
	if _, err := exit.(*BuiltinFunction).Fn([]Object{None(), None(), None()}, nil); err != nil {
		t.Fatalf("__exit__: %v", err)
	}
	if !fi.closed {
		t.Fatalf("__exit__ did not close file")
	}
}
