package symtable

import "fmt"

// nameSet is a small set-of-strings helper so the analyze pass reads
// the way CPython's PySet_*-driven version does. add/discard/contains/
// clone/union port PySet_Add / PySet_Discard / PySet_Contains /
// PySet_New / _PySet_Update respectively.
//
// CPython: Python/symtable.c PySet_* calls in analyze_block
type nameSet map[string]struct{}

func newNameSet() nameSet                   { return nameSet{} }
func (s nameSet) add(name string)           { s[name] = struct{}{} }
func (s nameSet) discard(name string) bool  { _, ok := s[name]; delete(s, name); return ok }
func (s nameSet) contains(name string) bool { _, ok := s[name]; return ok }

func (s nameSet) clone() nameSet {
	out := make(nameSet, len(s))
	for k := range s {
		out[k] = struct{}{}
	}
	return out
}

func (s nameSet) union(other nameSet) {
	for k := range other {
		s[k] = struct{}{}
	}
}

// analyze runs pass two: it walks each Entry top-down, classifies
// every symbol as Local / GlobalExplicit / GlobalImplicit / Free /
// Cell, propagates free variables up the lexical stack, and inlines
// non-generator comprehensions into their parent scopes.
//
// CPython: Python/symtable.c:L1369 symtable_analyze
func analyze(t *Table) error {
	free := newNameSet()
	global := newNameSet()
	typeParams := newNameSet()
	return analyzeBlock(t, t.Top, nil, free, global, typeParams, nil)
}

// analyzeBlock makes the final scope decisions for one Entry. Mirrors
// analyze_block in symtable.c.
//
// CPython: Python/symtable.c:L1131 analyze_block
func analyzeBlock(t *Table, ste *Entry, bound, free, global, typeParams nameSet, classEntry *Entry) error {
	local := newNameSet()
	scopes := make(map[string]Scope)
	newbound := newNameSet()
	newglobal := newNameSet()
	newfree := newNameSet()
	inlinedCells := newNameSet()

	prepareClassPreambleSets(ste, bound, global, newbound, newglobal)
	for name, flags := range ste.Symbols {
		if err := analyzeName(t, ste, scopes, name, flags,
			bound, local, free, global, typeParams, classEntry); err != nil {
			return err
		}
	}
	finalizeChildSets(ste, bound, global, local, newbound, newglobal)

	if err := analyzeChildren(t, ste, classEntry, scopes, newbound, newglobal, newfree, typeParams, inlinedCells); err != nil {
		return err
	}
	spliceInlinedChildren(ste)

	if ste.IsFunctionLike() {
		analyzeCells(scopes, newfree, inlinedCells)
	} else if ste.Type == ClassBlock {
		dropClassFree(ste, scopes, newfree)
	}
	classflag := ste.Type == ClassBlock || ste.CanSeeClassScope
	updateSymbols(ste.Symbols, scopes, bound, newfree, inlinedCells, classflag)

	free.union(newfree)
	return nil
}

// prepareClassPreambleSets seeds the class block's child-visible
// bound/global sets before name analysis. Mirrors the early
// "ClassBlock pre-population" branch in analyze_block.
//
// CPython: Python/symtable.c:L1131 analyze_block ClassBlock prologue
func prepareClassPreambleSets(ste *Entry, bound, global, newbound, newglobal nameSet) {
	if ste.Type != ClassBlock {
		return
	}
	newglobal.union(global)
	if bound != nil {
		newbound.union(bound)
	}
}

// finalizeChildSets fills the bound/global sets passed to children
// after name analysis runs.
//
// CPython: Python/symtable.c analyze_block child-set finalize step
func finalizeChildSets(ste *Entry, bound, global, local, newbound, newglobal nameSet) {
	if ste.Type == ClassBlock {
		newbound.add("__class__")
		newbound.add("__classdict__")
		newbound.add("__conditional_annotations__")
		return
	}
	if ste.IsFunctionLike() {
		newbound.union(local)
	}
	if bound != nil {
		newbound.union(bound)
	}
	newglobal.union(global)
}

// analyzeChildren recurses into each child block, propagating free
// variables back to the parent and inlining eligible comprehensions.
//
// CPython: Python/symtable.c analyze_block children-loop body
func analyzeChildren(t *Table, ste *Entry, classEntry *Entry, scopes map[string]Scope,
	newbound, newglobal, newfree, typeParams, inlinedCells nameSet,
) error {
	for _, child := range ste.Children {
		newClassEntry := pickClassEntry(ste, child, classEntry)
		// CPython 3.12 (PEP 709) inlines non-generator comprehensions
		// into their parent scope; the symtable side records that with
		// child.CompInlined. gopy's codegen still emits a separate code
		// object for the comp body (real inlining is spec 1696), so the
		// inline path leaves Free vars in the comp pointing at no cell.
		// Until codegen actually inlines, treat every comp as a normal
		// nested scope so analyzeCells promotes captured outer locals
		// to Cell and the closure tuple is built.
		//
		// CPython: Python/symtable.c:1265 inline_comprehension branch
		inlineComp := false

		childFree, err := analyzeChildBlock(t, child, newbound, newfree, newglobal, typeParams, newClassEntry)
		if err != nil {
			return err
		}
		if inlineComp {
			if err := inlineComprehension(ste, child, scopes, childFree, inlinedCells); err != nil {
				return err
			}
			child.CompInlined = true
		}
		newfree.union(childFree)
	}
	return nil
}

// pickClassEntry chooses the class scope (if any) that the child
// block can see, honoring CPython's CanSeeClassScope cascade.
//
// CPython: Python/symtable.c CanSeeClassScope cascade in analyze_block
func pickClassEntry(ste, child *Entry, classEntry *Entry) *Entry {
	if !child.CanSeeClassScope {
		return nil
	}
	if ste.Type == ClassBlock {
		return ste
	}
	return classEntry
}

// spliceInlinedChildren rewrites ste.Children so that inlined
// comprehensions are replaced by their own Children. Mirrors the
// PyList_SetSlice loop in analyze_block.
//
// CPython: Python/symtable.c PyList_SetSlice splice in analyze_block
func spliceInlinedChildren(ste *Entry) {
	if !anyInlined(ste.Children) {
		return
	}
	merged := make([]*Entry, 0, len(ste.Children))
	for _, c := range ste.Children {
		if c.CompInlined {
			merged = append(merged, c.Children...)
			continue
		}
		merged = append(merged, c)
	}
	ste.Children = merged
}

// analyzeChildBlock copies the inherited sets so each subtree gets a
// private working namespace. Mirrors analyze_child_block.
//
// CPython: Python/symtable.c:L1325 analyze_child_block
func analyzeChildBlock(t *Table, entry *Entry, bound, free, global, typeParams nameSet, classEntry *Entry) (nameSet, error) {
	tempBound := bound.clone()
	tempFree := free.clone()
	tempGlobal := global.clone()
	tempTypeParams := typeParams.clone()
	if err := analyzeBlock(t, entry, tempBound, tempFree, tempGlobal, tempTypeParams, classEntry); err != nil {
		return nil, err
	}
	return tempFree, nil
}

// analyzeName classifies a single name in ste.
//
// CPython: Python/symtable.c:L666 analyze_name
func analyzeName(t *Table, ste *Entry, scopes map[string]Scope, name string, flags SymbolFlags,
	bound, local, free, global, typeParams nameSet, classEntry *Entry,
) error {
	if flags&DefGlobal != 0 {
		if flags&DefNonlocal != 0 {
			return errorAtDirective(t, ste, name, msgNonlocalGlobal)
		}
		scopes[name] = GlobalExplicit
		global.add(name)
		if bound != nil {
			bound.discard(name)
		}
		return nil
	}
	if flags&DefNonlocal != 0 {
		if bound == nil {
			for _, d := range ste.Directives {
				if d.Name == name {
					return errorf(t.Filename, d.Loc, msgNonlocalAtModule)
				}
			}
			return errorf(t.Filename, ste.Loc, msgNonlocalAtModule)
		}
		if !bound.contains(name) {
			return errorAtDirective(t, ste, name, msgNoBindingNonlocal)
		}
		if typeParams.contains(name) {
			return errorAtDirective(t, ste, name, msgNonlocalTypeParam)
		}
		scopes[name] = Free
		free.add(name)
		return nil
	}
	if flags&DefBound != 0 {
		scopes[name] = Local
		local.add(name)
		global.discard(name)
		if flags&DefTypeParam != 0 {
			typeParams.add(name)
		} else {
			typeParams.discard(name)
		}
		return nil
	}
	if classEntry != nil {
		classFlags := classEntry.GetSymbol(name)
		if classFlags&DefGlobal != 0 {
			scopes[name] = GlobalExplicit
			return nil
		}
		if classFlags&DefBound != 0 && classFlags&DefNonlocal == 0 {
			scopes[name] = GlobalImplicit
			return nil
		}
	}
	if bound != nil && bound.contains(name) {
		scopes[name] = Free
		free.add(name)
		return nil
	}
	if global != nil && global.contains(name) {
		scopes[name] = GlobalImplicit
		return nil
	}
	scopes[name] = GlobalImplicit
	return nil
}

// errorAtDirective points the error at the global / nonlocal
// statement that introduced the conflict. CPython walks
// ste->ste_directives until it finds a name match; we do the same.
//
// CPython: Python/symtable.c:L576 error_at_directive
func errorAtDirective(t *Table, ste *Entry, name, msg string) error {
	for _, d := range ste.Directives {
		if d.Name == name {
			return errorf(t.Filename, d.Loc, msg, name)
		}
	}
	return errorf(t.Filename, ste.Loc, msg, name)
}

// analyzeCells flips Local → Cell for any local that a child captured.
//
// CPython: Python/symtable.c:L913 analyze_cells
func analyzeCells(scopes map[string]Scope, free, inlinedCells nameSet) {
	for name, scope := range scopes {
		if scope != Local {
			continue
		}
		if !free.contains(name) && !inlinedCells.contains(name) {
			continue
		}
		scopes[name] = Cell
		free.discard(name)
	}
}

// dropClassFree removes the implicit class-scope names from a class
// block's free set and records the corresponding ste_needs_* flag.
// When the discard fires the class scope owns the implicit cell, so
// stamp the symbol as Cell here to spare callers a separate lookup
// path. CPython does the same shaping in compile.c (u_cellvars), but
// gopy keeps the cell view inside the symtable.
//
// CPython: Python/symtable.c:L958 drop_class_free
func dropClassFree(ste *Entry, scopes map[string]Scope, free nameSet) {
	if free.discard("__class__") {
		ste.NeedsClassClosure = true
		stampImplicitCell(ste, scopes, "__class__")
	}
	if free.discard("__classdict__") {
		ste.NeedsClassDict = true
		stampImplicitCell(ste, scopes, "__classdict__")
	}
	if free.discard("__conditional_annotations__") {
		ste.HasConditionalAnnotations = true
	}
}

// stampImplicitCell records name as a Cell-scoped, locally-bound
// symbol on ste, keeping the parallel scopes map in sync so the
// updateSymbols pass that runs right after sees a resolved scope.
// CPython tracks the same fact via a separate u_cellvars list in the
// compiler unit; gopy folds it into the symtable so downstream codegen
// sees a uniform Cell scope without extra plumbing.
//
// CPython: Python/compile.c:L610 needs_class_closure cellvars handling
func stampImplicitCell(ste *Entry, scopes map[string]Scope, name string) {
	flags := ste.Symbols[name]
	flags |= DefLocal
	flags &^= ScopeMask << ScopeOffset
	ste.Symbols[name] = flags
	scopes[name] = Cell
}

// updateSymbols writes scope information back into ste.Symbols and
// records still-unresolved free variables.
//
// CPython: Python/symtable.c:L985 update_symbols
func updateSymbols(symbols map[string]SymbolFlags, scopes map[string]Scope,
	bound, free, inlinedCells nameSet, classflag bool,
) {
	for name, flags := range symbols {
		if inlinedCells.contains(name) {
			flags |= DefCompCell
		}
		scope, ok := scopes[name]
		if !ok {
			panic(fmt.Sprintf("symtable: missing scope for %q", name))
		}
		flags |= SymbolFlags(scope) << ScopeOffset
		symbols[name] = flags
	}
	freeFlag := SymbolFlags(Free) << ScopeOffset
	for name := range free {
		if existing, ok := symbols[name]; ok {
			if classflag {
				existing |= DefFreeClass
				symbols[name] = existing
			}
			continue
		}
		if bound != nil && !bound.contains(name) {
			continue
		}
		symbols[name] = freeFlag
	}
}

// inlineComprehension folds a comprehension's symbols into its parent
// when the analyzer chose to inline it.
//
// CPython: Python/symtable.c:L802 inline_comprehension
func inlineComprehension(ste, comp *Entry, scopes map[string]Scope, compFree, inlinedCells nameSet) error {
	var (
		removeDunderClass           bool
		removeDunderClassdict       bool
		removeDunderCondAnnotations bool
	)
	for k, compFlags := range comp.Symbols {
		if compFlags&DefParam != 0 {
			if k != ".0" {
				return fmt.Errorf("symtable: unexpected comprehension param %q", k)
			}
			continue
		}
		scope := compFlags.Scope()
		onlyFlags := compFlags & ((1 << ScopeOffset) - 1)
		if scope == Cell || onlyFlags&DefCompCell != 0 {
			inlinedCells.add(k)
		}
		_, existing := ste.Symbols[k]
		if scope == Free && ste.Type == ClassBlock &&
			(k == "__class__" || k == "__classdict__" || k == "__conditional_annotations__") {
			scope = GlobalImplicit
			compFree.discard(k)
			switch k {
			case "__class__":
				removeDunderClass = true
			case "__conditional_annotations__":
				removeDunderCondAnnotations = true
			default:
				removeDunderClassdict = true
			}
		}
		if !existing {
			ste.Symbols[k] = onlyFlags
			scopes[k] = scope
			continue
		}
		flags := ste.Symbols[k]
		if flags&DefBound != 0 && ste.Type != ClassBlock {
			if !isFreeInAnyChild(comp, k) {
				compFree.discard(k)
			}
		}
	}
	if removeDunderClass {
		delete(comp.Symbols, "__class__")
	}
	if removeDunderClassdict {
		delete(comp.Symbols, "__classdict__")
	}
	if removeDunderCondAnnotations {
		delete(comp.Symbols, "__conditional_annotations__")
	}
	return nil
}

// isFreeInAnyChild reports whether name is captured Free in any of
// entry's children.
//
// CPython: Python/symtable.c:L785 is_free_in_any_child
func isFreeInAnyChild(entry *Entry, name string) bool {
	for _, c := range entry.Children {
		if c.GetScope(name) == Free {
			return true
		}
	}
	return false
}

// anyInlined reports whether any child has been marked CompInlined.
//
// CPython: Python/symtable.c CompInlined check in analyze_block splice
func anyInlined(children []*Entry) bool {
	for _, c := range children {
		if c.CompInlined {
			return true
		}
	}
	return false
}
