package objects

import (
	"errors"
	"fmt"
	"testing"
)

func runCoroBody(c *Coroutine, body func(yield genYieldFn) error) {
	go func() {
		first := <-c.SendCh
		if first.Err != nil {
			c.YieldCh <- GenMsg{Err: ErrStopIteration}
			return
		}
		yieldFn := func(v Object) (Object, error) {
			c.YieldCh <- GenMsg{Val: v}
			m := <-c.SendCh
			if m.Err != nil {
				return nil, m.Err
			}
			return m.Val, nil
		}
		err := body(yieldFn)
		if err != nil && !errors.Is(err, ErrStopIteration) {
			c.YieldCh <- GenMsg{Err: err}
		} else {
			c.YieldCh <- GenMsg{Err: ErrStopIteration}
		}
	}()
}

func TestCoroutineSend(t *testing.T) {
	c := NewCoroutine("co")
	runCoroBody(c, func(yield genYieldFn) error {
		if _, err := yield(NewInt(11)); err != nil {
			return err
		}
		if _, err := yield(NewInt(22)); err != nil {
			return err
		}
		return nil
	})
	v, err := c.Send(None())
	if err != nil {
		t.Fatal(err)
	}
	if x, _ := v.(*Int).Int64(); x != 11 {
		t.Errorf("first yield = %d, want 11", x)
	}
	v, _ = c.Send(None())
	if x, _ := v.(*Int).Int64(); x != 22 {
		t.Errorf("second yield = %d, want 22", x)
	}
	if _, err := c.Send(None()); !errors.Is(err, ErrStopIteration) {
		t.Errorf("exhausted coroutine should StopIteration, got %v", err)
	}
}

func TestCoroutineSendNonNoneToFresh(t *testing.T) {
	c := NewCoroutine("co")
	runCoroBody(c, func(yield genYieldFn) error { return nil })
	_, err := c.Send(NewInt(1))
	if err == nil || err.Error() != "TypeError: can't send non-None value to a just-started coroutine" {
		t.Errorf("want coroutine-specific TypeError, got %v", err)
	}
}

func TestCoroutineThrow(t *testing.T) {
	c := NewCoroutine("co")
	runCoroBody(c, func(yield genYieldFn) error {
		_, err := yield(NewInt(1))
		return err
	})
	_, _ = c.Send(None())
	_, err := c.Throw(fmt.Errorf("kaboom"))
	if err == nil || err.Error() != "kaboom" {
		t.Errorf("throw should surface, got %v", err)
	}
}

func TestCoroutineCloseClean(t *testing.T) {
	c := NewCoroutine("co")
	runCoroBody(c, func(yield genYieldFn) error {
		_, err := yield(NewInt(1))
		return err
	})
	_, _ = c.Send(None())
	if err := c.Close(); err != nil {
		t.Errorf("clean close: %v", err)
	}
}

func TestCoroutineCloseIgnored(t *testing.T) {
	c := NewCoroutine("co")
	runCoroBody(c, func(yield genYieldFn) error {
		_, _ = yield(NewInt(1))
		_, _ = yield(NewInt(2))
		return nil
	})
	_, _ = c.Send(None())
	err := c.Close()
	if err == nil || err.Error() != "RuntimeError: coroutine ignored GeneratorExit" {
		t.Errorf("misbehaving close: got %v", err)
	}
}

func TestCoroutineAwait(t *testing.T) {
	c := NewCoroutine("co")
	runCoroBody(c, func(yield genYieldFn) error {
		_, err := yield(NewInt(7))
		return err
	})
	w := c.Await()
	if w.Type() != CoroAwaitType {
		t.Fatalf("Await must return %s, got %s", CoroAwaitType.Name, w.Type().Name)
	}
	v, err := CoroAwaitType.IterNext(w)
	if err != nil {
		t.Fatal(err)
	}
	if x, _ := v.(*Int).Int64(); x != 7 {
		t.Errorf("await iter = %d, want 7", x)
	}
	if _, err := CoroAwaitType.IterNext(w); !errors.Is(err, ErrStopIteration) {
		t.Errorf("after exhaustion want StopIteration, got %v", err)
	}
}

func TestCoroutineRepr(t *testing.T) {
	c := NewCoroutine("foo")
	s, err := coroRepr(c)
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("<coroutine object foo at %p>", c)
	if s != want {
		t.Errorf("repr = %q want %q", s, want)
	}
}
