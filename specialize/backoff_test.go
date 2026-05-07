package specialize

import "testing"

// CPython: Include/internal/pycore_backoff.h
// The whole header is small enough to pin every helper directly.

func TestMakeBackoffCounterPacks(t *testing.T) {
	c := MakeBackoffCounter(1, 1)
	if c.ValueAndBackoff != (1<<BackoffBits)|1 {
		t.Fatalf("warmup pattern: got 0x%04x want 0x%04x",
			c.ValueAndBackoff, (1<<BackoffBits)|1)
	}
	c = MakeBackoffCounter(0xFFF, 15)
	if c.ValueAndBackoff != 0xFFFF {
		t.Fatalf("max pattern: got 0x%04x want 0xFFFF", c.ValueAndBackoff)
	}
}

func TestMakeBackoffCounterRejectsOverflow(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on value overflow")
		}
	}()
	_ = MakeBackoffCounter(0x1000, 0)
}

func TestForgeAndIsUnreachable(t *testing.T) {
	if !IsUnreachable(InitialUnreachableBackoffCounter()) {
		t.Fatal("initial unreachable counter should be unreachable")
	}
	if IsUnreachable(AdaptiveCounterWarmup()) {
		t.Fatal("warmup counter must not be unreachable")
	}
	c := ForgeBackoffCounter(0x000F)
	if !IsUnreachable(c) {
		t.Fatalf("forged 0x000F should read as unreachable")
	}
}

func TestRestartBackoffCounterGrowsBackoffAndReseedsValue(t *testing.T) {
	c := AdaptiveCounterWarmup() // value=1, backoff=1
	c = RestartBackoffCounter(c)
	gotValue := c.ValueAndBackoff >> BackoffBits
	gotBackoff := c.ValueAndBackoff & 0xF
	if gotBackoff != 2 {
		t.Fatalf("backoff after restart from 1: got %d want 2", gotBackoff)
	}
	if gotValue != (1<<2)-1 {
		t.Fatalf("value after restart: got %d want %d", gotValue, (1<<2)-1)
	}
}

func TestRestartBackoffCounterCapsAtMaxBackoff(t *testing.T) {
	c := MakeBackoffCounter((1<<MaxBackoff)-1, MaxBackoff)
	c = RestartBackoffCounter(c)
	gotBackoff := c.ValueAndBackoff & 0xF
	if gotBackoff != MaxBackoff {
		t.Fatalf("backoff capped: got %d want %d", gotBackoff, MaxBackoff)
	}
	gotValue := c.ValueAndBackoff >> BackoffBits
	if gotValue != (1<<MaxBackoff)-1 {
		t.Fatalf("value at cap: got %d want %d", gotValue, (1<<MaxBackoff)-1)
	}
}

func TestRestartBackoffCounterPanicsOnUnreachable(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic restarting unreachable counter")
		}
	}()
	_ = RestartBackoffCounter(InitialUnreachableBackoffCounter())
}

func TestPauseBackoffCounterAddsOneToValue(t *testing.T) {
	c := MakeBackoffCounter(0, 3)
	got := PauseBackoffCounter(c)
	wantValue := uint16(1)
	wantBackoff := uint16(3)
	if (got.ValueAndBackoff >> BackoffBits) != wantValue {
		t.Fatalf("paused value: got %d want %d",
			got.ValueAndBackoff>>BackoffBits, wantValue)
	}
	if (got.ValueAndBackoff & 0xF) != wantBackoff {
		t.Fatalf("paused backoff: got %d want %d",
			got.ValueAndBackoff&0xF, wantBackoff)
	}
}

func TestAdvanceBackoffCounterDecrementsValue(t *testing.T) {
	c := MakeBackoffCounter(5, 2)
	got := AdvanceBackoffCounter(c)
	if (got.ValueAndBackoff >> BackoffBits) != 4 {
		t.Fatalf("advanced value: got %d want 4",
			got.ValueAndBackoff>>BackoffBits)
	}
	if (got.ValueAndBackoff & 0xF) != 2 {
		t.Fatalf("advanced backoff changed: got %d want 2",
			got.ValueAndBackoff&0xF)
	}
}

func TestBackoffCounterTriggersAtZeroValue(t *testing.T) {
	c := MakeBackoffCounter(1, 1)
	if BackoffCounterTriggers(c) {
		t.Fatal("counter with value=1 should not trigger")
	}
	c = AdvanceBackoffCounter(c)
	if !BackoffCounterTriggers(c) {
		t.Fatalf("counter at value=0, backoff=1 should trigger (raw=0x%04x)",
			c.ValueAndBackoff)
	}
}

func TestBackoffCounterTriggersFalseOnUnreachable(t *testing.T) {
	c := InitialUnreachableBackoffCounter()
	if BackoffCounterTriggers(c) {
		t.Fatal("unreachable counter should never trigger")
	}
}

func TestInitialJumpAndSideExitShapes(t *testing.T) {
	j := InitialJumpBackoffCounter()
	if (j.ValueAndBackoff >> BackoffBits) != JumpBackwardInitialValue {
		t.Fatalf("jump value: got %d want %d",
			j.ValueAndBackoff>>BackoffBits, JumpBackwardInitialValue)
	}
	if (j.ValueAndBackoff & 0xF) != JumpBackwardInitialBackoff {
		t.Fatalf("jump backoff: got %d want %d",
			j.ValueAndBackoff&0xF, JumpBackwardInitialBackoff)
	}
	s := InitialSideExitBackoffCounter()
	if (s.ValueAndBackoff >> BackoffBits) != SideExitInitialValue {
		t.Fatalf("side-exit value: got %d want %d",
			s.ValueAndBackoff>>BackoffBits, SideExitInitialValue)
	}
}

func TestAdaptiveWarmupAndCooldownShapes(t *testing.T) {
	w := AdaptiveCounterWarmup()
	if (w.ValueAndBackoff>>BackoffBits) != 1 || (w.ValueAndBackoff&0xF) != 1 {
		t.Fatalf("warmup shape: raw 0x%04x", w.ValueAndBackoff)
	}
	c := AdaptiveCounterCooldown()
	if (c.ValueAndBackoff>>BackoffBits) != 52 || (c.ValueAndBackoff&0xF) != 0 {
		t.Fatalf("cooldown shape: raw 0x%04x", c.ValueAndBackoff)
	}
}
