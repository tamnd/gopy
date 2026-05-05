// Distinct ParseString calls must be goroutine-safe. One Parser
// instance is not, but the package-level entry point hands a fresh
// State and Parser to each call so they must not share state.

package parser

import (
	"sync"
	"testing"

	"github.com/tamnd/gopy/ast"
)

func TestParseStringConcurrent(t *testing.T) {
	const goroutines = 32
	const calls = 16
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := range goroutines {
		go func(id int) {
			defer wg.Done()
			for range calls {
				mod, err := ParseString("x = 1\n", "x.py", ModeFile)
				if err != nil {
					t.Errorf("goroutine %d: %v", id, err)
					return
				}
				if _, ok := mod.(*ast.Module); !ok {
					t.Errorf("goroutine %d: got %T, want *ast.Module", id, mod)
					return
				}
			}
		}(g)
	}
	wg.Wait()
}
