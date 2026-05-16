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
		// CACHE codeunits are inline-cache slots emitted after every
		// adaptive opcode. They never disassemble as standalone
		// instructions; dis just steps past them.
		//
		// CPython: Lib/dis.py _get_instructions_bytes (skips when
		// opcode in _inline_cache_entries).
		if op == CACHE {
			continue
		}
		// Walk back any EXTENDED_ARG prefixes the assembler emitted.
		// CPython recombines them with shift+or; we just look back
		// up to three slots, hopping over CACHE cells that may sit
		// between the previous real instruction and this one.
		shift := 0
		for back := off - 2; back >= 0 && shift < 24; back -= 2 {
			prev := Opcode(co.Code[back])
			if prev == CACHE {
				continue
			}
			if prev != EXTENDED_ARG {
				break
			}
			shift += 8
			oparg |= int32(co.Code[back+1]) << shift
		}
		if op == EXTENDED_ARG {
			continue
		}
		fmt.Fprintf(b, "%s%4d %-30s %d\n", prefix, off, op.Name(), oparg)
		// Skip the trailing cache slots so the next iteration lands
		// on the next real instruction.
		off += 2 * CacheCount(op)
	}
	for _, c := range co.Consts {
		if inner, ok := c.(*Code); ok {
			fmt.Fprintf(b, "\n%sDisassembly of %s:\n", prefix, inner.Name)
			disasmCode(b, inner, indent+1)
		}
	}
}
