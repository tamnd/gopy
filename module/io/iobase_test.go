// Behavioral tests for _IOBase and _RawIOBase pinned against the CPython
// 3.14.5 contract in Modules/_io/iobase.c. Each test names the CPython
// function (or methodlist entry) whose behavior it exercises so a future
// reader can re-verify against the upstream source.

package io

import (
	"errors"
	"strings"
	"testing"

	"github.com/tamnd/gopy/objects"
)

// newIOBase builds a bare _IOBase instance via its tp_call.
func newIOBase(t *testing.T) objects.Object {
	t.Helper()
	o, err := IOBaseType.Call(nil, nil, nil)
	if err != nil {
		t.Fatalf("IOBaseType.Call: %v", err)
	}
	return o
}

func newRawIOBase(t *testing.T) objects.Object {
	t.Helper()
	o, err := RawIOBaseType.Call(nil, nil, nil)
	if err != nil {
		t.Fatalf("RawIOBaseType.Call: %v", err)
	}
	return o
}

func callMethod(t *testing.T, o objects.Object, name string, args ...objects.Object) (objects.Object, error) {
	t.Helper()
	fn, err := objects.GetAttr(o, objects.NewStr(name))
	if err != nil {
		return nil, err
	}
	return objects.Call(fn, objects.NewTuple(args), nil)
}

func mustCall(t *testing.T, o objects.Object, name string, args ...objects.Object) objects.Object {
	t.Helper()
	res, err := callMethod(t, o, name, args...)
	if err != nil {
		t.Fatalf("%s() raised: %v", name, err)
	}
	return res
}

func expectError(t *testing.T, err error, fragment string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", fragment)
	}
	if !strings.Contains(err.Error(), fragment) {
		t.Fatalf("error %q does not contain %q", err.Error(), fragment)
	}
}

// CPython: Modules/_io/iobase.c:117 _io__IOBase_seek_impl
func TestIOBaseSeekUnsupported(t *testing.T) {
	o := newIOBase(t)
	_, err := callMethod(t, o, "seek", objects.NewInt(0))
	expectError(t, err, "UnsupportedOperation")
}

// CPython: Modules/_io/iobase.c:132 _io__IOBase_tell_impl
// tell() must delegate to self.seek(0, 1); on bare IOBase that surfaces the
// same UnsupportedOperation seek would raise.
func TestIOBaseTellDelegatesToSeek(t *testing.T) {
	o := newIOBase(t)
	_, err := callMethod(t, o, "tell")
	expectError(t, err, "UnsupportedOperation")
}

// CPython: Modules/_io/iobase.c:151 _io__IOBase_truncate_impl
func TestIOBaseTruncateUnsupported(t *testing.T) {
	o := newIOBase(t)
	_, err := callMethod(t, o, "truncate")
	expectError(t, err, "UnsupportedOperation")
}

// CPython: Modules/_io/iobase.c:526 _io__IOBase_fileno_impl
func TestIOBaseFilenoUnsupported(t *testing.T) {
	o := newIOBase(t)
	_, err := callMethod(t, o, "fileno")
	expectError(t, err, "UnsupportedOperation")
}

// CPython: Modules/_io/iobase.c:405/437/470 seekable/readable/writable default
// to False on the abstract base.
func TestIOBaseCapabilityDefaultsFalse(t *testing.T) {
	o := newIOBase(t)
	for _, name := range []string{"seekable", "readable", "writable"} {
		res := mustCall(t, o, name)
		if objects.IsTrue(res) {
			t.Fatalf("%s() = True, want False", name)
		}
	}
}

// CPython: Modules/_io/iobase.c:170 _io__IOBase_flush_impl
// flush is a no-op while open and raises ValueError once closed.
func TestIOBaseFlushBehavior(t *testing.T) {
	o := newIOBase(t)
	if res := mustCall(t, o, "flush"); !objects.IsNone(res) {
		t.Fatalf("flush() = %v, want None", res)
	}
	mustCall(t, o, "close")
	_, err := callMethod(t, o, "flush")
	expectError(t, err, "closed")
}

// CPython: Modules/_io/iobase.c:268 _io__IOBase_close_impl
// close sets __IOBase_closed and is idempotent.
func TestIOBaseCloseIdempotent(t *testing.T) {
	o := newIOBase(t)
	mustCall(t, o, "close")
	closed, _ := objects.GetAttr(o, objects.NewStr("closed"))
	if !objects.IsTrue(closed) {
		t.Fatalf("closed after close() = %v, want True", closed)
	}
	// Second close must not raise.
	if _, err := callMethod(t, o, "close"); err != nil {
		t.Fatalf("second close() raised: %v", err)
	}
}

// CPython: Modules/_io/iobase.c:497 iobase_enter
// __enter__ on a closed file raises ValueError.
func TestIOBaseEnterOnClosed(t *testing.T) {
	o := newIOBase(t)
	res := mustCall(t, o, "__enter__")
	if res != o {
		t.Fatalf("__enter__ on open returned %v, want self", res)
	}
	mustCall(t, o, "close")
	_, err := callMethod(t, o, "__enter__")
	expectError(t, err, "closed")
}

// CPython: Modules/_io/iobase.c:506 iobase_exit
// __exit__ must call close().
func TestIOBaseExitCallsClose(t *testing.T) {
	o := newIOBase(t)
	mustCall(t, o, "__exit__")
	closed, _ := objects.GetAttr(o, objects.NewStr("closed"))
	if !objects.IsTrue(closed) {
		t.Fatalf("closed after __exit__ = %v, want True", closed)
	}
}

// CPython: Modules/_io/iobase.c:542 _io__IOBase_isatty_impl
// isatty returns False on open, raises on closed.
func TestIOBaseIsattyBehavior(t *testing.T) {
	o := newIOBase(t)
	res := mustCall(t, o, "isatty")
	if objects.IsTrue(res) {
		t.Fatalf("isatty() = True on fresh IOBase, want False")
	}
	mustCall(t, o, "close")
	_, err := callMethod(t, o, "isatty")
	expectError(t, err, "closed")
}

// CPython: Modules/_io/iobase.c:412/445/478 _PyIOBase_check_*
// _checkSeekable / _checkReadable / _checkWritable raise
// UnsupportedOperation on the abstract base where each capability is False.
func TestIOBaseCheckCapabilityRaises(t *testing.T) {
	o := newIOBase(t)
	for _, pair := range [][2]string{
		{"_checkSeekable", "not seekable"},
		{"_checkReadable", "not readable"},
		{"_checkWritable", "not writable"},
	} {
		_, err := callMethod(t, o, pair[0])
		expectError(t, err, pair[1])
	}
}

// CPython: Modules/_io/iobase.c:215 _PyIOBase_check_closed
func TestIOBaseCheckClosed(t *testing.T) {
	o := newIOBase(t)
	if _, err := callMethod(t, o, "_checkClosed"); err != nil {
		t.Fatalf("_checkClosed on open: %v", err)
	}
	mustCall(t, o, "close")
	_, err := callMethod(t, o, "_checkClosed")
	expectError(t, err, "closed")
}

// CPython: Modules/_io/iobase.c:677 iobase_iter — iter on a closed IOBase
// raises ValueError.
func TestIOBaseIterOnClosed(t *testing.T) {
	o := newIOBase(t)
	mustCall(t, o, "close")
	_, err := objects.Iter(o)
	expectError(t, err, "closed")
}

// CPython: Modules/_io/iobase.c:248 _PyIOBase_cannot_pickle
func TestIOBaseCannotPickle(t *testing.T) {
	o := newIOBase(t)
	_, err := IOBaseCannotPickle(o)
	expectError(t, err, "cannot pickle")
	if !strings.Contains(err.Error(), "_io._IOBase") {
		t.Fatalf("error %q missing type name", err.Error())
	}
}

// CPython: Modules/_io/iobase.c:920 _io__RawIOBase_read_impl
// read(-1) delegates to readall; read(n) calls readinto and stops at the
// returned count.
func TestRawIOBaseReadDispatch(t *testing.T) {
	o := newRawIOBase(t)
	// readall on bare RawIOBase loops self.read(DEFAULT_BUFFER_SIZE), which
	// itself dispatches back through readinto -> NotImplementedError.
	_, err := callMethod(t, o, "read", objects.NewInt(-1))
	expectError(t, err, "NotImplementedError")

	_, err = callMethod(t, o, "read", objects.NewInt(8))
	expectError(t, err, "NotImplementedError")
}

// CPython: Modules/_io/iobase.c:1022 rawiobase_readinto / :1029 rawiobase_write
func TestRawIOBaseReadintoWriteUnimplemented(t *testing.T) {
	o := newRawIOBase(t)
	_, err := callMethod(t, o, "readinto")
	expectError(t, err, "NotImplementedError")
	_, err = callMethod(t, o, "write")
	expectError(t, err, "NotImplementedError")
}

// ---- readline / readlines / writelines / iternext ------------------------
//
// These need a working read() and (optionally) peek() to exercise the peek
// fast path and the line-boundary logic. We wire that up via a minimal
// in-memory subclass of RawIOBase: scriptedReader holds a byte buffer and
// vends slices through read(n) / peek(n) just like CPython's BufferedReader
// does for IOBase.readline.

func makeScriptedReader(t *testing.T, data []byte, withPeek bool) objects.Object {
	t.Helper()
	o := newRawIOBase(t)
	pos := 0
	readFn := objects.NewBuiltinFunction("read", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		n := 1
		if len(args) >= 1 {
			if v, ok := args[0].(*objects.Int); ok {
				iv, _ := v.Int64()
				n = int(iv)
			}
		}
		if pos >= len(data) {
			return objects.NewBytes(nil), nil
		}
		end := min(pos+n, len(data))
		chunk := append([]byte(nil), data[pos:end]...)
		pos = end
		return objects.NewBytes(chunk), nil
	})
	if err := objects.SetAttr(o, objects.NewStr("read"), readFn); err != nil {
		t.Fatalf("SetAttr read: %v", err)
	}
	if withPeek {
		peekFn := objects.NewBuiltinFunction("peek", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			n := 1
			if len(args) >= 1 {
				if v, ok := args[0].(*objects.Int); ok {
					iv, _ := v.Int64()
					n = int(iv)
				}
			}
			if pos >= len(data) {
				return objects.NewBytes(nil), nil
			}
			end := min(pos+n, len(data))
			// peek must not advance.
			return objects.NewBytes(append([]byte(nil), data[pos:end]...)), nil
		})
		if err := objects.SetAttr(o, objects.NewStr("peek"), peekFn); err != nil {
			t.Fatalf("SetAttr peek: %v", err)
		}
	}
	return o
}

// CPython: Modules/_io/iobase.c:567 _io__IOBase_readline_impl
// readline reads up to and including the next '\n', or to EOF.
func TestIOBaseReadlineNoPeek(t *testing.T) {
	o := makeScriptedReader(t, []byte("ab\ncd\nef"), false)
	got := mustCall(t, o, "readline").(*objects.Bytes).Bytes()
	if string(got) != "ab\n" {
		t.Fatalf("readline 1 = %q, want %q", got, "ab\n")
	}
	got = mustCall(t, o, "readline").(*objects.Bytes).Bytes()
	if string(got) != "cd\n" {
		t.Fatalf("readline 2 = %q, want %q", got, "cd\n")
	}
	got = mustCall(t, o, "readline").(*objects.Bytes).Bytes()
	if string(got) != "ef" {
		t.Fatalf("readline 3 = %q, want %q (trailing line, no newline)", got, "ef")
	}
	got = mustCall(t, o, "readline").(*objects.Bytes).Bytes()
	if string(got) != "" {
		t.Fatalf("readline at EOF = %q, want empty", got)
	}
}

// CPython: Modules/_io/iobase.c:589 readline peek fast path
// readline must use peek when self has it, batching the read call.
func TestIOBaseReadlineWithPeek(t *testing.T) {
	o := makeScriptedReader(t, []byte("abc\ndef\n"), true)
	got := mustCall(t, o, "readline").(*objects.Bytes).Bytes()
	if string(got) != "abc\n" {
		t.Fatalf("readline with peek = %q, want %q", got, "abc\n")
	}
}

// CPython: Modules/_io/iobase.c:567 readline limit argument
// readline(limit) must stop at the limit even without a newline.
func TestIOBaseReadlineLimit(t *testing.T) {
	o := makeScriptedReader(t, []byte("abcdefgh\nijk"), false)
	got := mustCall(t, o, "readline", objects.NewInt(4)).(*objects.Bytes).Bytes()
	if string(got) != "abcd" {
		t.Fatalf("readline(4) = %q, want %q", got, "abcd")
	}
}

// CPython: Modules/_io/iobase.c:686 iobase_iternext
// Iteration walks readline until empty.
func TestIOBaseIternextWalksLines(t *testing.T) {
	o := makeScriptedReader(t, []byte("a\nb\nc\n"), false)
	it, err := objects.Iter(o)
	if err != nil {
		t.Fatalf("Iter: %v", err)
	}
	var lines []string
	for {
		v, err := objects.IterNext(it)
		if err != nil {
			if errors.Is(err, objects.ErrStopIteration) {
				break
			}
			t.Fatalf("IterNext: %v", err)
		}
		lines = append(lines, string(v.(*objects.Bytes).Bytes()))
	}
	want := []string{"a\n", "b\n", "c\n"}
	if strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Fatalf("iter lines = %v, want %v", lines, want)
	}
}

// CPython: Modules/_io/iobase.c:715 _io__IOBase_readlines_impl
// hint <= 0 collects every line.
func TestIOBaseReadlinesAll(t *testing.T) {
	o := makeScriptedReader(t, []byte("a\nb\nc\n"), false)
	res := mustCall(t, o, "readlines").(*objects.List)
	if res.Len() != 3 {
		t.Fatalf("readlines len = %d, want 3", res.Len())
	}
}

// CPython: Modules/_io/iobase.c:763 readlines hint break-after-append rule:
// "if line_length > hint - length: break". With hint=3 and lines "aa\n","bb\n",
// after the first line length=0, line_length=3 > 3-0=3 is false -> length=3;
// second iter: 3 > 3-3=0 is true -> break after appending the second line.
// Net result: two lines collected.
func TestIOBaseReadlinesHintBreak(t *testing.T) {
	o := makeScriptedReader(t, []byte("aa\nbb\ncc\n"), false)
	res := mustCall(t, o, "readlines", objects.NewInt(3)).(*objects.List)
	if res.Len() != 2 {
		t.Fatalf("readlines(hint=3) len = %d, want 2", res.Len())
	}
}

// CPython: Modules/_io/iobase.c:789 _io__IOBase_writelines
// writelines closed-check runs before iterating.
func TestIOBaseWritelinesOnClosed(t *testing.T) {
	o := newIOBase(t)
	mustCall(t, o, "close")
	_, err := callMethod(t, o, "writelines", objects.NewList(nil))
	expectError(t, err, "closed")
}

// CPython: Modules/_io/iobase.c:801 writelines loop calls self.write per item.
func TestIOBaseWritelinesCallsWrite(t *testing.T) {
	o := newRawIOBase(t)
	var calls []string
	writeFn := objects.NewBuiltinFunction("write", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		if len(args) >= 1 {
			if b, ok := args[0].(*objects.Bytes); ok {
				calls = append(calls, string(b.Bytes()))
			}
		}
		return objects.None(), nil
	})
	if err := objects.SetAttr(o, objects.NewStr("write"), writeFn); err != nil {
		t.Fatalf("SetAttr write: %v", err)
	}
	items := objects.NewList([]objects.Object{
		objects.NewBytes([]byte("hi")),
		objects.NewBytes([]byte("there")),
	})
	if _, err := callMethod(t, o, "writelines", items); err != nil {
		t.Fatalf("writelines: %v", err)
	}
	if strings.Join(calls, "|") != "hi|there" {
		t.Fatalf("write calls = %v, want [hi there]", calls)
	}
}

// CPython: Modules/_io/iobase.c:411/444/477 _PyIOBase_check_* pass when the
// subclass advertises the capability. We override seekable to return True
// and confirm _checkSeekable now returns silently.
func TestIOBaseCheckCapabilityPasses(t *testing.T) {
	o := newIOBase(t)
	seekFn := objects.NewBuiltinFunction("seekable", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return objects.True(), nil
	})
	if err := objects.SetAttr(o, objects.NewStr("seekable"), seekFn); err != nil {
		t.Fatalf("SetAttr: %v", err)
	}
	if _, err := callMethod(t, o, "_checkSeekable"); err != nil {
		t.Fatalf("_checkSeekable with seekable=True raised: %v", err)
	}
}
