// Error handler registry. Mirrors the Python-level codecs.register_error
// / codecs.lookup_error pair: handlers are stored by name and called
// when encode/decode hits an unencodable character or invalid sequence.
//
// CPython: Python/codecs.c:L763 PyCodec_RegisterError
package codecs

import (
	"fmt"
	"sync"
	"unicode/utf8"
)

// The xmlcharrefreplace / backslashreplace / namereplace / surrogatepass
// / surrogateescape handlers below are stub ports that read enough of
// the input slice to do the common-case replacement. CPython treats
// the slice as Py_UCS4 codepoints; gopy keeps it as UTF-8 bytes, so
// `start` and `end` are byte offsets.
//
// CPython: Python/codecs.c:1071-1567 codec_handler_*

// ErrorHandler is a function that handles an encode or decode error.
// It receives the position of the bad input plus the codec-supplied
// reason string (matches the UnicodeError.reason attribute on the
// upstream side) and returns a replacement string plus the new
// position to resume from.
//
// CPython: Python/codecs.c:L793 call_codec_error_handler
type ErrorHandler func(enc, reason string, input []byte, start, end int) (replacement string, newpos int, err error)

var (
	errorHandlerMu sync.RWMutex
	errorHandlers  = map[string]ErrorHandler{}
)

func init() {
	// seed the standard handlers that CPython always provides
	// CPython: Python/codecs.c:L939 codec_register_error
	errorHandlers["strict"] = strictHandler
	errorHandlers["ignore"] = ignoreHandler
	errorHandlers["replace"] = replaceHandler
	errorHandlers["xmlcharrefreplace"] = xmlCharRefReplaceHandler
	errorHandlers["backslashreplace"] = backslashReplaceHandler
	errorHandlers["namereplace"] = nameReplaceHandler
	errorHandlers["surrogatepass"] = surrogatePassHandler
	errorHandlers["surrogateescape"] = surrogateEscapeHandler
}

// RegisterError registers a named error handler. Overwrites any prior
// handler registered under the same name.
//
// CPython: Python/codecs.c:L763 PyCodec_RegisterError
func RegisterError(name string, fn ErrorHandler) {
	errorHandlerMu.Lock()
	errorHandlers[name] = fn
	errorHandlerMu.Unlock()
}

// UnregisterError removes the handler registered under name. Built-in
// handler names (strict, ignore, replace, xmlcharrefreplace,
// backslashreplace, namereplace, surrogatepass, surrogateescape) cannot
// be removed and return a ValueError.
//
// CPython: Python/codecs.c:637 _PyCodec_UnregisterError
func UnregisterError(name string) error {
	builtins := [...]string{
		"strict", "ignore", "replace",
		"xmlcharrefreplace", "backslashreplace", "namereplace",
		"surrogatepass", "surrogateescape",
	}
	for _, b := range builtins {
		if name == b {
			return fmt.Errorf("ValueError: cannot un-register built-in error handler '%s'", name)
		}
	}
	errorHandlerMu.Lock()
	delete(errorHandlers, name)
	errorHandlerMu.Unlock()
	return nil
}

// LookupError returns the handler registered under name.
//
// CPython: Python/codecs.c:L793 PyCodec_LookupError
func LookupError(name string) (ErrorHandler, error) {
	errorHandlerMu.RLock()
	fn, ok := errorHandlers[name]
	errorHandlerMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("LookupError: unknown error handler name %q", name)
	}
	return fn, nil
}

// UnicodeEncodeErr is a structured Go error for UnicodeEncodeError. Callers
// in the objects layer convert it to a Python UnicodeEncodeError exception with
// .encoding, .object, .start, .end, and .reason attributes.
//
// CPython: Objects/exceptions.c:3040 UnicodeError_init
type UnicodeEncodeErr struct {
	Encoding string
	Object   string // the input string being encoded
	Start    int
	End      int
	Reason   string
}

func (e *UnicodeEncodeErr) Error() string {
	if e.End == e.Start+1 {
		return fmt.Sprintf("UnicodeEncodeError: '%s' codec can't encode character '\\U%08X' in position %d: %s",
			e.Encoding, charAtRunePos([]byte(e.Object), e.Start), e.Start, e.Reason)
	}
	return fmt.Sprintf("UnicodeEncodeError: '%s' codec can't encode characters in position %d-%d: %s",
		e.Encoding, e.Start, e.End-1, e.Reason)
}

// UnicodeDecodeErr is a structured Go error for UnicodeDecodeError.
//
// CPython: Objects/exceptions.c:3040 UnicodeError_init
type UnicodeDecodeErr struct {
	Encoding string
	Object   []byte // the bytes being decoded
	Start    int
	End      int
	Reason   string
}

func (e *UnicodeDecodeErr) Error() string {
	if e.End == e.Start+1 && e.Start >= 0 && e.Start < len(e.Object) {
		return fmt.Sprintf("UnicodeDecodeError: '%s' codec can't decode byte 0x%02x in position %d: %s",
			e.Encoding, e.Object[e.Start], e.Start, e.Reason)
	}
	return fmt.Sprintf("UnicodeDecodeError: '%s' codec can't decode bytes in position %d-%d: %s",
		e.Encoding, e.Start, e.End-1, e.Reason)
}

// strictHandler raises a UnicodeDecodeError / UnicodeEncodeError. The
// formatting mirrors UnicodeDecodeError.__str__: a singular "byte 0xNN
// in position N" form when end == start + 1, plural otherwise.
//
// CPython: Python/codecs.c:L868 strict_errors
// CPython: Objects/exceptions.c:3815 UnicodeDecodeError_str
func strictHandler(enc, reason string, input []byte, start, end int) (replacement string, newpos int, err error) {
	if reason == "" {
		reason = "reason unknown"
	}
	// If the reason starts with "encode:" this is an encoding error;
	// strip the prefix and raise UnicodeEncodeError.
	if len(reason) >= 7 && reason[:7] == "encode:" {
		reason = reason[7:]
		return "", 0, &UnicodeEncodeErr{
			Encoding: enc,
			Object:   string(input),
			Start:    start,
			End:      end,
			Reason:   reason,
		}
	}
	return "", 0, &UnicodeDecodeErr{
		Encoding: enc,
		Object:   input,
		Start:    start,
		End:      end,
		Reason:   reason,
	}
}

// charAtRunePos returns the Unicode code point of the rune at position pos
// when input is interpreted as a UTF-8-encoded string. Uses lenient decoding
// so lone surrogates (e.g. U+DC80) stored as pseudo-UTF-8 pass through.
func charAtRunePos(input []byte, pos int) rune {
	runes := lenientRunes(input)
	if pos >= 0 && pos < len(runes) {
		return runes[pos]
	}
	return 0xfffd
}

// ignoreHandler silently skips undecodable bytes.
//
// CPython: Python/codecs.c:L875 ignore_errors
func ignoreHandler(_, _ string, _ []byte, _ int, end int) (replacement string, newpos int, err error) {
	return "", end, nil
}

// replaceHandler substitutes U+FFFD for undecodable bytes or ? for
// unencodable characters. The encode path prefixes the reason string
// with "encode:" so this handler can distinguish the two.
//
// CPython: Python/codecs.c:L882 replace_errors
func replaceHandler(_, reason string, _ []byte, _ int, end int) (replacement string, newpos int, err error) {
	if len(reason) >= 7 && reason[:7] == "encode:" {
		return "?", end, nil
	}
	return string(utf8.RuneError), end, nil
}

// surrogateToBytes encodes a surrogate rune (U+D800..U+DFFF) as the
// 3-byte pseudo-UTF-8 sequence that CPython uses internally.
// Go's utf8.EncodeRune rejects surrogates and emits U+FFFD instead.
func surrogateToBytes(r rune) []byte {
	return []byte{
		byte(0xE0 | (r >> 12)),
		byte(0x80 | ((r >> 6) & 0x3F)),
		byte(0x80 | (r & 0x3F)),
	}
}

// lenientRunes decodes bytes to runes accepting lone surrogates.
// Go's []rune() rejects surrogates as invalid UTF-8, but Python strings
// can contain lone surrogates stored as 3-byte pseudo-UTF-8 sequences
// (0xED 0xA0..0xBF 0x80..0xBF). This decoder passes them through as-is.
func lenientRunes(b []byte) []rune {
	var out []rune
	for i := 0; i < len(b); {
		if b[i]&0x80 == 0 {
			out = append(out, rune(b[i]))
			i++
			continue
		}
		if b[i]&0xE0 == 0xC0 && i+1 < len(b) && b[i+1]&0xC0 == 0x80 {
			r := rune(b[i]&0x1F)<<6 | rune(b[i+1]&0x3F)
			out = append(out, r)
			i += 2
			continue
		}
		if b[i]&0xF0 == 0xE0 && i+2 < len(b) && b[i+1]&0xC0 == 0x80 && b[i+2]&0xC0 == 0x80 {
			r := rune(b[i]&0x0F)<<12 | rune(b[i+1]&0x3F)<<6 | rune(b[i+2]&0x3F)
			out = append(out, r) // includes surrogates (e.g. U+DC80)
			i += 3
			continue
		}
		if b[i]&0xF8 == 0xF0 && i+3 < len(b) && b[i+1]&0xC0 == 0x80 && b[i+2]&0xC0 == 0x80 && b[i+3]&0xC0 == 0x80 {
			r := rune(b[i]&0x07)<<18 | rune(b[i+1]&0x3F)<<12 | rune(b[i+2]&0x3F)<<6 | rune(b[i+3]&0x3F)
			out = append(out, r)
			i += 4
			continue
		}
		out = append(out, utf8.RuneError)
		i++
	}
	return out
}

// xmlCharRefReplaceHandler emits &#N; for every unencodable codepoint.
// Encode-only; start/end are rune positions in the input string.
//
// CPython: Python/codecs.c:1071 PyCodec_XMLCharRefReplaceErrors
func xmlCharRefReplaceHandler(_, _ string, input []byte, start, end int) (string, int, error) {
	runes := lenientRunes(input)
	var out []byte
	for i := start; i < end && i < len(runes); i++ {
		out = fmt.Appendf(out, "&#%d;", runes[i])
	}
	return string(out), end, nil
}

// backslashReplaceHandler emits \xNN / \uNNNN / \UNNNNNNNN for every
// codepoint in the slice. Used by both encode and decode paths.
//
// CPython: Python/codecs.c:1020 PyCodec_BackslashReplaceErrors
func backslashReplaceHandler(_, reason string, input []byte, start, end int) (string, int, error) {
	var out []byte
	isEncode := len(reason) >= 7 && reason[:7] == "encode:"
	if isEncode {
		// Encode: input is UTF-8 bytes of a string, start/end are rune positions.
		runes := lenientRunes(input)
		for i := start; i < end && i < len(runes); i++ {
			r := runes[i]
			switch {
			case r < 0x100:
				out = fmt.Appendf(out, "\\x%02x", r)
			case r < 0x10000:
				out = fmt.Appendf(out, "\\u%04x", r)
			default:
				out = fmt.Appendf(out, "\\U%08x", r)
			}
		}
	} else {
		// Decode: input is raw bytes being decoded, start/end are byte positions.
		for i := start; i < end && i < len(input); i++ {
			out = fmt.Appendf(out, "\\x%02x", input[i])
		}
	}
	return string(out), end, nil
}

// nameReplaceHandler ports CPython's \N{NAME} escape. gopy does not
// vendor the unicode-name database yet, so we fall back to the
// backslash-replace formatting until that table lands.
//
// CPython: Python/codecs.c:1085 PyCodec_NameReplaceErrors
func nameReplaceHandler(enc, reason string, input []byte, start, end int) (string, int, error) {
	return backslashReplaceHandler(enc, reason, input, start, end)
}

// surrogatePassHandler passes UTF-16 surrogate halves through encode
// and decode unchanged. Encode emits the 3-byte pseudo-UTF-8 sequence
// for each surrogate; decode re-reads those sequences as surrogates.
//
// CPython: Python/codecs.c:1403 PyCodec_SurrogatePassErrors
func surrogatePassHandler(enc, reason string, input []byte, start, end int) (string, int, error) {
	isEncode := len(reason) >= 7 && reason[:7] == "encode:"
	if isEncode && enc == "utf-8" {
		runes := lenientRunes(input)
		var b []byte
		for i := start; i < end && i < len(runes); i++ {
			r := runes[i]
			if r < 0xD800 || r > 0xDFFF {
				return strictHandler(enc, reason, input, start, end)
			}
			b = append(b, surrogateToBytes(r)...)
		}
		return string(b), end, nil
	}
	if !isEncode && enc == "utf-8" {
		// Decode: the bad byte at start should be the lead of a 3-byte
		// surrogate pseudo-UTF-8 sequence: 0xED 0xA0-0xBF 0x80-0xBF.
		// Each call comes from decodeUTF8 with end = start+1 (one bad byte).
		if start+3 > len(input) || input[start] != 0xED ||
			input[start+1] < 0xA0 || input[start+1] > 0xBF ||
			input[start+2] < 0x80 || input[start+2] > 0xBF {
			return strictHandler(enc, reason, input, start, end)
		}
		r := rune(input[start]&0x0F)<<12 | rune(input[start+1]&0x3F)<<6 | rune(input[start+2]&0x3F)
		return string(surrogateToBytes(r)), start + 3, nil
	}
	return strictHandler(enc, reason, input, start, end)
}

// surrogateEscapeHandler is the PEP 383 codec error handler. On decode,
// bytes 0x80..0xFF map to surrogates U+DC80..U+DCFF. On encode, surrogates
// U+DC80..U+DCFF map back to bytes 0x80..0xFF; non-surrogate characters
// raise UnicodeEncodeError.
//
// CPython: Python/codecs.c:1496 PyCodec_SurrogateEscapeErrors
func surrogateEscapeHandler(enc, reason string, input []byte, start, end int) (string, int, error) {
	isEncode := len(reason) >= 7 && reason[:7] == "encode:"
	if isEncode {
		// Encode: start/end are rune positions in the string.
		runes := lenientRunes(input)
		var b []byte
		for i := start; i < end && i < len(runes); i++ {
			r := runes[i]
			if r < 0xDC80 || r > 0xDCFF {
				// Not in the DC80..DCFF range: raise UnicodeEncodeError covering
				// all consecutive non-escapable surrogates (CPython spans them).
				// Keep the "encode:" prefix so strictHandler formats UnicodeEncodeError.
				actualEnd := start
				for actualEnd < len(runes) {
					nr := runes[actualEnd]
					isSurrogate := nr >= 0xD800 && nr <= 0xDFFF
					isEscapable := nr >= 0xDC80 && nr <= 0xDCFF
					if !isSurrogate || isEscapable {
						break
					}
					actualEnd++
				}
				return strictHandler(enc, reason, input, start, actualEnd)
			}
			b = append(b, byte(r-0xDC00))
		}
		return string(b), end, nil
	}
	// Decode: start/end are byte positions in raw input.
	if start < 0 || end > len(input) || start >= end {
		return strictHandler(enc, reason, input, start, end)
	}
	var b []byte
	for i := start; i < end; i++ {
		c := input[i]
		if c < 0x80 {
			return strictHandler(enc, reason, input, start, end)
		}
		r := rune(0xDC00) + rune(c)
		b = append(b, surrogateToBytes(r)...)
	}
	return string(b), end, nil
}
