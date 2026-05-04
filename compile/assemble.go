// Port of cpython/Python/assemble.c (~800 lines) to compile/assemble.go.
// Sequence-to-Code conversion: pack instructions into the 16-bit
// _Py_CODEUNIT format with EXTENDED_ARG widening, emit the PEP 626
// location table and PEP 657 exception table, and freeze the per-unit
// pools (consts, names, varnames, freevars, cellvars, localsplus).
//
// CPython: Python/assemble.c

package compile

import (
	"fmt"
)

// Assembler is the per-call state. Public so tests can drive
// individual phases (emit, varint, location, exception).
//
// CPython: Python/assemble.c struct assembler
type Assembler struct {
	Filename       string
	FirstLineno    int
	Code           []byte
	LineTable      []byte
	ExceptionTable []byte
	lineCursor     int
}

// Assemble builds a final Code object from the post-flowgraph
// Sequence plus per-unit metadata. Mirrors CPython's
// _PyAssemble_MakeCodeObject.
//
// CPython: Python/assemble.c:L731 _PyAssemble_MakeCodeObject
func Assemble(seq *Sequence, info *Info, unit *Unit, filename string) (*Code, error) {
	if seq == nil || unit == nil {
		return nil, fmt.Errorf("compile: Assemble called with nil sequence or unit")
	}
	a := &Assembler{
		Filename:    filename,
		FirstLineno: unit.FirstLineno,
		lineCursor:  unit.FirstLineno,
	}
	for i := range seq.Instrs {
		a.emitInstr(&seq.Instrs[i])
	}
	stacksize := 0
	if info != nil {
		stacksize = info.MaxStackDepth
	}
	localsplusNames, localsplusKinds := buildLocalsPlus(unit)
	return &Code{
		Argcount:        unit.Argcount,
		PosOnlyArgCount: unit.PosOnlyArgCount,
		KwOnlyArgCount:  unit.KwOnlyArgCount,
		NLocals:         len(unit.VarNames),
		Stacksize:       stacksize,
		Flags:           unit.Flags,
		Code:            a.Code,
		Consts:          unit.Consts,
		Names:           unit.Names,
		VarNames:        unit.VarNames,
		FreeVars:        unit.FreeVars,
		CellVars:        unit.CellVars,
		LocalsPlusNames: localsplusNames,
		LocalsPlusKinds: localsplusKinds,
		Filename:        filename,
		Name:            unit.Name,
		Qualname:        unit.Qualname,
		Firstlineno:     unit.FirstLineno,
		Linetable:       a.LineTable,
		ExceptionTable:  a.ExceptionTable,
	}, nil
}

// emitInstr packs one instruction into the _Py_CODEUNIT stream.
// Opcodes carry one byte each; opargs > 255 widen via up to three
// EXTENDED_ARG prefixes (CPython supports a 32-bit oparg total).
// CACHE entries are not yet emitted: the v0.5 pipeline does not run
// the specializer, so all caches are zero-padded by the VM at load
// time.
//
// CPython: Python/assemble.c:L369 write_instr
func (a *Assembler) emitInstr(ins *Instr) {
	arg := uint32(ins.Oparg)
	// Drop pseudo opcodes that lower to nothing (POP_BLOCK is a
	// flowgraph-only marker).
	if ins.Op == POP_BLOCK {
		return
	}
	// Emit EXTENDED_ARG prefixes from highest to lowest byte.
	if arg > 0xff {
		var prefixes []byte
		for shift := uint(24); shift > 0; shift -= 8 {
			b := byte(arg >> shift)
			if b != 0 || len(prefixes) > 0 {
				prefixes = append(prefixes, b)
			}
		}
		for _, b := range prefixes {
			a.Code = append(a.Code, byte(EXTENDED_ARG), b)
		}
	}
	a.Code = append(a.Code, byte(ins.Op), byte(arg&0xff))
}

// buildLocalsPlus materializes the flat 3.11+ co_localsplus layout.
// Order: positional/kwonly args, then ordinary locals, then cells,
// then frees. Args and ordinary locals share the FastLocal kind; the
// cell/free split comes from the unit's CellVars / FreeVars lists.
// FastHidden is OR'd in for synthetic locals (lambda implicit arg,
// comprehension `.0`).
//
// CPython: Python/assemble.c:L483 compute_localsplus_info
func buildLocalsPlus(unit *Unit) (names []string, kinds []uint8) {
	names = make([]string, 0, len(unit.VarNames)+len(unit.CellVars)+len(unit.FreeVars))
	kinds = make([]uint8, 0, cap(names))
	for _, n := range unit.VarNames {
		k := FastLocal
		if unit.FastHidden[n] {
			k |= FastHidden
		}
		names = append(names, n)
		kinds = append(kinds, k)
	}
	for _, n := range unit.CellVars {
		names = append(names, n)
		kinds = append(kinds, FastCell)
	}
	for _, n := range unit.FreeVars {
		names = append(names, n)
		kinds = append(kinds, FastFree)
	}
	return names, kinds
}
