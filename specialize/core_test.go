package specialize

import (
	"encoding/binary"
	"testing"

	"github.com/tamnd/gopy/compile"
)

func newAdaptiveBuf(op compile.Opcode) []byte {
	buf := make([]byte, 2*(1+CacheCount(op)))
	buf[0] = byte(op)
	return buf
}

func TestSetOpcodeWritesByte(t *testing.T) {
	buf := newAdaptiveBuf(compile.LOAD_ATTR)
	if !SetOpcode(buf, 0, compile.LOAD_ATTR_INSTANCE_VALUE) {
		t.Fatalf("SetOpcode reported lost race on plain opcode")
	}
	if got := compile.Opcode(buf[0]); got != compile.LOAD_ATTR_INSTANCE_VALUE {
		t.Fatalf("opcode byte: got %s want LOAD_ATTR_INSTANCE_VALUE",
			got.Name())
	}
}

func TestSetOpcodeRefusesInstrumented(t *testing.T) {
	buf := []byte{byte(compile.INSTRUMENTED_CALL), 0, 0, 0, 0, 0, 0, 0}
	if SetOpcode(buf, 0, compile.CALL_PY_EXACT_ARGS) {
		t.Fatalf("SetOpcode returned true on instrumented slot")
	}
	if got := compile.Opcode(buf[0]); got != compile.INSTRUMENTED_CALL {
		t.Fatalf("instrumented byte clobbered: got %s", got.Name())
	}
}

func TestStoreLoadCounter(t *testing.T) {
	buf := newAdaptiveBuf(compile.LOAD_GLOBAL)
	StoreCounter(buf, 0, AdaptiveCounterCooldown())
	got := LoadCounter(buf, 0)
	if got != AdaptiveCounterCooldown() {
		t.Fatalf("counter round-trip: got %v want %v",
			got, AdaptiveCounterCooldown())
	}
	// Cross-check the raw little-endian write.
	raw := binary.LittleEndian.Uint16(buf[2:])
	if raw != AdaptiveCounterCooldown().ValueAndBackoff {
		t.Fatalf("raw cell: got 0x%04x want 0x%04x",
			raw, AdaptiveCounterCooldown().ValueAndBackoff)
	}
}

func TestSpecializeSetsCooldown(t *testing.T) {
	buf := newAdaptiveBuf(compile.LOAD_ATTR)
	Specialize(buf, 0, compile.LOAD_ATTR_INSTANCE_VALUE)
	if got := compile.Opcode(buf[0]); got != compile.LOAD_ATTR_INSTANCE_VALUE {
		t.Fatalf("opcode: got %s", got.Name())
	}
	if got := LoadCounter(buf, 0); got != AdaptiveCounterCooldown() {
		t.Fatalf("counter after specialize: got %v want cooldown", got)
	}
}

func TestUnspecializeRestoresParent(t *testing.T) {
	buf := newAdaptiveBuf(compile.LOAD_ATTR)
	buf[0] = byte(compile.LOAD_ATTR_INSTANCE_VALUE)
	StoreCounter(buf, 0, AdaptiveCounterCooldown())

	Unspecialize(buf, 0)
	if got := compile.Opcode(buf[0]); got != compile.LOAD_ATTR {
		t.Fatalf("opcode after unspecialize: got %s want LOAD_ATTR",
			got.Name())
	}
	got := LoadCounter(buf, 0)
	want := AdaptiveCounterBackoff(AdaptiveCounterCooldown())
	if got != want {
		t.Fatalf("counter after unspecialize: got %v want %v", got, want)
	}
}
