// Code-object construction pipeline. Mirrors Python/assemble.c's
// dict_keys_inorder / compute_localsplus_info / makecode /
// _PyAssemble_MakeCodeObject quartet. Called by the public Assemble
// entry once the cfg has been flattened and jump offsets resolved.

package compile

// dictKeysInorder returns a fresh slice containing the same names in
// the same order. CPython produces a tuple from a Python dict where
// each key maps to its index; gopy already carries the names as an
// ordered []string, so this collapses to a defensive copy. The
// `offset` parameter mirrors CPython's signature, where indices are
// shifted by `offset` before being placed in the tuple.
//
// CPython: Python/assemble.c:458 dict_keys_inorder
func dictKeysInorder(names []string, offset int) []string {
	out := make([]string, len(names))
	copy(out, names)
	_ = offset
	return out
}

// computeLocalsplusInfo materialises the flat 3.11+ co_localsplus
// layout: positional/kwonly args, then ordinary locals, then cells,
// then frees. Args/locals share FastLocal; FastHidden is OR'd in for
// synthetic locals (lambda implicit arg, comprehension `.0`).
// FastCell is OR'd in for cell names that also appear as locals (arg
// cells). Cells that duplicate a varname are skipped at the cellvars
// pass, mirroring the numdropped logic in CPython's
// fix_cell_offsets / compute_localsplus_info.
//
// CPython: Python/assemble.c:484 compute_localsplus_info
func computeLocalsplusInfo(unit *Unit, nlocalsplus int, codeFlags uint32) (names []string, kinds []uint8) {
	names = make([]string, nlocalsplus)
	kinds = make([]uint8, nlocalsplus)

	cellset := make(map[string]bool, len(unit.CellVars))
	for _, n := range unit.CellVars {
		cellset[n] = true
	}

	// Arg vars fill the first portion of the list. Walk varnames in
	// order; gopy stores them as a single []string already in declared
	// order so we don't need CPython's argvarkinds bucket loop. The
	// cell-overlap test stamps FastCell on arg cells.
	for i, name := range unit.VarNames {
		kind := FastLocal
		if unit.FastHidden[name] {
			kind |= FastHidden
		}
		if cellset[name] {
			kind |= FastCell
		}
		names[i] = name
		kinds[i] = kind
	}

	nlocals := len(unit.VarNames)
	varset := make(map[string]bool, nlocals)
	for _, n := range unit.VarNames {
		varset[n] = true
	}

	// Cellvars whose name already appears as a local are arg cells:
	// they were re-mapped onto the arg slot above and must be skipped
	// here. Surviving cells land at nlocals + (running offset).
	numdropped := 0
	cellOffset := -1
	for i, name := range unit.CellVars {
		if varset[name] {
			numdropped++
			continue
		}
		off := i - numdropped + nlocals
		cellOffset = off
		names[off] = name
		kinds[off] = FastCell
	}

	// Freevars follow the surviving cells; their index is i + nlocals
	// + ncellvars - numdropped. CPython asserts offset > cellOffset.
	for i, name := range unit.FreeVars {
		off := i + len(unit.CellVars) - numdropped + nlocals
		_ = cellOffset
		names[off] = name
		kinds[off] = FastFree
	}

	_ = codeFlags
	return names, kinds
}

// makecode builds the final Code object from the per-unit assembler
// state. Mirrors CPython's makecode helper: snapshot names via
// dict_keys_inorder, freeze consts as a tuple, compute localsplus
// names/kinds, then hand all of it to _PyCode_New.
//
// CPython: Python/assemble.c:574 makecode
func makecode(unit *Unit, a *Assembler, constslist []any, maxdepth, nlocalsplus int, codeFlags uint32, filename string) *Code {
	names := dictKeysInorder(unit.Names, 0)
	consts := make([]any, len(constslist))
	copy(consts, constslist)

	localsplusNames, localsplusKinds := computeLocalsplusInfo(unit, nlocalsplus, codeFlags)

	qualname := unit.Qualname
	if qualname == "" {
		qualname = unit.Name
	}

	posonlyargcount := unit.PosOnlyArgCount
	posorkwargcount := unit.Argcount
	kwonlyargcount := unit.KwOnlyArgCount

	return &Code{
		Argcount:        posonlyargcount + posorkwargcount,
		PosOnlyArgCount: posonlyargcount,
		KwOnlyArgCount:  kwonlyargcount,
		NLocals:         len(unit.VarNames),
		Stacksize:       maxdepth,
		Flags:           codeFlags,
		Code:            a.Code,
		Consts:          consts,
		Names:           names,
		VarNames:        unit.VarNames,
		FreeVars:        unit.FreeVars,
		CellVars:        unit.CellVars,
		LocalsPlusNames: localsplusNames,
		LocalsPlusKinds: localsplusKinds,
		Filename:        filename,
		Name:            unit.Name,
		Qualname:        qualname,
		Firstlineno:     unit.FirstLineno,
		Linetable:       a.LineTable,
		ExceptionTable:  a.ExceptionTable,
	}
}

// assembleMakeCodeObject is the assemble.c entry point. Runs the two
// jump-resolution passes, calls assembleEmit to fill the bytecode /
// location / exception tables, then hands off to makecode for the
// final Code object.
//
// CPython: Python/assemble.c:779 _PyAssemble_MakeCodeObject
func assembleMakeCodeObject(unit *Unit, consts []any, maxdepth int, seq *Sequence, nlocalsplus int, codeFlags uint32, filename string) *Code {
	resolveUnconditionalJumps(seq)
	resolveJumpOffsets(seq)
	a := &Assembler{
		Filename:    filename,
		FirstLineno: unit.FirstLineno,
		lineCursor:  unit.FirstLineno,
	}
	assembleEmit(a, seq, unit.FirstLineno)
	return makecode(unit, a, consts, maxdepth, nlocalsplus, codeFlags, filename)
}
