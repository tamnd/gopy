// Port of cpython/Python/codegen.c PEP 634 structural pattern
// matching (L5640-L6461). The match statement walks each case in
// order; each case emits a pattern that consumes the subject from
// the stack and either falls through (success) or jumps to a
// fail-pop block. On success any captured names are stored from the
// stack; on failure the next case picks up the saved subject from
// COPY 1.

package compile

import (
	"fmt"
	"math/big"

	"github.com/tamnd/gopy/ast"
)

// patternContext is the per-Match scratch space threaded through every
// codegen_pattern_* call. Mirrors CPython's pattern_context struct.
//
// CPython: Python/codegen.c:L5640 pattern_context
type patternContext struct {
	// stores accumulates the names that should be STORE'd if the
	// whole pattern matches. Captures push their value to the bottom
	// of the stack and add the name here so the case-success tail
	// can replay them in order.
	stores []string

	// allowIrrefutable lets a wildcard (`case _`) or bare name
	// pattern serve as the case body. Irrefutable patterns are only
	// legal when the case is the last one or has a guard; subpattern
	// recursion always sets this to true since they cannot prevent
	// the surrounding pattern from failing.
	allowIrrefutable bool

	// failPop is one fail-pop block per stack depth. failPop[k] is
	// the label to jump to when k items need to be popped before
	// resuming the next case. Built lazily by ensureFailPop.
	failPop []JumpTargetLabel

	// onTop counts the items the active pattern has pushed on top of
	// the subject (or its replacement) since entering the case.
	onTop int
}

// visitMatch is the Match statement entry point. CPython lifts the
// success/end label and the per-case loop into codegen_match_inner;
// we follow the same shape.
//
// CPython: Python/codegen.c:L6453 codegen_match
func (c *Compiler) visitMatch(s *ast.Match) error {
	pc := &patternContext{}
	return c.matchInner(s, pc)
}

// matchInner walks the cases. It evaluates the subject once, then
// for each case (a) duplicates the subject for the next case if any
// remain, (b) walks the pattern, (c) on success emits the captures
// and the body, (d) on failure rolls into the next case via the
// fail-pop blocks.
//
// CPython: Python/codegen.c:L6375 codegen_match_inner
func (c *Compiler) matchInner(s *ast.Match, pc *patternContext) error {
	if err := c.visitExpr(s.Subject); err != nil {
		return err
	}
	end := c.newLabel()
	cases := len(s.Cases)
	if cases == 0 {
		return c.errorAt(loc(s), "match has zero cases")
	}
	last := s.Cases[cases-1]
	hasDefault := isWildcardPattern(last.Pattern) && cases > 1

	limit := cases
	if hasDefault {
		limit--
	}
	for i := 0; i < limit; i++ {
		m := s.Cases[i]
		if err := c.matchOneCase(m, i, limit, cases, pc, end); err != nil {
			return err
		}
	}
	if hasDefault {
		if err := c.matchDefaultCase(last, cases, end); err != nil {
			return err
		}
	}
	c.useLabel(end)
	return nil
}

// matchOneCase emits a refutable case arm: pattern, optional guard,
// captures, body, JUMP to end, then the per-arm fail-pop tail.
//
// CPython: Python/codegen.c:L6384 codegen_match_inner case loop
func (c *Compiler) matchOneCase(m *ast.MatchCase, i, limit, cases int,
	pc *patternContext, end JumpTargetLabel,
) error {
	notLast := i != limit-1
	if notLast {
		c.addOpI(COPY, 1, loc(m.Pattern))
	}
	pc.stores = pc.stores[:0]
	// allow_irrefutable lifts the "name capture / wildcard makes
	// remaining patterns unreachable" guard. CPython gates it on
	// i == cases - 1 (the absolute last case, not the last refutable
	// one): when there's a trailing wildcard arm, the non-wildcard
	// case ahead of it still has to be refutable so the wildcard can
	// actually catch something.
	//
	// CPython: Python/codegen.c:6384 codegen_match_inner
	pc.allowIrrefutable = m.Guard != nil || i == cases-1
	pc.failPop = nil
	pc.onTop = 0

	if err := c.visitPattern(m.Pattern, pc); err != nil {
		return err
	}
	for _, name := range pc.stores {
		if err := c.nameOpStore(name, loc(m.Pattern)); err != nil {
			return err
		}
	}
	if m.Guard != nil {
		if err := c.ensureFailPop(pc, 0); err != nil {
			return err
		}
		if err := c.visitExpr(m.Guard); err != nil {
			return err
		}
		c.addOpJump(POP_JUMP_IF_FALSE, pc.failPop[0], loc(m.Pattern))
	}
	if notLast {
		c.addOp(POP_TOP, loc(m.Pattern))
	}
	if err := c.visitStmts(m.Body); err != nil {
		return err
	}
	c.addOpJump(JUMP, end, ast.Pos{})
	return c.emitAndResetFailPop(pc, loc(m.Pattern))
}

// matchDefaultCase emits the trailing `case _` arm. CPython skips the
// pattern emit for this irrefutable case to avoid a redundant
// POP_TOP / push pair.
//
// CPython: Python/codegen.c:L6432 codegen_match_inner default case
func (c *Compiler) matchDefaultCase(m *ast.MatchCase, cases int, end JumpTargetLabel) error {
	if cases == 1 {
		c.addOp(POP_TOP, loc(m.Pattern))
	} else {
		c.addOp(NOP, loc(m.Pattern))
	}
	if m.Guard != nil {
		if err := c.visitExpr(m.Guard); err != nil {
			return err
		}
		c.addOpJump(POP_JUMP_IF_FALSE, end, loc(m.Pattern))
	}
	return c.visitStmts(m.Body)
}

// isWildcardPattern returns true if p is `_` or any MatchAs without a
// name and without a sub-pattern.
//
// CPython: Python/codegen.c:L5654 WILDCARD_CHECK macro
func isWildcardPattern(p ast.Pattern) bool {
	as, ok := p.(*ast.MatchAs)
	return ok && as.Name == nil && as.Pattern == nil
}

// isWildcardStarPattern returns true if p is `*_` (a MatchStar with
// no name).
//
// CPython: Python/codegen.c:L5657 WILDCARD_STAR_CHECK macro
func isWildcardStarPattern(p ast.Pattern) bool {
	st, ok := p.(*ast.MatchStar)
	return ok && st.Name == nil
}

// ensureFailPop grows pc.failPop until it can serve a fail at depth
// n (i.e. n+1 entries). Each fresh slot gets a brand-new label.
//
// CPython: Python/codegen.c:L5666 ensure_fail_pop
func (c *Compiler) ensureFailPop(pc *patternContext, n int) error {
	size := n + 1
	for len(pc.failPop) < size {
		pc.failPop = append(pc.failPop, c.newLabel())
	}
	return nil
}

// jumpToFailPop emits a jump to the correct fail-pop block, sized to
// pop both pc.onTop scratch items and the pending captures.
//
// CPython: Python/codegen.c:L5687 jump_to_fail_pop
func (c *Compiler) jumpToFailPop(pc *patternContext, op Opcode, l ast.Pos) error {
	pops := pc.onTop + len(pc.stores)
	if err := c.ensureFailPop(pc, pops); err != nil {
		return err
	}
	c.addOpJump(op, pc.failPop[pops], l)
	return nil
}

// emitAndResetFailPop chains the fail-pop blocks together: each block
// pops one element then falls through to the next, with the deepest
// block first. The final block (failPop[0]) is left unpopped because
// the case loop's COPY already accounts for it.
//
// CPython: Python/codegen.c:L5701 emit_and_reset_fail_pop
func (c *Compiler) emitAndResetFailPop(pc *patternContext, l ast.Pos) error {
	if len(pc.failPop) == 0 {
		return nil
	}
	for i := len(pc.failPop) - 1; i > 0; i-- {
		c.useLabel(pc.failPop[i])
		c.addOp(POP_TOP, l)
	}
	c.useLabel(pc.failPop[0])
	pc.failPop = nil
	return nil
}

// visitPattern dispatches each pattern kind to its codegen.
//
// CPython: Python/codegen.c:L6348 codegen_pattern
func (c *Compiler) visitPattern(p ast.Pattern, pc *patternContext) error {
	switch n := p.(type) {
	case *ast.MatchValue:
		return c.patternValue(n, pc)
	case *ast.MatchSingleton:
		return c.patternSingleton(n, pc)
	case *ast.MatchSequence:
		return c.patternSequence(n, pc)
	case *ast.MatchMapping:
		return c.patternMapping(n, pc)
	case *ast.MatchClass:
		return c.patternClass(n, pc)
	case *ast.MatchStar:
		return c.patternStar(n, pc)
	case *ast.MatchAs:
		return c.patternAs(n, pc)
	case *ast.MatchOr:
		return c.patternOr(n, pc)
	}
	return c.errorAt(loc(p), "unknown pattern kind %T", p)
}

// patternSubpattern is visitPattern with allowIrrefutable forced on.
// Subpatterns can never refuse the parent pattern, so they're allowed
// to be irrefutable even if the parent isn't.
//
// CPython: Python/codegen.c:L5852 codegen_pattern_subpattern
func (c *Compiler) patternSubpattern(p ast.Pattern, pc *patternContext) error {
	saved := pc.allowIrrefutable
	pc.allowIrrefutable = true
	err := c.visitPattern(p, pc)
	pc.allowIrrefutable = saved
	return err
}

// patternValue handles `case 42` and `case Foo.BAR`. Only Constant
// and Attribute exprs are legal; the AST validator catches the rest
// but we re-check.
//
// CPython: Python/codegen.c:L6322 codegen_pattern_value
func (c *Compiler) patternValue(p *ast.MatchValue, pc *patternContext) error {
	switch p.Value.(type) {
	case *ast.Constant, *ast.Attribute:
	default:
		return c.errorAt(loc(p), "patterns may only match literals and attribute lookups")
	}
	if err := c.visitExpr(p.Value); err != nil {
		return err
	}
	c.addOpI(COMPARE_OP, int32(cmpEq), loc(p))
	c.addOp(TO_BOOL, loc(p))
	return c.jumpToFailPop(pc, POP_JUMP_IF_FALSE, loc(p))
}

// patternSingleton handles `case None / True / False`. Identity
// rather than equality.
//
// CPython: Python/codegen.c:L6338 codegen_pattern_singleton
func (c *Compiler) patternSingleton(p *ast.MatchSingleton, pc *patternContext) error {
	c.addLoadConst(p.Value, loc(p))
	c.addOpI(IS_OP, 0, loc(p))
	return c.jumpToFailPop(pc, POP_JUMP_IF_FALSE, loc(p))
}

// patternStar handles a bare `*name` capture inside a sequence
// pattern. By the time we land here the slice is already on the
// stack; we just store or pop it.
//
// CPython: Python/codegen.c:L5888 codegen_pattern_star
func (c *Compiler) patternStar(p *ast.MatchStar, pc *patternContext) error {
	return c.patternStoreName(p.Name, pc, loc(p))
}

// patternAs handles `pattern as name` and bare `name` / `_`. The
// nameless / patternless form is the wildcard `_`; bare-name form is
// treated as a capture.
//
// CPython: Python/codegen.c:L5862 codegen_pattern_as
func (c *Compiler) patternAs(p *ast.MatchAs, pc *patternContext) error {
	if p.Pattern == nil {
		if !pc.allowIrrefutable {
			if p.Name != nil {
				return c.errorAt(loc(p), "name capture %q makes remaining patterns unreachable", *p.Name)
			}
			return c.errorAt(loc(p), "wildcard makes remaining patterns unreachable")
		}
		return c.patternStoreName(p.Name, pc, loc(p))
	}
	pc.onTop++
	c.addOpI(COPY, 1, loc(p))
	if err := c.visitPattern(p.Pattern, pc); err != nil {
		return err
	}
	pc.onTop--
	return c.patternStoreName(p.Name, pc, loc(p))
}

// patternStoreName implements the deferred-store helper: rotate the
// captured value below any existing scratch + capture stack, then
// record the name in pc.stores so the case-success tail knows to
// emit the store.
//
// CPython: Python/codegen.c:L5740 codegen_pattern_helper_store_name
func (c *Compiler) patternStoreName(name *string, pc *patternContext, l ast.Pos) error {
	if name == nil {
		c.addOp(POP_TOP, l)
		return nil
	}
	for _, n := range pc.stores {
		if n == *name {
			return c.errorAt(l, "multiple assignments to name %q in pattern", *name)
		}
	}
	rotations := pc.onTop + len(pc.stores) + 1
	c.patternRotate(rotations, l)
	pc.stores = append(pc.stores, *name)
	return nil
}

// patternRotate emits SWAP n / SWAP n-1 / ... / SWAP 2 to slide the
// top item underneath count-1 scratch items.
//
// CPython: Python/codegen.c:L5731 codegen_pattern_helper_rotate
func (c *Compiler) patternRotate(count int, l ast.Pos) {
	for count > 1 {
		c.addOpI(SWAP, int32(count), l)
		count--
	}
}

// patternSequence handles `[a, b, *rest, c]`. Strategy depends on
// where the star is and how many bindings the unpack involves.
//
// CPython: Python/codegen.c:L6265 codegen_pattern_sequence
func (c *Compiler) patternSequence(p *ast.MatchSequence, pc *patternContext) error {
	star := -1
	allWild := true
	starWild := false
	for i, sub := range p.Patterns {
		if _, ok := sub.(*ast.MatchStar); ok {
			if star >= 0 {
				return c.errorAt(loc(p), "multiple starred names in sequence pattern")
			}
			star = i
			starWild = isWildcardStarPattern(sub)
		}
		if !isWildcardPattern(sub) && !isWildcardStarPattern(sub) {
			allWild = false
		}
	}
	// Keep the subject on top across MATCH_SEQUENCE and the length
	// checks so jumpToFailPop knows the gate must pop the subject when
	// the match fails.
	//
	// CPython: Python/codegen.c:6280 codegen_pattern_sequence
	pc.onTop++
	c.addOp(MATCH_SEQUENCE, loc(p))
	if err := c.jumpToFailPop(pc, POP_JUMP_IF_FALSE, loc(p)); err != nil {
		return err
	}
	size := len(p.Patterns)
	if star < 0 {
		c.addOp(GET_LEN, loc(p))
		c.addLoadConst(int64(size), loc(p))
		c.addOpI(COMPARE_OP, int32(cmpEq), loc(p))
		c.addOp(TO_BOOL, loc(p))
		if err := c.jumpToFailPop(pc, POP_JUMP_IF_FALSE, loc(p)); err != nil {
			return err
		}
	} else if size > 1 {
		c.addOp(GET_LEN, loc(p))
		c.addLoadConst(int64(size-1), loc(p))
		c.addOpI(COMPARE_OP, int32(cmpGtE), loc(p))
		c.addOp(TO_BOOL, loc(p))
		if err := c.jumpToFailPop(pc, POP_JUMP_IF_FALSE, loc(p)); err != nil {
			return err
		}
	}
	// Past the gates the subject is consumed by the unpack / POP_TOP.
	pc.onTop--
	if allWild {
		c.addOp(POP_TOP, loc(p))
		return nil
	}
	if starWild {
		return c.patternSequenceSubscr(p, star, pc)
	}
	return c.patternSequenceUnpack(p, star, pc)
}

// patternSequenceUnpack uses UNPACK_SEQUENCE / UNPACK_EX, then
// recurses into each subpattern.
//
// CPython: Python/codegen.c:L5791 pattern_helper_sequence_unpack
func (c *Compiler) patternSequenceUnpack(p *ast.MatchSequence, star int, pc *patternContext) error {
	n := len(p.Patterns)
	if star < 0 {
		c.addOpI(UNPACK_SEQUENCE, int32(n), loc(p))
	} else {
		before := star
		after := n - star - 1
		c.addOpI(UNPACK_EX, int32(before+(after<<8)), loc(p))
	}
	pc.onTop += n
	for _, sub := range p.Patterns {
		pc.onTop--
		if err := c.patternSubpattern(sub, pc); err != nil {
			return err
		}
	}
	return nil
}

// patternSequenceSubscr handles the fast path for `[..., *_, ...]`:
// keep the subject and index into it directly instead of unpacking.
//
// CPython: Python/codegen.c:L5813 pattern_helper_sequence_subscr
func (c *Compiler) patternSequenceSubscr(p *ast.MatchSequence, star int, pc *patternContext) error {
	pc.onTop++
	size := len(p.Patterns)
	for i, sub := range p.Patterns {
		if isWildcardPattern(sub) {
			continue
		}
		if i == star {
			continue
		}
		c.addOpI(COPY, 1, loc(p))
		if i < star || star < 0 {
			c.addLoadConst(int64(i), loc(p))
		} else {
			c.addOp(GET_LEN, loc(p))
			c.addLoadConst(int64(size-i), loc(p))
			c.addOpI(BINARY_OP, nbSubtract, loc(p))
		}
		c.addOpI(BINARY_OP, nbSubscr, loc(p))
		if err := c.patternSubpattern(sub, pc); err != nil {
			return err
		}
	}
	pc.onTop--
	c.addOp(POP_TOP, loc(p))
	return nil
}

// isValidMappingPatternKey accepts the set of expressions the PEG
// grammar's literal_expr / complex_number alternatives can build into
// a mapping pattern key: bare Constants, Attribute lookups, and the
// signed_number / complex_number shapes the optimizer would later fold
// to a single Constant. CPython gets to skip this because its compiler
// runs _PyAST_Optimize before codegen, folding `-0-0j` to a single
// Constant; gopy has no AST optimizer pass yet, so the codegen needs
// to recognize the parser's literal-expression spelling directly.
//
// CPython: Python/ast_opt.c:457 fold_unaryop / fold_binop, called via
// Python/compile.c:1733 _PyCompile_AstOptimize
func isValidMappingPatternKey(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.Constant, *ast.Attribute:
		return true
	case *ast.UnaryOp:
		// signed_number / signed_real_number: USub Constant
		if v.Op != ast.USub {
			return false
		}
		_, ok := v.Operand.(*ast.Constant)
		return ok
	case *ast.BinOp:
		// complex_number: signed_real_number (+|-) imaginary_number
		if v.Op != ast.Add && v.Op != ast.Sub {
			return false
		}
		switch lhs := v.Left.(type) {
		case *ast.Constant:
		case *ast.UnaryOp:
			if lhs.Op != ast.USub {
				return false
			}
			if _, ok := lhs.Operand.(*ast.Constant); !ok {
				return false
			}
		default:
			return false
		}
		_, ok := v.Right.(*ast.Constant)
		return ok
	}
	return false
}

// patternMapping handles `{key: value, ...}` patterns. MATCH_MAPPING
// gates the type, MATCH_KEYS extracts the values for the listed
// keys, then each value pattern matches against the corresponding
// extracted entry.
//
// CPython: Python/codegen.c:L6012 codegen_pattern_mapping
func (c *Compiler) patternMapping(p *ast.MatchMapping, pc *patternContext) error {
	keys := p.Keys
	patterns := p.Patterns
	if len(keys) != len(patterns) {
		return c.errorAt(loc(p), "mapping pattern keys/patterns length mismatch")
	}
	pc.onTop++
	c.addOp(MATCH_MAPPING, loc(p))
	if err := c.jumpToFailPop(pc, POP_JUMP_IF_FALSE, loc(p)); err != nil {
		return err
	}
	if len(keys) == 0 && p.Rest == nil {
		pc.onTop--
		c.addOp(POP_TOP, loc(p))
		return nil
	}
	if len(keys) > 0 {
		c.addOp(GET_LEN, loc(p))
		c.addLoadConst(int64(len(keys)), loc(p))
		c.addOpI(COMPARE_OP, int32(cmpGtE), loc(p))
		c.addOp(TO_BOOL, loc(p))
		if err := c.jumpToFailPop(pc, POP_JUMP_IF_FALSE, loc(p)); err != nil {
			return err
		}
	}
	// CPython: Python/codegen.c:6060 mirror the seen-set duplicate check.
	// Detect duplicates by Python equality: 0 == False == 0.0 == 0j == -0
	// all share a bucket and must collide here so the SyntaxError fires
	// before the bytecode reaches MATCH_KEYS.
	seen := map[mappingDedupKey]struct{}{}
	for _, k := range keys {
		if !isValidMappingPatternKey(k) {
			return c.errorAt(loc(k), "mapping pattern keys may only match literals and attribute lookups")
		}
		if v, ok := foldedMappingKey(k); ok {
			if dk, ok := normalizeMappingKey(v); ok {
				if _, dup := seen[dk]; dup {
					return c.errorAt(loc(k), "mapping pattern checks duplicate key (%s)", literalRepr(v))
				}
				seen[dk] = struct{}{}
			}
		}
		if err := c.visitExpr(k); err != nil {
			return err
		}
	}
	c.addOpI(BUILD_TUPLE, int32(len(keys)), loc(p))
	c.addOp(MATCH_KEYS, loc(p))
	pc.onTop += 2
	c.addOpI(COPY, 1, loc(p))
	c.addLoadConst(nil, loc(p))
	c.addOpI(IS_OP, 1, loc(p))
	if err := c.jumpToFailPop(pc, POP_JUMP_IF_FALSE, loc(p)); err != nil {
		return err
	}
	c.addOpI(UNPACK_SEQUENCE, int32(len(keys)), loc(p))
	pc.onTop += len(keys) - 1
	for _, sub := range patterns {
		pc.onTop--
		if err := c.patternSubpattern(sub, pc); err != nil {
			return err
		}
	}
	pc.onTop -= 2
	if p.Rest != nil {
		// Build `rest = dict(subject)` minus the matched keys. The
		// stack on entry is [subject, keys]; we lower it to a single
		// dict copy that patternStoreName then captures.
		//
		// CPython: Python/codegen.c:6094 codegen_pattern_mapping star_target
		c.addOpI(BUILD_MAP, 0, loc(p))
		c.addOpI(SWAP, 3, loc(p))
		c.addOpI(DICT_UPDATE, 2, loc(p))
		c.addOpI(UNPACK_SEQUENCE, int32(len(keys)), loc(p))
		remaining := len(keys)
		for remaining > 0 {
			c.addOpI(COPY, int32(1+remaining), loc(p))
			c.addOpI(SWAP, 2, loc(p))
			c.addOp(DELETE_SUBSCR, loc(p))
			remaining--
		}
		return c.patternStoreName(p.Rest, pc, loc(p))
	}
	c.addOp(POP_TOP, loc(p))
	c.addOp(POP_TOP, loc(p))
	return nil
}

// patternClass handles `Foo(x, y, attr=z)`. Builds a tuple of keyword
// argument names (CPython packs both positional count and the kw
// names into MATCH_CLASS oparg/operand) and lets the runtime pull
// every match value before walking the subpatterns.
//
// CPython: Python/codegen.c:L5918 codegen_pattern_class
func (c *Compiler) patternClass(p *ast.MatchClass, pc *patternContext) error {
	nargs := len(p.Patterns)
	nattrs := len(p.KwdAttrs)
	if nargs+nattrs > 1<<31-1 {
		return c.errorAt(loc(p), "too many sub-patterns in class pattern")
	}
	if nattrs != len(p.KwdPatterns) {
		return c.errorAt(loc(p), "kwd_attrs (%d) / kwd_patterns (%d) length mismatch in class pattern", nattrs, len(p.KwdPatterns))
	}
	if err := c.visitExpr(p.Cls); err != nil {
		return err
	}
	for i, a := range p.KwdAttrs {
		for j := i + 1; j < nattrs; j++ {
			if a == p.KwdAttrs[j] {
				return c.errorAt(loc(p.KwdPatterns[j]), "attribute name repeated in class pattern: %s", a)
			}
		}
	}
	for _, a := range p.KwdAttrs {
		c.addLoadConst(a, loc(p))
	}
	c.addOpI(BUILD_TUPLE, int32(nattrs), loc(p))
	c.addOpI(MATCH_CLASS, int32(nargs), loc(p))
	c.addOpI(COPY, 1, loc(p))
	c.addLoadConst(nil, loc(p))
	c.addOpI(IS_OP, 1, loc(p))
	pc.onTop++
	if err := c.jumpToFailPop(pc, POP_JUMP_IF_FALSE, loc(p)); err != nil {
		return err
	}
	c.addOpI(UNPACK_SEQUENCE, int32(nargs+nattrs), loc(p))
	pc.onTop += nargs + nattrs - 1
	for i := 0; i < nargs+nattrs; i++ {
		pc.onTop--
		var sub ast.Pattern
		if i < nargs {
			sub = p.Patterns[i]
		} else {
			sub = p.KwdPatterns[i-nargs]
		}
		if isWildcardPattern(sub) {
			c.addOp(POP_TOP, loc(p))
			continue
		}
		if err := c.patternSubpattern(sub, pc); err != nil {
			return err
		}
	}
	return nil
}

// patternOr handles `case A | B | C`. Each alternative shares the
// captured-store layout of the first; alternatives that bind names in
// a different order get their stack slots rotated to match. The whole
// OR keeps a COPY of the subject on top of stack across alternatives
// so each alt can match against it without consuming.
//
// CPython: Python/codegen.c:6120 codegen_pattern_or.
func (c *Compiler) patternOr(p *ast.MatchOr, pc *patternContext) error {
	end := c.newLabel()
	size := len(p.Patterns)
	if size < 2 {
		return c.errorAt(loc(p), "or-pattern requires at least two alternatives")
	}
	oldStores := pc.stores
	oldFailPop := pc.failPop
	oldOnTop := pc.onTop
	oldAllowIrrefutable := pc.allowIrrefutable
	var control []string
	for i, alt := range p.Patterns {
		pc.stores = nil
		pc.failPop = nil
		pc.onTop = 0
		pc.allowIrrefutable = (i == size-1) && oldAllowIrrefutable
		c.addOpI(COPY, 1, loc(alt))
		if err := c.visitPattern(alt, pc); err != nil {
			return err
		}
		nstores := len(pc.stores)
		if i == 0 {
			control = append([]string(nil), pc.stores...)
		} else {
			if nstores != len(control) {
				return c.errorAt(loc(alt), "alternative patterns bind different names")
			}
			if nstores > 0 {
				for icontrol := nstores - 1; icontrol >= 0; icontrol-- {
					name := control[icontrol]
					istores := indexOf(pc.stores, name)
					if istores < 0 {
						return c.errorAt(loc(alt), "alternative patterns bind different names")
					}
					if icontrol != istores {
						rotations := istores + 1
						rotated := append([]string(nil), pc.stores[:rotations]...)
						pc.stores = append(pc.stores[:0], pc.stores[rotations:]...)
						insertAt := icontrol - istores
						tail := append([]string(nil), pc.stores[insertAt:]...)
						pc.stores = append(pc.stores[:insertAt], rotated...)
						pc.stores = append(pc.stores, tail...)
						for r := rotations; r > 0; r-- {
							c.patternRotate(icontrol+1, loc(alt))
						}
					}
				}
			}
		}
		c.addOpJump(JUMP, end, loc(alt))
		if err := c.emitAndResetFailPop(pc, loc(alt)); err != nil {
			return err
		}
	}
	pc.stores = oldStores
	pc.failPop = oldFailPop
	pc.onTop = oldOnTop
	pc.allowIrrefutable = oldAllowIrrefutable
	c.addOp(POP_TOP, loc(p))
	if err := c.jumpToFailPop(pc, JUMP, loc(p)); err != nil {
		return err
	}
	c.useLabel(end)
	nrots := len(control) + 1 + pc.onTop + len(pc.stores)
	for _, name := range control {
		c.patternRotate(nrots, loc(p))
		for _, n := range pc.stores {
			if n == name {
				return c.errorAt(loc(p), "multiple assignments to name %q in pattern", name)
			}
		}
		pc.stores = append(pc.stores, name)
	}
	c.addOp(POP_TOP, loc(p))
	return nil
}

func indexOf(s []string, v string) int {
	for i, n := range s {
		if n == v {
			return i
		}
	}
	return -1
}

// mappingDedupKey is the canonical bucket every numeric value sharing
// Python equality collapses into. Strings, bytes, and singletons take
// their own kind so they cannot collide with a number.
type mappingDedupKey struct {
	kind byte
	s    string
	c    complex128
}

// foldedMappingKey returns the Python-value a mapping-pattern key
// resolves to. CPython gets here from AST optimizer constant folding;
// gopy folds the same three shapes (Constant, UnaryOp USub Constant,
// BinOp real ± imaginary) inline.
//
// CPython: Python/ast_opt.c:457 fold_unaryop / fold_binop (folded prior
// to codegen.c:5989 codegen_pattern_mapping_key)
func foldedMappingKey(e ast.Expr) (any, bool) {
	switch v := e.(type) {
	case *ast.Constant:
		return v.Value, true
	case *ast.UnaryOp:
		if v.Op != ast.USub {
			return nil, false
		}
		c, ok := v.Operand.(*ast.Constant)
		if !ok {
			return nil, false
		}
		return negateConstValue(c.Value), true
	case *ast.BinOp:
		if v.Op != ast.Add && v.Op != ast.Sub {
			return nil, false
		}
		left, lok := signedConstValue(v.Left)
		if !lok {
			return nil, false
		}
		rc, rok := v.Right.(*ast.Constant)
		if !rok {
			return nil, false
		}
		right := rc.Value
		if v.Op == ast.Sub {
			right = negateConstValue(right)
		}
		return addConstValues(left, right), true
	}
	return nil, false
}

// signedConstValue accepts a bare Constant or UnaryOp(USub, Constant)
// and returns the folded numeric value. Used as the left-operand
// helper for foldedMappingKey's BinOp arm.
func signedConstValue(e ast.Expr) (any, bool) {
	switch v := e.(type) {
	case *ast.Constant:
		return v.Value, true
	case *ast.UnaryOp:
		if v.Op != ast.USub {
			return nil, false
		}
		c, ok := v.Operand.(*ast.Constant)
		if !ok {
			return nil, false
		}
		return negateConstValue(c.Value), true
	}
	return nil, false
}

func negateConstValue(v any) any {
	switch x := v.(type) {
	case int64:
		return -x
	case float64:
		return -x
	case complex128:
		return -x
	case *big.Int:
		return new(big.Int).Neg(x)
	case bool:
		if x {
			return int64(-1)
		}
		return int64(0)
	}
	return v
}

func addConstValues(a, b any) any {
	af, aok := toComplexValue(a)
	bf, bok := toComplexValue(b)
	if !aok || !bok {
		return a
	}
	return af + bf
}

func toComplexValue(v any) (complex128, bool) {
	switch x := v.(type) {
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	case int64:
		return complex(float64(x), 0), true
	case float64:
		return complex(x, 0), true
	case complex128:
		return x, true
	case *big.Int:
		f, _ := new(big.Float).SetInt(x).Float64()
		return complex(f, 0), true
	}
	return 0, false
}

// normalizeMappingKey returns the dedup bucket for v. Numeric values
// (bool, int, float, complex) collapse to the same kind so that
// `{0: _, False: _}`, `{0: _, 0.0: _}`, `{0: _, 0j: _}`, and
// `{0: _, -0: _}` all flag as duplicates the way CPython's PySet would.
func normalizeMappingKey(v any) (mappingDedupKey, bool) {
	switch x := v.(type) {
	case nil:
		return mappingDedupKey{kind: 'N'}, true
	case bool, int64, float64, complex128:
		c, _ := toComplexValue(x)
		return mappingDedupKey{kind: 'n', c: c}, true
	case *big.Int:
		if x.IsInt64() {
			f := float64(x.Int64())
			return mappingDedupKey{kind: 'n', c: complex(f, 0)}, true
		}
		return mappingDedupKey{kind: 'I', s: x.String()}, true
	case string:
		return mappingDedupKey{kind: 's', s: x}, true
	case []byte:
		return mappingDedupKey{kind: 'b', s: string(x)}, true
	}
	return mappingDedupKey{}, false
}

// literalRepr returns a CPython-faithful repr() for the values that
// reach mapping-pattern keys. Only the literal kinds get a custom
// spelling; anything else falls back to %v which is good enough for
// SyntaxError diagnostics.
func literalRepr(v any) string {
	switch x := v.(type) {
	case nil:
		return "None"
	case bool:
		if x {
			return "True"
		}
		return "False"
	case int64:
		return fmt.Sprintf("%d", x)
	case *big.Int:
		return x.String()
	case float64:
		return fmt.Sprintf("%g", x)
	case complex128:
		if real(x) == 0 {
			return fmt.Sprintf("%gj", imag(x))
		}
		return fmt.Sprintf("(%g+%gj)", real(x), imag(x))
	case string:
		return fmt.Sprintf("%q", x)
	}
	return fmt.Sprintf("%v", v)
}
