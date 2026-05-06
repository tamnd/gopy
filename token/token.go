// Package token declares the token kind constants shared by the
// lexer and tokenize packages.
//
// Mirrors CPython's split between the C tokenizer's enum (in
// Include/internal/pycore_token.h) and the Python-level Lib/token.py
// re-export. Keeping the kinds in their own package lets the lexer
// produce them and the tokenize wrapper consume them without an
// import cycle.
//
// CPython: Include/internal/pycore_token.h Token enum
// CPython: Lib/token.py
package token

// Type is the token kind. Numeric values match CPython 3.14
// Grammar/Tokens one for one. The full constant set is in
// types_gen.go.
type Type int

// String returns the CPython-compatible token name (e.g. "NAME",
// "NUMBER"). Unknown values render as "TYPE(n)".
//
// CPython: Lib/token.py tok_name
func (t Type) String() string {
	if int(t) >= 0 && int(t) < len(tokenNames) && tokenNames[t] != "" {
		return tokenNames[t]
	}
	return "TYPE(" + itoa(int(t)) + ")"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
