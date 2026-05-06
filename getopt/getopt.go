// Package getopt is the gopy port of cpython/Python/getopt.c. CPython
// ships its own getopt to dodge GNU getopt's locale and global-state
// quirks; the interpreter init code (Modules/main.c, pylifecycle.c)
// calls _PyOS_GetOpt to walk argv before pyinit_core runs.
//
// The three module globals (_PyOS_opterr, _PyOS_optind, _PyOS_optarg)
// and the static opt_ptr collapse into State so the parser is
// re-entrant. argv is []string instead of wchar_t*-array because
// gopy already decoded os.Args to UTF-8 during pre-config.
//
// CPython: Python/getopt.c
package getopt

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// LongOption mirrors the static longopts entries in
// cpython/Python/getopt.c. Flag is kept for source-shape parity but
// no caller in gopy uses the flag-pointer mode (matches CPython's
// own usage where every entry sets flag=NULL).
//
// CPython: Include/internal/pycore_getopt.h _PyOS_LongOption
type LongOption struct {
	Name   string
	HasArg bool
	Flag   *int
	Val    int
}

// PythonShortOpts is the SHORT_OPTS string from getopt.c. Pinned
// here so the cmd/gopy entry point can use the exact CPython 3.14
// option set.
//
// CPython: Python/getopt.c:40 SHORT_OPTS
const PythonShortOpts = "bBc:dEhiIm:OPqRsStuvVW:xX:?"

// PythonLongOpts mirrors the longopts table in getopt.c. The
// trailing `{NULL, 0, -1}` C sentinel is implicit in Go because the
// slice carries its own length.
//
// CPython: Python/getopt.c:42 longopts
var PythonLongOpts = []LongOption{
	{Name: "check-hash-based-pycs", HasArg: true, Val: 0},
	{Name: "help-all", HasArg: false, Val: 1},
	{Name: "help-env", HasArg: false, Val: 2},
	{Name: "help-xoptions", HasArg: false, Val: 3},
}

// EOF marks "no more options". Mirrors the -1 _PyOS_GetOpt returns
// when argv is exhausted or the next argument is non-option.
//
// CPython: Python/getopt.c:67 (return -1)
const EOF = -1

// ErrorMark is the int the parser returns on a parse error. Mirrors
// the '_' sentinel _PyOS_GetOpt picks for the same.
//
// CPython: Python/getopt.c:119 (return '_')
const ErrorMark = '_'

// State carries the per-parse cursor. Each GetOpt call advances
// OptInd / OptArg / optPtr; OptErr controls whether parse errors
// print to Stderr.
//
// CPython: Python/getopt.c:32 _PyOS_opterr / 33 _PyOS_optind / 34 _PyOS_optarg / 36 opt_ptr
type State struct {
	OptInd int
	OptArg string
	OptErr bool
	// LongIndex is set to the longopts entry that matched on the
	// most recent --name return. Mirrors *longindex.
	//
	// CPython: Python/getopt.c:60 longindex parameter
	LongIndex int
	// Stderr is where parse-error messages go when OptErr is true.
	// Defaults to os.Stderr; tests can swap it for a buffer.
	Stderr io.Writer

	optPtr string
}

// New returns a State initialized the way _PyOS_ResetGetOpt leaves
// the globals: opterr=true, optind=1, optarg="", opt_ptr="".
//
// CPython: Python/getopt.c:52 _PyOS_ResetGetOpt
func New() *State {
	return &State{OptInd: 1, OptErr: true, Stderr: os.Stderr}
}

// GetOpt parses one option from argv starting at s.OptInd. Returns
// the option char (positive int), 0 when the long-option's flag was
// set, EOF at end-of-options, or ErrorMark on a parse error. argv[0]
// is treated as the program name and never inspected, matching the
// _PyOS_optind=1 starting cursor.
//
// CPython: Python/getopt.c:60 _PyOS_GetOpt
func (s *State) GetOpt(argv []string, shortOpts string, longOpts []LongOption) int {
	if r, ok := s.startWord(argv); ok {
		return r
	}
	if s.optPtr == "" {
		return EOF
	}
	option := s.optPtr[0]
	s.optPtr = s.optPtr[1:]

	if option == '-' {
		return s.parseLong(argv, longOpts)
	}
	return s.parseShort(argv, shortOpts, option)
}

// startWord advances to the next argv word and prepares optPtr to
// scan its option chars. Returns (val, true) when the caller should
// return immediately (EOF / -h / -V); (_, false) means optPtr is now
// loaded and the caller should keep parsing.
//
// CPython: Python/getopt.c:60 _PyOS_GetOpt prelude
func (s *State) startWord(argv []string) (int, bool) {
	if s.optPtr != "" {
		return 0, false
	}
	if s.OptInd >= len(argv) {
		return EOF, true
	}
	cur := argv[s.OptInd]
	if cur == "" || cur[0] != '-' || len(cur) == 1 /* lone dash */ {
		return EOF, true
	}
	switch cur {
	case "--":
		s.OptInd++
		return EOF, true
	case "--help":
		s.OptInd++
		return 'h', true
	case "--version":
		s.OptInd++
		return 'V', true
	}
	s.optPtr = cur[1:]
	s.OptInd++
	return 0, false
}

// parseLong matches optPtr against longOpts and consumes the
// argument when HasArg is set.
//
// CPython: Python/getopt.c:60 _PyOS_GetOpt long-option branch
func (s *State) parseLong(argv []string, longOpts []LongOption) int {
	if s.optPtr == "" {
		s.errorf("Expected long option\n")
		return EOF
	}
	s.LongIndex = 0
	var found *LongOption
	for i := range longOpts {
		if longOpts[i].Name == s.optPtr {
			s.LongIndex = i
			found = &longOpts[i]
			break
		}
	}
	if found == nil {
		s.errorf("Unknown option: %s\n", argv[s.OptInd-1])
		return ErrorMark
	}
	s.optPtr = ""
	if !found.HasArg {
		return found.Val
	}
	if s.OptInd >= len(argv) {
		s.errorf("Argument expected for the %s options\n", argv[s.OptInd-1])
		return ErrorMark
	}
	s.OptArg = argv[s.OptInd]
	s.OptInd++
	return found.Val
}

// parseShort handles a single short-option char including its
// argument (if shortOpts marks it with a trailing colon).
//
// CPython: Python/getopt.c:60 _PyOS_GetOpt short-option branch
func (s *State) parseShort(argv []string, shortOpts string, option byte) int {
	idx := strings.IndexByte(shortOpts, option)
	if idx < 0 {
		s.errorf("Unknown option: -%c\n", option)
		return ErrorMark
	}
	if idx+1 < len(shortOpts) && shortOpts[idx+1] == ':' {
		if s.optPtr != "" {
			s.OptArg = s.optPtr
			s.optPtr = ""
		} else {
			if s.OptInd >= len(argv) {
				s.errorf("Argument expected for the -%c option\n", option)
				return ErrorMark
			}
			s.OptArg = argv[s.OptInd]
			s.OptInd++
		}
	}
	return int(option)
}

func (s *State) errorf(format string, args ...any) {
	if !s.OptErr {
		return
	}
	w := s.Stderr
	if w == nil {
		w = os.Stderr
	}
	fmt.Fprintf(w, format, args...)
}
