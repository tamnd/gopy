// Port of cpython/Python/codegen.c statement visitors for the
// remaining simple kinds: Delete, AugAssign, AnnAssign, Raise,
// Assert, Import, ImportFrom.

package compile

import (
	"fmt"

	"github.com/tamnd/gopy/ast"
	"github.com/tamnd/gopy/symtable"
)

// visitDelete emits a delete sequence for each target. Each target's
// location info comes from the target node itself, matching CPython's
// VISIT_SEQ(c, expr, s->v.Delete.targets) dispatch.
//
// CPython: Python/codegen.c:3003 Delete_kind in codegen_visit_stmt
func (c *Compiler) visitDelete(s *ast.Delete) error {
	for _, t := range s.Targets {
		if err := c.deleteTarget(t); err != nil {
			return err
		}
	}
	return nil
}

// deleteTarget routes a single `del` target to nameOpDelete /
// visitAttribute / visitSubscript based on its concrete type. Names
// take their location from the target itself; Attribute / Subscript
// reuse their own visit helper which picks loc internally.
//
// CPython: Python/codegen.c codegen_delete per-target visit_expr
func (c *Compiler) deleteTarget(t ast.Expr) error {
	switch tt := t.(type) {
	case *ast.Name:
		return c.nameOpDelete(tt.Id, loc(tt))
	case *ast.Attribute:
		return c.visitAttribute(tt)
	case *ast.Subscript:
		return c.visitSubscript(tt)
	case *ast.Tuple:
		// `del (a, b, c[0])` recurses into each element with the same
		// Del context. CPython routes through codegen_tuple, which for
		// a Del-context Tuple just VISIT_SEQs the elements.
		//
		// CPython: Python/codegen.c:3449 codegen_tuple (Del branch)
		for _, e := range tt.Elts {
			if err := c.deleteTarget(e); err != nil {
				return err
			}
		}
		return nil
	case *ast.List:
		// `del [a, b]` has the same shape as `del (a, b)`.
		//
		// CPython: Python/codegen.c:3431 codegen_list (Del branch)
		for _, e := range tt.Elts {
			if err := c.deleteTarget(e); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("compile: cannot delete target %T", t)
}

// visitAugAssign emits the in-place compound assign. The target is
// loaded once, the value evaluated, and BINARY_OP with the NB_INPLACE_*
// subcode is emitted; then the result is stored back.
//
// CPython: Python/codegen.c:L5346 codegen_augassign
func (c *Compiler) visitAugAssign(s *ast.AugAssign) error {
	targetLoc := loc(s.Target)
	switch t := s.Target.(type) {
	case *ast.Name:
		if err := c.nameOpLoad(t.Id, targetLoc); err != nil {
			return err
		}
		if err := c.visitExpr(s.Value); err != nil {
			return err
		}
		op, err := inplaceOpKind(s.Op)
		if err != nil {
			return err
		}
		c.addOpI(BINARY_OP, op, loc(s))
		return c.nameOpStore(t.Id, targetLoc)
	case *ast.Attribute:
		if err := c.visitExpr(t.Value); err != nil {
			return err
		}
		c.addOpI(COPY, 1, targetLoc)
		pool := poolNames
		c.addOpName(LOAD_ATTR, &pool, t.Attr, targetLoc)
		if err := c.visitExpr(s.Value); err != nil {
			return err
		}
		op, err := inplaceOpKind(s.Op)
		if err != nil {
			return err
		}
		c.addOpI(BINARY_OP, op, loc(s))
		c.addOpI(SWAP, 2, targetLoc)
		c.addOpName(STORE_ATTR, &pool, t.Attr, targetLoc)
		return nil
	case *ast.Subscript:
		if err := c.visitExpr(t.Value); err != nil {
			return err
		}
		if err := c.visitExpr(t.Slice); err != nil {
			return err
		}
		c.addOpI(COPY, 2, targetLoc)
		c.addOpI(COPY, 2, targetLoc)
		c.addOpI(BINARY_OP, nbSubscr, targetLoc)
		if err := c.visitExpr(s.Value); err != nil {
			return err
		}
		op, err := inplaceOpKind(s.Op)
		if err != nil {
			return err
		}
		c.addOpI(BINARY_OP, op, loc(s))
		c.addOpI(SWAP, 3, targetLoc)
		c.addOpI(SWAP, 2, targetLoc)
		c.addOp(STORE_SUBSCR, targetLoc)
		return nil
	}
	return fmt.Errorf("compile: AugAssign target %T not supported", s.Target)
}

// inplaceOpKind maps the asdl operator to BINARY_OP's NB_INPLACE_* subcode.
//
// CPython: Include/opcode.h NB_INPLACE_*
func inplaceOpKind(op ast.Operator) (int32, error) {
	switch op {
	case ast.Add:
		return 13, nil
	case ast.BitAnd:
		return 14, nil
	case ast.FloorDiv:
		return 15, nil
	case ast.LShift:
		return 16, nil
	case ast.MatMult:
		return 17, nil
	case ast.Mult:
		return 18, nil
	case ast.ModOperator:
		return 19, nil
	case ast.BitOr:
		return 20, nil
	case ast.Pow:
		return 21, nil
	case ast.RShift:
		return 22, nil
	case ast.Sub:
		return 23, nil
	case ast.Div:
		return 24, nil
	case ast.BitXor:
		return 25, nil
	}
	return 0, fmt.Errorf("compile: unknown inplace operator %v", op)
}

// visitAnnAssign emits the annotation half of `x: T = v`. The value
// assignment, when present, follows the normal Assign target path.
// At module and class scope the annotation expression is evaluated
// eagerly and stored into __annotations__[name] so downstream code
// (e.g. dataclasses) can introspect the class. PEP 649's deferred /
// lazy form is pending; the eager store is the pre-3.14 baseline that
// keeps the surface working.
//
// CPython: Python/codegen.c:L5476 codegen_annassign
func (c *Compiler) visitAnnAssign(s *ast.AnnAssign) error {
	if s.Value != nil {
		if err := c.visitExpr(s.Value); err != nil {
			return err
		}
		if err := c.assignTo(s.Target, loc(s)); err != nil {
			return err
		}
	}
	if c.scope.Type == symtable.FunctionBlock {
		return nil
	}
	// Non-simple targets (parenthesized names like `(x): int`, attribute
	// targets `a.b: int`, subscript targets `a[i]: int`) do not update
	// __annotations__. Only simple bare names do.
	//
	// CPython: Python/codegen.c:5513 if (simple) { codegen_argannotation }
	if s.Simple == 0 {
		return nil
	}
	name, ok := s.Target.(*ast.Name)
	if !ok {
		// Subscript / attribute targets only get their value stored;
		// CPython skips the annotation-dict update for them too.
		return nil
	}
	// PEP 649 record-only at class and module scope. The body's emit
	// path (codegen_class.go for class, codegen_stmt.go for module)
	// drains DeferredAnnotations after visitStmts and synthesizes the
	// `__annotate__` function. typeGetAttr / moduleGetAnnotations call
	// it lazily on first `__annotations__` access.
	//
	// CPython: Python/codegen.c:5476 codegen_annassign
	c.unit().DeferredAnnotations = append(
		c.unit().DeferredAnnotations,
		deferredAnnotation{Name: name.Id, Value: s.Annotation, Loc: loc(s)},
	)
	return nil
}

// visitRaise emits RAISE_VARARGS with 0, 1, or 2 args.
//
//	raise            -> RAISE_VARARGS 0
//	raise exc        -> visit exc; RAISE_VARARGS 1
//	raise exc from c -> visit exc; visit c; RAISE_VARARGS 2
//
// CPython: Python/codegen.c codegen_raise
func (c *Compiler) visitRaise(s *ast.Raise) error {
	n := int32(0)
	if s.Exc != nil {
		if err := c.visitExpr(s.Exc); err != nil {
			return err
		}
		n = 1
		if s.Cause != nil {
			if err := c.visitExpr(s.Cause); err != nil {
				return err
			}
			n = 2
		}
	}
	c.addOpI(RAISE_VARARGS, n, loc(s))
	return nil
}

// visitAssert emits the assert sequence:
//
//	if not test: raise AssertionError(msg?)
//
// Skipped entirely when the compiler is running with -O.
//
// CPython: Python/codegen.c:L2932 codegen_assert
func (c *Compiler) visitAssert(s *ast.Assert) error {
	// Warn on non-empty tuple test: assert(x, msg) is always true.
	// CPython: Python/codegen.c:2932 codegen_assert tuple-check
	if err := c.assertTupleWarning(s); err != nil {
		return err
	}
	if c.Optimize > 0 {
		return nil
	}
	end := c.newLabel()
	if err := c.codegenJumpIf(s.Test, end, true); err != nil {
		return err
	}
	c.addOpI(LOAD_COMMON_CONSTANT, constantAssertionError, loc(s))
	if s.Msg != nil {
		if err := c.visitExpr(s.Msg); err != nil {
			return err
		}
		c.addOpI(CALL, 0, loc(s))
	}
	c.addOpI(RAISE_VARARGS, 1, loc(s.Test))
	c.useLabel(end)
	return nil
}

// constantAssertionError is the index passed to LOAD_COMMON_CONSTANT
// for the AssertionError class. CPython:
// Include/internal/pycore_opcode_utils.h CONSTANT_ASSERTIONERROR.
const constantAssertionError = 0

// constantNotImplementedError is the index passed to LOAD_COMMON_CONSTANT
// for the NotImplementedError class. CPython:
// Include/internal/pycore_opcode_utils.h CONSTANT_NOTIMPLEMENTEDERROR.
const constantNotImplementedError = 1

// visitImport handles `import a.b.c` and `import a.b.c as x`.
//
// CPython: Python/codegen.c:L2835 codegen_import
func (c *Compiler) visitImport(s *ast.Import) error {
	for _, alias := range s.Names {
		c.addLoadConst(int64(0), loc(s)) // level=0
		c.addLoadConst(nil, loc(s))      // fromlist=None
		pool := poolNames
		c.addOpName(IMPORT_NAME, &pool, alias.Name, loc(s))
		if alias.Asname != nil {
			if err := c.importAs(alias.Name, *alias.Asname, loc(s)); err != nil {
				return err
			}
			continue
		}
		// `import a.b.c` binds the top package name `a`.
		topName := alias.Name
		for i, ch := range topName {
			if ch == '.' {
				topName = topName[:i]
				break
			}
		}
		if err := c.nameOpStore(topName, loc(s)); err != nil {
			return err
		}
	}
	return nil
}

// importAs emits the IMPORT_FROM chain for `import a.b.c as x`,
// walking the dotted name and binding the leaf alias.
//
// CPython: Python/codegen.c:L2793 codegen_import_as
func (c *Compiler) importAs(dotted, asname string, l ast.Pos) error {
	parts := splitDotted(dotted)
	if len(parts) <= 1 {
		return c.nameOpStore(asname, l)
	}
	for _, attr := range parts[1:] {
		pool := poolNames
		c.addOpName(IMPORT_FROM, &pool, attr, l)
		c.addOpI(SWAP, 2, l)
		c.addOp(POP_TOP, l)
	}
	return c.nameOpStore(asname, l)
}

// visitImportFrom emits `from m import a, b, c` or `from m import *`.
//
// CPython: Python/codegen.c:L2881 codegen_from_import
func (c *Compiler) visitImportFrom(s *ast.ImportFrom) error {
	level := 0
	if s.Level != nil {
		level = *s.Level
	}
	c.addLoadConst(int64(level), loc(s))
	// fromlist is a tuple of strings.
	fromlist := make([]any, 0, len(s.Names))
	for _, a := range s.Names {
		fromlist = append(fromlist, a.Name)
	}
	c.addLoadConst(tupleOf(fromlist), loc(s))
	pool := poolNames
	module := ""
	if s.Module != nil {
		module = *s.Module
	}
	c.addOpName(IMPORT_NAME, &pool, module, loc(s))

	if len(s.Names) == 1 && s.Names[0].Name == "*" {
		c.addOpI(CALL_INTRINSIC_1, intrinsicImportStar, loc(s))
		c.addOp(POP_TOP, loc(s))
		return nil
	}
	for _, alias := range s.Names {
		c.addOpName(IMPORT_FROM, &pool, alias.Name, loc(s))
		bind := alias.Name
		if alias.Asname != nil {
			bind = *alias.Asname
		}
		if err := c.nameOpStore(bind, loc(s)); err != nil {
			return err
		}
	}
	c.addOp(POP_TOP, loc(s))
	return nil
}

const intrinsicImportStar int32 = 2

// splitDotted splits a dotted name `a.b.c` into ["a","b","c"].
//
// CPython: Python/codegen.c forbidden_name dotted-name split (analog)
func splitDotted(name string) []string {
	var out []string
	start := 0
	for i, ch := range name {
		if ch == '.' {
			out = append(out, name[start:i])
			start = i + 1
		}
	}
	out = append(out, name[start:])
	return out
}
