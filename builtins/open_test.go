// Coverage for the open() builtin. Argument validation and mode
// parsing live in this package; the round-trip through the File type
// (read what was written) is exercised end-to-end via vm/open_test.go.

package builtins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamnd/gopy/objects"
)

func TestOpenReadsExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "in.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	out, err := Open([]objects.Object{
		objects.NewStr(path),
	}, nil)
	if err != nil {
		t.Fatalf("open(): %v", err)
	}
	fi, ok := out.(*objects.File)
	if !ok {
		t.Fatalf("open returned %T, want *File", out)
	}
	defer fi.Close()
	got, err := fi.Read(-1)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.(*objects.Unicode).Value() != "hello" {
		t.Fatalf("Read = %q", got.(*objects.Unicode).Value())
	}
}

func TestOpenWritesAndCreates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	out, err := Open([]objects.Object{
		objects.NewStr(path),
		objects.NewStr("w"),
	}, nil)
	if err != nil {
		t.Fatalf("open(w): %v", err)
	}
	fi := out.(*objects.File)
	if _, err := fi.Write(objects.NewStr("written")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := fi.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	disk, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(disk) != "written" {
		t.Fatalf("on disk = %q", disk)
	}
}

func TestOpenBinaryReturnsBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "in.bin")
	if err := os.WriteFile(path, []byte{0, 1, 2}, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	out, err := Open([]objects.Object{
		objects.NewStr(path),
		objects.NewStr("rb"),
	}, nil)
	if err != nil {
		t.Fatalf("open(rb): %v", err)
	}
	fi := out.(*objects.File)
	defer fi.Close()
	got, err := fi.Read(-1)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	b, ok := got.(*objects.Bytes)
	if !ok {
		t.Fatalf("binary read returned %T, want *Bytes", got)
	}
	if string(b.Bytes()) != "\x00\x01\x02" {
		t.Fatalf("Read = %q", b.Bytes())
	}
}

func TestOpenInvalidModeChar(t *testing.T) {
	_, err := Open([]objects.Object{
		objects.NewStr("x"),
		objects.NewStr("z"),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "ValueError: invalid mode") {
		t.Fatalf("err = %v, want invalid-mode ValueError", err)
	}
}

func TestOpenDuplicateModeChar(t *testing.T) {
	_, err := Open([]objects.Object{
		objects.NewStr("x"),
		objects.NewStr("rr"),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "invalid mode") {
		t.Fatalf("err = %v, want invalid-mode ValueError", err)
	}
}

func TestOpenTextAndBinaryReject(t *testing.T) {
	_, err := Open([]objects.Object{
		objects.NewStr("x"),
		objects.NewStr("rtb"),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "text and binary") {
		t.Fatalf("err = %v, want text+binary ValueError", err)
	}
}

func TestOpenMissingDirection(t *testing.T) {
	_, err := Open([]objects.Object{
		objects.NewStr("x"),
		objects.NewStr("b"),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "must have exactly one") {
		t.Fatalf("err = %v, want missing-direction ValueError", err)
	}
}

func TestOpenMultipleDirections(t *testing.T) {
	_, err := Open([]objects.Object{
		objects.NewStr("x"),
		objects.NewStr("rw"),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "read/write/append") {
		t.Fatalf("err = %v, want multi-direction ValueError", err)
	}
}

func TestOpenMissingFileArg(t *testing.T) {
	_, err := Open(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "missing required argument") {
		t.Fatalf("err = %v, want missing 'file'", err)
	}
}

func TestOpenInvalidFileType(t *testing.T) {
	_, err := Open([]objects.Object{objects.NewInt(7)}, nil)
	if err == nil || !strings.Contains(err.Error(), "TypeError: invalid file") {
		t.Fatalf("err = %v, want TypeError on int file arg", err)
	}
}

func TestOpenUnknownKwarg(t *testing.T) {
	_, err := Open(
		[]objects.Object{objects.NewStr("x")},
		map[string]objects.Object{"flush": objects.None()},
	)
	if err == nil || !strings.Contains(err.Error(), "unexpected keyword argument") {
		t.Fatalf("err = %v, want unexpected-kwarg TypeError", err)
	}
}

func TestOpenAppendCreatesAndAppends(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.txt")
	if err := os.WriteFile(path, []byte("first\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	out, err := Open([]objects.Object{
		objects.NewStr(path),
		objects.NewStr("a"),
	}, nil)
	if err != nil {
		t.Fatalf("open(a): %v", err)
	}
	fi := out.(*objects.File)
	if _, err := fi.Write(objects.NewStr("second\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := fi.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	disk, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(disk) != "first\nsecond\n" {
		t.Fatalf("on disk = %q", disk)
	}
}

func TestOpenExclusiveFailsIfExists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(path, []byte("oops"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := Open([]objects.Object{
		objects.NewStr(path),
		objects.NewStr("x"),
	}, nil)
	if err == nil {
		t.Fatalf("open('x') over existing file accepted")
	}
}

func TestOpenUpdateModeReadable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rw.txt")
	if err := os.WriteFile(path, []byte("zzz"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	out, err := Open([]objects.Object{
		objects.NewStr(path),
		objects.NewStr("r+"),
	}, nil)
	if err != nil {
		t.Fatalf("open(r+): %v", err)
	}
	fi := out.(*objects.File)
	defer fi.Close()
	got, err := fi.Read(-1)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.(*objects.Unicode).Value() != "zzz" {
		t.Fatalf("Read after r+ = %q, want zzz", got.(*objects.Unicode).Value())
	}
}
