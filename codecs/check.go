package codecs

import "sync/atomic"

// devMode mirrors PyConfig.dev_mode. CheckEncodingErrors only runs the
// codec lookups when this is set, matching CPython's release-build
// behavior where the encoding/errors validation happens only under
// -X dev (or a debug build, which gopy does not model separately).
//
// CPython: Python/initconfig.c:142 SPEC(dev_mode)
var devMode atomic.Bool

// SetDevMode records whether the interpreter is running in development
// mode. The cmd front end calls this after parsing -X dev / reading
// PYTHONDEVMODE so the str(bytes) decode path can validate its
// encoding and errors arguments eagerly.
func SetDevMode(on bool) { devMode.Store(on) }

// CheckEncodingErrors validates the encoding and errors arguments of a
// decode/encode the way CPython's unicode_check_encoding_errors does:
// in development mode it looks up the codec and the error handler so a
// bogus name raises LookupError even when the data is empty (and would
// otherwise never reach the handler). Common built-in names are
// short-circuited exactly as CPython does. Outside dev mode it is a
// no-op.
//
// CPython: Objects/unicodeobject.c:612 unicode_check_encoding_errors
func CheckEncodingErrors(encoding, errors string) error {
	if !devMode.Load() {
		return nil
	}

	if encoding != "" &&
		encoding != "utf-8" &&
		encoding != "utf8" &&
		encoding != "ascii" {
		if _, err := Lookup(encoding); err != nil {
			return err
		}
	}

	if errors != "" &&
		errors != "strict" &&
		errors != "ignore" &&
		errors != "replace" &&
		errors != "surrogateescape" &&
		errors != "surrogatepass" {
		if _, err := LookupError(errors); err != nil {
			return err
		}
	}
	return nil
}
