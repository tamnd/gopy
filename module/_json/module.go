// Package _json is the gopy port of CPython's _json C accelerator.
// It exports the two string-handling helpers that json.py uses directly:
// scanstring and encode_basestring / encode_basestring_ascii.
//
// CPython: Modules/_json.c

package _json

import (
	"fmt"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("_json", buildModule)
}

// buildModule materializes the _json module dict. Mirrors PyInit__json /
// _json_exec.
//
// CPython: Modules/_json.c:1797 _json_exec
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_json")
	d := m.Dict()
	entries := []struct {
		name string
		val  objects.Object
	}{
		{"scanstring", objects.NewBuiltinFunction("scanstring", moduleScanstring)},
		{"encode_basestring", objects.NewBuiltinFunction("encode_basestring", moduleEncodeBasestring)},
		{"encode_basestring_ascii", objects.NewBuiltinFunction("encode_basestring_ascii", moduleEncodeBasestringASCII)},
		// make_encoder is the C-accelerated Encoder type itself.
		// Lib/json/encoder.py imports it as `c_make_encoder` and calls
		// it in the one-shot path so json.dumps avoids the Python
		// bytecode iterencode loop.
		//
		// CPython: Modules/_json.c:1889 PyEncoderType_spec
		{"make_encoder", encoderType},
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// scanstring
// ---------------------------------------------------------------------------

// moduleScanstring implements _json.scanstring(string, end, strict=True).
// It scans a JSON string literal beginning at string[end] (the character
// after the opening '"') and returns a 2-tuple (parsed_str, new_end).
//
// CPython: Modules/_json.c:155 scanstring_unicode
func moduleScanstring(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: scanstring() takes at least 2 arguments (%d given)", len(args))
	}
	pyStr, err := objects.Str(args[0])
	if err != nil {
		return nil, fmt.Errorf("TypeError: scanstring() argument 1 must be str")
	}
	endArg, ok := args[1].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: scanstring() argument 2 must be int")
	}
	endIdx, fits := endArg.Int64()
	if !fits {
		return nil, fmt.Errorf("ValueError: scanstring() end index out of range")
	}

	strict := true
	if len(args) >= 3 {
		b, err := objects.IsTruthy(args[2])
		if err != nil {
			return nil, err
		}
		strict = b
	} else if v, ok := kwargs["strict"]; ok {
		b, err := objects.IsTruthy(v)
		if err != nil {
			return nil, err
		}
		strict = b
	}

	// Convert the Python string index (code-point index) to a Go byte
	// offset. JSON strings are typically ASCII-dominant so we walk the
	// rune slice once.
	runes := []rune(pyStr)
	if int(endIdx) > len(runes) {
		return nil, fmt.Errorf("ValueError: end is out of bounds")
	}

	parsed, newEnd, err := scanStringRunes(runes, int(endIdx), strict)
	if err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{
		objects.NewStr(parsed),
		objects.NewInt(int64(newEnd)),
	}), nil
}

// scanStringRunes scans a JSON string starting at runes[pos] (the character
// after the opening '"'). Returns (parsed, new_pos_after_closing_quote).
//
// CPython: Modules/_json.c:155 scanstring_unicode
func scanStringRunes(runes []rune, pos int, strict bool) (string, int, error) {
	var b strings.Builder
	n := len(runes)
	for pos < n {
		ch := runes[pos]
		if ch == '"' {
			// Closing quote found.
			return b.String(), pos + 1, nil
		}
		if ch == '\\' {
			pos++
			if pos >= n {
				return "", 0, fmt.Errorf("ValueError: unterminated string starting at: line 1 column 1 (char 0)")
			}
			esc := runes[pos]
			switch esc {
			case '"':
				b.WriteRune('"')
			case '\\':
				b.WriteRune('\\')
			case '/':
				b.WriteRune('/')
			case 'b':
				b.WriteRune('\b')
			case 'f':
				b.WriteRune('\f')
			case 'n':
				b.WriteRune('\n')
			case 'r':
				b.WriteRune('\r')
			case 't':
				b.WriteRune('\t')
			case 'u':
				// \uXXXX — read 4 hex digits.
				if pos+4 >= n {
					return "", 0, fmt.Errorf("ValueError: Invalid \\uXXXX escape")
				}
				hexStr := string(runes[pos+1 : pos+5])
				var code uint32
				_, err := fmt.Sscanf(hexStr, "%04x", &code)
				if err != nil {
					return "", 0, fmt.Errorf("ValueError: Invalid \\uXXXX escape")
				}
				pos += 4
				r := rune(code)
				// Handle surrogate pairs.
				if utf16.IsSurrogate(r) {
					// Expect \uXXXX immediately after.
					if pos+6 < n && runes[pos+1] == '\\' && runes[pos+2] == 'u' {
						hexStr2 := string(runes[pos+3 : pos+7])
						var code2 uint32
						_, err2 := fmt.Sscanf(hexStr2, "%04x", &code2)
						if err2 == nil {
							r2 := rune(code2)
							if utf16.IsSurrogate(r2) {
								combined := utf16.DecodeRune(r, r2)
								if combined != utf8.RuneError {
									b.WriteRune(combined)
									pos += 6
									pos++
									continue
								}
							}
						}
					}
					if strict {
						return "", 0, fmt.Errorf("ValueError: Invalid \\uXXXX\\uXXXX surrogate pair")
					}
				}
				b.WriteRune(r)
			default:
				return "", 0, fmt.Errorf("ValueError: Invalid \\escape: %c", esc)
			}
			pos++
			continue
		}
		// Control character check when strict mode is on.
		if strict && ch < 0x20 {
			return "", 0, fmt.Errorf("ValueError: Invalid control character at")
		}
		b.WriteRune(ch)
		pos++
	}
	return "", 0, fmt.Errorf("ValueError: Unterminated string starting at")
}

// ---------------------------------------------------------------------------
// encode_basestring
// ---------------------------------------------------------------------------

// moduleEncodeBasestring implements _json.encode_basestring(s). It wraps
// the Go string in JSON string delimiters and escapes the mandatory
// characters: '"', '\', and ASCII control chars 0x00-0x1f.
//
// CPython: Modules/_json.c:76 encoder_encode_string (via PyUnicode path)
func moduleEncodeBasestring(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: encode_basestring() takes exactly one argument (%d given)", len(args))
	}
	s, err := objects.Str(args[0])
	if err != nil {
		return nil, fmt.Errorf("TypeError: encode_basestring() argument must be str")
	}
	return objects.NewStr(encodeBasestring(s, false)), nil
}

// moduleEncodeBasestringASCII implements _json.encode_basestring_ascii(s).
// Like encode_basestring but also escapes every non-ASCII codepoint as
// \uXXXX (or a surrogate pair for astral planes).
//
// CPython: Modules/_json.c:76 encoder_encode_string (ascii path)
func moduleEncodeBasestringASCII(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: encode_basestring_ascii() takes exactly one argument (%d given)", len(args))
	}
	s, err := objects.Str(args[0])
	if err != nil {
		return nil, fmt.Errorf("TypeError: encode_basestring_ascii() argument must be str")
	}
	return objects.NewStr(encodeBasestring(s, true)), nil
}

// encodeBasestring encodes a Go string into a JSON string literal
// including the surrounding double-quote characters. When asciiOnly is
// true, all non-ASCII codepoints are escaped as \uXXXX sequences
// (or 𐀀 surrogate pairs for codepoints above U+FFFF).
//
// CPython: Modules/_json.c:76 encoder_encode_string
func encodeBasestring(s string, asciiOnly bool) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				// Escape remaining ASCII control characters.
				fmt.Fprintf(&b, `\u%04x`, r)
			} else if asciiOnly && r > 0x7E {
				// Non-ASCII: encode as \uXXXX or surrogate pair.
				if r <= 0xFFFF {
					fmt.Fprintf(&b, `\u%04x`, r)
				} else {
					// Encode as UTF-16 surrogate pair.
					r1, r2 := utf16.EncodeRune(r)
					fmt.Fprintf(&b, `\u%04x\u%04x`, r1, r2)
				}
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
