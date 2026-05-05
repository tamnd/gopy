// First sets and NULLABLE analysis. Mirrors the two visitors in
// cpython/Tools/peg_generator/pegen/parser_generator.py
// (NullableVisitor, InitialNamesVisitor) and pegen/first_sets.py
// (FirstSetCalculator).
//
// Nullable rules can match the empty string. First sets list the
// terminals and keywords each rule can start with. The emitter uses
// both to decide alternative ordering, left-recursion, and to skip
// alts that can never start at the current token.
//
// CPython: Tools/peg_generator/pegen/parser_generator.py
// CPython: Tools/peg_generator/pegen/first_sets.py

package main

// Analyze runs nullability and first-set analysis over g and stamps
// the results on each Rule. Returns the first-set map keyed by rule
// name. The empty string in a first set means "rule can match
// empty"; callers usually filter that out.
func Analyze(g *Grammar) map[string]map[string]bool {
	rules := make(map[string]*Rule, len(g.Rules))
	for _, r := range g.Rules {
		rules[r.Name] = r
	}
	nv := &nullableVisitor{rules: rules, visited: map[*Rule]bool{}, nullable: map[any]bool{}}
	for _, r := range g.Rules {
		nv.visitRule(r)
	}
	fc := &firstSetCalc{rules: rules, nullable: nv.nullable, sets: map[string]map[string]bool{}, inProc: map[string]bool{}}
	for _, r := range g.Rules {
		fc.visitRule(r)
	}
	return fc.sets
}

// nullableVisitor is the Go translation of NullableVisitor. It walks
// the grammar fixpoint-free (one pass per rule, recursing through
// references) and stamps Rule.LeftRecursive later separately. The
// nullable map keys are *Rule and *NamedItem.
type nullableVisitor struct {
	rules    map[string]*Rule
	visited  map[*Rule]bool
	nullable map[any]bool
}

func (v *nullableVisitor) visitRule(r *Rule) bool {
	if v.visited[r] {
		return v.nullable[r]
	}
	v.visited[r] = true
	if v.visitRHS(r.RHS) {
		v.nullable[r] = true
	}
	return v.nullable[r]
}

func (v *nullableVisitor) visitRHS(r *RHS) bool {
	for _, a := range r.Alts {
		if v.visitAlt(a) {
			return true
		}
	}
	return false
}

func (v *nullableVisitor) visitAlt(a *Alt) bool {
	for _, ni := range a.Items {
		if !v.visitNamedItem(ni) {
			return false
		}
	}
	return true
}

func (v *nullableVisitor) visitNamedItem(ni *NamedItem) bool {
	if v.visitItem(ni.Item) {
		v.nullable[ni] = true
	}
	return v.nullable[ni]
}

func (v *nullableVisitor) visitItem(it Item) bool {
	switch x := it.(type) {
	case *Forced, *PositiveLookahead, *NegativeLookahead:
		return true
	case *Opt:
		return true
	case *Repeat0:
		return true
	case *Repeat1:
		return false
	case *Gather:
		return false
	case *Cut:
		return false
	case *Group:
		return v.visitRHS(x.RHS)
	case *RHS:
		return v.visitRHS(x)
	case *NameLeaf:
		if r, ok := v.rules[x.Value]; ok {
			return v.visitRule(r)
		}
		return false
	case *StringLeaf:
		return x.Value == "" || x.Value == "''" || x.Value == `""`
	}
	return false
}

// firstSetCalc is the Go translation of FirstSetCalculator. The
// first set of a rule is the set of terminals (token kinds and
// keyword strings) at which the rule can start matching.
type firstSetCalc struct {
	rules    map[string]*Rule
	nullable map[any]bool
	sets     map[string]map[string]bool
	inProc   map[string]bool
}

func (c *firstSetCalc) visitRule(r *Rule) map[string]bool {
	if c.inProc[r.Name] {
		return map[string]bool{}
	}
	if s, ok := c.sets[r.Name]; ok {
		return s
	}
	c.inProc[r.Name] = true
	s := c.visitRHS(r.RHS)
	if c.nullable[r] {
		s[""] = true
	}
	c.sets[r.Name] = s
	delete(c.inProc, r.Name)
	return s
}

func (c *firstSetCalc) visitRHS(r *RHS) map[string]bool {
	out := map[string]bool{}
	for _, a := range r.Alts {
		for k := range c.visitAlt(a) {
			out[k] = true
		}
	}
	return out
}

func (c *firstSetCalc) visitAlt(a *Alt) map[string]bool {
	out := map[string]bool{}
	toRemove := map[string]bool{}
	for _, ni := range a.Items {
		fresh := c.visitItem(ni.Item)
		if _, neg := ni.Item.(*NegativeLookahead); neg {
			for k := range fresh {
				toRemove[k] = true
			}
		}
		for k := range fresh {
			out[k] = true
		}
		for k := range toRemove {
			delete(out, k)
		}
		// If the item cannot match empty, this alt is committed to
		// these terminals and we stop. Otherwise (Opt, Repeat0,
		// negative lookahead, or contains "") fall through to the
		// next item.
		if fresh[""] {
			continue
		}
		switch ni.Item.(type) {
		case *Opt, *NegativeLookahead, *Repeat0:
			continue
		}
		break
	}
	delete(out, "")
	return out
}

func (c *firstSetCalc) visitItem(it Item) map[string]bool {
	switch x := it.(type) {
	case *Cut:
		return map[string]bool{}
	case *Group:
		return c.visitRHS(x.RHS)
	case *PositiveLookahead:
		return c.visitItem(x.Node)
	case *NegativeLookahead:
		return c.visitItem(x.Node)
	case *Forced:
		return c.visitItem(x.Node)
	case *Opt:
		s := c.visitItem(x.Item)
		s[""] = true
		return s
	case *Repeat0:
		s := c.visitItem(x.Item)
		s[""] = true
		return s
	case *Repeat1:
		return c.visitItem(x.Item)
	case *Gather:
		return c.visitItem(x.Node)
	case *NameLeaf:
		if r, ok := c.rules[x.Value]; ok {
			out := map[string]bool{}
			for k := range c.visitRule(r) {
				out[k] = true
			}
			return out
		}
		return map[string]bool{x.Value: true}
	case *StringLeaf:
		return map[string]bool{x.Value: true}
	case *RHS:
		return c.visitRHS(x)
	}
	return map[string]bool{}
}

// IsNullable reports whether r matches the empty string. Useful for
// the emitter when deciding whether an alternative needs a sentinel
// non-nil success marker.
func IsNullable(g *Grammar, r *Rule) bool {
	rules := make(map[string]*Rule, len(g.Rules))
	for _, x := range g.Rules {
		rules[x.Name] = x
	}
	v := &nullableVisitor{rules: rules, visited: map[*Rule]bool{}, nullable: map[any]bool{}}
	return v.visitRule(r)
}
