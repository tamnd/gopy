// Port of CPython's Parser/token.c operator/punctuation classifier.
// The lexer used to flatten every operator into token.OP and rely on
// the pegen layer to upgrade on intake; CPython's tokenizer instead
// emits the specific token type at emission time, which matters for
// downstream consumers like Python/Python-tokenize.c that surface the
// raw type to Python.
//
// CPython: Parser/token.c:84 _PyToken_OneChar
// CPython: Parser/token.c:115 _PyToken_TwoChars
// CPython: Parser/token.c:199 _PyToken_ThreeChars

package lexer

import "github.com/tamnd/gopy/token"

// oneCharOp maps a single operator/punctuation byte to its specific
// token type. Falls through to OP for unknown bytes (matches the
// `return OP;` default in CPython's _PyToken_OneChar).
//
// CPython: Parser/token.c:84 _PyToken_OneChar
func oneCharOp(c1 int) token.Type {
	switch c1 {
	case '!':
		return token.EXCLAMATION
	case '%':
		return token.PERCENT
	case '&':
		return token.AMPER
	case '(':
		return token.LPAR
	case ')':
		return token.RPAR
	case '*':
		return token.STAR
	case '+':
		return token.PLUS
	case ',':
		return token.COMMA
	case '-':
		return token.MINUS
	case '.':
		return token.DOT
	case '/':
		return token.SLASH
	case ':':
		return token.COLON
	case ';':
		return token.SEMI
	case '<':
		return token.LESS
	case '=':
		return token.EQUAL
	case '>':
		return token.GREATER
	case '@':
		return token.AT
	case '[':
		return token.LSQB
	case ']':
		return token.RSQB
	case '^':
		return token.CIRCUMFLEX
	case '{':
		return token.LBRACE
	case '|':
		return token.VBAR
	case '}':
		return token.RBRACE
	case '~':
		return token.TILDE
	}
	return token.OP
}

// classifyOp dispatches to threeCharOp / twoCharOp / oneCharOp based
// on how many characters the lexer consumed. It mirrors the fallback
// chain CPython performs in Parser/lexer/lexer.c when the longer
// lookup returns the generic OP sentinel.
//
// CPython: Parser/lexer/lexer.c:1395 (token type selection)
func classifyOp(c1, c2, c3 int) token.Type {
	if c3 != 0 {
		if t := threeCharOp(c1, c2, c3); t != token.OP {
			return t
		}
	}
	if c2 != 0 {
		if t := twoCharOp(c1, c2); t != token.OP {
			return t
		}
	}
	return oneCharOp(c1)
}

// twoCharOp maps a two-byte operator sequence to its specific token
// type. Returns token.OP when the pair has no specific assignment;
// the caller then falls back to the one-char emission for c1.
//
// CPython: Parser/token.c:115 _PyToken_TwoChars
func twoCharOp(c1, c2 int) token.Type {
	switch c1 {
	case '!':
		if c2 == '=' {
			return token.NOTEQUAL
		}
	case '%':
		if c2 == '=' {
			return token.PERCENTEQUAL
		}
	case '&':
		if c2 == '=' {
			return token.AMPEREQUAL
		}
	case '*':
		switch c2 {
		case '*':
			return token.DOUBLESTAR
		case '=':
			return token.STAREQUAL
		}
	case '+':
		if c2 == '=' {
			return token.PLUSEQUAL
		}
	case '-':
		switch c2 {
		case '=':
			return token.MINEQUAL
		case '>':
			return token.RARROW
		}
	case '/':
		switch c2 {
		case '/':
			return token.DOUBLESLASH
		case '=':
			return token.SLASHEQUAL
		}
	case ':':
		if c2 == '=' {
			return token.COLONEQUAL
		}
	case '<':
		switch c2 {
		case '<':
			return token.LEFTSHIFT
		case '=':
			return token.LESSEQUAL
		case '>':
			return token.NOTEQUAL
		}
	case '=':
		if c2 == '=' {
			return token.EQEQUAL
		}
	case '>':
		switch c2 {
		case '=':
			return token.GREATEREQUAL
		case '>':
			return token.RIGHTSHIFT
		}
	case '@':
		if c2 == '=' {
			return token.ATEQUAL
		}
	case '^':
		if c2 == '=' {
			return token.CIRCUMFLEXEQUAL
		}
	case '|':
		if c2 == '=' {
			return token.VBAREQUAL
		}
	}
	return token.OP
}

// threeCharOp maps a three-byte operator sequence to its specific
// token type. Returns token.OP when the triple has no specific
// assignment.
//
// CPython: Parser/token.c:199 _PyToken_ThreeChars
func threeCharOp(c1, c2, c3 int) token.Type {
	switch c1 {
	case '*':
		if c2 == '*' && c3 == '=' {
			return token.DOUBLESTAREQUAL
		}
	case '.':
		if c2 == '.' && c3 == '.' {
			return token.ELLIPSIS
		}
	case '/':
		if c2 == '/' && c3 == '=' {
			return token.DOUBLESLASHEQUAL
		}
	case '<':
		if c2 == '<' && c3 == '=' {
			return token.LEFTSHIFTEQUAL
		}
	case '>':
		if c2 == '>' && c3 == '=' {
			return token.RIGHTSHIFTEQUAL
		}
	}
	return token.OP
}
