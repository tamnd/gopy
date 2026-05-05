// Specialization family wiring (B5). A `family(NAME, counter) = { BASE,
// ADAPTIVE_1, ADAPTIVE_2 };` line groups one base opcode with its
// specialized variants. The specializer (v0.11) is what populates the
// adaptive paths at runtime; until then v0.6 emits each adaptive
// variant as a fallback to the base.
//
// CPython: Tools/cases_generator/analyzer.py family handling.

package main

// FamilyMap is variant-name -> base-name. The family declaration's name
// is the base opcode (e.g. `family(RESUME, 0) = { RESUME_CHECK }`); the
// listed members are the adaptive variants. Variants in the map should
// dispatch to the base case until v0.11's specializer is in place.
type FamilyMap map[string]string

// BuildFamilyMap walks defs and returns the variant-to-base map.
// Members that match the family name are skipped (some families list
// the base alongside its variants).
func BuildFamilyMap(defs []Def) FamilyMap {
	out := FamilyMap{}
	for _, d := range defs {
		f, ok := d.(*FamilyDef)
		if !ok {
			continue
		}
		for _, m := range f.Members {
			if m == f.Name {
				continue
			}
			out[m] = f.Name
		}
	}
	return out
}

// ResolveBase returns the base opcode for name, or name itself when it
// is already the base or not part of any family.
func (fm FamilyMap) ResolveBase(name string) string {
	if base, ok := fm[name]; ok {
		return base
	}
	return name
}
