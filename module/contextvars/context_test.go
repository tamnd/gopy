// Gate panel for the contextvars port. Mirrors the cases the spec
// (1663) calls out: round-trip set/get, set/reset, Context.run
// isolation, LookupError on missing, RuntimeError on re-entry,
// snapshot independence, and per-thread isolation across goroutines.

package contextvars

import (
	"sync"
	"testing"

	"github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

func newTS(t *testing.T) *state.Thread {
	t.Helper()
	return state.NewThread()
}

func TestSetGetRoundTrip(t *testing.T) {
	ts := newTS(t)
	cv := NewContextVar("x", nil, false)
	one := objects.NewInt(1)
	if _, err := cv.Set(ts, one); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := cv.Get(ts)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != one {
		t.Fatalf("Get returned %v, want %v", got, one)
	}
}

func TestSetResetRoundTrip(t *testing.T) {
	ts := newTS(t)
	cv := NewContextVar("x", nil, false)
	one := objects.NewInt(1)
	two := objects.NewInt(2)
	if _, err := cv.Set(ts, one); err != nil {
		t.Fatalf("Set 1: %v", err)
	}
	tok, err := cv.Set(ts, two)
	if err != nil {
		t.Fatalf("Set 2: %v", err)
	}
	if err := cv.Reset(ts, tok); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	got, err := cv.Get(ts)
	if err != nil {
		t.Fatalf("Get after reset: %v", err)
	}
	if got != one {
		t.Fatalf("Get after reset returned %v, want %v", got, one)
	}
}

func TestResetTokenSingleUse(t *testing.T) {
	ts := newTS(t)
	cv := NewContextVar("x", nil, false)
	tok, err := cv.Set(ts, objects.NewInt(1))
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := cv.Reset(ts, tok); err != nil {
		t.Fatalf("Reset 1: %v", err)
	}
	errors.Clear(ts)
	if err := cv.Reset(ts, tok); err == nil {
		t.Fatalf("Reset 2: expected error on reused token")
	}
	exc := errors.Occurred(ts)
	if exc == nil || !errors.Match(exc, errors.PyExc_RuntimeError) {
		t.Fatalf("Reset 2: expected RuntimeError, got %v", exc)
	}
}

func TestContextRunIsolation(t *testing.T) {
	ts := newTS(t)
	cv := NewContextVar("x", nil, false)
	if _, err := cv.Set(ts, objects.NewInt(1)); err != nil {
		t.Fatalf("outer Set: %v", err)
	}

	inner := CopyCurrent(ts)
	if _, err := inner.Run(ts, func() (objects.Object, error) {
		_, err := cv.Set(ts, objects.NewInt(99))
		return nil, err
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, err := cv.Get(ts)
	if err != nil {
		t.Fatalf("Get after Run: %v", err)
	}
	v, _ := got.(*objects.Int)
	if got1, ok := v.Int64(); !ok || got1 != 1 {
		t.Fatalf("outer should still see 1, got %v", got)
	}
}

func TestGetMissingRaisesLookupError(t *testing.T) {
	ts := newTS(t)
	cv := NewContextVar("x", nil, false)
	if _, err := cv.Get(ts); err == nil {
		t.Fatalf("expected LookupError, got nil")
	}
	exc := errors.Occurred(ts)
	if exc == nil || !errors.Match(exc, errors.PyExc_LookupError) {
		t.Fatalf("expected LookupError, got %v", exc)
	}
}

func TestGetUsesDefault(t *testing.T) {
	ts := newTS(t)
	def := objects.NewInt(42)
	cv := NewContextVar("x", def, true)
	got, err := cv.Get(ts)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != def {
		t.Fatalf("Get returned %v, want default %v", got, def)
	}
}

func TestRunReentryRaisesRuntimeError(t *testing.T) {
	ts := newTS(t)
	c := NewContext()
	if err := c.Enter(ts); err != nil {
		t.Fatalf("Enter 1: %v", err)
	}
	defer c.Exit(ts)

	if err := c.Enter(ts); err == nil {
		t.Fatalf("Enter 2: expected RuntimeError")
	}
	exc := errors.Occurred(ts)
	if exc == nil || !errors.Match(exc, errors.PyExc_RuntimeError) {
		t.Fatalf("Enter 2: expected RuntimeError, got %v", exc)
	}
}

func TestCopyContextSnapshotsAreIndependent(t *testing.T) {
	ts := newTS(t)
	cv := NewContextVar("x", nil, false)
	if _, err := cv.Set(ts, objects.NewInt(1)); err != nil {
		t.Fatalf("Set: %v", err)
	}

	snap := CopyCurrent(ts)
	if _, err := snap.Run(ts, func() (objects.Object, error) {
		if _, err := cv.Set(ts, objects.NewInt(2)); err != nil {
			return nil, err
		}
		got, err := cv.Get(ts)
		if err != nil {
			return nil, err
		}
		v, _ := got.(*objects.Int)
		if got2, ok := v.Int64(); !ok || got2 != 2 {
			t.Fatalf("inside snap.Run, expected 2, got %v", got)
		}
		return nil, nil
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, err := cv.Get(ts)
	if err != nil {
		t.Fatalf("Get after Run: %v", err)
	}
	v, _ := got.(*objects.Int)
	if got1, ok := v.Int64(); !ok || got1 != 1 {
		t.Fatalf("outer expected 1, got %v", got)
	}
}

func TestPerThreadIsolation(t *testing.T) {
	cv := NewContextVar("x", nil, false)

	var wg sync.WaitGroup
	saw := make([]int64, 4)
	for i := range 4 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ts := state.NewThread()
			val := objects.NewInt(int64(idx + 100))
			if _, err := cv.Set(ts, val); err != nil {
				t.Errorf("goroutine %d Set: %v", idx, err)
				return
			}
			got, err := cv.Get(ts)
			if err != nil {
				t.Errorf("goroutine %d Get: %v", idx, err)
				return
			}
			v, _ := got.(*objects.Int)
			if v == nil {
				t.Errorf("goroutine %d Get returned non-int %v", idx, got)
				return
			}
			n, _ := v.Int64()
			saw[idx] = n
		}(i)
	}
	wg.Wait()

	for i, v := range saw {
		if v != int64(i+100) {
			t.Fatalf("goroutine %d: saw %d, want %d", i, v, i+100)
		}
	}
}

func TestResetWrongContextRaisesValueError(t *testing.T) {
	ts := newTS(t)
	cv := NewContextVar("x", nil, false)
	tok, err := cv.Set(ts, objects.NewInt(1))
	if err != nil {
		t.Fatalf("Set: %v", err)
	}

	other := NewContext()
	if err := other.Enter(ts); err != nil {
		t.Fatalf("Enter: %v", err)
	}
	defer other.Exit(ts)

	if err := cv.Reset(ts, tok); err == nil {
		t.Fatalf("expected ValueError on cross-context reset")
	}
	exc := errors.Occurred(ts)
	if exc == nil || !errors.Match(exc, errors.PyExc_ValueError) {
		t.Fatalf("expected ValueError, got %v", exc)
	}
}

func TestContextEqAndLen(t *testing.T) {
	ts := newTS(t)
	cv1 := NewContextVar("x", nil, false)
	cv2 := NewContextVar("y", nil, false)
	if _, err := cv1.Set(ts, objects.NewInt(1)); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if _, err := cv2.Set(ts, objects.NewInt(2)); err != nil {
		t.Fatalf("Set: %v", err)
	}
	cur := CopyCurrent(ts)
	if cur.Len() != 2 {
		t.Fatalf("Len: got %d, want 2", cur.Len())
	}

	other := CopyCurrent(ts)
	eq, err := cur.Eq(other)
	if err != nil {
		t.Fatalf("Eq: %v", err)
	}
	if !eq {
		t.Fatalf("two snapshots of the same context should be equal")
	}
}

func TestTokenOldValueMissing(t *testing.T) {
	ts := newTS(t)
	cv := NewContextVar("x", nil, false)
	tok, err := cv.Set(ts, objects.NewInt(1))
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if tok.OldValue() != Missing {
		t.Fatalf("OldValue: got %v, want Missing", tok.OldValue())
	}
}
