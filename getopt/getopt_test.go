// Gate panel for the getopt port. Mirrors the cases the 1668 spec
// calls out: -c with an arg, --help long-option dispatch, and -W
// with combined short+arg.

package getopt

import (
	"bytes"
	"strings"
	"testing"
)

func newTestState() *State {
	s := New()
	s.Stderr = &bytes.Buffer{}
	return s
}

func TestParseDashCWithArgument(t *testing.T) {
	s := newTestState()
	argv := []string{"gopy", "-c", "print(1)", "foo"}
	got := s.GetOpt(argv, PythonShortOpts, PythonLongOpts)
	if got != 'c' {
		t.Fatalf("GetOpt: got %q, want 'c'", rune(got))
	}
	if s.OptArg != "print(1)" {
		t.Fatalf("OptArg: got %q, want %q", s.OptArg, "print(1)")
	}
	if s.OptInd != 3 {
		t.Fatalf("OptInd: got %d, want 3 (so foo is next non-option)", s.OptInd)
	}

	if next := s.GetOpt(argv, PythonShortOpts, PythonLongOpts); next != EOF {
		t.Fatalf("GetOpt after -c: got %q, want EOF", rune(next))
	}
	if argv[s.OptInd] != "foo" {
		t.Fatalf("argv[OptInd]=%q, want 'foo'", argv[s.OptInd])
	}
}

func TestParseLongHelp(t *testing.T) {
	s := newTestState()
	argv := []string{"gopy", "--help"}
	got := s.GetOpt(argv, PythonShortOpts, PythonLongOpts)
	if got != 'h' {
		t.Fatalf("GetOpt: got %q, want 'h'", rune(got))
	}
	if s.OptInd != 2 {
		t.Fatalf("OptInd: got %d, want 2", s.OptInd)
	}
}

func TestParseLongHelpAll(t *testing.T) {
	s := newTestState()
	argv := []string{"gopy", "--help-all"}
	got := s.GetOpt(argv, PythonShortOpts, PythonLongOpts)
	if got != 1 {
		t.Fatalf("GetOpt: got %d, want 1 (--help-all val)", got)
	}
	if s.LongIndex != 1 {
		t.Fatalf("LongIndex: got %d, want 1", s.LongIndex)
	}
}

func TestParseDashWWithArgument(t *testing.T) {
	s := newTestState()
	argv := []string{"gopy", "-W", "ignore::DeprecationWarning"}
	got := s.GetOpt(argv, PythonShortOpts, PythonLongOpts)
	if got != 'W' {
		t.Fatalf("GetOpt: got %q, want 'W'", rune(got))
	}
	if s.OptArg != "ignore::DeprecationWarning" {
		t.Fatalf("OptArg: got %q, want %q", s.OptArg, "ignore::DeprecationWarning")
	}
	if s.OptInd != 3 {
		t.Fatalf("OptInd: got %d, want 3", s.OptInd)
	}
}

func TestClusteredShortOptions(t *testing.T) {
	s := newTestState()
	argv := []string{"gopy", "-bB"}
	if got := s.GetOpt(argv, PythonShortOpts, PythonLongOpts); got != 'b' {
		t.Fatalf("first: got %q, want 'b'", rune(got))
	}
	if got := s.GetOpt(argv, PythonShortOpts, PythonLongOpts); got != 'B' {
		t.Fatalf("second: got %q, want 'B'", rune(got))
	}
	if got := s.GetOpt(argv, PythonShortOpts, PythonLongOpts); got != EOF {
		t.Fatalf("third: got %q, want EOF", rune(got))
	}
}

func TestShortOptionGluedArg(t *testing.T) {
	s := newTestState()
	argv := []string{"gopy", "-cprint(1)"}
	got := s.GetOpt(argv, PythonShortOpts, PythonLongOpts)
	if got != 'c' {
		t.Fatalf("GetOpt: got %q, want 'c'", rune(got))
	}
	if s.OptArg != "print(1)" {
		t.Fatalf("OptArg: got %q, want %q", s.OptArg, "print(1)")
	}
}

func TestUnknownShortOption(t *testing.T) {
	buf := &bytes.Buffer{}
	s := New()
	s.Stderr = buf
	got := s.GetOpt([]string{"gopy", "-z"}, PythonShortOpts, PythonLongOpts)
	if got != ErrorMark {
		t.Fatalf("GetOpt: got %d, want ErrorMark", got)
	}
	if !strings.Contains(buf.String(), "Unknown option: -z") {
		t.Fatalf("stderr=%q, want to contain Unknown option", buf.String())
	}
}

func TestMissingArgument(t *testing.T) {
	buf := &bytes.Buffer{}
	s := New()
	s.Stderr = buf
	got := s.GetOpt([]string{"gopy", "-c"}, PythonShortOpts, PythonLongOpts)
	if got != ErrorMark {
		t.Fatalf("GetOpt: got %d, want ErrorMark", got)
	}
	if !strings.Contains(buf.String(), "Argument expected for the -c option") {
		t.Fatalf("stderr=%q, want missing-arg message", buf.String())
	}
}

func TestDoubleDashTerminator(t *testing.T) {
	s := newTestState()
	argv := []string{"gopy", "-b", "--", "-x"}
	if got := s.GetOpt(argv, PythonShortOpts, PythonLongOpts); got != 'b' {
		t.Fatalf("first: got %q, want 'b'", rune(got))
	}
	if got := s.GetOpt(argv, PythonShortOpts, PythonLongOpts); got != EOF {
		t.Fatalf("after --: got %q, want EOF", rune(got))
	}
	if argv[s.OptInd] != "-x" {
		t.Fatalf("argv[OptInd]=%q, want -x", argv[s.OptInd])
	}
}

func TestLoneDashIsNotAnOption(t *testing.T) {
	s := newTestState()
	argv := []string{"gopy", "-"}
	if got := s.GetOpt(argv, PythonShortOpts, PythonLongOpts); got != EOF {
		t.Fatalf("got %q, want EOF for lone dash", rune(got))
	}
	if s.OptInd != 1 {
		t.Fatalf("OptInd: got %d, want 1 (lone dash should not advance)", s.OptInd)
	}
}

func TestLongOptionUnknown(t *testing.T) {
	buf := &bytes.Buffer{}
	s := New()
	s.Stderr = buf
	got := s.GetOpt([]string{"gopy", "--nonsense"}, PythonShortOpts, PythonLongOpts)
	if got != ErrorMark {
		t.Fatalf("got %d, want ErrorMark", got)
	}
	if !strings.Contains(buf.String(), "Unknown option: --nonsense") {
		t.Fatalf("stderr=%q, want unknown-option message", buf.String())
	}
}

func TestVersionLongOption(t *testing.T) {
	s := newTestState()
	got := s.GetOpt([]string{"gopy", "--version"}, PythonShortOpts, PythonLongOpts)
	if got != 'V' {
		t.Fatalf("got %q, want 'V'", rune(got))
	}
}

func TestCheckHashBasedPycsTakesArg(t *testing.T) {
	s := newTestState()
	argv := []string{"gopy", "--check-hash-based-pycs", "always"}
	got := s.GetOpt(argv, PythonShortOpts, PythonLongOpts)
	if got != 0 {
		t.Fatalf("got %d, want 0 (--check-hash-based-pycs val)", got)
	}
	if s.OptArg != "always" {
		t.Fatalf("OptArg: got %q, want 'always'", s.OptArg)
	}
}
