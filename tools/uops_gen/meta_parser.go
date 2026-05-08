// Parser for pycore_uop_metadata.h. The header bundles four tables:
//
//   - _PyUop_Flags[MAX_UOP_ID+1]: per-uop bitmask of HAS_*_FLAG values
//   - _PyUop_Replication[MAX_UOP_ID+1]: how many copies the trace
//     projection emits for replicated uops (LOAD_FAST has 8, etc.)
//   - _PyOpcode_uop_name[MAX_UOP_ID+1]: human-readable name
//   - _PyUop_num_popped(int opcode, int oparg): switch returning the
//     stack-input count per uop
//
// All four are designated-init or switch-case, one entry per line.
// Whitespace and trailing commas are tolerated.
//
// CPython: Tools/cases_generator/uop_metadata_generator.py

package main

import (
	"fmt"
	"strconv"
	"strings"
)

// UopMeta is one parsed row. The four fields cover the four tables;
// missing tables leave the corresponding slot at its zero value.
type UopMeta struct {
	Name        string // uop name with leading underscore stripped
	Flags       []string
	Replication uint8
	Popped      string // raw expression as written in the header (may reference oparg)
	HasPopped   bool
}

// section names for the four parsing modes.
type metaSection int

const (
	metaNone metaSection = iota
	metaFlags
	metaRepl
	metaNames
	metaPopped
)

// ParseUopMetadata walks src, classifies each line by current section,
// and merges the four table contributions per uop name. Returns a map
// keyed by upstream name (sans leading underscore) so the emitter can
// project against the IDs list.
func ParseUopMetadata(src string) (map[string]*UopMeta, error) {
	out := map[string]*UopMeta{}
	get := func(name string) *UopMeta {
		m, ok := out[name]
		if !ok {
			m = &UopMeta{Name: name}
			out[name] = m
		}
		return m
	}

	section := metaNone
	pendingCase := ""
	for lineNo, raw := range strings.Split(src, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "const uint16_t _PyUop_Flags"):
			section = metaFlags
			continue
		case strings.HasPrefix(line, "const uint8_t _PyUop_Replication"):
			section = metaRepl
			continue
		case strings.HasPrefix(line, "const char *const _PyOpcode_uop_name"):
			section = metaNames
			continue
		case strings.HasPrefix(line, "int _PyUop_num_popped"):
			section = metaPopped
			continue
		case line == "}" || line == "};":
			if section != metaPopped {
				section = metaNone
			}
			continue
		}

		switch section {
		case metaFlags:
			if name, rhs, ok := parseDesignatedInit(line); ok {
				m := get(name)
				m.Flags = parseFlagExpr(rhs)
			}
		case metaRepl:
			if name, rhs, ok := parseDesignatedInit(line); ok {
				n, err := strconv.ParseUint(strings.TrimSpace(rhs), 10, 8)
				if err != nil {
					return nil, fmt.Errorf("line %d: bad replication: %w", lineNo+1, err)
				}
				get(name).Replication = uint8(n)
			}
		case metaNames:
			// Names are derivable from the IDs header; we ignore the
			// table here. The emitter takes names from the parsed
			// UopID list.
		case metaPopped:
			if strings.HasPrefix(line, "case _") {
				name := strings.TrimSuffix(strings.TrimPrefix(line, "case _"), ":")
				pendingCase = name
				continue
			}
			if pendingCase != "" && strings.HasPrefix(line, "return ") {
				expr := strings.TrimSuffix(strings.TrimPrefix(line, "return "), ";")
				m := get(pendingCase)
				m.Popped = strings.TrimSpace(expr)
				m.HasPopped = true
				pendingCase = ""
			}
		}
	}
	return out, nil
}

// parseDesignatedInit parses a `[_NAME] = RHS,` line and returns the
// name (without the leading underscore) and RHS (without the trailing
// comma). Returns ok=false for lines that do not match.
func parseDesignatedInit(line string) (name, rhs string, ok bool) {
	if !strings.HasPrefix(line, "[_") {
		return "", "", false
	}
	endIdx := strings.Index(line, "]")
	if endIdx < 0 {
		return "", "", false
	}
	name = strings.TrimPrefix(line[:endIdx], "[_")
	eq := strings.Index(line[endIdx:], "=")
	if eq < 0 {
		return "", "", false
	}
	rhs = strings.TrimSpace(line[endIdx+eq+1:])
	rhs = strings.TrimSuffix(rhs, ",")
	rhs = strings.TrimSpace(rhs)
	return name, rhs, true
}

// parseFlagExpr splits "HAS_X_FLAG | HAS_Y_FLAG" into individual flag
// tokens, trimming HAS_ and _FLAG so the emitter can map to Go names.
// "0" returns an empty slice.
func parseFlagExpr(rhs string) []string {
	rhs = strings.TrimSpace(rhs)
	if rhs == "0" || rhs == "" {
		return nil
	}
	parts := strings.Split(rhs, "|")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		f := strings.TrimSpace(p)
		f = strings.TrimPrefix(f, "HAS_")
		f = strings.TrimSuffix(f, "_FLAG")
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}
