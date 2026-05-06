package objects

import (
	"strings"
	"testing"
)

func TestWeakProxyTypeNames(t *testing.T) {
	if WeakProxyType.Name != "weakproxy" {
		t.Errorf("WeakProxyType.Name = %q", WeakProxyType.Name)
	}
	if WeakCallableProxyType.Name != "weakcallableproxy" {
		t.Errorf("WeakCallableProxyType.Name = %q", WeakCallableProxyType.Name)
	}
}

func TestWeakProxyForwardsLen(t *testing.T) {
	target := NewList(nil)
	target.Append(NewInt(1))
	target.Append(NewInt(2))
	p := NewWeakProxy(target, nil)
	n, err := Length(p)
	if err != nil {
		t.Fatalf("Length: %v", err)
	}
	if n != 2 {
		t.Errorf("len = %d, want 2", n)
	}
}

func TestWeakProxyForwardsGetItem(t *testing.T) {
	target := NewList(nil)
	target.Append(NewInt(7))
	p := NewWeakProxy(target, nil)
	v, err := GetItem(p, NewInt(0))
	if err != nil {
		t.Fatalf("GetItem: %v", err)
	}
	n, _ := v.(*Int).Int64()
	if n != 7 {
		t.Errorf("proxy[0] = %d, want 7", n)
	}
}

func TestWeakProxyForwardsSetItem(t *testing.T) {
	target := NewList(nil)
	target.Append(NewInt(0))
	p := NewWeakProxy(target, nil)
	if err := SetItem(p, NewInt(0), NewInt(42)); err != nil {
		t.Fatalf("SetItem: %v", err)
	}
	got, _ := target.Item(0).(*Int).Int64()
	if got != 42 {
		t.Errorf("target[0] = %d, want 42", got)
	}
}

func TestWeakProxyForwardsContains(t *testing.T) {
	target := NewList(nil)
	target.Append(NewInt(5))
	p := NewWeakProxy(target, nil)
	ok, err := Contains(p, NewInt(5))
	if err != nil {
		t.Fatalf("Contains: %v", err)
	}
	if !ok {
		t.Error("Contains returned false; want true")
	}
}

func TestWeakProxyForwardsIter(t *testing.T) {
	target := NewList(nil)
	target.Append(NewInt(1))
	target.Append(NewInt(2))
	p := NewWeakProxy(target, nil)
	it, err := Iter(p)
	if err != nil {
		t.Fatalf("Iter: %v", err)
	}
	got := 0
	for i := 0; i < 5; i++ {
		v, err := IterNext(it)
		if err != nil {
			break
		}
		n, _ := v.(*Int).Int64()
		got += int(n)
	}
	if got != 3 {
		t.Errorf("sum via proxy iter = %d, want 3", got)
	}
}

func TestWeakProxyRepr(t *testing.T) {
	target := NewList(nil)
	p := NewWeakProxy(target, nil)
	r, err := Repr(p)
	if err != nil {
		t.Fatalf("Repr: %v", err)
	}
	if !strings.Contains(r, "weakproxy at") || !strings.Contains(r, "list") {
		t.Errorf("repr = %q", r)
	}
	p.Clear()
	r, err = Repr(p)
	if err != nil {
		t.Fatalf("Repr (dead): %v", err)
	}
	if !strings.Contains(r, "dead") {
		t.Errorf("dead repr = %q, want to contain 'dead'", r)
	}
}

func TestWeakProxyClearMakesAccessFail(t *testing.T) {
	target := NewList(nil)
	target.Append(NewInt(1))
	p := NewWeakProxy(target, nil)
	p.Clear()
	if _, err := Length(p); err == nil {
		t.Error("Length on dead proxy: want ReferenceError, got nil")
	}
	if _, err := GetItem(p, NewInt(0)); err == nil {
		t.Error("GetItem on dead proxy: want ReferenceError, got nil")
	}
	if _, err := Iter(p); err == nil {
		t.Error("Iter on dead proxy: want ReferenceError, got nil")
	}
}

func TestWeakProxyRichCmpForwards(t *testing.T) {
	a := NewInt(7)
	b := NewInt(7)
	p := NewWeakProxy(a, nil)
	eq, err := RichCmpBool(p, b, CompareEQ)
	if err != nil {
		t.Fatalf("RichCmp: %v", err)
	}
	if !eq {
		t.Error("proxy(7) == 7: got false")
	}
}

func TestWeakProxyClearIsIdempotent(t *testing.T) {
	target := NewList(nil)
	p := NewWeakProxy(target, nil)
	p.Clear()
	if got := p.Clear(); got != None() {
		t.Errorf("second Clear returned %v, want None", got)
	}
	if got := p.Referent(); got != nil {
		t.Errorf("Referent after Clear = %v, want nil", got)
	}
}

func TestWeakCallableProxyForwardsCall(t *testing.T) {
	called := 0
	fn := NewBuiltinFunction("fn", func(args []Object, _ map[string]Object) (Object, error) {
		called++
		if len(args) != 1 {
			t.Errorf("argc = %d, want 1", len(args))
		}
		return args[0], nil
	})
	p := NewWeakCallableProxy(fn, nil)
	if p.Type() != WeakCallableProxyType {
		t.Fatalf("Type = %v, want WeakCallableProxyType", p.Type())
	}
	v, err := WeakCallableProxyType.Call(p, []Object{NewInt(11)}, nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	n, _ := v.(*Int).Int64()
	if n != 11 {
		t.Errorf("Call result = %d, want 11", n)
	}
	if called != 1 {
		t.Errorf("called %d times, want 1", called)
	}
}

func TestNonCallableProxyHasNoCallSlot(t *testing.T) {
	target := NewList(nil)
	p := NewWeakProxy(target, nil)
	if WeakProxyType.Call != nil {
		t.Error("WeakProxyType has tp_call but should not")
	}
	_ = p
}

func TestWeakProxyDeadCallRaises(t *testing.T) {
	fn := NewBuiltinFunction("fn", func(args []Object, _ map[string]Object) (Object, error) {
		return None(), nil
	})
	p := NewWeakCallableProxy(fn, nil)
	p.Clear()
	if _, err := WeakCallableProxyType.Call(p, nil, nil); err == nil {
		t.Error("Call on dead callable proxy: want error, got nil")
	}
}
