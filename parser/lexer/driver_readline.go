// CPython: Parser/tokenizer/readline_tokenizer.c. REPL driver. The C
// source pulls lines from a Python callable (sys.stdin.readline by
// default); gopy takes a Go callback with the same shape.

package lexer

import "io"

// ReadlineFunc is the shape of the readline callback. Returning io.EOF
// signals end of input; any other error stops tokenisation.
type ReadlineFunc func() ([]byte, error)

// FromReadline builds a State whose underflow callback pulls one line
// per refill from rl. The interactive prompt and history are out of
// scope for the lexer; the embedder owns those.
//
// CPython: Parser/tokenizer/readline_tokenizer.c:24 _PyTokenizer_FromReadline
func FromReadline(rl ReadlineFunc, mode Mode) *State {
	s := newState()
	s.mode = mode
	s.lineno = 0
	s.firstLine = 1
	s.readline = rl
	s.underflow = func(st *State) bool {
		line, err := rl()
		if err == io.EOF || len(line) == 0 {
			st.done = eEOF
			return false
		}
		if err != nil {
			st.done = eErrLine
			st.recordError(err.Error())
			return false
		}
		st.buf = append(st.buf, line...)
		st.inp = len(st.buf)
		st.end = cap(st.buf)
		return true
	}
	return s
}
