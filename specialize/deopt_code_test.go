package specialize

import (
	"bytes"
	"testing"

	"github.com/tamnd/gopy/compile"
)

// Non-adaptive opcodes round-trip unchanged: deopt is a fixed point.
//
// CPython: Objects/codeobject.c:2293 deopt_code (no-op on un-specialized streams)
func TestDeoptCode_NonAdaptiveIdentity(t *testing.T) {
	code := []byte{
		byte(compile.LOAD_CONST), 0x01,
		byte(compile.LOAD_CONST), 0x02,
		byte(compile.BINARY_OP), 0x00,
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // BINARY_OP cache (5 codeunits)
		byte(compile.RETURN_VALUE), 0x00,
	}
	orig := append([]byte(nil), code...)
	got := DeoptCode(code)
	if !bytes.Equal(got, orig) {
		t.Fatalf("DeoptCode on adaptive-parent stream mutated output:\n got=%v\nwant=%v", got, orig)
	}
	if !bytes.Equal(code, orig) {
		t.Fatalf("DeoptCode mutated input (should be pure)")
	}
}

// A specialized opcode is rewritten to its adaptive parent and the
// trailing cache cells are zeroed. oparg is preserved.
//
// CPython: Objects/codeobject.c:3440 deopt_code_unit (preserves oparg, swaps opcode, zero cache)
func TestDeoptCode_SpecializedRewrite(t *testing.T) {
	// LOAD_ATTR has 9 cache codeunits. LOAD_ATTR_SLOT is a specialized
	// variant whose parent is LOAD_ATTR.
	const cacheUnits = 9
	cache := make([]byte, 2*cacheUnits)
	for i := range cache {
		cache[i] = 0xAB // garbage that must be zeroed by deopt
	}
	code := []byte{byte(compile.LOAD_ATTR_SLOT), 0x42}
	code = append(code, cache...)
	code = append(code, byte(compile.RETURN_VALUE), 0x00)

	got := DeoptCode(code)

	if compile.Opcode(got[0]) != compile.LOAD_ATTR {
		t.Fatalf("opcode not deopted: got %s want LOAD_ATTR", compile.Opcode(got[0]).Name())
	}
	if got[1] != 0x42 {
		t.Fatalf("oparg mutated: got 0x%x want 0x42", got[1])
	}
	for i := 2; i < 2+2*cacheUnits; i++ {
		if got[i] != 0 {
			t.Fatalf("cache cell at byte %d not zeroed: got 0x%x", i, got[i])
		}
	}
	if compile.Opcode(got[2+2*cacheUnits]) != compile.RETURN_VALUE {
		t.Fatalf("trailing opcode corrupted: got %s want RETURN_VALUE",
			compile.Opcode(got[2+2*cacheUnits]).Name())
	}
	if got[2+2*cacheUnits+1] != 0x00 {
		t.Fatalf("trailing oparg corrupted: got 0x%x", got[2+2*cacheUnits+1])
	}
}

// Deopt is idempotent: a second pass over already-deopted bytes is a
// no-op. CPython relies on this when _PyCode_GetCode is called twice
// for the same code object.
//
// CPython: Objects/codeobject.c:2293 deopt_code (fixed point on adaptive parents)
func TestDeoptCode_Idempotent(t *testing.T) {
	code := []byte{
		byte(compile.LOAD_ATTR_MODULE), 0x07,
		0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
		0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
		byte(compile.CALL_PY_EXACT_ARGS), 0x02,
		0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, // CALL has 3 cache codeunits
		byte(compile.RETURN_VALUE), 0x00,
	}
	first := DeoptCode(code)
	second := DeoptCode(first)
	if !bytes.Equal(first, second) {
		t.Fatalf("deopt not idempotent:\n  first=%v\n second=%v", first, second)
	}
	// And the first pass should have produced the canonical adaptive
	// form: LOAD_ATTR + zeroed cache, CALL + zeroed cache.
	if compile.Opcode(first[0]) != compile.LOAD_ATTR {
		t.Fatalf("first[0]: got %s want LOAD_ATTR", compile.Opcode(first[0]).Name())
	}
	if compile.Opcode(first[2+2*9]) != compile.CALL {
		t.Fatalf("first[CALL slot]: got %s want CALL", compile.Opcode(first[2+2*9]).Name())
	}
}

// Empty and sub-codeunit inputs are no-ops. CPython's deopt_code uses
// `size - 1` as the loop bound; on size < 1 it iterates zero times.
//
// CPython: Objects/codeobject.c:2297 (size = Py_SIZE / sizeof(_Py_CODEUNIT))
func TestDeoptCode_ShortInput(t *testing.T) {
	for _, in := range [][]byte{nil, {}, {0x42}} {
		got := DeoptCode(in)
		if !bytes.Equal(got, in) {
			t.Fatalf("short input mutated: in=%v got=%v", in, got)
		}
	}
}

// DeoptCode returns a fresh buffer; the caller-supplied slice is left
// untouched. The marshal path relies on this so deopting does not
// mutate the in-memory adaptive bytes.
func TestDeoptCode_DoesNotMutateInput(t *testing.T) {
	code := []byte{
		byte(compile.LOAD_ATTR_SLOT), 0x42,
		0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88,
		0x99, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x10, 0x20, 0x30,
	}
	snapshot := append([]byte(nil), code...)
	_ = DeoptCode(code)
	if !bytes.Equal(code, snapshot) {
		t.Fatalf("DeoptCode mutated its input:\n got=%v\nwant=%v", code, snapshot)
	}
}

// DeoptCodeInPlace edits the supplied buffer directly. Used by callers
// (marshal does not need this; reserved for callers that already own a
// fresh copy).
func TestDeoptCodeInPlace(t *testing.T) {
	code := []byte{
		byte(compile.LOAD_ATTR_SLOT), 0x05,
		0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x11, 0x22,
		0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0x00, 0x11, 0x22,
	}
	DeoptCodeInPlace(code)
	if compile.Opcode(code[0]) != compile.LOAD_ATTR {
		t.Fatalf("in-place deopt didn't rewrite opcode: got %s",
			compile.Opcode(code[0]).Name())
	}
	if code[1] != 0x05 {
		t.Fatalf("in-place deopt clobbered oparg: got 0x%x", code[1])
	}
	for i := 2; i < len(code); i++ {
		if code[i] != 0 {
			t.Fatalf("in-place deopt left non-zero cache byte at %d: 0x%x", i, code[i])
		}
	}
}

// A specialized opcode at the very end (no room for trailing cache)
// still gets its opcode swapped; the cache-zero loop bound clamps to
// len(code). Avoids panics on truncated streams.
func TestDeoptCode_TruncatedCache(t *testing.T) {
	// LOAD_ATTR_SLOT expects 9 cache codeunits; give it only 2.
	code := []byte{
		byte(compile.LOAD_ATTR_SLOT), 0x09,
		0xAA, 0xBB, 0xCC, 0xDD,
	}
	got := DeoptCode(code)
	if compile.Opcode(got[0]) != compile.LOAD_ATTR {
		t.Fatalf("truncated stream: opcode not rewritten")
	}
	if got[1] != 0x09 {
		t.Fatalf("truncated stream: oparg lost")
	}
	for i := 2; i < len(got); i++ {
		if got[i] != 0 {
			t.Fatalf("truncated stream: byte %d not zeroed: 0x%x", i, got[i])
		}
	}
}

// Walk every entry in DeoptParent and confirm DeoptCode reduces a
// stream starting with that opcode to its adaptive parent. This guards
// against future additions to the family map that forget to update the
// deopt walk.
//
// CPython: Include/internal/pycore_opcode_metadata.h _PyOpcode_Deopt (full table)
func TestDeoptCode_AllFamilyMembers(t *testing.T) {
	for spec, parent := range DeoptParent {
		if spec == parent {
			continue
		}
		// Build a minimal codeunit + enough cache padding for the
		// parent's declared cache width. We use the parent's cache
		// width because CPython's deopt_code reads CacheCount(base).
		width := CacheCount(parent)
		buf := make([]byte, CodeUnitWidth*(1+width))
		buf[0] = byte(spec)
		buf[1] = 0xA5
		for i := CodeUnitWidth; i < len(buf); i++ {
			buf[i] = 0xFF
		}
		got := DeoptCode(buf)
		if compile.Opcode(got[0]) != parent {
			t.Fatalf("%s: not deopted to %s (got %s)",
				spec.Name(), parent.Name(), compile.Opcode(got[0]).Name())
		}
		if got[1] != 0xA5 {
			t.Fatalf("%s: oparg lost: got 0x%x want 0xA5", spec.Name(), got[1])
		}
		for i := CodeUnitWidth; i < len(got); i++ {
			if got[i] != 0 {
				t.Fatalf("%s: cache byte %d not zeroed: 0x%x",
					spec.Name(), i, got[i])
			}
		}
	}
}
