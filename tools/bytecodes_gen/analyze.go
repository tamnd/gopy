// Stack-effect analysis (B2). Walks an InstDef signature to produce
// the named-binding view the emitter (B3) and action translator (B6)
// both consume: which input names map to which stack slots, the order
// to pop them in, the order to push outputs in, and the cache layout.
//
// CPython: Tools/cases_generator/stack.py + analyzer.py

package main

import "fmt"

// StackBinding is one named slot on the value stack. Direction tells
// the emitter whether to pop (Input) or push (Output). Index is the
// position relative to the current stack pointer (0 = TOS) at the
// moment of the operation. Variadic slots carry Sized=true and the
// runtime expression in SizeExpr.
type StackBinding struct {
	Name      string
	Direction string // "in" or "out"
	Index     int
	Sized     bool
	SizeExpr  string // textual size expression for variadic slots
	Type      string // optional :type annotation
}

// CacheBinding is one inline-cache slot. Offset is the position in
// code units relative to the start of the cache region.
type CacheBinding struct {
	Name   string
	Size   int
	Offset int
}

// SignatureAnalysis is the analyzed view of one InstDef.
type SignatureAnalysis struct {
	Name      string
	Inputs    []StackBinding // in stack-bottom-first order (so Inputs[0] is the lowest popped slot)
	Outputs   []StackBinding // in stack-bottom-first order (so Outputs[0] is pushed first)
	Caches    []CacheBinding
	Variadic  bool // any input or output is sized
	OpargUsed bool // body refers to oparg
}

// AnalyzeInst walks one InstDef and returns its SignatureAnalysis.
// Returns an error when names collide between inputs and outputs in a
// way the action translator can't disambiguate.
func AnalyzeInst(inst *InstDef) (*SignatureAnalysis, error) {
	a := &SignatureAnalysis{Name: inst.Name, OpargUsed: hasOparg(inst.Body)}

	// Inputs: walk in source order (lowest stack slot first). The
	// emitter pops them in reverse so the first listed slot ends up
	// at the bottom of the popped batch, matching CPython.
	cacheOffset := 0
	for _, in := range inst.Inputs {
		switch {
		case in.Stack != nil:
			b := StackBinding{
				Name:      in.Stack.Name,
				Direction: "in",
				Index:     len(a.Inputs),
				Sized:     in.Stack.Size != "",
				SizeExpr:  in.Stack.Size,
				Type:      in.Stack.Type,
			}
			if b.Sized {
				a.Variadic = true
			}
			a.Inputs = append(a.Inputs, b)
		case in.Cache != nil:
			a.Caches = append(a.Caches, CacheBinding{
				Name:   in.Cache.Name,
				Size:   in.Cache.Size,
				Offset: cacheOffset,
			})
			cacheOffset += in.Cache.Size
		}
	}

	seen := map[string]string{}
	for _, b := range a.Inputs {
		if b.Name == "" || b.Name == "unused" {
			continue
		}
		seen[b.Name] = "in"
	}

	for _, out := range inst.Outputs {
		if out.Stack == nil {
			continue
		}
		b := StackBinding{
			Name:      out.Stack.Name,
			Direction: "out",
			Index:     len(a.Outputs),
			Sized:     out.Stack.Size != "",
			SizeExpr:  out.Stack.Size,
			Type:      out.Stack.Type,
		}
		if b.Sized {
			a.Variadic = true
		}
		// CPython lets a name appear on both sides as long as the
		// position lines up (e.g. `(a, b -- a, c)`); the action body
		// then treats `a` as a passthrough. Reject pure aliasing
		// where the same name appears twice on the output side.
		if prev, ok := seen[b.Name]; ok && prev == "out" && b.Name != "" && b.Name != "unused" {
			return nil, fmt.Errorf("inst %s: output name %q appears twice", inst.Name, b.Name)
		}
		seen[b.Name] = "out"
		a.Outputs = append(a.Outputs, b)
	}
	return a, nil
}

// AnalyzeAll walks defs and returns one SignatureAnalysis per `inst`
// (skipping `op` fragments, which are inlined by macro expansion). The
// returned slice is in source order so the emitter's output is stable.
func AnalyzeAll(defs []Def) ([]*SignatureAnalysis, error) {
	var out []*SignatureAnalysis
	for _, d := range defs {
		inst, ok := d.(*InstDef)
		if !ok || inst.Kind != "inst" {
			continue
		}
		a, err := AnalyzeInst(inst)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}
