// CPython: Parser/pegen.c memo + lookahead + forced-expect surface.
// The generated parser table reaches into these to cache rule
// results, peek without committing, and surface "expected ':'"-style
// errors when a forced production fails.

package pegen

import (
	"github.com/tamnd/gopy/tokenize"
)

// IsMemoized checks whether the rule with id ruleType has already
// been parsed at the current mark. If yes, advances mark past the
// cached match and returns the cached node and true. If no, returns
// nil and false. Returns false also on EOF.
//
// CPython: Parser/pegen.c:349 _PyPegen_is_memoized
func (p *Parser) IsMemoized(ruleType int) (any, bool) {
	if p.mark == p.fill {
		if p.fillToken() < 0 {
			p.errorIndicator = true
			return nil, false
		}
	}
	if p.mark >= len(p.tokens) {
		return nil, false
	}
	for m := p.tokens[p.mark].memo; m != nil; m = m.next {
		if m.rule == ruleType {
			p.mark = m.mark
			return m.node, true
		}
	}
	return nil, false
}

// InsertMemo caches a parse result. mark is the position at which
// the rule started; the call site captures that before invoking the
// rule, then passes it back here once a node was produced.
//
// CPython: Parser/pegen.c:80 _PyPegen_insert_memo
func (p *Parser) InsertMemo(mark, ruleType int, node any) {
	if mark < 0 || mark >= len(p.tokens) {
		return
	}
	t := p.tokens[mark]
	t.memo = &memo{rule: ruleType, node: node, mark: p.mark, next: t.memo}
}

// UpdateMemo overwrites an existing cache entry for the same rule
// or inserts a new one. Used by left-recursive rule bodies.
//
// CPython: Parser/pegen.c:97 _PyPegen_update_memo
func (p *Parser) UpdateMemo(mark, ruleType int, node any) {
	if mark < 0 || mark >= len(p.tokens) {
		return
	}
	for m := p.tokens[mark].memo; m != nil; m = m.next {
		if m.rule == ruleType {
			m.node = node
			m.mark = p.mark
			return
		}
	}
	p.InsertMemo(mark, ruleType, node)
}

// Lookahead runs fn at the current mark, restores mark, and returns
// whether the result matches the requested polarity. Positive-true
// reports whether fn produced a non-nil node.
//
// CPython: Parser/pegen.c:392 _PyPegen_lookahead
func (p *Parser) Lookahead(positive bool, fn func(*Parser) any) bool {
	mark := p.mark
	res := fn(p)
	p.mark = mark
	matched := res != nil
	return matched == positive
}

// LookaheadWithName runs fn at the current mark, restores mark, and
// returns whether the result matches the requested polarity. The
// helper is the named-result variant the generated parser emits when
// a positive-lookahead block has a label; the body is identical to
// Lookahead, the name is propagated by the generator into the
// surrounding rule's diagnostic plumbing.
//
// CPython: Parser/pegen.c:402 _PyPegen_lookahead_with_name
func (p *Parser) LookaheadWithName(positive bool, fn func(*Parser) any, _ string) bool {
	return p.Lookahead(positive, fn)
}

// ExpectToken is the kind-only form CPython emits as expect_token.
// Returns the token on match (advancing mark) or nil on miss.
//
// CPython: Parser/pegen.c:296 _PyPegen_expect_token
func (p *Parser) ExpectToken(kind tokenize.Type) *Token { return p.Expect(kind) }

// ExpectForced advances past a token of kind kind. If the next
// token does not match, it raises a SyntaxError with the "expected
// '%s'" template and trips the error indicator.
//
// CPython: Parser/pegen.c:441 _PyPegen_expect_forced_token
func (p *Parser) ExpectForced(kind tokenize.Type, expected string) *Token {
	if p.errorIndicator {
		return nil
	}
	t := p.Peek()
	if t == nil || t.Type != kind {
		p.errorIndicator = true
		return nil
	}
	_ = expected // surfaced via the errors panel once builder.go is wired into the parser
	p.mark++
	return t
}

// ExpectSoftKeyword advances past a NAME whose body matches kw.
// Soft keywords (match, case, type, _) are not in the keyword
// table, so the generated parser uses this to recognize them by
// content rather than token kind.
//
// CPython: Parser/pegen.c:425 _PyPegen_expect_soft_keyword
func (p *Parser) ExpectSoftKeyword(kw string) *Token {
	t := p.Peek()
	if t == nil || t.Type != tokenize.NAME || string(t.Bytes) != kw {
		return nil
	}
	p.mark++
	return t
}

// LastNonWhitespaceToken returns the most recently seen token that
// is not a NEWLINE, NL, INDENT, or DEDENT. Used by error messages
// that want to point at the real preceding token.
//
// CPython: Parser/pegen.c:469 _PyPegen_get_last_nonnwhitespace_token
func (p *Parser) LastNonWhitespaceToken() *Token {
	for i := p.fill - 1; i >= 0; i-- {
		t := p.tokens[i]
		switch t.Type {
		case tokenize.NEWLINE, tokenize.NL, tokenize.INDENT, tokenize.DEDENT:
			continue
		}
		return t
	}
	return nil
}
