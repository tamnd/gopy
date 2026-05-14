// Coverage for the open() builtin. Argument validation and mode
// parsing now live in module/io; builtins.Open delegates there.
// Tests use the attribute-dispatch helpers to stay type-agnostic
// since open() now returns *io.TextIOWrapper or *io.FileIO instead
// of *objects.File.

package builtins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	_io "github.com/tamnd/gopy/module/io"
	"github.com/tamnd/gopy/objects"
)

// callMethod invokes a named method on obj (via getattr) with args.
func callMethod(t *testing.T, obj objects.Object, method string, args ...objects.Object) objects.Object {
	t.Helper()
	attr, err := obj.Type().Getattro(obj, objects.NewStr(method))
	if err != nil {
		t.Fatalf("getattr(%q): %v", method, err)
	}
	fn, ok := attr.(*objects.BuiltinFunction)
	if !ok {
		t.Fatalf("attr %q is %T, want *BuiltinFunction", method, attr)
	}
	result, err := fn.Fn(args, nil)
	if err != nil {
		t.Fatalf("%s(): %v", method, err)
	}
	return result
}

func closeObj(t *testing.T, obj objects.Object) {
	t.Helper()
	callMethod(t, obj, "close")
}

func readAll(t *testing.T, obj objects.Object) objects.Object {
	t.Helper()
	return callMethod(t, obj, "read", objects.NewInt(-1))
}

func writeStr(t *testing.T, obj objects.Object, s string) {
	t.Helper()
	callMethod(t, obj, "write", objects.NewStr(s))
}

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
	defer closeObj(t, out)
	got := readAll(t, out)
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
	writeStr(t, out, "written")
	closeObj(t, out)
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
	defer closeObj(t, out)
	got := readAll(t, out)
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
	writeStr(t, out, "second\n")
	closeObj(t, out)
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
	defer closeObj(t, out)
	got := readAll(t, out)
	if got.(*objects.Unicode).Value() != "zzz" {
		t.Fatalf("Read after r+ = %q, want zzz", got.(*objects.Unicode).Value())
	}
}

// TestIOOpenReturnTypes verifies that _io.Open returns the right concrete types.
func TestIOOpenReturnTypes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}

	// text mode -> TextIOWrapper
	out, err := _io.Open([]objects.Object{objects.NewStr(path)}, nil)
	if err != nil {
		t.Fatalf("open text: %v", err)
	}
	if _, ok := out.(*_io.TextIOWrapper); !ok {
		t.Fatalf("text open returned %T, want *_io.TextIOWrapper", out)
	}
	closeObj(t, out)

	// binary mode -> BufferedReader (which wraps FileIO).
	// CPython: Modules/_io/_iomodule.c:392 buffered selection.
	out2, err := _io.Open([]objects.Object{objects.NewStr(path), objects.NewStr("rb")}, nil)
	if err != nil {
		t.Fatalf("open binary: %v", err)
	}
	if out2.Type().Name != "_io.BufferedReader" {
		t.Fatalf("binary open returned %T (%s), want _io.BufferedReader", out2, out2.Type().Name)
	}
	closeObj(t, out2)

	// buffering=0 binary mode -> raw FileIO.
	out3, err := _io.Open([]objects.Object{objects.NewStr(path), objects.NewStr("rb"), objects.NewInt(0)}, nil)
	if err != nil {
		t.Fatalf("open binary unbuffered: %v", err)
	}
	if _, ok := out3.(*_io.FileIO); !ok {
		t.Fatalf("buffering=0 returned %T, want *_io.FileIO", out3)
	}
	closeObj(t, out3)
}
