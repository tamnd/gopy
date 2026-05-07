// Port of cpython/Python/codegen.c codegen_class / codegen_class_body
// (L1515-L1700). The class panel owes its shape to the way CPython
// turns `class C(B, metaclass=M): ...` into a __build_class__ call:
//
//	C = __build_class__(<body fn>, "C", *bases, **keywords)
//
// where <body fn> is a parameterless function whose locals become the
// class namespace.

package compile

import (
	"fmt"

	"github.com/tamnd/gopy/ast"
	"github.com/tamnd/gopy/symtable"
)

// visitClassDef compiles `class C(...): ...`. The full type-param /
// __classdict__ / __classcell__ machinery lands alongside PEP 695 and
// super() support; this step covers the basic shape: decorators,
// LOAD_BUILD_CLASS + PUSH_NULL, inner class body via MAKE_FUNCTION,
// the class name as a constant, the bases as positional args, and any
// keyword arguments (e.g. metaclass=...) folded through CALL_KW.
//
// CPython: Python/codegen.c:L1623 codegen_class
func (c *Compiler) visitClassDef(s *ast.ClassDef) error {
	if hasStarArg(s.Bases) || hasStarStar(s.Keywords) {
		return fmt.Errorf("compile: ClassDef with *args/**kwargs in bases not yet supported")
	}
	if len(s.TypeParams) > 0 {
		return fmt.Errorf("compile: ClassDef with PEP 695 type params not yet supported")
	}
	// 1. Decorators are evaluated outer-first; each wraps the produced
	// class object via a CALL 1 after the build-class call.
	if err := c.visitExprs(s.DecoratorList); err != nil {
		return err
	}

	// 2. LOAD_BUILD_CLASS pushes the __build_class__ builtin; PUSH_NULL
	// is the self/NULL slot the 3.14 CALL convention requires.
	c.addOp(LOAD_BUILD_CLASS, loc(s))
	c.addOp(PUSH_NULL, loc(s))

	// 3. Inner class body as a function-like code object. The body
	// runs in its own scope with __name__, __module__, __qualname__
	// bound up front and the body statements run after.
	innerScope := c.Symtable.Lookup(s)
	if innerScope == nil {
		return fmt.Errorf("compile: no symtable entry for class %q", s.Name)
	}

	// Closure tuple before MAKE_FUNCTION so the inner code can resolve
	// outer-scope free vars.
	closureFlag, err := c.emitClosure(innerScope, loc(s))
	if err != nil {
		return err
	}
	if err := c.emitInnerClassCode(innerScope, s); err != nil {
		return err
	}
	c.emitMakeFunction(closureFlag, loc(s))

	// 4. Class name as a constant. CPython passes this as the second
	// positional arg of __build_class__.
	c.addLoadConst(s.Name, loc(s))

	// 5. Bases (positional) and keywords. The leading 2 args (function
	// + name) are already on the stack so the CALL count starts at 2.
	for _, b := range s.Bases {
		if err := c.visitExpr(b); err != nil {
			return err
		}
	}
	nargs := int32(2 + len(s.Bases))
	if len(s.Keywords) > 0 {
		names := make([]any, 0, len(s.Keywords))
		for _, kw := range s.Keywords {
			if err := c.visitExpr(kw.Value); err != nil {
				return err
			}
			if kw.Arg != nil {
				names = append(names, *kw.Arg)
			}
		}
		c.addLoadConst(tupleOf(names), loc(s))
		c.addOpI(CALL_KW, nargs+int32(len(s.Keywords)), loc(s))
	} else {
		c.addOpI(CALL, nargs, loc(s))
	}

	// 6. Decorator chain: each wraps the result via CALL 1.
	for range s.DecoratorList {
		c.addOpI(CALL, 1, loc(s))
	}

	// 7. Bind the produced class object to the class name in the
	// outer scope.
	return c.nameOpStore(s.Name, loc(s))
}

// emitInnerClassCode pushes a fresh unit, emits the class body, then
// pops. The inner code object is left on the outer stack as a *Unit
// const; the assembler translates it to a real PyCodeObject.
//
// CPython: Python/codegen.c:L1515 codegen_class_body
func (c *Compiler) emitInnerClassCode(innerScope *symtable.Entry, s *ast.ClassDef) error {
	outerScope := c.scope
	outerFblocks := c.fblocks
	outerCaches := c.savedCaches()

	c.enterScope(innerScope)
	c.addOpI(RESUME, 0, loc(s))

	// MAKE_CELL for __class__ when an inner method referenced super or
	// __class__ directly. The cell stays unbound until __build_class__
	// patches it with the freshly built class object.
	if innerScope.NeedsClassClosure {
		cellPool := poolCellVars
		idx := c.poolIndex(&cellPool, "__class__")
		c.addOpI(MAKE_CELL, int32(idx), loc(s))
	}

	// __name__ -> __module__: the class body sees the enclosing
	// module's __name__ via the surrounding namespace (LOAD_NAME) and
	// stores it as the class's __module__ attribute.
	pool := poolNames
	c.addOpName(LOAD_NAME, &pool, "__name__", loc(s))
	c.addOpName(STORE_NAME, &pool, "__module__", loc(s))

	// __qualname__: the dotted path to the class. For top-level
	// classes this is just the class name; nested classes get a path
	// like "Outer.Inner". Full qualname resolution is pending; for now
	// the bare name is correct for the unnested case.
	c.addLoadConst(s.Name, loc(s))
	c.addOpName(STORE_NAME, &pool, "__qualname__", loc(s))

	if err := c.visitStmts(s.Body); err != nil {
		return err
	}

	// Return __classcell__ when the class needs the implicit cell so
	// __build_class__ can fill it with the new class object. Otherwise
	// fall through to LOAD_CONST None / RETURN_VALUE.
	if innerScope.NeedsClassClosure {
		// LOAD_CLOSURE is the conceptual op (push the cell itself); the
		// gopy assembler models it as LOAD_FAST against the cellvars
		// pool, matching how emitClosure threads cells into a child
		// function's MAKE_FUNCTION.
		cellPool := poolCellVars
		c.addOpName(LOAD_FAST, &cellPool, "__class__", loc(s))
		c.addOpI(COPY, 1, loc(s))
		namePool := poolNames
		c.addOpName(STORE_NAME, &namePool, "__classcell__", loc(s))
		c.addOp(RETURN_VALUE, loc(s))
	} else {
		c.addReturnNoneIfMissing(loc(s))
	}

	innerUnit := c.unit()
	innerUnit.Name = s.Name

	c.leaveScope()
	c.scope = outerScope
	c.fblocks = outerFblocks
	c.restoreCaches(outerCaches)

	c.addLoadConst(innerUnit, loc(s))
	return nil
}
