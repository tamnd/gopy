// Port of Python/assemble.c's per-instruction emit pipeline. The
// Code-object construction lives in assemble_makecode.go; the
// jump-resolution passes live in assemble_jumps.go; this file holds
// the Assembler struct, the per-instruction writer (assemble_emit_instr
// / write_instr), the per-sequence emit driver (assemble_emit), the
// public Assemble entry, and finalizeFlags (compute_code_flags).
//
// CPython: Python/assemble.c

package compile

import (
	"fmt"
)

// Assembler is the per-call state. Public so tests can drive
// individual phases (emit, varint, location, exception).
//
// CPython: Python/assemble.c:struct assembler
type Assembler struct {
	Filename       string
	FirstLineno    int
	Code           []byte
	LineTable      []byte
	ExceptionTable []byte
	lineCursor     int
}

// Assemble builds a final Code object from the post-flowgraph
// Sequence plus per-unit metadata. Thin wrapper around
// assembleMakeCodeObject that fills in maxdepth / nlocalsplus /
// codeFlags from the Info + Unit gopy carries through codegen.
//
// CPython: Python/assemble.c:779 _PyAssemble_MakeCodeObject
func Assemble(seq *Sequence, info *Info, unit *Unit, filename string) (*Code, error) {
	if seq == nil || unit == nil {
		return nil, fmt.Errorf("compile: Assemble called with nil sequence or unit")
	}
	maxdepth := 0
	if info != nil {
		maxdepth = info.MaxStackDepth
	}
	nlocalsplus := len(unit.VarNames) + len(unit.CellVars) + len(unit.FreeVars)
	codeFlags := finalizeFlags(unit)
	return assembleMakeCodeObject(unit, unit.Consts, maxdepth, seq, nlocalsplus, codeFlags, filename), nil
}

// assembleEmit walks seq, calling assembleEmitInstr per instruction,
// then fills the line and exception tables. Mirrors CPython's
// assemble_emit (which also calls assemble_init / assemble_location_info
// / assemble_exception_table in sequence).
//
// CPython: Python/assemble.c:432 assemble_emit
func assembleEmit(a *Assembler, seq *Sequence, firstLineno int) {
	for i := range seq.Instrs {
		ins := seq.Instrs[i]
		assembleEmitInstr(a, &ins)
	}
	a.LineTable = assembleLocationInfo(seq, firstLineno)
	a.ExceptionTable = assembleExceptionTable(seq)
}

// assembleEmitInstr packs one instruction into the _Py_CODEUNIT
// stream. Opcodes carry one byte each; opargs > 255 widen via up to
// three EXTENDED_ARG prefixes (CPython supports a 32-bit oparg total).
// Adaptive opcodes are followed by _PyOpcode_Caches[op] CACHE
// codeunits (op=0, arg=0) so specialize.Quicken can stamp seed
// counters into them post-load.
//
// CPython: Python/assemble.c:412 assemble_emit_instr (+ :369 write_instr)
func assembleEmitInstr(a *Assembler, ins *Instr) {
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
	// CACHE pseudo-ops: two bytes each (op=CACHE=0, arg=0). Cleared
	// by the assembler, populated by specialize.Quicken before first
	// dispatch.
	//
	// CPython: Python/assemble.c:378 write_instr (the trailing
	// memset of size cache * sizeof(_Py_CODEUNIT)).
	for range CacheCount(ins.Op) {
		a.Code = append(a.Code, 0, 0)
	}
}

// CoNoFree is set when a code object captures no free variables and
// has no cell variables. CPython sets it in compute_code_flags after
// flowgraph + symtable agree no closure cells are needed.
//
// CPython: Include/cpython/code.h CO_NOFREE
const CoNoFree uint32 = 0x0040

// finalizeFlags computes the assemble-side bits that depend on data
// only available after codegen finishes: CO_NOFREE (no free or cell
// variables) and a stable signature subset (CO_VARARGS, CO_VARKEYWORDS,
// CO_GENERATOR, CO_COROUTINE, CO_ASYNC_GENERATOR were already set by
// codegen; we leave them alone). Mirrors CPython's compute_code_flags.
//
// CPython: Python/compile.c:8061 compute_code_flags
func finalizeFlags(unit *Unit) uint32 {
	flags := unit.Flags
	if len(unit.FreeVars) == 0 && len(unit.CellVars) == 0 {
		flags |= CoNoFree
	}
	return flags
}
