// Port of cpython/Python/codegen.c comprehension visitors
// (L4371-L4924). Each comprehension is lowered to a fresh scope: the
// outer scope evaluates the outermost iterable and CALLs the inner
// function with that iter as its implicit `.0` parameter. The inner
// scope builds a container (or yields, for genexpr) by walking each
// generator clause.

package compile

import (
	"fmt"

	"github.com/tamnd/gopy/ast"
	"github.com/tamnd/gopy/symtable"
)

// compKind tags which container the inner scope builds.
type compKind int

const (
	compGenExp compKind = iota + 1
	compListComp
	compSetComp
	compDictComp
)

// visitListComp lowers `[elt for ... in iter ...]` to an inner
// function returning the built list.
//
// CPython: Python/codegen.c:L4895 codegen_listcomp
func (c *Compiler) visitListComp(e *ast.ListComp) error {
	return c.compileComprehension("<listcomp>", compListComp,
		e.Generators, e.Elt, nil, e)
}

// visitSetComp lowers `{elt for ...}`.
//
// CPython: Python/codegen.c:L4905 codegen_setcomp
func (c *Compiler) visitSetComp(e *ast.SetComp) error {
	return c.compileComprehension("<setcomp>", compSetComp,
		e.Generators, e.Elt, nil, e)
}

// visitDictComp lowers `{k: v for ...}`.
//
// CPython: Python/codegen.c:L4916 codegen_dictcomp
func (c *Compiler) visitDictComp(e *ast.DictComp) error {
	return c.compileComprehension("<dictcomp>", compDictComp,
		e.Generators, e.Key, e.Value, e)
}

// visitGeneratorExp lowers `(elt for ...)` to an inner generator
// function. The body uses YIELD_VALUE in place of the container
// builder, and the whole inner sequence is wrapped in a
// StopIteration handler.
//
// CPython: Python/codegen.c:L4885 codegen_genexp
func (c *Compiler) visitGeneratorExp(e *ast.GeneratorExp) error {
	return c.compileComprehension("<genexpr>", compGenExp,
		e.Generators, e.Elt, nil, e)
}

// compileComprehension is the shared driver for all four
// comprehension flavors. The non-inlined path is used here: the
// outer scope evaluates the outermost iterable, builds the inner
// function, and calls it.
//
// CPython: Python/codegen.c:L4764 codegen_comprehension
func (c *Compiler) compileComprehension(name string, kind compKind,
	gens ast.Seq[*ast.Comprehension], elt, val ast.Expr, key ast.Expr,
) error {
	innerScope := c.Symtable.Lookup(key)
	if innerScope == nil {
		return fmt.Errorf("compile: no symtable entry for comprehension %s", name)
	}

	// PEP 709: a list / set / dict comprehension whose symtable entry
	// was folded into the parent scope is emitted directly here, with no
	// nested function or CALL. The outermost iterable evaluates in the
	// outer scope, the comprehension's bound locals are isolated across
	// the body, and the result is left on the stack.
	//
	// CPython: Python/codegen.c:4794 codegen_comprehension (is_inlined)
	if innerScope.CompInlined {
		return c.compileInlinedComprehension(innerScope, kind, gens, elt, val, key)
	}

	flag, err := c.emitClosure(innerScope, loc(key))
	if err != nil {
		return err
	}
	closureFlag := flag
	if err := c.emitInnerComprehensionCode(innerScope, name, kind, gens, elt, val, key); err != nil {
		return err
	}
	c.emitMakeFunction(closureFlag, loc(key))

	// Outermost iterable evaluates in the outer scope and is passed
	// as the implicit .0 argument.
	outermost := gens[0]
	if err := c.visitExpr(outermost.Iter); err != nil {
		return err
	}
	if outermost.IsAsync != 0 {
		c.addOp(GET_AITER, loc(outermost.Iter))
	} else {
		c.addOp(GET_ITER, loc(outermost.Iter))
	}
	c.addOpI(CALL, 0, loc(key))

	// async comprehension call returns a coroutine; await it inline.
	if innerScope.Coroutine && kind != compGenExp {
		c.addOpI(GET_AWAITABLE, 0, loc(key))
		c.addLoadConst(nil, loc(key))
		c.addYieldFromLoop(loc(key))
	}
	return nil
}

// compileInlinedComprehension emits a list / set / dict comprehension
// directly into the enclosing code object (PEP 709). The outermost
// iterable is evaluated, the comprehension's bound locals are saved and
// isolated, the generator loop runs against the iterable already on the
// stack, then the saved locals are restored, leaving the built container
// as the top of stack.
//
// CPython: Python/codegen.c:4794 codegen_comprehension (is_inlined arm)
func (c *Compiler) compileInlinedComprehension(entry *symtable.Entry,
	kind compKind, gens ast.Seq[*ast.Comprehension], elt, val ast.Expr,
	key ast.Expr,
) error {
	l := loc(key)
	outermost := gens[0]

	// codegen_comprehension_iter: evaluate the outermost iterable.
	if err := c.visitExpr(outermost.Iter); err != nil {
		return err
	}
	if outermost.IsAsync != 0 {
		c.addOp(GET_AITER, loc(outermost.Iter))
	} else {
		c.addOp(GET_ITER, loc(outermost.Iter))
	}

	var state inlinedCompState
	c.tweakInlinedComprehensionScopes(entry, &state)
	c.pushInlinedComprehensionLocals(entry, &state, l)

	if kind != compGenExp {
		switch kind {
		case compListComp:
			c.addOpI(BUILD_LIST, 0, l)
		case compSetComp:
			c.addOpI(BUILD_SET, 0, l)
		case compDictComp:
			c.addOpI(BUILD_MAP, 0, l)
		}
		// The iterable sits below the freshly-built container; swap it
		// back to TOS so the generator loop consumes it.
		c.addOpI(SWAP, 2, l)
	}

	if err := c.compileGenerator(gens, 0, 0, elt, val, kind, l, true); err != nil {
		return err
	}

	c.popInlinedComprehensionLocals(&state, l)
	c.revertInlinedComprehensionScopes(&state)
	return nil
}

// emitInnerComprehensionCode emits the inner scope's body. The body
// receives the outermost iter through LOAD_FAST 0 and walks each
// generator clause in order.
//
// CPython: Python/codegen.c:L4764 codegen_comprehension (inner half)
func (c *Compiler) emitInnerComprehensionCode(innerScope *symtable.Entry,
	name string, kind compKind, gens ast.Seq[*ast.Comprehension],
	elt, val ast.Expr, key ast.Expr,
) error {
	outerScope := c.scope
	outerFblocks := c.fblocks
	outerCaches := c.savedCaches()

	c.enterScope(innerScope)
	// RESUME loc = LOCATION(firstlineno, firstlineno, 0, 0). Mirrors
	// CPython codegen_enter_scope.
	first := c.unit().FirstLineno
	c.addOpI(RESUME, 0, ast.Pos{Lineno: first, EndLineno: first})

	// Implicit .0 parameter: the outermost iter.
	c.declareArg(".0")

	// MAKE_CELL / COPY_FREE_VARS get inserted by the cfg pipeline's
	// prepare_localsplus at the entry block when the comprehension is
	// compiled as its own scope. Inlined comprehensions (CompInlined)
	// don't carry their own scope and don't need a prologue.
	//
	// CPython: Python/flowgraph.c:3760 insert_prefix_instructions

	switch kind {
	case compListComp:
		c.addOpI(BUILD_LIST, 0, loc(key))
	case compSetComp:
		c.addOpI(BUILD_SET, 0, loc(key))
	case compDictComp:
		c.addOpI(BUILD_MAP, 0, loc(key))
	}

	if err := c.compileGenerator(gens, 0, 0, elt, val, kind, loc(key), false); err != nil {
		return err
	}

	if kind != compGenExp {
		c.addOp(RETURN_VALUE, loc(key))
	} else {
		c.wrapInStopIterationHandler()
	}

	innerUnit := c.unit()
	innerUnit.Name = name
	innerUnit.Argcount = 1
	innerUnit.Flags |= CoOptimized | CoNewLocals
	// Mirror compute_code_flags: a comprehension whose inner scope is
	// both generator and coroutine becomes CO_ASYNC_GENERATOR; a sync
	// genexpr is CO_GENERATOR; any other coroutine inside the
	// comprehension (set / list / dict comp inside an async def) is
	// CO_COROUTINE alone.
	//
	// CPython: Python/compile.c:1379 compute_code_flags
	switch {
	case kind == compGenExp && innerScope.Coroutine:
		innerUnit.Flags |= CoAsyncGenerator
	case kind == compGenExp:
		innerUnit.Flags |= CoGenerator
	case innerScope.Coroutine:
		innerUnit.Flags |= CoCoroutine
	}

	c.leaveScope()
	c.scope = outerScope
	c.fblocks = outerFblocks
	c.restoreCaches(outerCaches)

	c.addLoadConst(innerUnit, loc(key))
	return nil
}

// compileGenerator emits one generator clause and recurses. depth
// is the current container-on-stack depth used by LIST_APPEND /
// SET_ADD / MAP_ADD oparg.
//
// CPython: Python/codegen.c:L4391 codegen_sync_comprehension_generator
func (c *Compiler) compileGenerator(gens ast.Seq[*ast.Comprehension],
	idx, depth int, elt, val ast.Expr, kind compKind, l ast.Pos,
	iterOnStack bool,
) error {
	gen := gens[idx]
	if gen.IsAsync != 0 {
		return c.compileAsyncGenerator(gens, idx, depth, elt, val, kind, l, iterOnStack)
	}

	start := c.newLabel()
	ifCleanup := c.newLabel()
	anchor := c.newLabel()

	// hasLoop tracks whether this generator emits a real FOR_ITER loop.
	// The temporary-variable assignment idiom (`for y in [f(x)]`) elides
	// the loop entirely: the single element is pushed and assigned once.
	hasLoop := true

	if !iterOnStack {
		if idx == 0 {
			// Implicit `.0` parameter holds the outermost iter.
			pool := poolVarNames
			c.addOpName(LOAD_FAST, &pool, ".0", loc(gen.Iter))
		} else {
			// Fast path for the temporary variable assignment idiom:
			//     for y in [f(x)]
			// A one-element list/tuple iter (non-starred) folds to a
			// direct push + assign, dropping the FOR_ITER loop.
			// CPython: Python/codegen.c:4420
			if elt := singleNonStarredElt(gen.Iter); elt != nil {
				if err := c.visitExpr(elt); err != nil {
					return err
				}
				hasLoop = false
			} else {
				if err := c.visitExpr(gen.Iter); err != nil {
					return err
				}
				c.addOp(GET_ITER, loc(gen.Iter))
			}
		}
	}

	if hasLoop {
		depth++
		c.useLabel(start)
		c.addOpJump(FOR_ITER, anchor, loc(gen.Iter))
	}
	if err := c.assignTo(gen.Target, loc(gen.Target)); err != nil {
		return err
	}

	for _, ifx := range gen.Ifs {
		if err := c.visitExpr(ifx); err != nil {
			return err
		}
		c.addOpJump(POP_JUMP_IF_FALSE, ifCleanup, loc(ifx))
	}

	eltLoc := loc(elt)
	if idx+1 < len(gens) {
		if err := c.compileGenerator(gens, idx+1, depth, elt, val, kind, l, false); err != nil {
			return err
		}
	} else {
		tailLoc, err := c.emitCompTail(kind, depth, elt, val)
		if err != nil {
			return err
		}
		eltLoc = tailLoc
	}

	c.useLabel(ifCleanup)
	if hasLoop {
		// The loop-closing JUMP is located on the element, not the
		// iterable, so the back-edge points at the produced value.
		//
		// CPython: Python/codegen.c:4508 ADDOP_JUMP(c, elt_loc, JUMP, start)
		c.addOpJump(JUMP, start, eltLoc)
		c.useLabel(anchor)
		c.addOp(END_FOR, ast.Pos{})
		c.addOp(POP_ITER, ast.Pos{})
	}
	return nil
}

// singleNonStarredElt returns the sole element of a one-element list or
// tuple display when that element is not a starred expression, else nil.
// It backs the comprehension assignment-idiom fast path.
//
// CPython: Python/codegen.c:4423 codegen_comprehension_generator
func singleNonStarredElt(iter ast.Expr) ast.Expr {
	var elts ast.Seq[ast.Expr]
	switch it := iter.(type) {
	case *ast.List:
		elts = it.Elts
	case *ast.Tuple:
		elts = it.Elts
	default:
		return nil
	}
	if len(elts) != 1 {
		return nil
	}
	if _, ok := elts[0].(*ast.Starred); ok {
		return nil
	}
	return elts[0]
}

// compileAsyncGenerator emits an async-for generator clause.
//
// CPython: Python/codegen.c:L4514 codegen_async_comprehension_generator
func (c *Compiler) compileAsyncGenerator(gens ast.Seq[*ast.Comprehension],
	idx, depth int, elt, val ast.Expr, kind compKind, l ast.Pos,
	iterOnStack bool,
) error {
	gen := gens[idx]
	start := c.newLabel()
	except := c.newLabel()
	ifCleanup := c.newLabel()

	if !iterOnStack {
		if idx == 0 {
			// gen_index 0 receives the outermost iter as the implicit
			// `.0` argument; CPython locates LOAD_FAST at the whole-
			// comprehension loc, not the iterable.
			//
			// CPython: Python/codegen.c:4546 ADDOP_I(c, loc, LOAD_FAST, 0)
			pool := poolVarNames
			c.addOpName(LOAD_FAST, &pool, ".0", l)
		} else {
			// CPython: Python/codegen.c:4544 ADDOP(c, LOC(gen->iter), GET_AITER)
			if err := c.visitExpr(gen.Iter); err != nil {
				return err
			}
			c.addOp(GET_AITER, loc(gen.Iter))
		}
	}

	// The async-for scaffolding (SETUP_FINALLY .. END_ASYNC_FOR) carries
	// the whole-comprehension loc in CPython, not the iterable's.
	//
	// CPython: Python/codegen.c:4549 codegen_async_comprehension_generator
	c.useLabel(start)
	c.pushFblock(fblockAsyncComprehensionGenerator, start, NoLabel, nil)
	c.addOpJump(SETUP_FINALLY, except, l)
	c.addOp(GET_ANEXT, l)
	c.addLoadConst(nil, l)
	c.addYieldFromLoop(l)
	c.addOp(POP_BLOCK, l)

	if err := c.assignTo(gen.Target, loc(gen.Target)); err != nil {
		return err
	}

	for _, ifx := range gen.Ifs {
		if err := c.visitExpr(ifx); err != nil {
			return err
		}
		c.addOpJump(POP_JUMP_IF_FALSE, ifCleanup, loc(ifx))
	}

	depth++
	eltLoc := loc(elt)
	if idx+1 < len(gens) {
		if err := c.compileGenerator(gens, idx+1, depth, elt, val, kind, l, false); err != nil {
			return err
		}
	} else {
		tailLoc, err := c.emitCompTail(kind, depth, elt, val)
		if err != nil {
			return err
		}
		eltLoc = tailLoc
	}

	c.useLabel(ifCleanup)
	// CPython: Python/codegen.c:4611 ADDOP_JUMP(c, elt_loc, JUMP, start)
	c.addOpJump(JUMP, start, eltLoc)
	if err := c.popFblock(fblockAsyncComprehensionGenerator); err != nil {
		return err
	}

	c.useLabel(except)
	// CPython: Python/codegen.c:4617 ADDOP_JUMP(c, loc, END_ASYNC_FOR, send)
	c.addOpJump(END_ASYNC_FOR, start, l)
	return nil
}

// emitCompTail writes the per-kind result accumulation step at the
// innermost generator depth. It returns the element location used for the
// accumulation opcode, which the caller reuses for the loop-closing JUMP.
//
// CPython: Python/codegen.c codegen_sync_comprehension_generator
// (last-generator switch)
func (c *Compiler) emitCompTail(kind compKind, depth int, elt, val ast.Expr) (ast.Pos, error) {
	eltLoc := loc(elt)
	switch kind {
	case compGenExp:
		if err := c.visitExpr(elt); err != nil {
			return eltLoc, err
		}
		// CPython: Python/codegen.c:4478 ADDOP_YIELD in comp_genexp tail.
		c.addopYield(eltLoc)
		c.addOp(POP_TOP, eltLoc)
	case compListComp:
		if err := c.visitExpr(elt); err != nil {
			return eltLoc, err
		}
		c.addOpI(LIST_APPEND, int32(depth+1), eltLoc)
	case compSetComp:
		if err := c.visitExpr(elt); err != nil {
			return eltLoc, err
		}
		c.addOpI(SET_ADD, int32(depth+1), eltLoc)
	case compDictComp:
		if err := c.visitExpr(elt); err != nil {
			return eltLoc, err
		}
		if err := c.visitExpr(val); err != nil {
			return eltLoc, err
		}
		// With '{k: v}', k is evaluated before v; the MAP_ADD location
		// spans from the key's start to the value's end.
		//
		// CPython: Python/codegen.c:4496 elt_loc = LOCATION(...)
		eltLoc = ast.Pos{
			Lineno:       loc(elt).Lineno,
			ColOffset:    loc(elt).ColOffset,
			EndLineno:    loc(val).EndLineno,
			EndColOffset: loc(val).EndColOffset,
		}
		c.addOpI(MAP_ADD, int32(depth+1), eltLoc)
	default:
		return eltLoc, fmt.Errorf("compile: unknown comprehension kind %d", kind)
	}
	return eltLoc, nil
}

// wrapInStopIterationHandler bolts a SETUP_CLEANUP at offset 0 plus a
// trailing handler that converts StopIteration leaks into a real
// runtime error. Used for genexpr only; CPython's codegen calls this
// after the body has been emitted so the handler covers the whole
// inner sequence.
//
// CPython: Python/codegen.c:L1175 codegen_wrap_in_stopiteration_handler
func (c *Compiler) wrapInStopIterationHandler() {
	handler := c.newLabel()
	// All five ops carry NO_LOCATION (Lineno -1), not the zero Pos. A
	// zero-lineno NOP would survive basicblock_remove_redundant_nops
	// (which only drops NOPs with lineno < 0), leaving a stray entry NOP.
	//
	// CPython: Python/codegen.c:1175 codegen_wrap_in_stopiteration_handler
	noLoc := ast.Pos{Lineno: -1}
	c.seq().Insert(0, SETUP_CLEANUP, int32(handler.ID()), noLoc)

	c.addLoadConst(nil, noLoc)
	c.addOp(RETURN_VALUE, noLoc)
	c.useLabel(handler)
	c.addOpI(CALL_INTRINSIC_1, intrinsicStopIterationError, noLoc)
	c.addOpI(RERAISE, 1, noLoc)
}
