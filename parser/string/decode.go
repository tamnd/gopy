// CPython: Objects/unicodeobject.c
// _PyUnicode_DecodeUnicodeEscapeInternal and Objects/bytesobject.c
// _PyBytes_DecodeEscape. The two functions share a structure but
// differ on the unicode escapes (\xNN, \uNNNN, \UNNNNNNNN,
// \N{name}) which only the unicode form accepts.

package string

import (
	"fmt"
	"unicode/utf8"
)

// DecodeError carries the byte-position pair that
// _PyUnicode_DecodeUnicodeEscapeInternal2 records on its
// PyUnicodeDecodeError so _Pypegen_raise_decode_error can format the
// codec-style prefix the parser surface needs.
//
// CPython: Objects/unicodeobject.c:6854 raise on "unicodeescape"
type DecodeError struct {
	Reason string
	Start  int
	End    int
	// Plain marks an error that CPython raises with PyErr_SetString on a
	// bare PyExc_UnicodeError rather than through the codec error
	// machinery, so it carries no "'unicodeescape' codec can't decode
	// bytes in position N-M:" prefix (e.g. the \N-module-load failure).
	Plain bool
}

func (e *DecodeError) Error() string {
	return e.Reason
}

// newDecodeError constructs a DecodeError that mirrors the
// PyUnicodeDecodeError raised inside the C decoder.
func newDecodeError(reason string, start, end int) *DecodeError {
	return &DecodeError{Reason: reason, Start: start, End: end}
}

// newPlainDecodeError constructs a DecodeError for the cases CPython
// raises directly on PyExc_UnicodeError (no codec position prefix).
func newPlainDecodeError(reason string) *DecodeError {
	return &DecodeError{Reason: reason, Plain: true}
}

// decodeUnicodeEscapes walks s and expands the standard Python
// escape sequences. The returned string is the decoded text in
// UTF-8 form. Unknown escape sequences are kept verbatim and
// reported via the warnings slice so the caller can surface them
// as SyntaxWarning text without aborting decoding.
//
// The body is first run through preprocessUnicodeEscapes so a
// trailing backslash, or a backslash immediately followed by a
// non-ASCII byte, gets rewritten into a `\` escape. CPython
// applies the same preprocessing because its inner decoder rejects
// trailing backslashes and treats `\<utf8>` as an invalid escape; the
// rewrite hides both quirks from callers (and lets f-string middles
// that legitimately end with `\` after a `\{` warning round-trip
// cleanly).
//
// CPython: Parser/string_parser.c:135 decode_unicode_with_escapes
// CPython: Objects/unicodeobject.c _PyUnicode_DecodeUnicodeEscapeInternal
func decodeUnicodeEscapes(s []byte) (text string, warnings []string, offsets []int, err error) {
	s = preprocessUnicodeEscapes(s)
	return decodeUnicodeEscapesInternal(s)
}

// preprocessUnicodeEscapes mirrors the buffer-rewrite pass in
// decode_unicode_with_escapes. CPython has to do this because its
// inner decoder operates on a flat byte buffer that knows nothing
// about UTF-8 or EOF, so it pre-translates the awkward cases:
//   - a trailing `\` becomes `\`
//   - a `\` followed by a high byte becomes `\` + the high byte
//
// We keep the rewrite even though our inner decoder is happier with
// raw UTF-8, because callers (DecodeFStringPart in particular) rely
// on the trailing-backslash rewrite to keep f-string middles like
// `\` decodable after a `\{` SyntaxWarning fires.
//
// CPython: Parser/string_parser.c:158 decode_unicode_with_escapes inner loop
func preprocessUnicodeEscapes(s []byte) []byte {
	hasTrigger := false
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' {
			if i+1 >= len(s) || s[i+1] >= 0x80 {
				hasTrigger = true
				break
			}
			i++
		}
	}
	if !hasTrigger {
		return s
	}
	out := make([]byte, 0, len(s)+5)
	i := 0
	for i < len(s) {
		if s[i] == '\\' {
			out = append(out, '\\')
			i++
			if i >= len(s) || s[i] >= 0x80 {
				out = append(out, 'u', '0', '0', '5', 'c')
				if i >= len(s) {
					break
				}
			}
		}
		if i < len(s) {
			out = append(out, s[i])
			i++
		}
	}
	return out
}

// decodeUnicodeEscapesInternal is the inner escape-decoder loop. It
// matches _PyUnicode_DecodeUnicodeEscapeInternal2's behavior and is
// only entered from decodeUnicodeEscapes after the buffer rewrite.
//
// CPython: Objects/unicodeobject.c _PyUnicode_DecodeUnicodeEscapeInternal2
func decodeUnicodeEscapesInternal(s []byte) (text string, warnings []string, offsets []int, err error) {
	var out []byte
	var warns []string
	var warnOffsets []int
	i := 0
	for i < len(s) {
		c := s[i]
		if c != '\\' {
			out = append(out, c)
			i++
			continue
		}
		escStart := i
		i++
		if i >= len(s) {
			return "", nil, nil, newDecodeError("\\ at end of string", escStart, i)
		}
		c = s[i]
		i++
		switch c {
		case '\n':
			// line continuation: backslash-newline drops both
		case '\\':
			out = append(out, '\\')
		case '\'':
			out = append(out, '\'')
		case '"':
			out = append(out, '"')
		case 'a':
			out = append(out, 0x07)
		case 'b':
			out = append(out, 0x08)
		case 'f':
			out = append(out, 0x0c)
		case 'n':
			out = append(out, '\n')
		case 'r':
			out = append(out, '\r')
		case 't':
			out = append(out, '\t')
		case 'v':
			out = append(out, 0x0b)
		case '0', '1', '2', '3', '4', '5', '6', '7':
			// CPython: Objects/unicodeobject.c:6713 octal escape.
			// Octal value is a Unicode ordinal: emit as a codepoint,
			// not as a raw byte (raw bytes >=0x80 corrupt UTF-8).
			// Values > 0o377 are reported as invalid escapes, but
			// the codepoint is still emitted. The warning text mirrors
			// Parser/string_parser.c:32 warn_invalid_escape_sequence
			// octal branch: it quotes the original digit triple, not the
			// numeric value, so `\477` shows `"\477"` in the message.
			digitsStart := escStart + 1
			val := int(c - '0')
			for k := 0; k < 2 && i < len(s) && s[i] >= '0' && s[i] <= '7'; k++ {
				val = val*8 + int(s[i]-'0')
				i++
			}
			if val > 0o377 {
				digits := string(s[digitsStart:i])
				warns = append(warns, fmt.Sprintf(
					"\"\\%s\" is an invalid octal escape sequence. "+
						"Such sequences will not work in the future. "+
						"Did you mean \"\\\\%s\"? A raw string is also an option.",
					digits, digits))
				warnOffsets = append(warnOffsets, escStart)
			}
			out = utf8.AppendRune(out, rune(val))
		case 'x':
			if i+2 > len(s) {
				return "", nil, nil, newDecodeError("truncated \\xXX escape", escStart, len(s))
			}
			v, err := parseHex(s[i : i+2])
			if err != nil {
				return "", nil, nil, newDecodeError("truncated \\xXX escape", escStart, i+2)
			}
			out = utf8.AppendRune(out, rune(v))
			i += 2
		case 'u':
			if i+4 > len(s) {
				return "", nil, nil, newDecodeError("truncated \\uXXXX escape", escStart, len(s))
			}
			v, err := parseHex(s[i : i+4])
			if err != nil {
				return "", nil, nil, newDecodeError("truncated \\uXXXX escape", escStart, i+4)
			}
			// Lone surrogates (U+D800-U+DFFF) are valid in Python string literals.
			// Go's utf8.AppendRune silently replaces them with U+FFFD; use WTF-8
			// encoding instead so the encoder path can recover the original value.
			//
			// CPython: Objects/unicodeobject.c stores surrogates as raw code points.
			if v >= 0xD800 && v <= 0xDFFF {
				r := rune(v)
				out = append(out, byte(0xE0|(r>>12)), byte(0x80|((r>>6)&0x3F)), byte(0x80|(r&0x3F)))
			} else {
				out = utf8.AppendRune(out, rune(v))
			}
			i += 4
		case 'U':
			if i+8 > len(s) {
				return "", nil, nil, newDecodeError("truncated \\UXXXXXXXX escape", escStart, len(s))
			}
			v, err := parseHex(s[i : i+8])
			if err != nil {
				return "", nil, nil, newDecodeError("truncated \\UXXXXXXXX escape", escStart, i+8)
			}
			if v > 0x10FFFF {
				return "", nil, nil, newDecodeError("illegal Unicode character", escStart, i+8)
			}
			out = utf8.AppendRune(out, rune(v))
			i += 8
		case 'N':
			// CPython: Objects/unicodeobject.c:6791 \N{name} expansion.
			// The matching errors live at unicodeobject.c:6801 ("malformed
			// \N character escape") and :6826 ("unknown Unicode character
			// name"); both are surfaced through PyUnicodeDecodeError so
			// _Pypegen_raise_decode_error can format the codec prefix.
			if i >= len(s) || s[i] != '{' {
				return "", nil, nil, newDecodeError("malformed \\N character escape", escStart, i)
			}
			j := i + 1
			for j < len(s) && s[j] != '}' {
				j++
			}
			if j >= len(s) {
				return "", nil, nil, newDecodeError("malformed \\N character escape", escStart, j)
			}
			name := string(s[i+1 : j])
			if name == "" {
				return "", nil, nil, newDecodeError("malformed \\N character escape", escStart, j+1)
			}
			// CPython loads the unicodedata module lazily here; if the
			// import is blocked (sys.modules['unicodedata'] = None) it
			// raises a UnicodeError before any name lookup.
			//
			// CPython: Objects/unicodeobject.c:6791 load_ucnhash
			if err := CheckUnicodeDataLoadable(); err != nil {
				return "", nil, nil, newPlainDecodeError("\\N escapes not supported (can't load unicodedata module)")
			}
			expanded, ok := NameLookup(name)
			if !ok {
				return "", nil, nil, newDecodeError("unknown Unicode character name", escStart, j+1)
			}
			// \N{} rejects named sequences: CPython calls getcode with
			// with_named_seq=0, so a name that resolves to a multi-codepoint
			// named sequence is reported as an unknown name. Only
			// unicodedata.lookup() (with_named_seq=1) returns the expansion.
			//
			// CPython: Modules/unicodedata.c:1399 _check_alias_and_seq
			if utf8.RuneCountInString(expanded) != 1 {
				return "", nil, nil, newDecodeError("unknown Unicode character name", escStart, j+1)
			}
			out = append(out, expanded...)
			i = j + 1
		default:
			// PEP 414 keeps the backslash in the output for
			// unrecognized escapes; CPython 3.14 also emits a
			// SyntaxWarning so the user can spot a typo. The text
			// mirrors Parser/string_parser.c:38 warn_invalid_escape_sequence
			// non-octal branch so the runtime warnings filter sees the
			// same key the tokenizer surface uses.
			out = append(out, '\\', c)
			warns = append(warns, fmt.Sprintf(
				"\"\\%c\" is an invalid escape sequence. "+
					"Such sequences will not work in the future. "+
					"Did you mean \"\\\\%c\"? A raw string is also an option.",
				c, c))
			warnOffsets = append(warnOffsets, escStart)
		}
	}
	if !isValidWTF8(out) {
		return "", nil, nil, fmt.Errorf("invalid utf-8 in string literal")
	}
	return string(out), warns, warnOffsets, nil
}

// isValidWTF8 returns true when b is valid WTF-8. WTF-8 is a superset of
// UTF-8 that also accepts lone surrogates (U+D800-U+DFFF) encoded as
// three-byte sequences 0xED 0xA0-0xBF 0x80-0xBF. All other invalid UTF-8
// sequences are rejected.
//
// CPython: Python strings may contain lone surrogates; gopy stores them as
// WTF-8 so the raw codepoint value survives Go string storage.
func isValidWTF8(b []byte) bool {
	for i := 0; i < len(b); {
		if b[i] < 0x80 {
			i++
			continue
		}
		r, size := utf8.DecodeRune(b[i:])
		if r != utf8.RuneError || size != 1 {
			i += size
			continue
		}
		// RuneError with size 1 means invalid UTF-8. Check for WTF-8 surrogate.
		if i+2 < len(b) && b[i] == 0xED && b[i+1] >= 0xA0 && b[i+1] <= 0xBF && b[i+2] >= 0x80 && b[i+2] <= 0xBF {
			i += 3
			continue
		}
		return false
	}
	return true
}

// decodeBytesEscapes is the bytes form. The unicode escapes are
// rejected here because they have no meaning in a byte literal.
//
// CPython: Objects/bytesobject.c _PyBytes_DecodeEscape
// decodeBytesEscapes is the bytes form. The unicode escapes are
// rejected here because they have no meaning in a byte literal.
//
// CPython: Objects/bytesobject.c _PyBytes_DecodeEscape
// CPython: Parser/tokenizer/helpers.c _PyTokenizer_warn_invalid_escape_sequence (message format)
func decodeBytesEscapes(s []byte) (out []byte, warnings []string, offsets []int, err error) {
	var warns []string
	var warnOffsets []int
	i := 0
	for i < len(s) {
		c := s[i]
		if c != '\\' {
			out = append(out, c)
			i++
			continue
		}
		escStart := i
		i++
		if i >= len(s) {
			return nil, nil, nil, fmt.Errorf("Trailing \\ in string") //nolint:staticcheck // Mirror CPython's exact error text.
		}
		c = s[i]
		i++
		switch c {
		case '\n':
		case '\\':
			out = append(out, '\\')
		case '\'':
			out = append(out, '\'')
		case '"':
			out = append(out, '"')
		case 'a':
			out = append(out, 0x07)
		case 'b':
			out = append(out, 0x08)
		case 'f':
			out = append(out, 0x0c)
		case 'n':
			out = append(out, '\n')
		case 'r':
			out = append(out, '\r')
		case 't':
			out = append(out, '\t')
		case 'v':
			out = append(out, 0x0b)
		case '0', '1', '2', '3', '4', '5', '6', '7':
			// CPython: Objects/bytesobject.c:_PyBytes_DecodeEscape octal branch.
			// Values > 0o377 are truncated to a byte but also emit SyntaxWarning
			// via _PyTokenizer_warn_invalid_escape_sequence in the tokenizer path.
			digitsStart := escStart + 1
			val := int(c - '0')
			for k := 0; k < 2 && i < len(s) && s[i] >= '0' && s[i] <= '7'; k++ {
				val = val*8 + int(s[i]-'0')
				i++
			}
			if val > 0o377 {
				digits := string(s[digitsStart:i])
				warns = append(warns, fmt.Sprintf(
					"\"\\%s\" is an invalid octal escape sequence. "+
						"Such sequences will not work in the future. "+
						"Did you mean \"\\\\%s\"? A raw string is also an option.",
					digits, digits))
				warnOffsets = append(warnOffsets, escStart)
			}
			out = append(out, byte(val&0xff))
		case 'x':
			if i+2 > len(s) {
				return nil, nil, nil, fmt.Errorf("truncated \\xXX escape")
			}
			v, err := parseHex(s[i : i+2])
			if err != nil {
				return nil, nil, nil, err
			}
			out = append(out, byte(v))
			i += 2
		default:
			// CPython: Parser/tokenizer/helpers.c:_PyTokenizer_warn_invalid_escape_sequence
			out = append(out, '\\', c)
			warns = append(warns, fmt.Sprintf(
				"\"\\%c\" is an invalid escape sequence. "+
					"Such sequences will not work in the future. "+
					"Did you mean \"\\\\%c\"? A raw string is also an option.",
				c, c))
			warnOffsets = append(warnOffsets, escStart)
		}
	}
	return out, warns, warnOffsets, nil
}

func parseHex(s []byte) (int, error) {
	v := 0
	for _, c := range s {
		var d int
		switch {
		case c >= '0' && c <= '9':
			d = int(c - '0')
		case c >= 'a' && c <= 'f':
			d = int(c-'a') + 10
		case c >= 'A' && c <= 'F':
			d = int(c-'A') + 10
		default:
			return 0, fmt.Errorf("invalid hex digit %q", c)
		}
		v = v<<4 | d
	}
	return v, nil
}
