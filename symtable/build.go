package symtable

import (
	"fmt"

	"github.com/tamnd/gopy/ast"
	"github.com/tamnd/gopy/future"
)

// Build performs the two-pass symbol table construction for mod and
// returns a populated Table. Pass 1 (this file) walks the AST and
// records DEF_*/USE bits per block; pass 2 (analyze.go) resolves each
// name to its final Scope.
//
// CPython: Python/symtable.c:L412 _PySymtable_Build
func Build(mod ast.Mod, filename string, ff *future.Features) (*Table, error) {
	if ff == nil {
		ff = &future.Features{}
	}
	t := &Table{
		Filename: filename,
		Blocks:   make(map[any]*Entry),
		Future:   ff,
	}
	b := &builder{table: t, filename: filename, future: ff, recursionRemaining: recursionLimit}
	// The top ModuleBlock carries loc {0,0,0,0}, not NoPos: symtable.get_lineno()
	// reports 0 for the module entry.
	// CPython: Python/symtable.c:438 _Py_SourceLocation loc0 = {0, 0, 0, 0}
	if err := b.enterBlock("top", ModuleBlock, mod, ast.Pos{}); err != nil {
		return nil, err
	}
	t.Top = b.cur
	switch m := mod.(type) {
	case *ast.Module:
		if hasModuleDocstring(m.Body) {
			b.cur.HasDocstring = true
		}
		for _, s := range m.Body {
			if err := b.visitStmt(s); err != nil {
				return nil, err
			}
		}
	case *ast.Interactive:
		for _, s := range m.Body {
			if err := b.visitStmt(s); err != nil {
				return nil, err
			}
		}
	case *ast.Expression:
		if err := b.visitExpr(m.Body); err != nil {
			return nil, err
		}
	case *ast.FunctionType:
		return nil, fmt.Errorf("symtable: this compiler does not handle FunctionTypes")
	default:
		return nil, fmt.Errorf("symtable: unknown mod kind %T", mod)
	}
	b.exitBlock()
	if err := analyze(t); err != nil {
		return nil, err
	}
	return t, nil
}

// hasModuleDocstring reports whether the body opens with a string
// literal expression. Mirrors _PyAST_GetDocString.
//
// CPython: Python/ast.c _PyAST_GetDocString
func hasModuleDocstring(body ast.Seq[ast.Stmt]) bool {
	if len(body) == 0 {
		return false
	}
	return ast.IsDocString(body[0])
}

// builder is the visit-pass state. It tracks the block stack and the
// current `private` name used for PEP 8 mangling.
// recursionLimit mirrors CPython Python/symtable.c C_RECURSION_LIMIT used
// via ENTER_RECURSIVE / Py_EnterRecursiveCall during symbol table building.
const recursionLimit = 500

type builder struct {
	table              *Table
	cur                *Entry
	stack              []*Entry
	private            string
	filename           string
	future             *future.Features
	nextID             int
	recursionRemaining int
}

// enterBlock allocates a new Entry, inherits Nested from the parent,
// and pushes it on the stack. Mirrors symtable_enter_block plus
// ste_new.
//
// CPython: Python/symtable.c:L92 ste_new + L1456 symtable_enter_block
func (b *builder) enterBlock(name string, kind Block, key any, loc ast.Pos) error {
	prev := b.cur
	b.nextID++
	ste := &Entry{
		ID:      b.nextID,
		Type:    kind,
		Name:    name,
		Symbols: make(map[string]SymbolFlags),
		Loc:     loc,
	}
	if prev != nil && (prev.Nested || prev.IsFunctionLike()) {
		ste.Nested = true
	}
	if prev != nil && prev.Type == ClassBlock && kind == FunctionBlock {
		ste.Method = true
	}
	b.table.Blocks[key] = ste
	return b.enterExisting(ste, true)
}

// enterExisting is the path for FunctionDef bodies: ste_new is called
// before symtable_visit_annotations so the annotation block can
// hang off the function's Entry; only afterwards does the function
// block get pushed via enter_existing_block.
//
// CPython: Python/symtable.c:L1416 symtable_enter_existing_block
func (b *builder) enterExisting(ste *Entry, addToChildren bool) error {
	prev := b.cur
	b.stack = append(b.stack, ste)
	if prev != nil {
		ste.CompIterExpr = prev.CompIterExpr
		if prev.MangledNames != nil && ste.Type != ClassBlock {
			ste.MangledNames = prev.MangledNames
		}
	}
	b.cur = ste
	if addToChildren && prev != nil {
		prev.Children = append(prev.Children, ste)
	}
	return nil
}

// exitBlock pops the current block off the stack.
//
// CPython: Python/symtable.c:L1400 symtable_exit_block
func (b *builder) exitBlock() {
	n := len(b.stack)
	if n == 0 {
		b.cur = nil
		return
	}
	b.stack = b.stack[:n-1]
	if len(b.stack) > 0 {
		b.cur = b.stack[len(b.stack)-1]
	} else {
		b.cur = nil
	}
}

// addDef records flag bits on `name` in the current block, with a
// store-versus-load sense inferred from flag.
//
// CPython: Python/symtable.c:L1652 symtable_add_def
func (b *builder) addDef(name string, flag SymbolFlags, loc ast.Pos) error {
	ctx := ast.Store
	if flag == Use {
		ctx = ast.Load
	}
	return b.addDefCtx(name, flag, loc, ctx)
}

// addDefCtx is the variant that takes an explicit expression context
// (used to allow del-vs-store distinctions on __debug__).
//
// CPython: Python/symtable.c:L1635 symtable_add_def_ctx
func (b *builder) addDefCtx(name string, flag SymbolFlags, loc ast.Pos, ctx ast.ExprContext) error {
	const writeMask = DefParam | DefLocal | DefImport
	if flag&writeMask != 0 {
		if err := b.checkName(name, loc, ctx); err != nil {
			return err
		}
	}
	if flag&DefTypeParam != 0 && b.cur.MangledNames != nil {
		b.cur.MangledNames[name] = struct{}{}
	}
	return b.addDefHelper(name, flag, b.cur, loc)
}

// addDefHelper writes the bits on a specific Entry. Splitting from
// addDef matches CPython where assignment expressions retarget a
// non-current entry.
//
// CPython: Python/symtable.c:L1498 symtable_add_def_helper
func (b *builder) addDefHelper(name string, flag SymbolFlags, ste *Entry, loc ast.Pos) error {
	mangled := MaybeMangle(b.private, b.cur, name)
	val, present := ste.Symbols[mangled]
	if present {
		if flag&DefParam != 0 && val&DefParam != 0 {
			return errorf(b.filename, loc, msgDupArgument, name)
		}
		if flag&DefTypeParam != 0 && val&DefTypeParam != 0 {
			return errorf(b.filename, loc, msgDupTypeParam, name)
		}
		val |= flag
	} else {
		val = flag
	}
	if ste.CompIterTarget {
		if val&(DefGlobal|DefNonlocal) != 0 {
			return errorf(b.filename, loc, msgNamedExprInner, name)
		}
		val |= DefCompIter
	}
	ste.SetSymbol(mangled, val)
	switch {
	case flag&DefParam != 0:
		ste.Varnames = append(ste.Varnames, mangled)
	case flag&DefGlobal != 0:
		gv := b.table.Top.Symbols[mangled]
		gv |= flag
		b.table.Top.SetSymbol(mangled, gv)
	}
	return nil
}

// checkName guards against assignments to or deletions of __debug__.
//
// CPython: Python/symtable.c:L1591 check_name
func (b *builder) checkName(name string, loc ast.Pos, ctx ast.ExprContext) error {
	if name == "__debug__" {
		switch ctx {
		case ast.Store:
			return errorf(b.filename, loc, msgAssignDebug)
		case ast.Del:
			return errorf(b.filename, loc, msgDeleteDebug)
		}
	}
	return nil
}

// lookup returns the current block's flags for name (post-mangle), or
// zero if absent.
//
// CPython: Python/symtable.c:L1492 symtable_lookup
func (b *builder) lookup(name string) SymbolFlags {
	mangled := MaybeMangle(b.private, b.cur, name)
	return b.cur.Symbols[mangled]
}

// lookupEntry is the entry-specific variant used by walrus retarget.
//
// CPython: Python/symtable.c:L1478 symtable_lookup_entry
func (b *builder) lookupEntry(ste *Entry, name string) SymbolFlags {
	mangled := MaybeMangle(b.private, ste, name)
	return ste.Symbols[mangled]
}

// recordDirective stores the location of a `global`/`nonlocal`
// statement so error messages can point at the directive that caused
// the conflict.
//
// CPython: Python/symtable.c:L1778 symtable_record_directive
func (b *builder) recordDirective(name string, loc ast.Pos) error {
	mangled := MaybeMangle(b.private, b.cur, name)
	b.cur.Directives = append(b.cur.Directives, Directive{Name: mangled, Loc: loc})
	return nil
}

// allowsTopLevelAwait mirrors PyCF_ALLOW_TOP_LEVEL_AWAIT at module
// scope. compile() folds the flag into ff_features, so it is true only
// when that bit is set and the current block is the module body. Inside
// a function (or a comprehension scope) the normal await/async
// validation applies, exactly as in CPython.
//
// CPython: Python/symtable.c:L1831 allows_top_level_await
func (b *builder) allowsTopLevelAwait() bool {
	return b.future != nil &&
		b.future.Bits&future.AllowTopLevelAwait != 0 &&
		b.cur.Type == ModuleBlock
}

// isAsyncDef reports whether the current block is an async function
// body.
//
// CPython: Python/symtable.c:L90 IS_ASYNC_DEF
func (b *builder) isAsyncDef() bool {
	return b.cur.Type == FunctionBlock && b.cur.Coroutine
}
