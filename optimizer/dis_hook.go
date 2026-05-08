// Runtime disassembly. compile.Disassemble works on compile.Code, the
// build-time intermediate that never carries ENTER_EXECUTOR — those
// only appear after specialize+optimize patches the live runtime
// bytecode in objects.Code. This package owns the runtime view, so
// the runtime-aware disassembler lives here too. dis.py rewrites
// every ENTER_EXECUTOR row back to the original Tier-1 instruction
// the executor patched, which keeps disassembly stable across
// trace warm-up; we mirror that.
//
// CPython: Lib/dis.py:218-237 _get_code_array

package optimizer

import (
	"fmt"
	"strings"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/specialize"
)

// DisassembleRuntime renders the live runtime bytecode of co as a
// dis.dis-style listing. ENTER_EXECUTOR install sites are rewritten
// back to the Tier-1 (op, arg) the executor stashed, so the listing
// reads as if no trace projection had happened.
//
// CPython: Lib/dis.py:218-237 _get_code_array
func DisassembleRuntime(co *objects.Code) string {
	var b strings.Builder
	arr := codeExecutors(co)
	for off := 0; off < len(co.Code); {
		op := compile.Opcode(co.Code[off])
		oparg := int32(co.Code[off+1])
		if op == compile.ENTER_EXECUTOR && arr != nil {
			slot := int(co.Code[off+1])
			if slot < len(arr.Entries) && arr.Entries[slot] != nil {
				exec := arr.Entries[slot]
				op = compile.Opcode(exec.VMData.Opcode)
				oparg = int32(exec.VMData.Oparg)
			}
		}
		fmt.Fprintf(&b, "%4d %-30s %d\n", off, op.Name(), oparg)
		off += 2 * (1 + specialize.CacheCount(compile.Opcode(co.Code[off])))
	}
	return b.String()
}
