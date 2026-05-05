// CPython: Parser/pegen.c token buffer fill / lookahead.
// Split from parser.go to keep the high-volume hot path in one
// file: every rule action that calls Peek/Expect runs through
// here. The mark/reset machinery is in parser.go.

package pegen

// PeekAhead returns the token n positions past the current mark,
// filling the buffer if needed. PeekAhead(0) is the same as
// Peek().
//
// CPython: Parser/pegen.c:128 _PyPegen_lookahead_with_token
func (p *Parser) PeekAhead(n int) *Token {
	want := p.mark + n
	for want >= p.fill {
		if p.fillToken() < 0 {
			return nil
		}
	}
	if want >= len(p.tokens) {
		return nil
	}
	return p.tokens[want]
}

// FillUntil grows the token buffer until it covers `mark+n` and
// returns the resulting fill index. Used by Lookahead and the
// invalid-rule replay machinery.
//
// CPython: Parser/pegen.c:69 _PyPegen_fill_until
func (p *Parser) FillUntil(n int) int {
	target := p.mark + n
	for target >= p.fill {
		if p.fillToken() < 0 {
			break
		}
	}
	return p.fill
}

// Tokens returns a snapshot of the filled token buffer. The
// generated parser only needs Peek/Expect; this is here for
// debug printing and error reporting.
func (p *Parser) Tokens() []*Token {
	out := make([]*Token, len(p.tokens))
	copy(out, p.tokens)
	return out
}

// Fill is the buffer high-water mark.
//
// CPython: Parser/pegen.h:78 fill
func (p *Parser) Fill() int { return p.fill }
