// Port of CPython's suggestion engine (Python/suggestions.c). The
// distance model charges MOVE_COST=2 for an insert/delete or a
// non-case substitution and CASE_COST=1 for a substitution that differs
// only in letter case, so a case-only typo always beats an ordinary
// one. The eval loop reuses CalculateSuggestions to append "Did you
// mean 'X'?" to the unexpected-keyword TypeError, matching the Python
// argument-binding path.
//
// CPython: Python/suggestions.c

package objects

import "math"

const (
	suggestMoveCost          = 2
	suggestCaseCost          = 1
	suggestMaxStringSize     = 40
	suggestMaxCandidateItems = 750
)

// substitutionCost returns the cost of substituting character a with b:
// zero when identical, CASE_COST when they differ only in case, and
// MOVE_COST otherwise.
//
// CPython: Python/suggestions.c:17 substitution_cost
func substitutionCost(a, b byte) int {
	if a&31 != b&31 {
		return suggestMoveCost
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
		return suggestCaseCost
	}
	return suggestMoveCost
}

// levenshteinDistance computes the bounded edit distance between a and
// b, returning maxCost+1 as soon as the running minimum can no longer
// beat maxCost.
//
// CPython: Python/suggestions.c:39 levenshtein_distance
func levenshteinDistance(a, b string, maxCost int) int {
	for a != "" && b != "" && a[0] == b[0] {
		a = a[1:]
		b = b[1:]
	}
	for a != "" && b != "" && a[len(a)-1] == b[len(b)-1] {
		a = a[:len(a)-1]
		b = b[:len(b)-1]
	}
	if a == "" || b == "" {
		return (len(a) + len(b)) * suggestMoveCost
	}
	if len(a) > suggestMaxStringSize || len(b) > suggestMaxStringSize {
		return maxCost + 1
	}
	if len(b) < len(a) {
		a, b = b, a
	}
	aSize := len(a)
	bSize := len(b)
	if (bSize-aSize)*suggestMoveCost > maxCost {
		return maxCost + 1
	}

	buf := make([]int, aSize)
	tmp := suggestMoveCost
	for i := 0; i < aSize; i++ {
		buf[i] = tmp
		tmp += suggestMoveCost
	}

	result := 0
	for bIdx := 0; bIdx < bSize; bIdx++ {
		code := b[bIdx]
		distance := result
		result = bIdx * suggestMoveCost
		minimum := math.MaxInt64
		for idx := 0; idx < aSize; idx++ {
			substitute := distance + substitutionCost(code, a[idx])
			distance = buf[idx]
			insertDelete := result
			if distance < insertDelete {
				insertDelete = distance
			}
			insertDelete += suggestMoveCost
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

// CalculateSuggestions returns the candidate closest to name, or "" when
// no candidate falls within the acceptance threshold (no more than ~1/3
// of the characters need changing, and never worse than a previously
// accepted match).
//
// CPython: Python/suggestions.c:128 _Py_CalculateSuggestions
func CalculateSuggestions(candidates []string, name string) string {
	if len(candidates) >= suggestMaxCandidateItems {
		return ""
	}
	suggestionDistance := math.MaxInt64
	suggestion := ""
	nameSize := len(name)
	for _, item := range candidates {
		if item == name {
			continue
		}
		itemSize := len(item)
		maxDistance := (nameSize + itemSize + 3) * suggestMoveCost / 6
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
