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
	// class object via a CALL 0 after the build-class call (the class
	// sits in the self_or_null slot which CALL promotes to the first
	// positional arg).
	if err := c.visitDecorators(s.DecoratorList); err != nil {
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

	// 6. Decorator chain. CALL 0 per layer: the class sits in the
	// self_or_null slot, which the CALL macro path promotes to a
	// first positional arg.
	//
	// CPython: Python/codegen.c:976 codegen_apply_decorators
	for range s.DecoratorList {
		c.addOpI(CALL, 0, loc(s))
	}

	// 7. Bind the produced class object to the class name in the
	// outer scope.
	return c.nameOpStore(s.Name, loc(s))
}

// extractDocstring returns the bare string literal that opens a body,
// matching consumeDocstring's recognition rule but without mutating
// the per-unit const pool. The class-body emitter needs the value as a
// fresh const after the qualname has already taken slot 0; pinning the
// docstring at slot 0 the way consumeDocstring does would shift the
// qualname and confuse downstream consts indexing.
//
// CPython: Python/codegen.c codegen_body (the docstring sniff before
// the body loop)
func extractDocstring(body ast.Seq[ast.Stmt]) (string, bool) {
	if len(body) == 0 {
		return "", false
	}
	es, ok := body[0].(*ast.ExprStmt)
	if !ok {
		return "", false
	}
	con, ok := es.Value.(*ast.Constant)
	if !ok {
		return "", false
	}
	s, ok := con.Value.(string)
	if !ok {
		return "", false
	}
	return s, true
}

// emitInnerClassCode pushes a fresh unit, emits the class body, then
// pops. The inner code object is left on the outer stack as a *Unit
// const; the assembler translates it to a real PyCodeObject.
//
// Emission order mirrors codegen_class_body in CPython 3.14:
//
//	MAKE_CELL __class__               (if needs_class_closure)
//	LOAD __name__ / STORE __module__
//	LOAD_CONST qualname / STORE __qualname__
//	LOAD_CONST firstlineno / STORE __firstlineno__
//	(needs_classdict: MAKE_CELL __classdict__, LOAD_LOCALS, STORE_DEREF)
//	(has_conditional_annotations: BUILD_SET 0, STORE_DEREF __conditional_annotations__)
//	(docstring: LOAD_CONST cleandoc, STORE __doc__)
//	body
//	process_deferred_annotations
//	LOAD_CONST static_attributes_tuple / STORE __static_attributes__
//	(needs_classdict: LOAD_CLOSURE __classdict__, STORE __classdictcell__)
//	(needs_class_closure: LOAD_CLOSURE __class__ + COPY + STORE __classcell__
//	 else LOAD_CONST None)
//	RETURN_VALUE
//
// CPython: Python/codegen.c:1515 codegen_class_body
func (c *Compiler) emitInnerClassCode(innerScope *symtable.Entry, s *ast.ClassDef) error {
	outerScope := c.scope
	outerFblocks := c.fblocks
	outerCaches := c.savedCaches()

	c.enterScope(innerScope)
	c.addOpI(RESUME, 0, loc(s))

	if innerScope.NeedsClassClosure {
		cellPool := poolCellVars
		idx := c.poolIndex(&cellPool, "__class__")
		c.addOpI(MAKE_CELL, int32(idx), loc(s))
	}

	pool := poolNames
	c.addOpName(LOAD_NAME, &pool, "__name__", loc(s))
	c.addOpName(STORE_NAME, &pool, "__module__", loc(s))

	qualname := s.Name
	if u := c.unit(); u != nil && u.Qualname != "" {
		qualname = u.Qualname
	}
	c.addLoadConst(qualname, loc(s))
	c.addOpName(STORE_NAME, &pool, "__qualname__", loc(s))

	// __firstlineno__: the source line where the class header starts
	// (or the first decorator's line when the class has decorators).
	// The class's FirstLineno is already set by enterScope from the
	// symtable entry's Loc.Lineno. Cast to int64 so the small-int
	// rewrite pass recognizes it.
	//
	// CPython: Python/codegen.c:1540
	c.addLoadConst(int64(c.unit().FirstLineno), loc(s))
	c.addOpName(STORE_NAME, &pool, "__firstlineno__", loc(s))

	if innerScope.NeedsClassDict {
		cellPool := poolCellVars
		idx := c.poolIndex(&cellPool, "__classdict__")
		c.addOpI(MAKE_CELL, int32(idx), loc(s))
		c.addOp(LOAD_LOCALS, loc(s))
		c.addOpI(STORE_DEREF, int32(idx), loc(s))
	}

	if innerScope.HasConditionalAnnotations {
		cellPool := poolCellVars
		idx := c.poolIndex(&cellPool, "__conditional_annotations__")
		c.addOpI(BUILD_SET, 0, loc(s))
		c.addOpI(STORE_DEREF, int32(idx), loc(s))
	}

	// Docstring lives inside codegen_body in CPython; emit it after the
	// firstlineno / classdict / conditional annotations prologue so the
	// class namespace gets __doc__ at the same instruction offset. The
	// LOAD_CONST carries the docstring expression's own location so the
	// linetable surfaces a row at the body line; STORE_NAME uses
	// NO_LOCATION so it inherits via propagateLineNumbers.
	//
	// CPython: Python/codegen.c:879 codegen_body (docstring branch)
	body := s.Body
	if doc, ok := extractDocstring(body); ok {
		cleaned := cleanDoc(doc)
		docLoc := loc(body[0])
		c.addLoadConst(cleaned, docLoc)
		c.addOpName(STORE_NAME, &pool, "__doc__", ast.Pos{Lineno: -1})
		body = body[1:]
	}

	if err := c.visitStmts(body); err != nil {
		return err
	}
	if err := c.emitDeferredAnnotations(loc(s)); err != nil {
		return err
	}

	// __static_attributes__: tuple of attribute names assigned via
	// `self.X = ...` in any method body. Collected during attribute
	// visits via maybeAddStaticAttribute. Sorted alphabetically; emitted
	// at NO_LOCATION so propagateLineNumbers inherits the previous
	// instruction's line (CPython renders these rows as continuations).
	//
	// CPython: Python/codegen.c:1560
	noLoc := ast.Pos{Lineno: -1}
	c.emitStaticAttributes(noLoc)

	if innerScope.NeedsClassDict {
		// __classdictcell__: pushes the __classdict__ cell into the
		// class namespace so type construction sees the dict-cell pair
		// it needs to wire __annotate__.
		cellPool := poolCellVars
		idx := c.poolIndex(&cellPool, "__classdict__")
		c.addOpI(LOAD_CLOSURE, int32(idx), noLoc)
		c.addOpName(STORE_NAME, &pool, "__classdictcell__", noLoc)
	}

	if innerScope.NeedsClassClosure {
		cellPool := poolCellVars
		idx := c.poolIndex(&cellPool, "__class__")
		c.addOpI(LOAD_CLOSURE, int32(idx), noLoc)
		c.addOpI(COPY, 1, noLoc)
		c.addOpName(STORE_NAME, &pool, "__classcell__", noLoc)
	} else {
		c.addLoadConst(nil, noLoc)
	}
	c.addOp(RETURN_VALUE, noLoc)

	innerUnit := c.unit()
	innerUnit.Name = s.Name

	c.leaveScope()
	c.scope = outerScope
	c.fblocks = outerFblocks
	c.restoreCaches(outerCaches)

	c.addLoadConst(innerUnit, loc(s))
	return nil
}

// emitStaticAttributes pulls the collected self.X store names off the
// current class unit, sorts them, and emits LOAD_CONST(tuple) +
// STORE_NAME __static_attributes__.
//
// CPython: Python/compile.c:948 _PyCompile_StaticAttributesAsTuple
func (c *Compiler) emitStaticAttributes(l ast.Pos) {
	u := c.unit()
	names := make([]string, 0, len(u.StaticAttributes))
	for name := range u.StaticAttributes {
		names = append(names, name)
	}
	sortStrings(names)
	items := make([]any, len(names))
	for i, n := range names {
		items[i] = n
	}
	c.addLoadConst(&ConstTuple{Values: items}, l)
	pool := poolNames
	c.addOpName(STORE_NAME, &pool, "__static_attributes__", l)
}

// cleanDoc strips uniform leading whitespace from a docstring, mirroring
// inspect.cleandoc / textwrap.dedent semantics. The first line keeps any
// trailing content but loses leading spaces; subsequent lines lose up to
// the minimum indent common to every non-blank line.
//
// CPython: Python/compile.c:1516 _PyCompile_CleanDoc
func cleanDoc(doc string) string {
	// First pass: expand tabs (PyUnicode str.expandtabs default tabsize 8)
	doc = expandTabs(doc, 8)
	// First pass: find minimum indentation of any non-blank line after
	// the first line.
	const maxMargin = int(^uint(0) >> 1)
	margin := maxMargin
	first := true
	for i := 0; i < len(doc); {
		if first {
			for i < len(doc) && doc[i] != '\n' {
				i++
			}
			if i < len(doc) {
				i++
			}
			first = false
			continue
		}
		start := i
		for i < len(doc) && doc[i] == ' ' {
			i++
		}
		if i < len(doc) && doc[i] != '\n' {
			if i-start < margin {
				margin = i - start
			}
		}
		for i < len(doc) && doc[i] != '\n' {
			i++
		}
		if i < len(doc) {
			i++
		}
	}
	if margin == maxMargin {
		margin = 0
	}
	// Strip leading spaces from line one.
	p := 0
	for p < len(doc) && doc[p] == ' ' {
		p++
	}
	if p == 0 && margin == 0 {
		return doc
	}
	var b []byte
	for p < len(doc) {
		ch := doc[p]
		p++
		b = append(b, ch)
		if ch == '\n' {
			break
		}
	}
	for p < len(doc) {
		dropped := 0
		for dropped < margin && p < len(doc) {
			if doc[p] != ' ' {
				break
			}
			p++
			dropped++
		}
		for p < len(doc) {
			ch := doc[p]
			p++
			b = append(b, ch)
			if ch == '\n' {
				break
			}
		}
	}
	return string(b)
}

// expandTabs replaces tab characters with spaces so each tab advances
// the column to the next multiple of tabsize. Newlines reset the column.
//
// CPython: Objects/unicodeobject.c unicode_expandtabs_impl
func expandTabs(s string, tabsize int) string {
	if tabsize <= 0 {
		return s
	}
	out := make([]byte, 0, len(s))
	col := 0
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch ch {
		case '\t':
			n := tabsize - col%tabsize
			for k := 0; k < n; k++ {
				out = append(out, ' ')
			}
			col += n
		case '\n', '\r':
			out = append(out, ch)
			col = 0
		default:
			out = append(out, ch)
			col++
		}
	}
	return string(out)
}
