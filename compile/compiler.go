// Port of cpython/Python/compile.c entry points to compile/compiler.go.
// The main public surface is Compile, which drives the full pipeline:
// symtable.Build, then codegen, then flowgraph.Optimize, then assemble.
//
// CPython: Python/compile.c _PyAST_Compile / _PyAST_CompileObject

package compile

import (
	"fmt"

	"github.com/tamnd/gopy/ast"
	"github.com/tamnd/gopy/future"
	"github.com/tamnd/gopy/symtable"
)

// Compile runs the full pipeline on a parsed module and returns the
// top-level Code object plus any nested code objects (one per nested
// scope).
//
// Pipeline:
//
//  1. symtable.Build resolves every name to its scope.
//  2. codegen walks the AST top-down; each scope produces a Unit.
//  3. flowgraph.Optimize resolves labels and runs the optimisation
//     passes that have landed.
//  4. assemble packs the Sequence and pools into a Code object.
//
// CPython: Python/compile.c:L353 _PyAST_Compile
func Compile(mod ast.Mod, filename string, optimize int) (*Code, error) {
	ff, err := future.FromAST(mod, filename)
	if err != nil {
		return nil, err
	}
	// AST preprocess runs between future scan and symtable build, matching
	// the CPython ordering in compiler_init. The pass applies printf-format
	// folding, __debug__ substitution, MatchValue numeric folding, and (at
	// -OO) docstring removal. SyntaxCheckOnly stays false so mutations land
	// before symtable sees the tree.
	//
	// CPython: Python/compile.c:139 compiler_init
	ast.Preprocess(mod, ast.PreprocessOptions{
		Filename:       filename,
		OptimizeLevel:  optimize,
		FFFeatures:     ff.Bits,
		EnableWarnings: true,
	})
	st, err := symtable.Build(mod, filename, ff)
	if err != nil {
		return nil, err
	}
	c := NewCompiler(filename, optimize, ff, st)
	return c.compileMod(mod, filename)
}

// compileMod is the recursive driver that codegen-then-assembles one
// scope and walks any nested scopes the codegen produced. CPython
// pushes a new compiler_unit per scope and runs codegen + flowgraph +
// assemble inline; here each Unit captures its own Seq plus a list of
// nested Units, and we walk the tree post-order.
//
// CPython: Python/compile.c:L412 compiler_codegen / compiler_mod
func (c *Compiler) compileMod(mod ast.Mod, filename string) (*Code, error) {
	scope := c.Symtable.Top
	if scope == nil {
		return nil, fmt.Errorf("compile: symtable has no top entry")
	}
	unit, err := c.Codegen(scope, mod)
	if err != nil {
		return nil, err
	}
	return assembleUnit(unit, filename)
}

// assembleUnit runs flowgraph + assemble on one Unit and recurses
// into the nested units the codegen attached. The recursion lands on
// each function/class/comprehension scope in post-order, mirroring
// CPython's compile_codegen path that finishes inner scopes before
// patching their MAKE_FUNCTION operand into the outer scope.
//
// CPython: Python/compile.c:L1843 compile_make_closure / inner-unit
// assembly inside compile_function
func assembleUnit(unit *Unit, filename string) (*Code, error) {
	// Recurse into nested Units before flowgraph runs on the parent:
	// codegen embedded each inner *Unit as a const-pool entry, and the
	// assembler must hand back a *Code in that slot so the marshal
	// stage sees a uniform shape. After recursion, the parent's
	// Consts holds *Code (not *Unit) for every nested scope.
	for i, c := range unit.Consts {
		child, ok := c.(*Unit)
		if !ok {
			continue
		}
		childCo, err := assembleUnit(child, filename)
		if err != nil {
			return nil, err
		}
		unit.Consts[i] = childCo
	}
	info, err := OptimizeWithFlags(unit.Seq, &unit.Consts, len(unit.VarNames), unit.Flags)
	if err != nil {
		return nil, fmt.Errorf("compile: %s: flowgraph: %w", unit.Name, err)
	}
	co, err := Assemble(unit.Seq, info, unit, filename)
	if err != nil {
		return nil, fmt.Errorf("compile: %s: assemble: %w", unit.Name, err)
	}
	return co, nil
}
