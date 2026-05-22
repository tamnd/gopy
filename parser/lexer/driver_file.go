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
	"bytes"
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
	head, cookie := readEncodingHead(r)
	if cookie != "" {
		// Non-UTF-8 cookie: read the rest of the stream and route
		// through FromBytes so codec decoding runs on the full body.
		// CPython: Parser/tokenizer/file_tokenizer.c:288 check_bom +
		// check_coding_spec rewind path.
		rest, _ := io.ReadAll(r)
		return FromBytes(append(head, rest...), mode)
	}
	// UTF-8 path: splice the head bytes back in front of the
	// remaining stream so the underflow callback sees the file from
	// byte zero.
	br := bufio.NewReaderSize(io.MultiReader(bytes.NewReader(head), r), 2*cookieFileBufsize)
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

// readEncodingHead reads the first two physical lines from r and
// returns those bytes plus the detected non-UTF-8 cookie name, or
// the empty string when the source is UTF-8 (either by default, by
// BOM, or by a cookie that names utf-8). Mirrors CPython's
// STATE_INIT branch of tok_underflow_file, which reads line-by-line
// via fp_getc until check_bom + check_coding_spec resolve. The
// returned bytes always cover the head consumed from r, so the
// caller can stitch them back in front of the remainder for the
// streaming path.
//
// CPython: Parser/tokenizer/file_tokenizer.c:285 tok_underflow_file
// (STATE_INIT branch, lines 287..344)
func readEncodingHead(r io.Reader) ([]byte, string) {
	head := readFirstTwoLines(r)
	if len(head) >= 3 && head[0] == 0xef && head[1] == 0xbb && head[2] == 0xbf {
		// BOM forces UTF-8: skip cookie-driven decoding entirely.
		return head, ""
	}
	name := DetectEncodingCookie(head)
	if name == "" || isUTF8Name(name) {
		return head, ""
	}
	return head, name
}

// readFirstTwoLines drains r up to the second '\n' or EOF, whichever
// comes first. Unlike the original peek-based scanner this has no
// fixed buffer cap, so cookies on arbitrarily long header lines are
// resolved.
func readFirstTwoLines(r io.Reader) []byte {
	br := bufio.NewReaderSize(r, 4096)
	head := make([]byte, 0, 2*cookieFileBufsize)
	for newlines := 0; newlines < 2; {
		line, err := br.ReadBytes('\n')
		head = append(head, line...)
		if len(line) > 0 && line[len(line)-1] == '\n' {
			newlines++
		}
		if err != nil {
			break
		}
	}
	// Drain anything bufio buffered past the second newline so the
	// caller's MultiReader doesn't double-read.
	if n := br.Buffered(); n > 0 {
		buf := make([]byte, n)
		_, _ = io.ReadFull(br, buf)
		head = append(head, buf...)
	}
	return head
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
