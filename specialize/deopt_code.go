// Deoptimization pass that prepares a bytecode buffer for marshal.
//
// CPython runs deopt_code (Objects/codeobject.c:2293) on every
// _PyCode_GetCode call so the bytes the marshal writer sees are
// always the adaptive form: specialized opcodes (LOAD_ATTR_SLOT,
// CALL_PY_EXACT_ARGS, etc.) map back to their adaptive parent and
// every inline cache cell is zeroed. On load, _PyCode_New runs
// _PyCode_Quicken again to re-stamp the adaptive counters. The net
// effect is that .pyc wire bytes are deterministic across runs and
// independent of any specialization state the in-memory code may
// have warmed at marshal time.
//
// gopy mirrors that shape: specialize.Enable already re-quickens on
// unmarshalCode, so all we need is a pre-write pass that deopts the
// bytes before marshal.writeCachedBytes sees them.
//
// CPython: Objects/codeobject.c:2293 deopt_code
// CPython: Objects/codeobject.c:3440 deopt_code_unit
// CPython: Objects/codeobject.c:2310 _PyCode_GetCode

package specialize

import "github.com/tamnd/gopy/compile"

// DeoptCode returns a fresh slice holding the adaptive form of code:
// every specialized opcode is rewritten to its adaptive parent (via
// Deopt) and every following cache cell is zeroed. The oparg byte is
// preserved unchanged because oparg encodes operand-level information
// (LOAD_CONST index, JUMP target, etc.) that the specializer never
// touches.
//
// The returned slice has the same length as the input. Empty / short
// inputs (less than one codeunit) round-trip unchanged. The walk
// terminates one codeunit before the end (matching CPython's
// size-1 stop in deopt_code) because the last codeunit cannot have
// an inline cache.
//
// CPython: Objects/codeobject.c:2293 deopt_code
func DeoptCode(code []byte) []byte {
	out := make([]byte, len(code))
	copy(out, code)
	DeoptCodeInPlace(out)
	return out
}

// DeoptCodeInPlace runs the deoptimization pass directly on code.
// Use this when the caller already owns a fresh buffer (e.g. the
// marshal path that built a defensive copy upstream).
//
// CPython: Objects/codeobject.c:2293 deopt_code
func DeoptCodeInPlace(code []byte) {
	size := len(code) / CodeUnitWidth
	if size < 1 {
		return
	}
	for i := 0; i < size; i++ {
		op := compile.Opcode(code[CodeUnitWidth*i])
		base := Deopt(op)
		if base != op {
			code[CodeUnitWidth*i] = byte(base)
		}
		caches := CacheCount(base)
		if caches > 0 {
			start := CodeUnitWidth * (i + 1)
			end := min(CodeUnitWidth*(i+1+caches), len(code))
			for j := start; j < end; j++ {
				code[j] = 0
			}
			i += caches
		}
	}
}
