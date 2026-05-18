// Public entry that drives gopy's compile pipeline through the cfg
// optimizer (cfgOptimizeCodeUnitWithHook), firing a user-supplied
// callback at every CPython-named phase boundary so an L2 parity gate
// can diff the dumps against patched-CPython's matching callback.
//
// CPython: Python/compile.c:1860 optimize_and_assemble_code_unit
// CPython: Python/flowgraph.c:3658 _PyCfg_OptimizeCodeUnit

package compile

import (
	"fmt"

	"github.com/tamnd/gopy/ast"
	"github.com/tamnd/gopy/future"
	"github.com/tamnd/gopy/symtable"
)

// CompilePhaseHook is called once per (unit, phase) during the cfg
// optimization pipeline. unit is the qualified scope name from
// codegen (`<module>` for the top level, `f`, `C.method`, etc. for
// nested scopes). phase is one of the boundaries emitted by
// cfgOptimizeCodeUnitWithHook: entry, translate_jump_labels_to_targets,
// mark_except_handlers, label_exception_targets, optimize_cfg,
// remove_unused_consts, add_checks_for_loads_of_uninitialized_variables,
// insert_superinstructions, push_cold_blocks_to_end,
// resolve_line_numbers. dump is the result of DumpCfg on the live
// graph at that boundary, so the gate can compare against CPython
// byte-for-byte without exporting cfgBuilder.
type CompilePhaseHook func(unit, phase, dump string)

// CompileWithCfgPhaseHook mirrors Compile but routes each unit's
// flowgraph through the cfg pipeline and fires hook at every phase.
// Used by the spec 1716 B.3 parity gate; not on the production path
// (Compile still drives the flat-sequence flowgraph until Phase D
// flips the default).
//
// CPython: Python/compile.c:353 _PyAST_Compile (driving) +
// Python/flowgraph.c:3658 _PyCfg_OptimizeCodeUnit (instrumented)
func CompileWithCfgPhaseHook(mod ast.Mod, filename string, optimize int, hook CompilePhaseHook) error {
	if hook == nil {
		return fmt.Errorf("compile: CompileWithCfgPhaseHook called with nil hook")
	}
	ff, err := future.FromAST(mod, filename)
	if err != nil {
		return err
	}
	ast.Preprocess(mod, ast.PreprocessOptions{
		Filename:       filename,
		OptimizeLevel:  optimize,
		FFFeatures:     ff.Bits,
		EnableWarnings: true,
	})
	st, err := symtable.Build(mod, filename, ff)
	if err != nil {
		return err
	}
	c := NewCompiler(filename, optimize, ff, st)
	scope := c.Symtable.Top
	if scope == nil {
		return fmt.Errorf("compile: symtable has no top entry")
	}
	unit, err := c.Codegen(scope, mod)
	if err != nil {
		return err
	}
	return runCfgPhaseHook(unit, hook)
}

// runCfgPhaseHook walks the unit tree post-order. For each unit it
// builds a cfgBuilder from the codegen-output Sequence and runs the
// full cfgOptimizeCodeUnit pipeline with a hook that forwards the
// unit's Qualname through. Mirrors compileMod / assembleUnit's
// recursion so nested scopes get visited in the same order as
// CPython's optimize_and_assemble_code_unit walk.
func runCfgPhaseHook(unit *Unit, hook CompilePhaseHook) error {
	for _, c := range unit.Consts {
		child, ok := c.(*Unit)
		if !ok {
			continue
		}
		if err := runCfgPhaseHook(child, hook); err != nil {
			return err
		}
	}
	g := cfgFromSequence(unit.Seq)
	unitName := unit.Qualname
	if unitName == "" {
		unitName = unit.Name
	}
	phaseHook := func(phase string, gg *cfgBuilder) {
		hook(unitName, phase, DumpCfg(gg))
	}
	consts := unit.Consts
	nlocals := len(unit.VarNames)
	if err := cfgOptimizeCodeUnitWithHook(g, &consts, nlocals, unit.Argcount, unit.FirstLineno, phaseHook); err != nil {
		return fmt.Errorf("compile: %s: cfg pipeline: %w", unitName, err)
	}
	return nil
}
