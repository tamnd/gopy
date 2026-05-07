package specialize

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

// adaptiveBuf returns a fresh bytecode buffer holding op at instr=0
// followed by its full inline-cache slot count.
func adaptiveBuf(op compile.Opcode) []byte {
	buf := make([]byte, 2*(1+CacheCount(op)))
	buf[0] = byte(op)
	return buf
}

// TestLoadGlobalModuleHit covers the case where the name resolves
// in globals. The bytecode must rewrite to LOAD_GLOBAL_MODULE and
// the cache must hold the globals keys version + index.
func TestLoadGlobalModuleHit(t *testing.T) {
	g := objects.NewDict()
	b := objects.NewDict()
	name := objects.NewStr("x").(*objects.Unicode)
	if err := g.SetItem(name, objects.NewInt(1)); err != nil {
		t.Fatalf("seed globals: %v", err)
	}

	buf := adaptiveBuf(compile.LOAD_GLOBAL)
	LoadGlobal(g, b, buf, 0, name)

	if got := compile.Opcode(buf[0]); got != compile.LOAD_GLOBAL_MODULE {
		t.Fatalf("opcode: got %s want LOAD_GLOBAL_MODULE", got.Name())
	}
	if v := CacheCell(buf, 0, 2); uint32(v) != g.GetKeysVersion() {
		t.Fatalf("module_keys_version: got 0x%04x want 0x%08x",
			v, g.GetKeysVersion())
	}
	idx, _ := g.LookupString(name)
	if v := CacheCell(buf, 0, 4); int(v) != idx {
		t.Fatalf("index: got %d want %d", v, idx)
	}
}

// TestLoadGlobalBuiltinHit covers the fall-through to builtins.
func TestLoadGlobalBuiltinHit(t *testing.T) {
	g := objects.NewDict()
	b := objects.NewDict()
	name := objects.NewStr("len").(*objects.Unicode)
	if err := b.SetItem(name, objects.NewInt(99)); err != nil {
		t.Fatalf("seed builtins: %v", err)
	}

	buf := adaptiveBuf(compile.LOAD_GLOBAL)
	LoadGlobal(g, b, buf, 0, name)

	if got := compile.Opcode(buf[0]); got != compile.LOAD_GLOBAL_BUILTIN {
		t.Fatalf("opcode: got %s want LOAD_GLOBAL_BUILTIN", got.Name())
	}
	if v := CacheCell(buf, 0, 2); uint32(v) != g.GetKeysVersion() {
		t.Fatalf("module_keys_version: got 0x%04x", v)
	}
	if v := CacheCell(buf, 0, 3); uint32(v) != b.GetKeysVersion() {
		t.Fatalf("builtin_keys_version: got 0x%04x", v)
	}
}

// TestLoadGlobalUnspecializeOnNonDict pins the fallback when
// globals is not a real dict. CPython unspecializes the slot.
func TestLoadGlobalUnspecializeOnNonDict(t *testing.T) {
	buf := adaptiveBuf(compile.LOAD_GLOBAL)
	StoreCounter(buf, 0, AdaptiveCounterWarmup())

	notDict := objects.NewInt(1)
	name := objects.NewStr("x").(*objects.Unicode)
	LoadGlobal(notDict, objects.NewDict(), buf, 0, name)

	if got := compile.Opcode(buf[0]); got != compile.LOAD_GLOBAL {
		t.Fatalf("opcode: got %s want LOAD_GLOBAL after unspecialize",
			got.Name())
	}
	// The counter should have been bumped through the backoff path.
	if got := LoadCounter(buf, 0); got == AdaptiveCounterWarmup() {
		t.Fatalf("counter not advanced after unspecialize: %v", got)
	}
}
