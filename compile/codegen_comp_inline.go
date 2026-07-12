// PEP 709 inlined comprehensions. A list / set / dict comprehension is
// emitted directly into the enclosing code object instead of building a
// nested function and calling it. The symtable folds the comprehension's
// symbols into the parent scope (symtable.inlineComprehension); this file
// is the codegen half that isolates the comprehension's locals across the
// inlined body so they do not leak into the enclosing namespace.
//
// CPython: Python/codegen.c codegen_comprehension (is_inlined path),
// Python/compile.c _PyCompile_TweakInlinedComprehensionScopes /
// _PyCompile_RevertInlinedComprehensionScopes.

package compile

import (
	"github.com/tamnd/gopy/ast"
	"github.com/tamnd/gopy/symtable"
)

// inlinedCompState carries the saved state needed to restore the
// enclosing scope after an inlined comprehension body finishes.
//
// CPython: Python/compile.h _PyCompile_InlinedComprehensionState
type inlinedCompState struct {
	// pushedLocals are the comprehension-bound names saved with
	// LOAD_FAST_AND_CLEAR before the body runs and restored with
	// STORE_FAST_MAYBE_NULL afterwards, in push order.
	pushedLocals []string
	// fastHidden are names temporarily promoted to fast locals in a
	// non-function (module / class) scope; reverted to False (still
	// CO_FAST_HIDDEN) when the comprehension finishes.
	fastHidden []string
	// tempSymbols saves the enclosing scope's symbol flags for names
	// whose scope differs inside the comprehension, restored on exit.
	tempSymbols map[string]symtable.SymbolFlags
	cleanup     JumpTargetLabel
}

// tweakInlinedComprehensionScopes temporarily rewrites the enclosing
// scope's symbol table so names that resolve differently inside the
// comprehension are compiled with the right scope, and marks
// comprehension-bound locals as fast-hidden in non-function scopes.
//
// CPython: Python/compile.c:1015 _PyCompile_TweakInlinedComprehensionScopes
func (c *Compiler) tweakInlinedComprehensionScopes(comp *symtable.Entry, state *inlinedCompState) {
	u := c.unit()
	inClassBlock := c.scope.Type == symtable.ClassBlock && u.InInlinedComp == 0
	u.InInlinedComp++

	for _, k := range comp.OrderedSymbols() {
		symbol := comp.Symbols[k]
		scope := symbol.Scope()
		outsymbol := c.scope.GetSymbol(k)
		outsc := outsymbol.Scope()

		// If a name has different scope inside than outside the
		// comprehension, handle it with the in-comprehension scope while
		// the body is compiled; if it is free inside, no special handling
		// (it is *_DEREF either way).
		// The middle term is CPython's !(scope == CELL && outsc == FREE),
		// written here with De Morgan's law as (scope != CELL || outsc != FREE).
		if (scope != outsc && scope != symtable.Free &&
			(scope != symtable.Cell || outsc != symtable.Free)) || inClassBlock {
			if state.tempSymbols == nil {
				state.tempSymbols = map[string]symtable.SymbolFlags{}
			}
			c.scope.Symbols[k] = symbol
			state.tempSymbols[k] = outsymbol
		}

		// Locals bound in the comprehension: in a non-function scope they
		// must be overridden to use fast locals for the duration of the
		// body so they stay isolated.
		if (symbol&symtable.DefLocal != 0 && symbol&symtable.DefNonlocal == 0) || inClassBlock {
			if !c.scope.IsFunctionLike() {
				if active, ok := u.FastHidden[k]; !ok || !active {
					u.FastHidden[k] = true
					state.fastHidden = append(state.fastHidden, k)
				}
			}
		}
	}
}

// revertInlinedComprehensionScopes restores the enclosing scope's symbol
// table and demotes the fast-hidden names to False (so they keep the
// CO_FAST_HIDDEN flag but resolve as ordinary module / class names again).
//
// CPython: Python/compile.c:1095 _PyCompile_RevertInlinedComprehensionScopes
func (c *Compiler) revertInlinedComprehensionScopes(state *inlinedCompState) {
	u := c.unit()
	u.InInlinedComp--
	for k, v := range state.tempSymbols {
		c.scope.Symbols[k] = v
	}
	state.tempSymbols = nil
	for _, k := range state.fastHidden {
		u.FastHidden[k] = false
	}
	state.fastHidden = nil
}

// pushInlinedComprehensionLocals saves the enclosing values of the
// comprehension's bound locals and arranges the cleanup handler that
// restores them if the body raises.
//
// CPython: Python/codegen.c:4621 codegen_push_inlined_comprehension_locals
func (c *Compiler) pushInlinedComprehensionLocals(comp *symtable.Entry, state *inlinedCompState, l ast.Pos) {
	u := c.unit()
	inClassBlock := c.scope.Type == symtable.ClassBlock && u.InInlinedComp == 0

	for _, k := range comp.OrderedSymbols() {
		symbol := comp.Symbols[k]
		scope := symbol.Scope()
		if (symbol&symtable.DefLocal != 0 && symbol&symtable.DefNonlocal == 0) || inClassBlock {
			// Push the existing value (which may be NULL if not defined)
			// onto the stack. For a cell, this pushes the cell itself; a
			// fresh cell is created for the comprehension and the original
			// restored afterwards.
			pool := poolVarNames
			c.addOpName(LOAD_FAST_AND_CLEAR, &pool, k, l)
			if scope == symtable.Cell {
				if c.scope.GetScope(k) == symtable.Free {
					c.emitMakeCellFree(k, l)
				} else {
					cellPool := poolCellVars
					c.addOpName(MAKE_CELL, &cellPool, k, l)
				}
			}
			state.pushedLocals = append(state.pushedLocals, k)
		}
	}

	if len(state.pushedLocals) > 0 {
		// The outermost iterable was already evaluated and sits beneath
		// the pushed locals; rotate it back to the top so the generator
		// loop can consume it. This rotation is reversed when the
		// comprehension result is swapped out on exit.
		c.addOpI(SWAP, int32(len(state.pushedLocals)+1), l)

		// A virtual try/finally restores the comprehension locals if the
		// body raises. No fblock is needed: a comprehension body cannot
		// contain return / continue / break.
		state.cleanup = c.newLabel()
		c.addOpJump(SETUP_FINALLY, state.cleanup, l)
	}
}

// emitMakeCellFree emits MAKE_CELL for a name that is free in the
// enclosing scope, using the deref-space offset (cells first, then
// frees) the flowgraph fix-up expects.
//
// CPython: Python/codegen.c:4658 ADDOP_NAME(c, loc, MAKE_CELL, k, freevars)
func (c *Compiler) emitMakeCellFree(name string, l ast.Pos) {
	pool := poolFreeVars
	idx := c.poolIndex(&pool, name)
	idx += len(c.unit().CellVars)
	c.seq().Addop(MAKE_CELL, int32(idx), l)
}

// popInlinedComprehensionLocals closes the virtual try/finally and
// restores the comprehension's saved locals on both the normal and
// exceptional exit paths.
//
// CPython: Python/codegen.c:4726 codegen_pop_inlined_comprehension_locals
func (c *Compiler) popInlinedComprehensionLocals(state *inlinedCompState, l ast.Pos) {
	if len(state.pushedLocals) == 0 {
		return
	}
	c.addOp(POP_BLOCK, ast.Pos{})

	end := c.newLabel()
	c.addOpJump(JUMP_NO_INTERRUPT, end, ast.Pos{})

	// Exception path: discard the incomplete comprehension result (beneath
	// the exception on the stack), restore the locals, and reraise.
	c.useLabel(state.cleanup)
	c.addOpI(SWAP, 2, ast.Pos{})
	c.addOp(POP_TOP, ast.Pos{})
	c.restoreInlinedComprehensionLocals(state, l)
	c.addOpI(RERAISE, 0, ast.Pos{})

	c.useLabel(end)
	c.restoreInlinedComprehensionLocals(state, l)
}

// restoreInlinedComprehensionLocals pops the saved locals off the stack
// back into their fast slots, preserving the comprehension result (or the
// exception) as the top of stack.
//
// CPython: Python/codegen.c:4710 restore_inlined_comprehension_locals
func (c *Compiler) restoreInlinedComprehensionLocals(state *inlinedCompState, l ast.Pos) {
	npops := len(state.pushedLocals)
	// Preserve the result (or exception) as TOS, reversing the SWAP the
	// push side did to surface the outermost iterable.
	c.addOpI(SWAP, int32(npops+1), l)
	pool := poolVarNames
	for i := npops - 1; i >= 0; i-- {
		c.addOpName(STORE_FAST_MAYBE_NULL, &pool, state.pushedLocals[i], l)
	}
}
