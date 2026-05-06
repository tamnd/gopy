package objects

import "testing"

func TestWeakrefCallReturnsReferent(t *testing.T) {
	target := NewList(nil)
	w := NewWeakref(target, nil)
	got, err := w.Type().Call(w, nil, nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if got != target {
		t.Fatalf("got=%v, want %v", got, target)
	}
}

func TestWeakrefClearReturnsCallback(t *testing.T) {
	target := NewList(nil)
	cb := NewList(nil)
	w := NewWeakref(target, cb)

	if got := w.Clear(); got != cb {
		t.Fatalf("Clear() = %v, want callback %v", got, cb)
	}
	// After clearing, Referent must return None and Clear is idempotent.
	if r := w.Referent(); r != None() {
		t.Fatalf("Referent after Clear = %v, want None", r)
	}
	if got := w.Clear(); got != None() {
		t.Fatalf("second Clear = %v, want None", got)
	}
}

func TestWeakrefClearWithNoCallback(t *testing.T) {
	target := NewList(nil)
	w := NewWeakref(target, nil)
	if got := w.Clear(); got != None() {
		t.Fatalf("Clear with no callback = %v, want None", got)
	}
}

func TestWeakrefCallAfterClearReturnsNone(t *testing.T) {
	target := NewList(nil)
	w := NewWeakref(target, nil)
	w.Clear()
	got, err := w.Type().Call(w, nil, nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if got != None() {
		t.Fatalf("Call after Clear = %v, want None", got)
	}
}
