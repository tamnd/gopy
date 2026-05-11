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
	// Inlined comprehensions (PEP 709) have their free vars folded
	// into the outer scope's locals by the symtable; emitting a
	// closure tuple here would fail since those names are not Cell in
	// the outer scope. Gopy still builds a separate function for the
	// comp body (the real inlining happens with spec 1696), so the
	// runtime behavior matches the pre-fix path: skip the closure
	// surface and let LOAD_DEREF surface the missing cell.
	var closureFlag int32
	if !innerScope.CompInlined {
		flag, err := c.emitClosure(innerScope, loc(key))
		if err != nil {
			return err
		}
		closureFlag = flag
	}
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
	c.addOpI(RESUME, 0, loc(key))

	// Implicit .0 parameter: the outermost iter.
	c.declareArg(".0")

	if !innerScope.CompInlined {
		if err := c.emitMakeCellAndCopyFree(innerScope, loc(key)); err != nil {
			c.scope = outerScope
			c.fblocks = outerFblocks
			c.restoreCaches(outerCaches)
			return err
		}
	}

	switch kind {
	case compListComp:
		c.addOpI(BUILD_LIST, 0, loc(key))
	case compSetComp:
		c.addOpI(BUILD_SET, 0, loc(key))
	case compDictComp:
		c.addOpI(BUILD_MAP, 0, loc(key))
	}

	if err := c.compileGenerator(gens, 0, 0, elt, val, kind, loc(key)); err != nil {
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
	if kind == compGenExp {
		innerUnit.Flags |= CoGenerator
	}
	if innerScope.Coroutine {
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
) error {
	gen := gens[idx]
	if gen.IsAsync != 0 {
		return c.compileAsyncGenerator(gens, idx, depth, elt, val, kind, l)
	}

	start := c.newLabel()
	ifCleanup := c.newLabel()
	anchor := c.newLabel()

	if idx == 0 {
		// Implicit `.0` parameter holds the outermost iter.
		pool := poolVarNames
		c.addOpName(LOAD_FAST, &pool, ".0", loc(gen.Iter))
	} else {
		if err := c.visitExpr(gen.Iter); err != nil {
			return err
		}
		c.addOp(GET_ITER, loc(gen.Iter))
	}
	depth++

	c.useLabel(start)
	c.addOpJump(FOR_ITER, anchor, loc(gen.Iter))
	if err := c.assignTo(gen.Target, loc(gen.Target)); err != nil {
		return err
	}

	for _, ifx := range gen.Ifs {
		if err := c.visitExpr(ifx); err != nil {
			return err
		}
		c.addOpJump(POP_JUMP_IF_FALSE, ifCleanup, loc(ifx))
	}

	if idx+1 < len(gens) {
		if err := c.compileGenerator(gens, idx+1, depth, elt, val, kind, l); err != nil {
			return err
		}
	} else {
		if err := c.emitCompTail(kind, depth, elt, val); err != nil {
			return err
		}
	}

	c.useLabel(ifCleanup)
	c.addOpJump(JUMP, start, loc(gen.Iter))
	c.useLabel(anchor)
	c.addOp(END_FOR, ast.Pos{})
	c.addOp(POP_ITER, ast.Pos{})
	return nil
}

// compileAsyncGenerator emits an async-for generator clause.
//
// CPython: Python/codegen.c:L4514 codegen_async_comprehension_generator
func (c *Compiler) compileAsyncGenerator(gens ast.Seq[*ast.Comprehension],
	idx, depth int, elt, val ast.Expr, kind compKind, l ast.Pos,
) error {
	gen := gens[idx]
	start := c.newLabel()
	except := c.newLabel()
	ifCleanup := c.newLabel()

	if idx == 0 {
		pool := poolVarNames
		c.addOpName(LOAD_FAST, &pool, ".0", loc(gen.Iter))
	} else {
		if err := c.visitExpr(gen.Iter); err != nil {
			return err
		}
		c.addOp(GET_AITER, loc(gen.Iter))
	}

	c.useLabel(start)
	c.pushFblock(fblockAsyncComprehensionGenerator, start, JumpTargetLabel{}, nil)
	c.addOpJump(SETUP_FINALLY, except, loc(gen.Iter))
	c.addOp(GET_ANEXT, loc(gen.Iter))
	c.addLoadConst(nil, loc(gen.Iter))
	c.addYieldFromLoop(loc(gen.Iter))
	c.addOp(POP_BLOCK, loc(gen.Iter))

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
	if idx+1 < len(gens) {
		if err := c.compileGenerator(gens, idx+1, depth, elt, val, kind, l); err != nil {
			return err
		}
	} else {
		if err := c.emitCompTail(kind, depth, elt, val); err != nil {
			return err
		}
	}

	c.useLabel(ifCleanup)
	c.addOpJump(JUMP, start, loc(gen.Iter))
	if err := c.popFblock(fblockAsyncComprehensionGenerator); err != nil {
		return err
	}

	c.useLabel(except)
	c.addOpJump(END_ASYNC_FOR, start, loc(gen.Iter))
	return nil
}

// emitCompTail writes the per-kind result accumulation step at the
// innermost generator depth.
//
// CPython: Python/codegen.c codegen_sync_comprehension_generator
// (last-generator switch)
func (c *Compiler) emitCompTail(kind compKind, depth int, elt, val ast.Expr) error {
	switch kind {
	case compGenExp:
		if err := c.visitExpr(elt); err != nil {
			return err
		}
		c.addOpI(YIELD_VALUE, 0, loc(elt))
		c.addOp(POP_TOP, loc(elt))
	case compListComp:
		if err := c.visitExpr(elt); err != nil {
			return err
		}
		c.addOpI(LIST_APPEND, int32(depth+1), loc(elt))
	case compSetComp:
		if err := c.visitExpr(elt); err != nil {
			return err
		}
		c.addOpI(SET_ADD, int32(depth+1), loc(elt))
	case compDictComp:
		if err := c.visitExpr(elt); err != nil {
			return err
		}
		if err := c.visitExpr(val); err != nil {
			return err
		}
		c.addOpI(MAP_ADD, int32(depth+1), loc(elt))
	default:
		return fmt.Errorf("compile: unknown comprehension kind %d", kind)
	}
	return nil
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
	c.seq().Insert(0, SETUP_CLEANUP, int32(handler.ID()), ast.Pos{})

	c.addLoadConst(nil, ast.Pos{})
	c.addOp(RETURN_VALUE, ast.Pos{})
	c.useLabel(handler)
	c.addOpI(CALL_INTRINSIC_1, intrinsicStopIterationError, ast.Pos{})
	c.addOpI(RERAISE, 1, ast.Pos{})
}
