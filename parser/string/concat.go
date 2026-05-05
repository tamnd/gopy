// CPython: Parser/string_parser.c. Implicit concatenation of
// adjacent string literals at parse time. The parser already
// produced one Result per literal; this file folds them. Mixing a
// bytes Result with a non-bytes one is a SyntaxError, matching the
// fold-then-report pattern in _PyPegen_concatenate_strings.

package string

import (
	"fmt"
	"strings"
)

// Concat folds a non-empty slice of literal results emitted by
// adjacent string tokens. The bytes/str sides cannot mix; if they
// do, Concat returns the CPython SyntaxError text "cannot mix bytes
// and nonbytes literals".
//
// CPython: Parser/string_parser.c:21 _PyPegen_concatenate_strings
func Concat(parts []Result) (Result, error) {
	if len(parts) == 0 {
		return Result{}, fmt.Errorf("concat of zero string parts")
	}
	if len(parts) == 1 {
		return parts[0], nil
	}
	bytesMode := parts[0].IsBytes
	for _, p := range parts[1:] {
		if p.IsBytes != bytesMode {
			return Result{}, fmt.Errorf("cannot mix bytes and nonbytes literals")
		}
	}
	if bytesMode {
		var out []byte
		for _, p := range parts {
			out = append(out, p.Bytes...)
		}
		return Result{Bytes: out, IsBytes: true}, nil
	}
	var sb strings.Builder
	for _, p := range parts {
		sb.WriteString(p.Text)
	}
	return Result{Text: sb.String()}, nil
}
