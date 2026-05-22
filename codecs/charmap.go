// Charmap codec helpers shared by every single-byte encoding registered
// in this package. A charmap codec is fully described by a 256-entry
// decode table (byte i to rune; -1 marks an unmapped slot) plus the
// inverse map for encode. CPython builds the inverse on the Python side
// (Lib/codecs.py charmap_build) and dispatches encode / decode through
// the C helpers below.
//
// CPython: Modules/_codecs/charmap.c PyUnicode_DecodeCharmap
// CPython: Modules/_codecs/charmap.c PyUnicode_EncodeCharmap

package codecs

import (
	"fmt"
	"unicode/utf8"
)

// Charmap is a single-byte codec described by a 256-entry decode table.
// The struct caches the precomputed inverse so encode is O(input).
//
// CPython: Modules/_codecs/charmap.c PyUnicode_DecodeCharmap
type Charmap struct {
	Name   string
	Decode [256]rune
	encode map[rune]byte
}

// NewCharmap builds the inverse encode map up front. Unmapped slots
// (decode[i] == -1) stay out of the inverse so encode can detect them.
//
// CPython: Lib/codecs.py charmap_build
func NewCharmap(name string, decode [256]rune) *Charmap {
	cm := &Charmap{Name: name, Decode: decode}
	cm.encode = make(map[rune]byte, 256)
	for b, r := range decode {
		if r >= 0 {
			cm.encode[r] = byte(b)
		}
	}
	return cm
}

// Info wraps the charmap as a CodecInfo so it can be returned from a
// SearchFunc and cached by the registry.
func (cm *Charmap) Info() *CodecInfo {
	return &CodecInfo{
		Name:   cm.Name,
		Encode: cm.encodeFn,
		Decode: cm.decodeFn,
	}
}

// decodeFn maps each input byte through the decode table. -1 entries
// invoke the error handler with reason "character maps to <undefined>",
// matching CPython.
//
// CPython: Modules/_codecs/charmap.c PyUnicode_DecodeCharmap
func (cm *Charmap) decodeFn(input []byte, errors string) (string, int, error) {
	var out []rune
	i := 0
	for i < len(input) {
		r := cm.Decode[input[i]]
		if r < 0 {
			handler, herr := LookupError(errors)
			if herr != nil {
				return "", i, herr
			}
			rep, newpos, herr := handler(cm.Name, "character maps to <undefined>", input, i, i+1)
			if herr != nil {
				return "", i, herr
			}
			out = append(out, []rune(rep)...)
			i = newpos
			continue
		}
		out = append(out, r)
		i++
	}
	s := string(out)
	return s, len(s), nil
}

// encodeFn looks each rune up in the inverse map. Unmapped runes go to
// the error handler with reason "character maps to <undefined>".
//
// CPython: Modules/_codecs/charmap.c PyUnicode_EncodeCharmap
func (cm *Charmap) encodeFn(input string, errors string) ([]byte, int, error) {
	out := make([]byte, 0, len(input))
	bytesInput := []byte(input)
	i := 0
	for i < len(bytesInput) {
		r, sz := utf8.DecodeRune(bytesInput[i:])
		if b, ok := cm.encode[r]; ok {
			out = append(out, b)
			i += sz
			continue
		}
		handler, herr := LookupError(errors)
		if herr != nil {
			return nil, i, herr
		}
		rep, _, herr := handler(cm.Name, "character maps to <undefined>", bytesInput, i, i+sz)
		if herr != nil {
			return nil, i, herr
		}
		// Encode replacement runes back through the same table so the
		// handler's replacement honors the codec's range.
		for _, rr := range rep {
			if b, ok := cm.encode[rr]; ok {
				out = append(out, b)
				continue
			}
			return nil, i, fmt.Errorf("UnicodeEncodeError: %s codec can't encode character %q from replacement", cm.Name, rr)
		}
		i += sz
	}
	return out, len(out), nil
}
