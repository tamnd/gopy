// Per-module __warningregistry__ dedup. Mirrors the C-level
// already_warned check in warn_explicit: each module gets a registry
// (a Go map standing in for CPython's per-module dict), keyed by the
// shape of the action.
//
// CPython: Python/_warnings.c:285 already_warned

package warnings

import "sync"

type registryKey struct {
	action  Action
	message string
	module  string
	lineno  int
	cat     *Category
}

var (
	registryMu sync.Mutex
	registry   = map[string]map[registryKey]struct{}{}
)

// onceRegistryBucket is the synthetic module name CPython carries
// the ActionOnce table under (module-level "once" filter shares one
// dict across the whole interpreter).
//
// CPython: Python/_warnings.c get_once_registry
const onceRegistryBucket = "__once__"

// shouldEmit checks the dedup state for the (module, action,
// category, message, lineno) combination and records the emission
// when the action is one of the once-per-shape rules.
//
// CPython: Python/_warnings.c:558 warn_explicit dedup branch
func shouldEmit(action Action, category *Category, message, module string, lineno int) bool {
	key, dedup := registryKeyFor(action, category, message, module, lineno)
	if !dedup {
		return true
	}
	bucket := module
	if action == ActionOnce {
		bucket = onceRegistryBucket
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	entries, ok := registry[bucket]
	if !ok {
		entries = map[registryKey]struct{}{}
		registry[bucket] = entries
	}
	if _, seen := entries[key]; seen {
		return false
	}
	entries[key] = struct{}{}
	return true
}

// registryKeyFor produces the dedup key matching CPython's
// already_warned hash basis. ActionAlways disables dedup; the
// once / module / default actions narrow the key to the shape
// CPython uses to track each one.
//
// CPython: Python/_warnings.c:285 already_warned key construction
func registryKeyFor(action Action, category *Category, message, module string, lineno int) (registryKey, bool) {
	switch action {
	case ActionAlways, ActionError, ActionIgnore:
		return registryKey{}, false
	case ActionOnce:
		return registryKey{action: ActionOnce, cat: category, message: message}, true
	case ActionModule:
		return registryKey{action: ActionModule, cat: category, module: module}, true
	case ActionDefault:
		return registryKey{action: ActionDefault, cat: category, module: module, lineno: lineno}, true
	}
	return registryKey{}, false
}

// ResetRegistry drops every per-module __warningregistry__ entry,
// forcing the next emission for any (module, action, category,
// message, lineno) shape to print again. Used by tests and by the
// C-level filters_mutated invalidation hook.
//
// CPython: Python/_warnings.c warnings_filters_mutated registry purge
func ResetRegistry() {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry = map[string]map[registryKey]struct{}{}
}
