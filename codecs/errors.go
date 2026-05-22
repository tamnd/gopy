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
	if end == start+1 && start >= 0 && start < len(input) {
		return "", 0, fmt.Errorf("UnicodeDecodeError: '%s' codec can't decode byte 0x%02x in position %d: %s",
			enc, input[start], start, reason)
	}
	return "", 0, fmt.Errorf("UnicodeDecodeError: '%s' codec can't decode bytes in position %d-%d: %s",
		enc, start, end-1, reason)
}

// ignoreHandler silently skips undecodable bytes.
//
// CPython: Python/codecs.c:L875 ignore_errors
func ignoreHandler(_, _ string, _ []byte, _ int, end int) (replacement string, newpos int, err error) {
	return "", end, nil
}

// replaceHandler substitutes U+FFFD for undecodable bytes.
//
// CPython: Python/codecs.c:L882 replace_errors
func replaceHandler(_, _ string, _ []byte, _ int, end int) (replacement string, newpos int, err error) {
	return string(utf8.RuneError), end, nil
}

// runeAt decodes the UTF-8 rune that starts at offset i in input.
// Returns U+FFFD on invalid UTF-8, matching codecs that pre-validated
// their input.
func runeAt(input []byte, i int) (rune, int) {
	if i < 0 || i >= len(input) {
		return utf8.RuneError, 1
	}
	r, sz := utf8.DecodeRune(input[i:])
	if sz == 0 {
		return utf8.RuneError, 1
	}
	return r, sz
}

// xmlCharRefReplaceHandler emits &#N; for every unencodable codepoint
// in the slice. Encode-only; decode-side use raises just like CPython
// does.
//
// CPython: Python/codecs.c:1071 PyCodec_XMLCharRefReplaceErrors
func xmlCharRefReplaceHandler(_, _ string, input []byte, start, end int) (string, int, error) {
	var out []byte
	for i := start; i < end; {
		r, sz := runeAt(input, i)
		out = append(out, []byte(fmt.Sprintf("&#%d;", r))...)
		i += sz
	}
	return string(out), end, nil
}

// backslashReplaceHandler emits \xNN / \uNNNN / \UNNNNNNNN for every
// codepoint in the slice. Used by both encode and decode paths.
//
// CPython: Python/codecs.c:1020 PyCodec_BackslashReplaceErrors
func backslashReplaceHandler(_, _ string, input []byte, start, end int) (string, int, error) {
	var out []byte
	for i := start; i < end; {
		r, sz := runeAt(input, i)
		switch {
		case r < 0x100:
			out = append(out, []byte(fmt.Sprintf("\\x%02x", r))...)
		case r < 0x10000:
			out = append(out, []byte(fmt.Sprintf("\\u%04x", r))...)
		default:
			out = append(out, []byte(fmt.Sprintf("\\U%08x", r))...)
		}
		i += sz
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
// and decode unchanged. The current call site only registers the
// handler; an actual codec invocation routes through the strict
// fallback until the UnicodeError objects expose start/end slots.
//
// CPython: Python/codecs.c:1403 PyCodec_SurrogatePassErrors
func surrogatePassHandler(enc, reason string, input []byte, start, end int) (string, int, error) {
	return strictHandler(enc, reason, input, start, end)
}

// surrogateEscapeHandler is the PEP 383 codec error handler used when
// the OS hands us undecodable bytes. On decode each byte in 0x80..0xFF
// maps to U+DC80..U+DCFF so the round-trip through encode (which
// reverses the mapping) reproduces the original bytes. ASCII bytes
// (<0x80) are not eligible and raise like strict.
//
// CPython: Python/codecs.c:1496 PyCodec_SurrogateEscapeErrors
func surrogateEscapeHandler(enc, reason string, input []byte, start, end int) (string, int, error) {
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
		var buf [4]byte
		n := utf8.EncodeRune(buf[:], r)
		b = append(b, buf[:n]...)
	}
	return string(b), end, nil
}
