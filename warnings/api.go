// User-facing filter API. Mirrors the simplefilter / filterwarnings /
// resetwarnings shape in Lib/_py_warnings.py: validation, dedup-aware
// insert with optional append, and a clear-everything reset.
//
// CPython: Lib/_py_warnings.py:254 filterwarnings, :294 simplefilter,
// :320 _add_filter, :337 resetwarnings

package warnings

import (
	"fmt"
	"regexp"
)

// validActions is the set CPython accepts for the action keyword.
// "all" is preserved as a Python-side alias for "always"; the
// dispatch switch in WarnAt treats any non-ignore, non-error action
// as a print, so "all" prints every emission like ActionAlways.
//
// CPython: Lib/_py_warnings.py:266 filterwarnings action check
var validActions = map[Action]struct{}{
	ActionError:   {},
	ActionIgnore:  {},
	ActionAlways:  {},
	ActionDefault: {},
	ActionModule:  {},
	ActionOnce:    {},
	"all":         {},
}

// Filterwarnings inserts a filter into the live list. With append
// false (the default), a duplicate is removed first and the new rule
// is prepended so it wins dispatch. With append true, the filter
// only lands at the tail when no equal entry already exists.
//
// CPython: Lib/_py_warnings.py:254 filterwarnings
func Filterwarnings(action Action, message *regexp.Regexp, category *Category, module *regexp.Regexp, lineno int, appendRule bool) error {
	if _, ok := validActions[action]; !ok {
		return fmt.Errorf("invalid action: %q", action)
	}
	if lineno < 0 {
		return fmt.Errorf("lineno must be an int >= 0")
	}
	if category == nil {
		category = WarningCategory
	}
	addFilter(Filter{
		Action:   action,
		Message:  message,
		Category: category,
		Module:   module,
		Lineno:   lineno,
	}, appendRule)
	return nil
}

// Simplefilter inserts a filter that matches every module and message,
// narrowing only on category and (optionally) line number.
//
// CPython: Lib/_py_warnings.py:294 simplefilter
func Simplefilter(action Action, category *Category, lineno int, appendRule bool) error {
	if _, ok := validActions[action]; !ok {
		return fmt.Errorf("invalid action: %q", action)
	}
	if lineno < 0 {
		return fmt.Errorf("lineno must be an int >= 0")
	}
	if category == nil {
		category = WarningCategory
	}
	addFilter(Filter{
		Action:   action,
		Category: category,
		Lineno:   lineno,
	}, appendRule)
	return nil
}

// Resetwarnings clears the filter list. Without any rules in place,
// findAction's no-entry fallback governs every emission, so the dedup
// state seeded under prior rules cannot silence a fresh warning.
//
// CPython: Lib/_py_warnings.py:337 resetwarnings
func Resetwarnings() {
	filterMu.Lock()
	filters = nil
	filterMu.Unlock()
	ResetRegistry()
}

// addFilter is the dedup-aware insert primitive. With appendRule
// false, any equal entry is dropped before the new filter lands at
// the front; with appendRule true, the filter only joins the tail
// when no equal entry already exists.
//
// CPython: Lib/_py_warnings.py:320 _add_filter
func addFilter(rule Filter, appendRule bool) {
	filterMu.Lock()
	if appendRule {
		for _, f := range filters {
			if filterEqual(f, rule) {
				filterMu.Unlock()
				ResetRegistry()
				return
			}
		}
		filters = append(filters, rule)
	} else {
		kept := make([]Filter, 0, len(filters)+1)
		kept = append(kept, rule)
		for _, f := range filters {
			if !filterEqual(f, rule) {
				kept = append(kept, f)
			}
		}
		filters = kept
	}
	filterMu.Unlock()
	ResetRegistry()
}

// filterEqual mirrors Python's tuple equality on (action, message,
// category, module, lineno). Regex equality is by source pattern
// since *regexp.Regexp pointers will rarely match across calls.
//
// CPython: Lib/_py_warnings.py:327 filters.remove(item)
func filterEqual(a, b Filter) bool {
	if a.Action != b.Action {
		return false
	}
	if a.Category != b.Category {
		return false
	}
	if a.Lineno != b.Lineno {
		return false
	}
	return regexpEqual(a.Message, b.Message) && regexpEqual(a.Module, b.Module)
}

func regexpEqual(a, b *regexp.Regexp) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.String() == b.String()
}
