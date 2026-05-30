package objects

import (
	"errors"
	"fmt"
	"testing"
)

// genYieldFn matches the real YIELD_VALUE semantics: write the
// yielded value to YieldCh, then suspend on SendCh and return the
// next sent value (or the next thrown error). Bodies use this so a
// 2-yield generator stays in lockstep with a 2-Send caller.
type genYieldFn func(Object) (Object, error)

func runGenBody(g *Generator, body func(yield genYieldFn) error) {
	go func() {
		first := <-g.SendCh
		if first.Err != nil {
			g.YieldCh <- GenMsg{Err: ErrStopIteration}
			return
		}
		yieldFn := func(v Object) (Object, error) {
			g.YieldCh <- GenMsg{Val: v}
			m := <-g.SendCh
			if m.Err != nil {
				return nil, m.Err
			}
			return m.Val, nil
		}
		err := body(yieldFn)
		if err != nil && !errors.Is(err, ErrStopIteration) {
			g.YieldCh <- GenMsg{Err: err}
		} else {
			g.YieldCh <- GenMsg{Err: ErrStopIteration}
		}
	}()
}

func TestGeneratorBasicIteration(t *testing.T) {
	g := NewGenerator("count3", "count3")
	runGenBody(g, func(yield genYieldFn) error {
		for _, v := range []int64{1, 2, 3} {
			if _, err := yield(NewInt(v)); err != nil {
				return err
			}
		}
		return nil
	})
	for _, want := range []int64{1, 2, 3} {
		v, err := g.Send(None())
		if err != nil {
			t.Fatalf("Send: %v", err)
		}
		got, _ := v.(*Int).Int64()
		if got != want {
			t.Errorf("got %d want %d", got, want)
		}
	}
	if _, err := g.Send(None()); !errors.Is(err, ErrStopIteration) {
		t.Errorf("after exhaustion, want StopIteration, got %v", err)
	}
}

func TestGeneratorSendNonNoneToFresh(t *testing.T) {
	g := NewGenerator("g", "g")
	runGenBody(g, func(yield genYieldFn) error { return nil })
	_, err := g.Send(NewInt(7))
	if err == nil || err.Error() != "TypeError: can't send non-None value to a just-started generator" {
		t.Errorf("want TypeError on non-None send to fresh, got %v", err)
	}
}

func TestGeneratorSendValueRoundTrip(t *testing.T) {
	g := NewGenerator("echo", "echo")
	runGenBody(g, func(yield genYieldFn) error {
		v, err := yield(NewInt(0))
		if err != nil {
			return err
		}
		if _, err := yield(v); err != nil {
			return err
		}
		return nil
	})
	_, _ = g.Send(None())
	v, err := g.Send(NewInt(42))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := v.(*Int).Int64()
	if got != 42 {
		t.Errorf("sent value not echoed back: got %d", got)
	}
}

func TestGeneratorThrowPropagates(t *testing.T) {
	g := NewGenerator("g", "g")
	runGenBody(g, func(yield genYieldFn) error {
		_, err := yield(NewInt(1))
		return err
	})
	_, _ = g.Send(None())
	_, err := g.Throw(fmt.Errorf("boom"))
	if err == nil || err.Error() != "boom" {
		t.Errorf("Throw should surface error, got %v", err)
	}
}

func TestGeneratorThrowCaught(t *testing.T) {
	g := NewGenerator("g", "g")
	runGenBody(g, func(yield genYieldFn) error {
		_, err := yield(NewInt(1))
		if err != nil && err.Error() == "boom" {
			if _, err := yield(NewInt(99)); err != nil {
				return err
			}
			return nil
		}
		return err
	})
	_, _ = g.Send(None())
	v, err := g.Throw(fmt.Errorf("boom"))
	if err != nil {
		t.Fatalf("Throw with catch should return yielded value, got err %v", err)
	}
	got, _ := v.(*Int).Int64()
	if got != 99 {
		t.Errorf("after catch want 99, got %d", got)
	}
}

func TestGeneratorThrowOnFresh(t *testing.T) {
	g := NewGenerator("g", "g")
	runGenBody(g, func(yield genYieldFn) error {
		_, err := yield(NewInt(1))
		return err
	})
	_, err := g.Throw(fmt.Errorf("early"))
	if err == nil || err.Error() != "early" {
		t.Errorf("Throw on fresh generator should propagate, got %v", err)
	}
}

func TestGeneratorCloseCleanExit(t *testing.T) {
	g := NewGenerator("g", "g")
	runGenBody(g, func(yield genYieldFn) error {
		_, err := yield(NewInt(1))
		return err
	})
	_, _ = g.Send(None())
	if err := g.Close(); err != nil {
		t.Errorf("clean close should be nil, got %v", err)
	}
}

func TestGeneratorCloseIgnored(t *testing.T) {
	g := NewGenerator("g", "g")
	runGenBody(g, func(yield genYieldFn) error {
		_, _ = yield(NewInt(1)) // swallow GeneratorExit
		_, _ = yield(NewInt(2))
		return nil
	})
	_, _ = g.Send(None())
	err := g.Close()
	if err == nil || err.Error() != "RuntimeError: generator ignored GeneratorExit" {
		t.Errorf("close on misbehaving body should be RuntimeError, got %v", err)
	}
}

func TestGeneratorCloseUnstarted(t *testing.T) {
	g := NewGenerator("g", "g")
	if err := g.Close(); err != nil {
		t.Errorf("close on unstarted generator should be nil, got %v", err)
	}
	if err := g.Close(); err != nil {
		t.Errorf("double-close should be nil, got %v", err)
	}
}

func TestGeneratorIterIsSelf(t *testing.T) {
	g := NewGenerator("g", "g")
	got, err := GeneratorType.Iter(g)
	if err != nil {
		t.Fatal(err)
	}
	if got != Object(g) {
		t.Error("generator.__iter__ must return self")
	}
}

func TestGeneratorRepr(t *testing.T) {
	g := NewGenerator("foo", "foo")
	s, err := genRepr(g)
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("<generator object foo at %p>", g)
	if s != want {
		t.Errorf("repr = %q, want %q", s, want)
	}
}
