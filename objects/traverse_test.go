// Behavioral tests for the tp_traverse slot panel. Pin the four
// container types that ship with v0.10's GC bootstrap.

package objects

import (
	"errors"
	"testing"
)

func collectVisited(o Object) ([]Object, error) {
	tr := o.Type().TpTraverse
	if tr == nil {
		return nil, errors.New("type has no TpTraverse")
	}
	var out []Object
	if err := tr(o, func(child Object) error {
		out = append(out, child)
		return nil
	}); err != nil {
		return nil, err
	}
	return out, nil
}

func TestTupleTraverse(t *testing.T) {
	a := NewStr("a")
	b := NewStr("b")
	tup := NewTuple([]Object{a, b})
	got, err := collectVisited(tup)
	if err != nil {
		t.Fatalf("traverse: %v", err)
	}
	if len(got) != 2 || got[0] != a || got[1] != b {
		t.Fatalf("visited %v, want [%v %v]", got, a, b)
	}
}

func TestListTraverse(t *testing.T) {
	a := NewStr("a")
	b := NewStr("b")
	lst := NewList([]Object{a, b})
	got, err := collectVisited(lst)
	if err != nil {
		t.Fatalf("traverse: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("visited count = %d, want 2", len(got))
	}
}

func TestDictTraverseKeysAndValues(t *testing.T) {
	d := NewDict()
	k1 := NewStr("k1")
	v1 := NewStr("v1")
	if err := d.SetItem(k1, v1); err != nil {
		t.Fatalf("SetItem: %v", err)
	}
	got, err := collectVisited(d)
	if err != nil {
		t.Fatalf("traverse: %v", err)
	}
	hasKey, hasVal := false, false
	for _, o := range got {
		if o == k1 {
			hasKey = true
		}
		if o == v1 {
			hasVal = true
		}
	}
	if !hasKey || !hasVal {
		t.Fatalf("dict traverse missed entries: %v", got)
	}
}

func TestSetTraverse(t *testing.T) {
	s := NewSet()
	a := NewStr("a")
	if err := s.Add(a); err != nil {
		t.Fatalf("Add: %v", err)
	}
	got, err := collectVisited(s)
	if err != nil {
		t.Fatalf("traverse: %v", err)
	}
	if len(got) != 1 || got[0] != a {
		t.Fatalf("set traverse = %v, want [%v]", got, a)
	}
}

func TestTraverseErrorShortCircuits(t *testing.T) {
	tup := NewTuple([]Object{NewStr("x"), NewStr("y"), NewStr("z")})
	stop := errors.New("stop")
	count := 0
	err := tup.Type().TpTraverse(tup, func(_ Object) error {
		count++
		return stop
	})
	if !errors.Is(err, stop) {
		t.Fatalf("err=%v, want stop", err)
	}
	if count != 1 {
		t.Fatalf("count=%d, want 1 (short-circuit)", count)
	}
}
