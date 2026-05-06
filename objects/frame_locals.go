// Fast-locals to f_locals snapshot. CPython lazily builds f_locals
// from the activation record's fast slots whenever Python user code
// reads it; FastToLocals does that build, FastToLocalsWithError is
// the same thing with an error-returning signature.
//
// CPython: Objects/frameobject.c:1430 PyFrame_FastToLocalsWithError
// CPython: Objects/frameobject.c:1493 PyFrame_FastToLocals

package objects

// FrameFastToLocals returns a fresh dict that maps the frame's local
// names to their current values. The order matches CPython:
//
//  1. fast locals (Code.Varnames)
//  2. cell vars   (Code.Cellvars)
//  3. free vars   (Code.Freevars)
//
// Unbound slots (Null in the activation record) are skipped, matching
// CPython's "missing key" behavior. The returned dict is owned by the
// caller; mutating it does not write back to the frame.
//
// CPython: Objects/frameobject.c:1430 PyFrame_FastToLocalsWithError
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

	// Fast locals.
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

	// Cell vars.
	ncells := interp.FrameNumCells()
	for i := 0; i < ncells && i < len(code.Cellvars); i++ {
		val := interp.FrameCellLocal(i)
		if val == nil {
			continue
		}
		if err := out.SetItem(NewStr(code.Cellvars[i]), val); err != nil {
			return nil, err
		}
	}

	// Free vars.
	nfrees := interp.FrameNumFrees()
	for i := 0; i < nfrees && i < len(code.Freevars); i++ {
		val := interp.FrameFreeLocal(i)
		if val == nil {
			continue
		}
		if err := out.SetItem(NewStr(code.Freevars[i]), val); err != nil {
			return nil, err
		}
	}

	return out, nil
}
