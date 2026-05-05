// Package warnings is the gopy port of cpython/Python/_warnings.c.
// v0.7 (1651-warn-A) lands the filter list, the default rule set,
// and the dispatch entry point _Py_Warn calls into. The per-module
// __warningregistry__ dedup follows in 1651-warn-B; the user-facing
// simplefilter / filterwarnings / resetwarnings API lands in
// 1651-warn-C.
//
// CPython: Python/_warnings.c
package warnings

import (
	"regexp"
	"sync"
)

// Action names a filter outcome. Matches the Python-level action
// strings _PyWarnings_DefaultAction stamps onto each filter.
//
// CPython: Python/_warnings.c:174 ACTION_*
type Action string

const (
	// ActionError raises the warning as an exception.
	ActionError Action = "error"
	// ActionIgnore drops the warning silently.
	ActionIgnore Action = "ignore"
	// ActionAlways prints every emission.
	ActionAlways Action = "always"
	// ActionDefault prints once per (category, module, lineno).
	ActionDefault Action = "default"
	// ActionModule prints once per (category, module).
	ActionModule Action = "module"
	// ActionOnce prints once per (category, message).
	ActionOnce Action = "once"
)

// Category mirrors the PyWarning class hierarchy. The full Python
// class port lands with the type machinery; v0.7 uses a string
// hierarchy so DeprecationWarning subclasses Warning the same way
// the C source treats it.
//
// CPython: Lib/warnings.py categories
type Category struct {
	Name   string
	Parent *Category
}

// IsSubclass mirrors PyType_IsSubtype walking the parent chain.
//
// CPython: Python/_warnings.c get_filter checks "issubclass(category, cat)"
func (c *Category) IsSubclass(other *Category) bool {
	for cur := c; cur != nil; cur = cur.Parent {
		if cur == other {
			return true
		}
	}
	return false
}

// The category singletons mirror Lib/warnings.py: every category
// inherits from Warning, with Pending and Deprecation being the
// hot ones for v0.7 because the eval loop emits both.
//
// CPython: Lib/warnings.py module body
var (
	WarningCategory                = &Category{Name: "Warning"}
	UserWarning                    = &Category{Name: "UserWarning", Parent: WarningCategory}
	DeprecationWarning             = &Category{Name: "DeprecationWarning", Parent: WarningCategory}
	PendingDeprecationWarning      = &Category{Name: "PendingDeprecationWarning", Parent: WarningCategory}
	SyntaxWarning                  = &Category{Name: "SyntaxWarning", Parent: WarningCategory}
	RuntimeWarning                 = &Category{Name: "RuntimeWarning", Parent: WarningCategory}
	FutureWarning                  = &Category{Name: "FutureWarning", Parent: WarningCategory}
	ImportWarning                  = &Category{Name: "ImportWarning", Parent: WarningCategory}
	UnicodeWarning                 = &Category{Name: "UnicodeWarning", Parent: WarningCategory}
	BytesWarning                   = &Category{Name: "BytesWarning", Parent: WarningCategory}
	ResourceWarning                = &Category{Name: "ResourceWarning", Parent: WarningCategory}
	EncodingWarning                = &Category{Name: "EncodingWarning", Parent: WarningCategory}
)

// Filter is one entry in the filter list. nil regexes match any
// value, and Lineno == 0 matches any line.
//
// CPython: Python/_warnings.c init_filters tuple shape
type Filter struct {
	Action   Action
	Message  *regexp.Regexp
	Category *Category
	Module   *regexp.Regexp
	Lineno   int
}

// matches checks whether the filter applies to a particular emission.
//
// CPython: Python/_warnings.c:200 get_filter inner match
func (f Filter) matches(message, module string, lineno int, cat *Category) bool {
	if f.Category != nil && !cat.IsSubclass(f.Category) {
		return false
	}
	if f.Message != nil && !f.Message.MatchString(message) {
		return false
	}
	if f.Module != nil && !f.Module.MatchString(module) {
		return false
	}
	if f.Lineno != 0 && f.Lineno != lineno {
		return false
	}
	return true
}

var (
	filterMu sync.RWMutex
	filters  = defaultFilters()
)

// defaultFilters mirrors _PyWarnings_InitState's seed: ignore
// DeprecationWarning unless it surfaces from __main__, ignore
// PendingDeprecationWarning everywhere, ignore ImportWarning and
// ResourceWarning by default, and emit the "default" action for
// everything else.
//
// CPython: Python/_warnings.c:1308 init_filters
func defaultFilters() []Filter {
	mainOnly := regexp.MustCompile(`^__main__$`)
	return []Filter{
		{Action: ActionDefault, Category: DeprecationWarning, Module: mainOnly},
		{Action: ActionIgnore, Category: DeprecationWarning},
		{Action: ActionIgnore, Category: PendingDeprecationWarning},
		{Action: ActionIgnore, Category: ImportWarning},
		{Action: ActionIgnore, Category: ResourceWarning},
		{Action: ActionDefault, Category: WarningCategory},
	}
}

// Filters returns a snapshot of the current filter list. The result
// shares no state with the live filter list; mutating the returned
// slice does not affect dispatch.
//
// CPython: Python/_warnings.c get_warnings_filters
func Filters() []Filter {
	filterMu.RLock()
	defer filterMu.RUnlock()
	out := make([]Filter, len(filters))
	copy(out, filters)
	return out
}

// SetFilters replaces the filter list atomically. The slice is
// copied so later mutations by the caller do not race with dispatch.
// The per-module registry is purged so dedup state cannot outlive
// the rule that produced it.
//
// CPython: Python/_warnings.c warnings_filters_mutated
func SetFilters(rules []Filter) {
	filterMu.Lock()
	filters = append([]Filter(nil), rules...)
	filterMu.Unlock()
	ResetRegistry()
}

// Reset restores the default filter list. The user-facing
// resetwarnings() forwards here.
//
// CPython: Python/_warnings.c warnings_resetwarnings
func Reset() {
	filterMu.Lock()
	filters = defaultFilters()
	filterMu.Unlock()
	ResetRegistry()
}

// findAction walks the filter list and returns the first matching
// rule's action. Falls back to ActionDefault when nothing matches,
// matching get_filter's "no entry found" branch.
//
// CPython: Python/_warnings.c:188 get_filter
func findAction(message, module string, lineno int, cat *Category) Action {
	filterMu.RLock()
	defer filterMu.RUnlock()
	for _, f := range filters {
		if f.matches(message, module, lineno, cat) {
			return f.Action
		}
	}
	return ActionDefault
}

// Insert prepends a filter, matching simplefilter's semantics
// (most-recently-added wins). The registry purge keeps a fresh
// rule from being shadowed by stale dedup state.
//
// CPython: Python/_warnings.c warnings_simple_filter
func Insert(rule Filter) {
	filterMu.Lock()
	filters = append([]Filter{rule}, filters...)
	filterMu.Unlock()
	ResetRegistry()
}

