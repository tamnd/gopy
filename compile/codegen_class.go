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

// visitClassDef compiles `class C(...): ...`. The basic shape:
// decorators, LOAD_BUILD_CLASS + PUSH_NULL, inner class body via
// MAKE_FUNCTION, the class name as a constant, the bases as positional
// args, and any keyword arguments (e.g. metaclass=...) folded through
// CALL_KW. When TypeParams is non-empty the whole emission lives
// inside a synthetic `<generic parameters of C>` wrapper scope.
//
// CPython: Python/codegen.c:1623 codegen_class
func (c *Compiler) visitClassDef(s *ast.ClassDef) error {
	// 1. Decorators are evaluated outer-first; each wraps the produced
	// class object via a CALL 0 after the build-class call (the class
	// sits in the self_or_null slot which CALL promotes to the first
	// positional arg).
	if err := c.visitDecorators(s.DecoratorList); err != nil {
		return err
	}

	if len(s.TypeParams) > 0 {
		if err := c.emitGenericClass(s); err != nil {
			return err
		}
	} else {
		if err := c.emitClassBuildCall(s); err != nil {
			return err
		}
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

// emitClassBuildCall lays out the LOAD_BUILD_CLASS + body fn + name +
// bases + keywords call. Shared between the non-generic path (called
// directly from visitClassDef) and the generic path (called inside the
// "<generic parameters of C>" wrapper scope, with .generic_base spliced
// in as the first base).
//
// CPython: Python/codegen.c:1623 codegen_class (non-generic arm)
func (c *Compiler) emitClassBuildCall(s *ast.ClassDef) error {
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

	// 5. Bases (positional) and keywords.
	return c.emitClassCallArgs(s, false)
}

// emitClassCallArgs pushes positional bases (optionally prefixed with
// .generic_base from the wrapper scope) and the keyword arguments,
// then emits CALL / CALL_KW with the right arg count. The function
// and name are already on the stack from emitClassBuildCall.
//
// When any base is *expr or any keyword is **expr the helper routes
// through the CALL_FUNCTION_EX path (mirrors codegen_call_helper_impl's
// ex_call branch): start with BUILD_LIST 0, walk the bases interleaving
// LIST_EXTEND for starred entries with pending plain-positional values
// flushed via BUILD_LIST + LIST_EXTEND, INTRINSIC_LIST_TO_TUPLE, then
// the matching BUILD_MAP / DICT_MERGE dance for kwargs.
//
// CPython: Python/codegen.c:4246 codegen_call_helper_impl
// (the splice-extra-positional branch is what the generic class uses
// to land .generic_base ahead of the user bases)
func (c *Compiler) emitClassCallArgs(s *ast.ClassDef, withGenericBase bool) error {
	// codegen_call_helper_impl runs codegen_validate_keywords first so
	// `class C(metaclass=type, metaclass=type)` raises SyntaxError at
	// compile time rather than tumbling through to __build_class__.
	//
	// CPython: Python/codegen.c:4254 codegen_call_helper_impl
	if err := c.validateKeywords(s.Keywords); err != nil {
		return err
	}
	if hasStarArg(s.Bases) || hasStarStar(s.Keywords) {
		return c.emitClassCallArgsEx(s, withGenericBase)
	}

	npos := len(s.Bases)
	for _, b := range s.Bases {
		if err := c.visitExpr(b); err != nil {
			return err
		}
	}
	// .generic_base (Generic[T,...]) is appended after the explicit bases,
	// matching CPython's codegen_class ordering: SUBSCRIPT_GENERIC result
	// lands last so orig_bases = (explicit..., Generic[T,...]).
	//
	// CPython: Python/codegen.c:1657-1672 codegen_class (generic bases)
	if withGenericBase {
		if err := c.nameOpLoad(".generic_base", loc(s)); err != nil {
			return err
		}
		npos++
	}
	nargs := int32(2 + npos)
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
	return nil
}

// emitClassCallArgsEx is the ex_call path for ClassDef. The stack at
// entry already carries [__build_class__, NULL, body_fn, name]; with
// withGenericBase we append .generic_base after the user bases so
// orig_bases = (user_base1, ..., Generic[T,...]).
// The walk mirrors starunpack_helper_impl: plain bases push and either
// stay loose (no star seen yet) or LIST_APPEND into the in-progress
// list, while a *base triggers BUILD_LIST pushed-so-far to wrap the
// pre-star prefix, then LIST_EXTEND. Trailing **kwargs go through
// BUILD_MAP / DICT_MERGE before CALL_FUNCTION_EX.
//
// CPython: Python/codegen.c:4296 codegen_call_helper_impl (ex_call arm)
// CPython: Python/codegen.c:3318 starunpack_helper_impl
func (c *Compiler) emitClassCallArgsEx(s *ast.ClassDef, withGenericBase bool) error {
	l := loc(s)
	// pushed counts the values already on the stack that should end up
	// at the head of the args tuple: body_fn, name. Plain bases get
	// pushed inline and are wrapped via BUILD_LIST on the first star
	// (or via BUILD_TUPLE pushed when no star is present).
	pushed := 2

	sequenceBuilt := false
	for i, b := range s.Bases {
		star, isStar := b.(*ast.Starred)
		if isStar {
			if !sequenceBuilt {
				c.addOpI(BUILD_LIST, int32(i+pushed), l)
				sequenceBuilt = true
			}
			if err := c.visitExpr(star.Value); err != nil {
				return err
			}
			c.addOpI(LIST_EXTEND, 1, l)
			continue
		}
		if err := c.visitExpr(b); err != nil {
			return err
		}
		if sequenceBuilt {
			c.addOpI(LIST_APPEND, 1, l)
		}
	}
	// .generic_base goes after all explicit bases, matching CPython's
	// codegen_class ordering where Generic[T,...] is the last positional.
	//
	// CPython: Python/codegen.c:1657-1672 codegen_class (generic bases)
	if withGenericBase {
		if err := c.nameOpLoad(".generic_base", l); err != nil {
			return err
		}
		if sequenceBuilt {
			c.addOpI(LIST_APPEND, 1, l)
		}
		pushed++
	}
	if sequenceBuilt {
		c.addOpI(CALL_INTRINSIC_1, intrinsicListToTuple, l)
	} else {
		// Only **kwargs triggered the ex_call dispatch; fold all
		// pushed values into a tuple directly.
		c.addOpI(BUILD_TUPLE, int32(len(s.Bases)+pushed), l)
	}

	flag := int32(0)
	if len(s.Keywords) > 0 {
		flag = 1
		if err := c.emitKwargsForEx(s.Keywords, l); err != nil {
			return err
		}
	}
	c.addOpI(CALL_FUNCTION_EX, flag, l)
	return nil
}

// emitKwargsForEx builds the kwargs dict that CALL_FUNCTION_EX expects.
// Plain `name=value` chunks are packed via codegen_subkwargs (visit
// pairs, BUILD_MAP n) and DICT_MERGE'd into the running dict; each
// `**expr` does its own DICT_MERGE 1 against the same dict. When the
// first chunk is a plain run the BUILD_MAP itself seeds the dict, so
// the leading empty-map is skipped.
//
// CPython: Python/codegen.c:4306 codegen_call_helper_impl (kwargs)
// CPython: Python/codegen.c:3565 codegen_subkwargs
func (c *Compiler) emitKwargsForEx(kws ast.Seq[*ast.Keyword], l ast.Pos) error {
	haveDict := false
	nseen := 0
	flushPlain := func(end int) error {
		if nseen == 0 {
			return nil
		}
		start := end - nseen
		for j := start; j < end; j++ {
			kw := kws[j]
			c.addLoadConst(*kw.Arg, l)
			if err := c.visitExpr(kw.Value); err != nil {
				return err
			}
		}
		c.addOpI(BUILD_MAP, int32(nseen), l)
		if haveDict {
			c.addOpI(DICT_MERGE, 1, l)
		}
		haveDict = true
		nseen = 0
		return nil
	}
	for i, kw := range kws {
		if kw.Arg == nil {
			if err := flushPlain(i); err != nil {
				return err
			}
			if !haveDict {
				c.addOpI(BUILD_MAP, 0, l)
				haveDict = true
			}
			if err := c.visitExpr(kw.Value); err != nil {
				return err
			}
			c.addOpI(DICT_MERGE, 1, l)
			continue
		}
		nseen++
	}
	if err := flushPlain(len(kws)); err != nil {
		return err
	}
	if !haveDict {
		c.addOpI(BUILD_MAP, 0, l)
	}
	return nil
}

// emitGenericClass wraps the class build call in a synthetic
// "<generic parameters of C>" function scope. Inside the scope:
// emit the TypeParams, store them as .type_params, run the normal
// class-build sequence (with .generic_base spliced into the bases),
// then RETURN_VALUE. In the outer scope, wrap the produced code into
// a function via MAKE_FUNCTION and CALL 0.
//
// CPython: Python/codegen.c:1623 codegen_class (is_generic arm)
func (c *Compiler) emitGenericClass(s *ast.ClassDef) error {
	wrapperScope := c.Symtable.LookupTypeParams(s)
	if wrapperScope == nil {
		return fmt.Errorf("compile: no TypeParametersBlock for class %q", s.Name)
	}

	outerScope := c.scope
	outerFblocks := c.fblocks
	outerCaches := c.savedCaches()

	c.enterScope(wrapperScope)
	first := c.unit().FirstLineno
	c.addOpI(RESUME, 0, ast.Pos{Lineno: first, EndLineno: first})

	if err := c.emitTypeParams(s.TypeParams); err != nil {
		return err
	}
	if err := c.nameOpStore(".type_params", loc(s)); err != nil {
		return err
	}

	if err := c.emitClassBuildCallGeneric(s); err != nil {
		return err
	}

	// Return the produced class object. The wrapper is invoked with
	// CALL 0 in the outer scope so this return value becomes the bound
	// class.
	c.addOp(RETURN_VALUE, loc(s))

	wrapperUnit := c.unit()
	wrapperUnit.Name = "<generic parameters of " + s.Name + ">"

	c.leaveScope()
	c.scope = outerScope
	c.fblocks = outerFblocks
	c.restoreCaches(outerCaches)

	// Outer scope: load the wrapper code, wrap into a function, CALL 0.
	closureFlag, err := c.emitClosure(wrapperScope, loc(s))
	if err != nil {
		return err
	}
	c.addLoadConst(wrapperUnit, loc(s))
	c.emitMakeFunction(closureFlag, loc(s))
	c.addOp(PUSH_NULL, loc(s))
	c.addOpI(CALL, 0, loc(s))
	return nil
}

// emitClassBuildCallGeneric is the inside-the-wrapper variant of
// emitClassBuildCall. After the class body is on the stack (as a
// MAKE_FUNCTION result) and the name is loaded, the wrapper loads
// .type_params, calls INTRINSIC_SUBSCRIPT_GENERIC to build the
// Generic[T,...] base, stores it under .generic_base, and emits the
// build-class call with that base spliced ahead of the user bases.
//
// CPython: Python/codegen.c:1657-1672 codegen_class (is_generic body)
func (c *Compiler) emitClassBuildCallGeneric(s *ast.ClassDef) error {
	c.addOp(LOAD_BUILD_CLASS, loc(s))
	c.addOp(PUSH_NULL, loc(s))

	innerScope := c.Symtable.Lookup(s)
	if innerScope == nil {
		return fmt.Errorf("compile: no symtable entry for class %q", s.Name)
	}
	closureFlag, err := c.emitClosure(innerScope, loc(s))
	if err != nil {
		return err
	}
	if err := c.emitInnerClassCode(innerScope, s); err != nil {
		return err
	}
	c.emitMakeFunction(closureFlag, loc(s))
	c.addLoadConst(s.Name, loc(s))

	// .type_params -> INTRINSIC_SUBSCRIPT_GENERIC -> .generic_base
	if err := c.nameOpLoad(".type_params", loc(s)); err != nil {
		return err
	}
	c.addOpI(CALL_INTRINSIC_1, intrinsicSubscriptGeneric, loc(s))
	if err := c.nameOpStore(".generic_base", loc(s)); err != nil {
		return err
	}

	return c.emitClassCallArgs(s, true)
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
	// RESUME loc = LOCATION(firstlineno, firstlineno, 0, 0); CPython
	// drops the def-stmt's columns on the class-entry RESUME.
	//
	// CPython: Python/codegen.c:654 codegen_enter_scope
	first := c.unit().FirstLineno
	c.addOpI(RESUME, 0, ast.Pos{Lineno: first, EndLineno: first})

	// MAKE_CELL for __class__ (when needs_class_closure) is inserted by
	// the cfg pipeline's prepare_localsplus; codegen does not emit it
	// inline. Same for __classdict__ below.
	//
	// CPython: Python/flowgraph.c:3760 insert_prefix_instructions

	if innerScope.NeedsClassClosure {
		// Register the cell name in the cellvars pool so the cfg
		// pipeline sees __class__ in CellVars and includes it in the
		// MAKE_CELL prologue.
		cellPool := poolCellVars
		c.poolIndex(&cellPool, "__class__")
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

	// Generic classes: load .type_params from the enclosing wrapper
	// scope's cell and store it as __type_params__ in the class namespace.
	// The .type_params symbol is registered as a Use in the class body
	// by the symtable visitor, so nameOpLoad resolves it as a Free var
	// and emits LOAD_LOCALS + LOAD_FROM_DICT_OR_DEREF.
	//
	// CPython: Python/codegen.c:1540 codegen_class_body (type_params arm)
	if len(s.TypeParams) > 0 {
		if err := c.nameOpLoad(".type_params", loc(s)); err != nil {
			return err
		}
		c.addOpName(STORE_NAME, &pool, "__type_params__", loc(s))
	}

	if innerScope.NeedsClassDict {
		// MAKE_CELL is inserted by prepare_localsplus; only register
		// __classdict__ in cellvars so the prologue picks it up, then
		// emit LOAD_LOCALS + STORE_DEREF to seed the cell with the
		// class namespace dict.
		cellPool := poolCellVars
		idx := c.poolIndex(&cellPool, "__classdict__")
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
	doc = expandTabs(doc, 8)
	margin := docMargin(doc)
	p := 0
	for p < len(doc) && doc[p] == ' ' {
		p++
	}
	if p == 0 && margin == 0 {
		return doc
	}
	var b []byte
	b, p = appendDocLine(b, doc, p)
	for p < len(doc) {
		p = skipUpTo(doc, p, margin)
		b, p = appendDocLine(b, doc, p)
	}
	return string(b)
}

// docMargin returns the minimum number of leading spaces on any non-blank
// line after the first. Lines that are entirely blank are ignored. Returns
// 0 when no following line has any indentation to strip.
func docMargin(doc string) int {
	const maxMargin = int(^uint(0) >> 1)
	margin := maxMargin
	i := skipPastNewline(doc, 0)
	for i < len(doc) {
		start := i
		for i < len(doc) && doc[i] == ' ' {
			i++
		}
		if i < len(doc) && doc[i] != '\n' {
			if i-start < margin {
				margin = i - start
			}
		}
		i = skipPastNewline(doc, i)
	}
	if margin == maxMargin {
		return 0
	}
	return margin
}

// skipPastNewline advances past the next '\n' (or end-of-string).
func skipPastNewline(doc string, i int) int {
	for i < len(doc) && doc[i] != '\n' {
		i++
	}
	if i < len(doc) {
		i++
	}
	return i
}

// skipUpTo advances at most n positions while the input is a space.
func skipUpTo(doc string, p, n int) int {
	for n > 0 && p < len(doc) && doc[p] == ' ' {
		p++
		n--
	}
	return p
}

// appendDocLine copies bytes from doc[p:] up to and including the next
// newline into b. Returns the updated buffer and position.
func appendDocLine(b []byte, doc string, p int) ([]byte, int) {
	for p < len(doc) {
		ch := doc[p]
		p++
		b = append(b, ch)
		if ch == '\n' {
			break
		}
	}
	return b, p
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
