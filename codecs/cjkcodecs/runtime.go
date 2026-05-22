// Synchronous encode / decode runtime for the cjkcodecs subsystem.
// This is the gopy port of the encode / decode outer loops and error
// dispatch helpers from CPython Modules/cjkcodecs/multibytecodec.c
// (lines 219..505 plus the synchronous entrypoints at 506..734).
//
// Each per-codec encode / decode function below mirrors the
// MultibyteCodec callback contract: it consumes one logical unit
// (one Unicode codepoint for encode, one byte cluster for decode),
// appends to the output buffer, and returns a positive error length
// or one of the MBERR_* sentinels. The outer loops in
// runDecode / runEncode walk the input and call the registered codec
// error handler when the callback signals trouble.
//
// CPython: Modules/cjkcodecs/multibytecodec.c:404 multibytecodec_decerror
// CPython: Modules/cjkcodecs/multibytecodec.c:507 multibytecodec_encode
// CPython: Modules/cjkcodecs/multibytecodec.c:672 MultibyteCodec.decode
package cjkcodecs

import (
	"strings"
	"unicode/utf8"

	"github.com/tamnd/gopy/codecs"
)

// decodeFunc consumes one byte cluster from in and appends the
// decoded codepoint(s) to w. It returns the number of bytes consumed
// on success, or a negative MBERR_* sentinel / positive sequence
// length on failure.
//
// CPython: Modules/cjkcodecs/multibytecodec.h:47 mbdecode_func
type decodeFunc func(state *codecState, in []byte, w *unicodeWriter) int

// encodeFunc consumes one codepoint cluster from input starting at
// inpos. It writes the encoded byte sequence to out and returns the
// number of input runes consumed, or a negative MBERR_* sentinel /
// positive sequence length on failure.
//
// CPython: Modules/cjkcodecs/multibytecodec.h:36 mbencode_func
type encodeFunc func(state *codecState, input []rune, inpos int, out *encodeBuffer, flags int) int

// codecState is the per-call state slab. Stateless codecs (the
// majority) ignore it. Per CPython's MultibyteCodec_State (8 bytes).
//
// CPython: Modules/cjkcodecs/multibytecodec.h:28 MultibyteCodec_State
type codecState struct {
	config int
}

// unicodeWriter accumulates decoded runes into a strings.Builder. It
// stands in for _PyUnicodeWriter; the runtime only ever appends.
//
// CPython: Modules/cjkcodecs/cjkcodecs.h:156 OUTCHAR
type unicodeWriter struct {
	b strings.Builder
}

func (w *unicodeWriter) writeRune(r rune) {
	w.b.WriteRune(r)
}

// encodeBuffer appends bytes for the encoder path.
type encodeBuffer struct {
	buf []byte
}

func (e *encodeBuffer) writeByte(b byte) {
	e.buf = append(e.buf, b)
}

func (e *encodeBuffer) writeBytes(bs ...byte) {
	e.buf = append(e.buf, bs...)
}

// runDecode runs the synchronous decode outer loop.
// Equivalent to _multibytecodec_MultibyteCodec_decode_impl
// (multibytecodec.c:672) plus the multibytecodec_decerror dispatch
// (multibytecodec.c:404).
func runDecode(encoding string, fn decodeFunc, config int, in []byte, errors string) (string, int, error) {
	if errors == "" {
		errors = "strict"
	}
	st := &codecState{config: config}
	w := &unicodeWriter{}
	pos := 0
	for pos < len(in) {
		consumed := fn(st, in[pos:], w)
		if consumed > 0 {
			pos += consumed
			continue
		}
		// Either MBERR_* (negative) or 0 means we made no progress
		// and the runtime has to take over.
		esize, reason, fatal := decErrorClassify(consumed, in[pos:])
		if fatal != nil {
			return "", pos, fatal
		}
		newpos, err := callDecodeError(encoding, errors, reason, in, pos, pos+esize, w)
		if err != nil {
			return "", pos, err
		}
		pos = newpos
	}
	out := w.b.String()
	return out, len(in), nil
}

// runEncode runs the synchronous encode outer loop.
// Equivalent to multibytecodec_encode (multibytecodec.c:507).
func runEncode(encoding string, fn encodeFunc, config int, input string, errors string) ([]byte, int, error) {
	if errors == "" {
		errors = "strict"
	}
	st := &codecState{config: config}
	runes := []rune(input)
	buf := &encodeBuffer{}
	pos := 0
	for pos < len(runes) {
		consumed := fn(st, runes, pos, buf, MBENC_FLUSH)
		if consumed > 0 {
			pos += consumed
			continue
		}
		esize, reason, fatal := encErrorClassify(consumed, runes[pos:])
		if fatal != nil {
			return nil, pos, fatal
		}
		newpos, err := callEncodeError(encoding, errors, reason, runes, pos, pos+esize, buf)
		if err != nil {
			return nil, pos, err
		}
		pos = newpos
	}
	return buf.buf, len(buf.buf), nil
}

// decErrorClassify turns a codec callback return into an error size
// + reason string, matching the switch in multibytecodec_decerror
// (multibytecodec.c:414).
func decErrorClassify(r int, remain []byte) (esize int, reason string, fatal error) {
	if r > 0 {
		return r, "illegal multibyte sequence", nil
	}
	switch r {
	case MBERR_TOOFEW:
		return len(remain), "incomplete multibyte sequence", nil
	case MBERR_INTERNAL:
		return 0, "", errInternal
	case MBERR_EXCEPTION:
		return 0, "", errException
	case MBERR_TOOSMALL:
		// retry; outer loop will spin forever. Treat as illegal.
		return 1, "illegal multibyte sequence", nil
	default:
		return 0, "", errUnknown
	}
}

func encErrorClassify(r int, remain []rune) (esize int, reason string, fatal error) {
	if r > 0 {
		return r, "illegal multibyte sequence", nil
	}
	switch r {
	case MBERR_TOOFEW:
		return len(remain), "incomplete multibyte sequence", nil
	case MBERR_INTERNAL:
		return 0, "", errInternal
	case MBERR_EXCEPTION:
		return 0, "", errException
	case MBERR_TOOSMALL:
		return 1, "illegal multibyte sequence", nil
	default:
		return 0, "", errUnknown
	}
}

// callDecodeError invokes the registered codec error handler and
// appends the returned replacement string to w. Matches the
// post-error-handler block in multibytecodec_decerror (line 485).
func callDecodeError(encoding, errors, reason string, full []byte, start, end int, w *unicodeWriter) (int, error) {
	handler, err := codecs.LookupError(errors)
	if err != nil {
		return start, err
	}
	rep, newpos, err := handler(encoding, reason, full, start, end)
	if err != nil {
		return start, err
	}
	w.b.WriteString(rep)
	if newpos < 0 {
		newpos += len(full)
	}
	if newpos < 0 || newpos > len(full) {
		return start, errOutOfBounds
	}
	return newpos, nil
}

// callEncodeError invokes the registered codec error handler for the
// encode path. The handler receives the UTF-8 bytes of the offending
// slice so a future surrogateescape-aware handler can inspect them;
// gopy's encode signature flattens runes to UTF-8 for that call.
func callEncodeError(encoding, errors, reason string, runes []rune, start, end int, buf *encodeBuffer) (int, error) {
	handler, err := codecs.LookupError(errors)
	if err != nil {
		return start, err
	}
	full := []byte(string(runes))
	// Map rune indices to byte indices for the handler's input slice.
	startByte := utf8Offset(runes, start)
	endByte := utf8Offset(runes, end)
	rep, _, err := handler(encoding, reason, full, startByte, endByte)
	if err != nil {
		return start, err
	}
	buf.buf = append(buf.buf, []byte(rep)...)
	return end, nil
}

func utf8Offset(runes []rune, n int) int {
	if n <= 0 {
		return 0
	}
	if n >= len(runes) {
		return len(string(runes))
	}
	off := 0
	for i := 0; i < n; i++ {
		off += utf8.RuneLen(runes[i])
	}
	return off
}

// runtime sentinels surfaced by classify functions.
var (
	errInternal    = mkErr("RuntimeError: internal codec error")
	errException   = mkErr("RuntimeError: codec callback raised")
	errUnknown     = mkErr("RuntimeError: unknown codec error")
	errOutOfBounds = mkErr("IndexError: position from error handler out of bounds")
)

type codecError struct{ msg string }

func (e *codecError) Error() string { return e.msg }

func mkErr(s string) error { return &codecError{msg: s} }
