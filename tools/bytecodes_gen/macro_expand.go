// Macro expansion (B4). A `macro(NAME) = OP1 + OP2 + cache/N;` line
// composes one named instruction out of an ordered list of `op`
// fragments and inline cache slots. Expansion concatenates the bodies
// and recomputes the stack signature so the result looks the same as a
// hand-written `inst` to B3 / B6.
//
// The result is returned as a synthetic InstDef so downstream stages
// don't need to know whether the source was a single inst or a macro.
//
// CPython: Tools/cases_generator/analyzer.py analyze_macro.

package main

import "fmt"

// ExpandMacros walks defs, resolves each MacroDef into a synthetic
// InstDef, and returns the union of real `inst` defs plus the expanded
// macros. Source order of the inst list is preserved; expanded macros
// are appended after the last source-order inst (mirroring CPython's
// generator, which emits hand-written insts first then macros).
func ExpandMacros(defs *Defs) ([]*InstDef, error) {
	opByName := map[string]*InstDef{}
	for _, d := range defs.Insts {
		if d.Kind == "op" {
			opByName[d.Name] = d
		}
	}

	out := make([]*InstDef, 0, len(defs.Insts))
	for _, d := range defs.Insts {
		if d.Kind == "inst" {
			out = append(out, d)
		}
	}

	for _, m := range defs.Macros {
		expanded, err := expandMacro(m, opByName)
		if err != nil {
			return nil, err
		}
		out = append(out, expanded)
	}
	return out, nil
}

// expandMacro composes one MacroDef into a synthetic InstDef. The
// expansion rule mirrors CPython's: walk the uop list left-to-right,
// thread stack effects through (later op's inputs match earlier op's
// outputs by position), and concatenate bodies.
func expandMacro(m *MacroDef, ops map[string]*InstDef) (*InstDef, error) {
	out := &InstDef{Kind: "inst", Name: m.Name, Line: m.Line}

	// Track the "live" stack at each step as the ordered list of
	// outputs the previous fragment produced. A new fragment's inputs
	// peel off the tail of that list; anything its inputs don't claim
	// stays as live for the merged signature.
	var live []StackEffect

	for _, u := range m.UOps {
		if u.Cache != nil {
			out.Inputs = append(out.Inputs, Input{Cache: u.Cache})
			continue
		}
		frag, ok := ops[u.Name]
		if !ok {
			return nil, fmt.Errorf("macro %s: unknown op %s", m.Name, u.Name)
		}
		fragInputs := stackOnly(frag.Inputs)
		// The fragment consumes len(fragInputs) values from the live
		// stack. Anything beyond that is supplied by the macro's
		// caller and becomes a real input.
		need := len(fragInputs)
		if need > len(live) {
			extra := fragInputs[:need-len(live)]
			for _, e := range extra {
				out.Inputs = append(out.Inputs, Input{Stack: dupStack(e)})
			}
			live = nil
		} else if need > 0 {
			live = live[:len(live)-need]
		}

		// Frag outputs become the new tail of live.
		for _, o := range frag.Outputs {
			if o.Stack == nil {
				continue
			}
			live = append(live, *dupStack(*o.Stack))
		}

		// Body concatenates.
		out.Body = append(out.Body, frag.Body...)
	}

	for _, s := range live {
		out.Outputs = append(out.Outputs, Output{Stack: dupStack(s)})
	}
	return out, nil
}

// stackOnly drops cache entries from an Input slice, leaving the
// stack-effect projection that drives stack composition.
func stackOnly(ins []Input) []StackEffect {
	out := make([]StackEffect, 0, len(ins))
	for _, in := range ins {
		if in.Stack != nil {
			out = append(out, *in.Stack)
		}
	}
	return out
}

func dupStack(s StackEffect) *StackEffect {
	c := s
	return &c
}
