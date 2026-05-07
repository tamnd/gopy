// 16-bit countdown counters with exponential backoff. The
// adaptive specializer uses one counter per adaptive instruction;
// a counter that triggers calls into the specializer, a
// specialize miss restarts the counter with a longer delay.
//
// The 16-bit counter is structured as a 12-bit unsigned value
// above a 4-bit backoff field. Resetting the counter increments
// the backoff (capped at 12) and seeds the value with a bit mask
// of 2**backoff - 1. The counter triggers when the value reaches
// zero, except when the entire counter equals UnreachableBackoff
// (0xF in the low nibble), which is the never-trigger sentinel.
//
// CPython: Include/internal/pycore_backoff.h

package specialize

// Backoff layout constants.
//
// CPython: Include/internal/pycore_backoff.h:34
const (
	BackoffBits        = 4
	MaxBackoff         = 12
	UnreachableBackoff = 15
)

// BackoffCounter packs a 12-bit value above a 4-bit backoff field.
//
// CPython: Include/internal/pycore_structs.h _Py_BackoffCounter
type BackoffCounter struct {
	ValueAndBackoff uint16
}

// MakeBackoffCounter packs value and backoff into a fresh counter.
// value must fit in 12 bits, backoff must fit in 4.
//
// CPython: Include/internal/pycore_backoff.h:44 make_backoff_counter
func MakeBackoffCounter(value, backoff uint16) BackoffCounter {
	if value > 0xFFF {
		panic("specialize: backoff value overflow")
	}
	if backoff > 15 {
		panic("specialize: backoff field overflow")
	}
	return BackoffCounter{ValueAndBackoff: (value << BackoffBits) | backoff}
}

// ForgeBackoffCounter wraps a raw 16-bit pattern. Used when reading
// an existing counter out of a bytecode cache cell.
//
// CPython: Include/internal/pycore_backoff.h:54 forge_backoff_counter
func ForgeBackoffCounter(raw uint16) BackoffCounter {
	return BackoffCounter{ValueAndBackoff: raw}
}

// IsUnreachable reports whether the counter is the
// never-trigger sentinel (value 0, backoff 15).
//
// CPython: Include/internal/pycore_backoff.h:38 is_unreachable_backoff_counter
func IsUnreachable(c BackoffCounter) bool {
	return c.ValueAndBackoff == UnreachableBackoff
}

// RestartBackoffCounter resets a counter after a specialize miss.
// The backoff field grows by one (capped at MaxBackoff) and the
// value is reseeded to 2**backoff - 1.
//
// CPython: Include/internal/pycore_backoff.h:62 restart_backoff_counter
func RestartBackoffCounter(c BackoffCounter) BackoffCounter {
	if IsUnreachable(c) {
		panic("specialize: restart on unreachable counter")
	}
	backoff := c.ValueAndBackoff & 15
	if backoff < MaxBackoff {
		return MakeBackoffCounter((1<<(backoff+1))-1, backoff+1)
	}
	return MakeBackoffCounter((1<<MaxBackoff)-1, MaxBackoff)
}

// PauseBackoffCounter bumps the value by 1<<BackoffBits to push
// the next trigger out by one tick without changing the backoff
// field. Used when a specialize attempt should be retried later
// without escalating the backoff.
//
// CPython: Include/internal/pycore_backoff.h:75 pause_backoff_counter
func PauseBackoffCounter(c BackoffCounter) BackoffCounter {
	return BackoffCounter{ValueAndBackoff: c.ValueAndBackoff | (1 << BackoffBits)}
}

// AdvanceBackoffCounter ticks the value down by one. Called every
// time the matching adaptive opcode executes.
//
// CPython: Include/internal/pycore_backoff.h:83 advance_backoff_counter
func AdvanceBackoffCounter(c BackoffCounter) BackoffCounter {
	return BackoffCounter{ValueAndBackoff: c.ValueAndBackoff - (1 << BackoffBits)}
}

// BackoffCounterTriggers reports whether the value is zero and
// the counter is not the unreachable sentinel.
//
// CPython: Include/internal/pycore_backoff.h:91 backoff_counter_triggers
func BackoffCounterTriggers(c BackoffCounter) bool {
	return c.ValueAndBackoff < UnreachableBackoff
}

// Initial JUMP_BACKWARD counter shape, used by the Tier-2 trace
// projector to pick hot loops.
//
// CPython: Include/internal/pycore_backoff.h:100
const (
	JumpBackwardInitialValue   = 4095
	JumpBackwardInitialBackoff = 12
)

// InitialJumpBackoffCounter returns the seed for a JUMP_BACKWARD
// instruction's counter slot.
//
// CPython: Include/internal/pycore_backoff.h:102 initial_jump_backoff_counter
func InitialJumpBackoffCounter() BackoffCounter {
	return MakeBackoffCounter(JumpBackwardInitialValue, JumpBackwardInitialBackoff)
}

// Initial side-exit temperature.
//
// CPython: Include/internal/pycore_backoff.h:113
const (
	SideExitInitialValue   = 4095
	SideExitInitialBackoff = 12
)

// InitialSideExitBackoffCounter returns the seed for a Tier-2
// side-exit temperature counter.
//
// CPython: Include/internal/pycore_backoff.h:116 initial_temperature_backoff_counter
func InitialSideExitBackoffCounter() BackoffCounter {
	return MakeBackoffCounter(SideExitInitialValue, SideExitInitialBackoff)
}

// InitialUnreachableBackoffCounter returns the never-trigger
// sentinel.
//
// CPython: Include/internal/pycore_backoff.h:124 initial_unreachable_backoff_counter
func InitialUnreachableBackoffCounter() BackoffCounter {
	return MakeBackoffCounter(0, UnreachableBackoff)
}

// Adaptive counter shapes used by the specializer. Warmup is the
// shape Quicken stamps into every fresh cache slot. Cooldown is
// the shape used after a successful specialize so the next miss
// has time to settle before a re-specialize attempt.
//
// CPython: Python/specialize.c top of file (adaptive_counter_warmup
// / adaptive_counter_cooldown helpers)
func AdaptiveCounterWarmup() BackoffCounter {
	return MakeBackoffCounter(1, 1)
}

func AdaptiveCounterCooldown() BackoffCounter {
	return MakeBackoffCounter(52, 0)
}

// AdaptiveCounterBackoff computes the next counter after a
// specialize miss. Mirrors RestartBackoffCounter; kept under a
// distinct name so the call sites read like the CPython source.
//
// CPython: Python/specialize.c adaptive_counter_backoff
func AdaptiveCounterBackoff(c BackoffCounter) BackoffCounter {
	return RestartBackoffCounter(c)
}
