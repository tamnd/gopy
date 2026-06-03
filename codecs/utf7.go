// UTF-7 codec (RFC 2152). UTF-7 encodes Unicode text as ASCII using
// base64 for non-ASCII and unsafe ASCII codepoints. Modified Base64 (no
// trailing padding `=`) is used, wrapped in `+...-` sequences. The
// literal `+` character is encoded as `+-`.
//
// CPython: Modules/_codecsmodule.c utf_7_decode/_encode
// CPython: Modules/_codecsmodule.c:146 _codecs_utf_7_decode_impl
package codecs

import (
	"encoding/base64"
	"unicode/utf16"
	"unicode/utf8"
)

func init() {
	// Register under the normalized alias used by builtinSearch.
	utf7codec := &CodecInfo{
		Name:   "utf-7",
		Encode: encodeUTF7,
		Decode: decodeUTF7,
	}
	// We add to the builtinSearch switch by registering a search func.
	Register(func(name string) (*CodecInfo, error) {
		if name == "utf_7" || name == "utf7" || name == "u7" {
			return utf7codec, nil
		}
		return nil, nil
	})
}

// direct characters (RFC 2152 Table 1 + Tab, CR, LF, SP):
// A-Za-z0-9 and most printable ASCII are "directly encoded" in UTF-7.
func isUTF7Direct(r rune) bool {
	if r >= 'A' && r <= 'Z' {
		return true
	}
	if r >= 'a' && r <= 'z' {
		return true
	}
	if r >= '0' && r <= '9' {
		return true
	}
	switch r {
	case '\t', '\n', '\r', ' ', '\'', '(', ')', ',', '-', '.', '/',
		':', '?', '"', '[', ']', '!', '#', '$', '%', '&', '*',
		';', '<', '=', '>', '@', '^', '_', '`', '{', '|', '}', '~':
		return true
	}
	return false
}

// isUTF7Base64 reports whether r is in the modified-Base64 alphabet
// (A-Za-z0-9+/). CPython uses this to decide whether a shift-out needs an
// explicit '-' terminator: a following non-Base64 character unshifts the
// sequence implicitly, so the '-' is omitted.
//
// CPython: Objects/unicodeobject.c IS_BASE64
func isUTF7Base64(r rune) bool {
	switch {
	case r >= 'A' && r <= 'Z':
		return true
	case r >= 'a' && r <= 'z':
		return true
	case r >= '0' && r <= '9':
		return true
	case r == '+' || r == '/':
		return true
	}
	return false
}

// encodeUTF7 encodes a Go string to UTF-7 bytes.
//
// CPython: Objects/unicodeobject.c PyUnicode_EncodeUTF7
func encodeUTF7(input, _ string) ([]byte, int, error) {
	var out []byte
	// Decode leniently so a lone surrogate (stored as 3-byte pseudo-UTF-8)
	// survives as a single code point; []rune would split it into three
	// U+FFFD runes and encode garbage.
	runes := lenientRunes([]byte(input))
	i := 0
	for i < len(runes) {
		r := runes[i]
		if r == '+' {
			out = append(out, '+', '-')
			i++
			continue
		}
		if isUTF7Direct(r) {
			out = append(out, byte(r))
			i++
			continue
		}
		// collect a run of non-direct chars into UTF-16BE, then base64
		var u16 []byte
		j := i
		for j < len(runes) && !isUTF7Direct(runes[j]) && runes[j] != '+' {
			r2 := runes[j]
			if r2 < 0x10000 {
				u16 = append(u16, byte(r2>>8), byte(r2))
			} else {
				// surrogate pair via UTF-16 encoding
				r1, r2s := utf16.EncodeRune(r2)
				_ = utf8.RuneError // imported for init side-effect only
				u16 = append(u16, byte(r1>>8), byte(r1), byte(r2s>>8), byte(r2s))
			}
			j++
		}
		enc := base64.StdEncoding.EncodeToString(u16)
		// strip trailing '='
		for enc != "" && enc[len(enc)-1] == '=' {
			enc = enc[:len(enc)-1]
		}
		out = append(out, '+')
		out = append(out, []byte(enc)...)
		// Emit the closing '-' only when it is needed: at end of string, or
		// when the following character is itself a Base64 char or '-' and
		// would otherwise be read as part of the shifted sequence. A plain
		// direct character (like '.') unshifts implicitly.
		if j >= len(runes) || isUTF7Base64(runes[j]) || runes[j] == '-' {
			out = append(out, '-')
		}
		i = j
	}
	return out, len(runes), nil
}

// fromBase64 returns the 6-bit value of a modified-Base64 character.
//
// CPython: Objects/unicodeobject.c:4651 FROM_BASE64
func fromBase64(c byte) uint32 {
	switch {
	case c >= 'A' && c <= 'Z':
		return uint32(c - 'A')
	case c >= 'a' && c <= 'z':
		return uint32(c-'a') + 26
	case c >= '0' && c <= '9':
		return uint32(c-'0') + 52
	case c == '+':
		return 62
	default: // '/'
		return 63
	}
}

// decodeDirect reports whether a byte decodes as itself: every ASCII byte
// except '+', which begins a Base64 shift. The decoder is permissive.
//
// CPython: Objects/unicodeobject.c:4667 DECODE_DIRECT
func decodeDirect(c byte) bool { return c <= 127 && c != '+' }

// decodeUTF7 decodes UTF-7 bytes to a string. Faithful port of CPython's
// incremental bit-accumulator decoder: it tracks the shift state, drains
// 16-bit UTF-16 units out of the Base64 bit buffer, joins surrogate pairs,
// and reports the same ill-formed/partial/padding errors CPython does.
//
// CPython: Objects/unicodeobject.c:4732 PyUnicode_DecodeUTF7Stateful
func decodeUTF7(input []byte, errors string) (string, int, error) { //nolint:gocognit // direct CPython port
	handler, herr := LookupError(errors)
	if herr != nil {
		return "", 0, herr
	}

	var out []rune
	var inShift bool
	var base64bits uint
	var base64buffer uint32
	var surrogate rune
	s := 0
	e := len(input)

	// fail runs the error handler over input[start:end], appending the
	// replacement and advancing s. It returns false when the handler raised
	// (strict mode), in which case the caller propagates the error.
	var failErr error
	fail := func(reason string, start, end int) bool {
		rep, newpos, herr := handler("utf-7", reason, input, start, end)
		if herr != nil {
			failErr = herr
			return false
		}
		out = append(out, lenientRunes([]byte(rep))...)
		s = newpos
		return true
	}

	for s < e {
		ch := input[s]
		switch {
		case inShift: // in a base-64 section
			if isBase64Char(ch) { // consume a base-64 character
				base64buffer = (base64buffer << 6) | fromBase64(ch)
				base64bits += 6
				s++
				if base64bits >= 16 {
					// enough bits for a UTF-16 value
					outCh := rune(base64buffer>>(base64bits-16)) & 0xFFFF
					base64bits -= 16
					base64buffer &= (1 << base64bits) - 1 // clear high bits
					if surrogate != 0 {
						// expecting a second surrogate
						if outCh >= 0xDC00 && outCh <= 0xDFFF {
							out = append(out, utf16.DecodeRune(surrogate, outCh))
							surrogate = 0
							continue
						}
						out = append(out, surrogate)
						surrogate = 0
					}
					if outCh >= 0xD800 && outCh <= 0xDBFF {
						surrogate = outCh // first surrogate
					} else {
						out = append(out, outCh)
					}
				}
				continue
			}
			// now leaving a base-64 section
			inShift = false
			startinpos := s
			if base64bits > 0 { // left-over bits
				if base64bits >= 6 {
					// we've seen at least one base-64 character
					s++
					if !fail("partial character in shift sequence", startinpos, s) {
						return "", 0, failErr
					}
					continue
				}
				// some bits remain; they should be zero
				if base64buffer != 0 {
					s++
					if !fail("non-zero padding bits in shift sequence", startinpos, s) {
						return "", 0, failErr
					}
					continue
				}
			}
			if surrogate != 0 && decodeDirect(ch) {
				out = append(out, surrogate)
			}
			surrogate = 0
			if ch == '-' {
				// '-' is absorbed; other terminating characters are preserved
				s++
			}
		case ch == '+':
			startinpos := s
			s++ // consume '+'
			switch {
			case s < e && input[s] == '-': // '+-' encodes '+'
				s++
				out = append(out, '+')
			case s < e && !isBase64Char(input[s]):
				s++
				if !fail("ill-formed sequence", startinpos, s) {
					return "", 0, failErr
				}
			default: // begin base64-encoded section
				inShift = true
				surrogate = 0
				base64bits = 0
				base64buffer = 0
			}
		case decodeDirect(ch): // character decodes as itself
			s++
			out = append(out, rune(ch))
		default:
			startinpos := s
			s++
			if !fail("unexpected special character", startinpos, s) {
				return "", 0, failErr
			}
		}
	}

	// end of string: in a shift sequence with no more to follow
	if inShift {
		inShift = false
		if surrogate != 0 || base64bits >= 6 || (base64bits > 0 && base64buffer != 0) {
			if !fail("unterminated shift sequence", e, e) {
				return "", 0, failErr
			}
		}
	}
	return runesToWTF8(out), len(input), nil
}

// runesToWTF8 encodes runes to a Go string, emitting lone surrogates as
// 3-byte pseudo-UTF-8 instead of the U+FFFD that Go's string(rune) yields.
// UTF-7's modified-Base64 carries UTF-16 code units verbatim, so a single
// surrogate must round-trip back to the same code point.
func runesToWTF8(runes []rune) string {
	var b []byte
	for _, r := range runes {
		if r >= 0xD800 && r <= 0xDFFF {
			b = append(b, surrogateToBytes(r)...)
		} else {
			b = utf8.AppendRune(b, r)
		}
	}
	return string(b)
}

func isBase64Char(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') ||
		(b >= '0' && b <= '9') || b == '+' || b == '/'
}
