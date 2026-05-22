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
