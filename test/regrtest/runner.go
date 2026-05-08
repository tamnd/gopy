package regrtest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Outcome buckets the result of running a single test entry.
type Outcome string

const (
	// OutcomePass means the gopy binary exited 0 against the entry.
	OutcomePass Outcome = "pass"
	// OutcomeFail means gopy ran but exited non-zero (test assertion
	// failed, raised exception, etc.).
	OutcomeFail Outcome = "fail"
	// OutcomeSkip means the manifest carries a deferred or
	// out-of-scope marker for this entry; the runner does not invoke
	// gopy.
	OutcomeSkip Outcome = "skip"
	// OutcomeMissing means the entry is in the manifest but the test
	// file (or directory) is absent from the corpus.
	OutcomeMissing Outcome = "missing"
	// OutcomeTimeout means the gopy invocation hit the per-test
	// deadline and was killed.
	OutcomeTimeout Outcome = "timeout"
	// OutcomeError means the harness itself failed (binary missing,
	// fork failure, etc.) - not a test failure.
	OutcomeError Outcome = "error"
)

// Result carries one entry's outcome plus enough output for the
// caller to print a useful failure summary.
type Result struct {
	Entry    Entry
	Outcome  Outcome
	ExitCode int
	Stdout   string
	Stderr   string
	Err      error
	Duration time.Duration
}

// Runner invokes the gopy binary against entries in a corpus.
//
// Binary is the absolute path to a built gopy binary, or any command
// callable via exec. Corpus is the directory holding the vendored
// CPython tests (test/cpython/ in this repo). Manifest is the parsed
// triage table; a non-nil Manifest is required so the runner can map
// entry name -> on-disk path and skip deferred / out-of-scope rows.
type Runner struct {
	Binary   string
	Corpus   string
	Manifest *Manifest
	Timeout  time.Duration
}

// Run drives one manifest entry through the gopy binary. Skip /
// missing decisions are made before exec; otherwise the binary is
// called as `<Binary> <corpus>/<entry.Name>` for a file entry, or
// against the package's __main__-equivalent for a directory entry.
//
// Directory entries are not yet supported (CPython's regrtest invokes
// `python -m unittest <package>`; gopy's import side has to land
// before that works) and surface as OutcomeError.
func (r *Runner) Run(ctx context.Context, e Entry) Result {
	res := Result{Entry: e}

	switch e.Status {
	case StatusDeferred, StatusOutOfScope:
		res.Outcome = OutcomeSkip
		return res
	}

	if r.Binary == "" {
		res.Outcome = OutcomeError
		res.Err = fmt.Errorf("regrtest: Runner.Binary is empty")
		return res
	}
	if r.Corpus == "" {
		res.Outcome = OutcomeError
		res.Err = fmt.Errorf("regrtest: Runner.Corpus is empty")
		return res
	}

	path, ok := r.locate(e)
	if !ok {
		res.Outcome = OutcomeMissing
		return res
	}

	if e.IsPackage() {
		res.Outcome = OutcomeError
		res.Err = fmt.Errorf("regrtest: package entries (%s) need -m support; not yet wired", e.Name)
		return res
	}

	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, r.Binary, path)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	res.Duration = time.Since(start)
	res.Stdout = stdout.String()
	res.Stderr = stderr.String()

	switch {
	case cctx.Err() == context.DeadlineExceeded:
		res.Outcome = OutcomeTimeout
		res.Err = cctx.Err()
	case err == nil:
		res.Outcome = OutcomePass
		res.ExitCode = 0
	default:
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			res.Outcome = OutcomeFail
			res.ExitCode = exitErr.ExitCode()
		} else {
			res.Outcome = OutcomeError
			res.Err = err
		}
	}
	return res
}

// RunAll iterates the manifest in order and returns one Result per
// entry. The caller decides which subset to run by passing
// m.Filter(...) into a fresh Runner if needed.
func (r *Runner) RunAll(ctx context.Context) []Result {
	if r.Manifest == nil {
		return nil
	}
	out := make([]Result, 0, len(r.Manifest.Entries))
	for _, e := range r.Manifest.Entries {
		out = append(out, r.Run(ctx, e))
		if ctx.Err() != nil {
			break
		}
	}
	return out
}

// Summary tallies a slice of Results by outcome.
func Summary(results []Result) map[Outcome]int {
	out := make(map[Outcome]int)
	for _, r := range results {
		out[r.Outcome]++
	}
	return out
}

// locate maps an entry name to an on-disk path under r.Corpus. File
// entries resolve to "<name>.py"; package entries resolve to the
// directory itself (without the trailing slash).
func (r *Runner) locate(e Entry) (string, bool) {
	name := strings.TrimSuffix(e.Name, "/")
	if e.IsPackage() {
		path := filepath.Join(r.Corpus, name)
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return path, true
		}
		return "", false
	}
	path := filepath.Join(r.Corpus, name+".py")
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		return path, true
	}
	return "", false
}

// CountFiles counts test_*.py files actually present under the
// corpus, useful for sanity-checking against the manifest tally.
func CountFiles(corpus string) (int, error) {
	if corpus == "" {
		return 0, fmt.Errorf("regrtest: empty corpus path")
	}
	n := 0
	err := filepath.WalkDir(corpus, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		if strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py") {
			n++
		}
		return nil
	})
	return n, err
}

