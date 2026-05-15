package _thread

import (
	"sync"
	"testing"

	"github.com/tamnd/gopy/objects"
)

// TestLocalRejectsArgsOnBaseClass mirrors CPython's local_new check:
// _local() with arguments raises TypeError unless a subclass overrides
// __init__.
//
// CPython: Modules/_threadmodule.c:1454 local_new (tp_init guard)
func TestLocalRejectsArgsOnBaseClass(t *testing.T) {
	_, err := localNew(localType, []objects.Object{objects.NewInt(1)}, nil)
	if err == nil {
		t.Fatal("expected TypeError, got nil")
	}
	if msg := err.Error(); msg == "" || msg[:11] != "TypeError: " {
		t.Errorf("err = %q, want TypeError prefix", msg)
	}
}

// TestLocalSetGetSameGoroutine pins the basic get/set round-trip on a
// single thread.
//
// CPython: Modules/_threadmodule.c:1639 _ldict
func TestLocalSetGetSameGoroutine(t *testing.T) {
	obj, err := localNew(localType, nil, nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := localSetattr(obj, objects.NewStr("x"), objects.NewInt(42)); err != nil {
		t.Fatalf("setattr: %v", err)
	}
	v, err := localGetattr(obj, objects.NewStr("x"))
	if err != nil {
		t.Fatalf("getattr: %v", err)
	}
	iv, _ := v.(*objects.Int).Int64()
	if iv != 42 {
		t.Errorf("got %d, want 42", iv)
	}
}

// TestLocalIsolatedAcrossGoroutines verifies each goroutine sees its
// own dict.
//
// CPython: Modules/_threadmodule.c:1320 (per-thread localsdict)
func TestLocalIsolatedAcrossGoroutines(t *testing.T) {
	obj, err := localNew(localType, nil, nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := localSetattr(obj, objects.NewStr("v"), objects.NewInt(1)); err != nil {
		t.Fatalf("setattr: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Other goroutines start with no `v`.
			if _, err := localGetattr(obj, objects.NewStr("v")); err == nil {
				t.Errorf("goroutine %d unexpectedly saw v", i)
			}
			if err := localSetattr(obj, objects.NewStr("v"), objects.NewInt(int64(i+100))); err != nil {
				t.Errorf("setattr: %v", err)
			}
			got, err := localGetattr(obj, objects.NewStr("v"))
			if err != nil {
				t.Errorf("getattr: %v", err)
				return
			}
			gv, _ := got.(*objects.Int).Int64()
			if gv != int64(i+100) {
				t.Errorf("goroutine %d got %d, want %d", i, gv, i+100)
			}
		}()
	}
	wg.Wait()

	// Main goroutine's value is untouched.
	v, err := localGetattr(obj, objects.NewStr("v"))
	if err != nil {
		t.Fatalf("getattr main: %v", err)
	}
	mv, _ := v.(*objects.Int).Int64()
	if mv != 1 {
		t.Errorf("main got %d, want 1", mv)
	}
}

// TestLocalDictAttr exposes __dict__ as the per-thread dict.
//
// CPython: Modules/_threadmodule.c:1763 local_getattro (__dict__ short-circuit)
func TestLocalDictAttr(t *testing.T) {
	obj, err := localNew(localType, nil, nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := localSetattr(obj, objects.NewStr("a"), objects.NewInt(7)); err != nil {
		t.Fatalf("setattr: %v", err)
	}
	d, err := localGetattr(obj, objects.NewStr("__dict__"))
	if err != nil {
		t.Fatalf("getattr __dict__: %v", err)
	}
	dict, ok := d.(*objects.Dict)
	if !ok {
		t.Fatalf("__dict__ is %T, want *Dict", d)
	}
	v, err := dict.GetItem(objects.NewStr("a"))
	if err != nil {
		t.Fatalf("dict['a']: %v", err)
	}
	iv, _ := v.(*objects.Int).Int64()
	if iv != 7 {
		t.Errorf("dict['a'] = %d, want 7", iv)
	}
}

// TestLocalDictReadOnly: assigning to __dict__ raises AttributeError.
//
// CPython: Modules/_threadmodule.c:1708 local_setattro (__dict__ read-only)
func TestLocalDictReadOnly(t *testing.T) {
	obj, err := localNew(localType, nil, nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	err = localSetattr(obj, objects.NewStr("__dict__"), objects.NewDict())
	if err == nil {
		t.Fatal("expected AttributeError, got nil")
	}
}

// TestLocalDelMissing raises AttributeError when deleting an attr the
// current thread never set.
//
// CPython: Modules/_threadmodule.c:1715 _PyObject_GenericSetAttrWithDict
func TestLocalDelMissing(t *testing.T) {
	obj, err := localNew(localType, nil, nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := localSetattr(obj, objects.NewStr("x"), nil); err == nil {
		t.Fatal("expected AttributeError, got nil")
	}
}
