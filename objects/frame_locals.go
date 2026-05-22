// Fast-locals to f_locals snapshot. CPython lazily builds f_locals
// from the activation record's fast slots whenever Python user code
// reads it; FastToLocals does that build, FastToLocalsWithError is
// the same thing with an error-returning signature.
//
// CPython: Objects/frameobject.c:1430 PyFrame_FastToLocalsWithError
// CPython: Objects/frameobject.c:1493 PyFrame_FastToLocals

package objects

// FrameFastToLocals returns a fresh dict that maps the frame's local
// names to their current values. CPython walks co_localsplusnames in
// order and uses the co_localspluskinds byte to decide how to extract
// each slot:
//
//   - CO_FAST_FREE: the slot holds a cell from the enclosing scope,
//     installed by COPY_FREE_VARS; unwrap via PyCell_GetRef and skip
//     when empty.
//   - CO_FAST_CELL: the slot holds a cell (locally-shared) or a raw
//     argument value for arg-cells before MAKE_CELL has run; unwrap
//     if it's a cell, otherwise treat as a value.
//   - otherwise: plain fast local, use the slot value as-is.
//
// Empty slots (Null in the activation record) and empty cells are
// skipped so missing names raise KeyError on lookup, matching
// CPython's "missing key" semantics.
//
// CPython: Objects/frameobject.c:2199 frame_get_var
func FrameFastToLocals(f *Frame) (*Dict, error) {
	out := NewDict()
	if f == nil {
		return out, nil
	}
	interp := f.interp
	code := interp.FrameCode()
	if code == nil {
		return out, nil
	}

	// Pre-3.11 fixtures populated Varnames/Cellvars/Freevars without
	// filling LocalsplusNames/Kinds. Fall back to the legacy walk in
	// that case so tests built against the old shape still work.
	if len(code.LocalsplusNames) == 0 || len(code.LocalsplusKinds) == 0 {
		return frameFastToLocalsLegacy(f, code)
	}

	for i, name := range code.LocalsplusNames {
		if i >= len(code.LocalsplusKinds) {
			break
		}
		kind := code.LocalsplusKinds[i]
		val := interp.FrameLocalsPlusItem(i)
		if val == nil {
			continue
		}
		switch {
		case kind&CoFastFree != 0:
			cell, ok := val.(*Cell)
			if !ok || cell == nil || cell.Contents == nil {
				continue
			}
			val = cell.Contents
		case kind&CoFastCell != 0:
			if cell, ok := val.(*Cell); ok {
				if cell == nil || cell.Contents == nil {
					continue
				}
				val = cell.Contents
			}
		}
		if err := out.SetItem(NewStr(name), val); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// frameFastToLocalsLegacy is the pre-3.11 split walk over
// Varnames/Cellvars/Freevars. Used only when LocalsplusNames/Kinds is
// empty (test fixtures and bare-bones code objects).
func frameFastToLocalsLegacy(f *Frame, code *Code) (*Dict, error) {
	out := NewDict()
	interp := f.interp

	nlocals := interp.FrameNumLocals()
	for i := 0; i < nlocals && i < len(code.Varnames); i++ {
		val := interp.FrameFastLocal(i)
		if val == nil {
			continue
		}
		if err := out.SetItem(NewStr(code.Varnames[i]), val); err != nil {
			return nil, err
		}
	}

	ncells := interp.FrameNumCells()
	for i := 0; i < ncells && i < len(code.Cellvars); i++ {
		val := interp.FrameCellLocal(i)
		if val == nil {
			continue
		}
		if cell, ok := val.(*Cell); ok {
			if cell.Contents == nil {
				continue
			}
			val = cell.Contents
		}
		if err := out.SetItem(NewStr(code.Cellvars[i]), val); err != nil {
			return nil, err
		}
	}

	nfrees := interp.FrameNumFrees()
	for i := 0; i < nfrees && i < len(code.Freevars); i++ {
		val := interp.FrameFreeLocal(i)
		if val == nil {
			continue
		}
		if cell, ok := val.(*Cell); ok {
			if cell.Contents == nil {
				continue
			}
			val = cell.Contents
		}
		if err := out.SetItem(NewStr(code.Freevars[i]), val); err != nil {
			return nil, err
		}
	}

	return out, nil
}
