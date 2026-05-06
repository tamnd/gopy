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

// ErrorHandler is a function that handles an encode or decode error.
// It receives the position of the bad input and returns a replacement
// string plus the new position to resume from.
//
// CPython: Python/codecs.c:L793 call_codec_error_handler
type ErrorHandler func(enc string, input []byte, start, end int) (replacement string, newpos int, err error)

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

// strictHandler raises a UnicodeDecodeError / UnicodeEncodeError.
//
// CPython: Python/codecs.c:L868 strict_errors
func strictHandler(enc string, _ []byte, start, end int) (replacement string, newpos int, err error) {
	return "", 0, fmt.Errorf("UnicodeDecodeError: %q codec can't decode bytes in position %d-%d", enc, start, end-1)
}

// ignoreHandler silently skips undecodable bytes.
//
// CPython: Python/codecs.c:L875 ignore_errors
func ignoreHandler(_ string, _ []byte, _ int, end int) (replacement string, newpos int, err error) {
	return "", end, nil
}

// replaceHandler substitutes U+FFFD for undecodable bytes.
//
// CPython: Python/codecs.c:L882 replace_errors
func replaceHandler(_ string, _ []byte, _ int, end int) (replacement string, newpos int, err error) {
	return string(utf8.RuneError), end, nil
}
