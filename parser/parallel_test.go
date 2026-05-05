// Spec 1642 surface guarantee: distinct ParseString calls are
// goroutine-safe. One Parser instance is not, but the package-level
// entry point hands a fresh State and Parser to each call so they
// must not share state.

package parser

import (
	"errors"
	"sync"
	"testing"
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
				_, err := ParseString("x = 1\n", "x.py", ModeFile)
				if !errors.Is(err, ErrParserNotImplemented) {
					t.Errorf("goroutine %d: err = %v, want sentinel", id, err)
					return
				}
			}
		}(g)
	}
	wg.Wait()
}
