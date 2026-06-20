package ast

import (
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"strings"
)

// WrapValidationError tags a Validate failure with the Python exception
// type it must surface as. Most validation failures are ValueError; a
// handful (NamedExpr / AnnAssign / TypeAlias targets, Constant type) are
// TypeError and already carry that prefix in their message, so they pass
// straight through. Callers that turn a Validate error into a Python
// exception route it through here instead of hardcoding ValueError.
//
// CPython: Python/ast.c PyErr_SetString(PyExc_TypeError/ValueError, ...)
func WrapValidationError(err error) error {
	msg := err.Error()
	for _, p := range []string{"TypeError: ", "SystemError: "} {
		if strings.HasPrefix(msg, p) {
			return errors.New(msg)
		}
	}
	return errors.New("ValueError: " + msg)
}

// Validate is the gopy port of _PyAST_Validate. It walks mod and
// returns nil if the tree is well-formed, or an error matching
// CPython's ValueError/TypeError text.
//
// CPython: Python/ast.c:1047 _PyAST_Validate
func Validate(mod Mod) error {
	switch m := mod.(type) {
	case *Module:
		return validateStmts(m.Body)
	case *Interactive:
		return validateStmts(m.Body)
	case *Expression:
		return validateExpr(m.Body, Load)
	case *FunctionType:
		for i := 0; i < m.Argtypes.Len(); i++ {
			if err := validateExpr(m.Argtypes.Get(i), Load); err != nil {
				return err
			}
		}
		return validateExpr(m.Returns, Load)
	case nil:
		return errors.New("validate: nil module")
	default:
		return fmt.Errorf("validate: unknown module kind %T", mod)
	}
}

// validateStmts walks a statement sequence.
//
// CPython: Python/ast.c validate_stmts
func validateStmts(seq Seq[Stmt]) error {
	for i := 0; i < seq.Len(); i++ {
		s := seq.Get(i)
		if s == nil {
			return errors.New("None disallowed in statement list")
		}
		if err := validateStmt(s); err != nil {
			return err
		}
	}
	return nil
}

// validateBody checks non-empty and validates all statements.
//
// CPython: Python/ast.c validate_body
func validateBody(body Seq[Stmt], owner string) error {
	if body.Len() == 0 {
		return fmt.Errorf("empty body on %s", owner)
	}
	return validateStmts(body)
}

// validateStmt validates one statement.
//
// CPython: Python/ast.c:686 validate_stmt
//
//nolint:gocognit,gocyclo // mirrors CPython's per-kind switch.
func validateStmt(s Stmt) error {
	if s == nil {
		return errors.New("None disallowed in statement list")
	}
	if err := validatePos(s.Position()); err != nil {
		return err
	}
	switch n := s.(type) {
	case *FunctionDef:
		if err := validateBody(n.Body, "FunctionDef"); err != nil {
			return err
		}
		if err := validateTypeParams(n.TypeParams); err != nil {
			return err
		}
		if err := validateArguments(n.Args); err != nil {
			return err
		}
		if err := validateExprs(n.DecoratorList, Load, false); err != nil {
			return err
		}
		if n.Returns != nil {
			return validateExpr(n.Returns, Load)
		}
		return nil
	case *AsyncFunctionDef:
		if err := validateBody(n.Body, "AsyncFunctionDef"); err != nil {
			return err
		}
		if err := validateTypeParams(n.TypeParams); err != nil {
			return err
		}
		if err := validateArguments(n.Args); err != nil {
			return err
		}
		if err := validateExprs(n.DecoratorList, Load, false); err != nil {
			return err
		}
		if n.Returns != nil {
			return validateExpr(n.Returns, Load)
		}
		return nil
	case *ClassDef:
		if err := validateBody(n.Body, "ClassDef"); err != nil {
			return err
		}
		if err := validateTypeParams(n.TypeParams); err != nil {
			return err
		}
		if err := validateExprs(n.Bases, Load, false); err != nil {
			return err
		}
		if err := validateKeywords(n.Keywords); err != nil {
			return err
		}
		return validateExprs(n.DecoratorList, Load, false)
	case *Return:
		if n.Value != nil {
			return validateExpr(n.Value, Load)
		}
		return nil
	case *Delete:
		return validateAssignList(n.Targets, Del)
	case *Assign:
		if err := validateAssignList(n.Targets, Store); err != nil {
			return err
		}
		return validateExpr(n.Value, Load)
	case *AugAssign:
		if err := validateExpr(n.Target, Store); err != nil {
			return err
		}
		return validateExpr(n.Value, Load)
	case *AnnAssign:
		if n.Target != nil {
			if _, ok := n.Target.(*Name); !ok && n.Simple != 0 {
				return errors.New("TypeError: AnnAssign with simple non-Name target")
			}
		}
		if err := validateExpr(n.Target, Store); err != nil {
			return err
		}
		if n.Value != nil {
			if err := validateExpr(n.Value, Load); err != nil {
				return err
			}
		}
		return validateExpr(n.Annotation, Load)
	case *TypeAlias:
		if n.Name != nil {
			if _, ok := n.Name.(*Name); !ok {
				return errors.New("TypeError: TypeAlias with non-Name name")
			}
		}
		if err := validateExpr(n.Name, Store); err != nil {
			return err
		}
		if err := validateTypeParams(n.TypeParams); err != nil {
			return err
		}
		return validateExpr(n.Value, Load)
	case *For:
		if err := validateExpr(n.Target, Store); err != nil {
			return err
		}
		if err := validateExpr(n.Iter, Load); err != nil {
			return err
		}
		if err := validateBody(n.Body, "For"); err != nil {
			return err
		}
		return validateStmts(n.Orelse)
	case *AsyncFor:
		if err := validateExpr(n.Target, Store); err != nil {
			return err
		}
		if err := validateExpr(n.Iter, Load); err != nil {
			return err
		}
		if err := validateBody(n.Body, "AsyncFor"); err != nil {
			return err
		}
		return validateStmts(n.Orelse)
	case *While:
		if err := validateExpr(n.Test, Load); err != nil {
			return err
		}
		if err := validateBody(n.Body, "While"); err != nil {
			return err
		}
		return validateStmts(n.Orelse)
	case *If:
		if err := validateExpr(n.Test, Load); err != nil {
			return err
		}
		if err := validateBody(n.Body, "If"); err != nil {
			return err
		}
		return validateStmts(n.Orelse)
	case *With:
		if n.Items.Len() == 0 {
			return errors.New("empty items on With")
		}
		for i := 0; i < n.Items.Len(); i++ {
			item := n.Items.Get(i)
			if item == nil {
				return errors.New("None disallowed in with items")
			}
			if item.ContextExpr == nil {
				return errors.New("field 'context_expr' is required for withitem")
			}
			if err := validateExpr(item.ContextExpr, Load); err != nil {
				return err
			}
			if item.OptionalVars != nil {
				if err := validateExpr(item.OptionalVars, Store); err != nil {
					return err
				}
			}
		}
		return validateBody(n.Body, "With")
	case *AsyncWith:
		if n.Items.Len() == 0 {
			return errors.New("empty items on AsyncWith")
		}
		for i := 0; i < n.Items.Len(); i++ {
			item := n.Items.Get(i)
			if item == nil {
				return errors.New("None disallowed in with items")
			}
			if item.ContextExpr == nil {
				return errors.New("field 'context_expr' is required for withitem")
			}
			if err := validateExpr(item.ContextExpr, Load); err != nil {
				return err
			}
			if item.OptionalVars != nil {
				if err := validateExpr(item.OptionalVars, Store); err != nil {
					return err
				}
			}
		}
		return validateBody(n.Body, "AsyncWith")
	case *Match:
		if err := validateExpr(n.Subject, Load); err != nil {
			return err
		}
		if n.Cases.Len() == 0 {
			return errors.New("empty cases on Match")
		}
		for i := 0; i < n.Cases.Len(); i++ {
			cs := n.Cases.Get(i)
			if cs == nil {
				return errors.New("None disallowed in match cases")
			}
			if cs.Pattern == nil {
				return errors.New("field 'pattern' is required for match_case")
			}
			if err := validatePattern(cs.Pattern, false); err != nil {
				return err
			}
			if cs.Guard != nil {
				if err := validateExpr(cs.Guard, Load); err != nil {
					return err
				}
			}
			if err := validateBody(cs.Body, "match_case"); err != nil {
				return err
			}
		}
		return nil
	case *Raise:
		if n.Exc != nil {
			if err := validateExpr(n.Exc, Load); err != nil {
				return err
			}
			if n.Cause != nil {
				return validateExpr(n.Cause, Load)
			}
			return nil
		}
		if n.Cause != nil {
			return errors.New("Raise with cause but no exception")
		}
		return nil
	case *Try:
		if err := validateBody(n.Body, "Try"); err != nil {
			return err
		}
		if n.Handlers.Len() == 0 && n.Finalbody.Len() == 0 {
			return errors.New("Try has neither except handlers nor finalbody")
		}
		if n.Handlers.Len() == 0 && n.Orelse.Len() > 0 {
			return errors.New("Try has orelse but no except handlers")
		}
		for i := 0; i < n.Handlers.Len(); i++ {
			h := n.Handlers.Get(i)
			if h == nil {
				return errors.New("None disallowed in handler list")
			}
			eh, ok := h.(*ExceptHandler)
			if !ok {
				return fmt.Errorf("validate: unknown handler kind %T", h)
			}
			if err := validatePos(eh.Pos); err != nil {
				return err
			}
			if eh.Type != nil {
				if err := validateExpr(eh.Type, Load); err != nil {
					return err
				}
			}
			if err := validateBody(eh.Body, "ExceptHandler"); err != nil {
				return err
			}
		}
		if n.Finalbody.Len() > 0 {
			if err := validateStmts(n.Finalbody); err != nil {
				return err
			}
		}
		return validateStmts(n.Orelse)
	case *TryStar:
		if err := validateBody(n.Body, "TryStar"); err != nil {
			return err
		}
		if n.Handlers.Len() == 0 && n.Finalbody.Len() == 0 {
			return errors.New("TryStar has neither except handlers nor finalbody")
		}
		if n.Handlers.Len() == 0 && n.Orelse.Len() > 0 {
			return errors.New("TryStar has orelse but no except handlers")
		}
		for i := 0; i < n.Handlers.Len(); i++ {
			h := n.Handlers.Get(i)
			if h == nil {
				return errors.New("None disallowed in handler list")
			}
			eh, ok := h.(*ExceptHandler)
			if !ok {
				return fmt.Errorf("validate: unknown handler kind %T", h)
			}
			if eh.Type != nil {
				if err := validateExpr(eh.Type, Load); err != nil {
					return err
				}
			}
			if err := validateBody(eh.Body, "ExceptHandler"); err != nil {
				return err
			}
		}
		if n.Finalbody.Len() > 0 {
			if err := validateStmts(n.Finalbody); err != nil {
				return err
			}
		}
		return validateStmts(n.Orelse)
	case *Assert:
		if err := validateExpr(n.Test, Load); err != nil {
			return err
		}
		if n.Msg != nil {
			return validateExpr(n.Msg, Load)
		}
		return nil
	case *Import:
		if n.Names.Len() == 0 {
			return errors.New("empty names on Import")
		}
		return validateImportAliases(n.Names)
	case *ImportFrom:
		return validateImportFrom(n)
	case *Global:
		if n.Names.Len() == 0 {
			return errors.New("empty names on Global")
		}
		return nil
	case *Nonlocal:
		if n.Names.Len() == 0 {
			return errors.New("empty names on Nonlocal")
		}
		return nil
	case *ExprStmt:
		return validateExpr(n.Value, Load)
	case *Pass, *Break, *Continue:
		return nil
	}
	return nil
}

// validateExpr validates one expression with its required context.
//
// CPython: Python/ast.c:379 validate_expr
//
//nolint:gocognit,gocyclo // mirrors CPython's per-kind switch.
func validateExpr(e Expr, ctx ExprContext) error {
	if e == nil {
		return errors.New("None disallowed in expression list")
	}
	if err := validatePos(e.Position()); err != nil {
		return err
	}

	// Context check: expressions that carry a ctx field must match.
	var actualCtx ExprContext
	checkCtx := true
	switch n := e.(type) {
	case *Attribute:
		actualCtx = n.Ctx
	case *Subscript:
		actualCtx = n.Ctx
	case *Starred:
		actualCtx = n.Ctx
	case *Name:
		if err := validateName(n.Id); err != nil {
			return err
		}
		actualCtx = n.Ctx
	case *List:
		actualCtx = n.Ctx
	case *Tuple:
		actualCtx = n.Ctx
	default:
		if ctx != Load {
			return fmt.Errorf("expression which can't be assigned to in %s context", ctx.String())
		}
		checkCtx = false
	}
	if checkCtx && actualCtx != ctx {
		return fmt.Errorf("expression must have %s context but has %s instead",
			ctx.String(), actualCtx.String())
	}

	// Content validation.
	switch n := e.(type) {
	case *BoolOp:
		if n.Values.Len() < 2 {
			return errors.New("BoolOp with less than 2 values")
		}
		return validateExprs(n.Values, Load, false)
	case *NamedExpr:
		if _, ok := n.Target.(*Name); !ok {
			return errors.New("TypeError: NamedExpr target must be a Name")
		}
		return validateExpr(n.Value, Load)
	case *BinOp:
		if err := validateExpr(n.Left, Load); err != nil {
			return err
		}
		return validateExpr(n.Right, Load)
	case *UnaryOp:
		return validateExpr(n.Operand, Load)
	case *Lambda:
		if err := validateArguments(n.Args); err != nil {
			return err
		}
		return validateExpr(n.Body, Load)
	case *IfExp:
		if err := validateExpr(n.Test, Load); err != nil {
			return err
		}
		if err := validateExpr(n.Body, Load); err != nil {
			return err
		}
		return validateExpr(n.Orelse, Load)
	case *Dict:
		if n.Keys.Len() != n.Values.Len() {
			return errors.New("Dict doesn't have the same number of keys as values")
		}
		// null_ok=true for keys (dict unpacking **{} uses None key)
		if err := validateExprs(n.Keys, Load, true); err != nil {
			return err
		}
		return validateExprs(n.Values, Load, false)
	case *Set:
		return validateExprs(n.Elts, Load, false)
	case *ListComp:
		if err := validateComprehension(n.Generators); err != nil {
			return err
		}
		return validateExpr(n.Elt, Load)
	case *SetComp:
		if err := validateComprehension(n.Generators); err != nil {
			return err
		}
		return validateExpr(n.Elt, Load)
	case *GeneratorExp:
		if err := validateComprehension(n.Generators); err != nil {
			return err
		}
		return validateExpr(n.Elt, Load)
	case *DictComp:
		if err := validateComprehension(n.Generators); err != nil {
			return err
		}
		if err := validateExpr(n.Key, Load); err != nil {
			return err
		}
		return validateExpr(n.Value, Load)
	case *Yield:
		if n.Value != nil {
			return validateExpr(n.Value, Load)
		}
		return nil
	case *YieldFrom:
		if n.Value == nil {
			return errors.New("field 'value' is required for YieldFrom")
		}
		return validateExpr(n.Value, Load)
	case *Await:
		if n.Value == nil {
			return errors.New("field 'value' is required for Await")
		}
		return validateExpr(n.Value, Load)
	case *Compare:
		if n.Comparators.Len() == 0 {
			return errors.New("Compare with no comparators")
		}
		if n.Comparators.Len() != n.Ops.Len() {
			return errors.New("Compare has a different number of comparators and operands")
		}
		if err := validateExprs(n.Comparators, Load, false); err != nil {
			return err
		}
		return validateExpr(n.Left, Load)
	case *Call:
		if err := validateExpr(n.Func, Load); err != nil {
			return err
		}
		if err := validateExprs(n.Args, Load, false); err != nil {
			return err
		}
		return validateKeywords(n.Keywords)
	case *Constant:
		return validateConstant(n.Value)
	case *JoinedStr:
		return validateExprs(n.Values, Load, false)
	case *TemplateStr:
		return validateExprs(n.Values, Load, false)
	case *FormattedValue:
		if err := validateExpr(n.Value, Load); err != nil {
			return err
		}
		if n.FormatSpec != nil {
			return validateExpr(n.FormatSpec, Load)
		}
		return nil
	case *Interpolation:
		if err := validateExpr(n.Value, Load); err != nil {
			return err
		}
		if n.FormatSpec != nil {
			return validateExpr(n.FormatSpec, Load)
		}
		return nil
	case *Attribute:
		return validateExpr(n.Value, Load)
	case *Subscript:
		if err := validateExpr(n.Slice, Load); err != nil {
			return err
		}
		return validateExpr(n.Value, Load)
	case *Starred:
		return validateExpr(n.Value, ctx)
	case *Slice:
		if n.Lower != nil {
			if err := validateExpr(n.Lower, Load); err != nil {
				return err
			}
		}
		if n.Upper != nil {
			if err := validateExpr(n.Upper, Load); err != nil {
				return err
			}
		}
		if n.Step != nil {
			if err := validateExpr(n.Step, Load); err != nil {
				return err
			}
		}
		return nil
	case *List:
		return validateExprs(n.Elts, ctx, false)
	case *Tuple:
		return validateExprs(n.Elts, ctx, false)
	case *Name:
		return nil
	}
	return nil
}

// validateExprs validates a sequence of expressions with the given context.
//
// CPython: Python/ast.c validate_exprs
func validateExprs(exprs Seq[Expr], ctx ExprContext, nullOk bool) error {
	for i := 0; i < exprs.Len(); i++ {
		e := exprs.Get(i)
		if e == nil {
			if !nullOk {
				return errors.New("None disallowed in expression list")
			}
			continue
		}
		if err := validateExpr(e, ctx); err != nil {
			return err
		}
	}
	return nil
}

// validateAssignList checks non-empty targets with the given context.
//
// CPython: Python/ast.c validate_assignlist
func validateAssignList(targets Seq[Expr], ctx ExprContext) error {
	label := "Assign"
	if ctx == Del {
		label = "Delete"
	}
	if targets.Len() == 0 {
		return fmt.Errorf("empty targets on %s", label)
	}
	return validateExprs(targets, ctx, false)
}

// validateArguments validates an arguments node.
//
// CPython: Python/ast.c:128 validate_arguments
func validateArguments(args *Arguments) error {
	if args == nil {
		return nil
	}
	if err := validateArgs(args.Posonlyargs); err != nil {
		return err
	}
	if err := validateArgs(args.Args); err != nil {
		return err
	}
	if args.Vararg != nil && args.Vararg.Annotation != nil {
		if err := validateExpr(args.Vararg.Annotation, Load); err != nil {
			return err
		}
	}
	if err := validateArgs(args.Kwonlyargs); err != nil {
		return err
	}
	if args.Kwarg != nil && args.Kwarg.Annotation != nil {
		if err := validateExpr(args.Kwarg.Annotation, Load); err != nil {
			return err
		}
	}
	npos := args.Posonlyargs.Len() + args.Args.Len()
	if args.Defaults.Len() > npos {
		return errors.New("more positional defaults than args on arguments")
	}
	if args.KwDefaults.Len() != args.Kwonlyargs.Len() {
		return errors.New("length of kwonlyargs is not the same as kw_defaults on arguments")
	}
	if err := validateExprs(args.Defaults, Load, false); err != nil {
		return err
	}
	return validateExprs(args.KwDefaults, Load, true)
}

// validateArgs validates a list of Arg nodes (checking annotations).
//
// CPython: Python/ast.c validate_args
func validateArgs(args Seq[*Arg]) error {
	for i := 0; i < args.Len(); i++ {
		arg := args.Get(i)
		if arg == nil {
			return errors.New("None disallowed in arg list")
		}
		if arg.Arg == "" {
			return errors.New("field 'arg' is required for arg")
		}
		if err := validatePos(arg.Pos); err != nil {
			return err
		}
		if arg.Annotation != nil {
			if err := validateExpr(arg.Annotation, Load); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateKeywords validates keyword argument values.
//
// CPython: Python/ast.c:90 validate_keywords
func validateKeywords(kws Seq[*Keyword]) error {
	for i := 0; i < kws.Len(); i++ {
		kw := kws.Get(i)
		if kw == nil {
			return errors.New("None disallowed in keyword list")
		}
		if kw.Value == nil {
			return errors.New("field 'value' is required for keyword")
		}
		if err := validateExpr(kw.Value, Load); err != nil {
			return err
		}
	}
	return nil
}

// validatePos mirrors CPython's VALIDATE_POSITIONS macro.
// -1 is the "not set" sentinel; negative fields are only invalid when
// they mismatch each other (e.g. lineno=-1 but end_lineno=5).
//
// CPython: Python/ast.c:30 VALIDATE_POSITIONS
func validatePos(p Pos) error {
	if p == NoPos {
		return nil
	}
	// CPython: Python/ast.c:31 rule 1 - lineno > end_lineno always invalid.
	// Guard end_lineno != -1: gopy's internal parser may leave end_lineno as the
	// -1 "unset" sentinel; CPython's parser always fills it in explicitly.
	if p.EndLineno != -1 && p.Lineno > p.EndLineno {
		return fmt.Errorf("AST node line range (%d, %d) is not valid", p.Lineno, p.EndLineno)
	}
	// CPython: Python/ast.c:37 rule 2 - mismatched negative sentinels.
	if (p.Lineno < 0 && p.EndLineno != p.Lineno) || (p.ColOffset < 0 && p.ColOffset != p.EndColOffset) {
		return fmt.Errorf("AST node column range (%d, %d) for line range (%d, %d) is not valid",
			p.ColOffset, p.EndColOffset, p.Lineno, p.EndLineno)
	}
	// CPython: Python/ast.c:44 rule 3 - same-line column inversion.
	// Guard end_col_offset != -1 for the same "unset" sentinel reason as rule 1.
	if p.EndColOffset != -1 && p.Lineno == p.EndLineno && p.ColOffset > p.EndColOffset {
		return fmt.Errorf("line %d, column %d-%d is not a valid range",
			p.Lineno, p.ColOffset, p.EndColOffset)
	}
	return nil
}

// validateImportFrom rejects a negative level.
//
// CPython: Python/ast.c:916 validate_stmt ImportFrom arm
func validateImportFrom(imp *ImportFrom) error {
	if imp.Level != nil && *imp.Level < 0 {
		return errors.New("Negative ImportFrom level")
	}
	if imp.Names.Len() == 0 {
		return errors.New("empty names on ImportFrom")
	}
	for i := 0; i < imp.Names.Len(); i++ {
		alias := imp.Names.Get(i)
		if alias == nil {
			continue
		}
		if alias.Name == "" {
			return errors.New("field 'name' is required for alias")
		}
		if err := validatePos(alias.Pos); err != nil {
			return err
		}
	}
	return nil
}

// validateImportAliases checks alias names for Import statements.
//
// CPython: Python/ast.c validate_stmt Import arm
func validateImportAliases(names Seq[*Alias]) error {
	for i := 0; i < names.Len(); i++ {
		a := names.Get(i)
		if a == nil {
			continue
		}
		if a.Name == "" {
			return errors.New("field 'name' is required for alias")
		}
	}
	return nil
}

// validateConstant mirrors validate_constant.
//
// CPython: Python/ast.c:156 validate_constant
func validateConstant(v any) error {
	if v == nil {
		return nil
	}
	if _, ok := v.(EllipsisType); ok {
		return nil
	}
	switch v.(type) {
	case bool,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64,
		complex64, complex128,
		string,
		[]byte:
		return nil
	case *big.Int:
		return nil
	}
	if items, ok := v.([]any); ok {
		for _, it := range items {
			if err := validateConstant(it); err != nil {
				return err
			}
		}
		return nil
	}
	if fs, ok := v.(FrozenSet); ok {
		for _, it := range fs {
			if err := validateConstant(it); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("TypeError: got an invalid type in Constant: %s", reflect.TypeOf(v))
}

// EllipsisType is the singleton type used to spell Python's `...` as
// a Constant value. CPython uses the dedicated Py_Ellipsis singleton.
type EllipsisType struct{}

// Ellipsis is the canonical sentinel for `...` as a Constant value.
var Ellipsis = EllipsisType{}

// FrozenSet wraps a slice of constant items for a frozenset literal.
// CPython tracks frozenset as a real type; v0.5 only needs a marker
// so Validate can recurse.
type FrozenSet []any
