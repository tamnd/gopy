// Package _suggestions is the gopy port of CPython's _suggestions C
// extension. It exposes _generate_suggestions(candidates, item) which
// returns the closest candidate to item using Levenshtein distance with
// MOVE_COST=2 and CASE_COST=1 (case-only substitution cheaper). The
// Python-side traceback._find_keyword_typos calls it to rank keyword
// typos before falling back to difflib.
//
// CPython: Modules/_suggestions.c
// CPython: Python/suggestions.c
package _suggestions

import (
	"fmt"
	"math"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("_suggestions", buildModule)
}

// buildModule materializes the _suggestions module dict.
//
// CPython: Modules/_suggestions.c:67 PyInit__suggestions
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_suggestions")
	d := m.Dict()
	entries := []struct {
		name string
		val  objects.Object
	}{
		{"_generate_suggestions", objects.NewBuiltinFunction("_generate_suggestions", generateSuggestions)},
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}
	return m, nil
}

const (
	moveCost = 2
	caseCost = 1
	maxStringSize  = 40
	maxCandidateItems = 750
)

// substitutionCost returns the cost of substituting character a with b.
// CPython: Python/suggestions.c:17 substitution_cost
func substitutionCost(a, b byte) int {
	if a&31 != b&31 {
		return moveCost
	}
	if a == b {
		return 0
	}
	if 'A' <= a && a <= 'Z' {
		a += 'a' - 'A'
	}
	if 'A' <= b && b <= 'Z' {
		b += 'a' - 'A'
	}
	if a == b {
		return caseCost
	}
	return moveCost
}

// levenshteinDistance computes the Levenshtein distance between a and b.
// Uses MOVE_COST=2 for insert/delete and CASE_COST=1 for case-only
// substitutions. Returns maxCost+1 when minimum is guaranteed to exceed maxCost.
//
// CPython: Python/suggestions.c:39 levenshtein_distance
func levenshteinDistance(a, b string, maxCost int) int {
	// Trim common prefix.
	for len(a) > 0 && len(b) > 0 && a[0] == b[0] {
		a = a[1:]
		b = b[1:]
	}
	// Trim common suffix.
	for len(a) > 0 && len(b) > 0 && a[len(a)-1] == b[len(b)-1] {
		a = a[:len(a)-1]
		b = b[:len(b)-1]
	}
	if len(a) == 0 || len(b) == 0 {
		return (len(a) + len(b)) * moveCost
	}
	if len(a) > maxStringSize || len(b) > maxStringSize {
		return maxCost + 1
	}

	// Prefer shorter buffer.
	if len(b) < len(a) {
		a, b = b, a
	}

	aSize := len(a)
	bSize := len(b)

	// Quick fail when a match is impossible.
	if (bSize-aSize)*moveCost > maxCost {
		return maxCost + 1
	}

	// One-row DP in place.
	buf := make([]int, aSize)
	tmp := moveCost
	for i := 0; i < aSize; i++ {
		buf[i] = tmp
		tmp += moveCost
	}

	result := 0
	for bIdx := 0; bIdx < bSize; bIdx++ {
		code := b[bIdx]
		distance := result
		result = bIdx * moveCost
		minimum := math.MaxInt64
		for idx := 0; idx < aSize; idx++ {
			substitute := distance + substitutionCost(code, a[idx])
			distance = buf[idx]
			insertDelete := result
			if distance < insertDelete {
				insertDelete = distance
			}
			insertDelete += moveCost
			if insertDelete < substitute {
				result = insertDelete
			} else {
				result = substitute
			}
			buf[idx] = result
			if result < minimum {
				minimum = result
			}
		}
		if minimum > maxCost {
			return maxCost + 1
		}
	}
	return result
}

// calculateSuggestions returns the candidate in dir closest to name, or
// "" when nothing is close enough. Mirrors _Py_CalculateSuggestions.
//
// CPython: Python/suggestions.c:128 _Py_CalculateSuggestions
func calculateSuggestions(candidates []string, name string) string {
	if len(candidates) >= maxCandidateItems {
		return ""
	}
	suggestionDistance := math.MaxInt64
	suggestion := ""
	nameSize := len(name)
	buf := make([]int, maxStringSize)
	_ = buf
	for _, item := range candidates {
		if item == name {
			continue
		}
		itemSize := len(item)
		// No more than 1/3 of the involved characters should need changed.
		maxDistance := (nameSize+itemSize+3)*moveCost/6
		// Don't take matches we've already beaten.
		if maxDistance > suggestionDistance-1 {
			maxDistance = suggestionDistance - 1
		}
		currentDistance := levenshteinDistance(name, item, maxDistance)
		if currentDistance > maxDistance {
			continue
		}
		if suggestion == "" || currentDistance < suggestionDistance {
			suggestion = item
			suggestionDistance = currentDistance
		}
	}
	return suggestion
}

// generateSuggestions implements _suggestions._generate_suggestions(candidates, item).
//
// CPython: Modules/_suggestions.c:18 _suggestions__generate_suggestions_impl
func generateSuggestions(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: _generate_suggestions() takes exactly 2 arguments")
	}
	lst, ok := args[0].(*objects.List)
	if !ok {
		return nil, fmt.Errorf("TypeError: candidates must be a list")
	}
	nameObj, ok := args[1].(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: item must be a str")
	}
	name := nameObj.Value()
	items := make([]string, lst.Len())
	for i := range items {
		s, ok := lst.Item(i).(*objects.Unicode)
		if !ok {
			return nil, fmt.Errorf("TypeError: all elements in 'candidates' must be strings")
		}
		items[i] = s.Value()
	}
	result := calculateSuggestions(items, name)
	if result == "" {
		return objects.None(), nil
	}
	return objects.NewStr(result), nil
}
