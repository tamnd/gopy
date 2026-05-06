package traceback_test

import (
	"testing"

	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/traceback"
)

func TestTracebackTypeName(t *testing.T) {
	if traceback.Type.Name != "traceback" {
		t.Fatalf("Type.Name = %q, want %q", traceback.Type.Name, "traceback")
	}
}

func TestTracebackImplementsObject(t *testing.T) {
	tb := traceback.New(traceback.Entry{File: "x.py", Line: 7, Name: "f"})
	if tb.Type() != traceback.Type {
		t.Fatalf("tb.Type = %v, want traceback.Type", tb.Type())
	}
}

func TestTracebackGetattrLineno(t *testing.T) {
	tb := traceback.New(traceback.Entry{File: "x.py", Line: 7, Name: "f"})
	v, err := objects.GetAttr(tb, objects.NewStr("tb_lineno"))
	if err != nil {
		t.Fatalf("tb_lineno: %v", err)
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 7 {
		t.Fatalf("tb_lineno = %d, want 7", got)
	}
}

func TestTracebackGetattrLastiDefaultsZero(t *testing.T) {
	tb := traceback.New(traceback.Entry{File: "x.py", Line: 1})
	v, err := objects.GetAttr(tb, objects.NewStr("tb_lasti"))
	if err != nil {
		t.Fatalf("tb_lasti: %v", err)
	}
	got, _ := v.(*objects.Int).Int64()
	if got != 0 {
		t.Fatalf("tb_lasti = %d, want 0", got)
	}
}

func TestTracebackGetattrFrameDefaultsNone(t *testing.T) {
	tb := traceback.New(traceback.Entry{})
	v, err := objects.GetAttr(tb, objects.NewStr("tb_frame"))
	if err != nil {
		t.Fatalf("tb_frame: %v", err)
	}
	if v != objects.None() {
		t.Fatalf("tb_frame = %v, want None", v)
	}
}

func TestTracebackGetattrNextChain(t *testing.T) {
	inner := traceback.New(traceback.Entry{File: "inner.py", Line: 1})
	outer := traceback.Push(inner, traceback.Entry{File: "outer.py", Line: 2})
	v, err := objects.GetAttr(outer, objects.NewStr("tb_next"))
	if err != nil {
		t.Fatalf("tb_next: %v", err)
	}
	if v != inner {
		t.Fatalf("tb_next = %v, want inner", v)
	}
	v, err = objects.GetAttr(inner, objects.NewStr("tb_next"))
	if err != nil {
		t.Fatalf("inner tb_next: %v", err)
	}
	if v != objects.None() {
		t.Fatalf("inner tb_next = %v, want None", v)
	}
}

func TestTracebackSetattrNext(t *testing.T) {
	a := traceback.New(traceback.Entry{File: "a.py", Line: 1})
	b := traceback.New(traceback.Entry{File: "b.py", Line: 2})
	if err := objects.SetAttr(a, objects.NewStr("tb_next"), b); err != nil {
		t.Fatalf("set tb_next: %v", err)
	}
	if a.Next != b {
		t.Fatalf("a.Next not updated")
	}
	if err := objects.SetAttr(a, objects.NewStr("tb_next"), objects.None()); err != nil {
		t.Fatalf("set tb_next=None: %v", err)
	}
	if a.Next != nil {
		t.Fatalf("a.Next not cleared")
	}
}

func TestTracebackSetattrReadonly(t *testing.T) {
	tb := traceback.New(traceback.Entry{})
	if err := objects.SetAttr(tb, objects.NewStr("tb_lineno"), objects.NewInt(99)); err == nil {
		t.Fatal("set tb_lineno: want AttributeError, got nil")
	}
}

func TestTracebackUnknownAttr(t *testing.T) {
	tb := traceback.New(traceback.Entry{})
	if _, err := objects.GetAttr(tb, objects.NewStr("nope")); err == nil {
		t.Fatal("getattr nope: want AttributeError, got nil")
	}
}
