// Token-leaf helpers that the C generator emits implicitly when a
// rule's lone item is a NAME / NUMBER / STRING token leaf. They are
// the Go ports of the same family in cpython/Parser/pegen.c.

package pegen

import (
	"strings"

	"github.com/tamnd/gopy/ast"
	"github.com/tamnd/gopy/module/unicodedata"
	"github.com/tamnd/gopy/token"
)

// nameFromToken builds a Load-context Name expression from a NAME
// token. Returns nil and pins the error indicator if the token is
// missing or its bytes do not decode. PEP 3131 requires non-ASCII
// identifiers to be NFKC-normalised before they reach the symtable;
// CPython does that in _PyPegen_new_identifier and gopy mirrors it
// here so identifiers like `µ` (U+00B5) compare equal to `μ`
// (U+03BC).
//
// CPython: Parser/pegen.c:502 _PyPegen_new_identifier
// CPython: Parser/pegen.c:572 _PyPegen_name_from_token
func nameFromToken(p *Parser, t *Token) ast.Expr {
	if t == nil {
		return nil
	}
	id := normalizeIdentifier(string(t.Bytes))
	if id == "" {
		p.errorIndicator = true
		return nil
	}
	return &ast.Name{
		Id:  id,
		Ctx: ast.Load,
		Pos: ast.Pos{
			Lineno:       t.Lineno,
			ColOffset:    t.ColOff,
			EndLineno:    t.EndLine,
			EndColOffset: t.EndCol,
		},
	}
}

// normalizeIdentifier returns the NFKC-normalised form of id when id
// contains any non-ASCII bytes. ASCII identifiers short-circuit.
//
// CPython: Parser/pegen.c:502 _PyPegen_new_identifier (the
// PyUnicode_IS_ASCII fast path + PyObject_Vectorcall(p->normalize)
// branch).
func normalizeIdentifier(id string) string {
	if id == "" {
		return id
	}
	ascii := true
	for i := 0; i < len(id); i++ {
		if id[i] >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii {
		return id
	}
	return string(unicodedata.NFKC([]rune(id)))
}

// nameToken consumes the next NAME token and lifts it to an Expr.
//
// CPython: Parser/pegen.c:592 _PyPegen_name_token
func nameToken(p *Parser) ast.Expr {
	t := p.ExpectToken(token.NAME)
	return nameFromToken(p, t)
}

// stringToken consumes the next STRING token. CPython returns void *
// (effectively Token *) so the caller can feed it into
// _PyPegen_concatenate_strings later; we mirror that by returning the
// raw Token unchanged.
//
// CPython: Parser/pegen.c:599 _PyPegen_string_token
func stringToken(p *Parser) *Token {
	return p.ExpectToken(token.STRING)
}

// numberToken consumes the next NUMBER token and lifts it to a
// Constant expression. The numeric value is parsed by the same logic
// as parsenumber / parsenumber_raw in CPython.
//
// CPython: Parser/pegen.c:695 _PyPegen_number_token
func numberToken(p *Parser) ast.Expr {
	t := p.ExpectToken(token.NUMBER)
	if t == nil {
		return nil
	}
	raw := string(t.Bytes)
	if p.featureVersion < 6 && strings.ContainsRune(raw, '_') {
		p.errorIndicator = true
		return nil
	}
	v, ok := parseNumberLiteral(raw)
	if !ok {
		p.errorIndicator = true
		return nil
	}
	return &ast.Constant{
		Value: v,
		Pos: ast.Pos{
			Lineno:       t.Lineno,
			ColOffset:    t.ColOff,
			EndLineno:    t.EndLine,
			EndColOffset: t.EndCol,
		},
	}
}
