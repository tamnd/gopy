// Builds the UopMeta map directly from an analyzer Analysis, bypassing the pycore_uop_metadata.h header.
//
// CPython: Tools/cases_generator/uop_metadata_generator.py:25-65 generate_names_and_flags

package main

import (
	"strings"
)

// BuildUopMetaFromAnalysis walks analysis.Uops to populate the same
// map[string]*UopMeta that ParseUopMetadata returns when reading the
// upstream header. Keys are uop names with the leading underscore
// stripped, matching ParseUopMetadata.
//
// Three tables are merged per uop name:
//   - Flags from CFlags(uop.Properties), parsed back into the same
//     HAS_*_FLAG token form ParseUopMetadata yields.
//   - Replication from uop.Replicated.
//   - Popped from a fresh Stack walked the same way the upstream
//     num_popped switch builds it.
//
// CPython: Tools/cases_generator/uop_metadata_generator.py:25-65 generate_names_and_flags
func BuildUopMetaFromAnalysis(analysis *Analysis) (map[string]*UopMeta, error) {
	out := map[string]*UopMeta{}
	get := func(name string) *UopMeta {
		stripped := strings.TrimPrefix(name, "_")
		m, ok := out[stripped]
		if !ok {
			m = &UopMeta{Name: stripped}
			out[stripped] = m
		}
		return m
	}

	for _, uop := range analysis.Uops {
		// CPython: uop_metadata_generator.py:32-35 _PyUop_Flags
		if uop.IsViable() && uop.Properties != nil && uop.Properties.Tier != 1 {
			m := get(uop.Name)
			m.Flags = parseFlagExpr(CFlags(uop.Properties))
		}
		// CPython: uop_metadata_generator.py:38-41 _PyUop_Replication
		if uop.Replicated != 0 {
			m := get(uop.Name)
			m.Replication = uint8(uop.Replicated)
		}
		// CPython: uop_metadata_generator.py:48-60 num_popped switch
		if uop.IsViable() && uop.Properties != nil && uop.Properties.Tier != 1 {
			popped, err := computeNumPopped(uop)
			if err != nil {
				return nil, err
			}
			m := get(uop.Name)
			m.Popped = popped
			m.HasPopped = true
		}
	}
	return out, nil
}

// computeNumPopped mirrors the upstream snippet:
//
//	stack = Stack()
//	for var in reversed(uop.stack.inputs):
//	    if var.peek:
//	        break
//	    stack.pop(var, null)
//	popped = (-stack.base_offset).to_c()
//
// CPython: Tools/cases_generator/uop_metadata_generator.py:51-58
func computeNumPopped(uop *Uop) (string, error) {
	stack := NewStack()
	null := NullCWriter()
	ins := uop.Stack.Inputs
	for i := len(ins) - 1; i >= 0; i-- {
		v := ins[i]
		if v.Peek {
			break
		}
		if _, err := stack.Pop(v, null); err != nil {
			return "", err
		}
	}
	return stack.BaseOffset.Neg().ToC(), nil
}
