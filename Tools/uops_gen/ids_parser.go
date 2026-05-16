// Parser for pycore_uop_ids.h. The header is a flat sequence of
// `#define _NAME N` (numeric uop IDs) and `#define _NAME OTHER` (uop
// IDs aliased to a Tier-1 opcode name). The trailing `#define
// MAX_UOP_ID N` pins the table size. Comments, ifdef blocks, and
// extern "C" boilerplate are skipped.
//
// CPython: Tools/cases_generator/uop_id_generator.py
// (the upstream emitter writes the same lines we read back here)

package main

import (
	"fmt"
	"strconv"
	"strings"
)

// UopID is one parsed entry from pycore_uop_ids.h. Numeric entries
// have IsAlias=false and Value set; alias entries have IsAlias=true
// and Alias set to the Tier-1 opcode name (e.g. "NOP", "LOAD_CONST").
type UopID struct {
	Name    string // upstream form with leading underscore stripped (e.g. "NOP", "LOAD_FAST_0")
	Value   uint16 // populated when IsAlias is false
	IsAlias bool
	Alias   string // populated when IsAlias is true (e.g. "NOP")
}

// ParseUopIDs walks src line by line. Returns the parsed entries in
// source order, the MAX_UOP_ID value, or an error if the header is
// malformed.
func ParseUopIDs(src string) ([]UopID, uint16, error) {
	var (
		ids   []UopID
		maxID uint16
		found bool
	)
	for lineNo, raw := range strings.Split(src, "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, "#define ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}
		name, value := fields[1], fields[2]
		if name == "MAX_UOP_ID" {
			n, err := strconv.ParseUint(value, 10, 16)
			if err != nil {
				return nil, 0, fmt.Errorf("line %d: bad MAX_UOP_ID: %w", lineNo+1, err)
			}
			maxID = uint16(n)
			found = true
			continue
		}
		if !strings.HasPrefix(name, "_") {
			continue
		}
		stripped := strings.TrimPrefix(name, "_")
		if n, err := strconv.ParseUint(value, 10, 16); err == nil {
			ids = append(ids, UopID{Name: stripped, Value: uint16(n)})
			continue
		}
		ids = append(ids, UopID{Name: stripped, IsAlias: true, Alias: value})
	}
	if !found {
		return nil, 0, fmt.Errorf("MAX_UOP_ID not found")
	}
	return ids, maxID, nil
}
