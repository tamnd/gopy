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
// symtable's ste_varnames in CPython; gopy stores the same count as
// Argcount + KwOnlyArgCount (positional plus kwonly, the only names
// that always have a value before the body runs).
//
// CPython: Python/compile.c:1411 optimize_and_assemble_code_unit
func optimizeAndAssembleCodeUnit(unit *Unit, codeFlags uint32, filename string) (*Code, error) {
	g := cfgFromSequence(unit.Seq)
	nlocals := len(unit.VarNames)
	nparams := unit.Argcount + unit.KwOnlyArgCount
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
