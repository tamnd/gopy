// Recovering the underlying bytecode from a live code object. The
// interpreter rewrites bytecode in place for both specialization
// (quickening) and instrumentation (the INSTRUMENTED_<X> markers plus
// the INSTRUMENTED_LINE side table). marshal needs the original,
// un-instrumented, un-specialized bytes so a .pyc never captures the
// transient monitoring state of the process that wrote it.
//
// CPython: Python/instrumentation.c:637 _Py_GetBaseCodeUnit
// CPython: Objects/codeobject.c:2293 deopt_code

package monitor

import (
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/specialize"
)

// GetBaseCodeUnit returns the underlying opcode at codeunit i, stripping
// both specialization and instrumentation. The oparg byte is returned
// unchanged for the common case; instrumentation markers carry no oparg
// rewrite (ENTER_EXECUTOR, the only opcode that does, is JIT-only and
// never appears in gopy bytecode).
//
// CPython: Python/instrumentation.c:637 _Py_GetBaseCodeUnit
func GetBaseCodeUnit(code *objects.Code, i int) compile.Opcode {
	opcode := compile.Opcode(code.Code[2*i])

	// Below the instrumented range it is purely a specialized opcode:
	// deopt to the adaptive parent and we are done.
	if !IsInstrumented(opcode) {
		return specialize.Deopt(opcode)
	}

	data := CoMonitoring(code)
	if opcode == compile.INSTRUMENTED_LINE && data != nil && data.Lines != nil {
		opcode = compile.Opcode(getOriginalOpcode(data.Lines, i))
	}
	if opcode == compile.INSTRUMENTED_INSTRUCTION && data != nil && i < len(data.PerInstructionOpcodes) {
		opcode = compile.Opcode(data.PerInstructionOpcodes[i])
	}
	if base := deinstrument[opcode]; base != 0 {
		return base
	}
	return specialize.Deopt(opcode)
}

// BaseCode returns a fresh slice holding code's bytecode with all
// specialization and instrumentation removed and every inline cache
// cell zeroed. This is the byte sequence marshal must write so .pyc
// output is independent of the warming/monitoring state of the
// process. On load specialize.Enable re-quickens the adaptive form.
//
// CPython: Objects/codeobject.c:2293 deopt_code (via _PyCode_GetCode)
func BaseCode(code *objects.Code) []byte {
	out := make([]byte, len(code.Code))
	copy(out, code.Code)
	size := len(out) / 2
	for i := 0; i < size; i++ {
		base := GetBaseCodeUnit(code, i)
		out[2*i] = byte(base)
		caches := specialize.CacheCount(base)
		for j := 1; j <= caches && i+j < size; j++ {
			out[2*(i+j)] = 0
			out[2*(i+j)+1] = 0
		}
		i += caches
	}
	return out
}
