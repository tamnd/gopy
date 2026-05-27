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

// encodeUTF7 encodes a Go string to UTF-7 bytes.
//
// CPython: Objects/unicodeobject.c PyUnicode_EncodeUTF7
func encodeUTF7(input, _ string) ([]byte, int, error) {
	var out []byte
	runes := []rune(input)
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
		out = append(out, '-')
		i = j
	}
	return out, len(runes), nil
}

// decodeUTF7 decodes UTF-7 bytes to a string.
//
// CPython: Objects/unicodeobject.c:7133 PyUnicode_DecodeUTF7Stateful
func decodeUTF7(input []byte, errors string) (string, int, error) { //nolint:gocognit // direct CPython port
	handler, herr := LookupError(errors)
	if herr != nil {
		return "", 0, herr
	}

	var out []rune
	i := 0
	for i < len(input) {
		b := input[i]
		// bytes >= 0x80 or 0x00 are illegal in UTF-7 outside modified base64
		if b >= 0x80 {
			rep, newpos, herr := handler("utf-7", "unexpected byte in utf-7 stream", input, i, i+1)
			if herr != nil {
				return "", i, herr
			}
			out = append(out, []rune(rep)...)
			i = newpos
			continue
		}
		if b == '+' {
			i++
			if i >= len(input) {
				// trailing +, invalid
				rep, newpos, herr := handler("utf-7", "unexpected end of UTF-7 stream", input, i-1, i)
				if herr != nil {
					return "", i - 1, herr
				}
				out = append(out, []rune(rep)...)
				i = newpos
				continue
			}
			if input[i] == '-' {
				// +- encodes literal +
				out = append(out, '+')
				i++
				continue
			}
			// Modified Base64 encoded block
			j := i
			for j < len(input) && isBase64Char(input[j]) {
				j++
			}
			b64Bytes := input[i:j]
			// optional trailing '-'
			if j < len(input) && input[j] == '-' {
				j++
			}
			// pad to multiple of 4
			pad := (4 - len(b64Bytes)%4) % 4
			padded := make([]byte, len(b64Bytes)+pad)
			copy(padded, b64Bytes)
			for p := 0; p < pad; p++ {
				padded[len(b64Bytes)+p] = '='
			}
			decoded, derr := base64.StdEncoding.DecodeString(string(padded))
			if derr != nil {
				rep, newpos, herr := handler("utf-7", "invalid modified base64 in UTF-7 stream", input, i-1, j)
				if herr != nil {
					return "", i - 1, herr
				}
				out = append(out, []rune(rep)...)
				i = newpos
				continue
			}
			// decoded is UTF-16BE bytes
			k := 0
			for k+1 < len(decoded) {
				hi := uint16(decoded[k])<<8 | uint16(decoded[k+1])
				k += 2
				if hi >= 0xD800 && hi <= 0xDBFF {
					// high surrogate
					if k+1 < len(decoded) {
						lo := uint16(decoded[k])<<8 | uint16(decoded[k+1])
						k += 2
						r := utf16.DecodeRune(rune(hi), rune(lo))
						out = append(out, r)
					} else {
						out = append(out, rune(hi))
					}
				} else {
					out = append(out, rune(hi))
				}
			}
			i = j
			continue
		}
		// direct character
		out = append(out, rune(b))
		i++
	}
	return string(out), len(input), nil
}

func isBase64Char(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') ||
		(b >= '0' && b <= '9') || b == '+' || b == '/'
}
