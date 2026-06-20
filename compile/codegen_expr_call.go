// Port of cpython/Python/codegen.c Call / Starred / Keyword visitors
// (L4036+). Three lowering paths:
//
//	plain          ->  CALL nargs               (no kw, no star)
//	kwargs only    ->  CALL_KW nargs            (oparg includes kw count;
//	                                              tuple of names follows)
//	stars or **    ->  CALL_FUNCTION_EX flags   (collect args / kwargs)

package compile

import (
	"github.com/tamnd/gopy/ast"
	"github.com/tamnd/gopy/symtable"
)

// visitCall emits one of the three call shapes above.
//
// CPython: Python/codegen.c:L4036 codegen_call
func (c *Compiler) visitCall(e *ast.Call) error {
	if err := c.checkCaller(e.Func); err != nil {
		return err
	}
	if err := c.validateKeywords(e.Keywords); err != nil {
		return err
	}
	if hasStarArg(e.Args) || hasStarStar(e.Keywords) {
		return c.emitCallEx(e)
	}
	ok, err := c.maybeOptimizeMethodCall(e)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	// A call whose positional + keyword footprint exceeds the stack
	// guideline routes through the CALL_FUNCTION_EX path so the
	// arguments accumulate into a tuple / dict instead of piling onto
	// the evaluation stack.
	//
	// CPython: Python/codegen.c:4258 codegen_call_helper_impl
	// (nelts + nkwelts*2 > _PY_STACK_USE_GUIDELINE -> ex_call)
	if len(e.Args)+len(e.Keywords)*2 > stackUseGuideline {
		return c.emitCallEx(e)
	}
	if hasKeyword(e.Keywords) {
		return c.emitCallKw(e)
	}
	return c.emitCallPlain(e)
}

// maybeOptimizeMethodCall emits the LOAD_ATTR-with-NULL-bit + CALL form
// for `obj.method(args)` so the runtime can take the bound-method
// fast path without materializing a method object. The receiver is
// pushed in the slot a PUSH_NULL would normally occupy, and LOAD_ATTR
// is encoded with `oparg = (name_index << 1) | 1` so the interpreter
// pushes self in place of the NULL.
//
// Skips the optimization when:
//   - the callable is not Attribute(Load)
//   - the call has *args / **kwargs (caller already routed to emitCallEx)
//   - any keyword has no name (i.e. **expr; same as above)
//   - argsl + kwdsl + (kwdsl != 0) hits _PY_STACK_USE_GUIDELINE
//
// CPython: Python/codegen.c:3944 maybe_optimize_method_call
func (c *Compiler) maybeOptimizeMethodCall(e *ast.Call) (bool, error) {
	attr, ok := e.Func.(*ast.Attribute)
	if !ok || attr.Ctx != ast.Load {
		return false, nil
	}
	for _, a := range e.Args {
		if _, star := a.(*ast.Starred); star {
			return false, nil
		}
	}
	kwExtra := 0
	for _, kw := range e.Keywords {
		if kw.Arg == nil {
			return false, nil
		}
	}
	if len(e.Keywords) > 0 {
		kwExtra = 1
	}
	if len(e.Args)+len(e.Keywords)+kwExtra >= stackUseGuideline {
		return false, nil
	}
	if err := c.visitExpr(attr.Value); err != nil {
		return false, err
	}
	pool := poolNames
	mangled := symtable.Mangle(c.unit().Private, attr.Attr)
	nameIdx := c.poolIndex(&pool, mangled)
	c.addOpI(LOAD_ATTR, int32((nameIdx<<1)|1), loc(attr))
	for _, a := range e.Args {
		if err := c.visitExpr(a); err != nil {
			return false, err
		}
	}
	if len(e.Keywords) == 0 {
		c.addOpI(CALL, int32(len(e.Args)), loc(e))
		return true, nil
	}
	names := make([]any, 0, len(e.Keywords))
	for _, kw := range e.Keywords {
		if err := c.visitExpr(kw.Value); err != nil {
			return false, err
		}
		names = append(names, *kw.Arg)
	}
	c.addLoadConst(tupleOf(names), loc(e))
	c.addOpI(CALL_KW, int32(len(e.Args)+len(e.Keywords)), loc(e))
	return true, nil
}

// emitCallPlain: LOAD callable, push self placeholder (NULL),
// LOAD args, CALL n. The stack layout CALL expects (bottom to top)
// is [callable, NULL/self, arg0, ..., argN].
//
// CPython: codegen_call simple branch
func (c *Compiler) emitCallPlain(e *ast.Call) error {
	if err := c.visitExpr(e.Func); err != nil {
		return err
	}
	c.addOp(PUSH_NULL, loc(e.Func))
	for _, a := range e.Args {
		if err := c.visitExpr(a); err != nil {
			return err
		}
	}
	c.addOpI(CALL, int32(len(e.Args)), loc(e))
	return nil
}

// emitCallKw: callable; positional args; kwarg values; LOAD_CONST
// (tuple of kw names); CALL_KW total.
//
// CPython: codegen_call kw branch
func (c *Compiler) emitCallKw(e *ast.Call) error {
	if err := c.visitExpr(e.Func); err != nil {
		return err
	}
	c.addOp(PUSH_NULL, loc(e.Func))
	for _, a := range e.Args {
		if err := c.visitExpr(a); err != nil {
			return err
		}
	}
	names := make([]any, 0, len(e.Keywords))
	for _, kw := range e.Keywords {
		if err := c.visitExpr(kw.Value); err != nil {
			return err
		}
		if kw.Arg != nil {
			names = append(names, *kw.Arg)
		}
	}
	c.addLoadConst(tupleOf(names), loc(e))
	c.addOpI(CALL_KW, int32(len(e.Args)+len(e.Keywords)), loc(e))
	return nil
}

// emitCallEx: build args sequence (BUILD_LIST 0 + LIST_APPEND /
// LIST_EXTEND for each arg or *star), convert to tuple. Then build a
// kwargs dict (BUILD_MAP 0 + DICT_MERGE / **rest). CALL_FUNCTION_EX
// oparg: 1 if kwargs map present, else 0.
//
// CPython: codegen_call_ex
func (c *Compiler) emitCallEx(e *ast.Call) error {
	if err := c.visitExpr(e.Func); err != nil {
		return err
	}
	c.addOp(PUSH_NULL, loc(e.Func))
	// A lone `*x` with no other positional args is passed straight to
	// CALL_FUNCTION_EX, so the opcode's check_args_iterable reports
	// "<func>() argument after * must be an iterable". Anything else is
	// gathered into a list and coerced to a tuple, where a non-iterable
	// star trips LIST_EXTEND's "Value after *" message instead.
	//
	// CPython: Python/codegen.c codegen_call_helper_impl (ex_call single-star)
	if len(e.Args) == 1 {
		if star, ok := e.Args[0].(*ast.Starred); ok {
			if err := c.visitExpr(star.Value); err != nil {
				return err
			}
			return c.emitCallExKwargs(e)
		}
	}
	// Gather the positional args into a tuple, going through the shared
	// starunpack helper so a large or starred list builds with a bounded
	// evaluation stack (BUILD_LIST 0 + LIST_APPEND / LIST_EXTEND, then
	// INTRINSIC_LIST_TO_TUPLE).
	//
	// CPython: Python/codegen.c codegen_call_helper_impl (ex_call ->
	// starunpack_helper_impl with build=BUILD_LIST, tuple=1)
	if err := c.emitStarunpack(e.Args, BUILD_LIST, LIST_APPEND, LIST_EXTEND, true, loc(e)); err != nil {
		return err
	}
	return c.emitCallExKwargs(e)
}

// emitCallExKwargs emits the keyword-unpacking tail shared by the two
// positional paths in emitCallEx: it builds the kwargs dict when the
// call carries keyword or **mapping arguments, then emits
// CALL_FUNCTION_EX with the kwargs-present flag.
//
// CPython: Python/codegen.c codegen_call_helper_impl (ex_call keyword tail)
func (c *Compiler) emitCallExKwargs(e *ast.Call) error {
	flag := int32(0)
	if hasKeyword(e.Keywords) {
		flag = 1
		kws := e.Keywords
		haveDict := false
		nseen := 0
		for i, kw := range kws {
			if kw.Arg == nil {
				// A `**mapping` splat: pack up any pending run of plain
				// keywords, then merge the mapping in.
				if nseen != 0 {
					if err := c.codegenSubkwargs(kws, i-nseen, i, loc(e)); err != nil {
						return err
					}
					if haveDict {
						c.addOpI(DICT_MERGE, 1, loc(e))
					}
					haveDict = true
					nseen = 0
				}
				if !haveDict {
					c.addOpI(BUILD_MAP, 0, loc(e))
					haveDict = true
				}
				if err := c.visitExpr(kw.Value); err != nil {
					return err
				}
				c.addOpI(DICT_MERGE, 1, loc(e))
				continue
			}
			nseen++
		}
		if nseen != 0 {
			if err := c.codegenSubkwargs(kws, len(kws)-nseen, len(kws), loc(e)); err != nil {
				return err
			}
			if haveDict {
				c.addOpI(DICT_MERGE, 1, loc(e))
			}
			haveDict = true
		}
	}
	c.addOpI(CALL_FUNCTION_EX, flag, loc(e))
	return nil
}

// codegenSubkwargs emits the [begin,end) run of plain keyword arguments
// as one BUILD_MAP. A run whose stack footprint exceeds the guideline
// opens with BUILD_MAP 0 and folds each pair in with MAP_ADD so the
// value stack stays bounded.
//
// CPython: Python/codegen.c:4198 codegen_subkwargs
func (c *Compiler) codegenSubkwargs(kws ast.Seq[*ast.Keyword], begin, end int, l ast.Pos) error {
	n := end - begin
	big := n*2 > stackUseGuideline
	if big {
		c.addOpI(BUILD_MAP, 0, l)
	}
	for i := begin; i < end; i++ {
		c.addLoadConst(*kws[i].Arg, l)
		if err := c.visitExpr(kws[i].Value); err != nil {
			return err
		}
		if big {
			c.addOpI(MAP_ADD, 1, l)
		}
	}
	if !big {
		c.addOpI(BUILD_MAP, int32(n), l)
	}
	return nil
}

// validateKeywords rejects calls that pass the same keyword twice.
// The diagnostic points at the second occurrence so the column matches
// CPython's LOC(other) argument to _PyCompile_Error.
//
// CPython: Python/codegen.c:4018 codegen_validate_keywords
func (c *Compiler) validateKeywords(kws ast.Seq[*ast.Keyword]) error {
	for i, key := range kws {
		if key.Arg == nil {
			continue
		}
		for j := i + 1; j < len(kws); j++ {
			other := kws[j]
			if other.Arg != nil && *other.Arg == *key.Arg {
				return c.errorAt(other.Pos, "keyword argument repeated: %s", *key.Arg)
			}
		}
	}
	return nil
}

// hasStarArg reports whether any positional arg is `*expr`.
//
// CPython: Python/codegen.c has_star (call-args inspection helper)
func hasStarArg(args ast.Seq[ast.Expr]) bool {
	for _, a := range args {
		if _, ok := a.(*ast.Starred); ok {
			return true
		}
	}
	return false
}

// hasStarStar reports whether any keyword is `**expr`.
//
// CPython: Python/codegen.c has_kwargs (call-kwargs inspection helper)
func hasStarStar(kws ast.Seq[*ast.Keyword]) bool {
	for _, k := range kws {
		if k.Arg == nil {
			return true
		}
	}
	return false
}

// hasKeyword reports whether any explicit keyword (name=value or
// **expr) is present.
//
// CPython: Python/codegen.c keywords nonempty check in codegen_call
func hasKeyword(kws ast.Seq[*ast.Keyword]) bool {
	return len(kws) > 0
}

// tupleOf returns a *ConstTuple holding items. The pointer is what
// goes into the unit's Consts pool; the assembler unwraps it into a
// real tuple at marshal time. Pointer identity makes the value
// hashable for the per-unit dedup cache; the flowgraph's const-cache
// pass collapses identical tuples emitted at different call sites.
//
// CPython: Python/codegen.c PyTuple_New + PyTuple_SET_ITEM construction
// in codegen_call_helper_impl
func tupleOf(items []any) any {
	out := make([]any, len(items))
	copy(out, items)
	return &ConstTuple{Values: out}
}

// ConstTuple is the codegen-side placeholder for a Python tuple
// constant. The assembler converts it to a real PyTuple during
// marshal.
type ConstTuple struct {
	Values []any
}
