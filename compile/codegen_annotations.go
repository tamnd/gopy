// PEP 649 / 749 annotation codegen. Two call sites:
//
//  1. Function defs: emitFunctionAnnotations builds an __annotate__
//     callable that is attached to the function via SET_FUNCTION_ATTRIBUTE
//     0x04.  The caller passes the result flag (0x04) back to
//     compileFunctionLike which OR's it into the MAKE_FUNCTION flags.
//
//  2. Class/module bodies: emitDeferredAnnotations drains
//     DeferredAnnotations and emits the callable as STORE_NAME __annotate__.
//
// CPython: Python/codegen.c:786 codegen_process_deferred_annotations,
// Python/codegen.c:737 codegen_deferred_annotations_body,
// Python/codegen.c:704 codegen_setup_annotations_scope,
// Python/codegen.c:1113 codegen_function_annotations

package compile

import (
	"fmt"

	"github.com/tamnd/gopy/ast"
	"github.com/tamnd/gopy/future"
	"github.com/tamnd/gopy/symtable"
)

// emitDeferredAnnotations builds the __annotate__ function from the
// current unit's DeferredAnnotations and emits the outer-scope MAKE_FUNCTION
// + STORE_NAME sequence. Caller must invoke this while the outer (class
// or module) scope is on top of c.units, before that scope's terminal
// RETURN sequence.
//
// The function takes one positional argument `format` (Format.VALUE=1
// today; FORWARDREF/STRING land with annotationlib). Free variables in
// the annotation expressions resolve via the closure machinery the
// symtable's AnnotationBlock already classified.
//
// For class bodies the synthetic function is stored as __annotate_func__
// so that a user-defined `def __annotate__` inside the class body is not
// overwritten. type_get_annotate checks __annotate__ first, then falls
// back to __annotate_func__. Module bodies keep __annotate__ (CPython
// uses the same split: ClassBlock → __annotate_func__, else __annotate__).
//
// CPython: Python/codegen.c:786 codegen_process_deferred_annotations
// CPython: Python/codegen.c:832 (ClassBlock ? __annotate_func__ : __annotate__)
func (c *Compiler) emitDeferredAnnotations(l ast.Pos) error {
	u := c.unit()
	if u == nil || len(u.DeferredAnnotations) == 0 {
		return nil
	}
	deferred := u.DeferredAnnotations

	// PEP 563 class body: CPython emits SETUP_ANNOTATIONS + inline
	// STORE_SUBSCR for each annotation rather than generating __annotate_func__.
	// get_annotate_from_class_namespace expects None (no __annotate__ key)
	// when the source was compiled under `from __future__ import annotations`.
	//
	// CPython: Python/codegen.c:874 codegen_body SETUP_ANNOTATIONS branch
	// CPython: Python/codegen.c:5498 codegen_annassign future_annotations path
	if c.Future != nil && c.Future.Bits&future.Annotations != 0 &&
		c.scope.Type == symtable.ClassBlock {
		pool := poolNames
		c.addOp(SETUP_ANNOTATIONS, l)
		for _, d := range deferred {
			s, err := ast.Unparse(d.Value)
			if err != nil {
				return fmt.Errorf("PEP 563 annotation unparse: %w", err)
			}
			c.addLoadConst(s, d.Loc)
			c.addOpName(LOAD_NAME, &pool, "__annotations__", d.Loc)
			mangled := symtable.MaybeMangle(c.unit().Private, c.scope, d.Name)
			c.addLoadConst(mangled, d.Loc)
			c.addOp(STORE_SUBSCR, d.Loc)
		}
		return nil
	}

	annScope := c.scope.AnnotationBlock
	if annScope == nil {
		return fmt.Errorf("compile: deferred annotations present but no AnnotationBlock in symtable (scope=%q)", c.scope.Name)
	}

	closureFlag, err := c.emitClosure(annScope, l)
	if err != nil {
		return err
	}
	if err := c.emitAnnotateBody(annScope, deferred, l); err != nil {
		return err
	}
	c.emitMakeFunction(closureFlag, l)
	pool := poolNames
	// For class bodies, the synthetic annotate is stored as __annotate_func__
	// so a user-defined `def __annotate__` in the class body is not overwritten.
	// typeGetAnnotate checks __annotate__ first and falls back to __annotate_func__.
	//
	// For module/function bodies, the user-defined __annotate__ (if any) is
	// classified as DefLocal in the symtable. When one is present we also use
	// __annotate_func__ so the user's definition wins. Otherwise we use __annotate__
	// so that module namespace consumers (e.g. ConditionalAnnotationTests that access
	// ns["__annotate__"] directly) continue to work.
	//
	// CPython: Python/codegen.c:832
	// (ste_type == ClassBlock ? __annotate_func__ : __annotate__)
	storeName := "__annotate__"
	if c.scope.Type == symtable.ClassBlock {
		storeName = "__annotate_func__"
	} else if c.scope.Symbols["__annotate__"]&symtable.DefLocal != 0 {
		storeName = "__annotate_func__"
	}
	c.addOpName(STORE_NAME, &pool, storeName, l)
	return nil
}

// emitAnnotateBody pushes a fresh Unit for the AnnotationBlock symtable
// entry and emits the function body that builds the annotations dict.
// Mirrors emitInnerFunctionCode minus the user-AST visitor walk: the
// body is synthetic, BUILD_MAP + per-annotation STORE_SUBSCR.
//
// CPython: Python/codegen.c:737 codegen_deferred_annotations_body
func (c *Compiler) emitAnnotateBody(innerScope *symtable.Entry, deferred []deferredAnnotation, l ast.Pos) error {
	outerScope := c.scope
	outerFblocks := c.fblocks
	outerCaches := c.savedCaches()

	c.enterScope(innerScope)

	// RESUME loc = LOCATION(firstlineno, firstlineno, 0, 0). Mirrors
	// CPython codegen_enter_scope.
	first := c.unit().FirstLineno
	c.addOpI(RESUME, 0, ast.Pos{Lineno: first, EndLineno: first})
	// The annotate function takes a single positional argument named
	// "format". CPython's generated __annotate__ raises
	// NotImplementedError for format > VALUE_WITH_FAKE_GLOBALS (2)
	// so that annotationlib can take over the FORWARDREF (3) and
	// STRING (4) paths through _StringifierDict. We emit:
	//
	//   LOAD_FAST 0 (format)
	//   LOAD_CONST 2 (VALUE_WITH_FAKE_GLOBALS)
	//   COMPARE_OP GT
	//   POP_JUMP_IF_FALSE body
	//   LOAD_COMMON_CONSTANT NotImplementedError
	//   RAISE_VARARGS 1
	//   body:
	//
	// CPython: Python/codegen.c:676 codegen_setup_annotations_scope
	c.declareArg("format")
	body := c.newLabel()
	c.addOpI(LOAD_FAST, 0, l)
	c.addLoadConst(int64(2), l) // VALUE_WITH_FAKE_GLOBALS
	c.addOpI(COMPARE_OP, int32(cmpGt), l)
	c.addOpJump(POP_JUMP_IF_FALSE, body, l)
	c.addOpI(LOAD_COMMON_CONSTANT, constantNotImplementedError, l)
	c.addOpI(RAISE_VARARGS, 1, l)
	c.useLabel(body)

	// MAKE_CELL / COPY_FREE_VARS get inserted by the cfg pipeline's
	// prepare_localsplus at the entry block; codegen no longer emits
	// them inline.
	//
	// CPython: Python/flowgraph.c:3760 insert_prefix_instructions
	_ = innerScope

	// Build the dict on top of stack, then for each annotation push
	// the evaluated expression, COPY the dict to the top, push the
	// name, and STORE_SUBSCR. STORE_SUBSCR consumes [value, container,
	// key] leaving the original dict at the bottom of the run.
	//
	// For conditional annotations (CondIdx >= 0) guard the store with:
	//   LOAD_CONST condIdx
	//   LOAD_DEREF/__conditional_annotations__  (class) or LOAD_GLOBAL (module)
	//   CONTAINS_OP 0 (in)
	//   POP_JUMP_IF_FALSE not_set
	// so annotations from code paths that were not executed are omitted.
	//
	// CPython: Python/codegen.c:748 codegen_deferred_annotations_body
	c.addOpI(BUILD_MAP, 0, l)
	for _, d := range deferred {
		var notSet JumpTargetLabel
		if d.CondIdx >= 0 {
			notSet = c.newLabel()
			c.addLoadConst(int64(d.CondIdx), d.Loc)
			if outerScope.Type == symtable.ClassBlock {
				pool := poolFreeVars
				idx := c.poolIndex(&pool, "__conditional_annotations__")
				c.addOpI(LOAD_DEREF, int32(len(c.unit().CellVars)+idx), d.Loc)
			} else {
				pool := poolNames
				c.addOpName(LOAD_GLOBAL, &pool, "__conditional_annotations__", d.Loc)
			}
			c.addOpI(CONTAINS_OP, 0, d.Loc)
			c.addOpJump(POP_JUMP_IF_FALSE, notSet, d.Loc)
		}
		if c.Future != nil && c.Future.Bits&future.Annotations != 0 {
			// PEP 563: store annotation as its source-text string.
			// CPython: Python/codegen.c:676 compiler_visit_annexpr
			s, err := ast.Unparse(d.Value)
			if err != nil {
				return fmt.Errorf("PEP 563 annotation unparse: %w", err)
			}
			c.addLoadConst(s, d.Loc)
		} else if err := c.visitExpr(d.Value); err != nil {
			return err
		}
		// Stack here: [dict, value]. COPY 2 pushes the dict (2 below
		// the top) so STORE_SUBSCR can consume [value, dict, name]
		// while preserving the original dict for the next iteration.
		c.addOpI(COPY, 2, d.Loc)
		// Name mangling: __foo in class C becomes _C__foo. The annotation
		// scope inherits u_private from the enclosing class unit, so
		// c.unit().Private is the class name. outerScope (the class body
		// symtable entry) has MangledNames=nil for ordinary classes, so
		// MaybeMangle falls through to Mangle unconditionally.
		//
		// CPython: Python/codegen.c:753 codegen_deferred_annotations_body
		// _PyCompile_Mangle(c, st->v.AnnAssign.target->v.Name.id)
		mangled := symtable.MaybeMangle(c.unit().Private, outerScope, d.Name)
		c.addLoadConst(mangled, d.Loc)
		c.addOp(STORE_SUBSCR, d.Loc)
		if d.CondIdx >= 0 {
			c.useLabel(notSet)
		}
	}
	c.addOp(RETURN_VALUE, l)

	innerUnit := c.unit()
	innerUnit.Name = "__annotate__"
	// format is positional-only (co_posonlyargcount=1). unit.Argcount tracks
	// pos-or-kw args only; posonly count is separate.
	// CPython: Python/codegen.c:676 codegen_setup_annotations_scope
	innerUnit.Argcount = 0
	innerUnit.PosOnlyArgCount = 1
	innerUnit.KwOnlyArgCount = 0

	c.leaveScope()
	c.scope = outerScope
	c.fblocks = outerFblocks
	c.restoreCaches(outerCaches)

	c.addLoadConst(innerUnit, l)
	return nil
}

// stashAnnotationCode implements the CPython annotation-setup stash for
// module (and class) bodies. It swaps out the active instruction sequence,
// emits the __conditional_annotations__ prologue (when needed) and the
// __annotate__ function into a fresh sequence, attaches that sequence to
// the main seq via SetAnnotationsCode, then restores the main seq.
// cfgFromSequence later prepends the stash before the body instructions.
//
// For class bodies the stash is called by visitClassBody instead.
//
// CPython: Python/compile.c:739 _PyCompile_StartAnnotationSetup /
// Python/compile.c:762 _PyCompile_EndAnnotationSetup
func (c *Compiler) stashAnnotationCode(l ast.Pos) error {
	u := c.unit()
	hasCondAnno := c.scope != nil && c.scope.HasConditionalAnnotations
	if len(u.DeferredAnnotations) == 0 && !hasCondAnno {
		return nil
	}
	mainSeq := u.Seq
	annoSeq := &Sequence{}
	u.Seq = annoSeq
	if hasCondAnno {
		pool := poolNames
		c.addOpI(BUILD_SET, 0, l)
		c.addOpName(STORE_NAME, &pool, "__conditional_annotations__", l)
	}
	if err := c.emitDeferredAnnotations(l); err != nil {
		u.Seq = mainSeq
		return err
	}
	u.Seq = mainSeq
	if len(annoSeq.Instrs) > 0 {
		mainSeq.SetAnnotationsCode(annoSeq)
	}
	return nil
}

// emitFunctionAnnotations compiles the __annotate__ function for a
// function definition and leaves it on the outer stack. Returns 0x04
// (MAKE_FUNCTION_ANNOTATE) when annotations exist, 0 otherwise.
//
// The AnnotationBlock for the function's args is looked up via
// Symtable.Lookup(args), which was registered by
// symtable.visitAnnotations under the key *ast.Arguments.
//
// CPython: Python/codegen.c:1113 codegen_function_annotations,
// Python/codegen.c:1081 codegen_annotations_in_scope,
// Python/codegen.c:1029 codegen_argannotation
func (c *Compiler) emitFunctionAnnotations(args *ast.Arguments, returns ast.Expr, l ast.Pos) (int32, error) {
	annScope := c.Symtable.Lookup(args)
	if annScope == nil || !annScope.AnnotationsUsed {
		return 0, nil
	}

	closureFlag, err := c.emitClosure(annScope, l)
	if err != nil {
		return 0, err
	}

	if err := c.emitFunctionAnnotateBody(annScope, args, returns, l); err != nil {
		return 0, err
	}
	c.emitMakeFunction(closureFlag, l)
	return 0x04, nil
}

// emitFunctionAnnotateBody pushes a fresh Unit for the function's
// AnnotationBlock and emits the body: format guard, BUILD_MAP,
// per-annotation STORE_SUBSCR for every annotated parameter and the
// return annotation, RETURN_VALUE.
//
// CPython: Python/codegen.c:1081 codegen_annotations_in_scope,
// Python/codegen.c:1029 codegen_argannotation,
// Python/codegen.c:666  codegen_setup_annotations_scope
func (c *Compiler) emitFunctionAnnotateBody(annScope *symtable.Entry, args *ast.Arguments, returns ast.Expr, l ast.Pos) error {
	outerScope := c.scope
	outerFblocks := c.fblocks
	outerCaches := c.savedCaches()

	c.enterScope(annScope)

	first := c.unit().FirstLineno
	c.addOpI(RESUME, 0, ast.Pos{Lineno: first, EndLineno: first})
	c.declareArg("format")
	body := c.newLabel()
	c.addOpI(LOAD_FAST, 0, l)
	c.addLoadConst(int64(2), l) // VALUE_WITH_FAKE_GLOBALS
	c.addOpI(COMPARE_OP, int32(cmpGt), l)
	c.addOpJump(POP_JUMP_IF_FALSE, body, l)
	c.addOpI(LOAD_COMMON_CONSTANT, constantNotImplementedError, l)
	c.addOpI(RAISE_VARARGS, 1, l)
	c.useLabel(body)

	c.addOpI(BUILD_MAP, 0, l)

	count := 0
	emitOne := func(name string, annot ast.Expr) error {
		if annot == nil {
			return nil
		}
		mangled := symtable.MaybeMangle(c.unit().Private, c.scope, name)
		if c.Future != nil && c.Future.Bits&future.Annotations != 0 {
			// PEP 563: store annotation as its source-text string.
			// CPython: Python/codegen.c:676 compiler_visit_annexpr
			s, err := ast.Unparse(annot)
			if err != nil {
				return fmt.Errorf("PEP 563 annotation unparse: %w", err)
			}
			c.addLoadConst(s, l)
		} else if starred, ok := annot.(*ast.Starred); ok {
			if err := c.visitExpr(starred.Value); err != nil {
				return err
			}
			c.addOpI(UNPACK_SEQUENCE, 1, l)
		} else {
			if err := c.visitExpr(annot); err != nil {
				return err
			}
		}
		c.addOpI(COPY, 2, l)
		c.addLoadConst(mangled, l)
		c.addOp(STORE_SUBSCR, l)
		count++
		return nil
	}

	for _, a := range args.Posonlyargs {
		if err := emitOne(a.Arg, a.Annotation); err != nil {
			return err
		}
	}
	for _, a := range args.Args {
		if err := emitOne(a.Arg, a.Annotation); err != nil {
			return err
		}
	}
	if args.Vararg != nil {
		if err := emitOne(args.Vararg.Arg, args.Vararg.Annotation); err != nil {
			return err
		}
	}
	for _, a := range args.Kwonlyargs {
		if err := emitOne(a.Arg, a.Annotation); err != nil {
			return err
		}
	}
	if args.Kwarg != nil {
		if err := emitOne(args.Kwarg.Arg, args.Kwarg.Annotation); err != nil {
			return err
		}
	}
	if returns != nil {
		if err := emitOne("return", returns); err != nil {
			return err
		}
	}

	c.addOp(RETURN_VALUE, l)

	innerUnit := c.unit()
	innerUnit.Name = "__annotate__"
	// format is positional-only. CPython: Python/codegen.c:676
	innerUnit.Argcount = 0
	innerUnit.PosOnlyArgCount = 1
	innerUnit.KwOnlyArgCount = 0

	c.leaveScope()
	c.scope = outerScope
	c.fblocks = outerFblocks
	c.restoreCaches(outerCaches)

	c.addLoadConst(innerUnit, l)
	_ = count
	return nil
}
