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

// computeLocalsplusInfo materializes the flat 3.11+ co_localsplus
// layout: positional-only args, positional-or-keyword args,
// keyword-only args, *args, **kwargs, ordinary locals, surviving
// cells, then frees. Each slot gets FastLocal plus the matching
// FastArg* sub-flags (CPython's argvarkinds bucket loop), FastHidden
// for synthetic locals, and FastCell for arg cells that overlap a
// varname. Cells that duplicate a varname are dropped at the cellvars
// pass via numdropped, matching fix_cell_offsets.
//
// CPython: Python/assemble.c:483 compute_localsplus_info
func computeLocalsplusInfo(unit *Unit, nlocalsplus int, codeFlags uint32) (names []string, kinds []uint8) {
	names = make([]string, nlocalsplus)
	kinds = make([]uint8, nlocalsplus)

	cellset := make(map[string]bool, len(unit.CellVars))
	for _, n := range unit.CellVars {
		cellset[n] = true
	}

	hasVarargs := codeFlags&CoVarargs != 0
	hasVarkeywords := codeFlags&CoVarkeywords != 0
	argvarkinds := [6]struct {
		count int
		kind  uint8
	}{
		{unit.PosOnlyArgCount, FastArgPos},
		{unit.Argcount, FastArgPos | FastArgKw},
		{unit.KwOnlyArgCount, FastArgKw},
		{boolInt(hasVarargs), FastArgVar | FastArgPos},
		{boolInt(hasVarkeywords), FastArgVar | FastArgKw},
		{-1, 0},
	}

	pos := 0
	bucketMax := 0
	for i := range 6 {
		if argvarkinds[i].count < 0 {
			bucketMax = len(unit.VarNames)
		} else {
			bucketMax += argvarkinds[i].count
		}
		for pos < bucketMax && pos < len(unit.VarNames) {
			name := unit.VarNames[pos]
			kind := FastLocal | argvarkinds[i].kind
			// CO_FAST_HIDDEN is assigned by key presence, not the bool
			// value: a name that was temporarily made a fast local for an
			// inlined comprehension is marked hidden whether it is still
			// active (True) or has been reverted (False).
			//
			// CPython: Python/assemble.c:517 PyDict_Contains(u_fasthidden, k)
			if _, ok := unit.FastHidden[name]; ok {
				kind |= FastHidden
			}
			if cellset[name] {
				kind |= FastCell
			}
			names[pos] = name
			kinds[pos] = kind
			pos++
		}
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

	return names, kinds
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
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
