package initconfig

// shortOpts is the option string CPython's _PyOS_GetOpt accepts.
// A trailing colon means the option takes an argument.
//
// CPython: Python/getopt.c:40 SHORT_OPTS
const shortOpts = "bBc:dEhiIm:OPqRsStuvVW:xX:?"

// longOption is one row of CPython's longopts table. HasArg=true
// means --name takes an argument; Val is the synthetic int the
// switch in config_parse_cmdline keys on (small ints are reserved
// for long options, ASCII characters for short options).
//
// CPython: Python/getopt.c:42 longopts
type longOption struct {
	Name   string
	HasArg bool
	Val    int
}

// longOpts mirrors the CPython long-options table.
//
// CPython: Python/getopt.c:42 longopts
var longOpts = []longOption{
	{"check-hash-based-pycs", true, 0},
	{"help-all", false, 1},
	{"help-env", false, 2},
	{"help-xoptions", false, 3},
}

// optEOF is returned by getOpt when there are no more options.
const optEOF = -1

// optBad is returned by getOpt when the option is malformed (unknown,
// missing argument, unrecognized long option). Mirrors the '_' return
// in CPython's _PyOS_GetOpt.
const optBad = '_'

// getoptState carries the iteration cursor _PyOS_GetOpt threads
// through repeated calls. Mirrors the file-level _PyOS_optind /
// _PyOS_optarg / opt_ptr state in CPython, but scoped to a struct so
// gopy stays goroutine-safe.
//
// CPython: Python/getopt.c:36 opt_ptr, Include/internal/pycore_getopt.h
type getoptState struct {
	argv   []string
	optInd int
	optArg string
	ptr    string
}

// newGetopt returns a fresh state ready to walk argv. argv[0] is the
// program name and is skipped (optInd starts at 1) to match CPython's
// _PyOS_ResetGetOpt + _PyOS_GetOpt entry contract.
func newGetopt(argv []string) *getoptState {
	return &getoptState{argv: argv, optInd: 1}
}

// next advances the iterator. Returns the option byte, or optEOF /
// optBad on terminal conditions. Long options return their Val; the
// caller reads the long-option index via longIndex.
//
// CPython: Python/getopt.c:60 _PyOS_GetOpt
func (s *getoptState) next() (opt int, longIndex int) {
	longIndex = -1
	if s.ptr == "" {
		if s.optInd >= len(s.argv) {
			return optEOF, longIndex
		}
		cur := s.argv[s.optInd]
		if cur == "" || cur[0] != '-' || cur == "-" {
			return optEOF, longIndex
		}
		if cur == "--" {
			s.optInd++
			return optEOF, longIndex
		}
		if cur == "--help" {
			s.optInd++
			return 'h', longIndex
		}
		if cur == "--version" {
			s.optInd++
			return 'V', longIndex
		}
		s.ptr = cur[1:]
		s.optInd++
	}

	option := s.ptr[0]
	s.ptr = s.ptr[1:]

	if option == '-' {
		if s.ptr == "" {
			return optBad, longIndex
		}
		name := s.ptr
		s.ptr = ""
		idx := -1
		for i, opt := range longOpts {
			if opt.Name == name {
				idx = i
				break
			}
		}
		if idx == -1 {
			return optBad, longIndex
		}
		longIndex = idx
		opt := longOpts[idx]
		if !opt.HasArg {
			return opt.Val, longIndex
		}
		if s.optInd >= len(s.argv) {
			return optBad, longIndex
		}
		s.optArg = s.argv[s.optInd]
		s.optInd++
		return opt.Val, longIndex
	}

	idx := indexByte(shortOpts, option)
	if idx < 0 {
		return optBad, longIndex
	}

	if idx+1 < len(shortOpts) && shortOpts[idx+1] == ':' {
		if s.ptr != "" {
			s.optArg = s.ptr
			s.ptr = ""
		} else {
			if s.optInd >= len(s.argv) {
				return optBad, longIndex
			}
			s.optArg = s.argv[s.optInd]
			s.optInd++
		}
	}

	return int(option), longIndex
}

// indexByte returns the index of c in s, or -1 if absent. Avoids the
// strings package so initconfig stays lean.
func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}
