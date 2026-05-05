// Strongly connected components and cycle detection. The PEG
// generator uses these to find left-recursive rule sets and pick a
// leader rule for each cycle.
//
// CPython: Tools/peg_generator/pegen/sccutils.py

package main

import "sort"

// stronglyConnected runs Tarjan's SCC algorithm over the directed
// graph defined by edges. Returns SCCs in reverse topological order
// (leaves first). Each SCC is sorted for stable iteration.
func stronglyConnected(nodes []string, edges map[string]map[string]bool) [][]string {
	idx := map[string]int{}
	low := map[string]int{}
	onStack := map[string]bool{}
	var stack []string
	var result [][]string
	counter := 0

	var strong func(v string)
	strong = func(v string) {
		idx[v] = counter
		low[v] = counter
		counter++
		stack = append(stack, v)
		onStack[v] = true
		for w := range edges[v] {
			if _, seen := idx[w]; !seen {
				strong(w)
				if low[w] < low[v] {
					low[v] = low[w]
				}
			} else if onStack[w] {
				if idx[w] < low[v] {
					low[v] = idx[w]
				}
			}
		}
		if low[v] == idx[v] {
			var scc []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				scc = append(scc, w)
				if w == v {
					break
				}
			}
			sort.Strings(scc)
			result = append(result, scc)
		}
	}

	for _, n := range nodes {
		if _, seen := idx[n]; !seen {
			strong(n)
		}
	}
	return result
}

// findCyclesInSCC enumerates simple cycles within scc starting at
// start. Used to identify rules common to every cycle in a SCC; that
// rule becomes the leader.
//
// CPython: Tools/peg_generator/pegen/sccutils.py find_cycles_in_scc
func findCyclesInSCC(graph map[string]map[string]bool, scc map[string]bool, start string) [][]string {
	var cycles [][]string
	path := []string{start}
	blocked := map[string]bool{start: true}
	var dfs func(node string) bool
	dfs = func(node string) bool {
		closed := false
		for next := range graph[node] {
			if !scc[next] {
				continue
			}
			if next == start {
				cyc := append([]string(nil), path...)
				cycles = append(cycles, cyc)
				closed = true
			} else if !blocked[next] {
				blocked[next] = true
				path = append(path, next)
				if dfs(next) {
					closed = true
				}
				path = path[:len(path)-1]
				delete(blocked, next)
			}
		}
		return closed
	}
	dfs(start)
	return cycles
}

// computeLeftRecursive stamps Rule.LeftRecursive and Rule.Leader on
// every rule that participates in a left-call cycle. The first-call
// graph maps each rule to the rules it can invoke at its initial
// position (computed from the InitialNamesVisitor).
//
// CPython: Tools/peg_generator/pegen/parser_generator.py compute_left_recursives
func computeLeftRecursive(g *Grammar) map[string]map[string]bool {
	rulesByName := map[string]*Rule{}
	for _, r := range g.Rules {
		rulesByName[r.Name] = r
	}
	graph := makeFirstGraph(g)
	nodes := make([]string, 0, len(graph))
	for k := range graph {
		nodes = append(nodes, k)
	}
	sort.Strings(nodes)
	sccs := stronglyConnected(nodes, graph)
	for _, scc := range sccs {
		stampSCC(scc, rulesByName, graph)
	}
	return graph
}

func stampSCC(scc []string, rulesByName map[string]*Rule, graph map[string]map[string]bool) {
	if len(scc) == 1 {
		n := scc[0]
		if graph[n][n] {
			if r := rulesByName[n]; r != nil {
				r.LeftRecursive = true
				r.Leader = true
			}
		}
		return
	}
	set := map[string]bool{}
	for _, n := range scc {
		set[n] = true
		if r := rulesByName[n]; r != nil {
			r.LeftRecursive = true
		}
	}
	leaders := map[string]bool{}
	for _, n := range scc {
		leaders[n] = true
	}
	for _, start := range scc {
		for _, cyc := range findCyclesInSCC(graph, set, start) {
			inCycle := map[string]bool{}
			for _, n := range cyc {
				inCycle[n] = true
			}
			for n := range leaders {
				if !inCycle[n] {
					delete(leaders, n)
				}
			}
		}
	}
	pick := ""
	for n := range leaders {
		if pick == "" || n < pick {
			pick = n
		}
	}
	if pick != "" && rulesByName[pick] != nil {
		rulesByName[pick].Leader = true
	}
}

// makeFirstGraph builds the first-call graph: an edge A → B exists
// if A can reach B at its initial parsing position. Requires the
// nullable analysis to have been run already.
//
// CPython: Tools/peg_generator/pegen/parser_generator.py make_first_graph
func makeFirstGraph(g *Grammar) map[string]map[string]bool {
	rulesByName := map[string]*Rule{}
	for _, r := range g.Rules {
		rulesByName[r.Name] = r
	}
	nv := &nullableVisitor{rules: rulesByName, visited: map[*Rule]bool{}, nullable: map[any]bool{}}
	for _, r := range g.Rules {
		nv.visitRule(r)
	}
	iv := &initialNamesVisitor{rules: rulesByName, nullable: nv.nullable}
	graph := map[string]map[string]bool{}
	vertices := map[string]bool{}
	for _, r := range g.Rules {
		names := iv.visitRule(r)
		graph[r.Name] = names
		for n := range names {
			vertices[n] = true
		}
	}
	for v := range vertices {
		if _, ok := graph[v]; !ok {
			graph[v] = map[string]bool{}
		}
	}
	return graph
}

// initialNamesVisitor returns the set of rule names a rule can call
// at its initial parsing position. A rule "can call" B if B is the
// first non-empty thing it tries on any alt; a nullable item lets the
// next item also count as initial.
//
// CPython: Tools/peg_generator/pegen/parser_generator.py InitialNamesVisitor
type initialNamesVisitor struct {
	rules    map[string]*Rule
	nullable map[any]bool
}

func (v *initialNamesVisitor) visitRule(r *Rule) map[string]bool {
	return v.visitRHS(r.RHS)
}

func (v *initialNamesVisitor) visitRHS(rhs *RHS) map[string]bool {
	out := map[string]bool{}
	for _, a := range rhs.Alts {
		for k := range v.visitAlt(a) {
			out[k] = true
		}
	}
	return out
}

func (v *initialNamesVisitor) visitAlt(a *Alt) map[string]bool {
	out := map[string]bool{}
	for _, ni := range a.Items {
		for k := range v.visitItem(ni.Item) {
			out[k] = true
		}
		if !v.nullable[ni] {
			break
		}
	}
	return out
}

func (v *initialNamesVisitor) visitItem(it Item) map[string]bool {
	switch x := it.(type) {
	case *NameLeaf:
		if _, ok := v.rules[x.Value]; ok {
			return map[string]bool{x.Value: true}
		}
		return map[string]bool{}
	case *Group:
		return v.visitRHS(x.RHS)
	case *RHS:
		return v.visitRHS(x)
	case *Opt:
		return v.visitItem(x.Item)
	case *Repeat0:
		return v.visitItem(x.Item)
	case *Repeat1:
		return v.visitItem(x.Item)
	case *Gather:
		return v.visitItem(x.Node)
	case *Forced:
		return v.visitItem(x.Node)
	case *PositiveLookahead:
		return v.visitItem(x.Node)
	case *NegativeLookahead:
		return v.visitItem(x.Node)
	}
	return map[string]bool{}
}
