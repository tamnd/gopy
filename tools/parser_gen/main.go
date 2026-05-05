// Command parser_gen generates the PEG parser table for gopy from
// cpython/Grammar/python.gram. It is the Go-targeted counterpart of
// CPython's Tools/peg_generator/.
//
// Inputs:
//   - $CPYTHON/Grammar/python.gram: the PEG grammar.
//
// Output:
//   - <repo>/parser/pegen/parser_gen.go: the rule-table struct, the
//     keyword tables, and the per-rule parse functions.
//
// Run with:
//
//	go run ./tools/parser_gen \
//	  -cpython=$HOME/github/python/cpython \
//	  -out=parser/pegen/parser_gen.go
//
// The full per-rule emitter is the next milestone. Today the
// generator extracts the rule list from python.gram and emits a
// stable scaffold: rule-id constants, a generated rule-name table,
// and a Dispatch entry point that returns ErrParserNotImplemented.
// Downstream code can program against the stable shape and the
// emitter will fill in real bodies in v0.6.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

func main() {
	var cpython, out string
	flag.StringVar(&cpython, "cpython", "", "path to a cpython 3.14 checkout")
	flag.StringVar(&out, "out", "parser/pegen/parser_gen.go", "output Go file")
	flag.Parse()
	if cpython == "" {
		log.Fatal("parser_gen: -cpython is required")
	}
	gram := filepath.Join(cpython, "Grammar", "python.gram")
	if _, err := os.Stat(gram); err != nil {
		log.Fatalf("parser_gen: cannot read %s: %v", gram, err)
	}
	data, err := os.ReadFile(gram)
	if err != nil {
		log.Fatalf("parser_gen: %v", err)
	}
	g, err := ParseGrammar(data)
	if err != nil {
		log.Fatalf("parser_gen: %v", err)
	}
	src := Emit(g)
	if err := os.WriteFile(out, []byte(src), 0o644); err != nil {
		log.Fatalf("parser_gen: %v", err)
	}
	fmt.Fprintf(os.Stderr, "parser_gen: wrote %d rules to %s\n", len(g.Rules), out)
}
