// Port of Python/compile.c's optimize_and_assemble_code_unit. This is
// the single driver that takes a finished compiler_unit (codegen
// output) and runs the four-step cfg pipeline: build a cfg from the
// instruction sequence, run the cfg optimizer, lower the cfg back to a
// flat instruction sequence, then assemble the final Code object.
//
// CPython: Python/compile.c:1411 optimize_and_assemble_code_unit

package compile

import "fmt"

// optimizeAndAssembleCodeUnit drives the four-call CFG pipeline for one
// Unit. Mirrors CPython's optimize_and_assemble_code_unit verbatim:
//
//	g = _PyCfg_FromInstructionSequence(u->u_instr_sequence);
//	_PyCfg_OptimizeCodeUnit(g, consts, const_cache,
//	                        nlocals, nparams, firstlineno);
//	_PyCfg_OptimizedCfgToInstructionSequence(g, &u->u_metadata, code_flags,
//	                                         &stackdepth, &nlocalsplus,
//	                                         &optimized_instrs);
//	co = _PyAssemble_MakeCodeObject(&u->u_metadata, const_cache, consts,
//	                                stackdepth, &optimized_instrs,
//	                                nlocalsplus, code_flags, filename);
//
// gopy carries consts on the Unit as a flat []any in declared order, so
// CPython's consts_dict_keys_inorder step collapses into passing
// unit.Consts straight through. CPython feeds the cfg optimizer the
// const_cache (a private interp-level dedup table); gopy does the same
// dedup inside codegen via objects.constantKey, so the cache argument
// goes away. nlocals is len(unit.VarNames). nparams comes from
// symtable's ste_varnames in CPython (posonly + posorkw + kwonly named
// params); gopy stores the same three counts on Unit separately, so the
// sum reproduces ste_varnames length.
//
// CPython: Python/compile.c:1411 optimize_and_assemble_code_unit
// CPython: Python/compile.c:1429 nparams = PyList_GET_SIZE(u_ste->ste_varnames)
// nparamsForUnit counts the function's parameter slots, matching
// CPython's PyList_GET_SIZE(ste_varnames): the positional-only, regular,
// and keyword-only named parameters, plus the *args and **kwargs slots
// when present. The optimizer treats these slots as defined on entry, so
// loads of them borrow (LOAD_FAST_BORROW) rather than check.
//
// CPython: Python/compile.c:1429 nparams = PyList_GET_SIZE(u_ste->ste_varnames)
func nparamsForUnit(unit *Unit) int {
	nparams := unit.PosOnlyArgCount + unit.Argcount + unit.KwOnlyArgCount
	if unit.Flags&CoVarargs != 0 {
		nparams++
	}
	if unit.Flags&CoVarkeywords != 0 {
		nparams++
	}
	return nparams
}

func optimizeAndAssembleCodeUnit(unit *Unit, codeFlags uint32, filename string) (*Code, error) {
	g := cfgFromSequence(unit.Seq)
	nlocals := len(unit.VarNames)
	nparams := nparamsForUnit(unit)
	if err := cfgOptimizeCodeUnit(g, &unit.Consts, nlocals, nparams, unit.FirstLineno); err != nil {
		return nil, fmt.Errorf("compile: %s: cfg optimize: %w", unit.Name, err)
	}
	optimized := &Sequence{}
	stackdepth, nlocalsplus, err := cfgOptimizedCfgToInstructionSequence(g, unit, codeFlags, optimized)
	if err != nil {
		return nil, fmt.Errorf("compile: %s: cfg to sequence: %w", unit.Name, err)
	}
	return assembleMakeCodeObject(unit, unit.Consts, stackdepth, optimized, nlocalsplus, codeFlags, filename), nil
}
