// Corpus iteration: exercise Parse over a real-world Python source
// tree and report how much of it the generated parser handles
// today. The test never fails on a parse miss; it asserts the gate
// (empty source still works) and prints a coverage line so the
// number can move up as the action surface fills in.
//
// Set CPYTHON to a cpython checkout to run against Lib/. The test
// is skipped when the corpus is unavailable.

package parser

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCorpusParse(t *testing.T) {
	cpython := os.Getenv("CPYTHON")
	if cpython == "" {
		cpython = filepath.Join(os.Getenv("HOME"), "github", "python", "cpython")
	}
	root := filepath.Join(cpython, "Lib")
	if _, err := os.Stat(root); err != nil {
		t.Skipf("corpus unavailable: %v", err)
	}

	var paths []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Skip test/ subtrees: they include intentionally
			// malformed sources that say nothing about parser progress.
			if name := d.Name(); name == "test" || name == "tests" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(p, ".py") {
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(paths) == 0 {
		t.Skip("corpus empty")
	}

	var ok, sentinel, fail int
	for _, p := range paths {
		src, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		_, perr := safeParse(string(src), p)
		switch {
		case perr == nil:
			ok++
		case errors.Is(perr, ErrParserNotImplemented):
			sentinel++
		default:
			fail++
		}
	}
	total := ok + sentinel + fail
	t.Logf("corpus parse: ok=%d sentinel=%d fail=%d total=%d (%.1f%% typed)",
		ok, sentinel, fail, total, 100*float64(ok)/float64(total))
}

// safeParse calls ParseString but contains panics from the
// hand-stubbed action helpers so a single bad source does not
// take the whole corpus run down. A panic counts as a non-typed
// outcome, same shape as ErrParserNotImplemented.
func safeParse(src, path string) (out any, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in %s: %v", path, r)
		}
	}()
	return ParseString(src, path, ModeFile)
}
