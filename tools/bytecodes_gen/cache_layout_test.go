// Cache layout parity (1621/B7). Pins the CacheSize column of the
// generated metadata against CPython's INLINE_CACHE_ENTRIES_<NAME>
// macros byte-for-byte. Drift here would silently break the v0.11
// specializer because cache offsets are computed from CacheSize.
//
// CPython: Include/internal/pycore_code.h:74 (and following) defines
// every INLINE_CACHE_ENTRIES_<NAME> as CACHE_ENTRIES(<struct>), where
// CACHE_ENTRIES = sizeof(struct)/sizeof(_Py_CODEUNIT) and a code unit
// is two bytes. Each uint16_t (or _Py_BackoffCounter) field counts as
// one entry; arrays multiply by length; unions take the maximum.

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestCacheLayoutMatchesCPython(t *testing.T) {
	cpython := os.Getenv("CPYTHON")
	if cpython == "" {
		t.Skip("CPYTHON env not set")
	}
	header, err := os.ReadFile(filepath.Join(cpython, "Include", "internal", "pycore_code.h"))
	if err != nil {
		t.Skipf("read pycore_code.h: %v", err)
	}
	bytecodes, err := os.ReadFile(filepath.Join(cpython, "Python", "bytecodes.c"))
	if err != nil {
		t.Skipf("read bytecodes.c: %v", err)
	}
	expect, err := parseInlineCacheEntries(string(header))
	if err != nil {
		t.Fatalf("parse pycore_code.h: %v", err)
	}
	if len(expect) == 0 {
		t.Fatal("no INLINE_CACHE_ENTRIES_<NAME> defines found")
	}
	defs, err := ParseBytecodes(string(bytecodes))
	if err != nil {
		t.Fatalf("ParseBytecodes: %v", err)
	}
	got := map[string]int{}
	for _, e := range CollectMetadata(defs.Order) {
		got[e.Name] = e.CacheSize
	}
	for name, want := range expect {
		have, ok := got[name]
		if !ok {
			t.Errorf("%s: not present in collected metadata", name)
			continue
		}
		if have != want {
			t.Errorf("%s: CacheSize = %d, want %d", name, have, want)
		}
	}
}

// parseInlineCacheEntries scans pycore_code.h for the INLINE_CACHE_ENTRIES_<NAME>
// defines and resolves each to its struct's size in code units (uint16
// slots). Returns map[opcode_name]code_units.
func parseInlineCacheEntries(src string) (map[string]int, error) {
	structs, err := parseCacheStructs(src)
	if err != nil {
		return nil, err
	}
	// CACHE_ENTRIES(<struct>) and the define may span two source
	// lines (the LOAD_ATTR / UNPACK_SEQUENCE forms wrap), so flatten
	// continuations first.
	flat := strings.ReplaceAll(src, "\\\n", " ")
	re := regexp.MustCompile(`#define\s+INLINE_CACHE_ENTRIES_(\w+)\s+CACHE_ENTRIES\((\w+)\)`)
	out := map[string]int{}
	for _, m := range re.FindAllStringSubmatch(flat, -1) {
		opcode, structName := m[1], m[2]
		size, ok := structs[structName]
		if !ok {
			return nil, fmt.Errorf("INLINE_CACHE_ENTRIES_%s references unknown struct %s", opcode, structName)
		}
		out[opcode] = size
	}
	return out, nil
}

// parseCacheStructs extracts every `typedef struct { ... } name;` whose
// name starts with `_Py` and ends with `Cache` and computes its size
// in 16-bit code units. Fields are limited to the set actually used in
// pycore_code.h: _Py_BackoffCounter, uint16_t (scalar or fixed array),
// and unions of those.
func parseCacheStructs(src string) (map[string]int, error) {
	out := map[string]int{}
	// Match: typedef struct { BODY } NAME;
	// BODY can contain nested {} for unions, so we walk braces by hand.
	for i := 0; i < len(src); {
		idx := strings.Index(src[i:], "typedef struct")
		if idx < 0 {
			break
		}
		i += idx
		open := strings.Index(src[i:], "{")
		if open < 0 {
			break
		}
		bodyStart := i + open + 1
		// Find matching close brace.
		depth := 1
		j := bodyStart
		for j < len(src) && depth > 0 {
			switch src[j] {
			case '{':
				depth++
			case '}':
				depth--
			}
			j++
		}
		if depth != 0 {
			return nil, fmt.Errorf("unbalanced braces near offset %d", i)
		}
		body := src[bodyStart : j-1]
		// After the close brace is the struct's typedef name, then ';'.
		semi := strings.Index(src[j:], ";")
		if semi < 0 {
			break
		}
		name := strings.TrimSpace(src[j : j+semi])
		i = j + semi + 1
		if !strings.HasPrefix(name, "_Py") || !strings.HasSuffix(name, "Cache") {
			continue
		}
		size, err := sizeInCodeUnits(body)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		out[name] = size
	}
	return out, nil
}

// sizeInCodeUnits walks a struct body (everything between the outer
// braces) and returns its size in 16-bit code units. Recognizes
// _Py_BackoffCounter, uint16_t, and unions; rejects anything else so
// drift in the field types fails loud.
func sizeInCodeUnits(body string) (int, error) {
	total := 0
	for i := 0; i < len(body); {
		// Skip whitespace and comments.
		switch {
		case body[i] == ' ' || body[i] == '\t' || body[i] == '\n':
			i++
			continue
		case strings.HasPrefix(body[i:], "//"):
			nl := strings.IndexByte(body[i:], '\n')
			if nl < 0 {
				return total, nil
			}
			i += nl + 1
			continue
		case strings.HasPrefix(body[i:], "/*"):
			end := strings.Index(body[i:], "*/")
			if end < 0 {
				return total, nil
			}
			i += end + 2
			continue
		case strings.HasPrefix(body[i:], "union"):
			open := strings.IndexByte(body[i:], '{')
			if open < 0 {
				return 0, fmt.Errorf("union missing brace")
			}
			depth := 1
			j := i + open + 1
			start := j
			for j < len(body) && depth > 0 {
				switch body[j] {
				case '{':
					depth++
				case '}':
					depth--
				}
				j++
			}
			members := strings.Split(body[start:j-1], ";")
			maxSize := 0
			for _, m := range members {
				m = strings.TrimSpace(m)
				if m == "" {
					continue
				}
				n, err := fieldSize(m)
				if err != nil {
					return 0, err
				}
				if n > maxSize {
					maxSize = n
				}
			}
			total += maxSize
			// Skip past the closing brace and the union's tag (if any) up
			// through the trailing ';'.
			semi := strings.IndexByte(body[j:], ';')
			if semi < 0 {
				return 0, fmt.Errorf("union missing semicolon")
			}
			i = j + semi + 1
			continue
		}
		// Read one field (up to ';').
		semi := strings.IndexByte(body[i:], ';')
		if semi < 0 {
			break
		}
		field := strings.TrimSpace(body[i : i+semi])
		if field != "" {
			n, err := fieldSize(field)
			if err != nil {
				return 0, err
			}
			total += n
		}
		i += semi + 1
	}
	return total, nil
}

// fieldSize returns the code-unit count of one struct field. Accepts
// `_Py_BackoffCounter NAME`, `uint16_t NAME`, or `uint16_t NAME[N]`.
func fieldSize(field string) (int, error) {
	field = strings.TrimSpace(field)
	// Pull off the type token.
	ws := strings.IndexAny(field, " \t")
	if ws < 0 {
		return 0, fmt.Errorf("malformed field %q", field)
	}
	typ := field[:ws]
	rest := strings.TrimSpace(field[ws:])
	switch typ {
	case "_Py_BackoffCounter", "uint16_t":
		// Look for [N]. Anything else (scalar) is one slot.
		lb := strings.IndexByte(rest, '[')
		if lb < 0 {
			return 1, nil
		}
		rb := strings.IndexByte(rest[lb:], ']')
		if rb < 0 {
			return 0, fmt.Errorf("missing ] in %q", field)
		}
		n, err := strconv.Atoi(strings.TrimSpace(rest[lb+1 : lb+rb]))
		if err != nil {
			return 0, fmt.Errorf("array size in %q: %w", field, err)
		}
		return n, nil
	default:
		return 0, fmt.Errorf("unsupported field type %q in %q", typ, field)
	}
}
