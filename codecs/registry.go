// Package codecs is the gopy port of Python/codecs.c.
// The registry maps codec names to CodecInfo structs; search functions
// are registered via Register and looked up via Lookup after name
// normalization (lowercase, hyphens and spaces to underscores).
//
// CPython: Python/codecs.c
package codecs

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// SearchFunc is a callable that receives a normalized codec name and
// returns a CodecInfo or nil if the codec is unknown.
//
// CPython: Python/codecs.c:L50 codec_register
type SearchFunc func(name string) (*CodecInfo, error)

// CodecInfo holds the callables that implement a codec.
//
// CPython: Modules/_codecsmodule.c:L34 codec_info_new
type CodecInfo struct {
	Name   string
	Encode func(input string, errors string) ([]byte, int, error)
	Decode func(input []byte, errors string) (string, int, error)
	// IncrementalDecode is like Decode but returns (output, remaining, err).
	// remaining holds any trailing bytes that form an incomplete multibyte
	// sequence; the caller buffers them for the next chunk.
	// When nil, the incremental decoder falls back to Decode (no buffering).
	//
	// CPython: Modules/cjkcodecs/multibytecodec.c:803 mbstreamreader_iread
	IncrementalDecode func(input []byte, errors string, final bool) (string, []byte, error)
	// NewIncrementalDecoder creates a fresh, independent stateful incremental
	// decoder for this codec. The returned closure owns its own codec state
	// and accepts errors at each call so the caller can change error handling
	// without recreating the instance. Required for stateful codecs (HZ,
	// ISO-2022) where shift/escape state must survive across chunk boundaries.
	//
	// CPython: Modules/cjkcodecs/multibytecodec.c:887 mbincrdecoder_decode
	NewIncrementalDecoder func() func(input []byte, errors string, final bool) (string, []byte, error)
	// NewIncrementalEncoder creates a fresh, independent stateful incremental
	// encoder for this codec. The returned closure owns its own codec state
	// and accepts errors at each call so the caller can change error handling
	// without recreating the instance. Required for stateful codecs (HZ,
	// ISO-2022) where shift/escape state must survive across encode() calls.
	//
	// CPython: Modules/cjkcodecs/multibytecodec.c:747 mbincrencoder_encode
	NewIncrementalEncoder func() func(input string, errors string, final bool) ([]byte, int, error)
	// NewIncrementalEncoderFull is like NewIncrementalEncoder but also returns
	// state accessors for getstate/setstate support. Returns the encode func,
	// a getState func (pending runes + 8-byte codec state), and a setState func.
	//
	// CPython: Modules/cjkcodecs/multibytecodec.c:852 mbincrencoder_getstate
	NewIncrementalEncoderFull func() (
		encode func(string, string, bool) ([]byte, int, error),
		getState func() (pending []rune, cBytes [8]byte),
		setState func(pending []rune, cBytes [8]byte) error,
	)
	// IsTextEncoding reports whether this codec is a text encoding (can be
	// used with bytes.decode() / str.encode()). Binary transform codecs
	// (base64, zlib, hex) and text-transform codecs (rot-13) set this false.
	// Built-in codecs (utf-8, ascii, latin-1, etc.) default to true.
	//
	// CPython: Lib/encodings/__init__.py CodecInfo._is_text_encoding
	IsTextEncoding bool
}

// searchEntry pairs a search function with a unique registration handle.
type searchEntry struct {
	id uint64
	fn SearchFunc
}

// nonTextEncodings holds the normalized names of codecs where
// _is_text_encoding is False (binary/text transforms). bytes.decode()
// and str.encode() reject these with LookupError.
//
// CPython: Lib/encodings/__init__.py CodecInfo._is_text_encoding
var (
	nonTextMu        sync.RWMutex
	nonTextEncodings = map[string]bool{}
)

// MarkNonTextEncoding records name as a non-text encoding so that
// bytes.decode() can reject it with an appropriate LookupError.
func MarkNonTextEncoding(name string) {
	n := NormalizeName(name)
	nonTextMu.Lock()
	nonTextEncodings[n] = true
	nonTextMu.Unlock()
}

// IsTextEncoding returns false if the codec is a known non-text encoding
// (binary transform or str→str transform). Returns true for all other codecs.
//
// CPython: Objects/bytesobject.c:1554 bytes_decode_impl (_is_text_encoding check)
func IsTextEncoding(name string) bool {
	n := NormalizeName(name)
	nonTextMu.RLock()
	v := nonTextEncodings[n]
	nonTextMu.RUnlock()
	return !v
}

var (
	registryMu sync.RWMutex
	searchPath []searchEntry
	codecCache = map[string]*CodecInfo{}
	nextID     uint64
)

// Register appends fn to the codec search path and returns an opaque
// handle that can be passed to UnregisterByID to remove the entry.
//
// CPython: Python/codecs.c:L50 PyCodec_Register
func Register(fn SearchFunc) uint64 {
	id := atomic.AddUint64(&nextID, 1)
	registryMu.Lock()
	searchPath = append(searchPath, searchEntry{id, fn})
	registryMu.Unlock()
	return id
}

// UnregisterByID removes the search function registered under the given
// handle (returned by Register) and clears the codec cache.
//
// CPython: Python/codecs.c:L68 PyCodec_Unregister
func UnregisterByID(id uint64) {
	registryMu.Lock()
	defer registryMu.Unlock()
	out := searchPath[:0]
	removed := false
	for _, e := range searchPath {
		if e.id == id {
			removed = true
		} else {
			out = append(out, e)
		}
	}
	searchPath = out
	if removed {
		codecCache = map[string]*CodecInfo{}
	}
}

// Lookup finds the codec for encoding. The name is normalized before
// the search path is consulted; a cached hit skips the search.
//
// CPython: Python/codecs.c:L99 _PyCodec_Lookup
func Lookup(encoding string) (*CodecInfo, error) {
	name := NormalizeName(encoding)
	registryMu.RLock()
	if ci, ok := codecCache[name]; ok {
		registryMu.RUnlock()
		return ci, nil
	}
	entries := make([]searchEntry, len(searchPath))
	copy(entries, searchPath)
	registryMu.RUnlock()

	for _, e := range entries {
		ci, err := e.fn(name)
		if err != nil {
			return nil, err
		}
		if ci != nil {
			registryMu.Lock()
			codecCache[name] = ci
			registryMu.Unlock()
			return ci, nil
		}
	}
	return nil, fmt.Errorf("LookupError: unknown encoding: %s", encoding)
}

// NormalizeName converts an encoding name to the canonical lowercase
// form. Consecutive runs of non-alphanumeric characters are collapsed
// into a single underscore; leading/trailing underscores are kept.
//
// CPython: Lib/encodings/__init__.py:normalize_encoding
func NormalizeName(name string) string {
	var buf []byte
	punct := false
	for i := 0; i < len(name); i++ {
		c := name[i]
		if isAlnum(c) || c == '.' {
			if punct && len(buf) > 0 {
				buf = append(buf, '_')
			}
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
			buf = append(buf, c)
			punct = false
		} else {
			punct = true
		}
	}
	return string(buf)
}

func isAlnum(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}
