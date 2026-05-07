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
// cookie); when a non-UTF-8 cookie is found the driver slurps the
// whole stream and decodes it via the codecs registry, then falls
// back to the in-memory pipeline.
//
// CPython: Parser/tokenizer/file_tokenizer.c:31 _PyTokenizer_FromFile
func FromReader(r io.Reader, mode Mode) *State {
	br := bufio.NewReader(r)
	if head, ok := readEncodingHead(br); ok {
		// Non-UTF-8 cookie: read the rest of the stream and route
		// through FromBytes so codec decoding runs on the full body.
		// CPython: Parser/tokenizer/file_tokenizer.c:288 check_bom +
		// check_coding_spec rewind path.
		rest, _ := io.ReadAll(br)
		head = append(head, rest...)
		return FromBytes(head, mode)
	}
	s := newState()
	s.mode = mode
	s.lineno = 0
	s.firstLine = 1
	s.col = 0
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

// readEncodingHead peeks the first two physical lines from br and
// reports whether a non-UTF-8 PEP 263 cookie is present. When ok is
// true, the returned bytes hold the unread head so the caller can
// stitch them back in front of the remainder. When ok is false, br is
// untouched (or only the bytes returned were consumed; callers using
// the streaming path read past them via the same bufio reader, which
// is why the head bytes are read with Read rather than Peek).
//
// CPython: Parser/tokenizer/file_tokenizer.c:288 check_bom and L337
// check_coding_spec on the first two lines.
func readEncodingHead(br *bufio.Reader) ([]byte, bool) {
	const peekSize = 2 * codingCookieMax
	peek, _ := br.Peek(peekSize)
	scan := peek
	if len(scan) >= 3 && scan[0] == 0xef && scan[1] == 0xbb && scan[2] == 0xbf {
		// BOM forces UTF-8: skip cookie-driven decoding entirely.
		return nil, false
	}
	name := DetectEncodingCookie(scan)
	if name == "" || isUTF8Name(name) {
		return nil, false
	}
	// Consume the peeked bytes so the caller can append the remainder.
	head := make([]byte, len(peek))
	n, _ := io.ReadFull(br, head)
	return head[:n], true
}
