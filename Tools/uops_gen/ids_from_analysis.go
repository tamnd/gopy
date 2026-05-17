// Builds the UopID list directly from an analyzer Analysis, bypassing the pycore_uop_ids.h header.
//
// CPython: Tools/cases_generator/uop_id_generator.py:24-51 generate_uop_ids

package main

import (
	"sort"
	"strings"
)

// BuildUopIDsFromAnalysis walks analysis.Uops in name-sorted order and
// produces the same []UopID + maxID pair that ParseUopIDs returns when
// reading the upstream header. The caller hands the result to
// EmitIDsFile unchanged.
//
// The numbering follows the upstream rules exactly: _EXIT_TRACE and
// _SET_IP are pre-defined at IDs 300/301 (or 1/2 when distinctNamespace
// is set), Tier-1 uops are filtered out, and an implicitly-created uop
// that is not replicated emits as an alias to its base Tier-1 opcode
// (matching `#define _NAME NAME` in the header).
//
// CPython: Tools/cases_generator/uop_id_generator.py:24-51 generate_uop_ids
//
//nolint:unparam // error return mirrors ParseUopIDs so the two paths are interchangeable in main.go
func BuildUopIDsFromAnalysis(analysis *Analysis, distinctNamespace bool) ([]UopID, uint16, error) {
	nextID := uint16(300)
	if distinctNamespace {
		nextID = 1
	}

	var ids []UopID
	// _EXIT_TRACE and _SET_IP come first by convention.
	// CPython: Tools/cases_generator/uop_id_generator.py:31-36
	ids = append(ids, UopID{Name: "EXIT_TRACE", Value: nextID})
	nextID++
	ids = append(ids, UopID{Name: "SET_IP", Value: nextID})
	nextID++

	preDefined := map[string]struct{}{
		"_EXIT_TRACE": {},
		"_SET_IP":     {},
	}

	// Walk uops sorted by uop.Name (the canonical underscore-prefixed
	// spelling), matching the upstream `sorted(uops)` over (name, uop)
	// pairs. Map keys in our analyzer mix underscored (op()) and bare
	// (inst()) entries, so we cannot sort on map keys directly.
	// CPython: Tools/cases_generator/uop_id_generator.py:38-40
	uopList := make([]*Uop, 0, len(analysis.Uops))
	for _, u := range analysis.Uops {
		uopList = append(uopList, u)
	}
	sort.Slice(uopList, func(i, j int) bool { return uopList[i].Name < uopList[j].Name })

	for _, uop := range uopList {
		name := uop.Name
		if _, ok := preDefined[name]; ok {
			continue
		}
		if uop.Properties != nil && uop.Properties.Tier == 1 {
			continue
		}
		// Names in analysis are stored with the leading underscore.
		// The UopID.Name we produce strips it to match ParseUopIDs.
		stripped := strings.TrimPrefix(name, "_")
		// CPython: Tools/cases_generator/uop_id_generator.py:45-49
		if uop.ImplicitlyCreated && !distinctNamespace && uop.Replicated == 0 {
			ids = append(ids, UopID{
				Name:    stripped,
				IsAlias: true,
				Alias:   stripped, // alias target is name without leading underscore
			})
			continue
		}
		ids = append(ids, UopID{Name: stripped, Value: nextID})
		nextID++
	}

	maxID := nextID - 1
	return ids, maxID, nil
}
