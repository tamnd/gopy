// PEP 649 / 749 deferred annotation emission. After a class body
// finishes its statements, drain the unit's DeferredAnnotations into
// a synthetic `__annotate__` function and STORE_NAME it in the class
// namespace. Phase 5 (objects/type_attr.go:typeGetAttr) picks the
// function up on the first __annotations__ access and calls it with
// format=VALUE to materialize the dict.
//
// CPython: Python/codegen.c:786 codegen_process_deferred_annotations,
// Python/codegen.c:737 codegen_deferred_annotations_body,
// Python/codegen.c:704 codegen_setup_annotations_scope

package compile

import (
	"fmt"

	"github.com/tamnd/gopy/ast"
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
// CPython: Python/codegen.c:786 codegen_process_deferred_annotations
func (c *Compiler) emitDeferredAnnotations(l ast.Pos) error {
	u := c.unit()
	if u == nil || len(u.DeferredAnnotations) == 0 {
		return nil
	}
	annScope := c.scope.AnnotationBlock
	if annScope == nil {
		return fmt.Errorf("compile: deferred annotations present but no AnnotationBlock in symtable (scope=%q)", c.scope.Name)
	}
	deferred := u.DeferredAnnotations

	closureFlag, err := c.emitClosure(annScope, l)
	if err != nil {
		return err
	}
	if err := c.emitAnnotateBody(annScope, deferred, l); err != nil {
		return err
	}
	c.emitMakeFunction(closureFlag, l)
	pool := poolNames
	c.addOpName(STORE_NAME, &pool, "__annotate__", l)
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

	c.addOpI(RESUME, 0, l)
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

	if err := c.emitMakeCellAndCopyFree(innerScope, l); err != nil {
		return err
	}

	// Build the dict on top of stack, then for each annotation push
	// the evaluated expression, COPY the dict to the top, push the
	// name, and STORE_SUBSCR. STORE_SUBSCR consumes [value, container,
	// key] leaving the original dict at the bottom of the run.
	c.addOpI(BUILD_MAP, 0, l)
	for _, d := range deferred {
		if err := c.visitExpr(d.Value); err != nil {
			return err
		}
		// Stack here: [dict, value]. COPY 2 pushes the dict (2 below
		// the top) so STORE_SUBSCR can consume [value, dict, name]
		// while preserving the original dict for the next iteration.
		c.addOpI(COPY, 2, d.Loc)
		c.addLoadConst(d.Name, d.Loc)
		c.addOp(STORE_SUBSCR, d.Loc)
	}
	c.addOp(RETURN_VALUE, l)

	innerUnit := c.unit()
	innerUnit.Name = "__annotate__"
	innerUnit.Argcount = 1
	innerUnit.PosOnlyArgCount = 0
	innerUnit.KwOnlyArgCount = 0

	c.leaveScope()
	c.scope = outerScope
	c.fblocks = outerFblocks
	c.restoreCaches(outerCaches)

	c.addLoadConst(innerUnit, l)
	return nil
}
