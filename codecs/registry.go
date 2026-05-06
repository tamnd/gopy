// Package codecs is the gopy port of Python/codecs.c.
// The registry maps codec names to CodecInfo structs; search functions
// are registered via Register and looked up via Lookup after name
// normalization (lowercase, hyphens and spaces to underscores).
//
// CPython: Python/codecs.c
package codecs

import (
	"fmt"
	"strings"
	"sync"
)

// SearchFunc is a callable that receives a normalized codec name and
// returns a CodecInfo or nil if the codec is unknown.
//
// CPython: Python/codecs.c:L50 codec_register
type SearchFunc func(name string) (*CodecInfo, error)

// CodecInfo holds the four callables that implement a codec.
//
// CPython: Modules/_codecsmodule.c:L34 codec_info_new
type CodecInfo struct {
	Name   string
	Encode func(input string, errors string) ([]byte, int, error)
	Decode func(input []byte, errors string) (string, int, error)
	// StreamReader and StreamWriter are omitted until streaming codecs land.
}

var (
	registryMu sync.RWMutex
	searchPath []SearchFunc
	codecCache = map[string]*CodecInfo{}
)

// Register appends fn to the codec search path. When Lookup is called
// it tries each function in order until one returns non-nil.
//
// CPython: Python/codecs.c:L50 PyCodec_Register
func Register(fn SearchFunc) {
	registryMu.Lock()
	searchPath = append(searchPath, fn)
	registryMu.Unlock()
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
	fns := make([]SearchFunc, len(searchPath))
	copy(fns, searchPath)
	registryMu.RUnlock()

	for _, fn := range fns {
		ci, err := fn(name)
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
// form with hyphens and spaces replaced by underscores.
//
// CPython: Python/codecs.c:L72 normalizestring
func NormalizeName(name string) string {
	s := strings.ToLower(name)
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, " ", "_")
	return s
}
