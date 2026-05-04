package errors

// SuggestAttr returns a "Did you mean: 'name'?" candidate for an
// AttributeError, picking the closest match from candidates by
// Levenshtein distance. Returns "" when nothing is close enough.
//
// CPython: Python/suggestions.c:L335 _Py_Offer_Suggestions for getattr
func SuggestAttr(name string, candidates []string) string {
	return bestMatch(name, candidates)
}

// SuggestKey returns a "Did you mean: 'key'?" candidate for a
// KeyError. CPython only suggests for string keys against string keys
// in a dict; gopy applies the same rule at the call site.
//
// CPython: Python/suggestions.c:L398 _Py_Offer_Suggestions for KeyError
func SuggestKey(key string, candidates []string) string {
	return bestMatch(key, candidates)
}

// bestMatch returns the candidate within MOVE_COST * (len(name)/2) edit
// distance of name. Empty string when nothing qualifies. Mirrors
// calculate_suggestions.
//
// CPython: Python/suggestions.c:L240 calculate_suggestions
func bestMatch(name string, candidates []string) string {
	if name == "" || len(candidates) == 0 {
		return ""
	}
	const maxCandidates = 100
	if len(candidates) > maxCandidates {
		return ""
	}
	bestDist := levenshteinMaxCost(name)
	best := ""
	for _, c := range candidates {
		if c == name {
			continue
		}
		d := levenshtein(name, c, bestDist+1)
		if d < bestDist {
			bestDist = d
			best = c
		}
	}
	return best
}

// levenshteinMaxCost is CPython's cap on suggestion distance: half the
// length of the name (in MOVE_COST units, then divided again). gopy
// keeps the cap simple: floor(len(name)/2).
//
// CPython: Python/suggestions.c:L274 MOVE_COST
func levenshteinMaxCost(name string) int {
	half := len(name) / 2
	if half < 1 {
		return 1
	}
	return half
}

// levenshtein computes the edit distance between a and b using the
// classic two-row DP. Returns maxDist when the running minimum already
// exceeds maxDist (early exit matches CPython's bounded variant).
//
// CPython: Python/suggestions.c:L75 levenshtein_distance
func levenshtein(a, b string, maxDist int) int {
	if a == b {
		return 0
	}
	if a == "" {
		return len(b)
	}
	if b == "" {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		rowMin := curr[0]
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = minInt3(curr[j-1]+1, prev[j]+1, prev[j-1]+cost)
			if curr[j] < rowMin {
				rowMin = curr[j]
			}
		}
		if rowMin >= maxDist {
			return maxDist
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

func minInt3(a, b, c int) int {
	return min(a, b, c)
}
