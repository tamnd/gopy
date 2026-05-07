// Initial cache stamping for adaptive opcodes.
//
// Quicken walks a freshly assembled bytecode buffer once and writes
// the seed BackoffCounter (or, for branch hint slots, the alternating
// pattern 0x5555) into every adaptive opcode's first cache cell.
// CPython runs this on a code object's first execution; gopy invokes
// it from the eval loop entry just before the first dispatch tick.
//
// CPython: Python/specialize.c:459 _PyCode_Quicken

package specialize

import (
	"encoding/binary"

	"github.com/tamnd/gopy/compile"
)

// Quicken stamps initial counters into the adaptive cache cells of
// code. The buffer holds packed _Py_CODEUNIT pairs (op, arg) with
// reserved cache slots after every adaptive opcode. enableCounters
// is the flag CPython spells the same way: when false, every counter
// is set to the unreachable sentinel so dispatch never trips a
// specialize attempt (used by the disassembler when it materializes
// a code object that should not run).
//
// CPython: Python/specialize.c:459 _PyCode_Quicken
func Quicken(code []byte, enableCounters bool) {
	var jumpCounter, adaptiveCounter BackoffCounter
	if enableCounters {
		jumpCounter = InitialJumpBackoffCounter()
		adaptiveCounter = AdaptiveCounterWarmup()
	} else {
		jumpCounter = InitialUnreachableBackoffCounter()
		adaptiveCounter = InitialUnreachableBackoffCounter()
	}
	size := len(code) / 2
	if size < 2 {
		return
	}
	// The last codeunit cannot carry a cache; CPython stops the walk
	// at size-1 for the same reason.
	for i := 0; i < size-1; i++ {
		op := compile.Opcode(code[2*i])
		nCaches := CacheCount(op)
		if nCaches == 0 {
			continue
		}
		var seed uint16
		switch op {
		case compile.JUMP_BACKWARD:
			seed = jumpCounter.ValueAndBackoff
		case compile.POP_JUMP_IF_FALSE,
			compile.POP_JUMP_IF_TRUE,
			compile.POP_JUMP_IF_NONE,
			compile.POP_JUMP_IF_NOT_NONE:
			// Branch-hint history: alternating 0,1 bits. CPython
			// spells this 0x5555 verbatim.
			seed = 0x5555
		default:
			seed = adaptiveCounter.ValueAndBackoff
		}
		binary.LittleEndian.PutUint16(code[2*(i+1):], seed)
		i += nCaches
	}
}
