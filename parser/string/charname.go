// CPython: Objects/unicodeobject.c _PyUnicode_DecodeUnicodeEscape
// path for the \N{NAME} escape. CPython routes the lookup through
// the unicodedata module's lookup() entry, which expands aliases and
// named sequences from the full UCD table. gopy plugs into the same
// module via module/unicodedata.Lookup so the parser sees every name
// CPython does.

package string

import (
	"fmt"

	"github.com/tamnd/gopy/module/unicodedata"
)

// CharByName returns the rune for a single-codepoint Unicode name.
// Named sequences (which expand to multi-rune strings) are rejected
// here; callers that want the full expansion use NameLookup.
//
// CPython: Modules/_unicodedata.c ucd_lookup
func CharByName(name string) (rune, error) {
	s, ok := unicodedata.Lookup(name)
	if !ok {
		return 0, fmt.Errorf("unknown Unicode character name %q", name)
	}
	runes := []rune(s)
	if len(runes) != 1 {
		return 0, fmt.Errorf("named sequence %q expands to %d runes", name, len(runes))
	}
	return runes[0], nil
}

// NameLookup returns the full expansion for a \N{NAME} escape, which
// may be a single rune or a named sequence (multi-rune). ok is false
// when the name is not in the UCD table.
//
// CPython: Modules/_unicodedata.c ucd_lookup
func NameLookup(name string) (string, bool) {
	return unicodedata.Lookup(name)
}

// UnicodeDataLoadCheck, when non-nil, mirrors CPython's lazy
// PyCapsule_Import of the unicodedata module before a \N{NAME} lookup:
// it returns an error when the module can't be loaded (for example after
// `sys.modules['unicodedata'] = None`), so the decoder can raise
// "\N escapes not supported (can't load unicodedata module)" instead of
// silently consulting the built-in table. The VM installs this hook; at
// bootstrap parse time it is nil and the table is used directly.
//
// CPython: Objects/unicodeobject.c:6791 load_ucnhash / ucnhash_capi
var UnicodeDataLoadCheck func() error

// CheckUnicodeDataLoadable runs the installed UnicodeDataLoadCheck hook,
// returning its error (nil when the hook is unset or the module loads).
func CheckUnicodeDataLoadable() error {
	if UnicodeDataLoadCheck == nil {
		return nil
	}
	return UnicodeDataLoadCheck()
}
