// CPython: Parser/tokenizer/file_tokenizer.c. File-backed driver.
// gopy uses io.Reader plus a bufio scan to feed lines on demand.

package lexer

import (
	"bufio"
	"io"
)

// FromReader builds a State that reads source incrementally from r.
// Lines are pulled on demand via the underflow callback. Encoding
// detection runs on the first two physical lines (BOM and PEP 263
// cookie); after that the driver assumes UTF-8.
//
// CPython: Parser/tokenizer/file_tokenizer.c:31 _PyTokenizer_FromFile
func FromReader(r io.Reader, mode Mode) *State {
	s := newState()
	s.mode = mode
	s.lineno = 0
	s.firstLine = 1
	s.col = 0
	br := bufio.NewReader(r)
	s.underflow = func(st *State) bool {
		line, err := br.ReadBytes('\n')
		if len(line) == 0 {
			st.done = eEOF
			return false
		}
		// Strip a UTF-8 BOM on the very first line.
		if st.lineno == 0 && len(line) >= 3 && line[0] == 0xef && line[1] == 0xbb && line[2] == 0xbf {
			line = line[3:]
		}
		st.buf = append(st.buf, line...)
		st.inp = len(st.buf)
		st.end = cap(st.buf)
		if err == io.EOF {
			// Last partial line landed; next refill will see len==0.
			return true
		}
		return err == nil
	}
	return s
}
