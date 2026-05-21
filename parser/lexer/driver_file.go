// CPython: Parser/tokenizer/file_tokenizer.c. File-backed driver.
//
// Function map (file_tokenizer.c → gopy):
//
//	fp_getc                              → bufio.Reader.ReadByte (in underflow)
//	fp_ungetc                            → bufio.Reader.UnreadByte
//	tok_underflow_file                   → State.underflow closure
//	tok_readline_recode                  → readEncodingHead + codecs.Decode
//	check_bom                            → source.go CheckBOMCookieConflict
//	check_coding_spec                    → source.go DetectEncodingCookie
//	_PyTokenizer_FromFile                → FromReader
//	_PyTokenizer_FindEncodingFilename    → FindEncodingFilename

package lexer

import (
	"bufio"
	"io"
	"os"
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
	br := bufio.NewReaderSize(r, 2*cookieFileBufsize)
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
		// The default encoding is UTF-8, so each line must validate.
		// CPython runs ensure_utf8 here when tok->encoding is NULL.
		// Lineno is one past the count we have already ingested; this
		// matches CPython's tok->lineno tracking after a successful
		// refill.
		//
		// CPython: Parser/tokenizer/file_tokenizer.c:352 ensure_utf8
		if st.encoding == "" {
			if vLine, bad, ok := ValidateUTF8(line); !ok {
				reportedLine := st.lineno + vLine
				st.lineno = reportedLine
				st.recordErrorWithText(
					nonUTF8ErrorMessage(bad, reportedLine),
					string(trimEOL(line)),
				)
				st.done = eEncoding
				return false
			}
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

// trimEOL strips a single trailing \n or \r\n so the SyntaxError text
// matches what the user sees in their editor (CPython's source-line
// extractor returns the line without terminator).
func trimEOL(line []byte) []byte {
	if n := len(line); n > 0 && line[n-1] == '\n' {
		line = line[:n-1]
		if n > 1 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}
	}
	return line
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
	// CPython reads the first two source lines through fp_getc, which
	// can grow tok->buf past BUFSIZ when a single line exceeds it.
	// Peeking 2*BUFSIZ here covers the common case of two long header
	// lines without forcing the caller to slurp the whole file just
	// to find the cookie. The Bytes-mode driver (FromBytes) has the
	// full source in memory and is unbounded.
	const peekSize = 2 * cookieFileBufsize
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

// FindEncodingFilename reads filename's first two physical lines and
// reports the PEP 263 source encoding. Mirrors the C entry point that
// powers `python -m tokenize -e`: read up to two newline-terminated
// segments, run them through check_bom + check_coding_spec, return
// "utf-8" by default. The function does not consume the file beyond
// the cookie window.
//
// CPython: Parser/tokenizer/file_tokenizer.c:449 _PyTokenizer_FindEncodingFilename
func FindEncodingFilename(name string) (string, error) {
	f, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	br := bufio.NewReader(f)
	peek, _ := br.Peek(2 * cookieFileBufsize)
	if conflict := CheckBOMCookieConflict(peek); conflict != "" {
		return "", &SyntaxError{Message: conflict}
	}
	if name := DetectEncodingCookie(peek); name != "" {
		return name, nil
	}
	return "utf-8", nil
}
