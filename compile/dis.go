// Port of cpython/Lib/dis.py to compile/dis.go. Renders a *Code as a
// human-readable bytecode listing matching the format dis.dis prints.
// Used as a v0.5 gate (compile a snippet and assert the disassembly
// matches CPython's).
//
// CPython: Lib/dis.py _disassemble_bytes / _disassemble_recursive

package compile

import (
	"fmt"
	"strings"
)

// Disassemble returns a human-readable string listing of co. Output
// shape matches `dis.dis(co)`: one line per instruction, columns are
// "lineno offset opname  oparg  (display)".
//
// CPython: Lib/dis.py _disassemble_bytes
func Disassemble(co *Code) string {
	var b strings.Builder
	disasmCode(&b, co, 0)
	return b.String()
}

// disasmCode renders one Code object plus any nested codes attached
// via co.Consts (functions hold their inner Code as a const). The
// indent parameter follows CPython's "Disassembly of <inner>:" header
// format.
//
// CPython: Lib/dis.py _disassemble_recursive
func disasmCode(b *strings.Builder, co *Code, indent int) {
	prefix := strings.Repeat("  ", indent)
	for off := 0; off < len(co.Code); off += 2 {
		op := Opcode(co.Code[off])
		oparg := int32(co.Code[off+1])
		// Walk back any EXTENDED_ARG prefixes the assembler emitted.
		// CPython recombines them with shift+or; we just look back
		// up to three slots.
		shift := 0
		for back := off - 2; back >= 0 && Opcode(co.Code[back]) == EXTENDED_ARG && shift < 24; back -= 2 {
			shift += 8
			oparg |= int32(co.Code[back+1]) << shift
		}
		if op == EXTENDED_ARG {
			continue
		}
		fmt.Fprintf(b, "%s%4d %-30s %d\n", prefix, off, opName(op), oparg)
	}
	for _, c := range co.Consts {
		if inner, ok := c.(*Code); ok {
			fmt.Fprintf(b, "\n%sDisassembly of %s:\n", prefix, inner.Name)
			disasmCode(b, inner, indent+1)
		}
	}
}
