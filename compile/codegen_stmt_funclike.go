// Port of cpython/Python/codegen.c function-like visitors
// (L1311-L1727). FunctionDef / AsyncFunctionDef / Lambda. ClassDef
// and TypeAlias share the same enter / leave scope shape; they live
// in sibling files.

package compile

import (
	"fmt"

	"github.com/tamnd/gopy/ast"
	"github.com/tamnd/gopy/symtable"
)

// visitFunctionDef compiles a `def f(...): ...` statement. The body
// runs in a fresh scope; the outer scope receives a MAKE_FUNCTION
// referencing the produced code object plus the right closure /
// defaults flags.
//
// CPython: Python/codegen.c:L1390 codegen_function
func (c *Compiler) visitFunctionDef(s *ast.FunctionDef) error {
	return c.compileFunctionLike(s.Name, s.Args, s.Body, s.Returns,
		s.DecoratorList, s.TypeParams, false, s)
}

// visitAsyncFunctionDef compiles `async def`. Same shape as
// FunctionDef but with CO_COROUTINE on the inner unit (set via the
// symtable Coroutine flag).
//
// CPython: Python/codegen.c:L1390 codegen_function with is_async=1
func (c *Compiler) visitAsyncFunctionDef(s *ast.AsyncFunctionDef) error {
	return c.compileFunctionLike(s.Name, s.Args, s.Body, s.Returns,
		s.DecoratorList, s.TypeParams, false, s)
}

// visitLambda compiles a Lambda expression. Same shape as a function
// body except the body is a single expression that becomes the
// return value, and the inner code object's name is "<lambda>".
//
// CPython: Python/codegen.c:L1999 codegen_lambda
func (c *Compiler) visitLambda(e *ast.Lambda) error {
	body := ast.Seq[ast.Stmt]{&ast.Return{Value: e.Body, Pos: e.Pos}}
	return c.compileFunctionLike("<lambda>", e.Args, body, nil, nil, nil, true, e)
}

// compileFunctionLike is the shared emit driver. It evaluates
// defaults in the outer scope, switches into the inner scope to emit
// the body, then emits MAKE_FUNCTION in the outer scope.
//
// CPython: Python/codegen.c:L1311 codegen_function_body driver
func (c *Compiler) compileFunctionLike(name string, args *ast.Arguments,
	body ast.Seq[ast.Stmt], returns ast.Expr, decorators ast.Seq[ast.Expr],
	typeParams ast.Seq[ast.TypeParam], isLambda bool, scopeKey any,
) error {
	// Evaluate decorator expressions (in source order, top-to-bottom)
	// before the function is created. They wrap the result of
	// MAKE_FUNCTION in reverse order via CALL.
	if err := c.visitDecorators(decorators); err != nil {
		return err
	}

	// Evaluate defaults in the outer scope. They land on the stack
	// and are folded into MAKE_FUNCTION via the flags oparg.
	flags, err := c.emitDefaults(args, loc(scopeKey))
	if err != nil {
		return err
	}

	isGeneric := len(typeParams) > 0

	var (
		outerScope    *symtable.Entry
		outerFblocks  []fblock
		outerCaches   savedCaches
		wrapperScope  *symtable.Entry
		wrapperUnit   *Unit
		numTPArgs     int32
		hasDefaults   = flags&0x01 != 0
		hasKwDefaults = flags&0x02 != 0
	)

	if isGeneric {
		wrapperScope = c.Symtable.LookupTypeParams(scopeKey)
		if wrapperScope == nil {
			return fmt.Errorf("compile: no TypeParametersBlock for %s %q", funcLikeKind(scopeKey), name)
		}
		if hasDefaults {
			numTPArgs++
		}
		if hasKwDefaults {
			numTPArgs++
		}
		// Both defaults and kwdefaults present: outer stack is
		// [defaults_tuple, kwdefaults_dict] with kwdefaults on TOS.
		// Swap so the wrapper's first positional arg (`.defaults`)
		// receives defaults_tuple and the second (`.kwdefaults`)
		// receives kwdefaults_dict after the eventual outer CALL.
		//
		// CPython: Python/codegen.c:1441 (num_typeparam_args==2 SWAP)
		if numTPArgs == 2 {
			c.addOpI(SWAP, 2, loc(scopeKey))
		}

		outerScope = c.scope
		outerFblocks = c.fblocks
		outerCaches = c.savedCaches()

		c.enterScope(wrapperScope)
		first := c.unit().FirstLineno
		c.addOpI(RESUME, 0, ast.Pos{Lineno: first, EndLineno: first})

		// Declare the wrapper's positional parameters so the var pool
		// allocates slot 0 (and slot 1 if both flavors are present)
		// in the order CPython expects from
		// codegen_function: .defaults first, .kwdefaults second.
		//
		// CPython: Python/codegen.c:1448 _PyCompile_CodeUnitMetadata u_argcount
		if hasDefaults {
			c.declareArg(".defaults")
		}
		if hasKwDefaults {
			c.declareArg(".kwdefaults")
		}
		c.unit().Argcount = int(numTPArgs)

		if err := c.emitTypeParams(typeParams); err != nil {
			return err
		}
		// Load the wrapper's positional args back so codegen_function_body's
		// MAKE_FUNCTION sees [type_params, defaults?, kwdefaults?, ...].
		//
		// CPython: Python/codegen.c:1456 LOAD_FAST loop
		for i := int32(0); i < numTPArgs; i++ {
			c.addOpI(LOAD_FAST, i, loc(scopeKey))
		}
	}

	// Compile the __annotate__ callable for PEP 649 lazy annotations.
	// For generic functions this runs inside the TypeParams scope (c.scope
	// is already wrapperScope after the isGeneric block above), matching
	// CPython where codegen_function_annotations is called after
	// codegen_enter_scope for type params.
	//
	// The callable must be pushed before emitClosure and
	// emitInnerFunctionCode because emitMakeFunction emits
	// SET_FUNCTION_ATTRIBUTE 0x04 after 0x08 (closure), expecting the
	// annotate callable to be one deeper than the closure tuple.
	//
	// CPython: Python/codegen.c:1113 codegen_function_annotations
	if !isLambda {
		annotFlag, err := c.emitFunctionAnnotations(args, returns, loc(scopeKey))
		if err != nil {
			return err
		}
		flags |= annotFlag
	}

	// Look up the inner scope from the symtable.
	innerScope := c.Symtable.Lookup(scopeKey)
	if innerScope == nil {
		return fmt.Errorf("compile: no symtable entry for %s %q", funcLikeKind(scopeKey), name)
	}

	// Emit closure cells before MAKE_FUNCTION so the inner code can
	// see them. Returns the closure flag bit (0x08) if any free vars
	// existed.
	closureFlag, err := c.emitClosure(innerScope, loc(scopeKey))
	if err != nil {
		return err
	}
	flags |= closureFlag

	if err := c.emitInnerFunctionCode(innerScope, name, args, body, scopeKey); err != nil {
		return err
	}

	c.emitMakeFunction(flags, loc(scopeKey))

	if isGeneric {
		// Wrapper stack: [type_params_tuple, func]. Attach the
		// type-params tuple to the new function object.
		//
		// CPython: Python/codegen.c:1479 SWAP 2 + CALL_INTRINSIC_2 SET_FUNCTION_TYPE_PARAMS
		c.addOpI(SWAP, 2, loc(scopeKey))
		c.addOpI(CALL_INTRINSIC_2, intrinsicSetFunctionTypeParams, loc(scopeKey))
		c.addOp(RETURN_VALUE, loc(scopeKey))

		wrapperUnit = c.unit()
		wrapperUnit.Name = "<generic parameters of " + name + ">"

		c.leaveScope()
		c.scope = outerScope
		c.fblocks = outerFblocks
		c.restoreCaches(outerCaches)

		// Outer scope: push closure + wrapper code + MAKE_FUNCTION.
		wrapperClosureFlag, err := c.emitClosure(wrapperScope, loc(scopeKey))
		if err != nil {
			return err
		}
		c.addLoadConst(wrapperUnit, loc(scopeKey))
		c.emitMakeFunction(wrapperClosureFlag, loc(scopeKey))

		// Outer stack: [defaults?, kwdefaults?, wrapper_func].
		// Move the wrapper to the bottom so CALL's func/self_or_null
		// slots resolve to wrapper(d, k).
		//
		// CPython: Python/codegen.c:1490 SWAP num+1 + CALL num-1
		if numTPArgs > 0 {
			c.addOpI(SWAP, numTPArgs+1, loc(scopeKey))
			c.addOpI(CALL, numTPArgs-1, loc(scopeKey))
		} else {
			c.addOp(PUSH_NULL, loc(scopeKey))
			c.addOpI(CALL, 0, loc(scopeKey))
		}
	}

	// Apply decorators. Each emits CALL 0: 3.14 lays out the stack as
	// [..., decorator, wrapped] and CALL's MAYBE_EXPAND_METHOD path
	// promotes the non-NULL self_or_null slot (wrapped) into the first
	// positional arg, yielding decorator(wrapped).
	//
	// CPython: Python/codegen.c:976 codegen_apply_decorators
	for range decorators {
		c.addOpI(CALL, 0, loc(scopeKey))
	}

	if isLambda {
		// Caller wants the value on the stack.
		return nil
	}

	// Bind the produced function object to the def name in the outer
	// scope.
	return c.nameOpStore(name, loc(scopeKey))
}

// emitInnerFunctionCode pushes a fresh unit, emits the body, and pops.
// On entry the outer unit is responsible for having already pushed
// any defaults / closure tuple needed by MAKE_FUNCTION. After this
// returns, the inner code object sits on top of the outer stack.
//
// CPython: Python/codegen.c:L1311 codegen_function_body
func (c *Compiler) emitInnerFunctionCode(innerScope *symtable.Entry,
	name string, args *ast.Arguments, body ast.Seq[ast.Stmt], key any,
) error {
	// Save the outer scope so we can restore it after the inner body.
	outerScope := c.scope
	outerFblocks := c.fblocks
	outerCaches := c.savedCaches()

	c.enterScope(innerScope)

	// Pin the docstring at consts[0] before RESUME / declareArgs run,
	// so the first const slot belongs to the docstring (functions surface
	// __doc__ via co_consts[0] when CO_HAS_DOCSTRING is set).
	body = c.consumeDocstring(body)

	// RESUME 0 marks the function entry point for the eval loop.
	// Mirrors CPython codegen_enter_scope: loc = LOCATION(firstlineno,
	// firstlineno, 0, 0). Using loc(key) would carry the def stmt's
	// columns, which CPython drops on the function-entry RESUME.
	//
	// CPython: Python/codegen.c:654 codegen_enter_scope (loc =
	// LOCATION(lineno, lineno, 0, 0); RESUME RESUME_AT_FUNC_START)
	first := c.unit().FirstLineno
	c.addOpI(RESUME, 0, ast.Pos{Lineno: first, EndLineno: first})

	if err := c.declareArgs(args); err != nil {
		return err
	}

	// MAKE_CELL / COPY_FREE_VARS are not emitted by codegen: the cfg
	// pipeline's prepare_localsplus / insert_prefix_instructions add
	// them at the entry block right after RESUME (and rewrites every
	// deref oparg to a localsplus offset via fix_cell_offsets).
	//
	// CPython: Python/flowgraph.c:3760 insert_prefix_instructions

	if err := c.visitStmts(body); err != nil {
		return err
	}

	// For generators and coroutines, wrap the body in a SETUP_CLEANUP that
	// converts any escaping StopIteration into RuntimeError (PEP 479).
	// wrapInStopIterationHandler already appends LOAD_CONST None / RETURN_VALUE
	// as the normal exit path, so addReturnNoneIfMissing is not needed.
	// For regular functions, fall back to addReturnNoneIfMissing.
	//
	// CPython: Python/codegen.c:1363 codegen_function_body (add_stopiteration_handler)
	// CPython: Python/codegen.c:1175 codegen_wrap_in_stopiteration_handler
	if innerScope.Generator || innerScope.Coroutine {
		c.wrapInStopIterationHandler()
	} else {
		// CPython: Python/codegen.c:867 codegen_body (implicit return None)
		c.addReturnNoneIfMissing(ast.Pos{Lineno: -1})
	}

	innerUnit := c.unit()
	innerUnit.Name = name
	// CPython convention: u_argcount counts positional-or-keyword args
	// only, NOT positional-only ones. assemble.c:makecode sums posonly +
	// posorkw into co_argcount. Keeping the unit fields aligned with
	// CPython means the cfg passes, makecode, and the dis-side metadata
	// dump all read the same numbers.
	//
	// CPython: Python/codegen.c:1338 u_argcount = asdl_seq_LEN(args->args)
	innerUnit.Argcount = len(args.Args)
	innerUnit.PosOnlyArgCount = len(args.Posonlyargs)
	innerUnit.KwOnlyArgCount = len(args.Kwonlyargs)
	if args.Vararg != nil {
		innerUnit.Flags |= CoVarargs
	}
	if args.Kwarg != nil {
		innerUnit.Flags |= CoVarkeywords
	}

	c.leaveScope()
	c.scope = outerScope
	c.fblocks = outerFblocks
	c.restoreCaches(outerCaches)

	// The inner Unit is appended to the outer unit's Consts pool
	// and a LOAD_CONST of that index is emitted. The assembler
	// translates Unit to a real code object during marshal.
	c.addLoadConst(innerUnit, loc(key))

	return nil
}

// declareArgs registers each parameter in the per-unit varnames pool
// in declaration order: posonly, args, varargs, kwonly, kwargs.
// Mirrors CPython's compiler_arguments.
//
// CPython: Python/compile.c compiler_arguments
func (c *Compiler) declareArgs(args *ast.Arguments) error {
	for _, a := range args.Posonlyargs {
		c.declareArg(a.Arg)
	}
	for _, a := range args.Args {
		c.declareArg(a.Arg)
	}
	if args.Vararg != nil {
		c.declareArg(args.Vararg.Arg)
	}
	for _, a := range args.Kwonlyargs {
		c.declareArg(a.Arg)
	}
	if args.Kwarg != nil {
		c.declareArg(args.Kwarg.Arg)
	}
	return nil
}

// declareArg adds name to the per-unit varnames pool. Names that look
// private (`__foo`, not dunders) are mangled against the enclosing
// class so the slot matches the LOAD_FAST / kwdefaults lookups, which
// also mangle via codegen_nameop. Without this the class-scope kwarg
// `__a` would pool both `__a` and `_X__a`, doubling the slot count.
//
// CPython: Python/compile.c compiler_arguments per-arg slot (the
// arg name is mangled before being added to u_varnames).
func (c *Compiler) declareArg(name string) {
	pool := poolVarNames
	mangled := symtable.MaybeMangle(c.unit().Private, c.scope, name)
	c.poolIndex(&pool, mangled)
}

// emitDefaults evaluates positional default expressions and the
// kwonly defaults dict, leaving them on the outer stack. Returns the
// bit pattern that goes into MAKE_FUNCTION's oparg.
//
// CPython: Python/codegen.c:L1146 codegen_defaults +
//
//	Python/codegen.c:L990 codegen_kwonlydefaults
func (c *Compiler) emitDefaults(args *ast.Arguments, l ast.Pos) (int32, error) {
	var flags int32
	if n := len(args.Defaults); n > 0 {
		for _, d := range args.Defaults {
			if err := c.visitExpr(d); err != nil {
				return 0, err
			}
		}
		c.addOpI(BUILD_TUPLE, int32(n), l)
		flags |= 0x01
	}
	// kwonly defaults: BUILD a dict from interleaved name/value
	// constants. Skipped when no kwonly default is set (None
	// sentinel in the parallel array).
	hasKwDefault := false
	for _, d := range args.KwDefaults {
		if d != nil {
			hasKwDefault = true
			break
		}
	}
	if hasKwDefault {
		count := int32(0)
		for i, d := range args.KwDefaults {
			if d == nil {
				continue
			}
			// CPython mangles each kwarg name against the enclosing
			// class so the kwdefaults dict key matches the
			// function's mangled fast-local slot. Without this the
			// `def f(self, *, __a=42)` inside `class X` builds the
			// dict with key '__a' while f's slot is named '_X__a',
			// so initialize_locals' default fill misses.
			//
			// CPython: Python/codegen.c codegen_function_body kwdefaults loop
			mangled := symtable.MaybeMangle(c.unit().Private, c.scope, args.Kwonlyargs[i].Arg)
			c.addLoadConst(mangled, l)
			if err := c.visitExpr(d); err != nil {
				return 0, err
			}
			count++
		}
		c.addOpI(BUILD_MAP, count, l)
		flags |= 0x02
	}
	return flags, nil
}

// emitMakeFunction emits MAKE_FUNCTION followed by one
// SET_FUNCTION_ATTRIBUTE per set flag bit, matching the 3.14 split:
// MAKE_FUNCTION just wraps the code object, then each attribute
// (closure, annotations, kwdefaults, defaults) gets stamped on by a
// separate op that consumes the value pushed earlier in the outer
// scope. The order is closure -> annotations -> kwdefaults ->
// defaults so each op finds its value sitting just below the
// function on the stack.
//
// CPython: Python/codegen.c:L923 codegen_make_closure
func (c *Compiler) emitMakeFunction(flags int32, l ast.Pos) {
	c.addOp(MAKE_FUNCTION, l)
	if flags&0x08 != 0 {
		c.addOpI(SET_FUNCTION_ATTRIBUTE, 0x08, l)
	}
	if flags&0x04 != 0 {
		c.addOpI(SET_FUNCTION_ATTRIBUTE, 0x04, l)
	}
	if flags&0x02 != 0 {
		c.addOpI(SET_FUNCTION_ATTRIBUTE, 0x02, l)
	}
	if flags&0x01 != 0 {
		c.addOpI(SET_FUNCTION_ATTRIBUTE, 0x01, l)
	}
}

// emitClosure emits the closure-cell tuple if the inner scope has
// any free variables. Returns the MAKE_FUNCTION oparg bit (0x08) if
// a closure tuple was pushed.
//
// CPython: Python/codegen.c:L923 codegen_make_closure
func (c *Compiler) emitClosure(inner *symtable.Entry, l ast.Pos) (int32, error) {
	freeNames := freeVarsOf(inner)
	if len(freeNames) == 0 {
		return 0, nil
	}
	for _, name := range freeNames {
		// Implicit class-scope cells (__class__, __classdict__): a method
		// captures the class's implicit closure cell, which is a separate
		// compiler cellvar registered via NeedsClassClosure. The class body
		// may ALSO reference the same name as a freevar (reading an enclosing
		// binding, e.g. zero-arg super()'s __class__) or bind it as a
		// namespace LOCAL, so the symbol scope is not necessarily Cell. The
		// method's cell is always the cellvar, so resolve it from the cellvar
		// pool before consulting the (possibly Free) symbol scope.
		//
		// CPython: Python/symtable.c:958 drop_class_free (needs_class_closure)
		if c.scope.Type == symtable.ClassBlock &&
			((name == "__class__" && c.scope.NeedsClassClosure) ||
				(name == "__classdict__" && c.scope.NeedsClassDict)) {
			pool := poolCellVars
			c.addOpName(LOAD_CLOSURE, &pool, name, l)
			continue
		}
		// Each free var is a cell or free in the outer scope. Push the
		// outer's cell (not its contents) onto the stack so BUILD_TUPLE
		// collects it into the closure. LOAD_CLOSURE reads
		// LocalsPlus[NLocals + oparg]; oparg is the cellvar index for
		// cells and NCells+freevar_index for frees, matching the same
		// deref-index space LOAD_DEREF uses.
		//
		// CPython: Python/codegen.c:923 codegen_make_closure (the
		// LOAD_CLOSURE arm)
		scope := c.scope.GetScope(name)
		switch scope {
		case symtable.Cell:
			pool := poolCellVars
			c.addOpName(LOAD_CLOSURE, &pool, name, l)
		case symtable.Free:
			pool := poolFreeVars
			idx := c.poolIndex(&pool, name)
			c.addOpI(LOAD_CLOSURE, int32(len(c.unit().CellVars)+idx), l)
		default:
			// DEF_FREE_CLASS: the outer is a class scope where this
			// name is bound locally (the class-body def/assign) but
			// the inner method still wants the enclosing function's
			// cell. enterScope put the name in u.FreeVars; load it
			// from there.
			//
			// CPython: Python/compile.c:641 compiler_enter_scope
			// (dictbytype FREE | DEF_FREE_CLASS)
			if c.scope.Symbols[name]&symtable.DefFreeClass != 0 {
				pool := poolFreeVars
				idx := c.poolIndex(&pool, name)
				c.addOpI(LOAD_CLOSURE, int32(len(c.unit().CellVars)+idx), l)
				continue
			}
			return 0, fmt.Errorf("compile: free var %q in nested scope %q has scope %v in outer %q",
				name, inner.Name, scope, c.scope.Name)
		}
	}
	c.addOpI(BUILD_TUPLE, int32(len(freeNames)), l)
	return 0x08, nil
}

// freeVarsOf returns the free-variable names of the inner scope in
// stable order. CPython uses dictbytype(symbols, FREE, DEF_FREE_CLASS,
// ...): names scoped FREE plus class-locals flagged DEF_FREE_CLASS
// (the latter happens when a class body binds a name AND a nested
// method references it; the cell still has to flow through).
//
// CPython: Python/compile.c:641 compiler_enter_scope
// (dictbytype(symbols, FREE, DEF_FREE_CLASS, ...))
func freeVarsOf(sc *symtable.Entry) []string {
	var out []string
	for name, flags := range sc.Symbols {
		if flags.Scope() == symtable.Free || flags&symtable.DefFreeClass != 0 {
			out = append(out, name)
		}
	}
	// Stable ordering: CPython sorts by insertion order of the
	// symbols dict; we sort by name to keep tests deterministic.
	sortStrings(out)
	return out
}

// visitDecorators evaluates a decorator list, leaving each callable
// on the stack in source order. The wrapped value (function or
// class) is pushed by the caller; each subsequent CALL 0 then
// consumes [decorator, wrapped] and pushes decorator(wrapped). The
// 3.14 CALL convention treats a non-NULL self_or_null slot as the
// first positional arg, so no PUSH_NULL is needed here.
//
// CPython: Python/codegen.c:962 codegen_decorators
func (c *Compiler) visitDecorators(decos ast.Seq[ast.Expr]) error {
	for _, d := range decos {
		if err := c.visitExpr(d); err != nil {
			return err
		}
	}
	return nil
}

// savedCaches captures the per-unit dedup caches that have to be
// swapped on enterScope. The outer scope's caches are restored after
// the inner body closes.
type savedCaches struct {
	consts   map[any]int
	names    map[string]int
	varnames map[string]int
	free     map[string]int
	cell     map[string]int
}

// savedCaches snapshots the dedup caches before entering an inner
// scope so the outer scope can resume with its own pools intact.
//
// CPython: Python/compile.c compiler_enter_scope save side
func (c *Compiler) savedCaches() savedCaches {
	return savedCaches{
		consts:   c.constCache,
		names:    c.nameCache,
		varnames: c.varnameCache,
		free:     c.freeCache,
		cell:     c.cellCache,
	}
}

// restoreCaches puts back the outer-scope dedup caches captured by
// savedCaches.
//
// CPython: Python/compile.c compiler_exit_scope restore side
func (c *Compiler) restoreCaches(s savedCaches) {
	c.constCache = s.consts
	c.nameCache = s.names
	c.varnameCache = s.varnames
	c.freeCache = s.free
	c.cellCache = s.cell
}

// funcLikeKind returns "function" / "async function" / "lambda" for
// error messages. Go-only helper.
func funcLikeKind(s any) string {
	switch s.(type) {
	case *ast.FunctionDef:
		return "function"
	case *ast.AsyncFunctionDef:
		return "async function"
	case *ast.Lambda:
		return "lambda"
	}
	return "function-like"
}

// sortStrings is a tiny insertion sort to avoid the sort import.
// Callers use this only on small slices (free var lists, kwonly
// names) so O(n^2) is fine. Go-only helper.
func sortStrings(xs []string) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j-1] > xs[j]; j-- {
			xs[j-1], xs[j] = xs[j], xs[j-1]
		}
	}
}
