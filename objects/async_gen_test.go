package objects

import (
	"errors"
	"fmt"
	"testing"
)

// testStopIterValue stands in for the StopIteration(value) that the
// await protocol raises when an async generator yields. The real
// errors package wires AsyncGenStopIterationHook to build a typed
// StopIteration carrying the value; objects/ tests cannot import that
// package, so a local hook reproduces the value-carrying surface that
// async_gen_unwrap_value drives through.
type testStopIterValue struct{ v Object }

func (e *testStopIterValue) Error() string { return "StopIteration" }

func init() {
	AsyncGenStopIterationHook = func(value Object) error {
		if value == nil {
			value = None()
		}
		return &testStopIterValue{v: value}
	}
}

// asyncYieldValue drains one asend/anext step and extracts the value
// the body yielded, which the await protocol surfaces through
// StopIteration rather than as a direct return.
func asyncYieldValue(t *testing.T, a Object) Object {
	t.Helper()
	_, err := AsyncGenASendType.IterNext(a)
	var sv *testStopIterValue
	if !errors.As(err, &sv) {
		t.Fatalf("anext should raise StopIteration(value), got %v", err)
	}
	return sv.v
}

func runAsyncGenBody(g *AsyncGenerator, body func(yield genYieldFn) error) {
	go func() {
		first := <-g.SendCh
		if first.Err != nil {
			g.YieldCh <- GenMsg{Err: ErrStopIteration}
			return
		}
		yieldFn := func(v Object) (Object, error) {
			// Mirror the ASYNC_GEN_WRAP intrinsic the real eval loop
			// applies to every async-generator yield: each value the
			// body surfaces is wrapped so async_gen_unwrap_value can
			// tell an async yield apart from an intermediate await and
			// clear ag_running_async between anext() steps.
			g.YieldCh <- GenMsg{Val: NewAsyncGenWrappedValue(v)}
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

func TestAsyncGeneratorAnext(t *testing.T) {
	g := NewAsyncGenerator("ag")
	runAsyncGenBody(g, func(yield genYieldFn) error {
		if _, err := yield(NewInt(1)); err != nil {
			return err
		}
		if _, err := yield(NewInt(2)); err != nil {
			return err
		}
		return nil
	})

	a := g.Anext()
	if a.Type() != AsyncGenASendType {
		t.Fatalf("anext type = %s, want async_generator_asend", a.Type().Name)
	}
	v := asyncYieldValue(t, a)
	if x, _ := v.(*Int).Int64(); x != 1 {
		t.Errorf("first anext = %d, want 1", x)
	}

	a2 := g.Anext()
	v2 := asyncYieldValue(t, a2)
	if x, _ := v2.(*Int).Int64(); x != 2 {
		t.Errorf("second anext = %d, want 2", x)
	}

	a3 := g.Anext()
	if _, err := AsyncGenASendType.IterNext(a3); !errors.Is(err, ErrStopAsyncIteration) {
		t.Errorf("exhausted async gen should StopAsyncIteration, got %v", err)
	}
}

func TestAsyncGeneratorAsendValue(t *testing.T) {
	g := NewAsyncGenerator("ag")
	runAsyncGenBody(g, func(yield genYieldFn) error {
		v, err := yield(NewInt(0))
		if err != nil {
			return err
		}
		if _, err := yield(v); err != nil {
			return err
		}
		return nil
	})
	a := g.Anext()
	asyncYieldValue(t, a)
	a2 := g.Asend(NewInt(99))
	v := asyncYieldValue(t, a2)
	if x, _ := v.(*Int).Int64(); x != 99 {
		t.Errorf("asend round-trip = %d, want 99", x)
	}
}

func TestAsyncGeneratorAthrow(t *testing.T) {
	g := NewAsyncGenerator("ag")
	runAsyncGenBody(g, func(yield genYieldFn) error {
		_, err := yield(NewInt(1))
		return err
	})
	a := g.Anext()
	_, _ = AsyncGenASendType.IterNext(a)
	at := g.Athrow(fmt.Errorf("ouch"))
	_, err := AsyncGenAThrowType.IterNext(at)
	if err == nil || err.Error() != "ouch" {
		t.Errorf("athrow should surface error, got %v", err)
	}
}

func TestAsyncGeneratorAcloseClean(t *testing.T) {
	g := NewAsyncGenerator("ag")
	runAsyncGenBody(g, func(yield genYieldFn) error {
		_, err := yield(NewInt(1))
		return err
	})
	a := g.Anext()
	_, _ = AsyncGenASendType.IterNext(a)
	ac := g.Aclose()
	_, err := AsyncGenAThrowType.IterNext(ac)
	if !errors.Is(err, ErrStopAsyncIteration) {
		t.Errorf("aclose should end with StopAsyncIteration, got %v", err)
	}
}

func TestAsyncGeneratorAcloseIgnored(t *testing.T) {
	g := NewAsyncGenerator("ag")
	runAsyncGenBody(g, func(yield genYieldFn) error {
		_, _ = yield(NewInt(1))
		_, _ = yield(NewInt(2))
		return nil
	})
	a := g.Anext()
	_, _ = AsyncGenASendType.IterNext(a)
	ac := g.Aclose()
	_, err := AsyncGenAThrowType.IterNext(ac)
	if err == nil || err.Error() != "RuntimeError: async generator ignored GeneratorExit" {
		t.Errorf("aclose with misbehaving body: got %v", err)
	}
}

func TestAsyncGeneratorRepr(t *testing.T) {
	g := NewAsyncGenerator("foo")
	s, err := asyncGenRepr(g)
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("<async_generator object foo at %p>", g)
	if s != want {
		t.Errorf("repr = %q, want %q", s, want)
	}
}

func TestAsyncGeneratorSendNonNoneToFresh(t *testing.T) {
	g := NewAsyncGenerator("ag")
	runAsyncGenBody(g, func(yield genYieldFn) error { return nil })
	_, err := g.Send(NewInt(1))
	if err == nil || err.Error() !=
		"TypeError: can't send non-None value to a just-started async generator" {
		t.Errorf("want async-gen-specific TypeError, got %v", err)
	}
}
