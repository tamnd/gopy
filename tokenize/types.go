// Package tokenize declares the token kind constants and the public
// iterator surface.
//
// Numeric values for the constants live in types_gen.go, generated
// from cpython/Grammar/Tokens via tools/tokens_go. Keeping the type
// itself in a hand-written file lets other files in the package
// depend on `Type` without requiring the generator to have run yet.
//
// CPython: Include/internal/pycore_token.h Token enum
package tokenize

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
