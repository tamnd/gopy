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
	"fmt"
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

// encodeResetFunc emits any trailing bytes a stateful encoder needs
// before the output is closed. Matches mbcodecreset_func.
//
// CPython: Modules/cjkcodecs/multibytecodec.h:42 mbcodecreset_func
type encodeResetFunc func(state *codecState, out *encodeBuffer)

// codecState is the per-call state slab. Stateless codecs (the
// majority) ignore it. Per CPython's MultibyteCodec_State (8 bytes
// of free-form state plus the codec config flag we use for the
// jisx0213 2000 / 2004 split).
//
// CPython: Modules/cjkcodecs/multibytecodec.h:28 MultibyteCodec_State
type codecState struct {
	config int
	cBytes [8]uint8
}

// unicodeWriter accumulates decoded runes into a strings.Builder. It
// stands in for _PyUnicodeWriter; the runtime only ever appends.
//
// stateAdv is set by stateful decoders (ISO-2022) before returning a
// positive consumed count when the bytes were a legitimate state-change
// escape sequence that produced no character output. The outer loop
// treats consumed==stateAdv as a successful advance even with no chars
// written.
//
// CPython: Modules/cjkcodecs/cjkcodecs.h:156 OUTCHAR
type unicodeWriter struct {
	b        strings.Builder
	stateAdv int
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
		prevOutLen := w.b.Len()
		w.stateAdv = 0
		consumed := fn(st, in[pos:], w)
		if consumed > 0 && (w.b.Len() > prevOutLen || w.stateAdv == consumed) {
			pos += consumed
			continue
		}
		// consumed > 0 but no chars written and no stateAdv = invalid sequence.
		// negative or 0 = MBERR_* sentinel.
		var esize int
		var reason string
		if consumed > 0 {
			esize = consumed
			reason = "illegal multibyte sequence"
		} else {
			var fatal error
			esize, reason, fatal = decErrorClassify(consumed, in[pos:])
			if fatal != nil {
				return "", pos, fatal
			}
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

// runDecodeIncremental is like runDecode but, when final=false and the
// codec signals MBERR_TOOFEW at the end of input, holds back those bytes
// rather than calling the error handler. The caller (MultibyteIncrementalDecoder)
// prepends them to the next chunk.
//
// CPython: Modules/cjkcodecs/multibytecodec.c:803 mbstreamreader_iread
func runDecodeIncremental(encoding string, fn decodeFunc, config int, in []byte, errors string, final bool) (string, []byte, error) {
	if errors == "" {
		errors = "strict"
	}
	st := &codecState{config: config}
	return runDecodeIncrementalWithState(encoding, fn, st, in, errors, final)
}

// runDecodeIncrementalWithState is runDecodeIncremental with an external
// codec state. Stateful codecs (HZ, ISO-2022) use this to preserve shift /
// escape state across successive decode() calls on the same instance.
//
// CPython: Modules/cjkcodecs/multibytecodec.c:887 mbincrdecoder_decode
func runDecodeIncrementalWithState(encoding string, fn decodeFunc, st *codecState, in []byte, errors string, final bool) (string, []byte, error) {
	if errors == "" {
		errors = "strict"
	}
	w := &unicodeWriter{}
	pos := 0
	for pos < len(in) {
		prevOutLen := w.b.Len()
		w.stateAdv = 0
		consumed := fn(st, in[pos:], w)
		if consumed > 0 && (w.b.Len() > prevOutLen || w.stateAdv == consumed) {
			pos += consumed
			continue
		}
		var esize int
		var reason string
		if consumed > 0 {
			esize = consumed
			reason = "illegal multibyte sequence"
		} else {
			var fatal error
			esize, reason, fatal = decErrorClassify(consumed, in[pos:])
			if fatal != nil {
				return "", nil, fatal
			}
		}
		// Incomplete sequence at end: buffer and stop when not final.
		if reason == "incomplete multibyte sequence" && !final {
			return w.b.String(), in[pos:], nil
		}
		newpos, err := callDecodeError(encoding, errors, reason, in, pos, pos+esize, w)
		if err != nil {
			return "", nil, err
		}
		pos = newpos
	}
	return w.b.String(), nil, nil
}

// runEncode runs the synchronous encode outer loop.
// Equivalent to multibytecodec_encode (multibytecodec.c:507).
func runEncode(encoding string, fn encodeFunc, config int, input string, errors string) ([]byte, int, error) {
	return runEncodeStateful(encoding, fn, nil, config, input, errors)
}

// runEncodeStateful is runEncode with an optional ENCODER_RESET
// callback. Stateless codecs pass nil. The reset closure runs once,
// after the input is fully consumed, mirroring multibytecodec_encode
// (multibytecodec.c:595).
func runEncodeStateful(encoding string, fn encodeFunc, reset encodeResetFunc, config int, input string, errors string) ([]byte, int, error) {
	if errors == "" {
		errors = "strict"
	}
	st := &codecState{config: config}
	out, _, n, err := runEncodeStatefulWithState(encoding, fn, reset, st, wtf8ToRunes(input), errors, true)
	return out, n, err
}

// wtf8ToRunes decodes a WTF-8 string to runes, preserving lone surrogates.
// Python strings may contain lone surrogates (U+D800-U+DFFF); gopy stores
// them using WTF-8 (same byte pattern as standard UTF-8 3-byte encoding) so
// they survive Go string storage. This decoder recognises those 3-byte
// patterns and yields the actual surrogate rune values instead of RuneError.
//
// CPython: Objects/unicodeobject.c handles surrogates as raw code points.
func wtf8ToRunes(s string) []rune {
	hasWTF8 := false
	for i := 0; i+2 < len(s); i++ {
		if s[i] == 0xED && s[i+1] >= 0xA0 && s[i+1] <= 0xBF && s[i+2] >= 0x80 && s[i+2] <= 0xBF {
			hasWTF8 = true
			break
		}
	}
	if !hasWTF8 {
		return []rune(s)
	}
	runes := make([]rune, 0, len(s))
	i := 0
	for i < len(s) {
		if i+2 < len(s) && s[i] == 0xED && s[i+1] >= 0xA0 && s[i+1] <= 0xBF && s[i+2] >= 0x80 && s[i+2] <= 0xBF {
			r := rune(s[i]&0x0F)<<12 | rune(s[i+1]&0x3F)<<6 | rune(s[i+2]&0x3F)
			runes = append(runes, r)
			i += 3
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		runes = append(runes, r)
		i += size
	}
	return runes
}

// runEncodeStatefulWithState is runEncodeStateful with an external codec
// state. Stateful codecs (HZ, ISO-2022) use this to preserve shift/escape
// state across successive incremental encode() calls on the same instance.
// When flushOnDone is true the reset callback runs after the last rune;
// incremental calls pass false and the caller controls flushing.
//
// When flushOnDone is false (incremental, non-final), the codec function
// receives flags=0 (no MBENC_FLUSH). If the codec signals MBERR_TOOFEW
// (needs more input to complete a multi-character sequence), the loop stops
// and the remaining runes are returned as the second value so the caller can
// prepend them to the next chunk. This mirrors CPython's
// encoder_encode_stateful pending-buffer mechanism.
//
// CPython: Modules/cjkcodecs/multibytecodec.c:793 encoder_encode_stateful
func runEncodeStatefulWithState(encoding string, fn encodeFunc, reset encodeResetFunc, st *codecState, runes []rune, errors string, flushOnDone bool) ([]byte, []rune, int, error) {
	if errors == "" {
		errors = "strict"
	}
	buf := &encodeBuffer{}
	flags := MBENC_FLUSH
	if !flushOnDone {
		flags = 0
	}
	pos := 0
	for pos < len(runes) {
		prevLen := len(buf.buf)
		consumed := fn(st, runes, pos, buf, flags)
		if consumed > 0 && len(buf.buf) > prevLen {
			pos += consumed
			continue
		}
		var esize int
		var reason string
		if consumed > 0 {
			esize = consumed
			reason = "illegal multibyte sequence"
		} else {
			var fatal error
			esize, reason, fatal = encErrorClassify(consumed, runes[pos:])
			if fatal != nil {
				return nil, runes[pos:], pos, fatal
			}
		}
		// MBERR_TOOFEW when not final: stop and hand remaining back to caller.
		//
		// CPython: multibytecodec.c:557 MBERR_TOOFEW stores remainder in pending
		if reason == "incomplete multibyte sequence" && !flushOnDone {
			return buf.buf, runes[pos:], pos, nil
		}
		newpos, err := callEncodeError(encoding, errors, reason, fn, st, runes, pos, pos+esize, buf)
		if err != nil {
			return nil, runes[pos:], pos, err
		}
		pos = newpos
	}
	if flushOnDone && reset != nil {
		reset(st, buf)
	}
	return buf.buf, nil, len(runes), nil
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
// encode path. The handler receives the UTF-8 bytes of the full input
// with byte offsets for the problematic range. The returned replacement
// string is re-encoded through fn/st so stateful codecs (ISO-2022)
// emit the correct charset-switch escape sequences. The returned
// position is a byte offset; this function converts it back to a rune index.
//
// CPython: Modules/cjkcodecs/multibytecodec.c:507 multibytecodec_encerror
func callEncodeError(encoding, errors, reason string, fn encodeFunc, st *codecState, runes []rune, start, end int, buf *encodeBuffer) (int, error) {
	handler, err := codecs.LookupError(errors)
	if err != nil {
		return start, err
	}
	full := []byte(string(runes))
	startByte := utf8Offset(runes, start)
	endByte := utf8Offset(runes, end)
	// Prefix reason with "encode:" so handlers (e.g. replace_errors) can
	// distinguish encode from decode context and return the right replacement.
	//
	// CPython: Python/codecs.c:882 replace_errors checks UnicodeEncodeError
	rep, newposByte, err := handler(encoding, "encode:"+reason, full, startByte, endByte)
	if err != nil {
		// The strict handler emits UnicodeDecodeError; reclassify as
		// UnicodeEncodeError on the encode path.
		//
		// CPython: multibytecodec.c passes a UnicodeEncodeError to the
		// error handler; strict_errors re-raises it as-is.
		if s := err.Error(); len(s) > 19 && s[:19] == "UnicodeDecodeError:" {
			err = fmt.Errorf("UnicodeEncodeError:%s", s[19:])
		}
		return start, err
	}
	// Re-encode the replacement string through the codec so stateful
	// encoders emit correct charset-switch sequences.
	//
	// CPython: Modules/cjkcodecs/multibytecodec.c:557 re-encode replacement
	repRunes := []rune(rep)
	ri := 0
	for ri < len(repRunes) {
		prevLen := len(buf.buf)
		consumed := fn(st, repRunes, ri, buf, MBENC_FLUSH)
		if consumed > 0 && len(buf.buf) > prevLen {
			ri += consumed
		} else {
			ri++
		}
	}
	if newposByte < 0 {
		newposByte += len(full)
	}
	if newposByte < 0 || newposByte > len(full) {
		return start, errOutOfBounds
	}
	return runeIndex(full, newposByte), nil
}

// runeIndex returns the number of runes (characters) encoded in the
// first n bytes of full. It handles partial UTF-8 sequences at the
// boundary by rounding down to the last complete rune.
func runeIndex(full []byte, n int) int {
	if n <= 0 {
		return 0
	}
	if n >= len(full) {
		return len([]rune(string(full)))
	}
	return len([]rune(string(full[:n])))
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
		sz := utf8.RuneLen(runes[i])
		if sz < 0 {
			sz = 3
		}
		off += sz
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
